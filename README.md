# Harness Benchmark

**Compare coding-agent *systems*** — harness + model + tools + workflow — on fixed real-world tasks, with **quality and cost** side by side.

## Install

```bash
curl -fsSL https://agentrodeo.dev/install.sh | sh
```

Then:

```bash
export PATH="$HOME/.local/bin:$PATH"
hb
```

`hb` prints what it found on your machine and **one** command to paste. That first ride is a baseline (no extra skills). Nothing spends tokens until you paste it.

After a ride:

```bash
hb report          # local HTML in ./hb-out
hb publish         # optional upload to agentrodeo.dev
```

Same installer from a coding agent: see [SKILL.md](./SKILL.md).

| | |
|---|---|
| **Status** | v0.2 — Go CLI |
| **CLI** | `hb` |
| **License** | MIT |

---

## Why this exists

When you switch models, harnesses, skills, and projects constantly, you cannot tell what helps. Different tasks muddle the signal. Casual demos lie.

Harness Benchmark **freezes the task** and varies only what you care about:

| Axis | Example question |
|------|------------------|
| Harness | Does pi beat the Grok CLI when both use Grok 4.5? |
| Workflow / skills | Does obra/superpowers pay for itself vs a clean baseline? |
| Interaction | Does a stakeholder-proxy Q&A loop help on incomplete specs? |

Cost (tokens, wall time, estimated USD) sits next to quality. Fancy workflows that pass the same tests for 4× the spend show up clearly.

---

## How it works

```text
scenario × config  →  run (workspace + snapshot)
                   →  agent (manual or hb execute)
                   →  patch + telemetry
                   →  independent judges (tests / FAIL_TO_PASS)
                   →  result record
                   →  static HTML report (artifact deep links)
```

| Term | Meaning |
|------|---------|
| **Scenario** | Frozen task: repo `base_ref`, intent prompt, optional gold ref, acceptance tests |
| **Config** | Treatment: harness@version + model + workflow + tools + budget + interaction |
| **Run** | One scenario × config attempt (with optional repeat index) |
| **Snapshot** | Frozen scenario+config JSON under `results/<id>/snapshot.json` for re-runs |
| **Judge** | Offline scorer — tests outside the system under test; gold is reference, not line-match |
| **Report** | Static HTML comparing runs; links into patches, logs, dialogue, snapshots |

Design principles and contamination strategy: **[DESIGN.md](./DESIGN.md)**.  
End-to-end walkthroughs of real experiments: **[docs/EXAMPLES.md](./docs/EXAMPLES.md)**.  
Future work: **[WISHLIST.md](./WISHLIST.md)**.

---

## Optional: harness CLIs + API keys

Headless execute needs whatever harness you pin in a config:

