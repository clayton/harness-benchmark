"""Prepare and reset scenario workspaces for runs."""

from __future__ import annotations

import json
import re
import shutil
import subprocess
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

from hb.models import Scenario

ROOT = Path(__file__).resolve().parent.parent


def workspaces_root(root: Path | None = None) -> Path:
    path = (root or ROOT) / "workspaces"
    path.mkdir(parents=True, exist_ok=True)
    return path


def repo_slug(url: str) -> str:
    """Turn a git URL into a filesystem-safe cache name."""
    cleaned = url.rstrip("/")
    if cleaned.endswith(".git"):
        cleaned = cleaned[:-4]
    name = cleaned.split("/")[-1]
    owner = cleaned.split("/")[-2] if "/" in cleaned else "repo"
    # strip scheme leftovers
    owner = owner.split(":")[-1]
    return re.sub(r"[^a-zA-Z0-9._-]+", "-", f"{owner}__{name}")


def repo_cache_dir(scenario: Scenario, root: Path | None = None) -> Path:
    return workspaces_root(root) / "repos" / repo_slug(scenario.repo.url)


def instance_dir(scenario_id: str, root: Path | None = None, run_id: str | None = None) -> Path:
    base = workspaces_root(root)
    if run_id:
        return base / "instances" / f"{scenario_id}__{run_id}"
    return base / "instances" / scenario_id


