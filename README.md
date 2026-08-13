# Harness Benchmark

**Compare coding-agent *systems*** — harness + model + tools + workflow — on fixed real-world tasks, with **quality and cost** side by side.

## Install

```bash
curl -fsSL https://agentrodeo.dev/install.sh | sh
hbench
```

The installer is pinned to a release tag. Checksums live in the script itself. It puts `hbench` in a directory already on your PATH when it can (Homebrew or `/usr/local/bin`). If it cannot, it prints one `export … && hbench` line for that terminal and says so if it appends to your shell rc.

`hbench` with no args prints **one** command. `hbench doctor` is the full report. Nothing spends tokens until you paste the printed command. The old `hb` name collided with Honeybadger's CLI, so the binary is `hbench`.

After a ride:

```bash
hbench report          # local HTML in ./hb-out
hbench publish         # optional upload to agentrodeo.dev
```

Same installer from a coding agent: see [SKILL.md](./SKILL.md).

| | |
|---|---|
| **Status** | v0.2 — Go CLI |
| **CLI** | `hbench` |
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
hbench                     # one suggested command; hbench doctor for the full report
# paste what it prints, e.g.:
hbench run -s go-chi-tee-bytes-double-count --harness grok && hbench execute

hbench report              # local HTML in ./hb-out — nothing is uploaded
# optional:
hbench publish             # upload the latest finished run to agentrodeo.dev
```

Manual path (GUI harness, or you want to drive the agent yourself):

```bash
hbench run -s go-chi-tee-bytes-double-count --harness manual
# stay in this directory; open the printed workspace, fix the prompt, then:
hbench finish
hbench report
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
| `hbench` | Print one suggested command |
| `hbench doctor` | Probe this machine (harnesses, toolchains, skills) |
| `hbench version` | Print `hbench <ver> (go)` |
| `hbench list scenarios` | Official corpus from the home cache |
| `hbench list runs` | Local runs in `./hb-out` |
| `hbench run -s <id> --harness <name>` | New pending run + workspace |
| `hbench execute [run_id]` | Headless harness, then finish/judge (spends tokens) |
| `hbench finish [run_id]` | Capture patch, judge, save. Stay in the start directory. `--force` to re-judge. |
| `hbench report` | Local HTML in `./hb-out/report.html` (does not upload) |
| `hbench publish [run_id]` | Optional upload to agentrodeo.dev |

```bash
hbench
hbench version
hbench list scenarios
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

Walkthroughs and directional findings: **[docs/EXAMPLES.md](./docs/EXAMPLES.md)**. Those YAML matrices were run with the older Python CLI; v0.2 is one official ride at a time via `hb run` / `hb execute`.

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
hb report          # ./hb-out/report.html — local only, nothing uploaded
```

Run records live under `./hb-out/<run_id>/` (`run.json`, `patch.diff`, `snapshot.json`, …). Official YAML is cached in the home data dir. Older `results/` and `reports/` trees in this repo are leftover personal experiment output.

---

## Repo layout

```text
cmd/hb/          Go CLI entry (this is `hb`)
internal/        doctor, corpus, run/execute/judge, report, publish
install.sh       One-line installer (release binary, else go build)
SKILL.md         Same installer for coding agents
scenarios/       Official task definitions (also embedded in the binary)
configs/         Older treatment recipes (YAML)
experiments/     Older matrix definitions (YAML)
hb/              Leftover Python package — not the user-facing CLI
tests/           Leftover Python unit tests
docs/            Examples and guides
DESIGN.md        Architecture & principles
WISHLIST.md      Future ideas
```

---

## Development

```bash
go test -mod=mod ./cmd/... ./internal/...
go build -mod=mod -o hb ./cmd/hb
```

The Python tree is not required to install or run `hb`.

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

**v0.3 is the Go CLI (`hbench`).** Install with the one-liner, run `hbench`, paste the suggested command. Community site: [agentrodeo.dev](https://agentrodeo.dev) — `hbench publish` after a local ride.

Not SWE-bench-scale. Directional signal first.

---

## License

MIT — see [LICENSE](./LICENSE).
