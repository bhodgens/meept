package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/taint"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// ---------------------------------------------------------------------------
// Phase A — Tool execution retry tests
// ---------------------------------------------------------------------------

// retryableTestError is a test error that satisfies the RetryableError interface.
type retryableTestError struct {
	msg string
}

func (e *retryableTestError) Error() string            { return e.msg }
func (e *retryableTestError) RetryAfter() time.Duration { return 0 }
func (e *retryableTestError) IsRetryable() bool         { return true }

func TestExecute_RetriesOnRetryableError(t *testing.T) {
	var attempts int32
	registry := NewPlaceholderToolRegistry()
	// Use "shell" tool which has shorter backoff delays (BaseDelay=1s, MaxAttempts=2).
	// We need 3 attempts (1 initial + 2 retries), which matches MaxAttempts=2.
	registry.Register(NewMockTool("shell", "shell", func(ctx context.Context, args map[string]any) (any, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return nil, &retryableTestError{msg: "connection reset"}
		}
		return "success", nil
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker, WithParallelism(1))

	result := executor.Execute(context.Background(), makeToolCall("call_1", "shell", `{"command":"echo hi"}`))

	if !result.Success {
		t.Fatalf("expected success after retries, got error: %s", result.Error)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestExecute_DoesNotRetryNonRetryableError(t *testing.T) {
	var attempts int32
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("web_search", "search", func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, errors.New("permanent failure: invalid argument")
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker, WithParallelism(1))

	result := executor.Execute(context.Background(), makeToolCall("call_1", "web_search", `{"query":"test"}`))

	if result.Success {
		t.Fatal("expected failure")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("expected 1 attempt (no retry), got %d", got)
	}
}

func TestExecute_DoesNotRetryPermissionDenied(t *testing.T) {
	var attempts int32
	registry := NewPlaceholderToolRegistry()
	// Register a tool with a name NOT in the safe-tools list so it gets blocked.
	registry.Register(NewMockTool("dangerous_tool", "dangerous", func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&attempts, 1)
		return "should not reach", nil
	}))

	// Nil security: fail-closed blocks everything except safe tools.
	executor := NewExecutor(registry, nil, WithParallelism(1))

	result := executor.Execute(context.Background(), makeToolCall("call_1", "dangerous_tool", `{}`))

	if result.Success {
		t.Fatal("expected failure (permission denied)")
	}
	if got := atomic.LoadInt32(&attempts); got != 0 {
		t.Errorf("expected 0 tool executions (blocked before execution), got %d", got)
	}
}

func TestExecute_DoesNotRetryCacheHit(t *testing.T) {
	var attempts int32
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("memory_search", "search", func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&attempts, 1)
		return "result", nil
	}))

	// nil security: memory_search is in the safe-tools allowlist.
	cache := NewResultCache(CacheConfig{MaxEntries: 10, DefaultTTL: time.Minute}, nil)
	executor := NewExecutor(registry, nil, WithParallelism(1), WithExecutorCache(cache))

	// First call populates cache.
	r1 := executor.Execute(context.Background(), makeToolCall("call_1", "memory_search", `{"query":"test"}`))
	if !r1.Success {
		t.Fatalf("first call failed: %s", r1.Error)
	}

	firstAttempts := atomic.LoadInt32(&attempts)

	// Second call should hit cache, not execute the tool.
	r2 := executor.Execute(context.Background(), makeToolCall("call_2", "memory_search", `{"query":"test"}`))
	if !r2.Success {
		t.Fatalf("second call failed: %s", r2.Error)
	}
	if !r2.Cached {
		t.Error("expected second call to be a cache hit")
	}

	secondAttempts := atomic.LoadInt32(&attempts)
	if secondAttempts != firstAttempts {
		t.Errorf("expected no additional tool execution for cache hit, first=%d second=%d", firstAttempts, secondAttempts)
	}
}

