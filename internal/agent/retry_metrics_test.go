package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRetryMetrics_RecordRetry verifies that TotalRetries, RetriesByType,
// RetriesByError, and BackoffDuration all update correctly for a sequence of
// RecordRetry calls with different operation types, errors, and delays.
func TestRetryMetrics_RecordRetry(t *testing.T) {
	m := NewRetryMetrics()

	errA := errors.New("timeout")
	errB := &HTTPError{StatusCode: 503, Body: "unavailable", URL: "http://x"}

	m.RecordRetry("llm", errA, 2*time.Second)
	m.RecordRetry("llm", errA, 4*time.Second)
	m.RecordRetry("tool", errB, 1*time.Second)

	snap := m.Snapshot()

	if snap.TotalRetries != 3 {
		t.Errorf("TotalRetries: want 3, got %d", snap.TotalRetries)
	}
	if snap.RetriesByType["llm"] != 2 {
		t.Errorf("RetriesByType[llm]: want 2, got %d", snap.RetriesByType["llm"])
	}
	if snap.RetriesByType["tool"] != 1 {
		t.Errorf("RetriesByType[tool]: want 1, got %d", snap.RetriesByType["tool"])
	}
	// errA is *errors.errorString, errB is *agent.HTTPError
	errAKey := "*errors.errorString"
	errBKey := "*agent.HTTPError"
	if snap.RetriesByError[errAKey] != 2 {
		t.Errorf("RetriesByError[%s]: want 2, got %d", errAKey, snap.RetriesByError[errAKey])
	}
	if snap.RetriesByError[errBKey] != 1 {
		t.Errorf("RetriesByError[%s]: want 1, got %d", errBKey, snap.RetriesByError[errBKey])
	}
	if snap.BackoffDuration != 7*time.Second {
		t.Errorf("BackoffDuration: want 7s, got %v", snap.BackoffDuration)
	}
}

// TestRetryMetrics_RecordRetry_NilError verifies that a nil error is classified
// without panicking (fmt.Sprintf("%T", nil) => "<nil>").
func TestRetryMetrics_RecordRetry_NilError(t *testing.T) {
	m := NewRetryMetrics()
	m.RecordRetry("http", nil, 500*time.Millisecond)

	snap := m.Snapshot()
	if snap.TotalRetries != 1 {
		t.Fatalf("TotalRetries: want 1, got %d", snap.TotalRetries)
	}
	if snap.RetriesByError["<nil>"] != 1 {
		t.Errorf("RetriesByError[<nil>]: want 1, got %d", snap.RetriesByError["<nil>"])
	}
}

// TestRetryMetrics_Snapshot_Isolation verifies that mutating the returned
// snapshot (or its maps) does not affect the internal state of RetryMetrics.
func TestRetryMetrics_Snapshot_Isolation(t *testing.T) {
	m := NewRetryMetrics()
	m.RecordRetry("llm", errors.New("boom"), 1*time.Second)

	snap1 := m.Snapshot()

	// Mutate the snapshot maps.
	snap1.RetriesByType["llm"] = 999
	snap1.RetriesByError["fake"] = 42
	snap1.TotalRetries = 0

	// Internal state should be unaffected.
	snap2 := m.Snapshot()
	if snap2.TotalRetries != 1 {
		t.Errorf("TotalRetries after mutation: want 1, got %d", snap2.TotalRetries)
	}
	if snap2.RetriesByType["llm"] != 1 {
		t.Errorf("RetriesByType[llm] after mutation: want 1, got %d", snap2.RetriesByType["llm"])
	}
	if _, ok := snap2.RetriesByError["fake"]; ok {
		t.Error("RetriesByError should not contain 'fake' key")
	}
}

// TestRetryMetrics_Concurrent runs parallel RecordRetry calls to verify no data
// races under -race.
func TestRetryMetrics_Concurrent(t *testing.T) {
	m := NewRetryMetrics()
	errs := []error{
		errors.New("e1"),
		errors.New("e2"),
		&HTTPError{StatusCode: 502, Body: "bad", URL: "http://x"},
	}
	opTypes := []string{"llm", "tool", "http"}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m.RecordRetry(opTypes[idx%3], errs[idx%3], time.Duration(idx)*time.Millisecond)
		}(i)
	}
	wg.Wait()

	snap := m.Snapshot()
	if snap.TotalRetries != 100 {
		t.Errorf("TotalRetries: want 100, got %d", snap.TotalRetries)
	}
	// Each opType gets a mix of calls.
	totalByType := snap.RetriesByType["llm"] + snap.RetriesByType["tool"] + snap.RetriesByType["http"]
	if totalByType != 100 {
		t.Errorf("sum of type counts: want 100, got %d", totalByType)
	}
}

