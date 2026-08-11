# Harness Benchmark — Design

## Problem

Coding agents are systems: model + harness + tools + workflow. Casual use across
different projects cannot tell you which combination is better. You need fixed
tasks, controlled configurations, independent judging, and cost metrics.

## Goal (v0)

A small, open-source, personal experiment platform that answers:

> For this task type, does configuration A beat configuration B on quality per cost?

Not an academic leaderboard. Directional signal with a handful of scenarios and
repeatable runs.

## Non-goals (v0)

- Full Docker evaluation harness like SWE-bench (maybe later)
- Exhaustive multi-language corpus
- Live multi-user web app / auth / hosting
- Perfect contamination immunity
- Line-diff equality with gold patches as the only success criterion

## Core concepts

| Term | Meaning |
|------|---------|
| **Scenario** | Fixed task: base repo state, prompt, optional gold ref, acceptance |
| **Config** | Treatment: harness@version + model(+version) + workflow + tools + budget |
| **Run** | One execution of (scenario × config), possibly with a repeat index |
| **Artifact** | Patch, logs, telemetry from a run |
| **Judge** | Offline scorer of an artifact (tests, rubric LLM, metrics) |
| **Result** | Immutable scored record |
| **Report** | Static HTML comparing results |

## Experiment principles

1. **Lock what you are not testing.** To test sub-agents vs single agent, fix harness@version + model + budget; vary only workflow.
2. **Always include a baseline.** Same harness@version + model, minimal workflow: “implement this.”
2b. **Pin harness version, not just family.** “Claude Code” or “Grok” is not a treatment — `claude_code@x.y.z` is. Silent CLI/product updates can flip quality without any model change. Capture **declared** pins on the config and **resolved** versions on the run when detectable.
2c. **Skill versions are deferred.** v0 records skill *names* only; pin skill versions later if skills prove as volatile as harnesses.
3. **Prompt is intent, not gold.** Agents see issue/changelog-derived briefs, never the gold patch.
4. **Gold is reference, not the only answer.** Prefer tests + behavior + review rubric.
5. **Independent judges.** Scoring runs outside the system under test.
6. **Cost is first-class.** Tokens, wall time, estimated $ sit next to quality.
7. **Repeats for variance.** N≥2–3 when claiming a workflow wins.

## Scenario types

| Type | Source of truth | Why |
|------|-----------------|-----|
| `bugfix` | GitHub issue + failing tests + fix PR | Isolated, SWE-bench-like, sharp oracle |
| `feature` | Changelog/release notes between tags | Matches “implement these features” workflows |
| `refactor` | PR that preserves behavior | Tests architecture / restraint |

v0 prioritizes **bugfix** first (cleaner oracle), then **feature** deltas.

## Contamination strategy (pragmatic)

Popular OSS is often in pretraining. We accept residual risk and mitigate:

- Prefer **recent** issues/tags when possible
- Record `contamination_risk: high|medium|low` on scenarios
- Flag patches that are near-identical to gold (possible memorization)
- Mix languages / less-famous repos over time
- Treat results as **relative config ranking**, not absolute capability claims
- Future: private or post-cutoff fixtures

## Fair comparison modes

| Mode | Use when |
|------|----------|
| **Controlled** | Scientific isolation: same budget, tool surface as close as possible |
| **Ecological** | “How a power user would actually use this harness” |

Reports should label which mode was used.

## Versioning rules

| Component | Pin in config? | Why |
|-----------|----------------|-----|
| Harness family (`claude_code`, `grok`, `codex`) | yes (`harness`) | Which product |
| Harness version | **yes** (`harness_version`) | Product updates change prompts, tools, defaults |
| Model id | yes (`model`) | Include dated/build suffix when the provider has one |
| Model version extra | optional (`model_version`) | When model is a family alias |
| Skills | name only for now | Defer skill version pins until needed |
| Tools | name list | Adapter can later record tool schema versions |

