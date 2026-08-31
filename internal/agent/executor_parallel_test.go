package agent

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// ---------------------------------------------------------------------------
// AdaptiveParallelismLimiter unit tests
// ---------------------------------------------------------------------------

func TestAdaptiveParallelismLimiter_AcquireRelease(t *testing.T) {
	limiter := NewAdaptiveParallelismLimiter(4)

	// IO-bound profile should have 4 slots; acquire one and release.
	if err := limiter.Acquire(context.Background(), ProfileIOBound); err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	limiter.Release(ProfileIOBound)

	// Should be able to acquire again after release.
	if err := limiter.Acquire(context.Background(), ProfileIOBound); err != nil {
		t.Fatalf("Acquire after Release returned error: %v", err)
	}
	limiter.Release(ProfileIOBound)
}

func TestAdaptiveParallelismLimiter_Limits(t *testing.T) {
	limiter := NewAdaptiveParallelismLimiter(4)

	if got := limiter.Limit(ProfileIOBound); got != 4 {
		t.Errorf("ProfileIOBound limit = %d, want 4", got)
	}
	if got := limiter.Limit(ProfileCPUBound); got != 2 {
		t.Errorf("ProfileCPUBound limit = %d, want 2", got)
	}
	if got := limiter.Limit(ProfileStateful); got != 1 {
		t.Errorf("ProfileStateful limit = %d, want 1", got)
	}
	if got := limiter.Limit(ProfileExclusive); got != 1 {
		t.Errorf("ProfileExclusive limit = %d, want 1", got)
	}
}

func TestAdaptiveParallelismLimiter_ExclusiveIsMutex(t *testing.T) {
	limiter := NewAdaptiveParallelismLimiter(4)

	ctx := context.Background()

	// Acquire the single exclusive slot.
	if err := limiter.Acquire(ctx, ProfileExclusive); err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}

	// A second Acquire must block. Use a goroutine + done channel.
	acquired := make(chan error, 1)
	go func() {
		acquired <- limiter.Acquire(ctx, ProfileExclusive)
	}()

	select {
	case err := <-acquired:
		t.Fatalf("second Acquire should have blocked but returned: %v", err)
	case <-time.After(50 * time.Millisecond):
		// Expected: still blocked.
	}

	// Release the first; the second goroutine should now complete.
	limiter.Release(ProfileExclusive)

	select {
	case err := <-acquired:
		if err != nil {
			t.Errorf("second Acquire returned error after Release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second Acquire did not complete after Release (deadlock?)")
	}

	limiter.Release(ProfileExclusive)
}

func TestAdaptiveParallelismLimiter_ProfileForTool(t *testing.T) {
	limiter := NewAdaptiveParallelismLimiter(4)

	tests := []struct {
		toolName string
		want     ToolConcurrencyProfile
	}{
		{"web_search", ProfileIOBound},
		{"web_fetch", ProfileIOBound},
		{"file_read", ProfileIOBound},
		{"file_write", ProfileIOBound},
		{"file_edit", ProfileIOBound},
		{"memory_search", ProfileIOBound},
		{"ast_parse", ProfileCPUBound},
		{"ast_query", ProfileCPUBound},
		{"shell", ProfileStateful},
		{"bash", ProfileStateful},
		{"git_diff", ProfileExclusive},
		{"git_commit", ProfileExclusive},
		{"unknown_tool_xyz", ProfileIOBound}, // default
		{"", ProfileIOBound},                 // empty string default
	}

	for _, tt := range tests {
		got := limiter.ProfileForTool(tt.toolName)
		if got != tt.want {
			t.Errorf("ProfileForTool(%q) = %s, want %s", tt.toolName, got, tt.want)
		}
	}
}

