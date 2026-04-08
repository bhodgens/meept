package agent

import (
	"context"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// testToolRegistry is a simple tool registry for tests.
// This replaces PlaceholderToolRegistry to use tools.Tool instead of agent.Tool.
type testToolRegistry struct {
	tools map[string]tools.Tool
}

func newTestToolRegistry() *testToolRegistry {
	return &testToolRegistry{
		tools: make(map[string]tools.Tool),
	}
}

func (r *testToolRegistry) Register(tool tools.Tool) {
	r.tools[tool.Name()] = tool
}

func (r *testToolRegistry) Get(name string) tools.Tool {
	return r.tools[name]
}

func (r *testToolRegistry) List() []tools.Tool {
	toolList := make([]tools.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		toolList = append(toolList, t)
	}
	return toolList
}

func (r *testToolRegistry) GetDefinitions() []llm.ToolDefinition {
	defs := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, llm.ToolDefinition{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.Parameters(),
			},
		})
	}
	return defs
}

// mockTool implements tools.Tool for testing.
type mockTool struct {
	name        string
	description string
	parameters  llm.FunctionParameters
	executeFunc func(ctx context.Context, args map[string]any) (any, error)
}

func newMockTool(name, description string, fn func(ctx context.Context, args map[string]any) (any, error)) *mockTool {
	return &mockTool{
		name:        name,
		description: description,
		parameters: llm.FunctionParameters{
			Type:       "object",
			Properties: map[string]llm.ParameterProperty{},
		},
		executeFunc: fn,
	}
}

func newMockToolWithParams(name, description string, params llm.FunctionParameters, fn func(ctx context.Context, args map[string]any) (any, error)) *mockTool {
	return &mockTool{
		name:        name,
		description: description,
		parameters:  params,
		executeFunc: fn,
	}
}

func (t *mockTool) Name() string {
	return t.name
}

func (t *mockTool) Description() string {
	return t.description
}

func (t *mockTool) Parameters() llm.FunctionParameters {
	return t.parameters
}

func (t *mockTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.executeFunc != nil {
		return t.executeFunc(ctx, args)
	}
	return map[string]any{"success": true, "mock": true}, nil
}
