# hb.study.v1

Required fields are `schema`, `id`, `question`, `comparison_mode`, `scenarios`, `arms`, `varied_axes`, `repeats`, `seed`, `judge_protocol`, `win_rule`, and `budget.max_minutes_per_run`.

`win_rule` is currently the fixed identifier `callout-title-v1`. It ranks reliability first, then quality, then complete cost and token efficiency.

Each scenario has an immutable `id` and required 64-character digest. Each arm names its harness, exact `harness_version`, model, and any provider, reasoning level, workflow, skills, extensions, plugins, tools, subagent topology, environment, and network policy. The harness version must match the installed harness `--version` output.

Post-run stop thresholds may include `max_usd_per_run`, `max_usd_total`, and `max_tokens_per_run`. The time value is a live timeout. Dollar and token thresholds are checked after each run, so the total can overshoot by one run. A manifest digest freezes the published contract.

Codex subagent topology uses `COUNTx:MODEL:EFFORT`, for example `5x:gpt-5.6-luna:ultra`. Counts are 1 through 16. This format lets hbench lock the child model, reasoning effort, and thread cap instead of relying on prompt labels.

Pi skills, extensions, and plugins use exact installed npm package versions, such as `pi-subagents@0.50.0` or `@ff-labs/pi-fff@0.10.3`. hbench resolves each package's declared Pi entry points and disables ambient discovery. A missing or different installed version stops the study before spending.
