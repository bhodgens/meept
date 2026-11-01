package turnjournal

import (
	"context"
	"fmt"
)

// StepRef identifies one step of a turn's step list for the resume
// planner: the stable step ID plus its position.
type StepRef struct {
	StepID    string
	StepIndex int
}

// ResumePlan is the skip/next plan for a resuming turn (master contract
// C4). Zero-value plan: no skip set, next = 0.
type ResumePlan struct {
	// Skip maps completed StepID -> the recorded result blob (research
	// F3: restore the journaled result, never re-invoke).
	Skip map[string][]byte
	// Next is the position of the first step without a step_completed
	// row (len(steps) when every step is already done).
	Next int
	// SkipCount is len(Skip), carried for logging.
	SkipCount int
}

// PlanResume consumes the journal's CompletedSteps for turnID and
// computes which steps of steps to skip and where execution resumes.
// Steps whose ID appears in the journal's step_completed rows are
// skipped (their recorded result rides the plan); Next is the first
// position whose step is NOT journaled (matching by position when the
// ID is unknown, so a reordered list degrades to "execute from the
// gap"). A journal read failure is an ERROR (not best-effort): the
// caller cannot decide resume position without the journal, and
// guessing would re-execute recorded steps (research F4 violation).
func PlanResume(ctx context.Context, store JournalStore, turnID string, steps []StepRef) (skip map[string][]byte, next int, err error) {
	if store == nil {
		return nil, 0, fmt.Errorf("turnjournal: plan resume: nil store")
	}
	completed, err := store.CompletedSteps(ctx, turnID)
	if err != nil {
		return nil, 0, fmt.Errorf("turnjournal: plan resume %s: %w", turnID, err)
	}

	skip = make(map[string][]byte)
	next = len(steps)
	for i, ref := range steps {
		if payload, ok := completed[ref.StepID]; ok {
			skip[ref.StepID] = payload
			continue
		}
		next = i
		break
	}
	return skip, next, nil
}
