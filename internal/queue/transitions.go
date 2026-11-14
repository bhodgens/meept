package queue

import (
	"errors"
	"strings"
)

// ErrInvalidTransition is returned when a requested state transition has no
// legal source state at all (a target the machine can never reach), as
// opposed to a job sitting in a state this edge does not start from — that
// case keeps returning ErrJobStateLocked, the error every existing caller
// already treats as "my attempt lost the race" (see Store.UpdateState).
var ErrInvalidTransition = errors.New("invalid job state transition")

// jobTransitions defines the allowed state transitions for a job. The map key
// is the current state, and the value is the set of legal next states.
//
// This map is THE authority for the queue state machine: Store mutators fold
// its edges into their SQL predicates (no check-then-write split), and the
// kanban projector (02/03) publishes a StateEvent per edge. Terminal states
// map to an empty slice — nothing leaves them.
var jobTransitions = map[JobState][]JobState{
	StatePending:    {StateClaimed, StateFailed},
	StateClaimed:    {StateProcessing, StateCompleted, StateFailed, StatePending},
	StateProcessing: {StateCompleted, StateFailed, StatePending, StateDead},
	StateFailed:     {StatePending, StateDead},
	StateCompleted:  {},
	StateDead:       {},
}

// queueAllows reports whether from -> to is a legal edge.
func queueAllows(from, to JobState) bool {
	allowed, ok := jobTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// legalSources returns the states that may transition to 'to', as parameter
// values for a bound `state IN (?, ?, ...)` predicate. Folding the edge set
// into the UPDATE's WHERE clause keeps the guard inside the same statement as
// the CAS, so no concurrent writer can slip between a check and a write.
//
// The returned slice is empty when nothing can reach 'to' — callers must then
// refuse instead of emitting `IN ()`, which is invalid SQL.
func legalSources(to JobState) []JobState {
	var sources []JobState
	for from, allowed := range jobTransitions {
		for _, s := range allowed {
			if s == to {
				sources = append(sources, from)
				break
			}
		}
	}
	return sources
}

// sourcePredicate renders the " AND state IN (?,?,...)" fragment for
// legalSources(to). It assumes a non-empty source set.
func sourcePredicate(sources []JobState) string {
	held := make([]string, len(sources))
	for i := range sources {
		held[i] = "?"
	}
	return " AND state IN (" + strings.Join(held, ", ") + ")"
}

// sourceArgs flattens the source states into query arguments.
func sourceArgs(sources []JobState) []any {
	args := make([]any, len(sources))
	for i, s := range sources {
		args[i] = string(s)
	}
	return args
}