def _run(cmd: list[str] | str, cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess:
    if isinstance(cmd, str):
        return subprocess.run(
            cmd,
            cwd=str(cwd) if cwd else None,
            shell=True,
            text=True,
            capture_output=True,
            check=check,
        )
    return subprocess.run(
        cmd,
        cwd=str(cwd) if cwd else None,
        text=True,
        capture_output=True,
        check=check,
    )


def _ensure_commit(repo: Path, sha: str) -> None:
    """Make sure `sha` exists in repo, fetching from origin if needed."""
    got = _run(["git", "cat-file", "-t", sha], cwd=repo, check=False)
    if got.returncode == 0:
        return
    # Try fetching the commit by SHA (works on GitHub)
    _run(["git", "fetch", "--depth=1", "origin", sha], cwd=repo, check=False)
    got = _run(["git", "cat-file", "-t", sha], cwd=repo, check=False)
    if got.returncode == 0:
        return
    _run(["git", "fetch", "--deepen=500", "origin"], cwd=repo, check=False)
    got = _run(["git", "cat-file", "-t", sha], cwd=repo, check=False)
    if got.returncode == 0:
        return
    _run(["git", "fetch", "origin"], cwd=repo, check=False)
    got = _run(["git", "cat-file", "-t", sha], cwd=repo, check=False)
    if got.returncode != 0:
        raise RuntimeError(f"Cannot find commit {sha} in {repo}")


def ensure_repo_cache(scenario: Scenario, root: Path | None = None) -> Path:
    """Clone (or update) a shared git cache for the scenario's repo.

    Uses a full clone (no partial blob filter) so detached checkouts of
    arbitrary SHAs are reliable.
    """
    cache = repo_cache_dir(scenario, root)
    if not (cache / ".git").exists():
        cache.parent.mkdir(parents=True, exist_ok=True)
        smoke_names = {
            "python-pytest-approx-inf-rel": "pytest",
            "go-chi-tee-bytes-double-count": "chi",
            "js-commander-negative-exp-E": "commander",
        }
        smoke = workspaces_root(root) / smoke_names.get(scenario.id, "")
        # Prefer copying an existing smoke clone when available (may still be partial)
        if smoke.is_dir() and (smoke / ".git").exists():
            _run(["git", "clone", str(smoke), str(cache)], check=False)
        if not (cache / ".git").exists():
            _run(["git", "clone", scenario.repo.url, str(cache)])
    # If this was a partial clone, materialize objects for needed commits
    _run(["git", "config", "remote.origin.promisor", "false"], cwd=cache, check=False)
    for ref in filter(None, [scenario.repo.base_ref, scenario.repo.gold_ref]):
        _ensure_commit(cache, ref)
        # Force blob fetch for that tree (helps partial clones)
        _run(["git", "checkout", "-f", "--detach", ref], cwd=cache, check=False)
    return cache


@dataclass
class PreparedWorkspace:
    path: Path
    scenario_id: str
    base_ref: str
    prompt_path: Path
    meta_path: Path
    setup_ran: bool
    setup_log: str | None = None


def prepare_workspace(
    scenario: Scenario,
    root: Path | None = None,
    *,
    run_id: str | None = None,
    force: bool = False,
    run_setup: bool = True,
    dest: Path | None = None,
) -> PreparedWorkspace:
    """Materialize a clean workspace at scenario.base_ref."""
    root = root or ROOT
    path = dest or instance_dir(scenario.id, root, run_id=run_id)

    if path.exists() and force:
        shutil.rmtree(path)
    path.mkdir(parents=True, exist_ok=True)

    cache = ensure_repo_cache(scenario, root)
    git_dir = path / ".git"
    if not git_dir.exists():
        # Clone from remote URL for a self-contained worktree (most reliable).
        # Cache still helps ensure commits are fetchable / for gold file reads.
        _run(["git", "clone", scenario.repo.url, str(path)])
    _ensure_commit(path, scenario.repo.base_ref)
    # Also fetch from local cache if present (offline-friendly)
    _run(["git", "remote", "remove", "hb-cache"], cwd=path, check=False)
    _run(["git", "remote", "add", "hb-cache", str(cache)], cwd=path, check=False)
    _run(
        ["git", "fetch", "hb-cache", scenario.repo.base_ref],
        cwd=path,
        check=False,
    )
    if scenario.repo.gold_ref:
        _run(
            ["git", "fetch", "hb-cache", scenario.repo.gold_ref],
            cwd=path,
            check=False,
        )

    chk = _run(
        ["git", "checkout", "-f", "--detach", scenario.repo.base_ref],
        cwd=path,
        check=False,
    )
    if chk.returncode != 0:
        # Last resort: fetch SHA from origin and retry
        _run(["git", "fetch", "origin", scenario.repo.base_ref], cwd=path, check=False)
        _run(["git", "checkout", "-f", "--detach", scenario.repo.base_ref], cwd=path)

    if force:
        _run(["git", "clean", "-fdx"], cwd=path, check=False)
    else:
        _run(
            ["git", "clean", "-fdx", "-e", ".venv", "-e", "node_modules"],
            cwd=path,
            check=False,
        )

    # Keep HB_* meta out of agent patches via exclude
    exclude = path / ".git" / "info" / "exclude"
    exclude.parent.mkdir(parents=True, exist_ok=True)
    extra = "\n".join(["HB_PROMPT.txt", "HB_META.json", "HB_RUN.md", ""])
    existing = exclude.read_text(encoding="utf-8") if exclude.exists() else ""
    if "HB_PROMPT.txt" not in existing:
        exclude.write_text(existing + extra, encoding="utf-8")

    prompt_path = path / "HB_PROMPT.txt"
    prompt_path.write_text(scenario.prompt.strip() + "\n", encoding="utf-8")

    setup_log: str | None = None
    setup_ran = False
    if run_setup and scenario.acceptance.setup_commands:
        setup_ran = True
        chunks: list[str] = []
        for cmd in scenario.acceptance.setup_commands:
            proc = _run(cmd, cwd=path, check=False)
            chunks.append(f"$ {cmd}\nexit={proc.returncode}\n{proc.stdout}\n{proc.stderr}")
            if proc.returncode != 0:
                setup_log = "\n".join(chunks)
                raise RuntimeError(
                    f"Setup command failed in {path}:\n$ {cmd}\n{proc.stdout}\n{proc.stderr}"
                )
        setup_log = "\n".join(chunks)

    meta = {
        "scenario_id": scenario.id,
        "base_ref": scenario.repo.base_ref,
        "gold_ref": scenario.repo.gold_ref,
        "repo_url": scenario.repo.url,
        "prepared_at": datetime.now(timezone.utc).isoformat(),
        "run_id": run_id,
        "setup_ran": setup_ran,
    }
    meta_path = path / "HB_META.json"
    meta_path.write_text(json.dumps(meta, indent=2) + "\n", encoding="utf-8")

    guide = path / "HB_RUN.md"
    guide.write_text(
        "\n".join(
            [
                f"# Harness Benchmark run workspace",
                f"",
                f"- Scenario: `{scenario.id}`",
                f"- Base: `{scenario.repo.base_ref}`",
                f"- Prompt: `HB_PROMPT.txt`",
                f"",
                f"## What to do",
                f"1. Point your coding harness at this directory.",
                f"2. Feed it the contents of `HB_PROMPT.txt` (and only that intent).",
                f"3. When finished, from the benchmark repo run:",
                f"",
                f"```bash",
                f"hb finish --worktree {path}"
                + (f" --run-id {run_id}" if run_id else f" --scenario {scenario.id}"),
                f"```",
                f"",
                f"Or re-prepare cleanly:",
                f"",
                f"```bash",
                f"hb reset {scenario.id}",
                f"```",
                f"",
            ]
        ),
        encoding="utf-8",
    )

    return PreparedWorkspace(
        path=path,
        scenario_id=scenario.id,
        base_ref=scenario.repo.base_ref,
        prompt_path=prompt_path,
        meta_path=meta_path,
        setup_ran=setup_ran,
        setup_log=setup_log,
    )


def reset_workspace(
    scenario: Scenario,
    root: Path | None = None,
    *,
    run_setup: bool = True,
    path: Path | None = None,
) -> PreparedWorkspace:
    """Reset an existing instance to a clean base_ref checkout."""
    return prepare_workspace(
        scenario,
        root,
        force=True,
        run_setup=run_setup,
        dest=path or instance_dir(scenario.id, root),
    )
