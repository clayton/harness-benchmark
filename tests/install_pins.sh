#!/bin/sh
# The installer must pin checksums locally, not fetch SUMS from GitHub.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
script="$ROOT/install.sh"
fail=0

if grep -q SHA256SUMS "$script"; then
  echo "FAIL install.sh still fetches SHA256SUMS"
  fail=1
else
  echo "ok   no SHA256SUMS fetch"
fi

count=$(grep -c 'hbench-darwin-arm64) echo "' "$script" || true)
if [ "$count" -ne 1 ]; then
  echo "FAIL missing inlined darwin-arm64 sha"
  fail=1
else
  echo "ok   inlined darwin-arm64 sha"
fi

hexes=$(grep -E 'echo "[0-9a-f]{64}"' "$script" | wc -l | tr -d ' ')
if [ "$hexes" -lt 4 ]; then
  echo "FAIL expected 4 inlined shas, got $hexes"
  fail=1
else
  echo "ok   $hexes inlined shas"
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "all pin tests passed"
