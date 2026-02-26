package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// ToolActionMap maps tool names to permission action categories.
var ToolActionMap = map[string]string{
	// File operations
	"shell":          "shell_execute",
	"file_read":      "file_read",
	"file_write":     "file_write",
	"file_delete":    "file_delete",
	"list_directory": "file_read",

	// Network operations
	"web_search": "network_request",
	"web_fetch":  "network_request",

	// Memory operations
	"memory_search":      "memory_read",
	"memory_get_context": "memory_read",
	"memory_store":       "memory_write",
	"memory_delete":      "memory_write",

	// Platform introspection (read-only, safe)
	"platform_status": "platform_read",
	"platform_agents": "platform_read",
	"platform_tools":  "platform_read",

	// Task management
	"task_create": "task_write",
	"task_get":    "task_read",
	"task_list":   "task_read",
	"task_update": "task_write",

	// Agent delegation
	"delegate_task": "agent_delegate",
}

// Tool represents a tool that can be executed by the agent.
// This is an interface to allow for different tool implementations.
type Tool interface {
	// Name returns the tool's name.
	Name() string
	// Description returns the tool's description.
	Description() string
	// Execute runs the tool with the given arguments.
	Execute(ctx context.Context, args map[string]any) (any, error)
}

// ToolRegistry provides access to available tools.
// This is a placeholder interface that will be implemented in Phase 8.
type ToolRegistry interface {
	// Get retrieves a tool by name.
	Get(name string) Tool
	// List returns all available tools.
	List() []Tool
	// GetDefinitions returns tool definitions for the LLM.
	GetDefinitions() []llm.ToolDefinition
}

