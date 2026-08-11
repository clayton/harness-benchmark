"""Orchestrate run lifecycle: create → (agent works) → finish/judge."""

from __future__ import annotations

import hashlib
import json
import time
from datetime import datetime, timezone
from pathlib import Path

from hb.fingerprint import combo_fingerprint
from hb.judge import judge_worktree
from hb.models import Config, JudgeScore, RunRecord, RunStatus, Scenario, Telemetry
from hb.patchutil import git_diff, stats_from_diff
from hb.store import load_run, new_run_id, results_dir, save_run, write_patch
from hb.telemetry import merge_telemetry, parse_agent_log
from hb.workspace import prepare_workspace


def _prompt_hash(prompt: str) -> str:
    return hashlib.sha256(prompt.strip().encode("utf-8")).hexdigest()[:16]


def write_run_snapshot(
    run_id: str,
    scenario: Scenario,
    config: Config,
    root: Path,
    *,
    seed: int | None = None,
    temperature: float | None = None,
    parent_run_id: str | None = None,
    experiment_id: str | None = None,
) -> Path:
    """Freeze scenario + config + pins so this run can be re-created later."""
    run_dir = results_dir(root) / run_id
    run_dir.mkdir(parents=True, exist_ok=True)
    snapshot = {
        "schema": "hb.snapshot.v1",
        "run_id": run_id,
        "created_at": datetime.now(timezone.utc).isoformat(),
        "parent_run_id": parent_run_id,
        "experiment_id": experiment_id,
        "seed": seed if seed is not None else config.seed,
        "temperature": temperature if temperature is not None else config.temperature,
        "prompt_sha256_16": _prompt_hash(scenario.prompt),
        "scenario": scenario.model_dump(mode="json"),
        "config": config.model_dump(mode="json"),
        "repo": {
            "url": scenario.repo.url,
            "base_ref": scenario.repo.base_ref,
            "gold_ref": scenario.repo.gold_ref,
        },
        "notes": (
            "This snapshot freezes the *test definition* (repo refs, prompt, config, "
            "requested seed/temperature). LLM agent runs are not bit-identical even "
            "with a seed; use `hb rerun <run_id>` to re-create the same setup."
        ),
    }
    path = run_dir / "snapshot.json"
    path.write_text(json.dumps(snapshot, indent=2) + "\n", encoding="utf-8")
    # Also keep raw prompt for human inspection
    (run_dir / "prompt.txt").write_text(scenario.prompt.strip() + "\n", encoding="utf-8")
    return path


def load_snapshot(run_id: str, root: Path) -> dict:
    path = results_dir(root) / run_id / "snapshot.json"
    if not path.exists():
        raise FileNotFoundError(
            f"No snapshot for run {run_id} (older runs may predate snapshots). "
            f"Expected {path}"
        )
    return json.loads(path.read_text(encoding="utf-8"))


def _launch_docs(config: Config, workspace: Path, run_id: str) -> str:
    opts = config.harness_options or {}
    interactive = opts.get("launch_interactive")
    headless = opts.get("launch_headless")
    seed = config.seed
    temp = config.temperature
    lines = [
        f"# Launch — run `{run_id}`",
        f"",
        f"- Config: `{config.id}`",
        f"- Harness: `{config.harness_label()}`",
        f"- Model: `{config.model_label()}`",
        f"- Workflow: `{config.workflow}`",
        f"- Seed: `{seed if seed is not None else '—'}` (requested; harness may ignore)",
        f"- Temperature: `{temp if temp is not None else '—'}`",
        f"- Workspace: `{workspace}`",
        f"- Snapshot: `results/{run_id}/snapshot.json`",
        f"",
        f"## Steps",
        f"",
        f"```bash",
        f"cd {workspace}",
        f"```",
        f"",
        f"Feed the agent **only** `HB_PROMPT.txt` (intent, not the gold fix).",
        f"",
    ]
    if interactive:
        lines += [
            f"### Interactive (recommended)",
            f"",
            f"```bash",
            f"{interactive}",
            f"```",
            f"",
        ]
    if headless:
        lines += [
            f"### Headless / non-interactive (best-effort)",
            f"",
            f"```bash",
            f"{headless}",
            f"```",
            f"",
        ]
    if not interactive and not headless:
        lines += [
            f"Open harness `{config.harness}` in this directory and paste `HB_PROMPT.txt`.",
            f"",
        ]
    lines += [
        f"## When finished",
        f"",
        f"```bash",
        f"hb finish {run_id}",
        f"```",
        f"",
        f"Optional cost telemetry:",
        f"",
        f"```bash",
        f"hb finish {run_id} --tokens-in N --tokens-out N --wall-ms N --estimated-usd N",
        f"```",
        f"",
    ]
    return "\n".join(lines)


