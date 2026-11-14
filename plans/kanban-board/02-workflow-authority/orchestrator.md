# 02-workflow-authority

## Goal
Implement the authority logic for Kanban transitions, ensuring state changes are guarded by valid transition rules and published as bus events.

## Interface Contracts
- Inherited: master.md C1-C10
- Local: 
    - C2: Bus topics for state events (`kanban.event.step.state`, `.goal.state`, etc.) (as defined in `internal/bus/Topic[T]`).
    - C10: Lease rule enforcement for card moves.

## Child Index

| Child | Type | Deps | Est context | Files |
|---|---|---|---|---|
| 02/01-task-transition-guard | Leaf | — | 60K | transitions.go, transitions_test.go |
| 02/02-queue-transition-guard | Leaf | — | 60K | transitions.go, transitions_test.go |
| 02/03-publishers-task-queue | Leaf | 02 | 90K | events.go, [edits to task/step/goal] |
| 02/04-publishers-plan-step-goal | Leaf | 02 | 90K | [edits to plan/step/goal] |

## Dispatch Protocol
1. `delegate_task` per leaf.
2. Review in-session by main model.
3. Do NOT commit. Do NOT run `git add`.
4. `git status --short` and `git diff --stat HEAD` after each async return.
5. Commit per leaf after review pass.
6. Update tracking table in `master.md`.

## Coding Conventions
- Go stdlib + existing deps only.
- Match neighbor file style; cite invariant docs.
- Implement guards mirroring the pattern in `internal/plan/transitions.go`.

## Completion Tracking Table
| Leaf | Status | Notes |
|---|---|---|
| 02/01-task-transition-guard | PENDING | |
| 02/02-queue-transition-guard | PENDING | |
| 02/03-publishers-task-queue | PENDING | |
| 02/04-publishers-plan-step-goal | PENDING | |

## Integration Test Plan
- Verify that invalid transitions for tasks/jobs/plans are blocked.
- Verify that valid transitions emit `StateEvent` on correct `kanban.event.*` topics.
- Ensure dead-letter transitions for queue jobs are permitted.

## Review Checklist
- [ ] No leaf owns >3 changed files.
- [ ] Contract IDs C1-C10 used verbatim.
- [ ] Every new Set*/setter has a nil guard.
- [ ] Every state has a publisher path via `StateEvent`.
- [ ] No I/O under mutex.
