package employee

import (
	"context"
	"errors"
	"testing"
	"time"
)

// retryCostExecutor fails a configurable number of attempts, reporting a
// different cent cost per attempt. It implements the optional
// executionCostReporter seam so executeBotWithRetry can accumulate cost.
type retryCostExecutor struct {
	remainingFailures int
	costs             []int // cent cost per attempt (index = attempt)
	attempts          int
}

func (e *retryCostExecutor) ExecuteBot(context.Context, string, string) (string, int, error) {
	e.attempts++
	if e.remainingFailures > 0 {
		e.remainingFailures--
		return "", 10, errors.New("transient failure")
	}
	return "done", 10, nil
}

func (e *retryCostExecutor) ExecutionCostCents() int {
	i := e.attempts - 1
	if i < 0 || i >= len(e.costs) {
		return 0
	}
	return e.costs[i]
}

// TestExecuteBotWithRetry_L9AccumulatesCostAcrossAttempts pins the L9 fix:
// a failed EXPENSIVE attempt (50c) retried into a successful CHEAP attempt
// (3c) must report the SUM (53c), not just the final attempt's 3c. RED
// before the fix: the round recorded executionCostCents(runner) ONCE after
// the loop, so the failed attempt's provider dollars were discarded and a
// costly retry storm could bypass the daily-budget lane.
func TestExecuteBotWithRetry_L9AccumulatesCostAcrossAttempts(t *testing.T) {
	exec := &retryCostExecutor{
		remainingFailures: 1,
		costs:             []int{50, 3},
	}
	loop := NewGoalLoop("emp-l9", testTier1Constitution(), nil, nil).
		WithRetryPolicy(RetryPolicy{MaxRetries: 2, RetryBackoff: time.Millisecond})

	_, _, cents, err := loop.executeBotWithRetry(context.Background(), exec, "sys", "work")
	if err != nil {
		t.Fatalf("executeBotWithRetry: %v (retry should have recovered)", err)
	}
	if exec.attempts != 2 {
		t.Errorf("attempts = %d, want 2", exec.attempts)
	}
	if cents != 53 {
		t.Errorf("accumulated cents = %d, want 53 (50 failed + 3 success); retry cost was discarded", cents)
	}
}

// TestExecuteBotWithRetry_L9NoCostReporterStaysZero pins byte-identical
// behavior for a plain BotExecutor with no cost reporter: it contributes 0
// cents, so the daily-budget lane sees only token counts as before.
func TestExecuteBotWithRetry_L9NoCostReporterStaysZero(t *testing.T) {
	exec := &flakyExecutor{err: errors.New("transient")}
	exec.failuresLeft.Store(1)
	loop := NewGoalLoop("emp-l9-plain", testTier1Constitution(), nil, nil).
		WithRetryPolicy(RetryPolicy{MaxRetries: 2, RetryBackoff: time.Millisecond})
	_, _, cents, err := loop.executeBotWithRetry(context.Background(), exec, "sys", "work")
	if err != nil {
		t.Fatalf("executeBotWithRetry: %v", err)
	}
	if cents != 0 {
		t.Errorf("cents = %d, want 0 (no cost reporter)", cents)
	}
}
