"""Unit tests for hard budget guards."""

from __future__ import annotations

import time
from pathlib import Path

from hb.budget import (
    BudgetLevel,
    check_budget,
    plan_budget,
    remaining_timeout_s,
)
from hb.execute import run_launch_once
from hb.models import Budget, Telemetry


def test_plan_budget_caps_timeout_to_max_minutes():
    plan = plan_budget(Budget(max_minutes=1, max_usd=1.0, max_turns=10), cli_timeout_s=3600)
    assert plan.max_wall_s == 60.0
    assert plan.effective_timeout_s == 60
    assert plan.max_usd == 1.0
    assert plan.max_turns == 10
    assert any("capped" in w for w in plan.warnings)


def test_plan_budget_null_limits():
    plan = plan_budget(Budget(max_minutes=None, max_usd=None, max_turns=None), cli_timeout_s=120)
    assert plan.max_wall_s is None
    assert plan.max_usd is None
    assert plan.effective_timeout_s == 120


def test_check_budget_ok_warn_exceeded():
    plan = plan_budget(Budget(max_minutes=10, max_usd=1.0, max_turns=100), cli_timeout_s=600)

    ok = check_budget(plan, wall_s=10, telemetry=Telemetry(estimated_usd=0.1, turns=5))
    assert ok.level == BudgetLevel.ok

    warn = check_budget(plan, wall_s=500, telemetry=Telemetry(estimated_usd=0.85, turns=5))
    assert warn.level == BudgetLevel.warn
    assert warn.should_warn
    assert any("usd" in r for r in warn.warn_reasons)

    hard = check_budget(plan, wall_s=10, telemetry=Telemetry(estimated_usd=1.5, turns=5))
    assert hard.exceeded
    assert any("usd" in r for r in hard.reasons)


def test_check_budget_turns_and_wall():
    plan = plan_budget(Budget(max_minutes=1, max_turns=3), cli_timeout_s=120)
    hard_turns = check_budget(plan, wall_s=1, telemetry=Telemetry(turns=3))
    assert hard_turns.exceeded
    hard_wall = check_budget(plan, wall_s=60, telemetry=Telemetry(turns=1))
    assert hard_wall.exceeded


def test_remaining_timeout():
    plan = plan_budget(Budget(max_minutes=2), cli_timeout_s=100)
    # 100s wall left of 120s budget, but effective_timeout is min(100,120)=100
    assert remaining_timeout_s(plan, 0) == 100
    assert remaining_timeout_s(plan, 90) == 30  # 120-90=30, min with 100
    assert remaining_timeout_s(plan, 120) == 0


def test_run_launch_once_kills_on_timeout(tmp_path: Path):
    """Sleep longer than timeout → killed with reason."""
    log = tmp_path / "agent.log"
    rc, text, killed, reason = run_launch_once(
        "sleep 30",
        tmp_path,
        log,
        timeout=1,
        poll_interval=0.2,
    )
    assert killed
    assert reason is not None
    assert "timeout" in reason
    assert rc != 0


def test_run_launch_once_kills_on_usd_budget(tmp_path: Path):
    """
    Command that slowly writes pi-style usage past max_usd → kill.
    """
    log = tmp_path / "agent.log"
    # Script writes NDJSON with cost over budget then sleeps
    script = tmp_path / "writer.sh"
    script.write_text(
        """#!/bin/sh
echo '{"type":"turn_end","message":{"role":"assistant","usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0,"cost":{"total":0.5}}}}'
sleep 0.3
echo '{"type":"turn_end","message":{"role":"assistant","usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0,"cost":{"total":0.6}}}}'
sleep 20
""",
        encoding="utf-8",
    )
    script.chmod(0o755)
    plan = plan_budget(Budget(max_usd=0.8, max_minutes=5), cli_timeout_s=30)
    events: list[str] = []
    t0 = time.time()
    rc, text, killed, reason = run_launch_once(
        f"sh {script}",
        tmp_path,
        log,
        timeout=25,
        plan=plan,
        harness="pi",
        on_event=events.append,
        poll_interval=0.15,
    )
    elapsed = time.time() - t0
    assert killed, f"expected kill, events={events} reason={reason}"
    assert reason and "usd" in reason
    assert elapsed < 15  # should not wait full sleep 20
