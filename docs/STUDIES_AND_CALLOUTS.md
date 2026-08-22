# Studies and Callouts

| Goal | Command path |
|---|---|
| Test one setup on one scenario | `hbench run` → `hbench execute RUN_ID` → `hbench publish --preview RUN_ID` → `hbench publish RUN_ID` |
| Compare two or more setups | `hbench study validate` → `plan` → `run` → `publish` |
| Challenge a published claim | `hbench callout challenge URL` → run the downloaded Study |

Before either path, run `command -v hbench` and `hbench version`. Do not change `PATH` silently to work around a version mismatch. Every command after `hbench run` should use its printed run ID, especially after a retry.

`hb.study.v1` describes a complete experiment. Every Study scenario uses `rodeo:slug@version` and that public version's `manifest_digest`; embedded local scenario IDs are for direct rides only. The contract fixes the question, sources, comparison mode, scenario versions, setup arms, changed axes, repeats, random seed, judge protocol, win rule, and post-run stop thresholds. `controlled` studies reject undeclared setup differences. `ecological` studies compare complete real-world bundles and disclose their confounds.

Study execution is sequential by default. The seed randomizes the arm and scenario order. Each finished cell is saved under `hb-out/studies`, so the same command resumes after an interruption. `hbench study run` requires `--approve-spend`. Publishing is a separate command.

Before any cell runs, hbench checks every scenario prerequisite. It reports missing or old tools and does not install them. If an uncached pinned repository, remote manifest, or lockfile-pinned dependency set is required, hbench prints one immutable fetch plan and asks for consent. Non-interactive agents must stop and give the user the printed `hbench fetch approve <digest>` command. A changed plan invalidates the prior approval.

The minute value is a live timeout. Token and dollar values are checked after each run because the supported harnesses do not expose a common live usage counter. A run can exceed a per-run threshold, and a study can exceed its total dollar threshold by one run. These values stop later runs; they are not billing-provider hard caps.

A Callout adds a testable statement and attribution to one frozen study digest. A Challenge uses that exact digest. A changed setup, scenario, repeat count, budget, or judge rule is a linked counterexample instead of a title challenge.

The bundled `run-agent-rodeo-study` skill turns a natural-language request into either one direct ride or a comparison manifest. It shows the exact setup, matrix when applicable, and budget before it asks for approval. Install it only when requested with `hbench skill install`.

Hosted execution remains Phase 3. Version 0.5 runs Study matrices on the user's machine and publishes them as Open Range evidence. The `controlled` comparison mode controls experimental axes; it does not grant the site's Controlled evidence badge. Signed hosted Study execution needs a runner that binds every attestation to the frozen study cell and enforces token and dollar limits. Herdr can monitor an operator workflow, but it is never part of a measured setup unless the contract explicitly names it.