func TestExecute_RetriesExhausted(t *testing.T) {
	var attempts int32
	registry := NewPlaceholderToolRegistry()
	// Use "shell" tool which has MaxAttempts=2 (shorter delays for test speed).
	registry.Register(NewMockTool("shell", "shell", func(ctx context.Context, args map[string]any) (any, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, &retryableTestError{msg: "connection refused"}
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker, WithParallelism(1))

	result := executor.Execute(context.Background(), makeToolCall("call_1", "shell", `{"command":"echo hi"}`))

	if result.Success {
		t.Fatal("expected failure (retries exhausted)")
	}
	// shell retry config: MaxAttempts=2, meaning 2 retry sleeps.
	// Total tool executions = 1 initial + 2 retries = 3.
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 attempts (1 initial + 2 retries), got %d", got)
	}
	if !strings.Contains(result.Error, "connection refused") {
		t.Errorf("expected error to contain 'connection refused', got: %s", result.Error)
	}
}

func TestGetRetryConfigForTool(t *testing.T) {
	executor := &Executor{}

	tests := []struct {
		name            string
		toolName        string
		wantBaseDelay   time.Duration
		wantMaxDelay    time.Duration
		wantMultiplier  float64
		wantMaxAttempts int
	}{
		{
			name:            "web_search",
			toolName:        "web_search",
			wantBaseDelay:   2 * time.Second,
			wantMaxDelay:    30 * time.Second,
			wantMultiplier:  2.0,
			wantMaxAttempts: 5,
		},
		{
			name:            "web_fetch",
			toolName:        "web_fetch",
			wantBaseDelay:   2 * time.Second,
			wantMaxDelay:    30 * time.Second,
			wantMultiplier:  2.0,
			wantMaxAttempts: 5,
		},
		{
			name:            "shell",
			toolName:        "shell",
			wantBaseDelay:   1 * time.Second,
			wantMaxDelay:    10 * time.Second,
			wantMultiplier:  1.5,
			wantMaxAttempts: 2,
		},
		{
			name:            "unknown_tool",
			toolName:        "some_other_tool",
			wantBaseDelay:   1 * time.Second,
			wantMaxDelay:    30 * time.Second,
			wantMultiplier:  2.0,
			wantMaxAttempts: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := executor.getRetryConfigForTool(tt.toolName)
			if cfg.BaseDelay != tt.wantBaseDelay {
				t.Errorf("BaseDelay = %v, want %v", cfg.BaseDelay, tt.wantBaseDelay)
			}
			if cfg.MaxDelay != tt.wantMaxDelay {
				t.Errorf("MaxDelay = %v, want %v", cfg.MaxDelay, tt.wantMaxDelay)
			}
			if cfg.Multiplier != tt.wantMultiplier {
				t.Errorf("Multiplier = %v, want %v", cfg.Multiplier, tt.wantMultiplier)
			}
			if cfg.MaxAttempts != tt.wantMaxAttempts {
				t.Errorf("MaxAttempts = %v, want %v", cfg.MaxAttempts, tt.wantMaxAttempts)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Phase B — Error aggregation tests
// ---------------------------------------------------------------------------

func TestDetectCascadingErrors(t *testing.T) {
	graph := NewToolDependencyGraph()
	graph.AddCall(makeToolCall("root", "file_read", `{}`))
	graph.AddCall(makeToolCall("dep1", "file_read", `{}`))
	graph.AddDependency("dep1", "root")

	results := []*ExecutionResult{
		{ToolCallID: "root", Success: false, Error: "permission denied"},
		{ToolCallID: "dep1", Success: false, Error: "skipped"},
	}

	detectCascadingErrors(results, graph)

	if results[0].IsCascading {
		t.Error("root failure should not be cascading")
	}
	if !results[1].IsCascading {
		t.Error("dep1 failure should be cascading (depends on root)")
	}
	if results[1].CascadeFrom != "root" {
		t.Errorf("CascadeFrom = %q, want 'root'", results[1].CascadeFrom)
	}
}

func TestDetectCascadingErrors_NilGraph(t *testing.T) {
	results := []*ExecutionResult{
		{ToolCallID: "a", Success: false, Error: "err"},
	}
	got := detectCascadingErrors(results, nil)
	if len(got) != 1 {
		t.Errorf("expected 1 result, got %d", len(got))
	}
	if got[0].IsCascading {
		t.Error("should not be cascading with nil graph")
	}
}

func TestDetectCascadingErrors_IndependentFailures(t *testing.T) {
	graph := NewToolDependencyGraph()
	graph.AddCall(makeToolCall("a", "file_read", `{}`))
	graph.AddCall(makeToolCall("b", "web_search", `{}`))

	results := []*ExecutionResult{
		{ToolCallID: "a", Success: false, Error: "err a"},
		{ToolCallID: "b", Success: false, Error: "err b"},
	}

	detectCascadingErrors(results, graph)

	if results[0].IsCascading {
		t.Error("independent failure 'a' should not be cascading")
	}
	if results[1].IsCascading {
		t.Error("independent failure 'b' should not be cascading")
	}
}

func TestAggregateErrors(t *testing.T) {
	results := []*ExecutionResult{
		{ToolCallID: "root", Success: false, Error: "permission denied"},
		{ToolCallID: "dep", Success: false, Error: "skipped", IsCascading: true, CascadeFrom: "root"},
		{ToolCallID: "ok", Success: true},
	}

	errs := AggregateErrors(results)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errs))
	}

	// Find root and dep errors.
	var rootErr, depErr *ParallelExecutionError
	for _, e := range errs {
		switch e.ToolCallID {
		case "root":
			rootErr = e
		case "dep":
			depErr = e
		}
	}

	if rootErr == nil || depErr == nil {
		t.Fatal("expected root and dep errors")
	}

	if rootErr.Severity != ErrorSeverityCritical {
		t.Errorf("root severity = %q, want %q", rootErr.Severity, ErrorSeverityCritical)
	}
	if depErr.Severity != ErrorSeverityWarning {
		t.Errorf("dep severity = %q, want %q", depErr.Severity, ErrorSeverityWarning)
	}
	if depErr.CascadeSource != "root" {
		t.Errorf("dep CascadeSource = %q, want 'root'", depErr.CascadeSource)
	}
	if len(rootErr.RelatedErrors) != 1 || rootErr.RelatedErrors[0] != "dep" {
		t.Errorf("root RelatedErrors = %v, want ['dep']", rootErr.RelatedErrors)
	}
}

func TestAggregateErrors_NoErrors(t *testing.T) {
	results := []*ExecutionResult{
		{ToolCallID: "a", Success: true},
		{ToolCallID: "b", Success: true},
	}
	errs := AggregateErrors(results)
	if errs != nil {
		t.Errorf("expected nil for all-success, got %d errors", len(errs))
	}
}

func TestParallelExecutionError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *ParallelExecutionError
		want string
	}{
		{
			name: "non-cascading",
			err:  &ParallelExecutionError{ToolCallID: "tc1", Message: "fail"},
			want: "tool tc1 failed: fail",
		},
		{
			name: "cascading",
			err:  &ParallelExecutionError{ToolCallID: "tc2", Message: "skip", CascadeSource: "tc1"},
			want: "tool tc2 failed (cascading from tc1): skip",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Phase C — Taint tracking in ExecuteAll test
// ---------------------------------------------------------------------------

// capturingHandler is a slog.Handler that captures log records for testing.
type capturingHandler struct {
	mu      sync.Mutex
	records []capturedRecord
}

type capturedRecord struct {
	level   slog.Level
	message string
}

func (h *capturingHandler) Enabled(_ context.Context, level slog.Level) bool {
	_ = level
	return true
}

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, capturedRecord{
		level:   r.Level,
		message: r.Message,
	})
	h.mu.Unlock()
	return nil
}

func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *capturingHandler) WithGroup(_ string) slog.Handler {
	return h
}

