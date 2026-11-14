# 02/03-publishers-task-queue

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run `git add`.

## Parent and Dependencies
- Parent: `02-workflow-authority` (orchestrator)
- Dependency: `02-workflow-authority/02-queue-transition-guard`

## Scope
Implement event publication for task and queue state changes to ensure the Kanban board stays synchronized.
1. Implement `internal/kanban/events.go` with helper functions for creating `StateEvent` payloads.
2. Integrate `StateEvent` publication (C2) on `kanban.event.task.state` and `kanban.event.job.state` (as per master.md C2) for all valid transitions.
3. Implement coverage tests that walk the task and job state enums to ensure every state transition has a corresponding publisher path.

## Estimated Context
90K tokens

## Interface Contract
Exposes the following event payloads via `internal/bus.Topic[StateEvent]`:
- `kanban.event.task.state` (Payload: `StateEvent` - C1)
- `kanban.event.job.state` (Payload: `StateEvent` - C1)

## Tasks
1. **TDD: failing test for missing event on transition**
   - `go test -v ./internal/kanban/ -run TestPublishers_MissingTaskEvent`
2. **Implementation: events.go and wiring edits**
   - Implement `internal/kanban/events.go` helpers.
   - Add `StateEvent` publication to `internal/task/` and `internal/queue/` (via existing bus/event mechanisms).
3. **Verification: coverage_test.go**
   - Implement a test that iterates through all enum states and asserts that every valid transition triggers a `StateEvent` on the correct topic.

## Self-Verification Checklist
- [ ] Every state transition emits a `StateEvent` (C1) on the correct topic (C2).
- [ ] Events are published to the correct `internal/bus` topics.
- [s] One enum-coverage test is implemented.

## Review Checklist
- [ ] No debug prints or TODOs.
- [ ] No I/O under mutex.
- [ ] `enabled=false` path compiles and is byte-identical.
