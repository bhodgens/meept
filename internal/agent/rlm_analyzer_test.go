package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// Test fixtures
// -----------------------------------------------------------------------

var _ TraceStoreReader = (*mockTraceStore)(nil)

// mockTraceStore satisfies TraceStoreReader for unit tests.
type mockTraceStore struct {
	mu        sync.RWMutex
	byID      map[string]traceFixture
	byTraceID map[string][]string // traceID -> [spanIDs]
}

type traceFixture struct {
	spans     []traceSpan
	spanIDs   []string
	overLimit bool
}

func newMockTraceStore() *mockTraceStore {
	return &mockTraceStore{
		byID:      make(map[string]traceFixture),
		byTraceID: make(map[string][]string),
	}
}

func (m *mockTraceStore) AddSpan(tf traceFixture) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sid := range tf.spanIDs {
		m.byID[sid] = tf
	}
}

// ListTraceIDs returns all unique trace IDs in the store.
func (m *mockTraceStore) ListTraceIDs() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.byTraceID))
	for id := range m.byTraceID {
		ids = append(ids, id)
	}
	return ids, nil
}

// GetSpansForTrace returns all span IDs for a trace.
func (m *mockTraceStore) GetSpansForTrace(traceID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sids, ok := m.byTraceID[traceID]
	if !ok {
		return nil, fmt.Errorf("trace %s not found", traceID)
	}
	return sids, nil
}

// ListSpans returns spans for a set of span IDs.
func (m *mockTraceStore) ListSpans(spanIDs []string) ([]traceSpan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var results []traceSpan
	for _, sid := range spanIDs {
		fix, ok := m.byID[sid]
		if ok {
			results = append(results, fix.spans...)
		}
	}
	return results, nil
}

// -----------------------------------------------------------------------
// TestRLMAnalyzer_StructuralDepthGating
// -----------------------------------------------------------------------

