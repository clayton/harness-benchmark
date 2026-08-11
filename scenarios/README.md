# Scenarios

A **scenario** is a frozen task the agent must solve. Agents receive only the
`prompt` and the repo at `base_ref`. They never see `gold_ref` or the gold patch.

## Types

| Type | Best for |
|------|----------|
| `bugfix` | Isolated, SWE-bench-like; failing tests as oracle |
| `feature` | Changelog / minor-version feature deltas |
| `refactor` | Behavior-preserving structural change |

## Authoring rules

1. **Prompt = human intent**, not the solution. Issue body + acceptance criteria is ideal.
2. Prefer tasks with **automated tests** you can run after the patch.
3. Tag `contamination_risk` honestly (`high` for famous old issues).
4. Keep `id` stable forever — results point at it.
5. One logical unit of work per scenario (one bug, or 1–3 tightly related features).

## v0 corpus (curated)

See [SHORTLIST.md](./SHORTLIST.md) for research notes and alternates.

| ID | Lang | Notes |
|----|------|-------|
| `python-pytest-approx-inf-rel` | Python | Easy smoke; pytest approx + inf/rel |
| `go-chi-tee-bytes-double-count` | Go | Easy smoke; middleware byte count |
| `js-commander-negative-exp-E` | JS | Easy smoke; negative exponent `-E` |
| `python-fastapi-stream-router-schema` | Python | Harder multi-file OpenAPI/routing |
| `python-fastapi-stream-router-incomplete` | Python | Incomplete SUT prompt; use with proxy/human |
| `ruby-rails-length-validator-nil-proc` | Ruby | Shortlisted; not fully smoked in v0 |
| `ts-got-searchparams-setter` | TS | Shortlisted; not fully smoked in v0 |

`example-synthetic-bugfix` is tooling-only — **do not use for claims**.

Feature-delta scenarios are deferred until the bugfix loop is proven.

### Selection heuristics

**Good:** clear intent, tests, scoped change, maintained history, recent merge.

**Risky:** celebrity textbook bugs, flaky builds, ambiguous acceptance, native/security-only work without sandboxing.

## File format

One YAML file per scenario. See any curated file or `example-synthetic-bugfix.yaml`.
