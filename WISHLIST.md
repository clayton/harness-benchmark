# Wishlist

Ideas we have **not** built yet (or only sketched). Ordered loosely by
usefulness for personal experiments — not a roadmap commitment.

PRs / forks welcome; pick whatever matches your axis of curiosity.

---

## Execution & harnesses

| Idea | Notes |
|------|--------|
| **First-class adapters** for Claude Code, Codex, Cursor, Aider, etc. | Today: pi/Grok via `launch_headless` strings; manual ingest for everything else. See `adapters/README.md`. |
| **Parallel matrix execute** | `hb experiment` creates runs; executing them is still sequential / manual. Fan-out with a process pool + shared rate limits. |
| **Experiment-level budget** | Cap total $ across an entire matrix, not only per run. |
| **Docker / sandbox isolation** | SWE-bench-style containers; network allowlists; safer bash for untrusted agent code. |
| **Windows / WSL notes** | Process-group kill and paths are macOS/Linux-first. |
| **Mid-run cost without JSON logs** | Fallback estimators when harnesses only print plain text. |
| **Resume after kill** | Checkpoint workspace + continue under a new budget (careful with fairness). |

---

## Scenarios & corpus

| Idea | Notes |
|------|--------|
| **Feature-delta scenarios** | Changelog / tag-to-tag “implement these features” tasks (deferred until bugfix loop is boringly solid). |
| **More languages / stacks** | Rails LengthValidator and got searchParams are stubbed/shortlisted; smoke + harden. |
| **Private / post-cutoff fixtures** | Reduce contamination for absolute claims. |
| **Scenario authoring CLI** | Scaffold YAML from issue URL + gold PR SHA; auto-detect FAIL_TO_PASS. |
| **Synthetic hard suite** | Controllable difficulty when OSS saturates quality. |
| **Multi-file / multi-hour tasks** | Long-horizon workflows; needs stronger budgets and maybe checkpoints. |

---

## Judging

| Idea | Notes |
|------|--------|
| **LLM rubric judge** | Separate model scores design, restraint, test quality — never the SUT model. |
| **Multi-judge panel** | Aggregate independent rubric scores. |
| **Near-gold similarity flag** | Heuristic “looks memorized” when patch ≈ gold (contamination signal). |
| **Flaky-test handling** | Retries, quarantine, quarantine metadata on scenarios. |
| **Human review UI** | Lightweight pass/fail + notes on top of auto judges. |
| **Patch quality metrics** | Files touched vs allowlist, churn, test-to-prod ratio. |

---

## Skills, workflows, interaction

| Idea | Notes |
|------|--------|
| **Skill version pins** | Today: skill *names* (+ superpowers git SHA in config). Full version matrix later. |
| **Grill-me / plan-then-implement presets** | First-class workflow configs beyond baseline vs superpowers. |
| **Richer human stakeholder UX** | TUI, browser form, Slack hook — not only stdin/file. |
| **Proxy evaluation metrics** | Did the SUT ask the *right* questions? Score dialogue quality. |
| **Forced-ask mode** | Non-ecological stress test: withhold a fact until the agent asks (vs ecological default). |
| **Multi-agent configs** | Orchestrator + workers as a documented treatment (Claude example config is sketch-only). |

---

## Reports, stats, publishing

| Idea | Notes |
|------|--------|
| **Statistical summaries** | Mean/median/CI for quality & cost across repeats; simple bootstrap. |
| **CSV / Parquet export** | For notebooks and external viz. |
| **Diff two runs** | Side-by-side patch / dialogue / telemetry. |
| **GitHub Pages publish** | Optional `scripts/publish_reports.sh` for static hosting. |
| **Result browser TUI** | Terminal navigation of runs without opening HTML. |
| **Leaderboard mode** | Explicit non-goal for v0; if ever, separate from personal A/B tool. |

---

## Reproducibility & ops

| Idea | Notes |
|------|--------|
| **CI** | `hb validate` + `pytest` on PR; optional smoke without API keys. |
| **Submodule / lockfile for vendor** | Optional git submodule for superpowers instead of manual clone. |
| **Resolved version capture** | Auto-fill `resolved_harness_version` / `resolved_model` from CLI output more reliably. |
| **Seed plumbing** | When harnesses expose seeds, pass them through `launch_headless` templates. |
| **Secret hygiene** | Document env vars per provider; never write keys into snapshots. |
| **Result retention policies** | Compress or drop `agent.log` after N days; keep `run.json` + patch. |

---

## Product / packaging (maybe never)

| Idea | Notes |
|------|--------|
| Hosted multi-user app | Explicit non-goal; static HTML is enough for personal use. |
| Full SWE-bench clone | Different product; we stay small and opinionated. |
| Marketplace of scenarios | Nice-to-have if others adopt the format. |

---

## Recently shipped (not wishlist)

Keep this list honest — these used to be “later” and now exist:

- [x] Headless execute + telemetry parse (pi / grok)
- [x] Budget preflight + mid-run kill
- [x] Stakeholder proxy + human interaction modes
- [x] Superpowers pin + A/B experiment YAMLs
- [x] Incomplete-spec scenario + ecological proxy
- [x] Combo fingerprint dedup / skip completed
- [x] Snapshots + `hb rerun`
- [x] Experiment HTML report + artifact deep links
- [x] FAIL_TO_PASS gold test overlay

---

## How to use this file

When you finish a session, either:

1. Check something off “Recently shipped”, or  
2. Add a new row if a pain point showed up twice.

Prefer small vertical slices over boiling the ocean.
