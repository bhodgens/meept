package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
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

	// Code intelligence - AST (read-only, safe)
	"ast_parse":   "code_read",
	"ast_symbols": "code_read",
	"ast_query":   "code_read",

	// Code intelligence - LSP (read-only, requires server)
	"lsp_goto_definition":   "code_read",
	"lsp_find_references":   "code_read",
	"lsp_hover":             "code_read",
	"lsp_workspace_symbols": "code_read",
	"lsp_diagnostics":       "code_read",
}


// ToolRegistry provides access to available tools.
// Production implementation lives in internal/tools/registry.go; the
// executor depends on this narrower interface so that unit tests can
// substitute NewPlaceholderToolRegistry without dragging in the full
// tool registry graph.
type ToolRegistry interface {
	// Get retrieves a tool by name.
	Get(name string) tools.Tool
	// List returns all available tools.
	List() []tools.Tool
	// GetDefinitions returns tool definitions for the LLM.
	GetDefinitions() []llm.ToolDefinition
}

// ExecutionResult represents the result of a tool execution.
type ExecutionResult struct {
	ToolCallID string `json:"tool_call_id"`
	Success    bool   `json:"success"`
	Result     any    `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	Cached     bool   `json:"cached,omitempty"` // True if result came from cache
}

// ToJSON converts the result to a JSON string.
func (r *ExecutionResult) ToJSON() string {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Sprintf(`{"success":false,"error":"failed to marshal result: %s"}`, err)
	}
	return string(data)
}

// ToCompressedJSON converts the result to a JSON string, compressing if over maxTokens.
// Uses 3 chars/token estimation (appropriate for JSON/code content). Large results are truncated with a summary.
func (r *ExecutionResult) ToCompressedJSON(maxTokens int) string {
	full := r.ToJSON()
	const charsPerToken = 3
	maxChars := maxTokens * charsPerToken

	if len(full) <= maxChars {
		return full
	}

	// Compress by truncating the result content
	compressed := &ExecutionResult{
		ToolCallID: r.ToolCallID,
		Success:    r.Success,
		Error:      r.Error,
		Cached:     r.Cached,
	}

	// Handle the result based on type
	switch result := r.Result.(type) {
	case string:
		compressed.Result = truncateWithMarker(result, maxChars-200) // Reserve space for JSON wrapper
	case map[string]any:
		compressed.Result = compressMapResult(result, maxChars-200)
	default:
		// For other types, marshal and truncate the raw JSON
		if data, err := json.Marshal(r.Result); err == nil && len(data) > maxChars-200 {
			compressed.Result = string(data[:maxChars-200]) + "...[truncated]"
		} else {
			compressed.Result = r.Result
		}
	}

	data, err := json.Marshal(compressed)
	if err != nil {
		return fmt.Sprintf(`{"success":false,"error":"failed to marshal result: %s"}`, err)
	}
	return string(data)
}

// truncateWithMarker truncates a string and adds a truncation marker.
func truncateWithMarker(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Keep first and last portions for context
	keepStart := maxLen * 2 / 3
	keepEnd := maxLen / 6
	marker := fmt.Sprintf("\n\n...[truncated %d chars]...\n\n", len(s)-keepStart-keepEnd)

	return s[:keepStart] + marker + s[len(s)-keepEnd:]
}

// compressMapResult compresses a map result by truncating long string values.
func compressMapResult(m map[string]any, maxChars int) map[string]any {
	compressed := make(map[string]any)
	totalChars := 0

	for k, v := range m {
		if totalChars >= maxChars {
			compressed["_truncated"] = true
			break
		}

		switch val := v.(type) {
		case string:
			remaining := maxChars - totalChars
			if len(val) > remaining {
				compressed[k] = truncateWithMarker(val, remaining)
				totalChars = maxChars
			} else {
				compressed[k] = val
				totalChars += len(val)
			}
		default:
			compressed[k] = v
			if data, err := json.Marshal(v); err == nil {
				totalChars += len(data)
			}
		}
	}

	return compressed
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
	cache       *ResultCache
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

// WithExecutorCache sets the result cache for the executor.
func WithExecutorCache(cache *ResultCache) ExecutorOption {
	return func(e *Executor) {
		e.cache = cache
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

	// Check cache BEFORE tool lookup (only if tool is enabled for caching)
	if e.cache != nil {
		if cachedResult, hit := e.cache.Get(toolName, args); hit {
			e.logger.Debug("Tool result cache hit",
				"agent", e.agentID,
				"tool", toolName,
			)
			return &ExecutionResult{
				ToolCallID: toolCall.ID,
				Success:    true,
				Result:     cachedResult,
				Cached:     true,
			}
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

	// Store in cache after successful execution
	if e.cache != nil {
		e.cache.Put(toolName, args, result)
		e.logger.Debug("Cached tool result",
			"agent", e.agentID,
			"tool", toolName,
		)
	}

	return &ExecutionResult{
		ToolCallID: toolCall.ID,
		Success:    true,
		Result:     result,
		Cached:     false, // Fresh result, not from cache
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

// PlaceholderToolRegistry is a simple implementation for testing.
// For production use, prefer the full tools.Registry implementation.
type PlaceholderToolRegistry struct {
	tools map[string]tools.Tool
}

// NewPlaceholderToolRegistry creates a new placeholder registry.
func NewPlaceholderToolRegistry() *PlaceholderToolRegistry {
	return &PlaceholderToolRegistry{
		tools: make(map[string]tools.Tool),
	}
}

// Register adds a tool to the registry.
func (r *PlaceholderToolRegistry) Register(tool tools.Tool) {
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name.
func (r *PlaceholderToolRegistry) Get(name string) tools.Tool {
	return r.tools[name]
}

// List returns all available tools.
func (r *PlaceholderToolRegistry) List() []tools.Tool {
	tools := make([]tools.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// GetDefinitions returns tool definitions for the LLM.
func (r *PlaceholderToolRegistry) GetDefinitions() []llm.ToolDefinition {
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

// MockTool is a mock tool for testing.
type MockTool struct {
	name        string
	description string
	parameters  llm.FunctionParameters
	executeFunc func(ctx context.Context, args map[string]any) (any, error)
}

// NewMockTool creates a new mock tool.
func NewMockTool(name, description string, fn func(ctx context.Context, args map[string]any) (any, error)) *MockTool {
	return &MockTool{
		name:        name,
		description: description,
		parameters: llm.FunctionParameters{
			Type:       "object",
			Properties: map[string]llm.ParameterProperty{},
		},
		executeFunc: fn,
	}
}

// NewMockToolWithParams creates a new mock tool with custom parameters.
func NewMockToolWithParams(name, description string, params llm.FunctionParameters, fn func(ctx context.Context, args map[string]any) (any, error)) *MockTool {
	return &MockTool{
		name:        name,
		description: description,
		parameters:  params,
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

// Parameters returns the JSON Schema parameters for this tool.
func (t *MockTool) Parameters() llm.FunctionParameters {
	return t.parameters
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
func (r *FilteredToolRegistry) Get(name string) tools.Tool {
	if len(r.allowed) > 0 && !r.allowed[name] {
		return nil
	}
	return r.parent.Get(name)
}

// List returns only allowed tools.
func (r *FilteredToolRegistry) List() []tools.Tool {
	all := r.parent.List()
	if len(r.allowed) == 0 {
		return all
	}

	filtered := make([]tools.Tool, 0)
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