On ingest/automation, also store:

- `resolved_harness_version` — what `cli --version` actually reported
- `resolved_model` — what the harness said it used

A mismatch between declared and resolved is a yellow flag on the run.

## Architecture

```
scenarios/          # frozen task definitions (YAML + optional assets)
configs/            # treatment recipes (YAML)
adapters/           # harness-specific runners (codex, claude, grok, …)
hb/                 # Python package: models, runner, judges, report
results/            # immutable run JSONL / directories
reports/            # generated static HTML
```

### Data flow

```
scenario + config
    → adapter (isolated worktree / future container)
    → artifact (diff, logs, telemetry)
    → judges (auto tests + optional LLM rubric)
    → result record
    → static HTML report
```

### Adapter interface (v0)

Each adapter must:

1. Materialize workspace at `base_ref`
2. Inject the scenario prompt (and only allowed context)
3. Invoke the harness/CLI with config knobs
4. Stream/capture events until done or budget exhausted
   (`hb execute` enforces config.budget: max_minutes/max_usd/max_turns/max_tokens;
   soft warn at 80%, hard kill process group at limit → status `budget_exceeded`)
5. Emit a `RunArtifact` (patch + metrics + raw log paths);
   static reports deep-link into `results/<run_id>/` artifacts

v0 may start with a **manual adapter**: human pastes the prompt into a harness,
then `hb ingest` records the patch and metrics. Automated adapters follow once
the data model is stable.

## Judging (v0)

**Automatic (required when available)**

- apply patch cleanly?
- build / install?
- existing tests pass?
- scenario `test_commands` pass?
- patch stats (files, lines)

**Optional LLM judge (separate model)**

- completeness vs prompt
- architecture fit
- over/under-engineering
- merge-readiness (1–5)

**Cost**

- wall_ms, tokens_in/out, estimated_usd, turns, tool_calls

Composite scores stay simple and documented in the report.

## Report

No server. `hb report` writes a self-contained HTML file (tables, comparisons,
links to patches). Good enough to open in a browser and later publish as
GitHub Pages if desired.

## Open questions (deferred, not blocking)

See [WISHLIST.md](./WISHLIST.md) for the living backlog. Highlights:

- Formal multi-harness adapter package (beyond launch-string configs)
- Whether Docker is needed before multi-language expansion
- Optional LLM rubric judge
- Stats across repeats; publish reports (e.g. GitHub Pages)

## Implementation order

1. Schemas + validate CLI + example fixtures ✅
2. Manual run ingest + result store ✅
3. Static HTML report ✅
4. `hb run` / `hb finish` / `hb experiment` loop ✅
5. Auto test judge + gold FAIL_TO_PASS overlay ✅
6. Headless execute (pi / Grok launch strings + telemetry) ✅
7. Budget guards, stakeholder proxy/human, fingerprints, snapshots ✅
8. Experiment report + artifact deep links ✅
9. Optional LLM judge · Docker · more adapters · feature scenarios — see wishlist

## CLI run lifecycle

```text
hb run -s SCENARIO -c CONFIG
    → workspaces/instances/<scenario>__<run_id>/  @ base_ref
    → results/<run_id>/{run.json,snapshot.json} (pending)

hb execute <run_id>          # preferred: headless + budget + auto-finish
    → agent.log, patch, judges, telemetry

# or manual agent, then:
hb finish <run_id>
    → capture git diff vs base_ref
    → overlay gold test files, run acceptance.test_commands
    → results/<run_id>/{patch.diff,judge.json,run.json}

hb experiment --from experiments/foo.yaml
    → matrix of pending runs (skips completed fingerprints)

hb report [--from experiments/foo.yaml]
    → reports/latest.html or reports/exp-<id>.html
```

User-facing docs: [README.md](./README.md), walkthroughs: [docs/EXAMPLES.md](./docs/EXAMPLES.md).
