# SOL-supervised Muse versus direct Muse on a hard ZIP rewrite

## Result

Direct Muse achieved exact evaluator success in 2 of 3 repeats. The
SOL-supervised Muse workflow achieved exact evaluator success in 1 of 3
repeats.

This is exploratory Open Range evidence from one hard task. It is published as
self-signed evidence, has not been independently replicated, and does not
affect the Rodeo Rating.

## Study

- Callout: [direct Muse versus SOL-supervised Muse](https://agentrodeo.dev/callouts/on-the-frozen-zip-password-finder-python-port-task-direct-muse-s-ec8af1)
- Public Study ID: `sol-muse-hard-zip-public-v2-2026-09-13`
- Public contract digest: `d7d08b82f63f02ffaa4c73c81374fb5eaa568eaa581f876f9121fbcef0170c02`
- Verified source Study ID: `sol-muse-hard-zip-private-v2-2026-09-13`
- Verified source contract digest: `92f9c80b2bd2e1a91e84bf83c7058d92e8679e2121aae3ed2b7755776dbe8e96`
- Comparison mode: ecological
- Task: rewrite zip-password-finder v0.11.1 from Rust to standard-library Python
- Evaluator: 18 reference-equivalence assertions
- Repeats: 3 per arm
- Timeout: 45 minutes per run
- Observed-estimate stop: $20 total; one run could overshoot
- Order: seeded randomization with seed `91357`

## Arms

### Direct Muse

- Pi 0.85.1
- `meta/muse-spark-1.3-contributor`
- High reasoning
- No plugins, skills, or child-agent delegation

### SOL-supervised Muse

- Pi 0.85.1
- Parent: `openai-codex/gpt-5.6-sol`, high reasoning
- Plugin: `pi-subagents@0.67.0`
- Frozen local orchestration skill digest:
  `fc65aaa43afbd0ce7a8595b5a985dd0115243f0843faa8d7ef2fa7761488780d`
- Every recorded child used
  `meta/muse-spark-1.3-contributor:high`
- SOL reviewed diffs and checks while Muse workers performed mutations

## Runs

| Arm | Repeat | Run | Status | Assertions | Exact success | Wall time | Recovered tokens | Recovered estimated cost |
| --- | ---: | --- | --- | ---: | --- | ---: | ---: | ---: |
| Direct Muse | 1 | [`7fdc744b1caf`](https://agentrodeo.dev/runs/52) | timeout | 18/18 | yes | 45.0m | 6,999,865 | $0.0796 |
| Direct Muse | 2 | [`d49ff96974ee`](https://agentrodeo.dev/runs/49) | timeout | 1/18 | no | 45.0m | 1,804,496 | $0.0203 |
| Direct Muse | 3 | [`7b64bf6fd559`](https://agentrodeo.dev/runs/51) | timeout | 18/18 | yes | 45.0m | 10,151,881 | $0.0659 |
| SOL + Muse | 1 | [`f44007a930b9`](https://agentrodeo.dev/runs/48) | timeout | 17/18 | no | 45.0m | 9,158,531 | $0.3154 |
| SOL + Muse | 2 | [`93baab1921e6`](https://agentrodeo.dev/runs/53) | completed | 18/18 | yes | 40.4m | 27,605,451 | $1.1907 |
| SOL + Muse | 3 | [`1254d6df087c`](https://agentrodeo.dev/runs/50) | failed | 14/18 | no | 28.3m | 9,856,955 | $2.4896 |

The timeout status and exact evaluator result are separate. Two direct Muse
runs reached 18/18 before the harness terminated the still-running agent at the
45-minute boundary. Their patches passed when hbench applied the evaluator.

## Aggregate interpretation

- Exact reliability favored direct Muse: 2/3 versus 1/3.
- Direct Muse passed 37 of 54 assertion opportunities (68.5%).
- SOL + Muse passed 49 of 54 assertion opportunities (90.7%).
- Direct Muse produced a non-empty patch in 2/3 runs.
- SOL + Muse produced a non-empty patch in 3/3 runs.
- SOL + Muse used 15 recorded Muse child runs across its three repeats:
  4, 6, and 5 children in repeat order 1, 2, and 3.
- The successful orchestrated repeat used six Muse children. All six completed
  without a recorded child error.

The orchestrated workflow was more consistent at producing a substantial,
near-complete implementation, but it was less reliable at reaching perfect
acceptance within this sample. Under the study's reliability-first win rule,
direct Muse is the descriptive winner.

## Time, tokens, and cost

The six valid cells accumulated 4 hours, 8 minutes, and 40 seconds of model
wall time. Direct Muse used 2 hours and 15 minutes. SOL + Muse used 1 hour, 53
minutes, and 40 seconds; concurrent child work remains inside each parent
run's wall clock. Setup added about 1 minute and 26 seconds across the matrix.

Direct Muse used 18,956,242 total tokens. The SOL parents reported 19,487,802
tokens, and the 15 preserved Muse child records add 27,133,135 tokens. The
recovered orchestration total is therefore 46,620,937 tokens. The full valid
matrix used 65,577,179 tokens.

Direct Muse's catalog-estimated cost was $0.1657. The SOL parents' estimated
cost was $3.7272, and the Muse children add $0.2686. The recovered orchestration
estimate is $3.9957, and the full valid matrix estimate is $4.1614.

The excluded preliminary model attempts logged another 1,478,867 tokens,
$0.6518 in estimated cost, and 25 minutes 54 seconds of wall time. One partial
direct-Muse attempt was interrupted before telemetry was saved. The entire
exercise therefore has a known lower bound of 67,056,046 tokens, $4.8132, and
4 hours 34 minutes 34 seconds, plus that unmeasured partial attempt.

These are Pi model-catalog estimates, not billed amounts. The original hbench run payloads did not aggregate child usage. hbench 0.7.2
recovered the preserved `pi-subagents` metadata before publication, so the
published token totals include the recorded children. Cost evidence remains
incomplete because these values are catalog estimates rather than comparable
billed totals; the Study cannot support an efficiency title.

## Protocol incidents and exclusions

Before the valid private Study, an attempted public `hb.study.v2` contract
produced invalid evidence and was excluded:

- two setup-failed records occurred before model execution because the embedded
  public task omitted the Cargo dependency fetch context;
- one direct Muse run and one SOL parent run then executed, but the reconstructed
  task omitted the evaluator overlay;
- the SOL run's Muse children also failed authentication in the isolated child
  environment.

The private Study fixed those infrastructure conditions before its matrix ran.
Its first direct-Muse cell was later interrupted by the operator while still
running. No process or patch remained, so the exact run ID was executed again
and then judged. This resume overwrote the partial agent log and is a protocol
irregularity. Keep the published result exploratory.

Some patches also captured generated `__pycache__` files or an extra local test
file. The acceptance evaluator did not score output hygiene separately.

## Conclusion

On this one hard source-port task, three repeats did not support the claim that
SOL orchestration improved exact completion reliability. Direct Muse won 2-1
on full evaluator success. SOL orchestration improved partial assertion
coverage and patch consistency, but two of its three runs stopped short of
perfect behavior.

The public contract now embeds the bounded evaluator and dependency-fetch
provenance, and publication includes recovered child usage. Repeat the
comparison on multiple hard tasks before making broader claims.
