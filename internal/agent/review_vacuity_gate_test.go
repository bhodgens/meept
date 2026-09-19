package agent

// Gate-level pins for the plan-vacuity guard wired into heuristicReviewPasses
// (leaf 03). A vacuous planner step must NOT come back ReviewApproved from
// the trivial-task heuristic path — it routes to full review. A planner step
// with a substantive (structured) plan result keeps auto-approving.

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// TestReviewStep_VacuousPlanNotAutoApproved drives the full ReviewStep path:
// a planner-hint ("plan") step whose result is pure narration with no tool
// evidence must not be heuristic-approved ("trivial task, non-empty result").
// It must fall through to the full-review lane (which here errors because no
// reviewer registry entry exists — an error, never an approval).
func TestReviewStep_VacuousPlanNotAutoApproved(t *testing.T) {
	rm := newTestReviewManager(t)
	// Non-nil registry with no registered reviewer spec: the full-review
	// path fails in registry.Get — an ERROR, never an approval. With the
	// gate missing, this method returned (ReviewApproved, nil) from the
	// trivial-task heuristic without ever reaching the registry.
	rm.registry = NewAgentRegistry(RegistryConfig{Logger: testLogger()})

	step := &task.TaskStep{
		ID:       "step-vacuity-gate",
		TaskID:   "task-vacuity-gate",
		Sequence: 0,
		State:    task.StepCompleted,
		ToolHint: "plan",
		Result:   vacuityOffenderText,
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step in store: %v", err)
	}

	res, err := rm.ReviewStep(context.Background(), step, nil)

	if err == nil && res != nil && res.Status == ReviewApproved {
		t.Fatalf("vacuous planner step was AUTO-APPROVED: %+v", res)
	}
	if res != nil && res.Feedback == "Auto-approved (heuristic check: trivial task, non-empty result)" {
		t.Errorf("trivial-task heuristic approved a narration-only planner step: %+v", res)
	}
}

// TestReviewStep_SubstantivePlanStillAutoApproves pins no-regression: a
// planner-hint step whose result IS a real step-list plan (structured
// artifact) with no error still auto-approves via the trivial-task heuristic.
func TestReviewStep_SubstantivePlanStillAutoApproves(t *testing.T) {
	rm := newTestReviewManager(t)

	step := &task.TaskStep{
		ID:       "step-vacuity-substantive",
		TaskID:   "task-vacuity-substantive",
		Sequence: 0,
		State:    task.StepCompleted,
		ToolHint: "plan",
		Result: "Plan for the task:\n" +
			"1. Create hello.txt containing the word 'hello'\n" +
			"2. Verify the file contents\n" +
			"3. Report the full path",
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step in store: %v", err)
	}

	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res == nil || res.Status != ReviewApproved {
		t.Fatalf("substantive planner step must still auto-approve; got %+v", res)
	}
	if res.Feedback != "Auto-approved (heuristic check: trivial task, non-empty result)" {
		t.Errorf("expected trivial-task heuristic approval, got: %s", res.Feedback)
	}
}