// ExecutionResult represents the result of a tool execution.
type ExecutionResult struct {
	ToolCallID string `json:"tool_call_id"`
	Success    bool   `json:"success"`
	Result     any    `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ToJSON converts the result to a JSON string.
func (r *ExecutionResult) ToJSON() string {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Sprintf(`{"success":false,"error":"failed to marshal result: %s"}`, err)
	}
	return string(data)
}

// ToChatMessage converts the result to a tool role chat message.
func (r *ExecutionResult) ToChatMessage() llm.ChatMessage {
	return llm.ChatMessage{
		Role:       llm.RoleTool,
		Content:    r.ToJSON(),
		ToolCallID: r.ToolCallID,
	}
}

// Executor handles tool execution with security checks.
type Executor struct {
	registry    ToolRegistry
	security    *security.PermissionChecker
	logger      *slog.Logger
	parallelism int
	agentID     string // Identifier for the agent/worker using this executor
}

// ExecutorOption is a functional option for configuring an Executor.
type ExecutorOption func(*Executor)

// WithExecutorLogger sets the logger for the executor.
func WithExecutorLogger(logger *slog.Logger) ExecutorOption {
	return func(e *Executor) {
		e.logger = logger
	}
}

// WithParallelism sets the maximum number of parallel tool executions.
func WithParallelism(n int) ExecutorOption {
	return func(e *Executor) {
		if n > 0 {
			e.parallelism = n
		}
	}
}

// WithExecutorAgentID sets an identifier for logging which agent/worker is executing.
func WithExecutorAgentID(id string) ExecutorOption {
	return func(e *Executor) {
		e.agentID = id
	}
}

// NewExecutor creates a new tool executor.
func NewExecutor(registry ToolRegistry, security *security.PermissionChecker, opts ...ExecutorOption) *Executor {
	e := &Executor{
		registry:    registry,
		security:    security,
		logger:      slog.Default(),
		parallelism: 4, // Default parallelism
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Execute runs a single tool call with security checks.
func (e *Executor) Execute(ctx context.Context, toolCall llm.ToolCall) *ExecutionResult {
	toolName := toolCall.Function.Name

	// Parse arguments
	args, err := toolCall.ParsedArguments()
	if err != nil {
		e.logger.Warn("Failed to parse tool arguments",
			"tool", toolName,
			"error", err,
		)
		return &ExecutionResult{
			ToolCallID: toolCall.ID,
			Success:    false,
			Error:      fmt.Sprintf("invalid JSON in tool arguments: %v", err),
		}
	}

	// Look up tool
	if e.registry == nil {
		return &ExecutionResult{
			ToolCallID: toolCall.ID,
			Success:    false,
			Error:      "tool registry not configured",
		}
	}

	tool := e.registry.Get(toolName)
	if tool == nil {
		e.logger.Warn("Unknown tool requested", "tool", toolName)
		return &ExecutionResult{
			ToolCallID: toolCall.ID,
			Success:    false,
			Error:      fmt.Sprintf("unknown tool: %s", toolName),
		}
	}

	// Security check
	if e.security != nil {
		result := e.checkPermission(toolName, args)
		if !result.Allowed {
			e.logger.Info("Tool blocked by security",
				"agent", e.agentID,
				"tool", toolName,
				"reason", result.Reason,
				"risk", result.EffectiveRisk.String(),
			)
			return &ExecutionResult{
				ToolCallID: toolCall.ID,
				Success:    false,
				Error:      fmt.Sprintf("permission denied: %s", result.Reason),
			}
		}

		if result.NeedsConfirm {
			// For now, we don't support async confirmation in the Go implementation.
			// This will be handled at a higher level.
			return &ExecutionResult{
				ToolCallID: toolCall.ID,
				Success:    false,
				Error:      "action requires user confirmation",
			}
		}
	}

	// Execute the tool
	e.logger.Info("Executing tool",
		"agent", e.agentID,
		"tool", toolName,
		"args_summary", summarizeArgs(args),
	)

	result, err := tool.Execute(ctx, args)
	if err != nil {
		e.logger.Error("Tool execution failed",
			"agent", e.agentID,
			"tool", toolName,
			"error", err,
		)
		return &ExecutionResult{
			ToolCallID: toolCall.ID,
			Success:    false,
			Error:      fmt.Sprintf("tool execution failed: %v", err),
		}
	}

	return &ExecutionResult{
		ToolCallID: toolCall.ID,
		Success:    true,
		Result:     result,
	}
}

// ExecuteAll runs multiple tool calls, potentially in parallel.
func (e *Executor) ExecuteAll(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult {
	if len(toolCalls) == 0 {
		return nil
	}

	// For single tool call, execute directly
	if len(toolCalls) == 1 {
		return []*ExecutionResult{e.Execute(ctx, toolCalls[0])}
	}

	// Execute in parallel with limited concurrency
	results := make([]*ExecutionResult, len(toolCalls))
	var wg sync.WaitGroup

	// Create a semaphore channel for limiting parallelism
	sem := make(chan struct{}, e.parallelism)

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, toolCall llm.ToolCall) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = &ExecutionResult{
					ToolCallID: toolCall.ID,
					Success:    false,
					Error:      "context cancelled",
				}
				return
			}

			results[idx] = e.Execute(ctx, toolCall)
		}(i, tc)
	}

	wg.Wait()
	return results
}

// ExecuteSequential runs tool calls sequentially (no parallelism).
func (e *Executor) ExecuteSequential(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult {
	results := make([]*ExecutionResult, len(toolCalls))
	for i, tc := range toolCalls {
		select {
		case <-ctx.Done():
			results[i] = &ExecutionResult{
				ToolCallID: tc.ID,
				Success:    false,
				Error:      "context cancelled",
			}
			return results
		default:
			results[i] = e.Execute(ctx, tc)
		}
	}
	return results
}

// checkPermission checks if a tool call is permitted.
func (e *Executor) checkPermission(toolName string, args map[string]any) security.CheckResult {
	// Map tool name to action category
	action, ok := ToolActionMap[toolName]
	if !ok {
		action = toolName
	}

	// Convert args to string map for security checker
	details := make(map[string]string)
	for k, v := range args {
		switch val := v.(type) {
		case string:
			details[k] = val
		default:
			if data, err := json.Marshal(val); err == nil {
				details[k] = string(data)
			}
		}
	}

	return e.security.CheckPermission(action, details)
}

// summarizeArgs returns a truncated string representation of arguments for logging.
func summarizeArgs(args map[string]any) string {
	const maxLen = 200

	data, err := json.Marshal(args)
	if err != nil {
		return "(failed to serialize)"
	}

	s := string(data)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// ResultsToChatMessages converts execution results to chat messages.
func ResultsToChatMessages(results []*ExecutionResult) []llm.ChatMessage {
	messages := make([]llm.ChatMessage, len(results))
	for i, r := range results {
		messages[i] = r.ToChatMessage()
	}
	return messages
}

// PlaceholderToolRegistry is a placeholder implementation for testing.
// This will be replaced with a real implementation in Phase 8.
type PlaceholderToolRegistry struct {
	tools map[string]Tool
}

// NewPlaceholderToolRegistry creates a new placeholder registry.
func NewPlaceholderToolRegistry() *PlaceholderToolRegistry {
	return &PlaceholderToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *PlaceholderToolRegistry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name.
func (r *PlaceholderToolRegistry) Get(name string) Tool {
	return r.tools[name]
}

// List returns all available tools.
func (r *PlaceholderToolRegistry) List() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// GetDefinitions returns tool definitions for the LLM.
func (r *PlaceholderToolRegistry) GetDefinitions() []llm.ToolDefinition {
	// Placeholder - returns empty for now
	return nil
}

// MockTool is a mock tool for testing.
type MockTool struct {
	name        string
	description string
	executeFunc func(ctx context.Context, args map[string]any) (any, error)
}

// NewMockTool creates a new mock tool.
func NewMockTool(name, description string, fn func(ctx context.Context, args map[string]any) (any, error)) *MockTool {
	return &MockTool{
		name:        name,
		description: description,
		executeFunc: fn,
	}
}

// Name returns the tool's name.
func (t *MockTool) Name() string {
	return t.name
}

// Description returns the tool's description.
func (t *MockTool) Description() string {
	return t.description
}

// Execute runs the mock tool.
func (t *MockTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.executeFunc != nil {
		return t.executeFunc(ctx, args)
	}
	return map[string]any{"success": true, "mock": true}, nil
}

// FilteredToolRegistry wraps a ToolRegistry and only exposes a subset of tools.
type FilteredToolRegistry struct {
	parent  ToolRegistry
	allowed map[string]bool
}

// NewFilteredToolRegistry creates a tool registry that filters tools by allowed names.
// If allowedTools is empty, all tools from the parent are allowed.
func NewFilteredToolRegistry(parent ToolRegistry, allowedTools []string) *FilteredToolRegistry {
	allowed := make(map[string]bool)
	for _, name := range allowedTools {
		allowed[name] = true
	}
	return &FilteredToolRegistry{
		parent:  parent,
		allowed: allowed,
	}
}

// Get retrieves a tool by name, returning nil if not in the allowed set.
func (r *FilteredToolRegistry) Get(name string) Tool {
	if len(r.allowed) > 0 && !r.allowed[name] {
		return nil
	}
	return r.parent.Get(name)
}

// List returns only allowed tools.
func (r *FilteredToolRegistry) List() []Tool {
	all := r.parent.List()
	if len(r.allowed) == 0 {
		return all
	}

	filtered := make([]Tool, 0)
	for _, t := range all {
		if r.allowed[t.Name()] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// GetDefinitions returns tool definitions for only allowed tools.
func (r *FilteredToolRegistry) GetDefinitions() []llm.ToolDefinition {
	all := r.parent.GetDefinitions()
	if len(r.allowed) == 0 {
		return all
	}

	filtered := make([]llm.ToolDefinition, 0)
	for _, def := range all {
		if r.allowed[def.Function.Name] {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

// FilterToolsForSkill creates a filtered tool registry based on a skill's allowed-tools.
// This is used when executing skills that have restricted tool access.
func FilterToolsForSkill(registry ToolRegistry, allowedTools []string) ToolRegistry {
	if len(allowedTools) == 0 {
		return registry // No filtering needed
	}
	return NewFilteredToolRegistry(registry, allowedTools)
}
