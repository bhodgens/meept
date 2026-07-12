package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// ---------------------------------------------------------------------------
// ExecuteAll dependency-aware execution tests
// ---------------------------------------------------------------------------

// newTestExecutor builds an executor with a placeholder registry and a set of
// tools, using a permissive security config so tools can actually execute.
func newTestExecutor() *Executor {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "read", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"content": "data"}, nil
	}))
	registry.Register(NewMockTool("file_write", "write", func(ctx context.Context, args map[string]any) (any, error) {
		return "written", nil
	}))
	registry.Register(NewMockTool("web_search", "search", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"results": []string{"r1"}}, nil
	}))
	registry.Register(NewMockTool("memory_search", "search memory", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"hits": 0}, nil
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	return NewExecutor(registry, secChecker, WithParallelism(4))
}

func TestExecuteAll_PreservesIndexOrder_IndependentCalls(t *testing.T) {
	executor := newTestExecutor()

	toolCalls := []llm.ToolCall{
		makeToolCall("call_A", "file_read", `{"file":"/a"}`),
		makeToolCall("call_B", "web_search", `{"query":"test"}`),
		makeToolCall("call_C", "memory_search", `{}`),
	}

	results := executor.ExecuteAll(context.Background(), toolCalls)

	if len(results) != len(toolCalls) {
		t.Fatalf("expected %d results, got %d", len(toolCalls), len(results))
	}

	for i, tc := range toolCalls {
		if results[i] == nil {
			t.Fatalf("result[%d] is nil", i)
		}
		if results[i].ToolCallID != tc.ID {
			t.Errorf("result[%d].ToolCallID = %q, want %q", i, results[i].ToolCallID, tc.ID)
		}
		if !results[i].Success {
			t.Errorf("result[%d] unexpectedly failed: %s", i, results[i].Error)
		}
	}
}

func TestExecuteAll_PreservesIndexOrder_DependentCalls(t *testing.T) {
	// Calls: [A_write(/x), B_read(/x), C_independent]
	// The inferrer should detect that B depends on A (write -> read same path).
	// C is independent.
	// Verify all 3 results present and correctly mapped to original indices.
	executor := newTestExecutor()

	toolCalls := []llm.ToolCall{
		makeToolCall("call_write", "file_write", `{"file":"/shared/x"}`),
		makeToolCall("call_read", "file_read", `{"file":"/shared/x"}`),
		makeToolCall("call_indep", "web_search", `{"query":"unrelated"}`),
	}

	results := executor.ExecuteAll(context.Background(), toolCalls)

	if len(results) != len(toolCalls) {
		t.Fatalf("expected %d results, got %d", len(toolCalls), len(results))
	}

	for i, tc := range toolCalls {
		if results[i] == nil {
			t.Fatalf("result[%d] is nil", i)
		}
		if results[i].ToolCallID != tc.ID {
			t.Errorf("result[%d].ToolCallID = %q, want %q", i, results[i].ToolCallID, tc.ID)
		}
		if !results[i].Success {
			t.Errorf("result[%d] unexpectedly failed: %s", i, results[i].Error)
		}
	}
}