func TestRLMAnalyzer_StructuralDepthGating(t *testing.T) {
	tests := []struct {
		name         string
		maxDepth     int
		wantSubAgent bool // should subagent tool exist at root (depth 0)
		wantSubAgentAt map[int]bool // depth -> should have subagent tool
	}{
		{
			name:         "depth 1 - root gets subagent, depth 1 does not",
			maxDepth:     1,
			wantSubAgent: true,
			wantSubAgentAt: map[int]bool{0: true, 1: false},
		},
		{
			name:         "depth 2 - root and depth 1 get subagent, depth 2 does not",
			maxDepth:     2,
			wantSubAgent: true,
			wantSubAgentAt: map[int]bool{0: true, 1: true, 2: false},
		},
		{
			name:           "depth 0 - no subagent tool anywhere",
			maxDepth:       0,
			wantSubAgent:   false,
			wantSubAgentAt: map[int]bool{0: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := RLMAnalyzerConfig{
				MaximumDepth:             tt.maxDepth,
				MaximumParallelSubagents: 2,
				MaximumTurns:             10,
			}

			store := newMockTraceStore()
			analyzer := NewRLMAnalyzer(cfg, store, nil, slog.Default())

			// Verify subagent tool availability per depth.
			for depth, want := range tt.wantSubAgentAt {
				has := analyzer.canSpawnSubagentAt(depth)
				if has != want {
					t.Errorf("depth %d: canSpawnSubagent = %v, want %v",
						depth, has, want)
				}
			}

			// Also verify the root match.
			rootHasSpawn := analyzer.canSpawnSubagentAt(0)
			if rootHasSpawn != tt.wantSubAgent {
				t.Errorf("root: canSpawnSubagent = %v, want %v",
					rootHasSpawn, tt.wantSubAgent)
			}

			// Verify _final sentinel tool is always registered.
			hasFinal := analyzer.hasFinalSentinel()
			if !hasFinal {
				t.Error("final sentinel tool should always exist")
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestRLMAnalyzer_PerDepthSemaphoreNoDeadlock
// -----------------------------------------------------------------------

func TestRLMAnalyzer_PerDepthSemaphoreNoDeadlock(t *testing.T) {
	cfg := RLMAnalyzerConfig{
		MaximumDepth:             2,
		MaximumParallelSubagents: 4,
		MaximumTurns:             20,
	}

	store := newMockTraceStore()
	analyzer := NewRLMAnalyzer(cfg, store, nil, slog.Default())

	// Simulate many goroutines trying to spawn at various depths concurrently.
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	spawned := make(chan int, goroutines)
	deadline := time.After(10 * time.Second)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			depth := n % 3 // vary between depths 0, 1, 2
			if !analyzer.canSpawnSubagentAt(depth) {
				return // skip depths where spawn is not allowed
			}
			// Acquire the per-depth semaphore.
			if err := analyzer.semaphore.Acquire(depth); err != nil {
				t.Errorf("depth %d acquire failed: %v", depth, err)
				return
			}
			spawned <- depth
			// Hold briefly, then release.
			time.Sleep(time.Millisecond)
			analyzer.semaphore.Release(depth)
		}(i)
	}

	// Wait for all to finish or timeout.
	go func() {
		wg.Wait()
		close(spawned)
	}()

	count := 0
	for {
		select {
		case _, ok := <-spawned:
			if !ok {
				return
			}
			count++
		case <-deadline:
			t.Fatalf("timeout after %d spawns (expected %d) - potential deadlock",
				count, goroutines)
		}
	}
}

// -----------------------------------------------------------------------
// TestRLMAnalyzer_Analyze - basic smoke test with mock trace store
// -----------------------------------------------------------------------

func TestRLMAnalyzer_Analyze(t *testing.T) {
	cfg := RLMAnalyzerConfig{
		MaximumDepth:             1,
		MaximumParallelSubagents: 2,
		MaximumTurns:             10,
	}

	store := newMockTraceStore()
	buildFixtureTraces(t, store)

	analyzer := NewRLMAnalyzer(cfg, store, nil, slog.Default())
	result, err := analyzer.Analyze(context.Background(), "Find error patterns")
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Without LLM, the deterministic scan should still produce a report.
	if result.Report == "" {
		t.Error("report should not be empty even without LLM")
	}

	// Validate any failure modes that appear.
	for _, fm := range result.FailureModes {
		if fm.ID == "" {
			t.Error("failure mode ID is empty")
		}
		if fm.Description == "" {
			t.Error("failure mode description is empty")
		}
		if fm.Severity != "" && fm.Severity != "critical" && fm.Severity != "high" &&
			fm.Severity != "medium" && fm.Severity != "low" {
			t.Errorf("invalid severity %q", fm.Severity)
		}
		if fm.Category != "" && fm.Category != "hallucination" && fm.Category != "redundant_args" &&
			fm.Category != "refusal_loop" && fm.Category != "semantic" && fm.Category != "tool_error" &&
			fm.Category != "timeout" {
			t.Errorf("invalid category %q", fm.Category)
		}
	}
}

// -----------------------------------------------------------------------
// TestRLMAnalyzer_Analyze_NoTraces - empty store edge case
// -----------------------------------------------------------------------

func TestRLMAnalyzer_Analyze_NoTraces(t *testing.T) {
	cfg := DefaultRLMConfig()
	store := newMockTraceStore()
	analyzer := NewRLMAnalyzer(cfg, store, nil, slog.Default())

	result, err := analyzer.Analyze(context.Background(), "Find errors")
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(result.FailureModes) != 0 {
		t.Errorf("expected 0 failure modes with no traces, got %d", len(result.FailureModes))
	}
	if result.Report != "No traces available to analyze." {
		t.Errorf("unexpected empty-report: %q", result.Report)
	}
}

// -----------------------------------------------------------------------
// TestGetDatasetOverviewTool_Name - verify tool registration name
// -----------------------------------------------------------------------

func TestGetDatasetOverviewTool_Name(t *testing.T) {
	tool := NewGetDatasetOverviewTool(nil)
	if tool.name != "get_dataset_overview" {
		t.Errorf("name: got %q, want %q", tool.name, "get_dataset_overview")
	}
}

// -----------------------------------------------------------------------
// TestFailureMode_ToIssue converts to selfimprove Issue.
// -----------------------------------------------------------------------

func TestFailureMode_ToIssue(t *testing.T) {
	fm := FailureMode{
		ID:          "test-mode-1",
		Description: "repeated tool errors in 3 traces",
		TraceIDs:    []string{"a", "b", "c"},
		Severity:    "high",
		Category:    "tool_error",
	}

	// Severity mapping validation.
	if sev := fm.SeverityAsIssueSeverity(); sev != "high" {
		t.Errorf("Severity: got %q, want %q", sev, "high")
	}

	// Empty severity should map to "low".
	fm2 := FailureMode{ID: "x", Description: "y", Severity: ""}
	if sev := fm2.SeverityAsIssueSeverity(); sev != "low" {
		t.Errorf("empty severity: got %q, want %q", sev, "low")
	}
}

// -----------------------------------------------------------------------
// TestPerDepthSemaphore_Basic
// -----------------------------------------------------------------------

func TestPerDepthSemaphore_Basic(t *testing.T) {
	sem := NewPerDepthSemaphore(map[int]int{
		1: 2,
		2: 1,
	})

	// Depth 1: two concurrent acquires should succeed.
	if err := sem.Acquire(1); err != nil {
		t.Fatalf("acquire depth 1: %v", err)
	}
	if err := sem.Acquire(1); err != nil {
		t.Fatalf("acquire depth 1 (2nd): %v", err)
	}

	// Third acquire at depth 1 should block (we use a goroutine + timeout).
	done := make(chan error, 1)
	go func() {
		done <- sem.Acquire(1)
	}()

	select {
	case err := <-done:
		t.Fatalf("third acquire at depth 1 should block, got: %v", err)
	case <-time.After(100 * time.Millisecond):
		// Good - it blocked.
	}

	// Release one, third should now succeed.
	sem.Release(1)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("third acquire after release: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("third acquire timed out after release")
	}

	// Clean up.
	sem.Release(1)
	sem.Release(1)

	// Test depth isolation: releasing depth 2 should not unblock depth 1.
	sem.Acquire(1) // fill depth 1 again
	sem.Acquire(1) // fill second slot

	blocked := make(chan struct{})
	done3 := make(chan error, 1)
	go func() {
		close(blocked)
		done3 <- sem.Acquire(1)
	}()

	<-blocked
	time.Sleep(50 * time.Millisecond)

	// Release depth 2 - should NOT unblock depth 1.
	sem.Release(2)

	select {
	case err := <-done3:
		t.Fatalf("depth 1 acquire should not be unblocked by releasing depth 2, got: %v", err)
	case <-time.After(100 * time.Millisecond):
		// Good - still blocked.
	}

	// Now release depth 1 - should unblock.
	sem.Release(1)

	select {
	case err := <-done3:
		if err != nil {
			t.Errorf("depth 1 acquire after release: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("depth 1 acquire timed out after release")
	}
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

func buildFixtureTraces(t *testing.T, store *mockTraceStore) {
	t.Helper()

	traces := []struct {
		traceID string
		spans   []traceSpan
	}{
		{
			traceID: "trace-a1",
			spans: []traceSpan{
				{spanID: "span-a1-0", spanName: "root_turn", service: "agent", model: "gpt-4.1-nano",
					inputTokens: 1024, outputTokens: 256},
				{spanID: "span-a1-1", spanName: "subagent_call", service: "agent", model: "gpt-4.1-nano",
					inputTokens: 512, outputTokens: 128},
				{spanID: "span-a1-2", spanName: "llm_call", service: "llm", model: "gpt-4.1-nano",
					inputTokens: 2048, outputTokens: 512},
			},
		},
		{
			traceID: "trace-b2",
			spans: []traceSpan{
				{spanID: "span-b2-0", spanName: "root_turn", service: "agent", model: "claude-sonnet-4-5",
					inputTokens: 4096, outputTokens: 64, hasError: true},
				{spanID: "span-b2-1", spanName: "llm_call", service: "llm", model: "claude-sonnet-4-5",
					inputTokens: 8192, outputTokens: 32, hasError: true},
			},
		},
		{
			traceID: "trace-c3",
			spans: []traceSpan{
				{spanID: "span-c3-0", spanName: "refusal_loop", service: "agent", model: "gpt-4.1-nano",
					inputTokens: 512, outputTokens: 4, hasError: true},
			},
		},
	}

	for _, tc := range traces {
		fixture := traceFixture{
			spans:     tc.spans,
			spanIDs:   make([]string, len(tc.spans)),
			overLimit: false,
		}
		for i, s := range tc.spans {
			fixture.spanIDs[i] = s.spanID
			//lint:ignore SA9005 traceSpan has unexported fields for test fixture
			b, _ := json.Marshal(s)
			s.rawJSON = b
		}
		store.AddSpan(fixture)
		// Register traceID -> spanIDs mapping.
		store.byTraceID[tc.traceID] = fixture.spanIDs
	}
}

//nolint:U1000 // test helper reserved for future tests
//lint:ignore U1000 test helper reserved for future tests
func slogLogger(t *testing.T) *slog.Logger {
	return slog.New(&testHandler{t: t})
}

type testHandler struct {
	t *testing.T
}

func (h *testHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *testHandler) Handle(_ context.Context, r slog.Record) error {
	msg := r.Message
	switch r.Level {
	case slog.LevelError:
		h.t.Errorf("ERROR: %s", msg)
	case slog.LevelWarn:
		h.t.Logf("WARN: %s", msg)
	default:
		h.t.Logf("INFO: %s", msg)
	}
	return nil
}
func (h *testHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *testHandler) WithGroup(name string) slog.Handler       { return h }

// Ensure handler satisfies slog.Handler interface.
var _ slog.Handler = (*testHandler)(nil)
