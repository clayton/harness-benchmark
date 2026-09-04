# Scenarios

A scenario is a frozen coding task. The agent receives the prompt and the
repository at `base_ref`. The judge may use `gold_ref`, hidden tests, or
behavioral checks; those values never enter the agent prompt or workspace.

## Choose a task

Use one logical bug, feature, refactor, rewrite, or endurance task. Prefer a
small change with an executable acceptance test and a maintained repository.
Keep the prompt focused on intent, constraints, and observable behavior.

Scenario IDs and published versions are immutable. If the repository ref,
dependency lockfile, image, setup command, or evaluator changes, publish a new
version.

## Author a manifest

For a repository task, provide a public GitHub URL, a 40-character immutable
`base_ref`, a target `gold_ref`, an intent prompt, and acceptance commands. For
an original task, use a scaffold workspace with authored files and keep the
solution in the evaluator or target reference.

The authoring helpers enforce canonical relative paths and reject unsafe
scaffolds:

```go
scenario, err := corpus.NewScaffoldScenario(corpus.NewScenarioOptions{
    ID: "cache-header-rewrite@1", Title: "Rewrite cache headers",
    Prompt: "Make the cache header ...", Type: "feature", Language: "go",
}, map[string]string{"README.md": "starter\n"})
if err != nil { return err }
return corpus.WriteScenario("scenarios/cache-header-rewrite.yaml", scenario)
```

For an existing YAML file, call `corpus.ValidateScenarioFile(path)` from a
focused Go check before submitting it. A local run also exercises resolution,
trust, fetching, workspace setup, and judging:

```bash
go test -mod=mod ./internal/corpus
hbench scenario new --out scenarios/cache-header-rewrite@1.yaml \
  --id cache-header-rewrite@1 --title "Rewrite cache headers" \
  --prompt "Make the cache header reflect the configured TTL." \
  --type rewrite --language go \
  --files-json '{"README.md":"starter\n"}'
hbench scenario validate scenarios/cache-header-rewrite@1.yaml
hbench run -s ./scenarios/cache-header-rewrite@1.yaml --harness manual
```

The run can remain private. Publication is a separate explicit action.

## Community submission

Submit the validated manifest through Agent Rodeo's Community Scenarios page.
The site checks repository access, ancestry, immutable refs, the execution
surface, and the scenario contract. Community acceptance and official rating
eligibility are separate: an accepted proposal becomes runnable community
material; only a trusted Controlled validation can affect the official
rating.

Do not include provider credentials, private evaluator files, target patches,
or commands that install unplanned dependencies. Dependency metadata must be
locked and versioned with the scenario.

## Scenario types

| Type | Use |
|---|---|
| `bugfix` | A focused regression with a failing or hidden test. |
| `feature` | A small behavior addition with acceptance tests. |
| `refactor` | A behavior-preserving structural change. |
| `rewrite` | A bounded original implementation, often scaffolded. |
| `endurance` | A repeat or long-running task with explicit limits. |

See [SHORTLIST.md](SHORTLIST.md) for research notes and existing candidate
tasks. `example-synthetic-bugfix.yaml` documents the shape only and must not
be used for a public claim.
