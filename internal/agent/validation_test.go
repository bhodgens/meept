package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

func TestValidateCompletion_NotRequired(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	// Chat step should not require validation
	step := &task.TaskStep{
		ID:          "step-1",
		TaskID:      "task-1",
		Description: "Answer the user's question",
		ToolHint:    "chat",
		AgentID:     "chat",
		Result:      "Here is the answer",
	}

	result, err := rm.ValidateCompletion(context.Background(), step, "answer questions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != CompletionValid {
		t.Errorf("expected valid for non-required step, got %s", result.Status)
	}
}

func TestValidateCompletion_EmptyResult(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-1",
		TaskID:      "task-1",
		Description: "Fix the bug in server.go",
		ToolHint:    "code",
		Result:      "",
	}

	result, err := rm.ValidateCompletion(context.Background(), step, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != CompletionInvalid {
		t.Errorf("expected invalid for empty result, got %s", result.Status)
	}
	if len(result.Missing) == 0 {
		t.Error("expected missing items for empty result")
	}
}

func TestValidateCompletion_NoEvidence(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-1",
		TaskID:      "task-1",
		Description: "Fix the bug in server.go",
		ToolHint:    "code",
		Result:      "I fixed the bug by adding a nil check.",
	}

	result, err := rm.ValidateCompletion(context.Background(), step, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Code steps without evidence should be partial
	if result.Status != CompletionPartial {
		t.Errorf("expected partial for code step without evidence, got %s", result.Status)
	}
}

func TestValidateCompletion_WithEvidence(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-1",
		TaskID:      "task-1",
		Description: "Fix the nil check bug in server.go",
		ToolHint:    "code",
		Result:      "Fixed the nil check in server.go. The function now properly handles nil values.",
		Evidence: []models.Evidence{
			{Type: "file_exists", Subject: "server.go"},
		},
	}

	result, err := rm.ValidateCompletion(context.Background(), step, "Fix nil check bug in server.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != CompletionValid {
		t.Errorf("expected valid for step with evidence, got %s: %s", result.Status, result.Feedback)
	}
}

func TestValidateCompletion_WithClaims(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-1",
		TaskID:      "task-1",
		Description: "Fix the bug in server.go",
		ToolHint:    "code",
		Result:      "Fixed the bug",
		Claims:      []string{"Added nil check in server.go"},
	}

	result, err := rm.ValidateCompletion(context.Background(), step, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != CompletionValid {
		t.Errorf("expected valid for step with claims, got %s", result.Status)
	}
}

func TestValidateCompletion_LowTaskRelevance(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	step := &task.TaskStep{
		ID:          "step-1",
		TaskID:      "task-1",
		Description: "Fix the authentication middleware bug",
		ToolHint:    "code",
		Result:      "I did something completely unrelated to the task at hand.",
		Evidence: []models.Evidence{
			{Type: "file_exists", Subject: "random.go"},
		},
	}

	result, err := rm.ValidateCompletion(context.Background(), step, "Fix authentication middleware bug in auth.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be partial due to low relevance to task description
	if result.Status != CompletionPartial {
		t.Errorf("expected partial for low task relevance, got %s", result.Status)
	}
}

func TestValidateCompletion_AnalystSkips(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	// Analyst agent should skip validation
	step := &task.TaskStep{
		ID:          "step-1",
		TaskID:      "task-1",
		Description: "Analyze the data",
		ToolHint:    "analyze",
		AgentID:     "analyst",
		Result:      "Analysis complete",
	}

	result, err := rm.ValidateCompletion(context.Background(), step, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != CompletionValid {
		t.Errorf("expected valid for analyst agent, got %s", result.Status)
	}
}

func TestValidateCompletion_SkipValidationAgents(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})

	// Chat agent should skip validation
	step := &task.TaskStep{
		ID:          "step-1",
		TaskID:      "task-1",
		Description: "Have a conversation",
		ToolHint:    "code", // Even with code tool hint
		AgentID:     "chat",
		Result:      "",
	}

	result, err := rm.ValidateCompletion(context.Background(), step, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Chat agent skips validation entirely
	if result.Status != CompletionValid {
		t.Errorf("expected valid for chat agent, got %s", result.Status)
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
		contains []string
	}{
		{
			name:     "simple sentence",
			input:    "Fix the bug in server.go",
			expected: 3, // "fix", "bug", "server.go"
			contains: []string{"fix", "bug", "server.go"},
		},
		{
			name:     "only stop words",
			input:    "the a an is are was were",
			expected: 0,
		},
		{
			name:     "complex description",
			input:    "Implement user authentication with JWT tokens and session management",
			expected: 7,
			contains: []string{"implement", "user", "authentication"},
		},
		{
			name:     "empty",
			input:    "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keywords := extractKeywords(tt.input)
			if len(keywords) != tt.expected {
				t.Errorf("expected %d keywords, got %d: %v", tt.expected, len(keywords), keywords)
			}
			for _, kw := range tt.contains {
				found := false
				for _, k := range keywords {
					if k == kw {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected keyword %q not found in %v", kw, keywords)
				}
			}
		})
	}
}

func TestValidationResult(t *testing.T) {
	vr := &ValidationResult{
		Status:   CompletionPartial,
		Feedback: "Partially complete",
		Missing:  []string{"evidence"},
		Verified: []string{"file creation"},
	}

	if vr.Status != CompletionPartial {
		t.Errorf("expected partial status")
	}
	if len(vr.Missing) != 1 {
		t.Errorf("expected 1 missing item")
	}
	if len(vr.Verified) != 1 {
		t.Errorf("expected 1 verified item")
	}
}

func TestReviewManager_SetValidationPolicy(t *testing.T) {
	rm := NewReviewManager(ReviewManagerConfig{})
	policy := rm.GetValidationPolicy()
	if policy == nil {
		t.Fatal("expected default validation policy")
	}
	if !policy.Enabled {
		t.Error("expected validation enabled by default")
	}

	newPolicy := &ValidationPolicy{
		Enabled:            false,
		MaxValidationLoops: 5,
	}
	rm.SetValidationPolicy(newPolicy)

	if rm.GetValidationPolicy().Enabled {
		t.Error("expected validation disabled after set")
	}
	if rm.GetValidationPolicy().MaxValidationLoops != 5 {
		t.Error("expected max validation loops 5")
	}
}
