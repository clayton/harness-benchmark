"""Helpers for capturing git diffs and patch stats."""

from __future__ import annotations

import re
import subprocess
from pathlib import Path

from hb.models import PatchStats


_HB_META_NAMES = frozenset(
    {"HB_PROMPT.txt", "HB_META.json", "HB_RUN.md", "HB_LAUNCH.md"}
)


def git_diff(worktree: Path, base_ref: str | None = None) -> str:
    """Return unified diff of working tree vs base_ref (or HEAD if clean index)."""
    worktree = worktree.resolve()
    if base_ref:
        cmd = ["git", "-C", str(worktree), "diff", base_ref]
    else:
        # Unstaged + staged against HEAD
        cmd = ["git", "-C", str(worktree), "diff", "HEAD"]
    result = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if result.returncode not in (0, 1):
        raise RuntimeError(result.stderr or f"git diff failed: {result.returncode}")
    diff = result.stdout
    # Include untracked as empty-file diffs when possible (skip HB meta files)
    untracked = subprocess.run(
        ["git", "-C", str(worktree), "ls-files", "--others", "--exclude-standard"],
        capture_output=True,
        text=True,
        check=False,
    )
    if untracked.returncode == 0 and untracked.stdout.strip():
        for rel in untracked.stdout.strip().splitlines():
            if Path(rel).name in _HB_META_NAMES or rel.startswith("HB_"):
                continue
            file_path = worktree / rel
            if not file_path.is_file():
                continue
            add = subprocess.run(
                ["git", "-C", str(worktree), "diff", "--no-index", "--", "/dev/null", rel],
                capture_output=True,
                text=True,
                check=False,
            )
            # git diff --no-index returns 1 when files differ
            if add.stdout:
                diff += ("\n" if diff and not diff.endswith("\n") else "") + add.stdout
    return diff


def stats_from_diff(diff: str) -> PatchStats:
    paths: list[str] = []
    insertions = 0
    deletions = 0
    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            path = line[6:]
            if path != "/dev/null" and path not in paths:
                paths.append(path)
        elif line.startswith("diff --git "):
            # diff --git a/foo b/foo
            m = re.match(r"diff --git a/(.+) b/(.+)", line)
            if m:
                path = m.group(2)
                if path not in paths:
                    paths.append(path)
        elif line.startswith("+") and not line.startswith("+++"):
            insertions += 1
        elif line.startswith("-") and not line.startswith("---"):
            deletions += 1
    return PatchStats(
        files_changed=len(paths),
        insertions=insertions,
        deletions=deletions,
        paths=paths,
    )
