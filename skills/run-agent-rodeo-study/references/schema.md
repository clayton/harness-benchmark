# hb.study.v2

Required fields are `schema`, `id`, `question`, `comparison_mode`, `scenarios`, `arms`, `varied_axes`, `repeats`, `seed`, `judge_protocol`, `win_rule`, and `budget.max_minutes_per_run`.

`win_rule` is currently the fixed identifier `callout-title-v1`. It ranks reliability first, then quality, then complete cost and token efficiency. Existing `hb.study.v1` contracts remain valid.

A public Study scenario can use either:

- `rodeo:slug@version` plus the 64-character `manifest_digest` returned by the public manifest; or
- an embedded, immutable `hb.task.v1` object derived from a local scenario with a public HTTPS repository and fixed 40- or 64-character commit.

The task digest covers canonical task JSON. Task contracts reject unknown fields, credentials, private hosts, machine paths, unbounded commands, excessive nesting, and oversized strings or collections. They contain only public setup and acceptance commands; hidden evaluator material and target patches are never embedded.

Each v2 arm names its `mode`, harness, exact `harness_version`, model, and any provider, model version, reasoning level, workflow, skills, extensions, plugins, tools, subagent topology, environment, network policy, prompt treatment, adapter, and enforcement assurance. Local skill directories appear in the public contract only as content hashes. hbench stores their machine paths separately in a mode-0600 `hb-out/studies/*.inputs.json` sidecar. The harness version must match the installed harness `--version` output.

External adapters use `hb.adapter.v1`. Their command is an argument array with only `${prompt}`, `${prompt_file}`, and `${workspace}` placeholders. Shell interpreters are rejected. The manifest and version are hashed into the setup.

Scenario manifests can declare command prerequisites under `requirements.commands` and approved input classes under `fetches`. A ride never installs prerequisites. `hbench doctor -s <scenario>` reports missing commands and minimum-version failures. Pinned source repositories and lockfile-pinned project dependencies can be fetched only after consent to an immutable fetch-plan digest.

Post-run stop thresholds may include `max_usd_per_run`, `max_usd_total`, and `max_tokens_per_run`. The time value is a live timeout. Dollar and token thresholds are checked after each run, so the total can overshoot by one run. A manifest digest freezes the published contract.

Codex subagent topology uses `COUNTx:MODEL:EFFORT`, for example `5x:gpt-5.6-luna:ultra`. Counts are 1 through 16. This format lets hbench lock the child model, reasoning effort, and thread cap instead of relying on prompt labels.

Pi skills, extensions, and plugins use exact installed npm package versions, such as `pi-subagents@0.50.0` or `@ff-labs/pi-fff@0.10.3`. hbench resolves each package's declared Pi entry points and disables ambient discovery. A missing or different installed version stops the study before spending.
