package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestBuildReviewPrompt_IncludesSpec(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-test-0-1234",
		TaskID:      "task-test",
		Description: "Implement the login handler",
		ToolHint:    "code",
		AgentID:     "coder",
		Result:      `{"success": true, "result": "login.go created with handler function"}`,
		Sequence:    0,
	}

	spec := &TaskSpec{
		TaskID: "task-test",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Implement the login handler",
				AcceptanceCriteria: "Code must compile without errors. Changes must be syntactically correct. No regressions in existing functionality.",
				Required:           true,
			},
		},
	}

	prompt := rm.buildReviewPrompt(step, spec)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "ACCEPTANCE CRITERIA") {
		t.Error("prompt should contain ACCEPTANCE CRITERIA section when spec is provided")
	}
	if !strings.Contains(prompt, "Code must compile without errors") {
		t.Error("prompt should contain the specific acceptance criterion text")
	}
}

func TestBuildReviewPrompt_NoSpec(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-test-0-1234",
		TaskID:      "task-test",
		Description: "Implement the login handler",
		ToolHint:    "code",
		AgentID:     "coder",
		Result:      `{"success": true}`,
		Sequence:    0,
	}

	prompt := rm.buildReviewPrompt(step, nil)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if strings.Contains(prompt, "ACCEPTANCE CRITERIA") {
		t.Error("prompt should NOT contain ACCEPTANCE CRITERIA section when spec is nil")
	}
	if !strings.Contains(prompt, "REVIEW TASK STEP") {
		t.Error("prompt should contain REVIEW TASK STEP header")
	}
}

func TestBuildReviewPrompt_SpecForDifferentStep(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-test-1-1234",
		TaskID:      "task-test",
		Description: "Write unit tests",
		ToolHint:    "code",
		AgentID:     "coder",
		Result:      `{"success": true}`,
		Sequence:    1,
	}

	spec := &TaskSpec{
		TaskID: "task-test",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Implement the login handler",
				AcceptanceCriteria: "Code compiles. No regressions.",
				Required:           true,
			},
			{
				StepSequence:       1,
				Description:        "Write unit tests",
				AcceptanceCriteria: "Tests cover login handler. All tests pass.",
				Required:           true,
			},
		},
	}

	prompt := rm.buildReviewPrompt(step, spec)
	if !strings.Contains(prompt, "Tests cover login handler") {
		t.Error("prompt should contain the acceptance criteria for step sequence 1")
	}
	if strings.Contains(prompt, "Code compiles. No regressions.") {
		t.Error("prompt should NOT contain criteria for step sequence 0 (different step)")
	}
}
