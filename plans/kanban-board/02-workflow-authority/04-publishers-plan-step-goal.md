# 02/04-publishers-plan-step-goal

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run `git add`.

## Parent and Dependencies
- Parent: `02-workflow-authority` (orchestrator)
- Dependency: `02-workflow-authority/03-publishers-task-queue`

## Scope
Implement event publication for plan, step, and goal state changes.
1. Add `kanban.event.plan.state` and associated state event publishing to the existing plan/step/goal lifecycle.
2. Ensure the addition does NOT modify the existing `plan.phase_state` field.
3. Implement coverage tests to ensure every state transition for these entities triggers a `StateEvent` (C1) on the correct topic (C2).

## Estimated Context
90K tokens

## Interface Contract
Exposes the following event payloads via `internal/bus.Topic[StateEvent]`:
- `kanban.event.plan.state` (Payload: `StateEvent` - C1)
- `kanban.event.step.state` (Payload: `StateEvent` - C1)
- `kanban.event.goal.state` (Payload: `StateEvent` - C1)

## Tasks
1. **TDD: failing test for missing plan event**
   - `go test -v ./internal/plan/ -run TestPublishers_MissingPlanEvent`
2. **Implementation: event wiring**
   - Add `StateEvent` publishing to `internal/plan/`, the step machine in `internal/task/step.go`, and the goal machine in `internal/employee/goal.go` (no internal/step or internal/goal packages exist).
   - Ensure `kanban.event.plan.state` is added without altering `plan.phase_state`.
3. **Verification: coverage_test.go**
   - Implement a test that iterates through the plan/step/goal enums and asserts that every valid transition triggers a `StateEvent`.

## Self-Verification Checklist
- [ ] `kanban.event.plan.state` is added WITHOUT changing `plan.phase_state`.
- [ ] Every state transition emits a `StateEvent` (C1) on the correct topic.
- [ ] One enum-coverage test is implemented.

## Review Checklist
- [ ] No debug prints or TODOs.
- [ ] No I/O under mutex.
- [ ] `enabled=false` path compiles and is byte-identical.
