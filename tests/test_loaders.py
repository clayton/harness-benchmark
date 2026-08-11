from pathlib import Path

from hb.loaders import load_all_configs, load_all_scenarios, validate_corpus
from hb.patchutil import stats_from_diff

ROOT = Path(__file__).resolve().parent.parent


def test_validate_corpus_ok():
    report = validate_corpus(ROOT)
    assert report.ok, report.issues
    assert report.scenarios_ok >= 1
    assert report.configs_ok >= 1


def test_example_scenario_loads():
    scenarios = {s.id: s for s in load_all_scenarios(ROOT)}
    s = scenarios["example-synthetic-bugfix"]
    assert s.type.value == "bugfix"
    assert "clamp" in s.prompt
    assert s.repo.gold_ref  # exists for judges, not for agents


def test_configs_load():
    configs = {c.id: c for c in load_all_configs(ROOT)}
    assert "baseline-manual" in configs
    assert "grok-baseline" in configs
    assert "grok-orchestrator-subagents" in configs
    # Real harness configs must pin a version (even a TODO placeholder)
    assert configs["grok-baseline"].harness_version
    assert configs["grok-baseline"].harness_label().startswith("grok@")
    assert configs["baseline-manual"].harness_version is None


def test_stats_from_diff():
    diff = """diff --git a/foo.py b/foo.py
--- a/foo.py
+++ b/foo.py
@@ -1,2 +1,3 @@
 line
+new
-old
"""
    stats = stats_from_diff(diff)
    assert stats.files_changed == 1
    assert stats.insertions == 1
    assert stats.deletions == 1
