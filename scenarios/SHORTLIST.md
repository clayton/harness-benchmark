# Scenario shortlist (research notes)

Curated 2026-08-11. Prefer **recent merged bugfixes** with tests, clear intent,
and laptop-buildable scopes. Gold patches are **judge-only**; agents get the prompt.

## Selected (v0 corpus)

| ID | Lang | Repo | PR | Merged | Size | Difficulty |
|----|------|------|-----|--------|------|------------|
| `python-pytest-approx-inf-rel` | Python | pytest-dev/pytest | [#14754](https://github.com/pytest-dev/pytest/pull/14754) | 2026-08-02 | +10/−0 | easy |
| `go-chi-tee-bytes-double-count` | Go | go-chi/chi | [#1085](https://github.com/go-chi/chi/pull/1085) | 2026-05-16 | +28/−1 | easy–medium |
| `js-commander-negative-exp-E` | JS | tj/commander.js | [#2544](https://github.com/tj/commander.js/pull/2544) | 2026-07-05 | +5/−1 | easy |
| `ruby-rails-length-validator-nil-proc` | Ruby | rails/rails | [#58428](https://github.com/rails/rails/pull/58428) | 2026-08-10 | +44/−1 | medium |
| `ts-got-searchparams-setter` | TS | sindresorhus/got | [#2454](https://github.com/sindresorhus/got/pull/2454) | 2026-06-21 | +55/−3 | medium |

### Why these

- **Language diversity:** Python, Go, JavaScript, Ruby, TypeScript — not a single-stack bench.
- **Real maintainer fixes** with regression tests (except rack-style one-liners we skipped).
- **Recent enough** (2026) to reduce (not eliminate) memorization vs ancient textbook bugs.
- **Scoped:** most are 1–3 production files + tests; Rails is large but change is ActiveModel-only.
- **Clear behavioral acceptance** (crash, wrong count, rejected arg, dropped query, etc.).

### Contamination notes

| Scenario | Risk | Why |
|----------|------|-----|
| pytest | medium–high | Famous project; PR is new but patterns may be familiar |
| chi | medium | Popular router; bug is narrow |
| commander | medium | Popular; tiny surface |
| rails | high issue age / medium fix age | Issue #40642 from 2020; fix is Aug 2026 — models may know the *symptom* |
| got | medium | Popular HTTP client; June 2026 fix |

Treat scores as **relative config ranking**, not absolute capability.

## Strong alternates (swap if a primary is too hard to run)

| Lang | Repo | PR | Notes |
|------|------|-----|-------|
| Go | spf13/cobra | [#2356](https://github.com/spf13/cobra/pull/2356) | `os.Args` mutation via `append` spare capacity; excellent test |
| Ruby | rack/rack | [#2420](https://github.com/rack/rack/pull/2420) | `MockResponse#body` with Proc; lighter than full Rails |
| TS | colinhacks/zod | [#6354](https://github.com/colinhacks/zod/pull/6354) | Declared `__proto__` key; Aug 2026; monorepo setup |
| Python | pytest-dev/pytest | [#14781](https://github.com/pytest-dev/pytest/pull/14781) | Duplicate exception chain output; merged 2026-08-11 |
| JS/TS | colinhacks/zod | [#6347](https://github.com/colinhacks/zod/pull/6347) | Emoji regex ReDoS; more security-flavored |

## Rejected (this pass)

- Random student “bug fix” PRs from global search — no quality bar.
- Nokogiri security/UAF fixes — great engineering, poor agent sandbox (native ext, security).
- Docs-only / CI-only fixes — no product signal.
- requests typing-only mutability PR — weak runtime oracle.
- rack mime `.pem` — too trivial / taste-ish for a first corpus.

## Smoke status (2026-08-11)

| Scenario | Status | Notes |
|----------|--------|-------|
| `python-pytest-approx-inf-rel` | **OK** | Needs `SETUPTOOLS_SCM_PRETEND_VERSION=9.1.0` + `pip install -e ".[dev]"`. Base: OverflowError; gold: ValueError. |
| `go-chi-tee-bytes-double-count` | **OK** | `go test ./middleware/`. Gold test on base fails 11 vs 22. |
| `js-commander-negative-exp-E` | **OK** | Use `node --test tests/negatives.test.js` (not full `npm test`). Node ≥22.12. |
| `python-fastapi-stream-router-schema` | **OK** | Hard. Base 26 pass; gold tests on base → 5 fail; gold 34 pass. Needs httpx2 + pytest-timeout. |
| rails / got | not smoked yet | |

Re-run:

```bash
./scripts/smoke_scenarios.sh          # pytest + chi + commander
./scripts/smoke_scenarios.sh chi      # one scenario
```

Workspaces live under `workspaces/` (gitignored).

## Next

1. Pin real harness versions on configs.
2. Run baseline vs one fancy workflow on these 3 scenarios (manual ingest or adapter).
3. Smoke rails + got when ready.
4. Implement auto-judge that applies FAIL_TO_PASS tests from gold when present.

