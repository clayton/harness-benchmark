# Harness adapters

Adapters connect `hbench` to a coding-agent CLI. The Go runner prepares the
workspace, launches a harness with an argument vector, captures its log, and
parses telemetry. It does not construct shell command strings.

## Running an adapter

Create a pending run with either a saved setup or explicit flags:

```bash
hbench run -s rodeo:scenario@version --setup my-pi --runtime native
hbench execute RUN_ID
```

`hbench execute` runs the selected headless harness in the printed workspace,
then `hbench finish` overlays gold tests and judges the patch. Manual runs skip
`execute`: edit the workspace and call `hbench finish RUN_ID`.

## Adapter contract

An adapter must make the measured setup explicit and keep unsupported claims
visible. At minimum it needs:

1. a stable harness name and detected version;
2. a direct `argv` launch with the prompt passed as data;
3. isolated credentials and environment handling;
4. timeout and process-group cleanup;
5. telemetry extraction that marks incomplete fields as incomplete;
6. a clear error when a requested setup axis cannot be enforced.

The run snapshot records the selected model, reasoning, workflow, skills, and
mode. A `personal` profile may contain descriptive values that a harness cannot
lock; the result must not present those values as enforced controls.

## Current paths

| Harness | Path | Notes |
|---|---|---|
| Manual | `hbench finish` | Any local agent or human can edit the workspace. |
| Pi | `hbench execute` | Headless JSON output; explicit model, reasoning, and skill paths. |
| Grok | `hbench execute` | Headless JSON output; baseline launch support. |
| Claude Code | `hbench execute` | Headless launch support; unsupported setup axes are rejected for clean studies. |
| Codex | `hbench execute` | Isolated `CODEX_HOME`; personal mode uses a frozen `~/.codex/config.toml` snapshot. |
| Cursor | `hbench execute` | Headless launch support; unsupported setup axes are rejected for clean studies. |

Local skill directories currently work through Pi's repeatable `--skill-dir`
flag. hbench copies each directory into the run artifact and records its
content hash before execution. Package resources remain subject to the
harness's installed package layout.

## Adding an adapter

Keep the change small:

1. add the launch and version detection in `internal/loop`;
2. define which profile fields the adapter enforces;
3. add telemetry fixtures for success, failure, timeout, and missing usage;
4. add a help example and a focused integration test;
5. run `go test -mod=mod ./internal/loop ./cmd/hb`.

Do not add host package installation to OCI scenarios. Do not publish a result
until the adapter's snapshot and telemetry are reviewable.
