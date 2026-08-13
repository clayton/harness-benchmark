---
name: harness-benchmark
description: Install the hbench CLI and run an official coding-agent benchmark ride.
---

# Harness Benchmark

Install the same way a human would:

```bash
curl -fsSL https://agentrodeo.dev/install.sh | sh
hbench
```

If the installer prints an `export PATH=...` line, run that once in the same terminal, then `hbench`.

`hbench` prints one command. Paste that. That is the first ride. Do not attach extra skills unless the user asks. After the ride:

```bash
hbench report
# optional
hbench publish
```
