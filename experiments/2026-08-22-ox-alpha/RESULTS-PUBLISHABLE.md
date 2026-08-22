# Ox Alpha publishable study results

## Status

**PUBLISHED — all 6 frozen-contract cells completed, passed, and are linked to one Self-published Agent Rodeo Study.**

- Study: `ox-alpha-vs-gpt-5-6-sol-go-chi-20260822-r3`
- Contract: `ccfb6803024947666e7587cc4119dfdd20a185a2790385922f43f56858d5a519`
- Scenario: `rodeo:go-chi-tee-bytes-double-count@1`
- Scenario digest: `808f9b936e9ddab987c4062da65533d7a50f7b962552aebfb18cb9d29dd9e6c5`
- Runner: `hbench 0.5.6 (go)`
- Harness: Pi `0.84.2`
- Provider: OpenRouter
- Reasoning setting: high requested and configured
- Study status: 6/6 runs complete

## Run results

| Run ID | Model | Repeat | Result | Judges | Total tokens | Reasoning tokens | Estimated cost | Wall time |
|---|---|---:|---|---|---:|---:|---:|---:|
| `1f912cf72625` | `stealth/ox-alpha` | 1 | Pass | 3/3 pass | 20,870 | 0 | $0.000000 | 62.327 s |
| `d044543706b5` | `stealth/ox-alpha` | 2 | Pass | 3/3 pass | 18,059 | 0 | $0.000000 | 75.696 s |
| `7f004fb1d2d3` | `stealth/ox-alpha` | 3 | Pass | 3/3 pass | 11,586 | 0 | $0.000000 | 36.577 s |
| `5fa16f4acbc3` | `openai/gpt-5.6-sol` | 1 | Pass | 3/3 pass | 14,868 | 48 | $0.021334 | 66.967 s |
| `1f332a005049` | `openai/gpt-5.6-sol` | 2 | Pass | 3/3 pass | 14,616 | 33 | $0.020401 | 71.300 s |
| `20ac4943a9f3` | `openai/gpt-5.6-sol` | 3 | Pass | 3/3 pass | 12,380 | 40 | $0.017628 | 56.332 s |

Every run passed `non_empty_patch`, `gold_test_overlay`, and `acceptance_tests`. The acceptance command was `go test ./middleware/ -count=1`. All patches removed the duplicate Tee-path byte increment without changing the non-Tee path.

## Arm summary

| Model | Pass rate | Median total tokens | Median wall time | Total estimated cost | Median estimated cost |
|---|---:|---:|---:|---:|---:|
| `stealth/ox-alpha` | 3/3 (100%) | 18,059 | 62.327 s | $0.000000 | $0.000000 |
| `openai/gpt-5.6-sol` | 3/3 (100%) | 14,616 | 66.967 s | $0.059363 | $0.020401 |

Recorded wall time totaled 174.600 seconds for Ox Alpha and 194.599 seconds for GPT-5.6 Sol.

## Provenance and telemetry

All six records match the frozen Study ID, contract, scenario digest, base ref, gold ref, Pi version, provider, model arm, high reasoning setting, and repeat assignment. No run log contains Pi's custom-model warning.

All six records report `complete: true` and `token_complete: true`. Each has a non-empty price snapshot captured at the same timestamp. The Ox Alpha snapshot records zero rates for input, output, cache read, and cache write. The GPT-5.6 Sol snapshot records per-million-token rates of $2.50 input, $15.00 output, $0.25 cache read, and $3.125 cache write. Costs remain labeled `estimated`, so they are snapshot-based estimates rather than billed amounts.

Ox Alpha reported zero native reasoning tokens in all three runs. The evidence proves that Pi/OpenRouter high reasoning was requested and configured. It does not prove that Ox Alpha produced hidden reasoning.

## Scoped claim

Using Pi 0.84.2 with OpenRouter high reasoning requested and configured on `rodeo:go-chi-tee-bytes-double-count@1`, `stealth/ox-alpha` and `openai/gpt-5.6-sol` each passed 3 of 3 runs. Ox Alpha matched GPT-5.6 Sol on pass rate and had a lower median recorded wall time, 62.327 seconds versus 66.967 seconds. Under the frozen price snapshots, estimated total cost was $0 for Ox Alpha and $0.059363 for GPT-5.6 Sol. These are estimates, not billed amounts.

## Publication

- Callout and Study: https://agentrodeo.dev/callouts/using-pi-0-84-2-with-openrouter-high-reasoning-requested-and-con-6d93d0
- Evidence badge: `Self-published`
- Linked matrix: 6 runs

## Limitations

This result covers one easy Go bug-fix scenario with three repeats per model. It does not establish performance on other tasks. The Ox Alpha alias remains opaque, so the result applies to the model served through that alias for this frozen contract. Zero reported reasoning tokens do not show whether Ox Alpha performed unreported internal reasoning.
