package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestBackoff_ExponentialGrowth verifies that delay grows by Multiplier each
// attempt when Jitter is zero. With BaseDelay=1s, Multiplier=2, expect the
// sequence 1s, 2s, 4s, 8s (capped by MaxDelay).
func TestBackoff_ExponentialGrowth(t *testing.T) {
	config := BackoffConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0, // disable jitter for deterministic assertion
		MaxAttempts: 10,
	}
	b := NewBackoff(config)

	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
	}
	for i, want := range expected {
		got, ok := b.NextDelay()
		if !ok {
			t.Fatalf("attempt %d: NextDelay returned ok=false", i)
		}
		if got != want {
			t.Errorf("attempt %d: got %v, want %v", i, got, want)
		}
	}
}

// TestBackoff_MaxDelay verifies that the MaxDelay cap is enforced once the
// exponential sequence would exceed it.
func TestBackoff_MaxDelay(t *testing.T) {
	config := BackoffConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    4 * time.Second,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 10,
	}
	b := NewBackoff(config)

	// Sequence: 1s, 2s, 4s (capped), 4s (capped), 4s (capped)
	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		4 * time.Second,
		4 * time.Second,
	}
	for i, want := range expected {
		got, ok := b.NextDelay()
		if !ok {
			t.Fatalf("attempt %d: unexpected ok=false", i)
		}
		if got != want {
			t.Errorf("attempt %d: got %v, want %v (capped=%v)", i, got, want, got == config.MaxDelay)
		}
	}
}

// TestBackoff_MaxAttempts verifies that NextDelay returns ok=false after the
// configured MaxAttempts is reached.
func TestBackoff_MaxAttempts(t *testing.T) {
	config := BackoffConfig{
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 3,
	}
	b := NewBackoff(config)

	// First 3 calls succeed.
	for i := 0; i < 3; i++ {
		if _, ok := b.NextDelay(); !ok {
			t.Fatalf("attempt %d: expected ok=true", i)
		}
	}
	// 4th call should fail (max attempts exhausted).
	if _, ok := b.NextDelay(); ok {
		t.Error("expected ok=false after MaxAttempts exhausted")
	}
}

// TestBackoff_Reset verifies that Reset returns the backoff to its initial state.
func TestBackoff_Reset(t *testing.T) {
	config := BackoffConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 2,
	}
	b := NewBackoff(config)

	// Exhaust attempts.
	for i := 0; i < 2; i++ {
		if _, ok := b.NextDelay(); !ok {
			t.Fatalf("attempt %d: expected ok=true", i)
		}
	}
	if _, ok := b.NextDelay(); ok {
		t.Fatal("expected ok=false after exhaustion")
	}

	// Reset should allow retries again.
	b.Reset()
	if delay, ok := b.NextDelay(); !ok || delay != config.BaseDelay {
		t.Errorf("after Reset: got (%v, %v), want (%v, true)", delay, ok, config.BaseDelay)
	}
}

// TestRunWithRetry_Success verifies that a function that succeeds on the first
// attempt is called exactly once and its result is returned.
func TestRunWithRetry_Success(t *testing.T) {
	calls := 0
	result, err := RunWithRetry(context.Background(), AggressiveBackoffConfig(), func() (string, error) {
		calls++
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("got result %q, want %q", result, "ok")
	}
	if calls != 1 {
		t.Errorf("function called %d times, want 1", calls)
	}
}

// TestRunWithRetry_RetryThenSuccess verifies that a function that fails a few
// times and then succeeds is retried, and the final successful result is returned.
func TestRunWithRetry_RetryThenSuccess(t *testing.T) {
	// Use a fast backoff to keep test runtime under 1s.
	config := BackoffConfig{
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    10 * time.Millisecond,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 10,
	}

	calls := 0
	result, err := RunWithRetry(context.Background(), config, func() (string, error) {
		calls++
		if calls < 3 {
			return "", Retryable(errors.New("transient failure"), true, 0, "test")
		}
		return "success-on-3rd", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "success-on-3rd" {
		t.Errorf("got result %q, want %q", result, "success-on-3rd")
	}
	if calls != 3 {
		t.Errorf("function called %d times, want 3", calls)
	}
}

// TestRunWithRetry_NonRetryableReturnedImmediately verifies that a non-retryable
// error is returned without retrying.
func TestRunWithRetry_NonRetryableReturnedImmediately(t *testing.T) {
	config := AggressiveBackoffConfig()
	calls := 0
	_, err := RunWithRetry(context.Background(), config, func() (string, error) {
		calls++
		return "", errors.New("permanent failure") // not retryable
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Errorf("function called %d times, want 1 (non-retryable should not retry)", calls)
	}
}

// TestRunWithRetry_MaxAttemptsExhausted verifies that when max attempts are
// reached without success, the error is returned.
func TestRunWithRetry_MaxAttemptsExhausted(t *testing.T) {
	config := BackoffConfig{
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 3,
	}

	calls := 0
	_, err := RunWithRetry(context.Background(), config, func() (string, error) {
		calls++
		return "", Retryable(errors.New("always fails"), true, 0, "test")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// MaxAttempts=3 means the function is called once initially, then retried
	// 3 more times (NextDelay ok at attempt=0,1,2) before backoff refuses.
	// Total = 1 initial + 3 retries = 4 calls.
	if calls != 4 {
		t.Errorf("function called %d times, want 4 (1 initial + 3 retries)", calls)
	}
}

// TestRunWithRetry_RetryBudgetExhausted verifies that a retry budget caps the
// number of retries before MaxAttempts.
func TestRunWithRetry_RetryBudgetExhausted(t *testing.T) {
	config := BackoffConfig{
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 10, // high; budget should kick in first
	}
	budget := NewRetryBudget(2) // allow 2 retries
	ctx := WithRetryBudget(context.Background(), budget)

	calls := 0
	_, err := RunWithRetry(ctx, config, func() (string, error) {
		calls++
		return "", Retryable(errors.New("transient"), true, 0, "test")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Initial call + 2 budget retries = 3 total calls.
	if calls != 3 {
		t.Errorf("function called %d times, want 3 (1 initial + 2 budgeted)", calls)
	}
}

// TestRunWithRetry_ContextCancelled verifies that context cancellation during a
// backoff wait returns ctx.Err().
func TestRunWithRetry_ContextCancelled(t *testing.T) {
	config := BackoffConfig{
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0,
		MaxAttempts: 10,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	calls := 0
	_, err := RunWithRetry(ctx, config, func() (string, error) {
		calls++
		return "", Retryable(errors.New("transient"), true, 0, "test")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}
