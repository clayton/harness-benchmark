"""Run SUT launch commands, optionally with stakeholder Q&A rounds and budget guards."""

from __future__ import annotations

import os
import signal
import subprocess
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable

from hb.budget import (
    BudgetPlan,
    check_budget,
    plan_budget,
    remaining_timeout_s,
)
from hb.dialogue import (
    DialogueLog,
    answer_as_human,
    answer_as_proxy,
    build_sut_user_message,
    extract_question,
)
from hb.models import Budget, Config, InteractionMode, Scenario, Telemetry
from hb.telemetry import parse_agent_log


BudgetEventFn = Callable[[str], None]


@dataclass
class ExecuteResult:
    returncode: int
    wall_ms: int
    sut_telemetry: Telemetry
    dialogue: DialogueLog
    log_path: Path
    qa_rounds: int
    budget_exceeded: bool = False
    budget_reason: str | None = None
    budget_warnings: list[str] = field(default_factory=list)
    killed: bool = False


def _write_prompt_file(worktree: Path, user_message: str) -> Path:
    """Write SUT user message to HB_PROMPT.txt (avoids ARG_MAX on long dialogues)."""
    path = worktree / "HB_PROMPT.txt"
    path.write_text(user_message, encoding="utf-8")
    return path


def _add_opt(a: int | float | None, b: int | float | None) -> int | float | None:
    if a is None and b is None:
        return None
    return (a or 0) + (b or 0)


def _merge_usage(prior: Telemetry | None, live: Telemetry) -> Telemetry:
    """Sum numeric usage fields for cumulative budget checks."""
    p = prior or Telemetry()
    return Telemetry(
        tokens_in=_add_opt(p.tokens_in, live.tokens_in),  # type: ignore[arg-type]
        tokens_out=_add_opt(p.tokens_out, live.tokens_out),  # type: ignore[arg-type]
        tokens_cache=_add_opt(p.tokens_cache, live.tokens_cache),  # type: ignore[arg-type]
        estimated_usd=_add_opt(p.estimated_usd, live.estimated_usd),  # type: ignore[arg-type]
        turns=_add_opt(p.turns, live.turns),  # type: ignore[arg-type]
        tool_calls=_add_opt(p.tool_calls, live.tool_calls),  # type: ignore[arg-type]
        proxy_estimated_usd=p.proxy_estimated_usd,
        proxy_tokens_in=p.proxy_tokens_in,
        proxy_tokens_out=p.proxy_tokens_out,
    )


def _kill_process_group(proc: subprocess.Popen) -> None:
    """Terminate the whole process group started with start_new_session=True."""
    if proc.poll() is not None:
        return
    try:
        os.killpg(proc.pid, signal.SIGTERM)
    except (ProcessLookupError, PermissionError, OSError):
        try:
            proc.terminate()
        except Exception:
            pass
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError, OSError):
            try:
                proc.kill()
            except Exception:
                pass
        try:
            proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            pass


def _sum_telemetry(
    parts: list[Telemetry], wall_ms: int, qa_rounds: int, interaction: InteractionMode
) -> Telemetry:
    tin = tout = cache = 0
    cost = 0.0
    turns = tool_calls = 0
    for t in parts:
        tin += t.tokens_in or 0
        tout += t.tokens_out or 0
        cache += t.tokens_cache or 0
        cost += t.estimated_usd or 0.0
        turns += t.turns or 0
        tool_calls += t.tool_calls or 0
    return Telemetry(
        wall_ms=wall_ms,
        tokens_in=tin or None,
        tokens_out=tout or None,
        tokens_cache=cache or None,
        estimated_usd=cost or None,
        turns=turns or None,
        tool_calls=tool_calls or None,
        human_interventions=qa_rounds if interaction == InteractionMode.human else 0,
        qa_rounds=qa_rounds,
    )


