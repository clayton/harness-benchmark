"""Report HTML: winners, artifact links, experiment metadata."""

from __future__ import annotations

from datetime import datetime, timezone

from hb.models import JudgeScore, RunRecord, RunStatus, Telemetry
from hb.report import render_report


def _run(
    rid: str,
    scenario: str,
    config: str,
    *,
    q: float = 1.0,
    usd: float = 0.1,
    wall_ms: int = 1000,
    experiment_id: str | None = None,
    status: RunStatus = RunStatus.completed,
) -> RunRecord:
    return RunRecord(
        id=rid,
        scenario_id=scenario,
        config_id=config,
        status=status,
        created_at=datetime.now(timezone.utc),
        telemetry=Telemetry(estimated_usd=usd, wall_ms=wall_ms, tokens_in=100, tokens_out=20),
        judges=[JudgeScore(name="acceptance", score=q, passed=q >= 1.0)],
        experiment_id=experiment_id,
        harness="pi",
        model="grok-4.5",
    )


def test_report_contains_artifact_links():
    runs = [
        _run("aaaaaaaaaaaa", "sc-a", "cfg-a"),
        _run("bbbbbbbbbbbb", "sc-a", "cfg-b", usd=0.5, wall_ms=5000),
    ]
    html = render_report(runs, results_href="../results")
    assert 'href="../results/aaaaaaaaaaaa/patch.diff"' in html
    assert 'href="../results/aaaaaaaaaaaa/agent.log"' in html
    assert 'href="../results/aaaaaaaaaaaa/dialogue.json"' in html
    assert 'href="../results/aaaaaaaaaaaa/snapshot.json"' in html
    assert 'href="../results/aaaaaaaaaaaa/judge.json"' in html
    assert 'href="../results/aaaaaaaaaaaa/run.json"' in html
    assert "artifacts" in html


def test_experiment_report_meta():
    runs = [_run("cccccccccccc", "sc-a", "cfg-a", experiment_id="exp-1")]
    html = render_report(
        runs,
        title="My experiment",
        experiment_id="exp-1",
        hypothesis="A beats B",
        notes="n=1",
    )
    assert "My experiment" in html
    assert "exp-1" in html
    assert "A beats B" in html


def test_budget_exceeded_status_label():
    runs = [
        _run(
            "dddddddddddd",
            "sc-a",
            "cfg-a",
            status=RunStatus.budget_exceeded,
            q=0.0,
        )
    ]
    html = render_report(runs)
    assert "budget" in html
