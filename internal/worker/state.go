// Package worker provides a worker pool for processing jobs.
package worker

import "slices"

import "time"

// State represents the current state of a worker.
type State string

const (
	StateIdle       State = "idle"       // Waiting for work
	StateClaiming   State = "claiming"   // Attempting to claim a job
	StateProcessing State = "processing" // Executing a job
	StateComplete   State = "complete"   // Just finished a job
	StateError      State = "error"      // Encountered an error
	StateStopping   State = "stopping"   // Shutting down
	StateStopped    State = "stopped"    // Stopped
)

func (s State) String() string {
	return string(s)
}

// IsActive returns true if the worker is doing something.
func (s State) IsActive() bool {
	return s == StateClaiming || s == StateProcessing
}

// CanClaim returns true if the worker can claim new work.
// NOTE: This is a logical check for whether a worker is available to claim work.
// Workers in StateComplete or StateError MUST transition to StateIdle before
// actually claiming a job. The worker.tryProcessJob method handles this by
// explicitly calling setState(StateIdle) before setState(StateClaiming).
func (s State) CanClaim() bool {
	return s == StateIdle || s == StateComplete || s == StateError
}

// StateTransition represents a state change event.
type StateTransition struct {
	WorkerID  string
	FromState State
	ToState   State
	JobID     string // If applicable
	Error     error  // If applicable
	Timestamp time.Time
}

// ValidTransitions defines allowed state transitions.
var ValidTransitions = map[State][]State{
	StateIdle:       {StateClaiming, StateStopping},
	StateClaiming:   {StateProcessing, StateIdle, StateError, StateStopping},
	StateProcessing: {StateComplete, StateError, StateStopping},
	StateComplete:   {StateIdle, StateStopping},
	StateError:      {StateIdle, StateStopping},
	StateStopping:   {StateStopped},
	StateStopped:    {StateIdle},
}

// IsValidTransition checks if a state transition is allowed.
func IsValidTransition(from, to State) bool {
	allowed, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	return slices.Contains(allowed, to)
}