def run_launch_once(
    launch_cmd: str,
    worktree: Path,
    log_path: Path,
    *,
    timeout: int,
    append_mode: bool = False,
    plan: BudgetPlan | None = None,
    wall_s_at_start: float = 0.0,
    prior_telemetry: Telemetry | None = None,
    harness: str | None = None,
    on_event: BudgetEventFn | None = None,
    poll_interval: float = 2.0,
) -> tuple[int, str, bool, str | None]:
    """
    Run one launch command, streaming stdout to log_path.

    When plan is set, polls the log for cost/turns and kills the process group
    if a hard budget limit is crossed (or wall/timeout expires).

    Returns (returncode, log_text, killed, kill_reason).
    """
    mode = "a" if append_mode else "w"
    killed = False
    kill_reason: str | None = None
    warned: set[str] = set()

    with log_path.open(mode, encoding="utf-8") as logf:
        header = (
            f"\n# launch\n# {launch_cmd[:500]}...\n"
            if append_mode
            else f"# launch\n# {launch_cmd[:800]}\n"
        )
        logf.write(header)
        logf.flush()

        proc = subprocess.Popen(
            launch_cmd,
            cwd=str(worktree),
            shell=True,
            stdout=logf,
            stderr=subprocess.STDOUT,
            text=True,
            env=os.environ.copy(),
            start_new_session=True,
        )

        round_start = time.monotonic()
        deadline = round_start + max(1, timeout)
        try:
            while True:
                rc = proc.poll()
                if rc is not None:
                    break

                now = time.monotonic()
                if now >= deadline:
                    killed = True
                    kill_reason = f"timeout after {timeout}s (per-round / remaining budget)"
                    if on_event:
                        on_event(f"KILL {kill_reason}")
                    _kill_process_group(proc)
                    break

                if plan is not None:
                    wall_s = wall_s_at_start + (now - round_start)
                    try:
                        live = parse_agent_log(log_path, harness=harness)
                    except Exception:
                        live = Telemetry()
                    combined = _merge_usage(prior_telemetry, live)
                    chk = check_budget(plan, wall_s=wall_s, telemetry=combined)
                    if chk.exceeded:
                        killed = True
                        kill_reason = "; ".join(chk.reasons)
                        if on_event:
                            on_event(f"KILL budget exceeded: {kill_reason}")
                        _kill_process_group(proc)
                        break
                    if chk.should_warn and on_event:
                        key = "|".join(chk.warn_reasons)
                        if key not in warned:
                            warned.add(key)
                            on_event(f"WARN {key}")

                time.sleep(poll_interval)
        except Exception:
            _kill_process_group(proc)
            raise

        rc = proc.returncode if proc.returncode is not None else (-15 if killed else 1)

    text = log_path.read_text(encoding="utf-8", errors="replace")
    return rc, text, killed, kill_reason


