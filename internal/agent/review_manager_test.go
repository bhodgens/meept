package agent

// Tests for the "honest completion states" contract (leaf 02 of
// chat-dispatch-ux):
//   - error steps are never auto-approved (Task 1),
//   - the heuristic gate cannot pass error-shaped results (Task 2),
//   - a task with failed steps finalizes as StateFailed with the error text
//     in the task.completed payload (Task 3).

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// newTestReviewManager mirrors the setup used by max_revision_guard_test.go:
// a real in-memory step store and the default review policy.
func newTestReviewManager(t *testing.T) *ReviewManager {
	t.Helper()
	return NewReviewManager(ReviewManagerConfig{
		StepStore: newTestStepStore(t),
	})
}

// TestReviewManager_ErrorStepNeverApproved: a step whose execution produced
// an error must come back ReviewRejected (never ReviewApproved), and the
// step must be marked StepFailed in the store.
func TestReviewManager_ErrorStepNeverApproved(t *testing.T) {
	rm := newTestReviewManager(t)
	step := &task.TaskStep{
		ID:       "step-err-1",
		TaskID:   "task-err-1",
		Sequence: 0,
		State:    task.StepCompleted,
		Result:   "error: file_write failed: no path specified",
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step in store: %v", err)
	}
	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status == ReviewApproved {
		t.Fatalf("error step was APPROVED: %+v", res)
	}
	if res.Status != ReviewRejected {
		t.Errorf("status = %v, want ReviewRejected", res.Status)
	}
	if !strings.Contains(res.Feedback, "execution error") {
		t.Errorf("feedback should name the execution error, got %q", res.Feedback)
	}

	// The step itself must be flipped to failed in the store.
	persisted, err := rm.stepStore.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != task.StepFailed {
		t.Errorf("step state = %q, want %q", persisted.State, task.StepFailed)
	}
}

// TestReviewManager_HeuristicRejectsErrorResult: heuristicReviewPasses must
// not wave through error-shaped results regardless of their length.
func TestReviewManager_HeuristicRejectsErrorResult(t *testing.T) {
	rm := newTestReviewManager(t)
	step := &task.TaskStep{Result: "error: unable to open database file"}
	if rm.heuristicReviewPasses(step) {
		t.Fatal("heuristic passed an error-shaped result")
	}
}
