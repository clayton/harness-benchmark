# hbench examples

These examples use the current `hbench` CLI. Results stay local until you
explicitly publish them.

## Manual run

```bash
hbench inspect -s rodeo:js-commander-negative-exp-E@1
hbench run -s rodeo:js-commander-negative-exp-E@1 --harness manual
# edit the printed workspace, then:
hbench finish
hbench report
```

## Headless run

```bash
hbench ride -s rodeo:js-commander-negative-exp-E@3 \
  --harness pi --model openrouter/z-ai/glm-5.3-flash \
  --approve-spend
hbench report
```

The participant command defaults to a $1 relay-enforced cap; change it with
`--max-usd`. It needs no host Node, npm, or Pi installation.

## OCI-controlled execution

Evaluator-controlled runs use an OCI-compatible CLI. `auto` selects Docker,
Podman, or nerdctl; Colima and Dory Docker-compatible contexts need no special
adapter.

```bash
hbench controlled run --scenario rodeo:slug@version \
  --pack ./evaluator --key-id <runner-key> \
  --runtime auto --approve-spend
```

The scenario manifest may require separate immutable fetch consent, and
`--approve-spend` is required before credential-backed execution. Use `--runtime native` only as the advanced compatibility path for scenarios
without a published OCI environment image.

## Study

```bash
hbench study validate experiments/example.yaml
hbench study plan experiments/example.yaml
hbench study run experiments/example.yaml --approve-spend
hbench study report experiments/example.yaml
```

`hbench study report` includes only runs bound to that study contract. It does
not upload results. Publishing is always a separate explicit command:

```bash
hbench publish <run-id>
```
