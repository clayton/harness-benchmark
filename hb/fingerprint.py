"""Experiment combo fingerprints — skip re-spending on identical setups by default."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path

from hb.models import Config, RunRecord, RunStatus, Scenario
from hb.store import load_all_runs


# Statuses that count as "already spent money / got a result"
_DONE = {
    RunStatus.completed,
    RunStatus.failed,
    RunStatus.timeout,
    RunStatus.ingested,
}


def combo_fingerprint(
    scenario: Scenario,
    config: Config,
    *,
    seed: int | None = None,
) -> str:
    """
    Stable id for (scenario definition + config treatment + seed).

    Includes prompt hash and superpowers pin so prompt/skill package changes
    invalidate the fingerprint. Does not include run_id or timestamps.
    """
    effective_seed = seed if seed is not None else config.seed
    payload = {
        "scenario_id": scenario.id,
        "prompt_sha16": hashlib.sha256(scenario.prompt.strip().encode()).hexdigest()[:16],
        "base_ref": scenario.repo.base_ref,
        "gold_ref": scenario.repo.gold_ref,
        "config_id": config.id,
        "harness": config.harness,
        "harness_version": config.harness_version,
        "model": config.model,
        "model_version": config.model_version,
        "workflow": config.workflow,
        "interaction": config.interaction.value,
        "skills": sorted(config.skills),
        "seed": effective_seed,
        "superpowers_sha": (config.harness_options or {}).get("superpowers_sha"),
        "stakeholder_model": (
            config.stakeholder.model if config.stakeholder else None
        ),
    }
    raw = json.dumps(payload, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(raw.encode()).hexdigest()[:16]


def find_prior_runs(
    fingerprint: str,
    root: Path,
    *,
    only_done: bool = True,
) -> list[RunRecord]:
    """Return existing runs with this combo fingerprint (newest first)."""
    matches: list[RunRecord] = []
    for run in load_all_runs(root):
        fp = (run.metadata or {}).get("combo_fingerprint")
        if fp != fingerprint:
            continue
        if only_done and run.status not in _DONE:
            # pending/running don't block (orphaned pending shouldn't forever block)
            # Actually user wants avoid re-spend — pending that never finished could re-run
            # Only skip if completed successfully or failed after agent work?
            # "already run this experiment" — include failed/completed/timeout/ingested
            continue
        matches.append(run)
    matches.sort(key=lambda r: r.created_at, reverse=True)
    return matches


def should_skip_combo(
    scenario: Scenario,
    config: Config,
    root: Path,
    *,
    seed: int | None = None,
    force: bool = False,
    allow_extra_repeats: int = 0,
) -> tuple[bool, str, list[RunRecord]]:
    """
    Decide whether to skip creating another run for this combo.

    - force=True → never skip
    - allow_extra_repeats: number of *additional* done runs allowed beyond 1
      (i.e. want n total → allow_extra_repeats = n-1)
    """
    if force:
        return False, "", []
    fp = combo_fingerprint(scenario, config, seed=seed)
    prior = find_prior_runs(fp, root, only_done=True)
    want = 1 + max(0, allow_extra_repeats)
    if len(prior) >= want:
        ids = ", ".join(r.id for r in prior[:3])
        more = f" (+{len(prior) - 3} more)" if len(prior) > 3 else ""
        return (
            True,
            f"combo {fp} already has {len(prior)} done run(s) [{ids}{more}]; "
            f"skip (use --force to re-run, or --repeats N for more trials)",
            prior,
        )
    return False, "", prior
