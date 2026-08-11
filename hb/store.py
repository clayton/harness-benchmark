"""Persist and load run results."""

from __future__ import annotations

import json
import uuid
from pathlib import Path

from hb.models import RunRecord

ROOT = Path(__file__).resolve().parent.parent


def results_dir(root: Path | None = None) -> Path:
    path = (root or ROOT) / "results"
    path.mkdir(parents=True, exist_ok=True)
    return path


def new_run_id() -> str:
    return uuid.uuid4().hex[:12]


def save_run(run: RunRecord, root: Path | None = None) -> Path:
    directory = results_dir(root) / run.id
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / "run.json"
    path.write_text(run.model_dump_json(indent=2), encoding="utf-8")
    return path


def load_run(run_id: str, root: Path | None = None) -> RunRecord:
    path = results_dir(root) / run_id / "run.json"
    if not path.exists():
        raise FileNotFoundError(f"Run not found: {run_id}")
    return RunRecord.model_validate_json(path.read_text(encoding="utf-8"))


def load_all_runs(root: Path | None = None) -> list[RunRecord]:
    base = results_dir(root)
    runs: list[RunRecord] = []
    for path in sorted(base.glob("*/run.json")):
        try:
            runs.append(RunRecord.model_validate_json(path.read_text(encoding="utf-8")))
        except Exception:
            continue
    return runs


def write_patch(run_id: str, patch_text: str, root: Path | None = None) -> Path:
    directory = results_dir(root) / run_id
    directory.mkdir(parents=True, exist_ok=True)
    path = directory / "patch.diff"
    path.write_text(patch_text, encoding="utf-8")
    return path
