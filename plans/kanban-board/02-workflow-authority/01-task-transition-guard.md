# 02/01-task-transition-guard

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run `git add`.

## Parent and Dependencies
- Parent: `02-workflow-authority` (orchestrator)

## Scope
Implement the authority to validate and execute task state transitions.
1. Create `internal/task/transitions.go` containing a transition map and logic mirroring the pattern in `internal/plan/transitions.go` (specifically referencing `internal/plan/transitions.go:46`, `77`, `136`).
2. Implement `taskAllows(from, to string) bool` logic.
3. Integrate a guard into `internal/task/task.go` (e.g., in `SetState`) to check transitions before application.

## Estimated Context
60K tokens

## Interface Contract
Exposes the following logic for task state enforcement:
- `taskTransitions map[TaskState][]TaskState` (internal)
- `taskAllows(from, to string) bool` (internal)
- Integration: `task.SetState(to string) error` must call the guard.

## Tasks
1. **TDD: failing test for invalid transition**
   - `go test -v ./internal/task/ -run TestTask_InvalidTransition`
2. **Implementation: transitions.go and task.go**
   - Implement the transition map and `taskAllows` in `internal/task/transitions.go`.
   - Add the hook in `internal/task/task.go` to enforce the guard.
3. **Verification: transitions_test.go**
   - Test all valid/invalid transitions for the `TaskState` enum.

## Self-Verification Checklist
- [ ] Implementation mirrors `internal/plan/transitions.go` structure.
- [ ] Transitions are defined as a map of allowed 'to' states for each 'from' state.
- [ ] `SetState` in `task.go` correctly returns `ErrInvalidTransition` (or equivalent) when guarded.

## Review Checklist
- [ ] No debug prints or TODOs.
- [ ] Every new Set*/setter has a nil guard.
- [ ] No I/O under mutex.
- [ ] Uses `pkg/id.Generate()` for any new IDs.
