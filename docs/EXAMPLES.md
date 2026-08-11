# Examples

End-to-end recipes that match how this repo was developed. Numbers below are
**directional** (single trials, easy tasks often quality-saturated). Re-run
yourself before citing claims.

Prerequisites: `pip install -e ".[dev]"`, harness CLIs + API keys as needed.

---

## 0. Sanity: validate and list

```bash
hb validate
hb list scenarios
hb list configs
hb list runs
```

Smoke scenario environments (clone/setup only — no LLM):

```bash
./scripts/smoke_scenarios.sh          # pytest + chi + commander
./scripts/smoke_scenarios.sh chi
```

---

## 1. Single run (manual or headless)

**Goal:** One bugfix, one config, one result row.

```bash
hb run -s js-commander-negative-exp-E -c pi-grok45-baseline
# prints run id + workspace path

hb execute <run_id>
# or: open workspace, run any agent on HB_PROMPT.txt, then hb finish <run_id>

hb report
open reports/latest.html
```

Artifacts under `results/<run_id>/`:

| File | What |
|------|------|
| `run.json` | Immutable result record |
| `patch.diff` | Agent changes vs `base_ref` |
| `agent.log` | Raw harness stdout (JSON usage when available) |
| `snapshot.json` | Frozen scenario + config |
| `judge.json` | Test command outcomes |
| `dialogue.json` | Q&A if interaction ≠ unattended |

---

## 2. Harness A/B — same model, different harness

**Question:** Holding **Grok 4.5** and **baseline workflow** fixed, does **pi**
or the **Grok CLI** win on quality per cost?

Experiment file: `experiments/harness-pi-vs-grok-model45.yaml`

```bash
hb experiment --from experiments/harness-pi-vs-grok-model45.yaml
hb list runs
hb execute <pi_run_id>
hb execute <grok_run_id>

hb report --from experiments/harness-pi-vs-grok-model45.yaml
open reports/exp-harness-pi-vs-grok-model45.html
```

**What to lock:** model, prompt, base_ref, budget intent.  
**What varies:** harness (+ provider routing — pi via OpenRouter vs Grok CLI native; document if that muddies the result).

**Early directional signal (commander uppercase `-E` bug, one trial each):**

- Both completed with quality ~1.0 (tests pass).
- Costs/times were in the same ballpark (tens of seconds, ~$0.15–0.20); pi was slightly faster on that trial.
- Easy scenarios often **do not differentiate** harnesses — both solve them cleanly.

---

## 3. Skills A/B — Superpowers ON vs OFF