def create_run(
    scenario: Scenario,
    config: Config,
    root: Path,
    *,
    repeat: int = 0,
    run_setup: bool = True,
    force_prepare: bool = True,
    seed: int | None = None,
    temperature: float | None = None,
    parent_run_id: str | None = None,
    experiment_id: str | None = None,
) -> RunRecord:
    """Create a pending run with a fresh workspace at base_ref + frozen snapshot."""
    run_id = new_run_id()
    prepared = prepare_workspace(
        scenario,
        root,
        run_id=run_id,
        force=force_prepare,
        run_setup=run_setup,
    )

    effective_seed = seed if seed is not None else config.seed
    effective_temp = temperature if temperature is not None else config.temperature
    fp = combo_fingerprint(scenario, config, seed=effective_seed)

    # Freeze definition for re-runs
    snap_path = write_run_snapshot(
        run_id,
        scenario,
        config,
        root,
        seed=effective_seed,
        temperature=effective_temp,
        parent_run_id=parent_run_id,
        experiment_id=experiment_id,
    )

    run_dir = results_dir(root) / run_id
    (run_dir / "workspace_path.txt").write_text(str(prepared.path) + "\n", encoding="utf-8")
    launch_md = _launch_docs(config, prepared.path, run_id)
    (run_dir / "instructions.md").write_text(launch_md, encoding="utf-8")
    (prepared.path / "HB_LAUNCH.md").write_text(launch_md, encoding="utf-8")
    # Keep launch doc out of patches
    exclude = prepared.path / ".git" / "info" / "exclude"
    if exclude.exists():
        text = exclude.read_text(encoding="utf-8")
        if "HB_LAUNCH.md" not in text:
            exclude.write_text(text + "HB_LAUNCH.md\n", encoding="utf-8")

    run = RunRecord(
        id=run_id,
        scenario_id=scenario.id,
        config_id=config.id,
        repeat=repeat,
        status=RunStatus.pending,
        worktree=str(prepared.path),
        harness=config.harness,
        harness_version=config.harness_version,
        model=config.model,
        model_version=config.model_version,
        seed=effective_seed,
        temperature=effective_temp,
        parent_run_id=parent_run_id,
        experiment_id=experiment_id,
        snapshot_path=str(snap_path),
        metadata={
            "harness": config.harness_label(),
            "model": config.model_label(),
            "workflow": config.workflow,
            "comparison_mode": config.comparison_mode.value,
            "phase": "awaiting_agent",
            "prompt_sha256_16": _prompt_hash(scenario.prompt),
            "base_ref": scenario.repo.base_ref,
            "gold_ref": scenario.repo.gold_ref,
            "combo_fingerprint": fp,
            "interaction": config.interaction.value,
        },
    )
    save_run(run, root)
    return run


def rerun_from(
    source_run_id: str,
    root: Path,
    *,
    run_setup: bool = True,
    seed: int | None = None,
) -> RunRecord:
    """
    Re-create a run from a frozen snapshot (same scenario definition + config).

    Uses the *snapshotted* scenario/config JSON, not live corpus files, so later
    edits to YAML do not silently change what is re-run.
    """
    snap = load_snapshot(source_run_id, root)
    scenario = Scenario.model_validate(snap["scenario"])
    config = Config.model_validate(snap["config"])
    try:
        parent = load_run(source_run_id, root)
        next_repeat = (parent.repeat or 0) + 1
    except FileNotFoundError:
        next_repeat = 0
    effective_seed = seed if seed is not None else snap.get("seed")
    return create_run(
        scenario,
        config,
        root,
        repeat=next_repeat,
        run_setup=run_setup,
        force_prepare=True,
        seed=effective_seed,
        temperature=snap.get("temperature"),
        parent_run_id=source_run_id,
        experiment_id=snap.get("experiment_id"),
    )


