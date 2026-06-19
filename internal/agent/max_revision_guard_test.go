package agent

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
	_ "modernc.org/sqlite"
)

func TestReviewPolicy_ExceedsMaxRevisions_Boundary(t *testing.T) {
	policy := DefaultReviewPolicy()
	policy.MaxRevisionCycles = 3

	tests := []struct {
		revisionCount int
		want          bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, true},
		{4, true},
	}
	for _, tt := range tests {
		step := &task.TaskStep{RevisionCount: tt.revisionCount}
		got := policy.ExceedsMaxRevisions(step)
		if got != tt.want {
			t.Errorf("ExceedsMaxRevisions(revisionCount=%d) = %v, want %v", tt.revisionCount, got, tt.want)
		}
	}
}

func TestReviewPolicy_ExceedsMaxRevisions_Disabled(t *testing.T) {
	policy := &ReviewPolicy{MaxRevisionCycles: 0}
	step := &task.TaskStep{RevisionCount: 100}
	if policy.ExceedsMaxRevisions(step) {
		t.Error("ExceedsMaxRevisions should return false when MaxRevisionCycles is 0")
	}
}

func newTestStepStore(t *testing.T) *task.StepStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store, err := task.NewStepStore(db, nil)
	if err != nil {
		t.Fatalf("failed to create step store: %v", err)
	}
	return store
}

func TestReviewStep_MaxRevisions_ReturnsNeedsInfo(t *testing.T) {
	policy := DefaultReviewPolicy()
	policy.MaxRevisionCycles = 2

	stepStore := newTestStepStore(t)

	rm := NewReviewManager(ReviewManagerConfig{
		Policy:    policy,
		StepStore: stepStore,
	})

	spec := &TaskSpec{
		TaskID: "task-guard",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Fix the bug",
				AcceptanceCriteria: "Bug is fixed and tests pass.",
				Required:           true,
			},
		},
	}

	step := &task.TaskStep{
		ID:            "step-guard-test",
		TaskID:        "task-guard",
		Description:   "Fix the bug",
		ToolHint:      "fix",
		AgentID:       "debugger",
		RevisionCount: 2,
	}

	result, err := rm.ReviewStep(context.TODO(), step, spec)
	if err != nil {
		t.Fatalf("ReviewStep failed: %v", err)
	}
	if result.Status != ReviewNeedsInfo {
		t.Errorf("expected ReviewNeedsInfo when max revisions exceeded, got %s", result.Status)
	}
	if result.Feedback == "" {
		t.Error("expected non-empty feedback explaining human intervention needed")
	}
	if !strings.Contains(result.Feedback, "Bug is fixed and tests pass.") {
		t.Errorf("feedback should include spec acceptance criteria, got: %s", result.Feedback)
	}
}

func TestReviewStep_MaxRevisions_WithoutSpec(t *testing.T) {
	policy := DefaultReviewPolicy()
	policy.MaxRevisionCycles = 2

	stepStore := newTestStepStore(t)

	rm := NewReviewManager(ReviewManagerConfig{
		Policy:    policy,
		StepStore: stepStore,
	})

	step := &task.TaskStep{
		ID:            "step-guard-nospec",
		TaskID:        "task-guard",
		Description:   "Fix the bug",
		ToolHint:      "fix",
		AgentID:       "debugger",
		RevisionCount: 2,
	}

	result, err := rm.ReviewStep(context.TODO(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep failed: %v", err)
	}
	if result.Status != ReviewNeedsInfo {
		t.Errorf("expected ReviewNeedsInfo when max revisions exceeded, got %s", result.Status)
	}
	if !strings.Contains(result.Feedback, "Maximum revision cycles (2) exceeded") {
		t.Errorf("feedback should mention max cycles, got: %s", result.Feedback)
	}
}
