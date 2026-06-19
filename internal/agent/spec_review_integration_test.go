package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestRevisionStep_ContainsFeedbackContext(t *testing.T) {
	originalStep := &task.TaskStep{
		ID:            "step-rev-0-1234",
		TaskID:        "task-rev",
		Description:   "implement the auth middleware",
		ToolHint:      "code",
		AgentID:       "coder",
		Sequence:      0,
		RevisionCount: 1,
	}

	result := &ReviewResult{
		Status:   ReviewRejected,
		Feedback: "Missing error handling for expired tokens",
		Issues:   []string{"No error handling for expired tokens", "Missing logging"},
	}

	spec := &TaskSpec{
		TaskID: "task-rev",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				AcceptanceCriteria: "Code must compile without errors. No regressions.",
				Required:           true,
			},
		},
	}

	revisionContext := BuildRevisionContext(result, spec)
	revision := task.CreateRevisionWithContext(originalStep, result.Feedback, revisionContext)

	if revision.AccumulatedContext == "" {
		t.Fatal("revision step should have accumulated context from review feedback")
	}
	if !strings.Contains(revision.AccumulatedContext, "No error handling for expired tokens") {
		t.Error("revision context should contain the specific issue from review")
	}
	if !strings.Contains(revision.AccumulatedContext, "Code must compile without errors") {
		t.Error("revision context should contain the original acceptance criteria")
	}
	if !strings.Contains(revision.AccumulatedContext, "PREVIOUS REVIEW FEEDBACK") {
		t.Error("revision context should contain PREVIOUS REVIEW FEEDBACK header")
	}
	if !strings.Contains(revision.AccumulatedContext, "ORIGINAL ACCEPTANCE CRITERIA") {
		t.Error("revision context should contain ORIGINAL ACCEPTANCE CRITERIA header")
	}
}

func TestEndToEnd_SpecGenerationToRevisionContext(t *testing.T) {
	// Step 1: Generate spec from planned steps
	steps := []*task.TaskStep{
		task.NewTaskStep("task-e2e", "Implement JWT token validation", 0).WithToolHint("code"),
		task.NewTaskStep("task-e2e", "Write tests for JWT validation", 1).WithToolHint("code"),
	}

	spec := GenerateSpecFromSteps(steps)
	if len(spec.Criteria) != 2 {
		t.Fatalf("expected 2 criteria, got %d", len(spec.Criteria))
	}

	// Step 2: Build review prompt for step 0 with spec
	rm := NewReviewManager(ReviewManagerConfig{})
	step0 := steps[0]
	step0.Result = `{"success": true, "result": "jwt_validation.go created"}`

	prompt := rm.buildReviewPrompt(step0, spec)
	if !strings.Contains(prompt, "ACCEPTANCE CRITERIA") {
		t.Error("review prompt should contain acceptance criteria for step 0")
	}

	// Step 3: Simulate rejection
	reviewResult := &ReviewResult{
		Status:   ReviewRejected,
		Feedback: "Token expiration check is missing",
		Issues:   []string{"No token expiration check", "Missing edge case for malformed tokens"},
	}

	// Step 4: Build revision context
	revisionContext := BuildRevisionContext(reviewResult, spec)
	if !strings.Contains(revisionContext, "No token expiration check") {
		t.Error("revision context should contain rejection issues")
	}
	if !strings.Contains(revisionContext, "ORIGINAL ACCEPTANCE CRITERIA") {
		t.Error("revision context should contain original acceptance criteria")
	}

	// Step 5: Create revision step with context
	step0.RevisionCount = 1
	revision := task.CreateRevisionWithContext(step0, reviewResult.Feedback, revisionContext)
	if revision.AccumulatedContext == "" {
		t.Error("revision should have accumulated context")
	}

	// Step 6: Store spec in task and verify round-trip
	tsk := task.NewTask("e2e-test", "end-to-end spec test")
	StoreSpecInTask(tsk, spec)
	extracted := ExtractSpecFromTask(tsk)
	if extracted == nil {
		t.Fatal("spec round-trip through task metadata failed")
	}
	if len(extracted.Criteria) != 2 {
		t.Errorf("extracted spec has %d criteria, want 2", len(extracted.Criteria))
	}
}

func TestEndToEnd_MaxRevisionGuard(t *testing.T) {
	policy := DefaultReviewPolicy()
	policy.MaxRevisionCycles = 2

	stepStore := newTestStepStore(t)

	rm := NewReviewManager(ReviewManagerConfig{
		Policy:    policy,
		StepStore: stepStore,
	})

	spec := &TaskSpec{
		TaskID: "task-guard-e2e",
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
		ID:            "step-guard-e2e-0-1234",
		TaskID:        "task-guard-e2e",
		Description:   "Fix the bug",
		ToolHint:      "fix",
		AgentID:       "debugger",
		RevisionCount: 2,
	}

	// Create the step in the store so ReviewStep's SetState can query it
	if err := stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step in store: %v", err)
	}

	result, err := rm.ReviewStep(context.TODO(), step, spec)
	if err != nil {
		t.Fatalf("ReviewStep failed: %v", err)
	}
	if result.Status != ReviewNeedsInfo {
		t.Errorf("expected NeedsInfo at max revisions, got %s", result.Status)
	}
	if !strings.Contains(result.Feedback, "Maximum revision cycles (2) exceeded") {
		t.Errorf("feedback should mention max cycles, got: %s", result.Feedback)
	}
	if !strings.Contains(result.Feedback, "Bug is fixed and tests pass") {
		t.Error("feedback should include the original acceptance criteria")
	}
}

