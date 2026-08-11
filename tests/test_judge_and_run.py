"""Integration-ish tests for prepare/judge using the smoked commander scenario."""

from pathlib import Path

import pytest

from hb.judge import judge_worktree, list_gold_test_files
from hb.loaders import find_scenario, ROOT
from hb.workspace import prepare_workspace


@pytest.fixture(scope="module")
def commander_ws(tmp_path_factory):
    """Prepare commander scenario once (uses network if cache empty)."""
    sc = find_scenario("js-commander-negative-exp-E", ROOT)
    dest = tmp_path_factory.mktemp("commander")
    # Reuse repo cache under real workspaces to speed up if present
    prepared = prepare_workspace(
        sc,
        ROOT,
        force=True,
        run_setup=True,
        dest=dest,
    )
    return sc, prepared.path


def test_gold_test_files_detected():
    sc = find_scenario("js-commander-negative-exp-E", ROOT)
    files = list_gold_test_files(sc, ROOT)
    assert any("negatives" in f for f in files)


def test_judge_fails_on_base_with_gold_tests(commander_ws):
    sc, path = commander_ws
    report = judge_worktree(sc, path, root=ROOT, apply_gold_tests=True)
    # Base code + gold tests should fail uppercase E cases
    assert report.gold_tests_applied
    assert report.passed is False


def test_judge_passes_after_gold_patch(commander_ws):
    sc, path = commander_ws
    # Apply gold production fix only (command.js), keep judging with gold tests
    import subprocess

    cache = path  # already a full clone
    # Checkout gold lib file into worktree
    gold = sc.repo.gold_ref
    content = subprocess.check_output(
        ["git", "show", f"{gold}:lib/command.js"], cwd=path, text=True
    )
    (path / "lib" / "command.js").write_text(content, encoding="utf-8")
    report = judge_worktree(sc, path, root=ROOT, apply_gold_tests=True)
    assert report.passed is True
