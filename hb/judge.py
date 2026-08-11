"""Automatic judges: shell tests + optional gold FAIL_TO_PASS overlay."""

from __future__ import annotations

import re
import subprocess
from dataclasses import dataclass, field
from pathlib import Path

from hb.models import JudgeScore, Scenario
from hb.workspace import ensure_repo_cache


@dataclass
class CommandResult:
    command: str
    returncode: int
    stdout: str
    stderr: str

    @property
    def ok(self) -> bool:
        return self.returncode == 0


@dataclass
class JudgeReport:
    scores: list[JudgeScore] = field(default_factory=list)
    command_results: list[CommandResult] = field(default_factory=list)
    gold_tests_applied: list[str] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        bools = [s.passed for s in self.scores if s.passed is not None]
        return bool(bools) and all(bools)

    def to_scores(self) -> list[JudgeScore]:
        return list(self.scores)


def run_shell(command: str, cwd: Path, timeout: int | None = 600) -> CommandResult:
    proc = subprocess.run(
        command,
        cwd=str(cwd),
        shell=True,
        text=True,
        capture_output=True,
        timeout=timeout,
        check=False,
    )
    return CommandResult(
        command=command,
        returncode=proc.returncode,
        stdout=proc.stdout or "",
        stderr=proc.stderr or "",
    )


def list_gold_test_files(scenario: Scenario, root: Path | None = None) -> list[str]:
    """Files changed between base and gold that look like tests."""
    if not scenario.repo.gold_ref:
        return []
    cache = ensure_repo_cache(scenario, root)
    proc = subprocess.run(
        [
            "git",
            "-C",
            str(cache),
            "diff",
            "--name-only",
            scenario.repo.base_ref,
            scenario.repo.gold_ref,
        ],
        text=True,
        capture_output=True,
        check=False,
    )
    if proc.returncode != 0:
        return []
    paths = []
    for line in proc.stdout.splitlines():
        p = line.strip()
        if not p:
            continue
        lower = p.lower()
        if re.search(r"(^|/)(test|tests|spec|__tests__)(/|$)", lower) or re.search(
            r"(test|spec)\.[a-z0-9]+$", lower
        ):
            # Prefer test files over fixtures/changelogs
            if lower.endswith((".md", ".rst", ".txt")):
                continue
            paths.append(p)
    return paths


def apply_gold_file(scenario: Scenario, worktree: Path, rel_path: str, root: Path | None = None) -> str:
    """Write gold_ref version of rel_path into worktree; return previous contents or marker."""
    cache = ensure_repo_cache(scenario, root)
    target = worktree / rel_path
    previous: str | None = None
    if target.exists():
        previous = target.read_text(encoding="utf-8", errors="replace")
    proc = subprocess.run(
        ["git", "-C", str(cache), "show", f"{scenario.repo.gold_ref}:{rel_path}"],
        text=True,
        capture_output=True,
        check=False,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"Cannot read gold file {rel_path}: {proc.stderr}")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(proc.stdout, encoding="utf-8")
    return previous if previous is not None else ""


def restore_file(worktree: Path, rel_path: str, previous: str | None) -> None:
    target = worktree / rel_path
    if previous is None:
        if target.exists():
            target.unlink()
        return
    if previous == "" and not (worktree / rel_path).exists():
        return
    # We stored "" for new files that didn't exist — remove them
    # Actually apply_gold_file returns "" for non-existent; restore should delete
    # Distinguish: use a sentinel? Simpler: if previous == "" and file was created from gold, delete.
    # We return previous content or empty string for missing. If missing, delete.
    # Caller passes previous from apply which is "" for new files.
    # Problem: empty previous file content is rare. Use optional None for missing.
    pass


def overlay_gold_tests(
    scenario: Scenario, worktree: Path, root: Path | None = None
) -> tuple[list[str], dict[str, str | None]]:
    """
    Overlay gold test files onto worktree.
    Returns (paths applied, map path -> previous content or None if did not exist).
    """
    files = list_gold_test_files(scenario, root)
    backups: dict[str, str | None] = {}
    applied: list[str] = []
    for rel in files:
        target = worktree / rel
        backups[rel] = target.read_text(encoding="utf-8", errors="replace") if target.exists() else None
        apply_gold_file(scenario, worktree, rel, root)
        applied.append(rel)
    return applied, backups


def restore_overlays(worktree: Path, backups: dict[str, str | None]) -> None:
    for rel, previous in backups.items():
        target = worktree / rel
        if previous is None:
            if target.exists():
                target.unlink()
            # clean empty parents? skip
        else:
            target.write_text(previous, encoding="utf-8")


def judge_worktree(
    scenario: Scenario,
    worktree: Path,
    *,
    root: Path | None = None,
    apply_gold_tests: bool = True,
    timeout: int = 600,
) -> JudgeReport:
    """Run acceptance tests; optionally overlay gold tests for FAIL_TO_PASS signal."""
    report = JudgeReport()
    worktree = worktree.resolve()
    if not worktree.exists():
        report.scores.append(
            JudgeScore(name="worktree_exists", passed=False, score=0.0, notes=str(worktree))
        )
        return report

    backups: dict[str, str | None] = {}
    try:
        if apply_gold_tests and scenario.repo.gold_ref:
            applied, backups = overlay_gold_tests(scenario, worktree, root)
            report.gold_tests_applied = applied
            report.scores.append(
                JudgeScore(
                    name="gold_test_overlay",
                    passed=True if applied else None,
                    score=1.0 if applied else None,
                    notes=f"Applied {len(applied)} gold test file(s): {', '.join(applied) or 'none'}",
                    raw={"files": applied},
                )
            )

        # Patch non-empty is caller's job; here run test commands
        if not scenario.acceptance.test_commands:
            report.scores.append(
                JudgeScore(
                    name="acceptance_tests",
                    passed=None,
                    score=None,
                    notes="No test_commands configured",
                )
            )
            return report

        all_ok = True
        details: list[dict] = []
        for cmd in scenario.acceptance.test_commands:
            result = run_shell(cmd, worktree, timeout=timeout)
            report.command_results.append(result)
            details.append(
                {
                    "command": cmd,
                    "returncode": result.returncode,
                    "stdout_tail": result.stdout[-2000:],
                    "stderr_tail": result.stderr[-2000:],
                }
            )
            if not result.ok:
                all_ok = False

        report.scores.append(
            JudgeScore(
                name="acceptance_tests",
                passed=all_ok,
                score=1.0 if all_ok else 0.0,
                notes="All test_commands exit 0" if all_ok else "One or more test_commands failed",
                raw={"commands": details, "gold_tests_applied": report.gold_tests_applied},
            )
        )
    finally:
        if backups:
            restore_overlays(worktree, backups)

    return report
