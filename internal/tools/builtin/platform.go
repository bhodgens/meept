// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// PlatformStatusTool returns platform status including uptime and component health.
type PlatformStatusTool struct {
	getStatus func() map[string]any
}

// NewPlatformStatusTool creates a new platform status tool.
func NewPlatformStatusTool(getStatus func() map[string]any) *PlatformStatusTool {
	return &PlatformStatusTool{getStatus: getStatus}
}

func (t *PlatformStatusTool) Name() string { return "platform_status" }

func (t *PlatformStatusTool) Description() string {
	return "Get current meept platform status including uptime and component health."
}

func (t *PlatformStatusTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       "object",
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

func (t *PlatformStatusTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.getStatus == nil {
		return map[string]any{"status": "running"}, nil
	}
	return t.getStatus(), nil
}

// PlatformAgentsTool lists available agent specifications.
type PlatformAgentsTool struct {
	registry *agent.AgentRegistry
}

// NewPlatformAgentsTool creates a new platform agents tool.
func NewPlatformAgentsTool(registry *agent.AgentRegistry) *PlatformAgentsTool {
	return &PlatformAgentsTool{registry: registry}
}

func (t *PlatformAgentsTool) Name() string { return "platform_agents" }

func (t *PlatformAgentsTool) Description() string {
	return "List available agent specifications with their IDs, names, roles, and purposes."
}

func (t *PlatformAgentsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       "object",
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

// AgentInfo represents information about an agent specification.
type AgentInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Purpose string `json:"purpose"`
}

// AgentsResult is the result of listing agents.
type AgentsResult struct {
	Agents []AgentInfo `json:"agents"`
	Count  int         `json:"count"`
}

func (t *PlatformAgentsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.registry == nil {
		return AgentsResult{
			Agents: []AgentInfo{},
			Count:  0,
		}, nil
	}

	specs := t.registry.ListSpecs()
	agents := make([]AgentInfo, 0, len(specs))

	for _, spec := range specs {
		agents = append(agents, AgentInfo{
			ID:      spec.ID,
			Name:    spec.Name,
			Role:    string(spec.Role),
			Purpose: spec.Purpose,
		})
	}

	return AgentsResult{
		Agents: agents,
		Count:  len(agents),
	}, nil
}

// PlatformToolsTool lists registered tools with their names and descriptions.
type PlatformToolsTool struct {
	registry *tools.Registry
}

// NewPlatformToolsTool creates a new platform tools tool.
func NewPlatformToolsTool(registry *tools.Registry) *PlatformToolsTool {
	return &PlatformToolsTool{registry: registry}
}

func (t *PlatformToolsTool) Name() string { return "platform_tools" }

func (t *PlatformToolsTool) Description() string {
	return "List all registered tools with their names and descriptions."
}

func (t *PlatformToolsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       "object",
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

// ToolInfo represents information about a registered tool.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ToolsResult is the result of listing tools.
type ToolsResult struct {
	Tools []ToolInfo `json:"tools"`
	Count int        `json:"count"`
}

func (t *PlatformToolsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.registry == nil {
		return ToolsResult{
			Tools: []ToolInfo{},
			Count: 0,
		}, nil
	}

	registeredTools := t.registry.List()
	toolInfos := make([]ToolInfo, 0, len(registeredTools))

	for _, tool := range registeredTools {
		toolInfos = append(toolInfos, ToolInfo{
			Name:        tool.Name(),
			Description: tool.Description(),
		})
	}

	return ToolsResult{
		Tools: toolInfos,
		Count: len(toolInfos),
	}, nil
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*PlatformStatusTool)(nil)
	_ tools.Tool = (*PlatformAgentsTool)(nil)
	_ tools.Tool = (*PlatformToolsTool)(nil)
)
