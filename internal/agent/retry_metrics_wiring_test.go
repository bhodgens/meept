package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/caimlas/meept/pkg/security"
)

// TestRetryMetrics_ExecutorWiring verifies that when an Executor is constructed
// with WithExecutorRetryMetrics, retry events from executeToolWithRetry are
// recorded in the metrics collector.
func TestRetryMetrics_ExecutorWiring(t *testing.T) {
	t.Parallel()

	metrics := NewRetryMetrics()

	var calls int32
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("flaky_tool", "a tool that fails once then succeeds", func(ctx context.Context, args map[string]any) (any, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			return nil, Retryable(
				errors.New("transient failure"),
				true, 0, "test retry",
			)
		}
		return "ok", nil
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker,
		WithParallelism(1),
		WithExecutorRetryMetrics(metrics),
	)

	fastCfg := fastBackoffConfig()
	tool := registry.tools["flaky_tool"]

	result, err := executor.executeToolWithRetry(
		context.Background(),
		tool, fastCfg,
		"call_wiring_1", "flaky_tool",
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	snap := metrics.Snapshot()
	if snap.TotalRetries != 1 {
		t.Errorf("TotalRetries: want 1, got %d", snap.TotalRetries)
	}
	if snap.RetriesByType["tool"] != 1 {
		t.Errorf("RetriesByType[tool]: want 1, got %d", snap.RetriesByType["tool"])
	}
}

// TestRetryMetrics_ExecutorNilSafe verifies that the executor functions
// correctly when no retry metrics collector is wired (nil safety).
func TestRetryMetrics_ExecutorNilSafe(t *testing.T) {
	t.Parallel()

	var calls int32
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("safe_tool", "nil-safe test", func(ctx context.Context, args map[string]any) (any, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			return nil, Retryable(
				errors.New("transient"),
				true, 0, "test",
			)
		}
		return "ok", nil
	}))

	secChecker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, secChecker, WithParallelism(1))

	fastCfg := fastBackoffConfig()
	tool := registry.tools["safe_tool"]

	result, err := executor.executeToolWithRetry(
		context.Background(),
		tool, fastCfg,
		"call_nil_1", "safe_tool",
		map[string]any{},
	)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
