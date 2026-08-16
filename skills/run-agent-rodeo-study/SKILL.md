---
name: run-agent-rodeo-study
description: Turn a natural-language coding-agent comparison, setup claim, or X post into a fair, budgeted Agent Rodeo study. Use when someone asks which model, harness, workflow, skill, plugin, or subagent setup is better, cheaper, faster, or more reliable; wants to challenge a public claim; or wants hbench study commands without learning the manifest format.
---

# Run Agent Rodeo Study

Convert the question into an explicit experiment before running agents.

1. Inspect the local harness versions, installed skills/extensions/plugins, scenario prerequisites, and available Agent Rodeo scenarios. Treat an X URL as attributed source material, not proof. Never install a system toolchain, harness, plugin, skill, or extension to make a ride pass.
2. Ask only for a missing comparison intent or post-run spending threshold. Do not make the user learn hbench syntax.
3. Choose `controlled` when one or more named axes must remain fixed. Choose `ecological` when complete real-world setups compete.
4. Write an `hb.study.v1` manifest. Follow [the schema](references/schema.md) and [fairness rules](references/fairness-and-telemetry.md).
5. Run `hbench study validate STUDY.yaml`, then `hbench study plan STUDY.yaml`.
6. Show the arms, changed axes, scenario count, repeats, total run count, confounds, timeout, and post-run stop thresholds. Say that a dollar threshold can overshoot by one run.
7. Get explicit approval before `hbench study run`. If hbench prints a fetch plan, show its source, ref or checksum, reason, destination, and size to the user. Let hbench ask `Proceed? [Y/n]`, or ask the user before running `hbench fetch approve <digest>` in a non-interactive session. Never approve a fetch on the user's behalf. Get separate explicit approval before `hbench study publish` or `hbench callout create`.
8. Monitor sequential runs. Resume from recorded state after interruption. Never use Herdr or an orchestration agent as part of a measured arm. If Herdr exists, it may only monitor the operator workflow.
9. Do not call missing cost or child-agent usage zero. Mark telemetry incomplete; an incomplete setup cannot win an efficiency title.

Use [callout guidance](references/callouts.md) when the request is a public claim or challenge. Start from [the example](references/example.yaml) when useful.
