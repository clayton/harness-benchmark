# Ox Alpha study retry results

## Status

**PUBLISHED AS 6 OPEN RANGE RUNS — all cells completed and passed; Study/Callout publication is blocked by a legacy scenario-ID contract mismatch.**

- Study: `ox-alpha-vs-gpt-5-6-sol-go-chi-20260822-r2`
- Contract: `18c0a806d4d7ffd6262f2f3039108b4d0cc5fbb9904d307e4bf69a041ff03a67`
- Runner: `hbench 0.5.5 (go)`
- Harness: Pi `0.84.2`
- Scenario: `go-chi-tee-bytes-double-count`
- Status command: `6/6 runs complete`

## Run results

| Run ID | Model | Repeat | Result | Judges | Total tokens | Estimated cost | Cost complete | Wall time |
|---|---|---:|---|---|---:|---:|---|---:|
| `0428a52cb3a2` | `stealth/ox-alpha` | 1 | Pass | 3/3 pass | 14,348 | $0.00866515 | No | 43.354 s |
| `2a05d2318bde` | `stealth/ox-alpha` | 2 | Pass | 3/3 pass | 13,762 | $0.00917584 | No | 45.746 s |
| `1f6d50034c4e` | `stealth/ox-alpha` | 3 | Pass | 3/3 pass | 8,747 | $0.00649042 | No | 36.125 s |
| `d98066b9e8ef` | `openai/gpt-5.6-sol` | 1 | Pass | 3/3 pass | 12,809 | $0.03487175 | No | 50.351 s |
| `a7a792b78895` | `openai/gpt-5.6-sol` | 2 | Pass | 3/3 pass | 15,204 | $0.03804650 | No | 58.089 s |
| `841562d86356` | `openai/gpt-5.6-sol` | 3 | Pass | 3/3 pass | 15,351 | $0.03962075 | No | 73.833 s |

Each run passed `non_empty_patch`, `gold_test_overlay`, and `acceptance_tests`. The acceptance command was `go test ./middleware/ -count=1`.

## Arm summary

| Model | Pass rate | Median tokens | Median wall time | Estimated cost total | Mean estimated cost |
|---|---:|---:|---:|---:|---:|
| `stealth/ox-alpha` | 3/3 (100%) | 13,762 | 43.354 s | $0.02433141 | $0.00811047 |
| `openai/gpt-5.6-sol` | 3/3 (100%) | 15,204 | 58.089 s | $0.11253900 | $0.03751300 |

Recorded wall time totaled 125.225 s for Ox Alpha and 182.273 s for GPT-5.6 Sol.

## Published runs

| Model | Repeat | Public run |
|---|---:|---|
| `stealth/ox-alpha` | 1 | https://agentrodeo.dev/runs/26 |
| `stealth/ox-alpha` | 2 | https://agentrodeo.dev/runs/27 |
| `stealth/ox-alpha` | 3 | https://agentrodeo.dev/runs/28 |
| `openai/gpt-5.6-sol` | 1 | https://agentrodeo.dev/runs/29 |
| `openai/gpt-5.6-sol` | 2 | https://agentrodeo.dev/runs/30 |
| `openai/gpt-5.6-sol` | 3 | https://agentrodeo.dev/runs/31 |

All six public records report quality `1.0`, Pi `0.84.2`, Open Range evidence, and incomplete cost evidence. The failed infrastructure run `20a034dbaebc` was not published.

The immutable Study and Callout were not published. Agent Rodeo rejected the CLI-valid frozen contract because its scenario uses the legacy local ID `go-chi-tee-bytes-double-count` instead of `rodeo:go-chi-tee-bytes-double-count@1`. Rewriting that ID after execution would change the contract digest and sever the run bindings.

## Limitations

This is one easy Go bug-fix scenario with three repeats per model. It does not establish performance on other tasks. All records have `token_complete: true`, but `complete: false` and `cost_kind: estimated`; therefore token comparisons are available, while billed-cost or general cost-efficiency claims are not supported. `stealth/ox-alpha` is an opaque model alias. Pi requested and configured OpenRouter high reasoning, and OpenRouter confirmed that `stealth/ox-alpha` was served by `Stealth`, but it reported zero native reasoning tokens. The evidence does not prove that Ox Alpha produced hidden reasoning.

## Scoped claim

Using Pi 0.84.2 with OpenRouter high reasoning requested and configured on the chi Tee byte-count bug, both `stealth/ox-alpha` and `openai/gpt-5.6-sol` passed 3 of 3 runs. Ox Alpha matched GPT-5.6 Sol on pass rate and had lower median recorded wall time (43.354 s versus 58.089 s). Estimated costs were lower for Ox Alpha, but telemetry is not cost-complete, so no billed-cost claim is warranted.