def execute_with_interaction(
    *,
    scenario: Scenario,
    config: Config,
    worktree: Path,
    run_dir: Path,
    timeout: int = 1800,
    human_wait_seconds: int = 3600,
    on_budget_event: BudgetEventFn | None = None,
    poll_interval: float = 2.0,
) -> ExecuteResult:
    """
    Run the SUT, optionally answering STAKEHOLDER_QUESTION blocks via proxy/human.

    Enforces config.budget:
      - preflight: clamp timeout to max_minutes
      - mid-run: kill process group on max_usd / max_turns / max_tokens / wall
      - soft warn at ~80% of any limit
      - between Q&A rounds: refuse to start another if already over budget

    Unattended: single launch with protocol appendix (questions ignored if asked).
    Proxy/human: multi-round until no question or max rounds.
    """
    opts = config.harness_options or {}
    template = opts.get("launch_headless")
    if not template:
        raise ValueError(f"Config {config.id} has no harness_options.launch_headless")

    interaction = config.interaction
    log_path = run_dir / "agent.log"
    dialogue = DialogueLog()
    dialogue.add("system", f"interaction={interaction.value}")

    plan = plan_budget(config.budget or Budget(), timeout)
    for w in plan.warnings:
        if on_budget_event:
            on_budget_event(f"preflight: {w}")
    if on_budget_event:
        on_budget_event("preflight limits: " + ", ".join(plan.summary_lines()))
    dialogue.add("system", "budget: " + ", ".join(plan.summary_lines()))

    base_prompt = scenario.prompt.strip()
    include_protocol = interaction != InteractionMode.unattended

    max_rounds = 1
    if interaction == InteractionMode.proxy:
        max_rounds = (config.stakeholder.max_qa_rounds if config.stakeholder else 8) + 1
    elif interaction == InteractionMode.human:
        max_rounds = 12

    start = time.time()
    last_rc = 0
    round_tels: list[Telemetry] = []
    qa_rounds = 0
    budget_exceeded = False
    budget_reason: str | None = None
    killed = False
    budget_warnings: list[str] = list(plan.warnings)

    for rnd in range(max_rounds):
        wall_s = time.time() - start
        prior = _sum_telemetry(
            round_tels, wall_ms=int(wall_s * 1000), qa_rounds=qa_rounds, interaction=interaction
        )
        pre = check_budget(plan, wall_s=wall_s, telemetry=prior)
        if pre.exceeded:
            budget_exceeded = True
            budget_reason = "; ".join(pre.reasons)
            dialogue.add("system", f"budget exceeded before round {rnd}: {budget_reason}")
            if on_budget_event:
                on_budget_event(f"stop before round {rnd}: {budget_reason}")
            break
        if pre.should_warn:
            for w in pre.warn_reasons:
                if w not in budget_warnings:
                    budget_warnings.append(w)
                    if on_budget_event:
                        on_budget_event(f"WARN {w}")

        round_timeout = remaining_timeout_s(plan, wall_s)
        if round_timeout <= 0:
            budget_exceeded = True
            budget_reason = f"wall budget exhausted before round {rnd}"
            if on_budget_event:
                on_budget_event(budget_reason)
            break

        user_msg = build_sut_user_message(
            base_prompt,
            dialogue,
            include_protocol=include_protocol or interaction == InteractionMode.unattended,
        )
        if interaction == InteractionMode.unattended and rnd == 0:
            user_msg = (
                base_prompt
                + "\n\nUNATTENDED MODE: Do not ask questions. Implement and verify from the prompt alone.\n"
            )

        _write_prompt_file(worktree, user_msg)
        (run_dir / f"sut_prompt_round_{rnd}.txt").write_text(user_msg, encoding="utf-8")
        launch_cmd = template

        round_log = run_dir / f"agent_round_{rnd}.log"
        rc, raw, was_killed, kill_reason = run_launch_once(
            launch_cmd,
            worktree,
            round_log,
            timeout=round_timeout,
            append_mode=False,
            plan=plan,
            wall_s_at_start=wall_s,
            prior_telemetry=prior,
            harness=config.harness,
            on_event=on_budget_event,
            poll_interval=poll_interval,
        )
        last_rc = rc
        if was_killed:
            killed = True
            budget_exceeded = True
            budget_reason = kill_reason or "killed by budget guard"
            dialogue.add("system", f"budget kill round {rnd}: {budget_reason}")

        with log_path.open("a" if rnd else "w", encoding="utf-8") as out:
            out.write(f"\n##### SUT ROUND {rnd} rc={rc} killed={was_killed} #####\n")
            out.write(raw)
            out.write("\n")

        live = parse_agent_log(round_log, harness=config.harness)
        round_tels.append(live)

        if was_killed:
            break

        question = extract_question(raw)
        if not question:
            break
        if interaction == InteractionMode.unattended:
            dialogue.add("sut_question", question)
            dialogue.add("system", "unattended: question ignored")
            break

        dialogue.add("sut_question", question)
        qa_rounds += 1

        if interaction == InteractionMode.proxy:
            if not config.stakeholder:
                raise ValueError("interaction=proxy requires config.stakeholder")
            if not scenario.stakeholder_brief and not scenario.description:
                raise ValueError("proxy mode needs scenario.stakeholder_brief or description")
            answer, ptel = answer_as_proxy(
                question, scenario, config.stakeholder, work_dir=run_dir
            )
            dialogue.add("stakeholder", answer, ptel)
            (run_dir / f"proxy_answer_round_{rnd}.txt").write_text(answer, encoding="utf-8")
        else:  # human
            answer = answer_as_human(question, run_dir, wait_seconds=human_wait_seconds)
            dialogue.add("stakeholder", answer)

    wall_ms = int((time.time() - start) * 1000)
    (run_dir / "wall_ms.txt").write_text(str(wall_ms), encoding="utf-8")
    (run_dir / "agent_exit.txt").write_text(str(last_rc), encoding="utf-8")
    if budget_exceeded:
        (run_dir / "budget_exceeded.txt").write_text(
            (budget_reason or "budget exceeded") + "\n", encoding="utf-8"
        )
    (run_dir / "dialogue.json").write_text(dialogue.to_json(), encoding="utf-8")

    sut_tel = _sum_telemetry(
        round_tels, wall_ms=wall_ms, qa_rounds=qa_rounds, interaction=interaction
    )
    proxy_tel = dialogue.proxy_telemetry()
    sut_tel.proxy_tokens_in = proxy_tel.proxy_tokens_in
    sut_tel.proxy_tokens_out = proxy_tel.proxy_tokens_out
    sut_tel.proxy_estimated_usd = proxy_tel.proxy_estimated_usd
    sut_tel.qa_rounds = qa_rounds

    final = check_budget(plan, wall_s=wall_ms / 1000.0, telemetry=sut_tel)
    if final.exceeded and not budget_exceeded:
        budget_exceeded = True
        budget_reason = "; ".join(final.reasons)
    for w in final.warn_reasons:
        if w not in budget_warnings:
            budget_warnings.append(w)

    return ExecuteResult(
        returncode=last_rc,
        wall_ms=wall_ms,
        sut_telemetry=sut_tel,
        dialogue=dialogue,
        log_path=log_path,
        qa_rounds=qa_rounds,
        budget_exceeded=budget_exceeded,
        budget_reason=budget_reason,
        budget_warnings=budget_warnings,
        killed=killed,
    )
