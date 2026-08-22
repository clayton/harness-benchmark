# Ox Alpha experiment papercuts

Record every CLI, site, docs, harness, provider, and local-environment problem here. Do not silently bypass failures.

| Time | Stage | Command | Observed result | Owner | Severity | Proposed fix | Status |
|---|---|---|---|---|---|---|---|
| 2026-08-22 | Setup | `command -v hbench` | No binary was installed. | CLI onboarding | high | Add explicit version/install preflight to `/cli`. | fixed locally |
| 2026-08-22 | Site | Compare `/cli` with `/install.sh` | Page said 0.3.3 while installer served 0.5.3. | site/release | high | Generate page version from installer release metadata. | pushed, deployment unverified |
| 2026-08-22 | Scenario | `hbench run -s js-commander-negative-exp-E ...` | `npm install` violates fetch-plan policy. | CLI/scenario | high | Support lockfile-pinned npm fetch consent. | open |
| 2026-08-22 | Grok | `hbench execute 2d24d2e4b989` | Isolated HOME lost working Grok auth. | Grok adapter | high | Preflight auth inside the isolated harness environment. | open |
| 2026-08-22 | Pi | `hbench execute 4471bebc28ca` | Selected `openai` provider was absent from copied auth. | Pi adapter | high | Preflight selected provider inside copied Pi auth. | open |
| 2026-08-22 | Evidence | `hbench list runs` | Infrastructure failures look like model failures. | CLI/reporting | high | Add failure classification and hide infrastructure failures from model summaries. | open |
| 2026-08-22 | Codex | `hbench execute 5420add3173f` | Codex workspace was read-only. | Codex adapter | critical | Pass `--sandbox workspace-write`. | fixed and pushed in `25d1ec6` |
| 2026-08-22 | Provenance | `hbench version` | Local build still reports release version 0.5.3. | CLI/release | medium | Include Git SHA and dirty/release provenance. | open |
| 2026-08-22 | Telemetry | Inspect `0e1720a1586a/run.json` | Successful run has `complete: false` without saying why. | telemetry/reporting | high | Explain missing telemetry and block cost-efficiency claims. | open |
| 2026-08-22 | Herdr | `herdr agent read rodeo-experiment ...` | Codex final answer was lost from host scrollback because of alternate screen. | Herdr/Codex integration | high | Capture final response directly or use non-TUI Codex mode. | open |
| 2026-08-22 | Subagent | Start `rodeo-experiment` | Unrelated MCP login/startup errors flooded the experiment pane. | local agent profile | low | Add a clean operator profile with ambient MCPs disabled. | open |
| 2026-08-22 | Model provenance | `pi --list-models` | `stealth/ox-alpha` is an opaque alias that can drift. | CLI/provider | high | Freeze model metadata and retrieval time in the Study artifact. | open |
| 2026-08-22 | Claims | `comparison_mode: controlled` | Easy to confuse controlled design with Controlled Arena evidence. | site/docs | high | Always label this `Controlled design · Open Range execution`. | open |
| 2026-08-21 23:46 MST | Study run | `hbench study run experiments/2026-08-22-ox-alpha/study.yaml --approve-spend` | Exited 1 after creating pending run `20a034dbaebc`: `could not resolve pi harness version`. | Pi harness resolver | critical | Make frozen Pi `0.84.2` resolvable, and add a study preflight that fails before creating a pending run. | open |
| 2026-08-22 | Study resume | `hbench study run experiments/2026-08-22-ox-alpha/study.yaml --approve-spend` | With `hbench 0.5.4 (go)`, resumed run `20a034dbaebc`, then exited 1: `run 20a034dbaebc has no complete token telemetry for its token limit`. Study stopped at 1/6. | telemetry/study runner | critical | Capture complete Pi token telemetry or define a safe enforceable fallback before running a token-limited Study cell. | open |
| 2026-08-22 | Retry instruction | User instruction: `run exactly ,` | The command after `run exactly` was missing. Verified `hbench 0.5.5 (go)` but did not infer or execute a spend-bearing command. | study operator handoff | high | Include the complete exact retry command in the operator instruction. | open |
