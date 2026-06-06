package skills

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestSkillMCPTool_NameDescription(t *testing.T) {
	tool := NewSkillMCPTool(llm.ToolDefinition{
		Function: llm.FunctionDef{
			Name:        "myserver.search",
			Description: "Search the web",
		},
	}, nil)

	if tool.Name() != "myserver.search" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "myserver.search")
	}
	if tool.Description() != "Search the web" {
		t.Errorf("Description() = %q, want %q", tool.Description(), "Search the web")
	}
}

func TestSkillMCPTool_Category(t *testing.T) {
	tool := NewSkillMCPTool(llm.ToolDefinition{
		Function: llm.FunctionDef{Name: "srv.tool", Description: "A tool"},
	}, nil)

	if tool.Category() != "mcp-skill" {
		t.Errorf("Category() = %q, want %q", tool.Category(), "mcp-skill")
	}
}

func TestStripServerPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"myserver.tool_name", "tool_name"},
		{"a.b.c", "b.c"},
		{"no_dot", "no_dot"},
		{"single.", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := stripServerPrefix(tt.input)
		if got != tt.want {
			t.Errorf("stripServerPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSkillMCPTool_ToLLMDefinition(t *testing.T) {
	params := llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"query": {Type: "string"},
		},
	}

	tool := NewSkillMCPTool(llm.ToolDefinition{
		Function: llm.FunctionDef{
			Name:        "web.search",
			Description: "Search the web",
			Parameters:  params,
		},
	}, nil)
	llmDef := tool.ToLLMDefinition()

	if llmDef.Function.Name != "web.search" {
		t.Errorf("ToLLMDefinition().Name = %q, want %q", llmDef.Function.Name, "web.search")
	}
	if llmDef.Function.Description != "Search the web" {
		t.Errorf("ToLLMDefinition().Description = %q, want %q", llmDef.Function.Description, "Search the web")
	}
	if llmDef.Function.Parameters.Type != "object" {
		t.Error("ToLLMDefinition().Parameters should have type 'object'")
	}
}

func TestSkillMCPTool_Parameters(t *testing.T) {
	params := llm.FunctionParameters{Type: "object"}
	tool := NewSkillMCPTool(llm.ToolDefinition{
		Function: llm.FunctionDef{
			Name:        "srv.tool",
			Description: "tool",
			Parameters:  params,
		},
	}, nil)

	if tool.Parameters().Type != "object" {
		t.Error("Parameters() should return the provided params")
	}
}

func TestClientToolPair_EmptyRuntime(t *testing.T) {
	runtime := NewMCPRuntime(nil, nil)
	pairs := runtime.ClientTools()
	if pairs != nil {
		t.Errorf("ClientTools() on empty runtime = %v, want nil", pairs)
	}
}

func TestClientToolPair_NotStarted(t *testing.T) {
	runtime := NewMCPRuntime([]MCPServerConfig{
		{Name: "test", Command: "echo"},
	}, nil)
	pairs := runtime.ClientTools()
	if pairs != nil {
		t.Errorf("ClientTools() on not-started runtime = %v, want nil", pairs)
	}
}
