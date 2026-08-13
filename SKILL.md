---
name: harness-benchmark
description: Install the hb CLI and run an official coding-agent benchmark ride.
---

# Harness Benchmark

Install the same way a human would:

```bash
curl -fsSL https://agentrodeo.dev/install.sh | sh
```

Then:

```bash
export PATH="$HOME/.local/bin:$PATH"
hb
```

`hb` prints one command. Paste that. That is the first ride. Do not attach extra skills unless the user asks. After the ride:

```bash
hb report
# optional
hb publish
```
