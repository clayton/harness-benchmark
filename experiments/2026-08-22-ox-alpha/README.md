# Ox Alpha hype-wave experiment

## Question

Does OpenRouter `stealth/ox-alpha` match or beat OpenRouter `openai/gpt-5.6-sol` on one frozen Go bug when both use stock Pi 0.84.2 at high reasoning?

## Fixed protocol

- Harness: Pi 0.84.2
- Provider: OpenRouter
- Scenario: `go-chi-tee-bytes-double-count`
- Scenario digest: `7d9d974a3bc686f57173801c1fb7354901ae8d8263b7b41afd5d2a6dc9974e6b`
- Reasoning: high
- Workflow: baseline, no skills/extensions/plugins/subagents
- Repeats: 3 per model, 6 runs total
- Contract: `3b17f13c7db1750ac8e6eb7d078025cbf3961cc8221388638a3eb68f99a995a7`
- Execution class: Controlled design, Open Range execution

## Win rule

Reliability first. One arm wins if it completes 3/3 and the other does not. If both complete 3/3, outcome is tied; compare time, tokens, and cost only when telemetry is complete for both arms. Infrastructure failures are not model losses.

This is one easy public Go scenario with medium contamination risk. It supports claims only about this task and protocol.

## Commands

```bash
hbench study validate experiments/2026-08-22-ox-alpha/study.yaml
hbench study plan experiments/2026-08-22-ox-alpha/study.yaml
hbench study run experiments/2026-08-22-ox-alpha/study.yaml --approve-spend
hbench study status experiments/2026-08-22-ox-alpha/study.yaml
```

Do not publish without a separate explicit approval. Record every problem in `PAPERCUTS.md`; do not silently work around it.
