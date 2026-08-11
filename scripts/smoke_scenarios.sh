#!/usr/bin/env bash
# Smoke-check curated scenarios: clone (if needed), setup, base tests, bug repro, gold tests.
# Usage:
#   ./scripts/smoke_scenarios.sh              # first 3 easy scenarios
#   ./scripts/smoke_scenarios.sh all          # reserved
#   ./scripts/smoke_scenarios.sh chi          # one of: pytest|chi|commander

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WS="$ROOT/workspaces"
mkdir -p "$WS"

run_pytest() {
  local dir="$WS/pytest"
  local base="1d5c40392ea42db38582b096f639eb762a5527c8"
  local gold="224e9ef86758841a716c43a2bc711e1f56934528"
  if [[ ! -d "$dir/.git" ]]; then
    git clone --filter=blob:none https://github.com/pytest-dev/pytest.git "$dir"
  fi
  cd "$dir"
  git fetch --depth=1 origin "$base" "$gold" 2>/dev/null || true
  git checkout -q "$base"
  python3 -m venv .venv
  # shellcheck disable=SC1091
  source .venv/bin/activate
  export SETUPTOOLS_SCM_PRETEND_VERSION=9.1.0
  pip install -q -e ".[dev]"
  echo "[pytest] base approx suite"
  python -m pytest testing/python/approx.py -q --tb=line
  python - <<'PY'
from datetime import timedelta
import pytest
try:
    pytest.approx(timedelta(seconds=1), rel=float("inf"))
    raise SystemExit("expected OverflowError at base")
except OverflowError:
    print("[pytest] base bug repro OK (OverflowError)")
PY
  git checkout -q "$gold"
  pip install -q -e ".[dev]"
  echo "[pytest] gold approx suite"
  python -m pytest testing/python/approx.py -q --tb=line
  python - <<'PY'
from datetime import timedelta
import pytest
try:
    pytest.approx(timedelta(seconds=1), rel=float("inf"))
    raise SystemExit("expected ValueError at gold")
except ValueError as e:
    print(f"[pytest] gold behavior OK: {e}")
PY
  echo "[pytest] SMOKE OK"
}

run_chi() {
  local dir="$WS/chi"
  local base="a54874f0e2f12647a19e82ee70dfa8185014100c"
  local gold="4ef87eaf2cfb27d3126d48194e1a84806acc1aed"
  if [[ ! -d "$dir/.git" ]]; then
    git clone --filter=blob:none https://github.com/go-chi/chi.git "$dir"
  fi
  cd "$dir"
  git fetch --depth=1 origin "$base" "$gold" 2>/dev/null || true
  git checkout -q "$base"
  echo "[chi] base middleware tests"
  go test ./middleware/ -count=1
  # FAIL_TO_PASS: gold tests on base code
  git show "$gold:middleware/wrap_writer_test.go" > middleware/wrap_writer_test.go
  if go test ./middleware/ -count=1 -run TestHttpFancyWriterReadFromByteCountWithTee; then
    git checkout -q -- middleware/wrap_writer_test.go
    echo "[chi] expected FAIL_TO_PASS to fail on base" >&2
    exit 1
  fi
  git checkout -q -- middleware/wrap_writer_test.go
  echo "[chi] FAIL_TO_PASS fails on base as expected"
  git checkout -q "$gold"
  echo "[chi] gold middleware tests"
  go test ./middleware/ -count=1
  echo "[chi] SMOKE OK"
}

run_commander() {
  local dir="$WS/commander"
  local base="c3ffcfcdac9237cb446ae0acc5b228380e6ba52a"
  local gold="a6bcd2ec188dd684c11076ea74747b46eb32f44c"
  if [[ ! -d "$dir/.git" ]]; then
    git clone --filter=blob:none https://github.com/tj/commander.js.git "$dir"
  fi
  cd "$dir"
  git fetch --depth=1 origin "$base" "$gold" 2>/dev/null || true
  git checkout -q "$base"
  npm install --silent
  echo "[commander] base negatives tests"
  node --test tests/negatives.test.js
  node - <<'JS'
import { Command } from './index.js';
const p = new Command();
p.exitOverride();
p.argument('<temp>');
try {
  p.parse(['-1E3'], { from: 'user' });
  throw new Error('expected unknown option at base');
} catch (e) {
  if (!String(e.message).includes('unknown option')) throw e;
  console.log('[commander] base bug repro OK');
}
JS
  git checkout -q "$gold"
  echo "[commander] gold negatives tests"
  node --test tests/negatives.test.js
  node - <<'JS'
import { Command } from './index.js';
const p = new Command();
p.exitOverride();
p.argument('<temp>');
p.parse(['-1E3'], { from: 'user' });
console.log('[commander] gold behavior OK', p.args);
JS
  echo "[commander] SMOKE OK"
}

target="${1:-easy3}"
case "$target" in
  easy3|all)
    run_pytest
    run_chi
    run_commander
    ;;
  pytest) run_pytest ;;
  chi) run_chi ;;
  commander) run_commander ;;
  *)
    echo "Usage: $0 [easy3|pytest|chi|commander]" >&2
    exit 2
    ;;
esac

echo "All requested smokes passed."
