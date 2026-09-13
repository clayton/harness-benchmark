# Pi model shootout — 2026-09-12

## Result

The final study completed all 9 cells. All three models produced behaviorally correct fixes on all three tasks.

There is no overall quality winner in this one-repeat shootout. Muse Spark was fastest by wall time, Composer reported the fewest tokens, and Luna was the only arm with complete cost telemetry. Composer does not expose a reasoning control, so its `high` setting was recorded by the contract but not applied by the model.

| Model | Behavioral passes | Mean hbench quality | Total wall time | Median wall time | Reported tokens | Reported estimated cost | Telemetry complete |
|---|---:|---:|---:|---:|---:|---:|---|
| Muse Spark 1.3 Contributor | 3/3 | 0.889 | 73.950s | 28.849s | 385,211 | $0.006163 | No |
| Cursor Composer 2.5 | 3/3 | 0.889 | 87.791s | 29.230s | 57,507 | $0.000000 | No |
| GPT-5.6 Luna | 3/3 | 0.889 | 117.128s | 42.180s | 295,191 | $0.020406 | Yes |

Composer's zero-dollar value is incomplete telemetry, not proof of zero cost. Muse's price estimate also lacks complete hbench telemetry. Neither can receive an efficiency title.

## Task results

| Scenario | Muse Spark | Composer | Luna |
|---|---:|---:|---:|
| `go-chi-tee-bytes-double-count@1` | 1.00, pass | 1.00, pass | 1.00, pass |
| `js-commander-negative-exp-E@3` | 1.00, pass | 1.00, pass | 1.00, pass |
| `python-pytest-approx-inf-rel@2` | 0.67, acceptance pass | 0.67, acceptance pass | 0.67, acceptance pass |

The pytest scenario applied zero gold test files, which capped every arm at 0.67 even though the acceptance suite passed. A read-only follow-up executed the missing regression behavior in each workspace; all three correctly raised `ValueError` for an infinite relative tolerance.

## Final study contract

- Study: `pi-model-shootout-final-v2-2026-09-12`
- Contract digest: `95148f990c4629170cded804fcac5e90a631b713c432073528e1e91709ee506e`
- Harness: Pi 0.85.1
- hbench: 0.5.8 at `/opt/homebrew/bin/hbench`
- Comparison: controlled; provider and model varied
- Reasoning: `high` requested for every arm; unsupported by Composer 2.5
- Workflow: baseline
- Skills: none
- Provider adapter: `pi-cursor-sdk@0.3.6`, fixed across all arms
- Repeats: 1
- Timeout: 30 minutes per run
- Post-run spending threshold: $25; not reached
- Publication: final run records published as Open Range evidence

Manifest: `study-final-v2.yaml`

## Final run IDs

| Run ID | Model | Scenario |
|---|---|---|
| `35b6b3d88aec` | Muse Spark | Go/chi |
| `eb4f6b212446` | Muse Spark | JavaScript/Commander |
| `1063bc3aee7b` | Muse Spark | Python/pytest |
| `18f137d3e362` | Composer | Go/chi |
| `59641ca29a51` | Composer | JavaScript/Commander |
| `cfa4953c77a6` | Composer | Python/pytest |
| `bd608b7367c4` | Luna | Go/chi |
| `83c2189a5335` | Luna | JavaScript/Commander |
| `74c6c2547e3c` | Luna | Python/pytest |

## Published run URLs

- Muse Spark: [Go/chi](https://agentrodeo.dev/runs/39), [JavaScript/Commander](https://agentrodeo.dev/runs/40), [Python/pytest](https://agentrodeo.dev/runs/41)
- Composer: [Go/chi](https://agentrodeo.dev/runs/42), [JavaScript/Commander](https://agentrodeo.dev/runs/43), [Python/pytest](https://agentrodeo.dev/runs/44)
- Luna: [Go/chi](https://agentrodeo.dev/runs/45), [JavaScript/Commander](https://agentrodeo.dev/runs/46), [Python/pytest](https://agentrodeo.dev/runs/47)

The Study contract is published to the existing [Callout](https://agentrodeo.dev/callouts/on-three-focused-public-coding-regressions-in-pi-muse-spark-1-3--cccff9). Its nine historical Open Range runs are declared evidence from one publisher, so the Callout is not independently replicated.

## Excluded attempts

hbench retained every earlier run for diagnosis.

- Initial matrix, excluded because Muse could not authenticate and the `got` scenario failed on a shared Node 26 lint incompatibility: `3fb98ed4d473`, `16df712d8a4b`, `728e10346fba`, `720cca1a5118`, `fd4cc831112a`, `2794e5d13e41`, `5f01fc3f44f2`, `c2bcd7446ca4`, `bf1ce8b1a11d`.
- Four-task attempt, excluded because Rails setup could not link the locked `mysql2` gem against `zstd`: `0ef366d88ba2`, `45cd940f3e16`, `8a20623ed903`, `287919e0afb8`, `661c8154e66f`, `7cd0128f5156`, `669d02562f31`, `118aee1de795`.
- First three-task retry, excluded because 1Password auth expired inside hbench's isolated environment: `39b80e49fd15`, `2998706998fd`, `fcd910fb71f3`, `cb6632abb23c`, `09c25d944f9c`, `93b35229c48e`, `990d2166946f`, `213d2d004320`, `d781af68fbef`.

The final run used a permission-`600` temporary Meta key file. The shell trap removed it after hbench exited. No credentials were written to the study manifest or report.
