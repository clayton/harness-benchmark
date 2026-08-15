# Studies and Callouts

`hb.study.v1` describes a complete experiment. It fixes the question, sources, comparison mode, scenario versions, setup arms, changed axes, repeats, random seed, judge protocol, win rule, and post-run stop thresholds. `controlled` studies reject undeclared setup differences. `ecological` studies compare complete real-world bundles and disclose their confounds.

Study execution is sequential by default. The seed randomizes the arm and scenario order. Each finished cell is saved under `hb-out/studies`, so the same command resumes after an interruption. `hbench study run` requires `--approve-spend`. Publishing is a separate command.

The minute value is a live timeout. Token and dollar values are checked after each run because the supported harnesses do not expose a common live usage counter. A run can exceed a per-run threshold, and a study can exceed its total dollar threshold by one run. These values stop later runs; they are not billing-provider hard caps.

A Callout adds a testable statement and attribution to one frozen study digest. A Challenge uses that exact digest. A changed setup, scenario, repeat count, budget, or judge rule is a linked counterexample instead of a title challenge.

The bundled `run-agent-rodeo-study` skill turns a natural-language question or public claim into the manifest. It shows the full matrix and budget before it asks for approval. Install it only when requested with `hbench skill install`.

Hosted execution remains Phase 3. Version 0.5 runs Study matrices on the user's machine and publishes them as Open Range evidence. The `controlled` comparison mode controls experimental axes; it does not grant the site's Controlled evidence badge. Signed hosted Study execution needs a runner that binds every attestation to the frozen study cell and enforces token and dollar limits. Herdr can monitor an operator workflow, but it is never part of a measured setup unless the contract explicitly names it.
