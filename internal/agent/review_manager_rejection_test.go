package agent

// Regression tests for the 2026-09-06 live-run finding: a review rejection
// left the original step in "reviewing" (the SetState(rejected) write was
// overwritten by the subsequent full-row Update carrying stale state), so
// the created revision step depended on a permanently non-terminal original
// and never scheduled. Also: revisions were created for already-failed
// (execution-error) steps, which can never be promoted.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// TestReviewManager_RejectionPersistsRejectedState: after a ReviewRejected
// result, the original step's persisted state must be "rejected" (not the
// stale "reviewing" the old two-write sequence restored).
func TestReviewManager_RejectionPersistsRejectedState(t *testing.T) {
	rm := newTestReviewManager(t)
	step := &task.TaskStep{
		ID:       "step-rej-state",
		TaskID:   "task-rej-state",
		Sequence: 0,
		State:    task.StepCompleted,
		Result:   "wrote the wrong thing entirely",
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	res, err := rm.HandleReviewResult(context.Background(), step.ID, &ReviewResult{
		Status:     ReviewRejected,
		Feedback:   "no plan was generated",
		Confidence: 0.95,
	}, nil)
	if err != nil {
		t.Fatalf("HandleReviewResult: %v", err)
	}

	persisted, err := rm.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != task.StepRejected {
		t.Fatalf("persisted state = %q, want %q (stale in-memory state must not overwrite the rejection)", persisted.State, task.StepRejected)
	}
	if persisted.RevisionCount != 1 {
		t.Errorf("revision count = %d, want 1", persisted.RevisionCount)
	}
	if len(res) != 1 {
		t.Fatalf("revisions created = %d, want 1", len(res))
	}

	// The revision must depend only on terminal deps so promotion is possible.
	rev := res[0]
	for _, dep := range rev.DependsOn {
		if dep != step.ID {
			t.Errorf("unexpected dependency %q", dep)
		}
	}
	// Original is now rejected — rejected is NOT successfully-terminal, so
	// PromoteReadySteps must NOT promote the revision while the original is
	// merely rejected... but the scheduler treats rejected+revised originals
	// via the revision chain; assert the store reflects terminal-rejected so
	// downstream state checks are coherent.
	if persisted.State.IsSuccessfullyTerminal() {
		t.Errorf("rejected should not be successfully-terminal")
	}
}

// TestReviewManager_ErrorStepNoRevision: a step that failed execution
// (stepHasError) must NOT spawn a revision step — a revision depending on a
// failed original can never be promoted (stranded pending row, 2026-09-06
// rev-1002 finding).
func TestReviewManager_ErrorStepNoRevision(t *testing.T) {
	rm := newTestReviewManager(t)
	step := &task.TaskStep{
		ID:       "step-err-rev",
		TaskID:   "task-err-rev",
		Sequence: 1,
		State:    task.StepCompleted,
		Result:   "error: LLM call failed: quota limit exceeded",
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	// ReviewStep fails the errored step and returns ReviewRejected.
	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status != ReviewRejected {
		t.Fatalf("status = %v, want ReviewRejected", res.Status)
	}
	if !strings.Contains(res.Feedback, "execution error") {
		t.Errorf("feedback should name the execution error, got %q", res.Feedback)
	}

	revisions, err := rm.HandleReviewResult(context.Background(), step.ID, res, nil)
	if err != nil {
		t.Fatalf("HandleReviewResult: %v", err)
	}
	if len(revisions) != 0 {
		t.Fatalf("revisions created for failed step = %d, want 0 (failed deps can never promote)", len(revisions))
	}

	persisted, err := rm.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != task.StepFailed {
		t.Errorf("persisted state = %q, want failed (terminal failure must stick)", persisted.State)
	}
}
