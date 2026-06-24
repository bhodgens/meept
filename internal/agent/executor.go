package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/taint"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// ToolActionMap maps tool names to permission action categories.
var ToolActionMap = map[string]string{
	// File operations
	"shell":           ToolShellExecute,
	ToolFileRead:      ToolFileRead,
	ToolFileWrite:     ToolFileWrite,
	ToolFileDelete:    ToolFileDelete,
	ToolListDirectory: ToolFileRead,

	// Network operations
	ToolWebSearch: "network_request",
	ToolWebFetch:  "network_request",

	// Memory operations
	ToolMemorySearch:     "memory_read",
	ToolMemoryGetContext: "memory_read",
	ToolMemoryStore:      "memory_write",
	ToolMemoryDelete:     "memory_write",

	// Platform introspection (read-only, safe)
	ToolPlatformStatus: "platform_read",
	ToolPlatformAgents: "platform_read",
	ToolPlatformTools:  "platform_read",

	// Task management
	"task_create": "task_write",
	"task_get":    "task_read",
	"task_list":   "task_read",
	"task_update": "task_write",

	// Agent delegation
	"delegate_task":   "agent_delegate",
	ToolRequestReview: "agent_delegate",

	// Code intelligence - AST (read-only, safe)
	"ast_parse":   ToolCodeRead,
	"ast_symbols": ToolCodeRead,
	"ast_query":   ToolCodeRead,

	// Code intelligence - LSP (read-only, requires server)
	"lsp_goto_definition":   ToolCodeRead,
	"lsp_find_references":   ToolCodeRead,
	"lsp_hover":             ToolCodeRead,
	"lsp_workspace_symbols": ToolCodeRead,
	"lsp_diagnostics":       ToolCodeRead,
}

// ToolRegistry provides access to available tools.
//
// This is the agent-layer interface for looking up and listing tools. It is
// intentionally narrower than the production [tools.Registry] so that unit
// tests can substitute [PlaceholderToolRegistry] without dragging in the
// full tool registry graph.
//
// Relationship to other interfaces:
//   - [tools.Tool]           -- interface for a single tool (Name, Description, Parameters, Execute)
//   - agent.ToolRegistry     -- this interface: a collection of tools with lookup (Get, List, GetDefinitions)
//   - [tools.Registry]       -- production implementation that satisfies agent.ToolRegistry
//   - [tools.ToolExecutor]   -- interface for executing a tool by name with permission checks
//
// The production [tools.Registry] satisfies this interface; see
// [tools.Registry.GetDefinitions] which is an alias for compatibility.
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
	ToolCallID string            `json:"tool_call_id"`
	Success    bool              `json:"success"`
	Result     any               `json:"result,omitempty"`
	Error      string            `json:"error,omitempty"`
	Cached     bool              `json:"cached,omitempty"`    // True if result came from cache
	Evidence   []models.Evidence `json:"evidence,omitempty"`  // Evidence of tool side-effects
	Terminate  bool              `json:"terminate,omitempty"` // Advisory: hint that result is final and needs no LLM follow-up
	// TaintLabel is the provenance taint propagated from ToolResult.
	// When non-empty, downstream policy checks can apply stricter rules
	// (e.g., blocking the tainted value from reaching shell_exec).
	TaintLabel taint.TaintLabel `json:"taint_label,omitempty"`
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
		TaintLabel: r.TaintLabel,
	}

	// Handle the result based on type
	switch result := r.Result.(type) {
	case string:
		if looksLikeCode(result) {
			compressed.Result = compressCodeResult(result, maxChars-200)
		} else {
			compressed.Result = truncateWithMarker(result, maxChars-200) // Reserve space for JSON wrapper
		}
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
	if maxLen <= 0 {
		return "...[truncated]"
	}
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
				if looksLikeCode(val) {
					compressed[k] = compressCodeResult(val, remaining)
				} else {
					compressed[k] = truncateWithMarker(val, remaining)
				}
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

// looksLikeCode performs a heuristic check to determine if a string resembles
// source code. It checks for common code indicators like keywords, braces,
// and structural patterns.
func looksLikeCode(s string) bool {
	if len(s) < 20 {
		return false
	}

	// Check for common code keywords/patterns
	codeIndicators := []string{
		"func ", "package ", "import ", "type ", "struct ", "interface ",
		"func(", "func (",
		"def ", "class ", "async def ",
		"fn ", "impl ", "pub fn ", "pub struct ",
		"void ", "int main(", "public class ", "private ",
		"function ", "const ", "let ", "var ",
		"module ", "require(", "#include",
	}

	// Count how many indicators match
	matches := 0
	for _, indicator := range codeIndicators {
		if strings.Contains(s, indicator) {
			matches++
		}
	}

	// If we find 2 or more indicators, it's likely code
	if matches >= 2 {
		return true
	}

	// Check for structural patterns: balanced braces with content
	braceCount := 0
	hasStructuralContent := false
	lines := strings.SplitSeq(s, "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, ch := range trimmed {
			switch ch {
			case '{':
				braceCount++
			case '}':
				braceCount--
			}
		}
		// Check for lines that look like declarations
		if strings.HasSuffix(trimmed, "{") ||
			strings.HasPrefix(trimmed, "//") ||
			(strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#!")) {
			hasStructuralContent = true
		}
	}

	// Balanced braces with structural content suggests code
	return braceCount == 0 && hasStructuralContent && matches >= 1
}