func TestExecuteAll_TaintTracking(t *testing.T) {
	// Build tools that return tainted ToolResults.
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("memory_search", "search", func(ctx context.Context, args map[string]any) (any, error) {
		return &tools.ToolResult{
			Success:    true,
			Result:     "found",
			TaintLabel: taint.TaintExternal,
		}, nil
	}))
	registry.Register(NewMockTool("memory_get_context", "get context", func(ctx context.Context, args map[string]any) (any, error) {
		return &tools.ToolResult{
			Success:    true,
			Result:     "ctx",
			TaintLabel: taint.TaintUntrusted,
		}, nil
	}))

	handler := &capturingHandler{}
	logger := slog.New(handler)

	// nil security: both memory_search and memory_get_context are in safe-tools.
	executor := NewExecutor(registry, nil, WithParallelism(2), WithExecutorLogger(logger))

	toolCalls := []llm.ToolCall{
		makeToolCall("taint_a", "memory_search", `{"query":"x"}`),
		makeToolCall("taint_b", "memory_get_context", `{}`),
	}

	results := executor.ExecuteAll(context.Background(), toolCalls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r == nil || !r.Success {
			t.Fatalf("result[%d] failed", i)
		}
	}

	// Verify a warn-level log about combined taint was emitted.
	handler.mu.Lock()
	records := make([]capturedRecord, len(handler.records))
	copy(records, handler.records)
	handler.mu.Unlock()

	found := false
	for _, rec := range records {
		if rec.level == slog.LevelWarn && strings.Contains(rec.message, "combined taint") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a warn-level log about combined taint, but none was emitted")
	}
}
