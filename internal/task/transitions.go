package task

import (
	"errors"
	"fmt"
	"slices"
)

// ErrInvalidTransition is returned when a task transition is not allowed.
var ErrInvalidTransition = errors.New("invalid task state transition")

// ErrInvalidTaskState is returned when a state STRING is not a member of the
// TaskState enum at all (a typo like "done"), as opposed to a legal enum value
// reached by an illegal edge. Both surfaces that accept untrusted state strings
// (the agent task-update tool, the RPC/HTTP/bus write paths) must validate the
// enum BEFORE consulting taskTransitions.
var ErrInvalidTaskState = errors.New("invalid task state")

// knownTaskStates is the human-readable enum listing used in error text, in
// declaration order.
const knownTaskStates = "pending, planning, awaiting_approval, executing, testing, completed, failed, rejected, cancelled"

// AllowedTransitionsText renders the legal edge set for an error message. The
// agent task-update tool has shipped this exact shape since bughunt M-1; every
// transport (RPC, HTTP, bus) reuses it so all surfaces report identical text.
// Exported so callers outside internal/task (e.g. internal/services) do not
// keep a second copy of the legal-edge list that can silently drift.
func AllowedTransitionsText() string {
	return "allowed transitions: pending -> planning/awaiting_approval/executing/cancelled; " +
		"planning -> awaiting_approval/executing/testing/cancelled/failed; " +
		"awaiting_approval -> executing/testing/cancelled/failed/rejected; " +
		"executing -> testing/completed/failed/rejected/cancelled; " +
		"testing -> completed/failed/rejected/cancelled/executing"
}

// validateTaskStateString is the single state-string gate for every write
// surface that takes a string: it rejects unknown enum values with an error
// carrying ErrInvalidTaskState and naming the known set.
func validateTaskStateString(s string) error {
	if !ValidTaskState(TaskState(s)) {
		return fmt.Errorf("%w %q: must be one of %s", ErrInvalidTaskState, s, knownTaskStates)
	}
	return nil
}

// taskTransitions is the SINGLE source of legal task state transitions.
var taskTransitions = map[TaskState][]TaskState{
	// pending -> planning, awaiting_approval, executing, cancelled
	StatePending: {StatePlanning, StateAwaitingApproval, StateExecuting, StateCancelled},
	// planning -> awaiting_approval, executing, testing, cancelled, failed
	StatePlanning: {StateAwaitingApproval, StateExecuting, StateTesting, StateCancelled, StateFailed},
	// awaiting_approval -> executing, testing, cancelled, failed, rejected
	StateAwaitingApproval: {StateExecuting, StateTesting, StateCancelled, StateFailed, StateRejected},
	// executing -> testing, completed, failed, rejected, cancelled
	StateExecuting: {StateTesting, StateCompleted, StateFailed, StateRejected, StateCancelled},
	// testing -> completed, failed, rejected, cancelled, executing
	StateTesting: {StateCompleted, StateFailed, StateRejected, StateCancelled, StateExecuting},
}

// taskAllows reports whether from -> to is a member of the taskTransitions map.
func taskAllows(from, to TaskState) bool {
	return slices.Contains(taskTransitions[from], to)
}
