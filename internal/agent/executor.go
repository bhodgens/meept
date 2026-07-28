package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/code/ast"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/taint"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// ToolConcurrencyProfile categorizes tools by their resource profile to drive
// adaptive parallelism (Phase 3 of parallel-tool-execution plan).
type ToolConcurrencyProfile uint8

const (
	// ProfileIOBound: network, file I/O. High parallelism safe.
	ProfileIOBound ToolConcurrencyProfile = iota
	// ProfileCPUBound: AST parsing, code analysis. Limited parallelism.
	ProfileCPUBound
	// ProfileStateful: shell sessions, DB connections. Sequential per resource.
	ProfileStateful
	// ProfileExclusive: git commit, global mutations. One at a time.
	ProfileExclusive
)

// String returns the human-readable name of the profile.
func (p ToolConcurrencyProfile) String() string {
	switch p {
	case ProfileIOBound:
		return "io_bound"
	case ProfileCPUBound:
		return "cpu_bound"
	case ProfileStateful:
		return "stateful"
	case ProfileExclusive:
		return "exclusive"
	default:
		return "unknown"
	}
}

// toolProfiles maps tool names to their concurrency profiles.
// Tools not listed here default to ProfileIOBound.
var toolProfiles = map[string]ToolConcurrencyProfile{
	"web_search":    ProfileIOBound,
	"web_fetch":     ProfileIOBound,
	"file_read":     ProfileIOBound,
	"file_write":    ProfileIOBound,
	"file_edit":     ProfileIOBound,
	"memory_search": ProfileIOBound,
	"ast_parse":     ProfileCPUBound,
	"ast_query":     ProfileCPUBound,
	"shell":         ProfileStateful,
	"bash":          ProfileStateful,
	"git_diff":      ProfileExclusive,
	"git_commit":    ProfileExclusive,
}

// maxAdaptiveLimit is the ceiling for any profile's limit after runtime
// adjustment via AdjustLimits.
const maxAdaptiveLimit = 20

// ExecutionMetrics captures runtime statistics for adaptive parallelism tuning.
type ExecutionMetrics struct {
	AvgLatency      time.Duration `json:"avg_latency"`
	ErrorRate       float64       `json:"error_rate"`
	ActiveGoroutines int          `json:"active_goroutines"`
	Throughput      float64       `json:"throughput"`
}

// AdaptiveParallelismLimiter limits concurrency per ToolConcurrencyProfile.
// Each profile has its own semaphore channel; tools within the same profile
// share that profile's slot pool.
type AdaptiveParallelismLimiter struct {
	mu     sync.Mutex
	slots  map[ToolConcurrencyProfile]chan struct{}
	limits map[ToolConcurrencyProfile]int
	logger *slog.Logger
}

// NewAdaptiveParallelismLimiter creates a limiter where baseParallelism is the
// IO-bound slot limit. CPU-bound gets max(1, baseParallelism/2); stateful and
// exclusive each get 1.
func NewAdaptiveParallelismLimiter(baseParallelism int) *AdaptiveParallelismLimiter {
	if baseParallelism < 1 {
		baseParallelism = 1
	}
	cpuBoundLimit := baseParallelism / 2
	if cpuBoundLimit < 1 {
		cpuBoundLimit = 1
	}

	limits := map[ToolConcurrencyProfile]int{
		ProfileIOBound:    baseParallelism,
		ProfileCPUBound:   cpuBoundLimit,
		ProfileStateful:   1,
		ProfileExclusive:  1,
	}
	slots := make(map[ToolConcurrencyProfile]chan struct{}, len(limits))
	for profile, lim := range limits {
		slots[profile] = make(chan struct{}, lim)
	}

	return &AdaptiveParallelismLimiter{
		slots:  slots,
		limits: limits,
	}
}

// Acquire blocks until a slot for the given profile is available or ctx is
// cancelled.
func (l *AdaptiveParallelismLimiter) Acquire(ctx context.Context, profile ToolConcurrencyProfile) error {
	ch := l.slotChannel(profile)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ch <- struct{}{}:
		return nil
	}
}

