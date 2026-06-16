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

// mockChatter is a test double for llm.Chatter that returns canned responses.
type mockChatter struct {
	response *llm.Response
	err      error
	called   bool
}

func (m *mockChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockChatter) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(context.Background(), nil)
}

func (m *mockChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{
		ModelID:    "model-b",
		ProviderID: "provider1",
	}
}

func TestExecutor_ExecuteWithMCPServers_SkillHasNoServers(t *testing.T) {
	resolver := testResolver()
	mock := &mockChatter{
		response: &llm.Response{
			Content: "ok",
			Model:   "provider1/model-a",
			Usage:   llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	}
	exec := NewExecutor(resolver, WithClient(mock))

	skill := &Skill{
		Name:     "no-mcp-skill",
		Requires: []string{"code"},
		Body:     "Do something.",
	}

	result, err := exec.Execute(context.Background(), skill, "test")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !mock.called {
		t.Error("expected LLM Chat to be called")
	}
	if result.MCPServersStarted {
		t.Error("MCPServersStarted should be false when skill has no MCP servers")
	}
	if len(result.MCPTools) != 0 {
		t.Errorf("MCPTools should be empty, got %d tools", len(result.MCPTools))
	}
}

func TestExecutor_ExecuteWithMCPServers_StartupError(t *testing.T) {
	resolver := testResolver()
	mock := &mockChatter{
		response: &llm.Response{
			Content: "ok",
			Model:   "provider1/model-a",
			Usage:   llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	}
	exec := NewExecutor(resolver, WithClient(mock))

	// Skill with an MCP server that uses a nonexistent command, so Start will fail.
	skill := &Skill{
		Name:     "bad-mcp-skill",
		Requires: []string{"code"},
		Body:     "Do something.",
		MCPServers: []MCPServerConfig{
			{
				Name:    "nonexistent",
				Command: "/nonexistent/binary/that/does/not/exist",
			},
		},
	}

	// Execution should NOT return an error -- MCP failures are logged and
	// execution continues with whatever servers managed to start (none here).
	result, err := exec.Execute(context.Background(), skill, "test")
	if err != nil {
		t.Fatalf("Execute should not fail on MCP startup error, got: %v", err)
	}
	if !mock.called {
		t.Error("expected LLM Chat to still be called despite MCP failure")
	}
	// MCPRuntime.Start() was called but the server failed.
	// The result should reflect the actual state: no tools if all servers failed.
	if result.MCPServersStarted && len(result.MCPTools) > 0 {
		t.Error("MCPTools should be empty when all servers failed to start")
	}
}

func TestSkillExecutionResult_MCPFields(t *testing.T) {
	result := &SkillExecutionResult{
		Content:          "response",
		Model:            "model-a",
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		MCPTools: []ToolDef{
			{Name: "myserver.tool_a", Description: "A tool", ServerName: "myserver"},
			{Name: "myserver.tool_b", Description: "B tool", ServerName: "myserver"},
		},
		MCPServersStarted: true,
	}

	if !result.MCPServersStarted {
		t.Error("MCPServersStarted should be true")
	}
	if len(result.MCPTools) != 2 {
		t.Fatalf("expected 2 MCP tools, got %d", len(result.MCPTools))
	}
	if result.MCPTools[0].Name != "myserver.tool_a" {
		t.Errorf("MCPTools[0].Name = %q, want %q", result.MCPTools[0].Name, "myserver.tool_a")
	}
	if result.MCPTools[1].ServerName != "myserver" {
		t.Errorf("MCPTools[1].ServerName = %q, want %q", result.MCPTools[1].ServerName, "myserver")
	}

	// Verify zero-value defaults
	empty := &SkillExecutionResult{}
	if empty.MCPServersStarted {
		t.Error("zero-value MCPServersStarted should be false")
	}
	if len(empty.MCPTools) != 0 {
		t.Errorf("zero-value MCPTools should be nil/empty, got %v", empty.MCPTools)
	}
}