func TestAdaptiveParallelismLimiter_ContextCancellation(t *testing.T) {
	limiter := NewAdaptiveParallelismLimiter(4)

	// Fill the exclusive slot.
	if err := limiter.Acquire(context.Background(), ProfileExclusive); err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	defer limiter.Release(ProfileExclusive)

	// Now try to acquire with a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := limiter.Acquire(ctx, ProfileExclusive)
	if err == nil {
		t.Fatal("expected error from Acquire with cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestAdaptiveParallelismLimiter_CPUBoundConcurrency(t *testing.T) {
	// With baseParallelism=4, CPUBound gets 2 slots.
	limiter := NewAdaptiveParallelismLimiter(4)

	if err := limiter.Acquire(context.Background(), ProfileCPUBound); err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	if err := limiter.Acquire(context.Background(), ProfileCPUBound); err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}

	// Third should block (limit is 2).
	acquired := make(chan error, 1)
	go func() {
		acquired <- limiter.Acquire(context.Background(), ProfileCPUBound)
	}()

	select {
	case err := <-acquired:
		t.Fatalf("third Acquire should have blocked but returned: %v", err)
	case <-time.After(50 * time.Millisecond):
		// Expected: still blocked.
	}

	limiter.Release(ProfileCPUBound)

	select {
	case err := <-acquired:
		if err != nil {
			t.Errorf("third Acquire returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("third Acquire did not complete after Release")
	}

	limiter.Release(ProfileCPUBound)
}

// ---------------------------------------------------------------------------
// toolProfiles map tests
// ---------------------------------------------------------------------------

func TestToolProfiles_ContainsCommonTools(t *testing.T) {
	expected := map[string]ToolConcurrencyProfile{
		"web_search": ProfileIOBound,
		"file_read":  ProfileIOBound,
		"shell":      ProfileStateful,
		"git_commit": ProfileExclusive,
	}

	for tool, wantProfile := range expected {
		gotProfile, ok := toolProfiles[tool]
		if !ok {
			t.Errorf("toolProfiles missing entry for %q", tool)
			continue
		}
		if gotProfile != wantProfile {
			t.Errorf("toolProfiles[%q] = %s, want %s", tool, gotProfile, wantProfile)
		}
	}
}

// ---------------------------------------------------------------------------
// Profile String() method test
// ---------------------------------------------------------------------------

func TestToolConcurrencyProfile_String(t *testing.T) {
	tests := []struct {
		profile ToolConcurrencyProfile
		want    string
	}{
		{ProfileIOBound, "io_bound"},
		{ProfileCPUBound, "cpu_bound"},
		{ProfileStateful, "stateful"},
		{ProfileExclusive, "exclusive"},
	}

	for _, tt := range tests {
		if got := tt.profile.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.profile, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ExecuteAll behavioral test: exclusive profile enforcement
// ---------------------------------------------------------------------------

func TestExecuteAll_RespectsExclusiveProfile(t *testing.T) {
	// We test that the adaptive limiter serializes calls for tools whose profile
	// is ProfileExclusive (limit=1). We use "memory_search" as the tool name
	// because it passes the security checker, but temporarily override its
	// profile to ProfileExclusive so the limiter treats it as exclusive.

	// Override profile for the test tool.
	originalProfile := toolProfiles["memory_search"]
	toolProfiles["memory_search"] = ProfileExclusive
	defer func() { toolProfiles["memory_search"] = originalProfile }()

	var currentConcurrent int64
	var maxConcurrent int64

	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("memory_search", "memory search", func(ctx context.Context, args map[string]any) (any, error) {
		cur := atomic.AddInt64(&currentConcurrent, 1)
		// Track max concurrency.
		for {
			max := atomic.LoadInt64(&maxConcurrent)
			if cur <= max || atomic.CompareAndSwapInt64(&maxConcurrent, max, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond) // Hold the slot briefly to force overlap if not serialized.
		atomic.AddInt64(&currentConcurrent, -1)
		return "found", nil
	}))

	// nil security: fail-closed allows memory_search.
	executor := NewExecutor(registry, nil, WithParallelism(4))

	// Two memory_search calls — dependency inferrer treats them as independent
	// (no shared file path), so they land in the same parallel group.
	// The adaptive limiter must serialize them via ProfileExclusive.
	toolCalls := []llm.ToolCall{
		makeToolCall("search_1", "memory_search", `{"query":"first"}`),
		makeToolCall("search_2", "memory_search", `{"query":"second"}`),
	}

	results := executor.ExecuteAll(context.Background(), toolCalls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r == nil {
			t.Fatalf("result[%d] is nil", i)
		}
		if !r.Success {
			t.Errorf("result[%d] failed: %s", i, r.Error)
		}
	}

	if got := atomic.LoadInt64(&maxConcurrent); got != 1 {
		t.Errorf("max concurrent exclusive calls = %d, want 1 (ProfileExclusive)", got)
	}
}

// ---------------------------------------------------------------------------
// WithExecutorParallelismLimiter option test
// ---------------------------------------------------------------------------

func TestWithExecutorParallelismLimiter(t *testing.T) {
	customLimiter := NewAdaptiveParallelismLimiter(8)

	// Non-nil should be set.
	executor := NewExecutor(nil, nil, WithExecutorParallelismLimiter(customLimiter))
	if executor.parallelismLimiter != customLimiter {
		t.Error("expected parallelismLimiter to be set via WithExecutorParallelismLimiter")
	}

	// Nil should NOT override the default.
	executor2 := NewExecutor(nil, nil, WithExecutorParallelismLimiter(nil))
	if executor2.parallelismLimiter == nil {
		t.Error("expected default parallelismLimiter to remain when nil passed")
	}
}

// ---------------------------------------------------------------------------
// AdjustLimits (runtime-adaptive parallelism tuning) tests
// ---------------------------------------------------------------------------

func TestAdaptiveParallelismLimiter_AdjustLimits(t *testing.T) {
	t.Run("high error rate reduces io_bound and cpu_bound", func(t *testing.T) {
		limiter := NewAdaptiveParallelismLimiter(8) // io=8, cpu=4

		limiter.AdjustLimits(ExecutionMetrics{
			ErrorRate: 0.6,
		})

		if got := limiter.Limit(ProfileIOBound); got != 7 {
			t.Errorf("io_bound limit after high-error AdjustLimits = %d, want 7", got)
		}
		if got := limiter.Limit(ProfileCPUBound); got != 3 {
			t.Errorf("cpu_bound limit after high-error AdjustLimits = %d, want 3", got)
		}
		// Stateful and exclusive are never adjusted.
		if got := limiter.Limit(ProfileStateful); got != 1 {
			t.Errorf("stateful limit = %d, want 1 (never adjusted)", got)
		}
		if got := limiter.Limit(ProfileExclusive); got != 1 {
			t.Errorf("exclusive limit = %d, want 1 (never adjusted)", got)
		}
	})

	t.Run("low error rate and low latency increases io_bound", func(t *testing.T) {
		limiter := NewAdaptiveParallelismLimiter(4) // io=4

		limiter.AdjustLimits(ExecutionMetrics{
			ErrorRate:  0.01,
			AvgLatency: 100 * time.Millisecond,
		})

		if got := limiter.Limit(ProfileIOBound); got != 5 {
			t.Errorf("io_bound limit after healthy AdjustLimits = %d, want 5", got)
		}
	})

	t.Run("floor of 1 enforced", func(t *testing.T) {
		limiter := NewAdaptiveParallelismLimiter(1) // io=1, cpu=1

		// Repeated high-error adjustments should not go below 1.
		for i := 0; i < 5; i++ {
			limiter.AdjustLimits(ExecutionMetrics{ErrorRate: 0.9})
		}

		if got := limiter.Limit(ProfileIOBound); got != 1 {
			t.Errorf("io_bound limit after repeated shrinkage = %d, want 1 (floor)", got)
		}
		if got := limiter.Limit(ProfileCPUBound); got != 1 {
			t.Errorf("cpu_bound limit after repeated shrinkage = %d, want 1 (floor)", got)
		}
	})

	t.Run("cap at maxAdaptiveLimit enforced", func(t *testing.T) {
		// Use a limiter whose io-bound is already at the cap.
		limiter := NewAdaptiveParallelismLimiter(maxAdaptiveLimit)

		// Healthy metrics should not increase beyond cap.
		for i := 0; i < 5; i++ {
			limiter.AdjustLimits(ExecutionMetrics{
				ErrorRate:  0.0,
				AvgLatency: 10 * time.Millisecond,
			})
		}

		if got := limiter.Limit(ProfileIOBound); got != maxAdaptiveLimit {
			t.Errorf("io_bound limit after repeated growth = %d, want %d (cap)", got, maxAdaptiveLimit)
		}
	})

	t.Run("moderate error rate does not adjust", func(t *testing.T) {
		limiter := NewAdaptiveParallelismLimiter(4) // io=4, cpu=2

		limiter.AdjustLimits(ExecutionMetrics{
			ErrorRate:  0.2,
			AvgLatency: 100 * time.Millisecond,
		})

		if got := limiter.Limit(ProfileIOBound); got != 4 {
			t.Errorf("io_bound limit should be unchanged = %d, want 4", got)
		}
		if got := limiter.Limit(ProfileCPUBound); got != 2 {
			t.Errorf("cpu_bound limit should be unchanged = %d, want 2", got)
		}
	})

	t.Run("high error rate but high latency does not grow", func(t *testing.T) {
		limiter := NewAdaptiveParallelismLimiter(4)

		limiter.AdjustLimits(ExecutionMetrics{
			ErrorRate:  0.01,
			AvgLatency: 2 * time.Second, // too slow
		})

		if got := limiter.Limit(ProfileIOBound); got != 4 {
			t.Errorf("io_bound limit should not grow with high latency = %d, want 4", got)
		}
	})

	t.Run("adjust then acquire works with new limit", func(t *testing.T) {
		limiter := NewAdaptiveParallelismLimiter(4) // io=4

		// Grow to 5.
		limiter.AdjustLimits(ExecutionMetrics{
			ErrorRate:  0.0,
			AvgLatency: 50 * time.Millisecond,
		})

		if got := limiter.Limit(ProfileIOBound); got != 5 {
			t.Fatalf("io_bound limit = %d, want 5", got)
		}

		// Acquire 5 slots without blocking.
		for i := 0; i < 5; i++ {
			if err := limiter.Acquire(context.Background(), ProfileIOBound); err != nil {
				t.Fatalf("Acquire[%d] failed: %v", i, err)
			}
		}

		// 6th should block.
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if err := limiter.Acquire(ctx, ProfileIOBound); err == nil {
			t.Error("6th Acquire should have timed out (limit=5)")
		}

		// Release all.
		for i := 0; i < 5; i++ {
			limiter.Release(ProfileIOBound)
		}
	})
}

func TestAdaptiveParallelismLimiter_SetLogger(t *testing.T) {
	limiter := NewAdaptiveParallelismLimiter(4)

	// Nil should be ignored (no panic).
	limiter.SetLogger(nil)

	// Non-nil should be set.
	l := slog.Default()
	limiter.SetLogger(l)
	if limiter.logger != l {
		t.Error("expected logger to be set")
	}
}
