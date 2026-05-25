package builtin

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/tools"
)

// mockDelegateRegistry implements delegateRegistry for testing.
type mockDelegateRegistry struct {
	response string
	err      error
	spec     *agent.AgentSpec
	specs    []*agent.AgentSpec
}

func (m *mockDelegateRegistry) GetSpec(id string) (*agent.AgentSpec, bool) {
	if m.spec != nil {
		return m.spec, true
	}
	return &agent.AgentSpec{ID: id, Name: "mock"}, true
}

func (m *mockDelegateRegistry) ListSpecs() []*agent.AgentSpec {
	if m.specs != nil {
		return m.specs
	}
	if m.spec != nil {
		return []*agent.AgentSpec{m.spec}
	}
	return []*agent.AgentSpec{{ID: "mock", Name: "mock"}}
}

func (m *mockDelegateRegistry) RunAgent(_ context.Context, _, message, _ string) (string, error) {
	return m.response, m.err
}

func TestDelegateTaskTool_WithOutputSchema(t *testing.T) {
	ctx := context.Background()

	t.Run("valid JSON matching schema", func(t *testing.T) {
		reg := &mockDelegateRegistry{
			response: `{"name": "Alice", "age": 30}`,
		}
		tool := &DelegateTaskTool{registry: reg}

		result, err := tool.Execute(ctx, map[string]any{
			"agent_id": "coder",
			"message":  "generate a person",
			"output_schema": map[string]any{
				"type": "object",
				"required": []any{"name", "age"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"age":  map[string]any{"type": "integer"},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Result should be a tools.ToolResult
		tr, ok := result.(tools.ToolResult)
		if !ok {
			t.Fatalf("expected tools.ToolResult, got %T", result)
		}
		if !tr.Success {
			t.Errorf("expected success, got error: %s", tr.Error)
		}

		// Check the structured data
		dataMap, ok := tr.Result.(map[string]any)
		if !ok {
			t.Fatalf("expected map result, got %T", tr.Result)
		}
		if dataMap["success"] != true {
			t.Errorf("expected success=true in data")
		}

		innerData, ok := dataMap["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected data map, got %T", dataMap["data"])
		}
		if innerData["name"] != "Alice" {
			t.Errorf("expected name=Alice, got %v", innerData["name"])
		}
	})

	t.Run("JSON in code block", func(t *testing.T) {
		reg := &mockDelegateRegistry{
			response: "Here is the result:\n```json\n{\"status\": \"ok\", \"count\": 5}\n```\nDone.",
		}
		tool := &DelegateTaskTool{registry: reg}

		result, err := tool.Execute(ctx, map[string]any{
			"agent_id": "coder",
			"message":  "get status",
			"output_schema": map[string]any{
				"type": "object",
				"required": []any{"status"},
				"properties": map[string]any{
					"status": map[string]any{"type": "string"},
					"count":  map[string]any{"type": "integer"},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tr, ok := result.(tools.ToolResult)
		if !ok {
			t.Fatalf("expected tools.ToolResult, got %T", result)
		}
		if !tr.Success {
			t.Errorf("expected success, got error: %s", tr.Error)
		}
	})

	t.Run("invalid JSON response", func(t *testing.T) {
		reg := &mockDelegateRegistry{
			response: "I couldn't generate that.",
		}
		tool := &DelegateTaskTool{registry: reg}

		result, err := tool.Execute(ctx, map[string]any{
			"agent_id": "coder",
			"message":  "generate a person",
			"output_schema": map[string]any{
				"type": "object",
				"required": []any{"name"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tr, ok := result.(*tools.ToolResult)
		if !ok {
			t.Fatalf("expected *tools.ToolResult, got %T", result)
		}
		if tr.Success {
			t.Error("expected failure for invalid JSON response")
		}
	})

	t.Run("schema validation failure", func(t *testing.T) {
		reg := &mockDelegateRegistry{
			response: `{"name": "Alice"}`,
		}
		tool := &DelegateTaskTool{registry: reg}

		result, err := tool.Execute(ctx, map[string]any{
			"agent_id": "coder",
			"message":  "generate a person",
			"output_schema": map[string]any{
				"type": "object",
				"required": []any{"name", "age"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"age":  map[string]any{"type": "integer"},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tr, ok := result.(*tools.ToolResult)
		if !ok {
			t.Fatalf("expected *tools.ToolResult, got %T", result)
		}
		if tr.Success {
			t.Error("expected failure for schema validation failure")
		}
	})

	t.Run("wrong type in response", func(t *testing.T) {
		reg := &mockDelegateRegistry{
			response: `{"name": 42}`,
		}
		tool := &DelegateTaskTool{registry: reg}

		result, err := tool.Execute(ctx, map[string]any{
			"agent_id": "coder",
			"message":  "generate a person",
			"output_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tr, ok := result.(*tools.ToolResult)
		if !ok {
			t.Fatalf("expected *tools.ToolResult, got %T", result)
		}
		if tr.Success {
			t.Error("expected failure for wrong type in response")
		}
	})
}

func TestDelegateTaskTool_WithoutOutputSchema(t *testing.T) {
	ctx := context.Background()

	t.Run("backward compatible - no schema", func(t *testing.T) {
		reg := &mockDelegateRegistry{
			response: "Task completed successfully",
		}
		tool := &DelegateTaskTool{registry: reg}

		result, err := tool.Execute(ctx, map[string]any{
			"agent_id": "coder",
			"message":  "do something",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		dr, ok := result.(DelegateResult)
		if !ok {
			t.Fatalf("expected DelegateResult, got %T", result)
		}
		if !dr.Success {
			t.Errorf("expected success, got error: %s", dr.Error)
		}
		if dr.Response != "Task completed successfully" {
			t.Errorf("unexpected response: %s", dr.Response)
		}
	})

	t.Run("backward compatible - with context", func(t *testing.T) {
		reg := &mockDelegateRegistry{
			response: "Done",
		}
		tool := &DelegateTaskTool{registry: reg}

		result, err := tool.Execute(ctx, map[string]any{
			"agent_id": "coder",
			"message":  "do something",
			"context":  "extra info",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		dr, ok := result.(DelegateResult)
		if !ok {
			t.Fatalf("expected DelegateResult, got %T", result)
		}
		if !dr.Success {
			t.Errorf("expected success, got error: %s", dr.Error)
		}
	})
}
