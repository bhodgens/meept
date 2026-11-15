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
// This map is THE authority for the queue state machine: every state-mutating
// SQL statement folds its `state IN (...)` predicate from legalSources() via
// predicateFor() (no hand-maintained literal lists), and the kanban projector
// (02/03) publishes a StateEvent per edge. Terminal states map to an empty
// slice — nothing leaves them.
//
// TRUTH (issue #134) — the three edges below are DELIBERATE, load-bearing
// extensions of the minimal happy-path graph, chosen as Option A (extend the
// map to describe reality) over Option B (conform the SQL to the minimal map):
//
//   - processing -> processing (self-edge): MarkProcessing is an idempotent
//     re-mark. A worker whose processing-ack was lost, or that re-enters the
//     processing window, re-stamps its own live attempt. Removing the edge
//     would make a benign retry look like a lost race.
//   - failed -> failed (self-edge): the token-LESS operator re-fail path
//     (queue.fail, HTTP force-fail) of an already-failed attempt is an
//     idempotent success, and the fail path's idempotent-retry branch reads
//     the row back as 'failed' — which is only reachable if failed->failed
//     is admitted.
//   - pending -> dead: retry-budget exhaustion (retryCount >= maxRetries) is
//     reachable from a 'pending' row — e.g. a MaxRetries(0) job failed
//     straight out of pending — so the fail path must be able to dead-letter
//     from pending, not just from in-flight states.
//   - claimed -> dead: same exhaustion path from a CLAIMED row — a MaxRetries(0)
//     job that was claimed then failed dead-letters without ever reaching
//     'processing' (TestRequeue_GiveUpDeadLettersViaExistingPath).
//
// Every edge here is load-bearing: the mutator SQL admits it, the projector
// publishes it, and TestMutatorPredicatesMatchTransitions / the self-edge
// pins fail if any side drifts.
var jobTransitions = map[JobState][]JobState{
	StatePending:    {StateClaimed, StateFailed, StateDead},
	StateClaimed:    {StateProcessing, StateCompleted, StateFailed, StatePending, StateDead},
	StateProcessing: {StateCompleted, StateFailed, StatePending, StateDead, StateProcessing},
	StateFailed:     {StatePending, StateDead, StateFailed},
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

// predicateFor returns the canonical folded predicate for a target state: the
// " AND state IN (?,?,...)" fragment whose source set is exactly what
// legalSources(target) yields from jobTransitions. Every SQL mutator uses this
// (not a hand-maintained literal list) so the edge table and the SQL cannot
// disagree; TestMutatorPredicatesMatchTransitions pins each mutator's emitted
// fragment against this function.
//
// Panics if target has no legal source — that is a programming error (a state
// no mutator can reach), and emitting `IN ()` would be invalid SQL. Callers
// that accept an arbitrary target (UpdateState) guard with legalSources()
// first and return ErrInvalidTransition.
func predicateFor(target JobState) string {
	sources := legalSources(target)
	if len(sources) == 0 {
		panic("queue: no legal source for state " + string(target))
	}
	return sourcePredicate(sources)
}

// predicateArgs returns the bound arguments for a predicateFor(target)
// fragment, in placeholder order.
func predicateArgs(target JobState) []any {
	return sourceArgs(legalSources(target))
}

// sourceArgs flattens the source states into query arguments.
func sourceArgs(sources []JobState) []any {
	args := make([]any, len(sources))
	for i, s := range sources {
		args[i] = string(s)
	}
	return args
}
