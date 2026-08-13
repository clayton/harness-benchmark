"""CLI entrypoint: prepare, run, finish, experiment, judge, report."""

from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import typer
from rich.console import Console
from rich.table import Table

from hb import __version__
from hb.judge import judge_worktree
from hb.loaders import (
    ROOT,
    find_config,
    find_scenario,
    load_all_configs,
    load_all_scenarios,
    validate_corpus,
)
from hb.models import JudgeScore, RunRecord, RunStatus, Telemetry
from hb.patchutil import git_diff, stats_from_diff
from hb.report import render_report, write_report
from hb.runner import create_run, finish_run, load_snapshot, rerun_from
from hb.store import load_all_runs, load_run, new_run_id, results_dir, save_run, write_patch
from hb.workspace import instance_dir, prepare_workspace, reset_workspace

app = typer.Typer(
    name="hb",
    help="Harness Benchmark — compare coding-agent systems on fixed tasks.",
    no_args_is_help=True,
)
console = Console()


@app.callback()
def main() -> None:
    """Harness Benchmark CLI."""


@app.command()
def version() -> None:
    """Print package version."""
    console.print(__version__)


@app.command("validate")
def validate_cmd(
    root: Optional[Path] = typer.Option(None, help="Repo root (default: package parent)"),
) -> None:
    """Validate all scenario and config YAML files."""
    report = validate_corpus(root)
    console.print(
        f"[bold]Scenarios OK:[/bold] {report.scenarios_ok}  "
        f"[bold]Configs OK:[/bold] {report.configs_ok}"
    )
    if report.issues:
        for issue in report.issues:
            console.print(f"[red]✗[/red] {issue.path}: {issue.message}")
        raise typer.Exit(code=1)
    console.print("[green]Corpus valid.[/green]")


