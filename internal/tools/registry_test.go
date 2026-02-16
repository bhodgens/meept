package tools

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// mockTool is a simple tool implementation for testing.
type mockTool struct {
	name        string
	description string
	params      llm.FunctionParameters
	execFunc    func(ctx context.Context, args map[string]any) (any, error)
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return m.description }
func (m *mockTool) Parameters() llm.FunctionParameters {
	if m.params.Type == "" {
		return llm.FunctionParameters{
			Type:       "object",
			Properties: make(map[string]llm.ParameterProperty),
		}
	}
	return m.params
}
func (m *mockTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, args)
	}
	return map[string]any{"result": "ok"}, nil
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry(nil)

	tool := &mockTool{name: "test_tool", description: "A test tool"}
	r.Register(tool)

	if r.Count() != 1 {
		t.Errorf("expected 1 tool, got %d", r.Count())
	}

	got := r.Get("test_tool")
	if got == nil {
		t.Fatal("expected to find test_tool")
	}
	if got.Name() != "test_tool" {
		t.Errorf("expected name 'test_tool', got '%s'", got.Name())
	}
}

func TestRegistry_RegisterReplace(t *testing.T) {
	r := NewRegistry(nil)

	tool1 := &mockTool{name: "test_tool", description: "First"}
	tool2 := &mockTool{name: "test_tool", description: "Second"}

	r.Register(tool1)
	r.Register(tool2)

	if r.Count() != 1 {
		t.Errorf("expected 1 tool after replacement, got %d", r.Count())
	}

	got := r.Get("test_tool")
	if got.Description() != "Second" {
		t.Errorf("expected 'Second', got '%s'", got.Description())
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry(nil)

	tool := &mockTool{name: "test_tool"}
	r.Register(tool)

	err := r.Unregister("test_tool")
	if err != nil {
		t.Fatalf("unregister failed: %v", err)
	}

	if r.Count() != 0 {
		t.Errorf("expected 0 tools, got %d", r.Count())
	}

	// Try to unregister again - should fail
	err = r.Unregister("test_tool")
	if err == nil {
		t.Error("expected error when unregistering non-existent tool")
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry(nil)

	// Get non-existent tool
	got := r.Get("nonexistent")
	if got != nil {
		t.Error("expected nil for non-existent tool")
	}

	// Register and get
	tool := &mockTool{name: "test_tool"}
	r.Register(tool)

	got = r.Get("test_tool")
	if got == nil {
		t.Error("expected to find test_tool")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry(nil)

	r.Register(&mockTool{name: "tool_b"})
	r.Register(&mockTool{name: "tool_a"})
	r.Register(&mockTool{name: "tool_c"})

	tools := r.List()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	// Should be sorted
	expected := []string{"tool_a", "tool_b", "tool_c"}
	for i, tool := range tools {
		if tool.Name() != expected[i] {
			t.Errorf("position %d: expected '%s', got '%s'", i, expected[i], tool.Name())
		}
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry(nil)

	r.Register(&mockTool{name: "tool_b"})
	r.Register(&mockTool{name: "tool_a"})

	names := r.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}

	if names[0] != "tool_a" || names[1] != "tool_b" {
		t.Errorf("names not sorted: %v", names)
	}
}

func TestRegistry_ToLLMDefinitions(t *testing.T) {
	r := NewRegistry(nil)

	r.Register(&mockTool{
		name:        "test_tool",
		description: "A test tool",
		params: llm.FunctionParameters{
			Type: "object",
			Properties: map[string]llm.ParameterProperty{
				"input": {
					Type:        "string",
					Description: "The input value",
				},
			},
			Required: []string{"input"},
		},
	})

	defs := r.ToLLMDefinitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}

	def := defs[0]
	if def.Type != "function" {
		t.Errorf("expected type 'function', got '%s'", def.Type)
	}
	if def.Function.Name != "test_tool" {
		t.Errorf("expected name 'test_tool', got '%s'", def.Function.Name)
	}
	if def.Function.Description != "A test tool" {
		t.Errorf("expected description 'A test tool', got '%s'", def.Function.Description)
	}
	if len(def.Function.Parameters.Properties) != 1 {
		t.Errorf("expected 1 property, got %d", len(def.Function.Parameters.Properties))
	}
}

func TestRegistry_Execute(t *testing.T) {
	r := NewRegistry(nil)

	r.Register(&mockTool{
		name: "echo",
		execFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{"echo": args["message"]}, nil
		},
	})

	ctx := context.Background()
	result, err := r.Execute(ctx, "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}

	resultMap, ok := result.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result.Result)
	}
	if resultMap["echo"] != "hello" {
		t.Errorf("expected 'hello', got '%v'", resultMap["echo"])
	}
}

func TestRegistry_ExecuteNotFound(t *testing.T) {
	r := NewRegistry(nil)

	ctx := context.Background()
	_, err := r.Execute(ctx, "nonexistent", nil)
	if err == nil {
		t.Error("expected error for non-existent tool")
	}
}

func TestRegistry_ExecuteError(t *testing.T) {
	r := NewRegistry(nil)

	r.Register(&mockTool{
		name: "failing",
		execFunc: func(ctx context.Context, args map[string]any) (any, error) {
			return nil, context.DeadlineExceeded
		},
	})

	ctx := context.Background()
	result, err := r.Execute(ctx, "failing", nil)
	if err != nil {
		t.Fatalf("execute should not return error: %v", err)
	}

	if result.Success {
		t.Error("expected failure")
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

// Test concurrent access
func TestRegistry_Concurrent(t *testing.T) {
	r := NewRegistry(nil)

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			name := string(rune('a' + n))
			r.Register(&mockTool{name: "tool_" + name})
			r.Get("tool_" + name)
			r.List()
			r.Names()
			r.ToLLMDefinitions()
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if r.Count() != 10 {
		t.Errorf("expected 10 tools, got %d", r.Count())
	}
}