// Release frees a slot for the given profile. It is non-blocking; calling
// Release without a matching Acquire is a programming error.
func (l *AdaptiveParallelismLimiter) Release(profile ToolConcurrencyProfile) {
	ch := l.slotChannel(profile)
	select {
	case <-ch:
	default:
		// Slot already free; this should not happen in correct usage.
	}
}

// ProfileForTool returns the concurrency profile for the given tool name.
// Unknown tools default to ProfileIOBound.
func (l *AdaptiveParallelismLimiter) ProfileForTool(toolName string) ToolConcurrencyProfile {
	if profile, ok := toolProfiles[toolName]; ok {
		return profile
	}
	return ProfileIOBound
}

// Limit returns the concurrency limit for the given profile.
func (l *AdaptiveParallelismLimiter) Limit(profile ToolConcurrencyProfile) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lim, ok := l.limits[profile]; ok {
		return lim
	}
	return 0
}

// slotChannel returns the semaphore channel for the profile, initializing it
// lazily if missing (defensive).
func (l *AdaptiveParallelismLimiter) slotChannel(profile ToolConcurrencyProfile) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch, ok := l.slots[profile]
	if !ok {
		// Unknown profile: create a single-slot channel as a safe default.
		ch = make(chan struct{}, 1)
		l.slots[profile] = ch
		l.limits[profile] = 1
	}
	return ch
}

// SetLogger sets the logger used for recording limit adjustments.
// Nil is ignored per the CLAUDE.md setter-nil-guard convention.
func (l *AdaptiveParallelismLimiter) SetLogger(logger *slog.Logger) {
	if logger != nil {
		l.logger = logger
	}
}

// AdjustLimits tunes the IO-bound and CPU-bound concurrency limits based on
// runtime execution metrics. The adjustment replaces the semaphore channel
// pointer under the write lock so that the change only affects NEW Acquire
// calls; goroutines already holding slots on the old channel are unaffected.
// Old channels are garbage-collected once all holders drain.
//
// Rules:
//   - High error rate (> 0.5): reduce ioBoundLimit and cpuBoundLimit by 1 (floor 1).
//   - Healthy system (error rate < 0.05 AND avg latency < 500ms): increase
//     ioBoundLimit by 1 (cap at maxAdaptiveLimit).
//   - ProfileStateful and ProfileExclusive are never adjusted (always limit 1).
func (l *AdaptiveParallelismLimiter) AdjustLimits(metrics ExecutionMetrics) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if metrics.ErrorRate > 0.5 {
		l.shrinkLimit(ProfileIOBound)
		l.shrinkLimit(ProfileCPUBound)
		if l.logger != nil {
			l.logger.Debug("adjustlimits: reduced concurrency due to high error rate",
				"error_rate", metrics.ErrorRate,
				"io_bound_limit", l.limits[ProfileIOBound],
				"cpu_bound_limit", l.limits[ProfileCPUBound])
		}
		return
	}

	if metrics.ErrorRate < 0.05 && metrics.AvgLatency < 500*time.Millisecond {
		l.growLimit(ProfileIOBound)
		if l.logger != nil {
			l.logger.Debug("adjustlimits: increased io-bound concurrency due to healthy metrics",
				"error_rate", metrics.ErrorRate,
				"avg_latency_ms", metrics.AvgLatency.Milliseconds(),
				"io_bound_limit", l.limits[ProfileIOBound])
		}
	}
}

// shrinkLimit decrements the profile's limit by 1 (floor 1) and rebuilds the
// semaphore channel. Caller must hold l.mu.
func (l *AdaptiveParallelismLimiter) shrinkLimit(profile ToolConcurrencyProfile) {
	current := l.limits[profile]
	if current <= 1 {
		return // already at floor
	}
	newLimit := current - 1
	l.limits[profile] = newLimit
	l.slots[profile] = make(chan struct{}, newLimit)
}

// growLimit increments the profile's limit by 1 (cap maxAdaptiveLimit) and
// rebuilds the semaphore channel. Caller must hold l.mu.
func (l *AdaptiveParallelismLimiter) growLimit(profile ToolConcurrencyProfile) {
	current := l.limits[profile]
	if current >= maxAdaptiveLimit {
		return // already at ceiling
	}
	newLimit := current + 1
	l.limits[profile] = newLimit
	l.slots[profile] = make(chan struct{}, newLimit)
}

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
	"project_info":     "platform_read",

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