def finish_run(
    run: RunRecord,
    root: Path,
    scenario: Scenario,
    *,
    worktree: Path | None = None,
    apply_gold_tests: bool = True,
    tokens_in: int | None = None,
    tokens_out: int | None = None,
    wall_ms: int | None = None,
    estimated_usd: float | None = None,
    turns: int | None = None,
    tool_calls: int | None = None,
    proxy_tokens_in: int | None = None,
    proxy_tokens_out: int | None = None,
    proxy_estimated_usd: float | None = None,
    qa_rounds: int | None = None,
    human_interventions: int | None = None,
    resolved_harness_version: str | None = None,
    resolved_model: str | None = None,
    notes: str | None = None,
    judge_timeout: int = 600,
    budget_exceeded: bool = False,
    budget_reason: str | None = None,
) -> RunRecord:
    """Capture patch from workspace, judge, and mark completed."""
    wt = Path(worktree or run.worktree or "")
    if not wt.exists():
        raise FileNotFoundError(f"Worktree not found: {wt}")

    started = time.time()
    patch_text = git_diff(wt, scenario.repo.base_ref)
    patch_path = write_patch(run.id, patch_text, root)
    stats = stats_from_diff(patch_text)

    judges: list[JudgeScore] = [
        JudgeScore(
            name="non_empty_patch",
            passed=bool(patch_text.strip()),
            score=1.0 if patch_text.strip() else 0.0,
            notes="Automatic: patch has content",
        )
    ]

    report = judge_worktree(
        scenario,
        wt,
        root=root,
        apply_gold_tests=apply_gold_tests,
        timeout=judge_timeout,
    )
    judges.extend(report.to_scores())

    # Persist judge log
    run_dir = results_dir(root) / run.id
    run_dir.mkdir(parents=True, exist_ok=True)
    (run_dir / "judge.json").write_text(
        json.dumps(
            {
                "passed": report.passed,
                "gold_tests_applied": report.gold_tests_applied,
                "scores": [s.model_dump() for s in report.scores],
                "commands": [
                    {
                        "command": c.command,
                        "returncode": c.returncode,
                        "stdout": c.stdout[-8000:],
                        "stderr": c.stderr[-8000:],
                    }
                    for c in report.command_results
                ],
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )

    elapsed_ms = int((time.time() - started) * 1000)
    tel = run.telemetry.model_copy()

    # Auto-parse harness agent log if present (pi --mode json, grok --output-format json)
    agent_log = results_dir(root) / run.id / "agent.log"
    if agent_log.exists():
        parsed = parse_agent_log(agent_log, harness=run.harness)
        tel = merge_telemetry(tel, parsed)
        # Also keep wall_ms from wall_ms.txt if agent wrote it
        wall_file = results_dir(root) / run.id / "wall_ms.txt"
        if wall_file.exists() and tel.wall_ms is None:
            try:
                tel.wall_ms = int(wall_file.read_text(encoding="utf-8").strip())
            except ValueError:
                pass

    if tokens_in is not None:
        tel.tokens_in = tokens_in
    if tokens_out is not None:
        tel.tokens_out = tokens_out
    if wall_ms is not None:
        tel.wall_ms = wall_ms
    elif tel.wall_ms is None:
        tel.wall_ms = elapsed_ms
    if estimated_usd is not None:
        tel.estimated_usd = estimated_usd
    if turns is not None:
        tel.turns = turns
    if tool_calls is not None:
        tel.tool_calls = tool_calls
    if proxy_tokens_in is not None:
        tel.proxy_tokens_in = proxy_tokens_in
    if proxy_tokens_out is not None:
        tel.proxy_tokens_out = proxy_tokens_out
    if proxy_estimated_usd is not None:
        tel.proxy_estimated_usd = proxy_estimated_usd
    if qa_rounds is not None:
        tel.qa_rounds = qa_rounds
    if human_interventions is not None:
        tel.human_interventions = human_interventions

    run.patch_path = str(patch_path)
    run.patch_stats = stats
    run.judges = judges
    run.telemetry = tel
    run.worktree = str(wt)
    run.status = RunStatus.completed if report.passed else RunStatus.failed
    # If patch empty, failed even if tests "pass" at base
    if not patch_text.strip():
        run.status = RunStatus.failed
    # Budget kill is a first-class outcome (still judged for partial quality)
    if budget_exceeded:
        run.status = RunStatus.budget_exceeded
        run.error = budget_reason or "budget exceeded"
    run.finished_at = datetime.now(timezone.utc)
    if resolved_harness_version:
        run.resolved_harness_version = resolved_harness_version
    if resolved_model:
        run.resolved_model = resolved_model
    if notes:
        run.notes = notes
    run.metadata = {
        **run.metadata,
        "phase": "finished",
        "judge_passed": report.passed,
        "gold_tests_applied": report.gold_tests_applied,
        "budget_exceeded": budget_exceeded,
        "budget_reason": budget_reason,
    }
    save_run(run, root)
    return run