| Config family | Needs |
|---------------|--------|
| `pi-grok45-*` | [pi](https://github.com/badlogic/pi-mono) CLI, OpenRouter (or other) key for `x-ai/grok-4.5` |
| `grok-grok45-*` | Grok Build / `grok` CLI authenticated to grok.com |
| `pi-*-superpowers` | Vendored superpowers pin (see [vendor/README.md](./vendor/README.md)) |

Manual mode needs no API keys: prepare a workspace, run any agent yourself, `hb finish`.

---

## 5-minute loop

```bash
# 1) Create a pending run (fresh workspace at scenario base_ref)
hb run -s js-commander-negative-exp-E -c pi-grok45-baseline

# 2a) Headless (preferred — captures tokens/$ from JSON agent logs)
hb execute <run_id>

# 2b) Or open the printed workspace and run your agent on HB_PROMPT.txt, then:
# hb finish <run_id>

# 3) Static report with links into results/<run_id>/
hb report
open reports/latest.html
```

Re-spend the same combo:

```bash
hb run -s js-commander-negative-exp-E -c pi-grok45-baseline --force
# or always create a new trial from a frozen snapshot:
hb rerun <run_id> && hb execute <new_run_id>
```

---

## Features (what works today)

### Corpus

- **Scenarios** — real OSS bugfixes (pytest, chi, commander.js, FastAPI stream router, …). Agents see **intent only**; `gold_ref` is judge-side.
- **Configs** — baseline and treatment recipes with **pinned harness versions** and models.
- **Experiments** — YAML matrices (`scenario_ids` × `config_ids` × repeats) under `experiments/`.

### Execution

- **`hb execute`** — headless launch from `config.harness_options.launch_headless`, multi-round stakeholder Q&A, telemetry parse (pi JSONL / grok JSON).
- **Budget guards** — `max_minutes` / `max_usd` / `max_turns` / `max_tokens`; soft warn ~80%; hard kill process group; status `budget_exceeded`.
- **Interaction modes** — `unattended` | `proxy` (cheap stakeholder model) | `human`.
- **Dedup fingerprints** — skip completed scenario×config combos unless `--force`.
- **Snapshots + rerun** — freeze definition; re-create setup (not bit-identical model output).

### Judging & reporting

- **FAIL_TO_PASS overlay** — pull regression tests from `gold_ref` when the base tree lacks them.
- **Static HTML** — quality / cost / time / tokens winner chips; artifact links to `patch.diff`, `agent.log`, `dialogue.json`, `snapshot.json`, `judge.json`, `run.json`.
- **Experiment reports** — `hb report --from experiments/foo.yaml` → `reports/exp-<id>.html`.

### What is intentionally *not* here yet

See [WISHLIST.md](./WISHLIST.md): Docker sandboxes, formal multi-adapter package, LLM rubric judges, parallel matrix runners, stats packages, published leaderboards, etc.

---

## CLI map

| Command | Purpose |
|---------|---------|
| `hb validate` | Schema-check scenarios / configs |
| `hb list` | `scenarios` \| `configs` \| `runs` \| `all` |
| `hb prepare` / `hb reset` | Shared workspace helpers |
| `hb show-prompt` | Print agent prompt |
| `hb run` | New run id + fresh instance workspace |
| `hb execute` | Headless agent + budget + finish/judge |
| `hb finish` | Capture patch, judge, save (manual path) |
| `hb experiment` | Matrix of pending runs (`--from`, `--force`, `--repeats`) |
| `hb rerun` | New trial from frozen snapshot |
| `hb show-snapshot` | Inspect frozen definition |
| `hb judge` | Tests only (no result record) |
| `hb ingest` | One-shot record from worktree/patch |
| `hb report` | Static HTML (`--from` / `--experiment` / `--out`) |

```bash
hb --help
hb execute --help
hb report --help
```

---

## Experiments in this repo

Pre-built matrices under `experiments/`:

| File | Question |
|------|----------|
| `harness-pi-vs-grok-model45.yaml` | Same model (Grok 4.5), different harness (pi vs Grok CLI) |
| `skills-superpowers-vs-baseline.yaml` | Superpowers ON vs OFF (easy scenarios) |
| `skills-superpowers-hard.yaml` | Same on a harder FastAPI multi-file bug |
| `skills-superpowers-proxy-incomplete.yaml` | Incomplete prompt + stakeholder proxy |

Walkthroughs and directional findings: **[docs/EXAMPLES.md](./docs/EXAMPLES.md)**.

```bash
hb experiment --from experiments/harness-pi-vs-grok-model45.yaml
hb execute <run_id>          # for each pending run
hb report --from experiments/harness-pi-vs-grok-model45.yaml
```

---

## Authoring

### Scenario (YAML)

```yaml
id: my-bugfix
type: bugfix
title: Short title
description: What success looks like
prompt: |
  Human intent only. Never paste the gold patch.
repo:
  url: https://github.com/org/repo.git
  base_ref: <sha-before-fix>
  gold_ref: <sha-with-fix>   # optional; judge-only
acceptance:
  setup_commands: [...]
  test_commands: [...]
  fail_to_pass: [path/or/node/id]   # tests that fail at base, pass after fix
contamination_risk: medium
```

Rules: [scenarios/README.md](./scenarios/README.md), shortlist notes: [scenarios/SHORTLIST.md](./scenarios/SHORTLIST.md).

### Config (YAML)

```yaml
id: pi-grok45-baseline
name: pi · Grok 4.5 · baseline
harness: pi
harness_version: "0.84.1"
model: grok-4.5
workflow: baseline
interaction: unattended
budget:
  max_minutes: 45
  max_turns: 80
  max_usd: 2.0          # optional hard $ cap
harness_options:
  launch_headless: >-
    pi -p --mode json --provider openrouter --model x-ai/grok-4.5 ...
```

### Experiment (YAML)

```yaml
id: my-ab
name: "A/B title"
hypothesis: >
  What you expect and why.
scenario_ids: [js-commander-negative-exp-E]
config_ids: [pi-grok45-baseline, pi-grok45-superpowers]
repeats: 1
```

---

## Budget guards

Configs declare `budget:`. `hb execute` enforces non-null fields:

| Limit | Behavior |
|-------|----------|
| `max_minutes` | Caps wall clock; clamps CLI `--timeout` |
| `max_usd` | Polls agent JSON usage; kills process group when over |
| `max_turns` / `max_tokens` | Same mid-run kill |
| ~80% of any limit | Soft warn only |

Exceeded runs still get judged and are marked `budget_exceeded`.

---

## Interaction modes (stakeholder Q&A)

| Mode | Config | Behavior |
|------|--------|----------|
| `unattended` | default | No Q&A; implement from prompt |
| `proxy` | `interaction: proxy` + `stakeholder:` | Answers `STAKEHOLDER_QUESTION` from `scenario.stakeholder_brief` (cheap model) |
| `human` | `interaction: human` | Pauses for stdin / answer file |

```text
STAKEHOLDER_QUESTION:
What should the error message say?
STAKEHOLDER_END
```

Dialogue → `results/<run_id>/dialogue.json`. Proxy $ is tracked separately from SUT cost.

---

## Reports & artifacts

```bash
hb report                              # reports/latest.html
hb report --from experiments/foo.yaml  # reports/exp-<id>.html + hypothesis
hb report -e skills-superpowers-hard
```

Open HTML **from the repo tree** so relative links resolve:

```text
reports/latest.html  →  ../results/<run_id>/patch.diff
                     →  ../results/<run_id>/agent.log
                     →  ../results/<run_id>/dialogue.json
                     →  ../results/<run_id>/snapshot.json
                     →  ../results/<run_id>/judge.json
                     →  ../results/<run_id>/run.json
```

Local run data (`results/*`, `reports/*`, `workspaces/`) is **gitignored** by default so clones stay small. Re-run experiments on your machine to regenerate them.

---

## Repo layout

```text
hb/              Python package (CLI, execute, judge, report, budget, …)
scenarios/       Task definitions (YAML)
configs/         Treatment recipes (YAML) — pin harness_version
experiments/     Matrix definitions (YAML)
tests/           Unit tests (pytest)
scripts/         Smoke helpers for scenario envs
adapters/        Notes on future harness adapters
vendor/          Pins for experiment deps (clone instructions; large checkouts ignored)
docs/            Examples and guides
results/         Local run records (gitignored contents)
reports/         Local HTML (gitignored contents)
workspaces/      Local clones/instances (gitignored)
DESIGN.md        Architecture & principles
WISHLIST.md      Future ideas
```

---

## Development

```bash
pip install -e ".[dev]"
pytest -q
ruff check hb tests
hb validate
./scripts/smoke_scenarios.sh    # optional: prepare easy scenario envs
```

---

## Principles (short)

1. **Lock non-variables** — test one axis at a time when possible.
2. **Always baseline** — minimal workflow control for every claim.
3. **Pin harness version**, not just family (`pi@0.84.1`, not “pi”).
4. **Prompt is intent** — agents never see the gold patch.
5. **Gold is reference** — correctness via tests/behavior, not line match.
6. **Independent judges** — scoring outside the system under test.
7. **Cost is first-class** — tokens, time, $ next to quality.
8. **Repeats for variance** — N≥2–3 before strong workflow claims.

---

## Status

**v0 is usable for personal experiments.** Easy and harder scenarios smoked; headless pi/Grok execute with tokens/$; superpowers A/B; incomplete-spec + proxy; budget kill; experiment HTML with artifact links.

Community site (v1): [agentrodeo.dev](https://agentrodeo.dev) — `hb rider init` then `hb publish <run_id>`.

Not SWE-bench-scale. Directional signal first.

---

## License

MIT — see [LICENSE](./LICENSE).
