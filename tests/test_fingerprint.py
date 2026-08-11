from hb.fingerprint import combo_fingerprint, should_skip_combo
from hb.loaders import find_config, find_scenario, ROOT
from hb.models import RunRecord, RunStatus
from hb.store import save_run


def test_fingerprint_stable():
    sc = find_scenario("js-commander-negative-exp-E", ROOT)
    cfg = find_config("pi-grok45-baseline", ROOT)
    a = combo_fingerprint(sc, cfg)
    b = combo_fingerprint(sc, cfg)
    assert a == b
    assert len(a) == 16


def test_fingerprint_changes_with_interaction(tmp_path):
    sc = find_scenario("python-fastapi-stream-router-incomplete", ROOT)
    base = find_config("pi-grok45-superpowers", ROOT)
    proxy = find_config("pi-grok45-superpowers-proxy", ROOT)
    assert combo_fingerprint(sc, base) != combo_fingerprint(sc, proxy)
