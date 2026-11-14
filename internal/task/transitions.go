package task

import (
	"errors"
)

// ErrInvalidTransition is returned when a task transition is not allowed.
var ErrInvalidTransition = errors.New("invalid task state transition")

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
	for _, t := range taskTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}
