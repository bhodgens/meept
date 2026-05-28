package agent

import (
	"strings"
	"testing"
)

func TestBuildRevisionContext(t *testing.T) {
	result := &ReviewResult{
		Status:   ReviewRejected,
		Feedback: "The login handler does not validate input. Missing error handling for edge cases.",
		Issues: []string{
			"No input validation on email field",
			"Missing error handling for database connection failure",
			"No rate limiting on login attempts",
		},
		Confidence: 0.85,
	}

	spec := &TaskSpec{
		TaskID: "task-test",
		Criteria: []StepCriterion{
			{
				StepSequence:       0,
				Description:        "Implement the login handler",
				AcceptanceCriteria: "Code must compile without errors. No regressions in existing functionality.",
				Required:           true,
			},
		},
	}

	context := BuildRevisionContext(result, spec)
	if context == "" {
		t.Fatal("expected non-empty revision context")
	}
	if !strings.Contains(context, "PREVIOUS REVIEW FEEDBACK") {
		t.Error("revision context should contain PREVIOUS REVIEW FEEDBACK section")
	}
	if !strings.Contains(context, "No input validation on email field") {
		t.Error("revision context should contain the specific issue text")
	}
	if !strings.Contains(context, "ORIGINAL ACCEPTANCE CRITERIA") {
		t.Error("revision context should contain ORIGINAL ACCEPTANCE CRITERIA section")
	}
	if !strings.Contains(context, "Code must compile without errors") {
		t.Error("revision context should contain the acceptance criteria text")
	}
}

func TestBuildRevisionContext_NilSpec(t *testing.T) {
	result := &ReviewResult{
		Status:   ReviewRejected,
		Feedback: "Fix the bugs",
		Issues:   []string{"bug 1", "bug 2"},
	}

	context := BuildRevisionContext(result, nil)
	if !strings.Contains(context, "PREVIOUS REVIEW FEEDBACK") {
		t.Error("revision context should contain feedback section even without spec")
	}
	if strings.Contains(context, "ORIGINAL ACCEPTANCE CRITERIA") {
		t.Error("revision context should NOT contain acceptance criteria when spec is nil")
	}
}

func TestBuildRevisionContext_NilResult(t *testing.T) {
	context := BuildRevisionContext(nil, nil)
	if context != "" {
		t.Errorf("expected empty context for nil result, got %q", context)
	}
}