// TestBackoff_WithLogger verifies that WithLogger sets the logger field and
// returns the same Backoff pointer. Also verifies the nil guard.
func TestBackoff_WithLogger(t *testing.T) {
	b := NewBackoff(DefaultBackoffConfig())

	var buf bytes.Buffer
	custom := slog.New(slog.NewJSONHandler(&buf, nil))

	returned := b.WithLogger(custom)
	if returned != b {
		t.Error("WithLogger should return the same Backoff pointer")
	}
	if b.logger != custom {
		t.Error("WithLogger should set b.logger to the provided logger")
	}

	// Nil guard: should not change the logger.
	original := b.logger
	b.WithLogger(nil)
	if b.logger != original {
		t.Error("WithLogger(nil) should not change the logger")
	}
}

// TestBackoff_Sleep_Logging_MaxAttempts verifies that Sleep emits
// "backoff max attempts reached" when the retry budget is exhausted.
func TestBackoff_Sleep_Logging_MaxAttempts(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	b := NewBackoff(BackoffConfig{
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 1,
	})
	b.WithLogger(logger)

	ctx := context.Background()
	// First Sleep succeeds (attempt 0).
	if !b.Sleep(ctx) {
		t.Fatal("first Sleep should succeed")
	}
	// Second Sleep fails (max attempts reached).
	if b.Sleep(ctx) {
		t.Fatal("second Sleep should fail (max attempts)")
	}

	output := buf.String()
	if !strings.Contains(output, "backoff max attempts reached") {
		t.Errorf("expected 'backoff max attempts reached' in logs, got: %s", output)
	}
}

// TestBackoff_Sleep_Logging_Waiting verifies that Sleep emits
// "backoff waiting" with attempt and delay_ms fields.
func TestBackoff_Sleep_Logging_Waiting(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	b := NewBackoff(BackoffConfig{
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 3,
	})
	b.WithLogger(logger)

	b.Sleep(context.Background())

	output := buf.String()
	if !strings.Contains(output, "backoff waiting") {
		t.Errorf("expected 'backoff waiting' in logs, got: %s", output)
	}

	// Verify structured fields are present in the JSON.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "backoff waiting") {
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("failed to parse log JSON: %v", err)
			}
			if _, ok := entry["attempt"]; !ok {
				t.Error("backoff waiting log should contain 'attempt' field")
			}
			if _, ok := entry["delay_ms"]; !ok {
				t.Error("backoff waiting log should contain 'delay_ms' field")
			}
			break
		}
	}
}

// TestBackoff_Sleep_Logging_ContextCancellation verifies that Sleep emits
// "backoff interrupted by context cancellation" when the context is cancelled.
func TestBackoff_Sleep_Logging_ContextCancellation(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	b := NewBackoff(BackoffConfig{
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 5,
	})
	b.WithLogger(logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so Sleep returns right away

	b.Sleep(ctx)

	output := buf.String()
	if !strings.Contains(output, "backoff interrupted by context cancellation") {
		t.Errorf("expected 'backoff interrupted by context cancellation' in logs, got: %s", output)
	}
}

// TestRunWithRetry_Logging_NonRetryable verifies that RunWithRetry logs
// "retry stopped: non-retryable error" for non-retryable errors.
func TestRunWithRetry_Logging_NonRetryable(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldDefault)

	_, err := RunWithRetry(context.Background(), AggressiveBackoffConfig(), func() (string, error) {
		return "", errors.New("permanent")
	})
	if err == nil {
		t.Fatal("expected error")
	}

	output := buf.String()
	if !strings.Contains(output, "retry stopped: non-retryable error") {
		t.Errorf("expected 'retry stopped: non-retryable error' in logs, got: %s", output)
	}
}

// TestRunWithRetry_Logging_RetrySuccess verifies that RunWithRetry logs
// "retry succeeding after attempts" when a retry eventually succeeds, and
// "retrying after error" for each retry attempt.
func TestRunWithRetry_Logging_RetrySuccess(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldDefault)

	config := BackoffConfig{
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 5,
	}
	calls := 0
	_, err := RunWithRetry(context.Background(), config, func() (string, error) {
		calls++
		if calls < 2 {
			return "", Retryable(errors.New("transient"), true, 0, "test")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "retry succeeding after attempts") {
		t.Errorf("expected 'retry succeeding after attempts' in logs, got: %s", output)
	}
	if !strings.Contains(output, "retrying after error") {
		t.Errorf("expected 'retrying after error' in logs, got: %s", output)
	}
}
