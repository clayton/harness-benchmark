---
name: harness-benchmark
description: Install the hbench CLI and run an official coding-agent benchmark ride.
---

# Harness Benchmark

Public Agent Rodeo discovery index: https://agentrodeo.dev/llms.txt

Install the same way a human would. First inspect the selected executable and
version when a study or comparison is requested:

```bash
command -v hbench
hbench version
hbench run --help
curl -fsSL https://agentrodeo.dev/install.sh | sh
hbench
```

If `hbench` is not installed, run the installer and then repeat the identity
checks. Do not silently change `PATH` or install another harness, skill,
extension, plugin, or system toolchain.

`hbench` with no arguments prints one suggested clean-baseline ride. Paste that
command only after reviewing its scenario and spend approval. Do not attach
extra skills unless the user asks.

For a personal setup, save the selected recipe and use the native path:

```bash
hbench setup save review-pi \
  --harness pi --provider openrouter \
  --model openrouter/z-ai/glm-5.3-flash \
  --reasoning high --workflow plan-first \
  --skill-dir ./skills/review
hbench setup show review-pi
hbench run -s rodeo:slug@version --setup review-pi --runtime native
hbench execute RUN_ID
hbench report
```

`setup save` defaults to `personal`. Personal Codex runs freeze only
`~/.codex/config.toml`; a local Pi skill directory is copied and hashed per
run. Use `--mode clean-baseline` for a clean recipe. Personal or local-skill
Studies must be private.

For two or more setups, initialize and inspect a frozen local Study:

```bash
hbench study init --question "Question" --scenario rodeo:slug@version \
  --setup clean-pi --setup review-pi --visibility private --out study.yaml
hbench study validate study.yaml
hbench study plan study.yaml
hbench study run study.yaml --approve-spend
hbench study report study.yaml
```

Use `hbench publish --preview RUN_ID` to inspect a privacy-filtered result.
Publishing is a separate explicit action: `hbench publish RUN_ID`.

To author an original scaffold, generate and validate its YAML before a local
manual dry run:

```bash
hbench scenario new --out scenarios/task@1.yaml --id task@1 \
  --title "Task" --prompt "Implement the requested behavior." \
  --language go --files-json '{"README.md":"starter\n"}'
hbench scenario validate scenarios/task@1.yaml
```

For a complete comparison workflow, read the bundled
[`run-agent-rodeo-study` skill](skills/run-agent-rodeo-study/SKILL.md) and its
[Study schema](skills/run-agent-rodeo-study/references/schema.md).

After a ride:

```bash
hbench report
```

Never publish benchmark results without explicit user approval. Use
`hbench publish --preview RUN_ID` first.
