package skills

import (
	"context"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// Test helper to create a resolver with test providers config
func testResolver() *llm.Resolver {
	cfg := &llm.ProvidersConfig{
		Model:      "provider1/model-a",
		SmallModel: "provider1/model-b",
		Providers: map[string]llm.ProviderConfig{
			"provider1": {
				API: "openai",
				Options: llm.ProviderOptionsConfig{
					BaseURL: "http://localhost:11434/v1",
				},
				Models: map[string]llm.ModelDef{
					"model-a": {
						Name:         "model-a",
						Capabilities: []string{"code", "reasoning"},
						InputCost:    1.0,
						OutputCost:   2.0,
						ContextLimit: 128000,
						MaxOutput:    4096,
						Temperature:  0.7,
					},
					"model-b": {
						Name:         "model-b",
						Capabilities: []string{"code"},
						InputCost:    0.5,
						OutputCost:   1.0,
						ContextLimit: 32000,
						MaxOutput:    2048,
						Temperature:  0.5,
					},
				},
			},
			"provider2": {
				API: "openai",
				Options: llm.ProviderOptionsConfig{
					BaseURL: "https://api.example.com/v1",
				},
				Models: map[string]llm.ModelDef{
					"model-x": {
						Name:         "model-x",
						Capabilities: []string{"code", "reasoning", "tool_use"},
						InputCost:    3.0,
						OutputCost:   15.0,
						ContextLimit: 200000,
						MaxOutput:    8192,
						Temperature:  0.7,
					},
				},
			},
		},
	}

	return llm.NewResolver(cfg, nil)
}

func TestExecutor_NewExecutor(t *testing.T) {
	resolver := testResolver()

	exec := NewExecutor(resolver)
	if exec == nil {
		t.Fatal("NewExecutor returned nil")
	}
}

func TestExecutor_CanExecute(t *testing.T) {
	resolver := testResolver()
	exec := NewExecutor(resolver)

	// Skill with satisfiable requirements
	skill := &Skill{
		Name:     "test-skill",
		Requires: []string{"code"},
	}

	if !exec.CanExecute(skill) {
		t.Error("CanExecute should return true for satisfiable requirements")
	}

	// Skill with unsatisfiable requirements
	impossible := &Skill{
		Name:     "impossible",
		Requires: []string{"magic", "teleportation"},
	}

	if exec.CanExecute(impossible) {
		t.Error("CanExecute should return false for unsatisfiable requirements")
	}

	// Nil skill
	if exec.CanExecute(nil) {
		t.Error("CanExecute should return false for nil skill")
	}
}

func TestExecutor_GetModelForSkill(t *testing.T) {
	resolver := testResolver()
	exec := NewExecutor(resolver)

	// Skill requiring code only
	codeSkill := &Skill{
		Name:     "code-skill",
		Requires: []string{"code"},
	}

	model, err := exec.GetModelForSkill(codeSkill)
	if err != nil {
		t.Fatalf("GetModelForSkill failed: %v", err)
	}

	// Should get cheapest model with code capability
	if model.ModelID != "model-b" && model.ModelID != "model-a" {
		t.Errorf("Expected model-a or model-b, got %q", model.ModelID)
	}

	// Skill requiring tool_use
	toolSkill := &Skill{
		Name:     "tool-skill",
		Requires: []string{"tool_use"},
	}

	model, err = exec.GetModelForSkill(toolSkill)
	if err != nil {
		t.Fatalf("GetModelForSkill failed: %v", err)
	}

	if model.ModelID != "model-x" {
		t.Errorf("Expected model-x for tool_use, got %q", model.ModelID)
	}
}

func TestExecutor_GetModelForSkill_NoMatch(t *testing.T) {
	resolver := testResolver()
	exec := NewExecutor(resolver)

	skill := &Skill{
		Name:     "impossible",
		Requires: []string{"vision", "audio"},
	}

	_, err := exec.GetModelForSkill(skill)
	if err == nil {
		t.Error("Expected error for unsatisfiable requirements")
	}

	var capErr *llm.CapabilityError
	if !errors.As(err, &capErr) {
		t.Errorf("Expected CapabilityError, got %T", err)
	}
}

func TestExecutor_Execute_NilSkill(t *testing.T) {
	resolver := testResolver()
	exec := NewExecutor(resolver)

	_, err := exec.Execute(context.Background(), nil, "test input")
	if !errors.Is(err, ErrNoSkill) {
		t.Errorf("Expected ErrNoSkill, got %v", err)
	}
}

func TestExecutor_Execute_NilResolver(t *testing.T) {
	exec := &Executor{resolver: nil}

	skill := &Skill{Name: "test"}
	_, err := exec.Execute(context.Background(), skill, "test input")
	if !errors.Is(err, ErrNoResolver) {
		t.Errorf("Expected ErrNoResolver, got %v", err)
	}
}

func TestExecutorError(t *testing.T) {
	err := &ExecutorError{
		SkillName: "test-skill",
		Message:   "test error",
	}

	expected := `skill "test-skill": test error`
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}

	// With cause
	cause := errors.New("root cause")
	errWithCause := &ExecutorError{
		SkillName: "test-skill",
		Message:   "test error",
		Cause:     cause,
	}

	if !errors.Is(errWithCause.Unwrap(), cause) {
		t.Error("Unwrap should return cause")
	}
}

func TestBuildPrompt(t *testing.T) {
	exec := NewExecutor(testResolver())

	skill := &Skill{
		Name: "test",
		Body: "System instructions here.",
	}

	prompt := exec.buildPrompt(skill, "  user input  ")
	if prompt != "user input" {
		t.Errorf("buildPrompt = %q, want trimmed 'user input'", prompt)
	}
}

// Note: Full execution tests would require mocking the HTTP client,
// which is better done with integration tests or a mock server.
// These tests verify the executor logic without network calls.

func TestSkillExecutionResult(t *testing.T) {
	result := &SkillExecutionResult{
		Content:          "Test response",
		Model:            "model-a",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	if result.Content != "Test response" {
		t.Errorf("Content = %q", result.Content)
	}

	if result.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150", result.TotalTokens)
	}
}
