package githubsvc

import (
	"errors"
	"time"
)

// CIRef identifies the CI wait a parked github_ci_wait targets, so a
// scheduler can re-arm exactly one owner/repo/PR combination.
type CIRef struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

// ReArmSink is the schedule seam a parked CI wait hands back to the daemon:
// ScheduleReArm records that CIRef should be re-checked at the given time
// under the given waitID. The in-memory recording implementation
// (InstructionReArmSink, a real scheduler instruction row) is owned by a
// later leaf; this interface pins the contract the builtin tools program
// against.
type ReArmSink interface {
	ScheduleReArm(ref CIRef, at time.Time, waitID string) error
}

// ErrCIWaitParked is returned by github_ci_wait when the wait gives up at
// its deadline without reaching a terminal CI outcome: the tool has parked
// the wait (re-armed the sink) and the caller can resume from the returned
// waitID instead of blocking again.
var ErrCIWaitParked = errors.New("githubsvc: ci wait parked; re-armed via schedule")
