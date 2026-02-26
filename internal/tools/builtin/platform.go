// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"fmt"
	"time"

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

// DelegateTaskTool delegates a task to a specific agent.
type DelegateTaskTool struct {
	registry *agent.AgentRegistry
}

// NewDelegateTaskTool creates a new delegate task tool.
func NewDelegateTaskTool(registry *agent.AgentRegistry) *DelegateTaskTool {
	return &DelegateTaskTool{registry: registry}
}

func (t *DelegateTaskTool) Name() string { return "delegate_task" }

func (t *DelegateTaskTool) Description() string {
	return "Delegate a task to a specific specialist agent by ID. Returns the agent's response. Use platform_agents first to discover available agents."
}

func (t *DelegateTaskTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"agent_id": {
				Type:        "string",
				Description: "The ID of the agent to delegate to (e.g., 'coder', 'planner', 'analyst').",
			},
			"message": {
				Type:        "string",
				Description: "The message/task to send to the agent.",
			},
			"context": {
				Type:        "string",
				Description: "Optional additional context to provide to the agent.",
			},
		},
		Required: []string{"agent_id", "message"},
	}
}

// DelegateResult is the result of delegating a task.
type DelegateResult struct {
	AgentID  string `json:"agent_id"`
	Success  bool   `json:"success"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (t *DelegateTaskTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.registry == nil {
		return DelegateResult{
			Success: false,
			Error:   "agent registry not available",
		}, nil
	}

	agentID, _ := args["agent_id"].(string)
	message, _ := args["message"].(string)
	context, _ := args["context"].(string)

	if agentID == "" {
		return DelegateResult{
			Success: false,
			Error:   "agent_id is required",
		}, nil
	}

	if message == "" {
		return DelegateResult{
			Success: false,
			Error:   "message is required",
		}, nil
	}

	// Check if agent exists
	spec, ok := t.registry.GetSpec(agentID)
	if !ok {
		// List available agents for helpful error
		specs := t.registry.ListSpecs()
		available := make([]string, len(specs))
		for i, s := range specs {
			available[i] = s.ID
		}
		return DelegateResult{
			AgentID: agentID,
			Success: false,
			Error:   "agent not found: " + agentID + ". Available: " + joinStrings(available),
		}, nil
	}

	// Build the full message with context
	fullMessage := message
	if context != "" {
		fullMessage = "Context: " + context + "\n\nTask: " + message
	}

	// Generate a conversation ID for this delegation
	conversationID := "delegate-" + agentID + "-" + generateDelegateID()

	// Run the agent
	response, err := t.registry.RunAgent(ctx, spec.ID, fullMessage, conversationID)
	if err != nil {
		return DelegateResult{
			AgentID: agentID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return DelegateResult{
		AgentID:  agentID,
		Success:  true,
		Response: response,
	}, nil
}

func joinStrings(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += ", " + strs[i]
	}
	return result
}

func generateDelegateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*PlatformStatusTool)(nil)
	_ tools.Tool = (*PlatformAgentsTool)(nil)
	_ tools.Tool = (*PlatformToolsTool)(nil)
	_ tools.Tool = (*DelegateTaskTool)(nil)
)
