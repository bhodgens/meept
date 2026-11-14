# 02/02-queue-transition-guard

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run `git add`.

## Parent and Dependencies
- Parent: `02-workflow-authority` (orchestrator)

## Scope
Implement state transition guards for queue jobs to ensure valid lifecycle movements.
1. Create `internal/queue/transitions.go` containing a transition map for `JobState` (including dead-letter edges).
2. Implement `queueAllows(from, to string) bool` logic.
3. Integrate a guard in `internal/queue/store.go` — the state mutator is
   `Store.UpdateState` (`internal/queue/store.go:801`) and
   `Store.CompleteAttempt` (`internal/queue/store.go:866`); there is no
   `SetState` in this package. Ensure state changes respect the transition
   rules while preserving existing SQL CAS (Compare-And-Swap) logic.

## Estimated Context
60K tokens

## Interface Contract
Exposes the following logic for job lifecycle enforcement:
- `jobTransitions map[JobState][]JobState` (internal)
- `queueAllows(from, to string) bool` (internal)
- Integration: State transitions in `internal/queue/store.go` must validate against the guard before updating.

## Tasks
1. **TDD: failing test for invalid job transition**
   - `go test -v ./internal/queue/ -run TestQueue_InvalidTransition`
2. **Implementation: transitions.go and store.go**
   - Implement `jobTransitions` and `queueAllows` in `internal/queue/transitions.go`.
   - Add the guard hook into `internal/queue/store.go` (e.g., in `SetState` or `Claim` logic).
3. **Verification: transitions_test.go**
   - Test all valid/invalid transitions for the `JobState` enum, including `dead-letter` edges.

## Self-Verification Checklist
- [ ] Implementation preserves existing SQL CAS (Compare-And-Swap) for atomic updates.
- [ ] All valid state transitions are covered in the `jobTransitions` map.
- [ ] `UpdateState` and `CompleteAttempt` in `internal/queue/store.go` correctly block invalid transitions.

## Review Checklist
- [ ] No debug prints or TODOs.
- [ ] Every new Set*/setter has a nil guard.
- [ ] No I/O under mutex.
- [ ] No new external dependencies.
