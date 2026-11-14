# 02/01-task-transition-guard

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run `git add`.

## Parent and Dependencies
- Parent: `02-workflow-authority` (orchestrator)

## Scope
Implement the authority to validate and execute task state transitions.
1. Create `internal/task/transitions.go` containing a transition map and logic mirroring the pattern in `internal/plan/transitions.go` (specifically referencing `internal/plan/transitions.go:46`, `77`, `136`).
2. Implement `taskAllows(from, to string) bool` logic.
3. Integrate a guard into `internal/task/task.go` `SetState`.

**BLAST-RADIUS NOTE (do not skip):** `Task.SetState` today is
`func (t *Task) SetState(state TaskState)` with NO error return
(`internal/task/task.go:188`). Guarding requires changing the signature to
return `error`. That touches ~20 call sites: `internal/agent/strategic.go`
(8 sites), `internal/task/registry.go` (5 sites),
`internal/tools/builtin/task.go:360`. Options, in order of preference:
(a) add `SetStateChecked(to TaskState) error` that wraps the guard + calls
SetState, migrate call sites incrementally, and keep plain `SetState` as a
deprecated passthrough that logs a guard violation; or (b) change the
signature and fix all call sites in this leaf. Choose (a) unless the
call-site count at execution time is small. Record the choice in the
completion-notes for the orchestrator. Files beyond transitions.go/task.go
may be touched ONLY for this call-site migration; list them explicitly in
the report.

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
