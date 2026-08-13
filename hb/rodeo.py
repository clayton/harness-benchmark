"""Publish finished runs to Agent Rodeo."""

from __future__ import annotations

import json
import os
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

from hb.store import load_run, results_dir


def rodeo_url() -> str:
    return os.environ.get("HB_RODEO_URL", "https://agentrodeo.dev").rstrip("/")


def rider_path() -> Path:
    override = os.environ.get("HB_RIDER_FILE")
    if override:
        return Path(override)
    return Path.home() / ".config" / "hb" / "rider.json"


def load_rider() -> dict | None:
    path = rider_path()
    if not path.exists():
        return None
    return json.loads(path.read_text(encoding="utf-8"))


def save_rider(data: dict) -> Path:
    path = rider_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
    return path


def _request(method: str, path: str, *, token: str | None = None, body: dict | None = None) -> dict:
    data = None if body is None else json.dumps(body).encode("utf-8")
    headers = {"Accept": "application/json", "Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = Request(f"{rodeo_url()}{path}", data=data, headers=headers, method=method)
    try:
        with urlopen(req, timeout=30) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except HTTPError as e:
        detail = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"rodeo {e.code}: {detail}") from e
    except URLError as e:
        raise RuntimeError(f"cannot reach {rodeo_url()}: {e.reason}") from e


def init_rider() -> dict:
    existing = load_rider()
    if existing and existing.get("token"):
        return existing
    created = _request("POST", "/api/v1/riders")
    save_rider(created)
    return created


def publish_run(run_id: str, root: Path) -> dict:
    rider = init_rider()
    record = load_run(run_id, root)
    snap_path = results_dir(root) / run_id / "snapshot.json"
    snapshot = {}
    if snap_path.exists():
        snapshot = json.loads(snap_path.read_text(encoding="utf-8"))
    payload = {"run": json.loads(record.model_dump_json()), "snapshot": snapshot}
    return _request("POST", "/api/v1/runs", token=rider["token"], body=payload)
