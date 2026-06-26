# Turbo Innovations Adoption — Plan Index

**Spec:** `docs/superpowers/specs/2026-06-24-turbo-innovations-adoption-design.md`
**Created:** 2026-06-25
**Status:** Ready for execution

The spec covers 5 independent subsystems. Per the writing-plans skill's scope-guidance, each subsystem has its own plan document so it can be verified and dispatched independently. Shared types are defined in the earliest plan that introduces them and imported by later plans.

## Plans

| # | Plan | Thread | Depends on | Independent value? |
|---|------|--------|------------|-------------------|
| 1 | [turbo-a-markdown-templates](2026-06-25-turbo-a-markdown-templates.md) | A | none | Yes — markdown-overridable planner templates |
| 2 | [turbo-d-complexity-routing](2026-06-25-turbo-d-complexity-routing.md) | D | A (template loader) | Yes — graded mode signal (direct/plan/spec_plan/spec_pair) |
| 3 | [turbo-cf-orchestrator-phases](2026-06-25-turbo-cf-orchestrator-phases.md) | C+F | D (`PlanRequest.Mode`, `decompose_spec.md`) | Yes — phases with produces/consumes, proactive chunking |
| 4 | [turbo-b-context-isolation](2026-06-25-turbo-b-context-isolation.md) | B | A (loader), C+F (`plan.Artifact`, `startNextPhase`) | Yes — structured handoffs + per-task-per-agent loops |
| 5 | [turbo-e-self-reflection](2026-06-25-turbo-e-self-reflection.md) | E | A (loader for reflection templates) | Yes — per-turn reflection, `/remember`, improvements queue |

## Execution order

Per spec §"Implementation Sequence":

```
Phase 1: Plan 1 (Thread A)         — no dependencies
Phase 2: Plan 2 (Thread D)         — depends on A's template loader
Phase 3: Plan 3 (Thread C+F)       — depends on D's mode signal
Phase 4: Plan 4 (Thread B)         — B-c depends on C+F; B-a and B-b parallel with Phase 3
Phase 5: Plan 5 (Thread E)         — depends on A's template loader; parallel with Phases 2-4
```

Plans 1 and 5 can be dispatched in parallel immediately. Plan 2 must wait for Plan 1 to merge. Plan 3 must wait for Plan 2. Plan 4 must wait for Plan 3 (but B-a and B-b pieces can be worked in parallel with Plan 3 once the `plan.Artifact` type exists).

## Shared types cross-reference

| Type | Defined in | Used by |
|------|-----------|---------|
| `plannerTemplateLoader` | Plan 1 (Thread A) | Plans 2, 3, 4, 5 |
| `PlanRequest.Mode` field | Plan 2 (Thread D) | Plans 3 |
| `IntentType.SuggestedMode()` | Plan 2 (Thread D) | Plan 2 only |
| `PlanPhaseSpec`, `plannerPhaseOutput` | Plan 3 (Thread C+F) | Plan 3 only |
| `Artifact` | Plan 3 Task 1 (in `agent`), then **moved** to `plan` package in Plan 3 Task 7 | Plans 3, 4 (`StepHandoff.Artifacts`) |
| `StepHandoff` | Plan 4 (Thread B) | Plan 4 only |
| `Trajectory`, `ReflectionProposal` | Plan 5 (Thread E) | Plan 5 only |

## Files touched by multiple plans (coordination points)

| File | A | D | C+F | B | E |
|------|---|---|-----|---|---|
| `internal/agent/strategic.go` | ✓ | ✓ | ✓ | | |
| `internal/agent/orchestrator.go` | | | ✓ | ✓ | |
| `internal/agent/registry.go` | | | ✓ | ✓ | |
| `internal/daemon/components.go` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `internal/agent/loop.go` | | | | ✓ | ✓ |
| `internal/config/schema.go` | | ✓ | ✓ | | ✓ |
| `config/meept.json5` | | ✓ | ✓ | | ✓ |
| `internal/agent/planner_template.go` | ✓ (create) | ✓ | ✓ | ✓ | ✓ |

When dispatching parallel plans, each plan's implementer must be aware of these shared files. The `superpowers:subagent-driven-development` skill dispatches one subagent per phase with verification between phases — sequential execution of the 5 plans avoids merge conflicts on these files.

## Q Agent rework — separate plan (not in scope)

Per spec, Q Agent rework is deferred to `docs/superpowers/specs/YYYY-MM-DD-q-agent-rework-design.md` (TBD). Plan 5 (Thread E) reserves the integration point (Q proposals land in `.meept/improvements.md`) but does not implement Q internals.

## Verification strategy (per spec)

| Thread | Verification |
|--------|--------------|
| A | `meept config prompts planner decompose` shows markdown source; editing `.meept/prompts/planner/decompose.md` changes planner behavior |
| D | `meept chat "what's X"` → mode=direct ACK; `meept chat "/plan refactor auth"` → mode=plan; `meept chat "rebuild the search subsystem"` → mode=spec_plan |
| C+F | Plan with multiple phases renders in `meept plans show <id>` with produces/consumes blocks; oversized step gets split (visible in logs) |
| B | Phase 2's prompt doesn't contain phase 1's tool outputs (only consumes); two concurrent tasks' loops are distinct Go objects |
| E | `.meept/improvements.md` accumulates proposals after sessions; `/implement-improvements` applies them; `/remember "..."` works as both agent tool and user slash command |
