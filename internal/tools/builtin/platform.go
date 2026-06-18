// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
)

// extractFirstLine returns the first non-heading, non-empty line from a
// markdown body. Used to produce brief summaries from agent purpose bodies.
func extractFirstLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		return trimmed
	}
	return ""
}

// PlatformStatusTool returns platform status including uptime and component health.
type PlatformStatusTool struct {
	getStatus func() map[string]any
}

// NewPlatformStatusTool creates a new platform status tool.
func NewPlatformStatusTool(getStatus func() map[string]any) *PlatformStatusTool {
	return &PlatformStatusTool{getStatus: getStatus}
}

func (t *PlatformStatusTool) Name() string { return "platform_status" }

func (t *PlatformStatusTool) Category() string { return "platform" }

func (t *PlatformStatusTool) Description() string {
	return "Get current meept platform status including uptime and component health."
}

func (t *PlatformStatusTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       schemaTypeObject,
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

func (t *PlatformAgentsTool) Category() string { return "platform" }

func (t *PlatformAgentsTool) Description() string {
	return "List available agent specifications with their IDs, names, roles, and purposes."
}

func (t *PlatformAgentsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       schemaTypeObject,
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
		return "No agents available.", nil
	}

	specs := t.registry.ListSpecs()
	if len(specs) == 0 {
		return "No agents available.", nil
	}

	// Build formatted markdown output for better readability
	var sb strings.Builder
	sb.WriteString("## Available Agents\n\n")
	sb.WriteString("Specialist agents for different task types:\n\n")

	for _, spec := range specs {
		sb.WriteString(fmt.Sprintf("### %s (`%s`)\n", spec.Name, spec.ID))
		sb.WriteString(fmt.Sprintf("**Role**: %s\n\n", spec.Role))
		// Extract first meaningful line from the purpose body to avoid
		// nested markdown headings breaking the list structure.
		purpose := extractFirstLine(spec.Purpose)
		if purpose == "" {
			purpose = spec.Purpose
		}
		if len(purpose) > 300 {
			purpose = purpose[:297] + "..."
		}
		sb.WriteString(fmt.Sprintf("%s\n\n", purpose))
	}

	sb.WriteString(fmt.Sprintf("*Total: %d agents*\n", len(specs)))

	return sb.String(), nil
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

func (t *PlatformToolsTool) Category() string { return "platform" }

func (t *PlatformToolsTool) Description() string {
	return "List all registered tools with their names and descriptions."
}

func (t *PlatformToolsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       schemaTypeObject,
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
		return "No tools available.", nil
	}

	registeredTools := t.registry.List()
	if len(registeredTools) == 0 {
		return "No tools available.", nil
	}

	// Group tools by category for better organization
	toolsByCategory := make(map[string][]ToolInfo)
	for _, tool := range registeredTools {
		cat := tools.GetCategory(tool)
		if cat == "" {
			cat = "other"
		}
		toolsByCategory[cat] = append(toolsByCategory[cat], ToolInfo{
			Name:        tool.Name(),
			Description: tool.Description(),
		})
	}

	// Build formatted markdown output
	var sb strings.Builder
	sb.WriteString("## Available Tools\n\n")

	// Sort categories for consistent output
	categories := make([]string, 0, len(toolsByCategory))
	for cat := range toolsByCategory {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	for _, cat := range categories {
		tools := toolsByCategory[cat]
		sb.WriteString(fmt.Sprintf("### %s Tools\n\n", strings.Title(cat)))
		for _, tool := range tools {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", tool.Name, tool.Description))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("*Total: %d tools*\n", len(registeredTools)))

	return sb.String(), nil
}

// delegateRegistry defines the subset of AgentRegistry used by DelegateTaskTool.
// This enables testing with mock implementations.
type delegateRegistry interface {
	GetSpec(id string) (*agent.AgentSpec, bool)
	ListSpecs() []*agent.AgentSpec
	RunAgent(ctx context.Context, agentID, message, conversationID string) (string, error)
}

// DelegateTaskTool delegates a task to a specific agent.
type DelegateTaskTool struct {
	registry delegateRegistry
}

// NewDelegateTaskTool creates a new delegate task tool.
func NewDelegateTaskTool(registry *agent.AgentRegistry) *DelegateTaskTool {
	return &DelegateTaskTool{registry: registry}
}

func (t *DelegateTaskTool) Name() string { return "delegate_task" }

func (t *DelegateTaskTool) Category() string { return "platform" }

func (t *DelegateTaskTool) Description() string {
	return "Delegate a task to a specific specialist agent by ID. Returns the agent's response. Use platform_agents first to discover available agents."
}

func (t *DelegateTaskTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"agent_id": {
				Type:        schemaTypeString,
				Description: "The ID of the agent to delegate to (e.g., 'coder', 'planner', 'analyst').",
			},
			schemaPropMessage: {
				Type:        schemaTypeString,
				Description: "The message/task to send to the agent.",
			},
			"context": {
				Type:        schemaTypeString,
				Description: "Optional additional context to provide to the agent.",
			},
			"output_schema": {
				Type:        schemaTypeObject,
				Description: "Optional JSON Schema to validate subagent output. When provided, the subagent is instructed to produce JSON matching this schema.",
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
	contextStr, _ := args["context"].(string)

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
			Error:   "agent not found: " + agentID + ". Available: " + strings.Join(available, ", "),
		}, nil
	}

	// Build the full message with context
	fullMessage := message
	if contextStr != "" {
		fullMessage = "Context: " + contextStr + "\n\nTask: " + message
	}

	// Check for output_schema parameter
	schemaRaw, hasSchema := args["output_schema"].(map[string]any)
	if hasSchema {
		schemaJSON, _ := json.Marshal(schemaRaw)
		fullMessage = fmt.Sprintf(
			"%s\n\nIMPORTANT: Your response must be valid JSON matching this schema:\n```json\n%s\n```\nRespond with ONLY the JSON, no additional text.",
			fullMessage,
			schemaJSON,
		)
	}

	// Generate a conversation ID for this delegation
	conversationID := id.Generate("delegate-")

	// Run the agent
	response, err := t.registry.RunAgent(ctx, spec.ID, fullMessage, conversationID)
	if err != nil {
		return DelegateResult{
			AgentID: agentID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// If output_schema was provided, validate the response
	if hasSchema {
		data, extractErr := ExtractJSONFromText(response)
		if extractErr != nil {
			return tools.NewErrorResult(fmt.Sprintf(
				"subagent did not produce valid JSON: %v\n\nRaw response: %s",
				extractErr, response,
			)), nil
		}

		if validateErr := ValidateJSONSchema(data, schemaRaw); validateErr != nil {
			return tools.NewErrorResult(fmt.Sprintf(
				"schema validation failed: %v",
				validateErr,
			)), nil
		}

		return tools.ToolResult{
			Success: true,
			Result: map[string]any{
				"success":     true,
				"data":        data,
				"tokens_used": 0,
			},
		}, nil
	}

	return DelegateResult{
		AgentID:  agentID,
		Success:  true,
		Response: response,
	}, nil
}


// SessionHistoryTool provides access to recent session activity and completed work.
// This enables agents to provide reports about what was accomplished.
type SessionHistoryTool struct {
	getRecentTasks    func(limit int) ([]SessionTaskInfo, error)
	getRecentMessages func(sessionID string, limit int) ([]SessionMessageInfo, error)
}

// SessionTaskInfo represents summary information about a task.
type SessionTaskInfo struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	State         string    `json:"state"`
	AssignedAgent string    `json:"assigned_agent,omitempty"`
	CompletedJobs int       `json:"completed_jobs"`
	TotalJobs     int       `json:"total_jobs"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// SessionMessageInfo represents a message in the session history.
type SessionMessageInfo struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// SessionHistoryResult is the result of querying session history.
type SessionHistoryResult struct {
	Tasks     []SessionTaskInfo    `json:"tasks"`
	Messages  []SessionMessageInfo `json:"messages,omitempty"`
	TaskCount int                  `json:"task_count"`
	Summary   string               `json:"summary"`
}

// NewSessionHistoryTool creates a new session history tool.
func NewSessionHistoryTool(
	getRecentTasks func(limit int) ([]SessionTaskInfo, error),
	getRecentMessages func(sessionID string, limit int) ([]SessionMessageInfo, error),
) *SessionHistoryTool {
	return &SessionHistoryTool{
		getRecentTasks:    getRecentTasks,
		getRecentMessages: getRecentMessages,
	}
}

func (t *SessionHistoryTool) Name() string { return "session_history" }

func (t *SessionHistoryTool) Category() string { return "platform" }

func (t *SessionHistoryTool) Description() string {
	return "Query recent session activity including completed tasks, work summary, and conversation history. Use this to provide reports about what was accomplished."
}

func (t *SessionHistoryTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropLimit: {
				Type:        schemaTypeInteger,
				Description: "Maximum number of recent tasks to return (default: 10, max: 50).",
			},
			"session_id": {
				Type:        schemaTypeString,
				Description: "Optional session ID to filter history for a specific session.",
			},
			"include_messages": {
				Type:        schemaTypeBoolean,
				Description: "Whether to include recent conversation messages (default: false).",
			},
		},
		Required: []string{},
	}
}

func (t *SessionHistoryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// Parse parameters
	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = min(int(l), 50)
	}

	sessionID, _ := args["session_id"].(string)
	includeMessages, _ := args["include_messages"].(bool)

	result := SessionHistoryResult{
		Tasks:    []SessionTaskInfo{},
		Messages: []SessionMessageInfo{},
	}

	// Get recent tasks
	if t.getRecentTasks != nil {
		tasks, err := t.getRecentTasks(limit)
		if err != nil {
			return nil, fmt.Errorf("failed to get recent tasks: %w", err)
		}
		result.Tasks = tasks
		result.TaskCount = len(tasks)
	}

	// Get recent messages if requested
	if includeMessages && t.getRecentMessages != nil && sessionID != "" {
		messages, err := t.getRecentMessages(sessionID, limit)
		if err != nil {
			// Non-fatal - just skip messages
			result.Messages = []SessionMessageInfo{}
		} else {
			result.Messages = messages
		}
	}

	// Generate summary
	result.Summary = t.generateSummary(result.Tasks)

	return result, nil
}

// generateSummary creates a human-readable summary of recent work.
func (t *SessionHistoryTool) generateSummary(tasks []SessionTaskInfo) string {
	if len(tasks) == 0 {
		return "No recent tasks found."
	}

	var completed, inProgress, pending, failed int
	for _, task := range tasks {
		switch task.State {
		case "completed":
			completed++
		case "executing", "planning":
			inProgress++
		case "pending":
			pending++
		case "failed":
			failed++
		}
	}

	summary := fmt.Sprintf("Recent activity: %d tasks total", len(tasks))
	if completed > 0 {
		summary += fmt.Sprintf(", %d completed", completed)
	}
	if inProgress > 0 {
		summary += fmt.Sprintf(", %d in progress", inProgress)
	}
	if pending > 0 {
		summary += fmt.Sprintf(", %d pending", pending)
	}
	if failed > 0 {
		summary += fmt.Sprintf(", %d failed", failed)
	}
	summary += "."

	return summary
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*PlatformStatusTool)(nil)
	_ tools.Tool = (*PlatformAgentsTool)(nil)
	_ tools.Tool = (*PlatformToolsTool)(nil)
	_ tools.Tool = (*DelegateTaskTool)(nil)
	_ tools.Tool = (*SessionHistoryTool)(nil)
)

// TerminatingTool implementations for platform tools.
// These tools return definitive results that do not need LLM follow-up.

// TerminateHint implements tools.TerminatingTool for PlatformStatusTool.
func (t *PlatformStatusTool) TerminateHint(args map[string]any) bool { return true }

// TerminateHint implements tools.TerminatingTool for PlatformAgentsTool.
func (t *PlatformAgentsTool) TerminateHint(args map[string]any) bool { return true }

// TerminateHint implements tools.TerminatingTool for PlatformToolsTool.
func (t *PlatformToolsTool) TerminateHint(args map[string]any) bool { return true }

// Ensure platform tools implement TerminatingTool
var (
	_ tools.TerminatingTool = (*PlatformStatusTool)(nil)
	_ tools.TerminatingTool = (*PlatformAgentsTool)(nil)
	_ tools.TerminatingTool = (*PlatformToolsTool)(nil)
)
