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
| 02/01-task-transition-guard | Leaf | — | 60K | internal/task/transitions.go, internal/task/task.go (+ documented SetState call-site migration per the leaf's blast-radius note) |
| 02/02-queue-transition-guard | Leaf | — | 60K | internal/queue/transitions.go, internal/queue/store.go |
| 02/03-publishers-task-queue | Leaf | 02 | 90K | internal/kanban/events.go, internal/task/registry.go + internal/queue/queue.go publisher wiring (task+job only) |
| 02/04-publishers-plan-step-goal | Leaf | 02 | 90K | internal/plan/manager.go, internal/task/step.go, internal/employee/goal.go publisher wiring |

NOTE: this branch violates the master "no leaf owns >3 changed files" rule
by design — event publication must touch each machine's package. The two
publisher leaves are sequential (04 depends on 03) and the master grants
them a documented exception.

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
| 02/01-task-transition-guard | DONE | 8c1de6b6 (option (a) SetStateChecked; run 1 claimed COMPLETE and landed nothing — re-dispatched, verified on disk) |
| 02/02-queue-transition-guard | DONE | b6b33fc9 (fold the transition guard into the state-mutator CAS) |
| 02/03-publishers-task-queue | DONE | 6affd763 (StateEvent publishers, opt-in gate) |
| 02/04-publishers-plan-step-goal | DONE | 6affd763 + c0788bb0 (plan/step/goal publisher wiring) |

> Reconciliation note (2026-11-16, issue #156): rows above were under-reporting merged work; statuses updated from git (each SHA verified an ancestor of main). Tables were last refreshed d492867c (2026-11-14).

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
