package mcp

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestNewMCPTool(t *testing.T) {
	def := llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "server.tool_name",
			Description: "Test tool description",
			Parameters: llm.FunctionParameters{
				Type:       "object",
				Properties: map[string]llm.ParameterProperty{},
			},
		},
	}

	tool := NewMCPTool(def, nil, "server")

	if tool.Name() != "server.tool_name" {
		t.Errorf("expected name 'server.tool_name', got %q", tool.Name())
	}

	if tool.Description() != "Test tool description" {
		t.Errorf("expected description 'Test tool description', got %q", tool.Description())
	}

	if tool.Server() != "server" {
		t.Errorf("expected server 'server', got %q", tool.Server())
	}
}

func TestMCPToolParameters(t *testing.T) {
	params := llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"param1": {
				Type:        "string",
				Description: "First parameter",
			},
			"param2": {
				Type:        "integer",
				Description: "Second parameter",
			},
		},
		Required: []string{"param1"},
	}

	def := llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "server.tool_with_params",
			Description: "Tool with parameters",
			Parameters:  params,
		},
	}

	tool := NewMCPTool(def, nil, "server")
	gotParams := tool.Parameters()

	if gotParams.Type != "object" {
		t.Errorf("expected type 'object', got %q", gotParams.Type)
	}

	if len(gotParams.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(gotParams.Properties))
	}

	if len(gotParams.Required) != 1 {
		t.Errorf("expected 1 required, got %d", len(gotParams.Required))
	}

	if gotParams.Required[0] != "param1" {
		t.Errorf("expected required 'param1', got %q", gotParams.Required[0])
	}
}

func TestMCPToolToLLMDefinition(t *testing.T) {
	def := llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "server.test_tool",
			Description: "Test tool",
			Parameters: llm.FunctionParameters{
				Type:       "object",
				Properties: map[string]llm.ParameterProperty{},
			},
		},
	}

	tool := NewMCPTool(def, nil, "server")
	llmDef := tool.ToLLMDefinition()

	if llmDef.Type != "function" {
		t.Errorf("expected type 'function', got %q", llmDef.Type)
	}

	if llmDef.Function.Name != "server.test_tool" {
		t.Errorf("expected name 'server.test_tool', got %q", llmDef.Function.Name)
	}
}

func TestMCPToolExecuteNilManager(t *testing.T) {
	def := llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "server.test_tool",
			Description: "Test tool",
			Parameters: llm.FunctionParameters{
				Type:       "object",
				Properties: map[string]llm.ParameterProperty{},
			},
		},
	}

	tool := NewMCPTool(def, nil, "server")

	// Calling Execute with nil manager should cause a panic or error
	// We defer/recover to handle the panic case
	defer func() {
		if r := recover(); r != nil {
			// Expected - nil manager causes panic
		}
	}()

	_, err := tool.Execute(context.TODO(), nil)
	// If we get here without panic, err should be non-nil
	if err == nil {
		t.Error("expected error when executing with nil manager")
	}
}