func TestExecuteAll_EarlyTermination_OnPermissionDenied(t *testing.T) {
	// We need a tool that fails with "permission denied" to trigger early
	// termination. The security checker naturally produces this when it
	// blocks an unknown tool. We verify that a dependent tool in a later
	// group gets skipped.
	registry := NewPlaceholderToolRegistry()

	var mu sync.Mutex
	downstreamCalled := false

	// "failing_tool" — the security checker blocks it with
	// "permission denied: <reason>" since it's not a known safe tool.
	registry.Register(NewMockTool("failing_tool", "blocked by security", func(ctx context.Context, args map[string]any) (any, error) {
		return nil, errors.New("permission denied: access restricted")
	}))

	// "downstream_tool" should be skipped due to early termination.
	registry.Register(NewMockTool("downstream_tool", "should be skipped", func(ctx context.Context, args map[string]any) (any, error) {
		mu.Lock()
		downstreamCalled = true
		mu.Unlock()
		return "should not reach", nil
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker, WithParallelism(2))

	// Inject an inferrer that creates a dependency from downstream to fail
	// so they land in separate groups (fail in group 0, downstream in group 1).
	executor.depInferrer = newTestInferrer()

	// Explicit argument reference forces dependency: downstream depends on fail.
	toolCalls := []llm.ToolCall{
		makeToolCall("call_fail", "failing_tool", `{}`),
		makeToolCall("call_downstream", "downstream_tool", `{"ref":"$call_fail.result"}`),
	}

	results := executor.ExecuteAll(context.Background(), toolCalls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result (failing_tool) should have failed with permission denied.
	if results[0] == nil {
		t.Fatal("result[0] is nil")
	}
	if results[0].ToolCallID != "call_fail" {
		t.Errorf("result[0].ToolCallID = %q, want call_fail", results[0].ToolCallID)
	}
	if results[0].Success {
		t.Error("result[0] should have failed")
	}
	if !strings.Contains(results[0].Error, "permission denied") {
		t.Errorf("result[0].Error = %q, want it to contain 'permission denied'", results[0].Error)
	}

	// Second result (downstream_tool) should be skipped.
	if results[1] == nil {
		t.Fatal("result[1] is nil")
	}
	if results[1].ToolCallID != "call_downstream" {
		t.Errorf("result[1].ToolCallID = %q, want call_downstream", results[1].ToolCallID)
	}
	if results[1].Success {
		t.Error("result[1] should be skipped (not successful)")
	}
	if !strings.Contains(results[1].Error, "skipped") {
		t.Errorf("result[1].Error = %q, want it to contain 'skipped'", results[1].Error)
	}

	// downstream_tool's executeFunc should NOT have been called.
	mu.Lock()
	defer mu.Unlock()
	if downstreamCalled {
		t.Error("downstream_tool executeFunc should not have been called")
	}
}

func TestExecuteAll_SingleCall(t *testing.T) {
	executor := newTestExecutor()

	toolCalls := []llm.ToolCall{
		makeToolCall("call_solo", "file_read", `{"file":"/a"}`),
	}

	results := executor.ExecuteAll(context.Background(), toolCalls)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0] == nil {
		t.Fatal("result[0] is nil")
	}
	if results[0].ToolCallID != "call_solo" {
		t.Errorf("result[0].ToolCallID = %q, want call_solo", results[0].ToolCallID)
	}
	if !results[0].Success {
		t.Errorf("result[0] failed: %s", results[0].Error)
	}
}

func TestExecuteAll_Empty(t *testing.T) {
	executor := newTestExecutor()

	results := executor.ExecuteAll(context.Background(), nil)

	if results != nil {
		t.Errorf("expected nil for empty input, got %d results", len(results))
	}
}

func TestExecuteAll_FallsBackWhenNoInferrer(t *testing.T) {
	// Construct executor with explicit nil inferrer (the default).
	// It should lazily init from registry and still work.
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "read", func(ctx context.Context, args map[string]any) (any, error) {
		return "ok", nil
	}))
	registry.Register(NewMockTool("web_search", "search", func(ctx context.Context, args map[string]any) (any, error) {
		return "found", nil
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker, WithParallelism(2))

	// Explicitly verify depInferrer is nil before call.
	if executor.depInferrer != nil {
		t.Fatal("expected depInferrer to be nil before first ExecuteAll call")
	}

	toolCalls := []llm.ToolCall{
		makeToolCall("c1", "file_read", `{"file":"/a"}`),
		makeToolCall("c2", "web_search", `{"query":"q"}`),
	}

	results := executor.ExecuteAll(context.Background(), toolCalls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, tc := range toolCalls {
		if results[i] == nil {
			t.Fatalf("result[%d] is nil", i)
		}
		if results[i].ToolCallID != tc.ID {
			t.Errorf("result[%d].ToolCallID = %q, want %q", i, results[i].ToolCallID, tc.ID)
		}
		if !results[i].Success {
			t.Errorf("result[%d] failed: %s", i, results[i].Error)
		}
	}

	// After the call, lazy init should have populated the inferrer.
	if executor.depInferrer == nil {
		t.Error("expected depInferrer to be lazily initialized after ExecuteAll")
	}
}

// ---------------------------------------------------------------------------
// isCriticalError / shouldTerminateEarly unit tests
// ---------------------------------------------------------------------------

func TestIsCriticalError(t *testing.T) {
	executor := &Executor{}

	tests := []struct {
		err  string
		want bool
	}{
		{"permission denied: access restricted", true},
		{"authentication failed: invalid token", true},
		{"some other error", false},
		{"", false},
		{"tool execution failed: network timeout", false},
	}

	for _, tt := range tests {
		got := executor.isCriticalError(tt.err)
		if got != tt.want {
			t.Errorf("isCriticalError(%q) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestShouldTerminateEarly(t *testing.T) {
	executor := &Executor{}

	tests := []struct {
		name    string
		results []*ExecutionResult
		want    bool
	}{
		{
			name: "all success",
			results: []*ExecutionResult{
				{Success: true},
				{Success: true},
			},
			want: false,
		},
		{
			name: "non-critical failure",
			results: []*ExecutionResult{
				{Success: false, Error: "network timeout"},
			},
			want: false,
		},
		{
			name: "permission denied",
			results: []*ExecutionResult{
				{Success: true},
				{Success: false, Error: "permission denied: no access"},
			},
			want: true,
		},
		{
			name: "authentication failed",
			results: []*ExecutionResult{
				{Success: false, Error: "authentication failed: bad key"},
			},
			want: true,
		},
		{
			name:    "nil results",
			results: nil,
			want:    false,
		},
		{
			name: "nil entry in results",
			results: []*ExecutionResult{
				nil,
				{Success: true},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.shouldTerminateEarly(tt.results)
			if got != tt.want {
				t.Errorf("shouldTerminateEarly() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WithExecutorInferrer option test
// ---------------------------------------------------------------------------

func TestWithExecutorInferrer(t *testing.T) {
	customInferrer := NewDependencyInferrer(NewPlaceholderToolRegistry(), slog.Default())

	// Non-nil should be set.
	executor := NewExecutor(nil, nil, WithExecutorInferrer(customInferrer))
	if executor.depInferrer == nil {
		t.Error("expected depInferrer to be set via WithExecutorInferrer")
	}

	// Nil should NOT be set (nil guard).
	executor2 := NewExecutor(nil, nil, WithExecutorInferrer(nil))
	if executor2.depInferrer != nil {
		t.Error("expected depInferrer to remain nil when passed nil")
	}
}

// ---------------------------------------------------------------------------
// inferDependencies lazy init test
// ---------------------------------------------------------------------------

func TestInferDependencies_LazyInit(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	executor := NewExecutor(registry, nil)
	// depInferrer is nil initially.

	graph := executor.inferDependencies([]llm.ToolCall{
		makeToolCall("a", "file_read", `{}`),
	})

	// Graph should not be nil and should contain the call.
	if graph == nil {
		t.Fatal("expected non-nil graph")
	}
	// After lazy init, depInferrer should be populated.
	if executor.depInferrer == nil {
		t.Error("expected depInferrer to be lazily initialized")
	}
	// The node should be in the graph with no dependencies.
	deps := graph.GetDependencies("a")
	if len(deps) != 0 {
		t.Errorf("expected call 'a' to have 0 dependencies, got %v", deps)
	}
}

func TestInferDependencies_NilRegistry_ReturnsEmptyGraph(t *testing.T) {
	// When registry is nil and inferrer is nil, should return empty graph
	// (no panic).
	executor := NewExecutor(nil, nil)

	graph := executor.inferDependencies([]llm.ToolCall{
		makeToolCall("a", "file_read", `{}`),
		makeToolCall("b", "web_search", `{}`),
	})

	if graph == nil {
		t.Fatal("expected non-nil graph")
	}
	groups := graph.IndependentGroups()
	// Empty graph has no nodes, so IndependentGroups returns empty.
	if len(groups) != 0 {
		t.Errorf("expected 0 groups from empty graph, got %d", len(groups))
	}
}
