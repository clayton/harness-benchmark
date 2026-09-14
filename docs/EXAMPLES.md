# hbench examples

These commands use the Go `hbench` CLI. Results stay local until you explicitly
publish them. Replace `RUN_ID` with the ID printed by hbench.

## Personal setup

Save a repeatable recipe for a model, reasoning level, workflow, and local Pi
skill:

```bash
hbench setup save review-pi \
  --harness pi \
  --provider openrouter \
  --model openrouter/z-ai/glm-5.3-flash \
  --reasoning high \
  --workflow plan-first \
  --skill-dir ./skills/review
hbench setup show review-pi
hbench run -s rodeo:js-commander-negative-exp-E@3 --setup review-pi --runtime native
hbench execute RUN_ID
hbench report
```

The local skill directory is copied and hashed for that run. Personal profiles
record selected values; adapter support determines which values are enforced.

## Clean baseline

Use the sealed OCI path when the scenario publishes digest-pinned images:

```bash
hbench ride -s rodeo:js-commander-negative-exp-E@3 \
  --harness pi --model openrouter/z-ai/glm-5.3-flash \
  --runtime auto --approve-spend
```

The OCI relay has a default **$1 hard cap per ride**. `--max-usd` changes that
cap. The command requires `--approve-spend` because it invokes a model.

## Manual run

```bash
hbench run -s rodeo:js-commander-negative-exp-E@3 --harness manual
# edit the printed workspace
hbench finish RUN_ID
hbench report
```

## Compare setups

```bash
hbench study init \
  --question "Does the review skill improve pass rate?" \
  --scenario rodeo:js-commander-negative-exp-E@3 \
  --setup clean-pi --setup review-pi \
  --visibility private \
  --out review-study.yaml
hbench study validate review-study.yaml
hbench study plan review-study.yaml
hbench study run review-study.yaml --approve-spend
hbench study report review-study.yaml
```

`study run` is sequential, resumable, and local. Study dollar limits are
post-run stop thresholds and may overshoot by one run. They do not replace the
OCI relay hard cap.

Use `--visibility public` to create an `hb.study.v2` contract. Public Studies
may use personal profiles, local skill content hashes, external adapters, and a
local scenario with a public immutable repository. hbench embeds that scenario
as `hb.task.v1`, including bounded evaluator files and lockfile-pinned fetch
declarations, strips machine paths and secrets from the contract, and keeps
creator-side scenario and skill path mappings in a private
`hb-out/studies/*.inputs.json` sidecar. Use `--visibility private` when the
Study itself must not be uploaded. To recover an already completed private
workaround without paid reruns, use
`hbench study promote PRIVATE.yaml --out PUBLIC.yaml` before creating the
Callout.

## Publish a finding

Preview the privacy-filtered payload before uploading:

```bash
hbench publish --preview RUN_ID
hbench publish RUN_ID
```

Publishing is separate from running. Open Range evidence is discoverable but
does not alter the official Rodeo Rating.

## Controlled execution

Registered operators with a private evaluator pack use the controlled path:

```bash
hbench controlled run \
  --scenario rodeo:slug@version \
  --pack ./evaluator \
  --key-id RUNNER_KEY \
  --runtime auto \
  --approve-spend
```

This is an operator workflow. It requires a registered key, digest-pinned
images, and a private evaluator pack.
