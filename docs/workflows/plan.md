# Plan Pipeline

The plan pipeline turns a request into a sealed plan document, reviews it, and
(optional, opt-in) executes it. Plans are plain markdown with a fixed section
skeleton, and every stage writes its artifact to disk so the pipeline is
inspectable after the fact.

This page documents the artifacts and where they live. For the execution
semantics (serial phases, frontier-driven parallel phases, worktrees, budget
selection) see `docs/workflows/agent-orchestration.md`.

## Artifacts

A plan run writes one markdown file per stage:

| File | Stage |
|---|---|
| `plan-the-refactor.md` | the brainstorm/plan stage: what to change and why |
| `carry-out-the-migration-plan.md` | the execution stage: steps and order |
| `quickplan-wave-one.md` | a quickplan wave: batched execution without a user review loop |
| `sealed-plan.md` | the sealed plan: the reviewed, frozen artifact the executor consumes |

They live under the owning package so the plan sits next to the code it
changes:

- `internal/agent/docs/plans/` - agent-loop work
- `internal/daemon/docs/plans/` - daemon work
- `internal/plan/docs/plans/` - plan-package work
- `docs/plans/` - cross-package work

`sealed-plan.md` may appear in more than one directory because each plan run
seals its own copy next to its target package.

## Plan file skeleton

Every plan file carries the same skeleton so reviewers know where to look:

```markdown
# <Title>

## Problem
<!-- What problem does it solve? -->

## Behavior
<!-- How does it work? -->

## Configuration
<!-- Configuration options -->

## Edge Cases
<!-- Important edge cases -->
```

The comments are placeholders: an author fills each section or states why it
does not apply. A sealed plan keeps the skeleton even when a section is short,
so a reviewer can see that the question was asked.

## Stages

1. **Draft.** The plan stage writes the skeleton with the problem and intended
   behavior filled in.
2. **Review.** The plan is checked for completeness (every section addressed)
   and consistency with the code it touches.
3. **Seal.** The reviewed plan is written as `sealed-plan.md`. A sealed plan is
   frozen: later changes are a new plan, not an edit.
4. **Execute (opt-in).** The plan compiler pipeline
   (`plans.plan_compiler_enabled`, default false) consumes the sealed plan.
   With the flag off, the legacy JSON `spec_plan` path runs instead and no code
   may assume the draft store exists.

## Edge Cases

- A sealed plan that no longer matches the code is stale. The fix is a new plan
  that supersedes it, not an edit to the sealed file.
- Plans are per-package on purpose: a plan for `internal/agent` should not need
  a cross-package review to change one loop.
- The draft store (`task.Metadata["plan_draft"]`) exists only when the plan
  compiler is enabled; code that reads it must tolerate its absence.

## Related

- `docs/workflows/agent-orchestration.md` - plan execution semantics
- `docs/plans/classifier-iteration/master.md` - a long-running plan tree in use
