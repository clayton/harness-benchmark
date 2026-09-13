# Agent Rodeo

`hbench` runs repeatable coding-agent benchmarks on your machine. It records
the scenario, setup, patch, tests, telemetry, and a local HTML report. Nothing
is uploaded until you run `hbench publish`.

## Install and inspect

Install the pinned CLI, then check what is available:

```bash
curl -fsSL https://agentrodeo.dev/install.sh | sh
hbench version
hbench doctor
```

The installer verifies the platform binary against a SHA-256 value pinned in the script and fails closed on download or checksum errors.

`doctor` reports installed harnesses, detected skills, and scenario
prerequisites. It does not install tools.

## Run your own setup

Use a saved profile when you want to try a model, reasoning level, workflow,
or local skill tree more than once:

```bash
hbench setup save my-pi \
  --harness pi \
  --provider openrouter \
  --model openrouter/z-ai/glm-5.3-flash \
  --reasoning high \
  --workflow plan-first \
  --skill-dir ./skills/review

hbench setup list
hbench setup show my-pi
```

The profile is stored under the hbench data directory. `personal` is the
default for `setup save`; use `--mode clean-baseline` for an isolated baseline
recipe. A personal profile records the selected values. Adapter support still
determines which values are enforced. Personal Codex runs snapshot
`~/.codex/config.toml` by digest and use that frozen file inside an isolated
`CODEX_HOME`; other global files are not copied.

Run a published scenario with that setup through the native compatibility path:

```bash
hbench run -s rodeo:js-commander-negative-exp-E@3 \
  --setup my-pi --runtime native
```

The command prints a run ID and workspace. For a headless harness, continue
with:

```bash
hbench execute RUN_ID
hbench report
```

For manual work, edit the printed workspace and run `hbench finish RUN_ID`.
Use `hbench list runs` to recover IDs and next actions.

`--skill-dir PATH` accepts a local Pi skill directory. hbench copies it into
the run directory before execution and records a SHA-256 over its relative
paths and file contents. The frozen hash is included in the publish preview.

## Run the clean OCI baseline

Published scenarios with digest-pinned OCI environments can run without a
local language runtime or harness:

```bash
hbench ride -s rodeo:js-commander-negative-exp-E@3 \
  --harness pi --model openrouter/z-ai/glm-5.3-flash \
  --runtime auto --approve-spend
```

`auto` selects Docker, Podman, or nerdctl. OCI rides use the sealed clean
baseline. Use native `hbench run` for personal models, workflows, or skills.
The default relay cap is **$1 per ride**; `--max-usd` changes that hard relay
boundary. Model execution requires `--approve-spend`.

## Compare setups and share findings

Arbitrary local harnesses can use a validated `hb.adapter.v1` manifest:

```bash
hbench adapter validate ./adapter.yaml
hbench run -s ./task.yaml --adapter ./adapter.yaml
```

The manifest is hashed into the run setup and launches its command as an argv array, never through a shell.

Create a study from saved setup profiles:

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

Public `hb.study.v2` Studies may use personal profiles, local-skill hashes,
external adapters, and immutable local `hb.task.v1` scenarios. The generated
public contract strips machine paths and secrets; hbench keeps any local skill
path mapping in a mode-0600 sidecar under `hb-out/studies`. Private v1 Studies
remain supported. Studies freeze the question, scenarios, arms, changed axes,
repeats, seed, judge protocol, and budgets. Study execution is local,
sequential, resumable, and costs model tokens. A Study dollar threshold is
checked after each run and
can overshoot by one run; it is a post-run stop threshold. The OCI `--max-usd`
value is a relay-enforced hard cap for each request.

Review the generated report before sharing it:

```bash
hbench publish --preview RUN_ID
hbench publish RUN_ID
```

Publishing is explicit. hbench signs each Open payload with an origin-scoped
Ed25519 publisher key and uploads a bounded `patch.diff` when present; raw
agent logs stay local. Open Range runs are community evidence and never change
the official Rodeo Rating. Run pages and Callouts show execution assurance
(Declared, Self-signed, or Approved-runner verified) separately from
independent replication. A Callout or Study publication freezes the contract
so other riders can reproduce it.

## Contribute a scenario

Choose one small task with a clear prompt and an executable acceptance test.
Use a public GitHub repository with immutable base and target commits, or use a
scaffold scenario when the task starts from authored files.

Validate a local manifest before asking the community site to review it:

```bash
go test -mod=mod ./internal/corpus
hbench scenario validate ./scenarios/my-task.yaml
hbench run -s ./path/to/scenario.yaml --harness manual
```

The first command checks the authoring helpers. The second exercises the
scenario through the same trust, fetch, workspace, and judge path used for a
ride. Do not put the target commit or hidden evaluator in the agent prompt.
Scenario versions are immutable: change the version when environment or
dependency metadata changes.

Submit the proposal through Agent Rodeo's Community Scenarios page. The site
checks repository access, commit ancestry, the manifest, and the public
execution surface. A proposal is not an official rating result until a trusted
Controlled run validates it.

## What is tested

There are two different test layers:

* **Benchmark acceptance tests** belong to each scenario. hbench runs them
  after the agent changes the workspace; they judge the agent's patch.
* **Repository tests** check hbench itself. Run focused Go tests under
  `harness-benchmark`, and focused Rails tests under `agentrodeo` when changing
  the web application. They do not measure an agent setup.

Useful local checks are:

```bash
go test -mod=mod ./cmd/... ./internal/...
(cd ../agentrodeo && bundle exec rspec)
```

The Rails command is only for the `agentrodeo` directory. Tests that fetch
external repositories or contact a model provider need the relevant network
and credential access.

## Command reference

```text
hbench doctor [-s SCENARIO]
hbench setup save ID [flags]
hbench setup list
hbench setup show ID
hbench scenario new|validate [flags]
hbench run -s SCENARIO [--setup ID | --harness NAME] [flags]
hbench ride -s SCENARIO --harness pi --model MODEL --approve-spend
hbench execute [RUN_ID]
hbench finish [RUN_ID]
hbench report
hbench publish [--preview] [RUN_ID]
hbench study init|validate|plan|run|status|report|publish STUDY.yaml
hbench adapter validate ADAPTER.yaml
hbench reproduce CALLOUT_URL
hbench inspect -s SCENARIO
hbench trust -s SCENARIO
hbench skill install --target DIR
```

See [docs/EXAMPLES.md](docs/EXAMPLES.md) for runnable recipes, [the study
guide](docs/STUDIES_AND_CALLOUTS.md) for contract details, and
[scenarios/README.md](scenarios/README.md) for authoring rules.
