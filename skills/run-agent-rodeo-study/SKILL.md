---
name: run-agent-rodeo-study
description: Turn a natural-language coding-agent benchmark, comparison, setup claim, or X post into an Agent Rodeo ride or fair budgeted study. Use when someone wants to test one setup; asks which model, harness, workflow, skill, plugin, or subagent setup is better, cheaper, faster, or more reliable; wants to challenge a public claim; or wants hbench commands without learning the CLI or manifest format.
---

# Run Agent Rodeo Study

Route the request before running agents.

1. Run `command -v hbench`, `hbench version`, and `hbench run --help`. Report the selected executable and version. Never prepend to `PATH` or switch binaries silently. If the installed CLI lacks a required flag, stop and tell the user how to update it; do not update it yourself.
2. Inspect harness versions, saved setup profiles (`hbench setup list/show`), installed skills/extensions/plugins, scenario prerequisites, and Agent Rodeo scenarios. Treat an X URL as attributed source material, not proof. Never install a system toolchain, harness, plugin, skill, or extension to make a ride pass.
3. For one setup on one scenario, use a direct ride: `hbench run`, the printed run ID with `hbench execute RUN_ID` or `hbench finish RUN_ID`, `hbench report`, and `hbench publish --preview RUN_ID`. Publish only after explicit approval. Do not create a Study manifest for one setup.
4. For two or more setups, create a public `hb.study.v2` comparison with `hbench study init --visibility public` unless the user wants to keep it private. Saved profiles can be passed with repeated `--setup`; explicit model or skill treatments use repeated `--model` or `--skill`. A public Study may use `rodeo:slug@version` or a local scenario backed by a public repository and fixed commit; hbench embeds the latter as a path-free `hb.task.v1` contract. Public arms may include personal setup metadata, local-skill hashes, prompt treatments, and validated external adapters. Ask only for a missing comparison intent or post-run spending threshold. Choose `controlled` when named axes must remain fixed and `ecological` when complete real-world setups compete.
5. Follow [the schema](references/schema.md) and [fairness rules](references/fairness-and-telemetry.md). Run `hbench study validate STUDY.yaml`, then `hbench study plan STUDY.yaml`, then `hbench study run STUDY.yaml --approve-spend`, and finally `hbench study report STUDY.yaml`.
6. Show the arms, changed axes, scenario count, repeats, total run count, confounds, timeout, and post-run stop thresholds. Say that a dollar threshold can overshoot by one run.
7. Get explicit approval before any model spend. If hbench prints a fetch plan, show its source, ref or checksum, reason, destination, and size. Let hbench ask `Proceed? [Y/n]`, or ask the user before running `hbench fetch approve <digest>` in a non-interactive session. Never approve a fetch on the user's behalf. Get separate explicit approval before publication or Callout creation.
8. Record every run ID from command output and pass it to all later commands. Never use an implicit latest run when retries created more than one record. Use `hbench list runs` to explain pending and setup-failed records; do not delete them automatically.
9. Monitor sequential runs. Resume from recorded state after interruption. Never use Herdr or an orchestration agent as part of a measured arm. If Herdr exists, it may only monitor the operator workflow.
10. Do not call missing cost or child-agent usage zero. Mark telemetry incomplete; an incomplete setup cannot win an efficiency title.

For an original authored task, use the CLI scaffold path before submission:

```bash
hbench scenario new --out scenarios/task@1.yaml --id task@1 \
  --title "Task" --prompt "Implement the requested behavior." \
  --language go --files-json '{"README.md":"starter\n"}'
hbench scenario validate scenarios/task@1.yaml
```

See [scenario authoring](../../scenarios/README.md) for repository and
scaffold manifest rules. Upload a validated YAML through Agent Rodeo's
Community Scenarios page; do not put private evaluator files or target patches
in the public manifest.

Use [callout guidance](references/callouts.md) when the request is a public claim or challenge. Start from [the example](references/example.yaml) when useful.