@app.command("list")
def list_cmd(
    what: str = typer.Argument("all", help="scenarios | configs | runs | all"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """List scenarios, configs, or runs."""
    root = root or ROOT
    if what in ("scenarios", "all"):
        table = Table(title="Scenarios")
        table.add_column("id")
        table.add_column("type")
        table.add_column("title")
        table.add_column("risk")
        for s in load_all_scenarios(root):
            table.add_row(s.id, s.type.value, s.title, s.contamination_risk.value)
        console.print(table)
    if what in ("configs", "all"):
        table = Table(title="Configs")
        table.add_column("id")
        table.add_column("harness")
        table.add_column("model")
        table.add_column("workflow")
        for c in load_all_configs(root):
            table.add_row(c.id, c.harness_label(), c.model_label(), c.workflow)
        console.print(table)
    if what in ("runs", "all"):
        table = Table(title="Runs")
        table.add_column("id")
        table.add_column("scenario")
        table.add_column("config")
        table.add_column("status")
        table.add_column("quality")
        for r in load_all_runs(root):
            q = r.primary_quality()
            table.add_row(
                r.id,
                r.scenario_id,
                r.config_id,
                r.status.value,
                f"{q:.2f}" if q is not None else "—",
            )
        console.print(table)


@app.command()
def prepare(
    scenario: str = typer.Argument(..., help="Scenario id"),
    force: bool = typer.Option(False, "--force", "-f", help="Wipe and recreate workspace"),
    no_setup: bool = typer.Option(False, "--no-setup", help="Skip setup_commands"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """Clone/checkout a scenario at base_ref and run setup (shared instance)."""
    root = root or ROOT
    sc = find_scenario(scenario, root)
    console.print(f"Preparing [bold]{sc.id}[/bold] @ {sc.repo.base_ref[:12]}…")
    prepared = prepare_workspace(
        sc, root, force=force, run_setup=not no_setup
    )
    console.print(f"[green]Ready[/green] {prepared.path}")
    console.print(f"  prompt: {prepared.prompt_path}")
    console.print(f"  guide: {prepared.path / 'HB_RUN.md'}")
    if prepared.setup_ran:
        console.print("  setup: ok")


@app.command()
def reset(
    scenario: str = typer.Argument(..., help="Scenario id"),
    no_setup: bool = typer.Option(False, "--no-setup"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """Hard-reset the shared scenario instance to a clean base_ref."""
    root = root or ROOT
    sc = find_scenario(scenario, root)
    prepared = reset_workspace(sc, root, run_setup=not no_setup)
    console.print(f"[green]Reset[/green] {prepared.path} → {sc.repo.base_ref[:12]}")


@app.command()
def show_prompt(
    scenario: str = typer.Argument(..., help="Scenario id or path"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """Print the exact agent prompt for a scenario."""
    sc = find_scenario(scenario, root or ROOT)
    console.print(f"# {sc.title}\n")
    console.print(f"Repo: {sc.repo.url}")
    console.print(f"Checkout: {sc.repo.base_ref}\n")
    console.print("--- PROMPT ---")
    console.print(sc.prompt)
    console.print("--- END ---")
    if sc.acceptance.test_commands:
        console.print("\nAcceptance tests (judges):")
        for cmd in sc.acceptance.test_commands:
            console.print(f"  $ {cmd}")


@app.command()
def run(
    scenario: str = typer.Option(..., "--scenario", "-s"),
    config: str = typer.Option(..., "--config", "-c"),
    repeat: int = typer.Option(0, "--repeat", "-r"),
    force: bool = typer.Option(
        False, "--force", help="Create even if this combo already completed"
    ),
    no_setup: bool = typer.Option(False, "--no-setup"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """
    Start a run: fresh workspace + pending result record.

    By default skips if this scenario×config already has a completed run.
    Use --force (or `hb rerun <id>`) to spend again.
    """
    from hb.fingerprint import should_skip_combo

    root = root or ROOT
    sc = find_scenario(scenario, root)
    cfg = find_config(config, root)
    skip, reason, prior = should_skip_combo(sc, cfg, root, force=force)
    if skip:
        console.print(f"[yellow]skip[/yellow] {reason}")
        if prior:
            console.print(f"  prior run: {prior[0].id}  status={prior[0].status.value}")
            console.print(f"  re-spend:  hb run -s {sc.id} -c {cfg.id} --force")
            console.print(f"  or:        hb rerun {prior[0].id}")
        raise typer.Exit(0)
    console.print(
        f"Starting run [bold]{sc.id}[/bold] × [bold]{cfg.id}[/bold] "
        f"({cfg.harness_label()} · {cfg.workflow} · {cfg.interaction.value})…"
    )
    record = create_run(
        sc, cfg, root, repeat=repeat, run_setup=not no_setup, force_prepare=True
    )
    console.print(f"[green]Run created[/green] {record.id}")
    console.print(f"  status:    {record.status.value}")
    console.print(f"  workspace: {record.worktree}")
    console.print(f"  fingerprint: {(record.metadata or {}).get('combo_fingerprint')}")
    console.print(f"  prompt:    {Path(record.worktree or '') / 'HB_PROMPT.txt'}")
    console.print(f"  next:      [bold]hb execute {record.id}[/bold]")


@app.command()
def rerun(
    run_id: str = typer.Argument(..., help="Existing run id with a snapshot.json"),
    seed: Optional[int] = typer.Option(
        None, "--seed", help="Override seed (default: seed from snapshot)"
    ),
    no_setup: bool = typer.Option(False, "--no-setup"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """
    Re-create a run from its frozen snapshot (same prompt, base_ref, config pins).

    This re-creates the *setup*, not a bit-identical model rollout. Use when you
    want another trial of the same controlled experiment.
    """
    root = root or ROOT
    try:
        snap = load_snapshot(run_id, root)
    except FileNotFoundError as e:
        console.print(f"[red]{e}[/red]")
        raise typer.Exit(1)
    record = rerun_from(run_id, root, run_setup=not no_setup, seed=seed)
    console.print(f"[green]Re-run created[/green] {record.id}")
    console.print(f"  parent:    {run_id}")
    console.print(f"  scenario:  {record.scenario_id}")
    console.print(f"  config:    {record.config_id}")
    console.print(f"  seed:      {record.seed if record.seed is not None else '—'}")
    console.print(f"  base_ref:  {snap.get('repo', {}).get('base_ref')}")
    console.print(f"  workspace: {record.worktree}")
    console.print(f"  snapshot:  results/{record.id}/snapshot.json")
    console.print(f"  next:      [bold]hb execute {record.id}[/bold]")


@app.command("show-snapshot")
def show_snapshot(
    run_id: str = typer.Argument(..., help="Run id"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """Print the frozen snapshot for a run (reproducibility record)."""
    root = root or ROOT
    try:
        snap = load_snapshot(run_id, root)
    except FileNotFoundError as e:
        console.print(f"[red]{e}[/red]")
        raise typer.Exit(1)
    console.print_json(data={
        "run_id": snap.get("run_id"),
        "parent_run_id": snap.get("parent_run_id"),
        "experiment_id": snap.get("experiment_id"),
        "seed": snap.get("seed"),
        "temperature": snap.get("temperature"),
        "prompt_sha256_16": snap.get("prompt_sha256_16"),
        "repo": snap.get("repo"),
        "scenario_id": (snap.get("scenario") or {}).get("id"),
        "config_id": (snap.get("config") or {}).get("id"),
        "harness": (snap.get("config") or {}).get("harness"),
        "harness_version": (snap.get("config") or {}).get("harness_version"),
        "model": (snap.get("config") or {}).get("model"),
        "workflow": (snap.get("config") or {}).get("workflow"),
    })


@app.command()
def execute(
    run_id: str = typer.Argument(..., help="Pending run id from `hb run` / experiment"),
    root: Optional[Path] = typer.Option(None),
    timeout: int = typer.Option(1800, help="Agent timeout seconds per SUT round"),
    skip_finish: bool = typer.Option(False, "--skip-finish", help="Only run agent; do not judge"),
    human_wait: int = typer.Option(
        3600, "--human-wait", help="Seconds to wait for human stakeholder answer"
    ),
) -> None:
    """
    Run the config's headless launch in the workspace, capture agent.log, finish+judge.

    Interaction modes (config.interaction):
      unattended — single pass, no Q&A
      proxy      — multi-round; answers STAKEHOLDER_QUESTION via stakeholder proxy
      human      — multi-round; pauses for your answer (file or stdin)

    Expects launch_headless JSON usage where possible (pi --mode json, grok --output-format json).
    """
    from hb.execute import execute_with_interaction

    root = root or ROOT
    record = load_run(run_id, root)
    # Prefer snapshotted scenario if present (frozen brief)
    sc = find_scenario(record.scenario_id, root)
    cfg = find_config(record.config_id, root)
    try:
        from hb.runner import load_snapshot
        from hb.models import Scenario as ScModel, Config as CfgModel

        snap = load_snapshot(run_id, root)
        sc = ScModel.model_validate(snap["scenario"])
        cfg = CfgModel.model_validate(snap["config"])
    except Exception:
        pass

    wt = Path(record.worktree or "")
    if not wt.exists():
        console.print(f"[red]Workspace missing: {wt}[/red]")
        raise typer.Exit(1)

    if not (cfg.harness_options or {}).get("launch_headless"):
        console.print(f"[red]Config {cfg.id} has no harness_options.launch_headless[/red]")
        raise typer.Exit(1)

    run_dir = results_dir(root) / run_id
    run_dir.mkdir(parents=True, exist_ok=True)
    b = cfg.budget
    console.print(
        f"Executing [bold]{cfg.harness_label()}[/bold] "
        f"interaction=[bold]{cfg.interaction.value}[/bold] in {wt}"
    )
    console.print(
        f"  budget: max_minutes={b.max_minutes} max_usd={b.max_usd} "
        f"max_turns={b.max_turns} max_tokens={b.max_tokens}  "
        f"(CLI --timeout {timeout}s; wall caps to max_minutes when set)"
    )

    record.status = RunStatus.running
    save_run(record, root)

    def _budget_event(msg: str) -> None:
        if msg.startswith("KILL") or msg.startswith("stop"):
            console.print(f"[red]budget[/red] {msg}")
        elif msg.startswith("WARN"):
            console.print(f"[yellow]budget[/yellow] {msg}")
        else:
            console.print(f"[dim]budget[/dim] {msg}")

    try:
        result = execute_with_interaction(
            scenario=sc,
            config=cfg,
            worktree=wt,
            run_dir=run_dir,
            timeout=timeout,
            human_wait_seconds=human_wait,
            on_budget_event=_budget_event,
        )
    except Exception as e:
        console.print(f"[red]execute failed:[/red] {e}")
        record.status = RunStatus.failed
        record.error = str(e)
        save_run(record, root)
        raise typer.Exit(1)

    tel = result.sut_telemetry
    console.print(
        f"  agent exit={result.returncode} wall_ms={result.wall_ms} "
        f"qa_rounds={result.qa_rounds} "
        f"sut_in={tel.tokens_in} sut_out={tel.tokens_out} sut_usd={tel.estimated_usd} "
        f"proxy_usd={tel.proxy_estimated_usd}"
    )
    if result.budget_exceeded:
        console.print(f"[red]budget exceeded:[/red] {result.budget_reason}")
    if result.qa_rounds:
        console.print(f"  dialogue → {run_dir / 'dialogue.json'}")

    if not skip_finish:
        notes = (
            f"hb execute interaction={cfg.interaction.value}; "
            f"agent_exit={result.returncode}; qa_rounds={result.qa_rounds}"
        )
        if result.budget_exceeded:
            notes += f"; budget_exceeded={result.budget_reason}"
        finished = finish_run(
            record,
            root,
            sc,
            worktree=wt,
            wall_ms=result.wall_ms,
            tokens_in=tel.tokens_in,
            tokens_out=tel.tokens_out,
            estimated_usd=tel.estimated_usd,
            turns=tel.turns,
            tool_calls=tel.tool_calls,
            proxy_tokens_in=tel.proxy_tokens_in,
            proxy_tokens_out=tel.proxy_tokens_out,
            proxy_estimated_usd=tel.proxy_estimated_usd,
            qa_rounds=tel.qa_rounds,
            human_interventions=tel.human_interventions,
            notes=notes,
            budget_exceeded=result.budget_exceeded,
            budget_reason=result.budget_reason,
        )
        color = "red" if finished.status == RunStatus.budget_exceeded else "green"
        console.print(
            f"[{color}]Finished[/{color}] {finished.id} status={finished.status.value} "
            f"quality={finished.primary_quality()}"
        )
        t = finished.telemetry
        total_usd = (t.estimated_usd or 0) + (t.proxy_estimated_usd or 0)
        console.print(
            f"  telemetry: sut_in={t.tokens_in} sut_out={t.tokens_out} sut_usd={t.estimated_usd} "
            f"proxy_usd={t.proxy_estimated_usd} total_usd≈{total_usd:.4f} wall_ms={t.wall_ms}"
        )


@app.command()
def finish(
    run_id: Optional[str] = typer.Argument(None, help="Run id from `hb run`"),
    worktree: Optional[Path] = typer.Option(
        None, "--worktree", "-w", help="Workspace path (if not using run id)"
    ),
    scenario: Optional[str] = typer.Option(
        None, "--scenario", "-s", help="Required with --worktree if not a tracked run"
    ),
    config: Optional[str] = typer.Option(
        None, "--config", "-c", help="Config id when finishing an ad-hoc worktree"
    ),
    no_gold_tests: bool = typer.Option(
        False, "--no-gold-tests", help="Do not overlay gold FAIL_TO_PASS tests"
    ),
    tokens_in: Optional[int] = typer.Option(None),
    tokens_out: Optional[int] = typer.Option(None),
    wall_ms: Optional[int] = typer.Option(None),
    estimated_usd: Optional[float] = typer.Option(None),
    turns: Optional[int] = typer.Option(None),
    tool_calls: Optional[int] = typer.Option(None),
    resolved_harness_version: Optional[str] = typer.Option(None, "--resolved-harness-version"),
    resolved_model: Optional[str] = typer.Option(None, "--resolved-model"),
    notes: Optional[str] = typer.Option(None),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """Capture patch from a run workspace, judge it, and save results."""
    root = root or ROOT

    if run_id:
        record = load_run(run_id, root)
        sc = find_scenario(record.scenario_id, root)
        wt = worktree or (Path(record.worktree) if record.worktree else None)
        if wt is None:
            console.print("[red]Run has no worktree; pass --worktree[/red]")
            raise typer.Exit(1)
    else:
        if not worktree or not scenario:
            console.print("[red]Provide RUN_ID or --worktree + --scenario[/red]")
            raise typer.Exit(1)
        sc = find_scenario(scenario, root)
        cfg = find_config(config or "baseline-manual", root)
        rid = new_run_id()
        record = RunRecord(
            id=rid,
            scenario_id=sc.id,
            config_id=cfg.id,
            status=RunStatus.pending,
            worktree=str(worktree),
            harness=cfg.harness,
            harness_version=cfg.harness_version,
            model=cfg.model,
            model_version=cfg.model_version,
        )
        wt = worktree

    console.print(f"Finishing [bold]{record.id}[/bold]…")
    finished = finish_run(
        record,
        root,
        sc,
        worktree=wt,
        apply_gold_tests=not no_gold_tests,
        tokens_in=tokens_in,
        tokens_out=tokens_out,
        wall_ms=wall_ms,
        estimated_usd=estimated_usd,
        turns=turns,
        tool_calls=tool_calls,
        resolved_harness_version=resolved_harness_version,
        resolved_model=resolved_model,
        notes=notes,
    )
    stats = finished.patch_stats
    console.print(f"[green]Saved[/green] {finished.id}  status={finished.status.value}")
    if stats:
        console.print(
            f"  patch: {stats.files_changed} files +{stats.insertions}/-{stats.deletions}"
        )
    for j in finished.judges:
        mark = "✓" if j.passed else ("·" if j.passed is None else "✗")
        console.print(f"  judge {mark} {j.name}: {j.notes or ''}")
    console.print("  report: hb report")


@app.command()
def judge(
    scenario: str = typer.Option(..., "--scenario", "-s"),
    worktree: Path = typer.Option(..., "--worktree", "-w"),
    no_gold_tests: bool = typer.Option(False, "--no-gold-tests"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """Run acceptance tests on a workspace without recording a full run."""
    root = root or ROOT
    sc = find_scenario(scenario, root)
    if not worktree.exists():
        console.print(f"[red]Missing worktree: {worktree}[/red]")
        raise typer.Exit(1)
    report = judge_worktree(
        sc, worktree, root=root, apply_gold_tests=not no_gold_tests
    )
    for j in report.scores:
        mark = "✓" if j.passed else ("·" if j.passed is None else "✗")
        console.print(f"{mark} {j.name}: {j.notes or ''}")
    for c in report.command_results:
        color = "green" if c.ok else "red"
        console.print(f"[{color}]exit {c.returncode}[/{color}] $ {c.command}")
        if not c.ok:
            tail = (c.stderr or c.stdout)[-1500:]
            if tail.strip():
                console.print(tail)
    raise typer.Exit(0 if report.passed else 1)


@app.command()
def experiment(
    scenarios: Optional[str] = typer.Option(
        None,
        "--scenarios",
        "-s",
        help="Comma-separated scenario ids",
    ),
    configs: Optional[str] = typer.Option(
        None,
        "--configs",
        "-c",
        help="Comma-separated config ids",
    ),
    repeats: int = typer.Option(
        1,
        "--repeats",
        help="Desired number of done trials per scenario×config (creates only what's missing)",
    ),
    from_file: Optional[Path] = typer.Option(
        None,
        "--from",
        help="Experiment YAML (scenario_ids, config_ids, repeats)",
    ),
    force: bool = typer.Option(
        False,
        "--force",
        help="Create runs even if this exact combo already has completed results",
    ),
    no_setup: bool = typer.Option(False, "--no-setup"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """
    Create a matrix of pending runs (scenario × config × repeat).

    By default skips combos that already have enough completed runs (same
    scenario, config, interaction, skills pin, seed). Use --force to re-spend,
    or --repeats N to collect statistical trials.
    """
    import yaml

    from hb.fingerprint import combo_fingerprint, should_skip_combo

    root = root or ROOT
    experiment_id: str | None = None
    experiment_seed: int | None = None
    if from_file:
        data = yaml.safe_load(from_file.read_text(encoding="utf-8")) or {}
        scenario_ids = list(data.get("scenario_ids") or [])
        config_ids = list(data.get("config_ids") or [])
        repeats = int(data.get("repeats") or repeats)
        experiment_id = data.get("id")
        if data.get("seed") is not None:
            experiment_seed = int(data["seed"])
        if data.get("hypothesis"):
            console.print(f"[bold]Hypothesis:[/bold] {data['hypothesis'].strip()}\n")
    else:
        if not scenarios or not configs:
            console.print("[red]Provide --scenarios and --configs, or --from experiment.yaml[/red]")
            raise typer.Exit(1)
        scenario_ids = [x.strip() for x in scenarios.split(",") if x.strip()]
        config_ids = [x.strip() for x in configs.split(",") if x.strip()]

    created: list[RunRecord] = []
    skipped: list[str] = []

    for sid in scenario_ids:
        sc = find_scenario(sid, root)
        for cid in config_ids:
            cfg = find_config(cid, root)
            # How many new runs do we still need for this combo?
            skip, reason, prior = should_skip_combo(
                sc,
                cfg,
                root,
                seed=experiment_seed,
                force=force,
                allow_extra_repeats=max(0, repeats - 1),
            )
            if skip:
                console.print(f"[yellow]skip[/yellow] {sc.id} × {cfg.id}")
                console.print(f"  {reason}")
                skipped.append(f"{sc.id}×{cfg.id}")
                continue
            # Create only missing trials
            already = len(prior)
            need = repeats - already if not force else repeats
            if force:
                need = repeats
            for rep in range(already, already + need):
                fp = combo_fingerprint(sc, cfg, seed=experiment_seed)
                console.print(
                    f"→ {sc.id} × {cfg.id} ({cfg.harness_label()} · {cfg.model_label()}) "
                    f"repeat={rep} fp={fp}"
                )
                record = create_run(
                    sc,
                    cfg,
                    root,
                    repeat=rep,
                    run_setup=not no_setup,
                    force_prepare=True,
                    seed=experiment_seed,
                    experiment_id=experiment_id,
                )
                created.append(record)
                console.print(f"  run {record.id}")
                console.print(f"  workspace {record.worktree}")
                console.print(f"  snapshot  results/{record.id}/snapshot.json")
                console.print(f"  launch    {Path(record.worktree or '') / 'HB_LAUNCH.md'}")

    console.print(
        f"\n[green]Created {len(created)} run(s)[/green]"
        + (f", skipped {len(skipped)} combo(s)" if skipped else "")
    )
    if created:
        console.print("Next: [bold]hb execute <run_id>[/bold]")
    table = Table(title="Pending runs")
    table.add_column("run_id")
    table.add_column("scenario")
    table.add_column("config")
    table.add_column("harness")
    table.add_column("model")
    table.add_column("repeat")
    for r in created:
        cfg = find_config(r.config_id, root)
        table.add_row(
            r.id, r.scenario_id, r.config_id, cfg.harness_label(), cfg.model_label(), str(r.repeat)
        )
    if created:
        console.print(table)


@app.command()
def ingest(
    scenario: str = typer.Option(..., "--scenario", "-s", help="Scenario id or YAML path"),
    config: str = typer.Option(..., "--config", "-c", help="Config id or YAML path"),
    worktree: Optional[Path] = typer.Option(
        None, "--worktree", "-w", help="Git worktree with agent changes"
    ),
    patch_file: Optional[Path] = typer.Option(
        None, "--patch", "-p", help="Unified diff file (alternative to worktree)"
    ),
    base_ref: Optional[str] = typer.Option(
        None, help="Override base ref for git diff (default: scenario.repo.base_ref)"
    ),
    repeat: int = typer.Option(0, help="Repeat index for variance"),
    tokens_in: Optional[int] = typer.Option(None),
    tokens_out: Optional[int] = typer.Option(None),
    wall_ms: Optional[int] = typer.Option(None),
    estimated_usd: Optional[float] = typer.Option(None),
    turns: Optional[int] = typer.Option(None),
    tool_calls: Optional[int] = typer.Option(None),
    resolved_harness_version: Optional[str] = typer.Option(None, "--resolved-harness-version"),
    resolved_model: Optional[str] = typer.Option(None, "--resolved-model"),
    notes: Optional[str] = typer.Option(None),
    judge: bool = typer.Option(True, "--judge/--no-judge", help="Run automatic judges"),
    no_gold_tests: bool = typer.Option(False, "--no-gold-tests"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """Record a completed worktree/patch into results/ (legacy one-shot path)."""
    root = root or ROOT
    sc = find_scenario(scenario, root)
    cfg = find_config(config, root)

    if not worktree and not patch_file:
        console.print("[red]Provide --worktree or --patch[/red]")
        raise typer.Exit(code=1)

    if worktree:
        if not worktree.exists():
            console.print(f"[red]Worktree not found: {worktree}[/red]")
            raise typer.Exit(code=1)
        ref = base_ref or sc.repo.base_ref
        patch_text = git_diff(worktree, ref)
    else:
        assert patch_file is not None
        patch_text = patch_file.read_text(encoding="utf-8")

    run_id = new_run_id()
    patch_path = write_patch(run_id, patch_text, root)
    stats = stats_from_diff(patch_text)

    judges = [
        JudgeScore(
            name="non_empty_patch",
            passed=bool(patch_text.strip()),
            score=1.0 if patch_text.strip() else 0.0,
            notes="Automatic: patch has content",
        )
    ]
    status = RunStatus.ingested
    if judge and worktree:
        report = judge_worktree(
            sc, worktree, root=root, apply_gold_tests=not no_gold_tests
        )
        judges.extend(report.to_scores())
        status = RunStatus.completed if report.passed and patch_text.strip() else RunStatus.failed

    run = RunRecord(
        id=run_id,
        scenario_id=sc.id,
        config_id=cfg.id,
        repeat=repeat,
        status=status,
        finished_at=datetime.now(timezone.utc),
        worktree=str(worktree) if worktree else None,
        patch_path=str(patch_path),
        patch_stats=stats,
        telemetry=Telemetry(
            wall_ms=wall_ms,
            tokens_in=tokens_in,
            tokens_out=tokens_out,
            estimated_usd=estimated_usd,
            turns=turns,
            tool_calls=tool_calls,
        ),
        judges=judges,
        notes=notes,
        harness=cfg.harness,
        harness_version=cfg.harness_version,
        model=cfg.model,
        model_version=cfg.model_version,
        resolved_harness_version=resolved_harness_version,
        resolved_model=resolved_model,
        metadata={
            "harness": cfg.harness_label(),
            "model": cfg.model_label(),
            "workflow": cfg.workflow,
            "comparison_mode": cfg.comparison_mode.value,
        },
    )
    out = save_run(run, root)
    console.print(f"[green]Ingested run[/green] {run_id}  status={status.value}")
    console.print(f"  scenario={sc.id} config={cfg.id}")
    console.print(f"  system={cfg.harness_label()} · model={cfg.model_label()}")
    console.print(
        f"  patch: {stats.files_changed} files, +{stats.insertions}/-{stats.deletions}"
    )
    console.print(f"  wrote {out}")


@app.command()
def report(
    out: Optional[Path] = typer.Option(
        None,
        "--out",
        "-o",
        help="Output HTML path (default: reports/latest.html or reports/exp-<id>.html)",
    ),
    experiment: Optional[str] = typer.Option(
        None,
        "--experiment",
        "-e",
        help="Filter runs by experiment_id and title the report for that matrix",
    ),
    experiment_file: Optional[Path] = typer.Option(
        None,
        "--from",
        help="Experiment YAML — filter by id and include hypothesis/notes in the report",
    ),
    results_href: str = typer.Option(
        "../results",
        "--results-href",
        help="Relative href from the HTML file to the results/ directory (for artifact links)",
    ),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """
    Build a static HTML report from results/.

    Artifact columns link to results/<run_id>/{patch.diff,agent.log,dialogue.json,
    snapshot.json,judge.json,run.json}. Open the HTML from the repo tree so
    relative paths resolve (default reports/ → ../results/).
    """
    import yaml

    root = root or ROOT
    hypothesis: str | None = None
    notes: str | None = None
    experiment_id = experiment
    experiment_name: str | None = None

    if experiment_file:
        data = yaml.safe_load(experiment_file.read_text(encoding="utf-8")) or {}
        experiment_id = experiment_id or data.get("id")
        hypothesis = data.get("hypothesis")
        notes = data.get("notes")
        experiment_name = data.get("name")

    runs = load_all_runs(root)
    if experiment_id:
        runs = [r for r in runs if r.experiment_id == experiment_id]
        if not runs:
            console.print(
                f"[yellow]No runs with experiment_id={experiment_id}[/yellow] "
                f"(older runs may predate experiment tagging)"
            )

    scenarios = {s.id: s for s in load_all_scenarios(root)}
    configs = {c.id: c for c in load_all_configs(root)}

    if experiment_id:
        title = experiment_name or f"Experiment · {experiment_id}"
        default_out = Path(f"reports/exp-{experiment_id}.html")
    else:
        title = "Harness Benchmark Report"
        default_out = Path("reports/latest.html")

    out_path = out or default_out
    doc = render_report(
        runs,
        scenarios=scenarios,
        configs=configs,
        title=title,
        experiment_id=experiment_id,
        hypothesis=hypothesis,
        notes=notes,
        results_href=results_href,
    )
    path = write_report(doc, out_path if out_path.is_absolute() else root / out_path)
    console.print(f"[green]Wrote[/green] {path}  ({len(runs)} run(s))")
    if runs:
        console.print(f"  artifacts → {results_href}/<run_id>/…")


@app.command("rider")
def rider_cmd(
    action: str = typer.Argument("whoami", help="init | whoami"),
) -> None:
    """Create or show your Agent Rodeo rider token."""
    from hb.rodeo import init_rider, load_rider, rider_path, rodeo_url

    if action == "init":
        data = init_rider()
        console.print(f"[green]rider[/green] {data.get('display_name')}  slug={data.get('slug')}")
        console.print(f"  token stored at {rider_path()}")
        console.print(f"  rodeo {rodeo_url()}")
        return
    data = load_rider()
    if not data:
        console.print("No rider yet. Run [bold]hb rider init[/bold]")
        raise typer.Exit(1)
    console.print(f"{data.get('display_name')}  slug={data.get('slug')}")
    console.print(f"  {rider_path()}")


@app.command()
def publish(
    run_id: str = typer.Argument(..., help="Finished run id"),
    root: Optional[Path] = typer.Option(None),
) -> None:
    """Push a finished run to Agent Rodeo (agentrodeo.dev)."""
    from hb.loaders import ROOT
    from hb.rodeo import publish_run, rodeo_url

    root = root or ROOT
    try:
        result = publish_run(run_id, root)
    except Exception as e:
        console.print(f"[red]publish failed:[/red] {e}")
        raise typer.Exit(1)
    console.print(f"[green]Published[/green] {result.get('id')}  {result.get('url')}")
    if result.get("unofficial"):
        console.print("[yellow]unofficial[/yellow] — on your profile only, not the main board")
    console.print(f"  rodeo {rodeo_url()}")


if __name__ == "__main__":
    app()