// WorkingDirSetter is implemented by tools that accept a session-scoped
// working directory resolver (e.g. ProjectInfoTool). The agent layer uses
// this interface to avoid importing the builtin package (which imports agent).
type WorkingDirSetter interface {
	SetWorkingDirFunc(fn func() string)
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
	// CascadeFrom is the tool call ID that caused this failure (if cascading).
	CascadeFrom string `json:"cascade_from,omitempty"`
	// IsCascading is true when this failure is a downstream effect of an earlier failure.
	IsCascading bool `json:"is_cascading,omitempty"`
}

// ErrorSeverity classifies the severity of a parallel execution error.
type ErrorSeverity string

const (
	ErrorSeverityCritical ErrorSeverity = "critical"
	ErrorSeverityWarning  ErrorSeverity = "warning"
	ErrorSeverityInfo     ErrorSeverity = "info"
)

// ParallelExecutionError aggregates errors from multiple tool calls in a parallel batch.
type ParallelExecutionError struct {
	ToolCallID    string        `json:"tool_call_id"`
	Message       string        `json:"error"`
	IsCascading   bool          `json:"is_cascading"`
	CascadeSource string        `json:"cascade_source"`
	RelatedErrors []string      `json:"related_errors"`
	Severity      ErrorSeverity `json:"severity"`
}

// Error returns the error string for the parallel execution error.
func (e *ParallelExecutionError) Error() string {
	if e.CascadeSource != "" {
		return fmt.Sprintf("tool %s failed (cascading from %s): %s", e.ToolCallID, e.CascadeSource, e.Message)
	}
	return fmt.Sprintf("tool %s failed: %s", e.ToolCallID, e.Message)
}

// detectCascadingErrors marks downstream failures caused by an upstream failure.
// When a tool call fails and other calls depended on it, those dependent calls
// would have been skipped or received no data — their failures are cascading
// symptoms rather than independent root causes.
func detectCascadingErrors(results []*ExecutionResult, graph *ToolDependencyGraph) []*ExecutionResult {
	if graph == nil || len(results) == 0 {
		return results
	}

	// Build a set of failed tool call IDs.
	failedIDs := make(map[string]bool)
	for _, r := range results {
		if r == nil {
			continue
		}
		if !r.Success {
			failedIDs[r.ToolCallID] = true
		}
	}

	// For each failed result, check if it had upstream dependencies that also failed.
	// If so, mark it as cascading.
	for _, r := range results {
		if r == nil || r.Success {
			continue
		}
		deps := graph.GetDependencies(r.ToolCallID)
		for _, depID := range deps {
			if failedIDs[depID] {
				// This failure is downstream of another failure.
				r.IsCascading = true
				r.CascadeFrom = depID
				break
			}
		}
	}

	return results
}