// compressCodeResult compresses a code string using AST-aware compression.
// It detects the language from content heuristics, then uses tree-sitter
// to preserve function signatures and type definitions while compressing bodies.
// Falls back to simple truncation if the language cannot be detected or parsed.
func compressCodeResult(code string, maxChars int) string {
	if len(code) <= maxChars {
		return code
	}

	lang := detectLanguageFromContent(code)
	if lang == ast.LangUnknown {
		return truncateWithMarker(code, maxChars)
	}

	compressed := ast.CompressCodeAtBoundaries([]byte(code), lang, maxChars)
	if len(compressed) > maxChars+50 {
		// AST compression produced something still too large; fallback
		return truncateWithMarker(code, maxChars)
	}
	return compressed
}

// detectLanguageFromContent attempts to determine the programming language
// of a code string using content-based heuristics.
func detectLanguageFromContent(s string) ast.Language {
	// Go indicators
	if strings.Contains(s, "package ") && (strings.Contains(s, "func ") || strings.Contains(s, "func(")) {
		return ast.LangGo
	}
	if strings.Contains(s, "func (") && strings.Contains(s, "type ") {
		return ast.LangGo
	}

	// Python indicators
	if (strings.HasPrefix(s, "def ") || strings.Contains(s, "\ndef ")) &&
		!strings.Contains(s, "func ") {
		return ast.LangPython
	}
	if strings.Contains(s, "class ") && strings.Contains(s, "def ") &&
		strings.Contains(s, "self") {
		return ast.LangPython
	}

	// Rust indicators
	if strings.Contains(s, "fn ") && (strings.Contains(s, "let ") || strings.Contains(s, "impl ") || strings.Contains(s, "pub ")) {
		return ast.LangRust
	}
	if strings.Contains(s, "pub fn ") || strings.Contains(s, "pub struct ") {
		return ast.LangRust
	}

	// JavaScript/TypeScript indicators
	if (strings.Contains(s, "function ") || strings.Contains(s, "= function") || strings.Contains(s, "=> ")) &&
		(strings.Contains(s, "const ") || strings.Contains(s, "let ") || strings.Contains(s, "var ")) {
		return ast.LangJavaScript
	}

	// Java indicators
	if (strings.Contains(s, "public class ") || strings.Contains(s, "private ")) && strings.Contains(s, "void ") {
		return ast.LangJava
	}

	// C/C++ indicators
	if strings.Contains(s, "#include") && (strings.Contains(s, "int main") || strings.Contains(s, "void ")) {
		return ast.LangC
	}

	// Ruby indicators
	if strings.Contains(s, "def ") && strings.Contains(s, "end") && !strings.Contains(s, "func ") {
		return ast.LangRuby
	}

	return ast.LangUnknown
}

// ToChatMessage converts the result to a tool role chat message.
func (r *ExecutionResult) ToChatMessage() llm.ChatMessage {
	return llm.ChatMessage{
		Role:        llm.RoleTool,
		Content:     r.ToJSON(),
		ToolCallID:  r.ToolCallID,
		IsToolError: !r.Success,
	}
}

// Executor handles tool execution with security checks.
type Executor struct {
	mu          sync.RWMutex
	registry    ToolRegistry
	security    *security.PermissionChecker
	logger      *slog.Logger
	parallelism int
	agentID     string          // Identifier for the agent/worker using this executor
	cache       *ResultCache    // Tool result cache
	bus         *bus.MessageBus // Optional: for publishing streaming progress events
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
		if cache != nil {
			e.cache = cache
		}
	}
}

