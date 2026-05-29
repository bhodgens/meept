package builtin

import (
	"context"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// MCPServerInfo holds minimal info about a connected MCP server.
type MCPServerInfo struct {
	Name      string `json:"name"`
	ToolCount int    `json:"tool_count"`
	Connected bool   `json:"connected"`
}

// MCPServersResult is the result of listing MCP servers.
type MCPServersResult struct {
	Servers []MCPServerInfo `json:"servers"`
	Count   int             `json:"count"`
	Note    string          `json:"note,omitempty"`
}

// MCPServersTool lists connected MCP servers and their tool counts.
type MCPServersTool struct {
	listServers func() []MCPServerInfo
}

// NewMCPServersTool creates a new mcp_servers tool.
func NewMCPServersTool(listServers func() []MCPServerInfo) *MCPServersTool {
	return &MCPServersTool{listServers: listServers}
}

func (t *MCPServersTool) Name() string { return "mcp_servers" }

func (t *MCPServersTool) Description() string {
	return "List connected MCP (Model Context Protocol) servers with their connection status and available tool counts."
}

func (t *MCPServersTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

func (t *MCPServersTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.listServers == nil {
		return MCPServersResult{
			Servers: []MCPServerInfo{},
			Count:   0,
			Note:    "no MCP manager configured",
		}, nil
	}

	servers := t.listServers()
	result := MCPServersResult{
		Servers: servers,
		Count:   len(servers),
	}
	if len(servers) > 0 {
		result.Note = "use platform_tools to see the full list of available tools (including MCP tools)"
	}

	return result, nil
}

// TerminateHint implements tools.TerminatingTool for MCPServersTool.
func (t *MCPServersTool) TerminateHint(args map[string]any) bool { return true }

// Ensure MCPServersTool implements the required interfaces.
var (
	_ tools.Tool           = (*MCPServersTool)(nil)
	_ tools.TerminatingTool = (*MCPServersTool)(nil)
)
