package mcp

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/llm"
)

// MCPTool wraps an MCP tool to expose it as a local tool.
// It implements the tools.Tool interface.
type MCPTool struct {
	def     llm.ToolDefinition
	manager *Manager
	server  string // Server name for routing
}

// NewMCPTool creates a new MCPTool wrapper.
func NewMCPTool(def llm.ToolDefinition, manager *Manager, server string) *MCPTool {
	return &MCPTool{
		def:     def,
		manager: manager,
		server:  server,
	}
}

// Name returns the fully qualified tool name (server.tool).
func (t *MCPTool) Name() string {
	return t.def.Function.Name
}

// Description returns the tool description.
func (t *MCPTool) Description() string {
	return t.def.Function.Description
}

// Parameters returns the tool parameters as FunctionParameters.
func (t *MCPTool) Parameters() llm.FunctionParameters {
	return t.def.Function.Parameters
}

// Execute invokes the MCP tool via the manager.
func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	result, err := t.manager.CallTool(ctx, t.Name(), args)
	if err != nil {
		return nil, err
	}

	// Return the result content directly
	if result.Success {
		return result.Result, nil
	}

	// For error results, return an error
	return nil, fmt.Errorf("mcp tool error: %s", result.Error)
}

// ToLLMDefinition returns the LLM tool definition.
func (t *MCPTool) ToLLMDefinition() llm.ToolDefinition {
	return t.def
}

// Server returns the MCP server name this tool belongs to.
func (t *MCPTool) Server() string {
	return t.server
}

// Tool is an alias to avoid import cycles.
// MCPTool implements the tools.Tool interface from internal/tools.