// WithExecutorBus sets the message bus for streaming progress events.
func WithExecutorBus(msgBus *bus.MessageBus) ExecutorOption {
	return func(e *Executor) {
		if msgBus != nil {
			e.bus = msgBus
		}
	}
}

// NewExecutor creates a new tool executor.
func NewExecutor(registry ToolRegistry, permChecker *security.PermissionChecker, opts ...ExecutorOption) *Executor {
	e := &Executor{
		registry:    registry,
		security:    permChecker,
		logger:      slog.Default(),
		parallelism: 4, // Default parallelism
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// SetRegistry updates the tool registry used by this executor.
// This is used when the registry needs to be swapped (e.g., for skill execution
// with filtered tools). AGENT-6 fix.
func (e *Executor) SetRegistry(registry ToolRegistry) {
	if registry == nil {
		return
	}
	e.mu.Lock()
	e.registry = registry
	e.mu.Unlock()
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
			// Emit synthetic cache-hit progress event
			e.publishToolProgress(ctx, toolCall.ID, toolName, tools.ProgressUpdate{
				Message:    "cache hit",
				Percent:    100,
				ToolCallID: toolCall.ID,
			})
			cachedExecResult := &ExecutionResult{
				ToolCallID: toolCall.ID,
				Success:    true,
				Result:     cachedResult,
				Cached:     true,
			}
			e.publishToolComplete(toolCall.ID, toolName, cachedExecResult)
			return cachedExecResult
		}
	}

	// Look up tool
	e.mu.RLock()
	registry := e.registry
	e.mu.RUnlock()
	if registry == nil {
		return &ExecutionResult{
			ToolCallID: toolCall.ID,
			Success:    false,
			Error:      "tool registry not configured",
		}
	}

	tool := registry.Get(toolName)
	if tool == nil {
		e.logger.Warn("Unknown tool requested", "tool", toolName)
		return &ExecutionResult{
			ToolCallID: toolCall.ID,
			Success:    false,
			Error:      fmt.Sprintf("unknown tool: %s", toolName),
		}
	}

	// Security check - FAIL CLOSED: require security to be configured
	if e.security == nil {
		// Fail-closed: security not configured, block all tool execution except safe introspection
		allowedSafeTools := map[string]bool{
			ToolPlatformStatus:   true,
			ToolPlatformAgents:   true,
			ToolPlatformTools:    true,
			ToolMemorySearch:     true,
			ToolMemoryGetContext: true,
		}
		if !allowedSafeTools[toolName] {
			e.logger.Error("Tool execution blocked: security not configured (fail-closed)",
				"agent", e.agentID,
				"tool", toolName,
			)
			return &ExecutionResult{
				ToolCallID: toolCall.ID,
				Success:    false,
				Error:      "security system not configured - tool execution blocked by fail-closed policy",
			}
		}
	} else {
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

	// Check if tool supports streaming progress
	var toolResult any
	var toolErr error
	if st, ok := tool.(tools.StreamingTool); ok && e.bus != nil {
		toolResult, toolErr = st.ExecuteStreaming(ctx, args, func(pu tools.ProgressUpdate) {
			pu.ToolCallID = toolCall.ID
			e.publishToolProgress(ctx, toolCall.ID, toolName, pu)
		})
	} else {
		toolResult, toolErr = tool.Execute(ctx, args)
	}
	if toolErr != nil {
		e.logger.Error("Tool execution failed",
			"agent", e.agentID,
			"tool", toolName,
			"error", toolErr,
		)
		return &ExecutionResult{
			ToolCallID: toolCall.ID,
			Success:    false,
			Error:      fmt.Sprintf("tool execution failed: %v", toolErr),
		}
	}

	// Extract evidence from ToolResult if present
	var evidence []models.Evidence
	var terminate bool
	var label taint.TaintLabel
	if tr, ok := toolResult.(*tools.ToolResult); ok && tr != nil {
		if len(tr.Evidence) > 0 {
			evidence = tr.Evidence
			e.logger.Debug("Tool produced evidence",
				"agent", e.agentID,
				"tool", toolName,
				"evidence_count", len(evidence),
			)
		}
		// Check ToolResult-level Terminate flag
		if tr.Terminate {
			terminate = true
		}
		// Propagate the taint label so downstream policy checks apply.
		label = tr.TaintLabel
		// Use the actual result from ToolResult
		toolResult = tr.Result
	}

	// Check TerminatingTool interface for per-call terminate hint
	if tt, ok := tool.(tools.TerminatingTool); ok {
		if tt.TerminateHint(args) {
			terminate = true
		}
	}

	// Store in cache after successful execution (cache the result, not evidence)
	if e.cache != nil {
		e.cache.Put(toolName, args, toolResult)
		e.logger.Debug("Cached tool result",
			"agent", e.agentID,
			"tool", toolName,
		)
	}

	result := &ExecutionResult{
		ToolCallID: toolCall.ID,
		Success:    true,
		Result:     toolResult,
		Cached:     false, // Fresh result, not from cache
		Evidence:   evidence,
		Terminate:  terminate,
		TaintLabel: label,
	}

	// Publish tool.execution.complete event after successful execution
	e.publishToolComplete(toolCall.ID, toolName, result)

	return result
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

// publishToolProgress emits a tool execution progress event on the bus.
func (e *Executor) publishToolProgress(_ context.Context, toolCallID, toolName string, pu tools.ProgressUpdate) {
	if e.bus == nil {
		return
	}
	payload := map[string]any{
		"tool_call_id": toolCallID,
		"tool_name":    toolName,
		KeyAgentID:     e.agentID,
		"message":      pu.Message,
		"percent":      pu.Percent,
	}
	if len(pu.PartialResult) > 0 {
		payload["partial_result"] = pu.PartialResult
	}
	msg, err := models.NewBusMessage(models.MessageTypeStatusUpdate, "executor", payload)
	if err != nil {
		e.logger.Warn("Failed to create progress bus message", "error", err)
		return
	}
	e.bus.Publish("tool.execution.progress", msg)
}

// publishToolComplete emits a tool.execution.complete event on the bus.
func (e *Executor) publishToolComplete(toolCallID, toolName string, result *ExecutionResult) {
	if e.bus == nil {
		return
	}
	payload := map[string]any{
		"tool_call_id": toolCallID,
		"tool_name":    toolName,
		KeyAgentID:     e.agentID,
		"success":      result.Success,
		"terminate":    result.Terminate,
		"cached":       result.Cached,
	}

	// Extract edited files from file_edit tool results.
	// The result summary format is: "Applied N edit(s) to /path/to/file (X lines -> Y lines)"
	// For pending changes: "Created pending change ... for /path/to/file ..."
	if toolName == "file_edit" && result.Success && result.Result != nil {
		if resultStr, ok := result.Result.(string); ok {
			if files := extractEditedFiles(resultStr); len(files) > 0 {
				payload["edited_files"] = files
			}
		}
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "executor", payload)
	if err != nil {
		e.logger.Warn("Failed to create complete bus message", "error", err)
		return
	}
	e.bus.Publish("tool.execution.complete", msg)
}

// extractEditedFiles parses a file_edit result summary and returns the file paths.
// Handles both direct mode ("Applied N edit(s) to /path") and pending changes mode
// ("Created pending change ... for /path").
func extractEditedFiles(summary string) []string {
	// Pending changes format: "Created pending change <id> for <path> (<edits> -> <lines> lines)..."
	if idx := strings.Index(summary, " for "); idx != -1 {
		rest := summary[idx+4:]
		// Extract path up to the "(" that precedes line counts
		if parenIdx := strings.Index(rest, " ("); parenIdx != -1 {
			return []string{strings.TrimSpace(rest[:parenIdx])}
		}
		return []string{strings.TrimSpace(rest)}
	}
	// Direct mode format: "Applied N edit(s) to /path (X lines -> Y lines)"
	if idx := strings.Index(summary, " to "); idx != -1 {
		rest := summary[idx+4:]
		if parenIdx := strings.Index(rest, " ("); parenIdx != -1 {
			return []string{strings.TrimSpace(rest[:parenIdx])}
		}
		return []string{strings.TrimSpace(rest)}
	}
	return nil
}

// ShouldTerminate checks if ALL results in the batch indicate termination.
// Returns true only if every result has Terminate=true and at least one result exists.
func ShouldTerminate(results []*ExecutionResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if r == nil || !r.Terminate {
			return false
		}
	}
	return true
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
	result := make([]tools.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
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
