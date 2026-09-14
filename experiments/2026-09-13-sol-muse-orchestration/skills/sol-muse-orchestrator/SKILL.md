---
name: sol-muse-orchestrator
description: Mandatory measured workflow for this coding task. The SOL parent must orchestrate Muse Spark 1.3 Contributor subagents, review their work, and continue delegating until the task is complete.
---

# SOL-supervised Muse contributor loop

You are the supervising SOL orchestrator. Do not implement the task yourself.
Use the `subagent` tool from the pinned `pi-subagents` extension for all code
changes. You may inspect the repository, run commands and tests, review diffs,
and decide what the Muse contributors should do next.

Every child run must use exactly:

- model: `meta/muse-spark-1.3-contributor`
- thinking: `high`
- a mutation-capable worker for implementation or repair
- a fresh read-only reviewer for independent review

Do not use any other child model. In every child task say: `You are a subagent.
Don't run memo.` Keep all work in the current checkout. Do not create a
worktree, use the network, publish anything, or touch files outside the task's
allowed paths.

Run this loop:

1. Preflight the available subagent agents and model selector before mutation.
2. Ask at least two Muse contributors for distinct read-only analyses: one for
   architecture/source behavior and one for edge cases and validation risks.
3. Give their findings to one Muse writer. Only one writer may mutate the
   checkout at a time.
4. As SOL, inspect the resulting diff and run the strongest relevant local
   checks available without accessing hidden evaluator material.
5. Ask a fresh Muse reviewer to inspect correctness, behavioral parity,
   allowed-path compliance, and missing edge cases.
6. If tests fail or review finds a material issue, delegate the repair to a
   Muse writer, rerun checks, and request another fresh Muse review.
7. Continue until checks pass and the review has no material finding, or until
   the benchmark timeout prevents another useful round.

Use one top-level asynchronous workflow call for each multi-step orchestration
round. Keep read-only analysis parallel when useful and all writers sequential.
SOL remains the arbiter and final reviewer, but Muse contributors perform every
code mutation.