// AggregateErrors converts ExecutionResult failures into a ParallelExecutionError
// slice with severity heuristics for downstream observability.
// Cascading failures get Warning severity (they are symptoms, not root causes).
// Non-cascading failures get Critical severity.
func AggregateErrors(results []*ExecutionResult) []*ParallelExecutionError {
	var errors []*ParallelExecutionError
	for _, r := range results {
		if r == nil || r.Success {
			continue
		}
		pe := &ParallelExecutionError{
			ToolCallID:  r.ToolCallID,
			Message:     r.Error,
			IsCascading: r.IsCascading,
		}
		if r.IsCascading {
			pe.CascadeSource = r.CascadeFrom
			pe.Severity = ErrorSeverityWarning
		} else {
			pe.Severity = ErrorSeverityCritical
		}
		errors = append(errors, pe)
	}
	// Populate RelatedErrors: each cascading error references the root cause's
	// tool call ID and vice versa.
	for _, pe := range errors {
		if pe.IsCascading && pe.CascadeSource != "" {
			for _, root := range errors {
				if root.ToolCallID == pe.CascadeSource {
					root.RelatedErrors = append(root.RelatedErrors, pe.ToolCallID)
				}
			}
		}
	}
	if errors == nil {
		return nil
	}
	return errors
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
	mu                 sync.RWMutex
	registry           ToolRegistry
	security           *security.PermissionChecker
	logger             *slog.Logger
	parallelism        int
	agentID            string // Identifier for the agent/worker using this executor
	cache              *ResultCache
	bus                *bus.MessageBus // Optional: for publishing streaming progress events
	depInferrer        *DependencyInferrer
	parallelismLimiter *AdaptiveParallelismLimiter
	retryMetrics       *RetryMetrics
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

// WithExecutorInferrer sets the dependency inferrer for dependency-aware parallel
// scheduling. If nil (or not provided), the executor lazily initializes one from
// the registry when ExecuteAll is first called.
func WithExecutorInferrer(inferrer *DependencyInferrer) ExecutorOption {
	return func(e *Executor) {
		if inferrer != nil {
			e.depInferrer = inferrer
		}
	}
}

// WithExecutorParallelismLimiter sets a custom adaptive parallelism limiter.
// If nil (or not provided), the executor initializes a default limiter with
// baseParallelism matching the parallelism setting.
func WithExecutorParallelismLimiter(limiter *AdaptiveParallelismLimiter) ExecutorOption {
	return func(e *Executor) {
		if limiter != nil {
			e.parallelismLimiter = limiter
		}
	}
}

// WithExecutorRetryMetrics sets the retry metrics collector for the executor.
// If nil (or not provided), retry events are not recorded.
func WithExecutorRetryMetrics(m *RetryMetrics) ExecutorOption {
	return func(e *Executor) {
		if m != nil {
			e.retryMetrics = m
		}
	}
}

// NewExecutor creates a new tool executor.
func NewExecutor(registry ToolRegistry, permChecker *security.PermissionChecker, opts ...ExecutorOption) *Executor {
	e := &Executor{
		registry:           registry,
		security:           permChecker,
		logger:             slog.Default(),
		parallelism:        4, // Default parallelism
		parallelismLimiter: NewAdaptiveParallelismLimiter(4),
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

// SetRetryMetrics sets the retry metrics collector after executor construction.
// Nil-guarded per CLAUDE.md setter convention.
func (e *Executor) SetRetryMetrics(m *RetryMetrics) {
	if m == nil {
		return
	}
	e.mu.Lock()
	e.retryMetrics = m
	e.mu.Unlock()
}

// SetAgentID updates the agent/employee identifier used by this executor.
// The agentID is injected into the permission check details map so the
// PermissionChecker can route to the registered PreExecChecker (employee
// constitution gate). This is safe to call per-invocation from the agent
// loop; the value is snapshotted under a read lock in checkPermission.
func (e *Executor) SetAgentID(id string) {
	e.mu.Lock()
	e.agentID = id
	e.mu.Unlock()
}

// SetParallelismConfig adjusts the adaptive parallelism limiter from config.
// baseParallelism sets the IO-bound limit (and derives CPU-bound as base/2
// when no explicit cpu_bound profile is given). Profiles map keys: "io_bound",
// "cpu_bound", "stateful", "exclusive". Nil-guarded per CLAUDE.md.
func (e *Executor) SetParallelismConfig(baseParallelism int, profiles map[string]int) {
	if e == nil || e.parallelismLimiter == nil || baseParallelism <= 0 {
		return
	}

	// Build the resize map from profile name strings to ToolConcurrencyProfile.
	resizeMap := make(map[ToolConcurrencyProfile]int, 4)

	// Apply explicit profile values.
	for name, parallelism := range profiles {
		if parallelism < 1 {
			parallelism = 1
		}
		switch name {
		case "io_bound":
			resizeMap[ProfileIOBound] = parallelism
		case "cpu_bound":
			resizeMap[ProfileCPUBound] = parallelism
		case "stateful":
			resizeMap[ProfileStateful] = parallelism
		case "exclusive":
			resizeMap[ProfileExclusive] = parallelism
		}
	}

	// If io_bound wasn't explicitly set, use baseParallelism.
	if _, ok := resizeMap[ProfileIOBound]; !ok {
		resizeMap[ProfileIOBound] = baseParallelism
	}
	// If cpu_bound wasn't explicitly set, derive from base.
	if _, ok := resizeMap[ProfileCPUBound]; !ok {
		cpuLimit := baseParallelism / 2
		if cpuLimit < 1 {
			cpuLimit = 1
		}
		resizeMap[ProfileCPUBound] = cpuLimit
	}

	e.mu.Lock()
	e.parallelism = baseParallelism
	limiter := e.parallelismLimiter
	e.mu.Unlock()

	limiter.Resize(resizeMap)
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
			"project_info":       true,
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

	// Execute with retry on retryable errors. Only the actual tool execution
	// call is retried — all earlier exit paths (cache hit, unknown tool,
	// permission denied, security block) are deterministic failures that
	// bypass this code entirely.
	retryConfig := e.getRetryConfigForTool(toolName)
	toolResult, toolErr := e.executeToolWithRetry(ctx, tool, retryConfig, toolCall.ID, toolName, args)
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

// executeToolWithRetry runs a single tool's execution with exponential backoff
// retry on retryable errors. Non-retryable errors and successes return
// immediately. Streaming tools use their streaming path when the bus is
// available.
func (e *Executor) executeToolWithRetry(ctx context.Context, tool tools.Tool, config BackoffConfig, toolCallID, toolName string, args map[string]any) (any, error) {
	backoff := NewBackoff(config)
	var lastErr error

	for {
		// Check context before attempting.
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}

		var toolResult any
		var toolErr error
		if st, ok := tool.(tools.StreamingTool); ok && e.bus != nil {
			toolResult, toolErr = st.ExecuteStreaming(ctx, args, func(pu tools.ProgressUpdate) {
				pu.ToolCallID = toolCallID
				e.publishToolProgress(ctx, toolCallID, toolName, pu)
			})
		} else {
			toolResult, toolErr = tool.Execute(ctx, args)
		}

		if toolErr == nil {
			return toolResult, nil
		}

		lastErr = toolErr

		// Only retry if the error is classified as retryable.
		if !IsRetryable(toolErr) {
			return nil, toolErr
		}

		e.logger.Debug("Tool execution failed (retryable), will retry",
			"agent", e.agentID,
			"tool", toolName,
			"error", toolErr,
		)

		// Honor Retry-After hint if present, otherwise use backoff.
		if retryAfter := GetRetryAfter(toolErr); retryAfter > 0 {
			if e.retryMetrics != nil {
				e.retryMetrics.RecordRetry("tool", toolErr, retryAfter)
			}
			select {
			case <-ctx.Done():
				return nil, lastErr
			case <-time.After(retryAfter):
			}
		} else {
			if e.retryMetrics != nil {
				e.retryMetrics.RecordRetry("tool", toolErr, 0)
			}
			if !backoff.Sleep(ctx) {
				// Context cancelled or max attempts exhausted.
				return nil, lastErr
			}
		}
	}
}

// getRetryConfigForTool returns the backoff configuration appropriate for the
// given tool. Network tools get more retries with longer delays; shell tools
// get fewer retries with shorter delays; all others use the default config.
// Per-operation config overrides (from config.Agent.Retry.PerOperation) are
// consulted for "tool_web" and "tool_shell" keys.
func (e *Executor) getRetryConfigForTool(toolName string) BackoffConfig {
	// Network tools: more retries, longer delays.
	if toolName == "web_search" || toolName == "web_fetch" {
		preset := BackoffConfig{
			BaseDelay:   2 * time.Second,
			MaxDelay:    30 * time.Second,
			Multiplier:  2.0,
			Jitter:      0.3,
			MaxAttempts: 5,
		}
		return applyOverride(preset, getPerOperationOverride("tool_web"))
	}
	// Shell commands: fewer retries, shorter delays.
	if toolName == "shell" {
		preset := BackoffConfig{
			BaseDelay:   1 * time.Second,
			MaxDelay:    10 * time.Second,
			Multiplier:  1.5,
			Jitter:      0.2,
			MaxAttempts: 2,
		}
		return applyOverride(preset, getPerOperationOverride("tool_shell"))
	}
	// Default.
	return DefaultBackoffConfig()
}

// trackCombinedTaint creates a ParallelTaintTracker, records taint labels from
// successful results, and logs a warning if the combined taint is non-none.
// This is observability-only — results are not modified.
func (e *Executor) trackCombinedTaint(results []*ExecutionResult) {
	tracker := taint.NewParallelTaintTracker()
	var successfulCallIDs []string
	for _, r := range results {
		if r == nil || !r.Success {
			continue
		}
		if r.TaintLabel != taint.TaintNone && r.TaintLabel != "" {
			tracker.RecordTaint(r.ToolCallID, r.TaintLabel)
		}
		successfulCallIDs = append(successfulCallIDs, r.ToolCallID)
	}
	combinedTaint := tracker.GetCombinedTaint(successfulCallIDs)
	if combinedTaint != taint.TaintNone && combinedTaint != "" {
		e.logger.Warn("parallel execution produced combined taint",
			"taint", combinedTaint,
			"tool_calls", successfulCallIDs,
		)
	}
}

// profileForTool returns the concurrency profile for the given tool, preferring
// tool-declared safety over the static toolProfiles map. Read-only and
// concurrency-safe tools are always ProfileIOBound (high parallelism).
func (e *Executor) profileForTool(toolName string, input map[string]any) ToolConcurrencyProfile {
	e.mu.RLock()
	registry := e.registry
	e.mu.RUnlock()
	if registry != nil {
		if tool := registry.Get(toolName); tool != nil {
			if tool.IsReadOnly(input) {
				return ProfileIOBound
			}
			if tool.IsConcurrencySafe(input) {
				return ProfileIOBound
			}
		}
	}
	if profile, ok := toolProfiles[toolName]; ok {
		return profile
	}
	return ProfileIOBound
}

// ExecuteAll runs multiple tool calls with dependency-aware scheduling.
//
// Independent tool calls are executed in parallel within each wave (group);
// dependent calls are ordered across waves. The result slice preserves the
// original input order regardless of execution grouping, so callers that
// correlate results by index (e.g., executeToolCalls) continue to work
// correctly.
func (e *Executor) ExecuteAll(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult {
	if len(toolCalls) == 0 {
		return nil
	}
	if len(toolCalls) == 1 {
		return []*ExecutionResult{e.Execute(ctx, toolCalls[0])}
	}

	graph := e.inferDependencies(toolCalls)
	groups := graph.IndependentGroups()

	// results preserves original index order regardless of group execution order.
	results := make([]*ExecutionResult, len(toolCalls))
	// Build ID -> original index map for fast lookup.
	idToIdx := make(map[string]int, len(toolCalls))
	for i, tc := range toolCalls {
		idToIdx[tc.ID] = i
	}

	for _, group := range groups {
		// Execute this group's calls in parallel.
		groupResults := e.executeParallelGroup(ctx, group)
		// Scatter results back to their original positions.
		for j, tc := range group {
			origIdx := idToIdx[tc.ID]
			if j < len(groupResults) {
				results[origIdx] = groupResults[j]
			}
		}
		if e.shouldTerminateEarly(groupResults) {
			// Fill remaining nil slots with skipped-error results.
			for i := range results {
				if results[i] == nil {
					results[i] = &ExecutionResult{
						ToolCallID: toolCalls[i].ID,
						Success:    false,
						Error:      "skipped: earlier group reported a critical error",
					}
				}
			}
			// Detect cascading errors before returning early.
			detectCascadingErrors(results, graph)
			e.trackCombinedTaint(results)
			return results
		}
	}

	// Detect cascading errors: mark downstream failures that were caused by
	// upstream failures in the dependency graph.
	detectCascadingErrors(results, graph)

	// Track combined taint from parallel execution for observability.
	e.trackCombinedTaint(results)

	return results
}

// inferDependencies builds a ToolDependencyGraph for the given calls. Falls
// back to an empty graph (all calls independent) if no inferrer is configured.
func (e *Executor) inferDependencies(toolCalls []llm.ToolCall) *ToolDependencyGraph {
	// Snapshot registry under RLock for consistency with Execute().
	e.mu.RLock()
	registry := e.registry
	e.mu.RUnlock()

	e.mu.Lock()
	if e.depInferrer == nil {
		// Lazy init if registry is available; otherwise return empty graph.
		if registry == nil {
			e.mu.Unlock()
			return NewToolDependencyGraph()
		}
		e.depInferrer = NewDependencyInferrer(registry, e.logger)
	}
	inferrer := e.depInferrer
	e.mu.Unlock()

	return inferrer.InferDependencies(toolCalls)
}

// executeParallelGroup runs a group of independent tool calls in parallel.
// Concurrency is limited per-tool via the AdaptiveParallelismLimiter when
// configured; otherwise it falls back to the executor's base parallelism.
// Results are returned in the same order as the input slice so callers can
// correlate by position within the group.
func (e *Executor) executeParallelGroup(ctx context.Context, group []llm.ToolCall) []*ExecutionResult {
	if len(group) == 0 {
		return nil
	}
	if len(group) == 1 {
		return []*ExecutionResult{e.Execute(ctx, group[0])}
	}

	results := make([]*ExecutionResult, len(group))
	var wg sync.WaitGroup

	for i, tc := range group {
		wg.Add(1)
		go func(idx int, call llm.ToolCall) {
			defer wg.Done()

			if e.parallelismLimiter != nil {
				args, _ := call.ParsedArguments()
				profile := e.profileForTool(call.Function.Name, args)
				if err := e.parallelismLimiter.Acquire(ctx, profile); err != nil {
					results[idx] = &ExecutionResult{
						ToolCallID: call.ID,
						Success:    false,
						Error:      "context cancelled acquiring slot: " + err.Error(),
					}
					return
				}
				defer e.parallelismLimiter.Release(profile)
			}

			results[idx] = e.Execute(ctx, call)
		}(i, tc)
	}

	wg.Wait()
	return results
}

// shouldTerminateEarly returns true if execution should stop due to critical
// errors in the given group's results.
func (e *Executor) shouldTerminateEarly(results []*ExecutionResult) bool {
	for _, r := range results {
		if r == nil {
			continue
		}
		if !r.Success && e.isCriticalError(r.Error) {
			return true
		}
	}
	return false
}

// isCriticalError returns true for errors that should halt execution of
// subsequent dependency groups. The check is case-insensitive so that wrapped
// errors with non-standard casing (e.g. "Permission Denied") are still caught.
func (e *Executor) isCriticalError(err string) bool {
	lower := strings.ToLower(err)
	return strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "authentication failed")
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

	// Inject the tool name so the pre-exec checker (employee constitution
	// gate) can match against tools_allowed / tools_forbidden entries that
	// use tool names rather than action categories. The PreExecChecker
	// receives this via details["tool_name"].
	details["tool_name"] = toolName

	// Inject agent ID so the PermissionChecker can route to the
	// registered PreExecChecker (employee constitution gate). The agentID
	// is set via the WithExecutorAgentID option or the SetAgentID method.
	e.mu.RLock()
	agentID := e.agentID
	e.mu.RUnlock()
	if agentID != "" {
		details["agent_id"] = agentID
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

// IsReadOnly returns false for mock tools by default.
func (t *MockTool) IsReadOnly(map[string]any) bool { return false }

// IsConcurrencySafe returns false for mock tools by default.
func (t *MockTool) IsConcurrencySafe(map[string]any) bool { return false }

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

// Resize adjusts per-profile concurrency limits in bulk. Only profiles present
// in the limits map are changed; omitted profiles keep their current limits.
// The semaphore channels are rebuilt under lock so that only NEW Acquire calls
// are affected; goroutines already holding slots on old channels are unaffected.
// Values are clamped to [1, maxAdaptiveLimit].
func (l *AdaptiveParallelismLimiter) Resize(limits map[ToolConcurrencyProfile]int) {
	if l == nil || len(limits) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for profile, newLimit := range limits {
		if newLimit < 1 {
			newLimit = 1
		}
		if newLimit > maxAdaptiveLimit {
			newLimit = maxAdaptiveLimit
		}
		l.limits[profile] = newLimit
		l.slots[profile] = make(chan struct{}, newLimit)
	}
}

// FilterToolsForSkill creates a filtered tool registry based on a skill's allowed-tools.
// This is used when executing skills that have restricted tool access.
func FilterToolsForSkill(registry ToolRegistry, allowedTools []string) ToolRegistry {
	if len(allowedTools) == 0 {
		return registry // No filtering needed
	}
	return NewFilteredToolRegistry(registry, allowedTools)
}
