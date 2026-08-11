# Adapters

Adapters bridge **Harness Benchmark** to a specific coding harness (Grok, Claude
Code, Codex, Cursor, …).

## Interface (target)

```text
prepare(scenario) -> worktree
run(config, prompt, worktree, budget) -> stream events
finalize() -> patch + telemetry
```

## v0 approach

Most “adapters” today are **config launch strings**, not Python plugins:

1. `hb run -s … -c …` prepares a workspace + snapshot
2. `hb execute <run_id>` shells out to `harness_options.launch_headless`
3. Telemetry is parsed from agent logs (pi JSONL, grok JSON today)
4. Or: run any agent yourself and `hb finish` / `hb ingest`

Prefer thin wrappers around each harness CLI rather than re-implementing agent loops.

## Status

| Path | Status |
|------|--------|
| Manual (`hb finish` / `hb ingest`) | Supported |
| pi via `launch_headless` | Supported (JSON telemetry) |
| Grok CLI via `launch_headless` | Supported (JSON telemetry) |
| Claude Code / Codex / Cursor | Config sketches only — wire launch strings + parsers |
| Formal adapter package API | Wishlist (see [WISHLIST.md](../WISHLIST.md)) |