**Question:** Does [obra/superpowers](https://github.com/obra/superpowers) improve
quality enough to justify higher cost vs a clean pi baseline?

1. Vendor the pin (once):

```bash
git clone https://github.com/obra/superpowers.git vendor/superpowers
cd vendor/superpowers && git checkout 44c9b2d   # example pin; match configs
cd ../..
```

2. Run the matrix:

```bash
# Easy scenarios (commander, chi, …)
hb experiment --from experiments/skills-superpowers-vs-baseline.yaml
hb execute <each_run_id> --timeout 1800

# Harder FastAPI multi-file OpenAPI/routing bug
hb experiment --from experiments/skills-superpowers-hard.yaml
hb execute <each_run_id> --timeout 2400

hb report --from experiments/skills-superpowers-hard.yaml
open reports/exp-skills-superpowers-hard.html
```

Configs:

| Config | Intent |
|--------|--------|
| `pi-grok45-baseline` | `--no-skills --no-extensions`, unattended |
| `pi-grok45-superpowers` | Superpowers skill path, unattended, larger budget |

**Early directional signal:**

- On easy tasks, both arms often hit **quality 1.0**; Superpowers was roughly **3–4.5×** more expensive (more turns / scaffolding).
- On the harder complete FastAPI scenario, both still passed in early trials; Superpowers still cost more wall and $.
- **Takeaway:** when quality saturates, cost is the differentiator — Superpowers must be judged on tasks that *need* structure, or on incomplete specs (next example).

---

## 4. Incomplete prompt + stakeholder proxy

**Question:** On an **incomplete** task brief, does Superpowers + a cheap
stakeholder proxy improve outcomes vs guessing unattended?

Scenario: `python-fastapi-stream-router-incomplete`  
(richer intent lives in `stakeholder_brief`, not the SUT prompt)

```bash
hb experiment --from experiments/skills-superpowers-proxy-incomplete.yaml
hb execute <run_id> --timeout 2400

hb report --from experiments/skills-superpowers-proxy-incomplete.yaml
open reports/exp-skills-superpowers-proxy-incomplete.html
```

Config sketch (`pi-grok45-superpowers-proxy`):

- `interaction: proxy`
- `stakeholder.model: anthropic/claude-haiku-4.5` (cheap; answers only — no coding tools)
- SUT may emit:

```text
STAKEHOLDER_QUESTION:
…
STAKEHOLDER_END
```

**Ecological note:** the proxy does **not** force questions. If the SUT never
asks, `qa_rounds=0` and the incomplete brief is all it had. That is a valid
outcome (and a useful signal about the workflow).

Proxy tokens/$ are stored separately from SUT telemetry.

---

## 5. Matrix workflow + dedup

```bash
# Create only missing combos (skips completed fingerprints)
hb experiment --from experiments/skills-superpowers-vs-baseline.yaml

# Force re-spend everything
hb experiment --from experiments/skills-superpowers-vs-baseline.yaml --force

# Aim for 3 completed trials per cell
hb experiment --from experiments/skills-superpowers-vs-baseline.yaml --repeats 3

# One-off re-trial of an existing run's frozen snapshot
hb rerun <run_id>
hb execute <new_run_id>
```

Fingerprint includes scenario ids/refs/prompt hash, harness, model, interaction,
skills pin, seed — so “same experiment tomorrow” does not double-bill by default.

---

## 6. Budget kill (safety)

```yaml
# in a config YAML
budget:
  max_minutes: 10
  max_usd: 0.50
  max_turns: 40
```

```bash
hb run -s js-commander-negative-exp-E -c <config-with-tight-budget>
hb execute <run_id>
# soft WARN near 80%; KILL process group at limit → status budget_exceeded
```

Use this when exploring expensive workflows so a runaway agent cannot burn the card.

---

## 7. Human stakeholder mode

```bash
# config: pi-grok45-superpowers-human (interaction: human)
hb run -s python-fastapi-stream-router-incomplete -c pi-grok45-superpowers-human
hb execute <run_id> --human-wait 3600
# when the SUT asks, answer via stdin (end with END) or
# results/<id>/stakeholder_answer.txt
```

Useful for “am I the missing context?” debugging; proxy is better for unattended matrices.

---

## 8. Reading a report

Winner chips (per scenario comparison set):

| Chip | Meaning |
|------|---------|
| **best** | Highest mean judge score |
| **cheap** | Lowest estimated USD |
| **fast** | Lowest wall time |
| **lean** | Lowest token total |

Click **artifacts** on a row to open the patch, agent log, dialogue, snapshot, judge log, or full `run.json`.

---

## Interpreting results (honest defaults)

1. **Easy scenarios saturate** — both arms pass → look at cost/time, or move to harder / incomplete tasks.
2. **N=1 is anecdote** — use `--repeats 3` before strong claims.
3. **Provider routing** (OpenRouter vs native) is part of the system under test unless you control it.
4. **Contamination** on famous OSS is real — treat rankings as relative, not absolute capability.
5. **Gold is not the only correct patch** — judges use tests; different implementations can all pass.

---

## Related docs

- [README.md](../README.md) — install, CLI, concepts
- [DESIGN.md](../DESIGN.md) — architecture, fairness, versioning
- [WISHLIST.md](../WISHLIST.md) — not built yet
- [scenarios/README.md](../scenarios/README.md) — authoring scenarios
- [vendor/README.md](../vendor/README.md) — pinning superpowers
