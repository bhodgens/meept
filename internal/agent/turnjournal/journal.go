// Package turnjournal implements the turn-level durable journal: an
// append-only SQLite step ledger over turnjournal.db (parks.db
// conventions) so a daemon killed mid-turn resumes at the NEXT STEP
// instead of re-running the job from step 1.
//
// Design basis: docs/research/turn-journal-findings.md. D1 pins the
// granularity (journal at STEP boundaries, one row per step, not full
// event-sourcing of every LLM call); F3 pins the reuse rule (a
// journaled step_completed result is returned on replay, never
// re-invoked); F6 pins the retention story (PruneClosed for closed
// runs). The journal records step boundaries only: it never replays
// LLM outputs into new executions and never duplicates queue-side
// completion dedup (finding F4).
//
// This file holds the record types; store_sqlite.go the SQLite
// store; store.go the consumer-facing JournalStore interface.
package turnjournal

import "time"

// RecordKind identifies which step-boundary event a journal row
// records.
type RecordKind string

// The four journal record kinds (master contract C1).
const (
	// KindRunStarted marks a turn's first step START. Its row carries
	// the class payload blob in ResultPayload so boot rehydrate can
	// requeue without reconstructing history (mirrors the park store's
	// persisted turn_payload).
	KindRunStarted RecordKind = "run_started"
	// KindStepStarted marks the beginning of a step.
	KindStepStarted RecordKind = "step_started"
	// KindStepCompleted marks a step's completion; ResultPayload is the
	// opaque JSON result blob (the only kind with a non-nil payload).
	KindStepCompleted RecordKind = "step_completed"
	// KindRunClosed is the terminal write of a completed job. An
	// interrupted job leaves a dangling open run (no run_closed row).
	KindRunClosed RecordKind = "run_closed"
)

// Record is one journal row: a turn/step boundary event.
type Record struct {
	// TurnID is the task/turn identifier (task ID from the queue).
	TurnID string
	// SessionID is the owning session.
	SessionID string
	// StepID is the stable step identifier from the task's step list.
	// Empty for run_started/run_closed.
	StepID string
	// StepIndex is the step's position in the turn.
	StepIndex int
	// Kind selects which boundary event this row records.
	Kind RecordKind
	// ResultPayload is an opaque JSON blob. step_completed carries the
	// step result (the schema CHECK requires non-nil there);
	// run_started carries the class payload blob so leaf 03's
	// rehydrate can requeue without reconstructing history (mirrors
	// the park store's persisted turn_payload). Other kinds: nil.
	ResultPayload []byte
	// Timestamp is the event time (stored as Unix nanos in created_at).
	Timestamp time.Time
}

// OpenRun is a turn whose journal has no run_closed row: a turn that
// was in flight when the daemon last stopped.
type OpenRun struct {
	TurnID      string
	SessionID   string
	TurnPayload []byte // the class payload persisted at run start
}

// valid reports whether k is one of the four frozen record kinds.
func (k RecordKind) valid() bool {
	switch k {
	case KindRunStarted, KindStepStarted, KindStepCompleted, KindRunClosed:
		return true
	}
	return false
}
