package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// -----------------------------------------------------------------------
// Helper: create error types for testing
// -----------------------------------------------------------------------

func rateLimitErr() error {
	return &llm.RateLimitError{
		ProviderID: "test",
		ModelID:    "test-model",
		RetryAfter: 100 * time.Millisecond,
	}
}

func authErr() error {
	return &HTTPError{
		StatusCode: 401,
		Body:       "unauthorized",
		URL:        "http://test",
	}
}

func invalidReqErr() error {
	return fmt.Errorf("invalid_parameter: field 'x' is required")
}

func timeoutErr() error {
	return context.DeadlineExceeded
}

func networkErr() error {
	return fmt.Errorf("connection refused")
}

func recoverableHTTP502() error {
	return Retryable(fmt.Errorf("502: bad gateway"), true, 0, "502")
}

// -----------------------------------------------------------------------
// TestNewRetryRecovery
// -----------------------------------------------------------------------

func TestRetryRecovery_NewDefaults(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{})
	if r == nil {
		t.Fatal("expected non-nil RetryRecovery")
	}
	state := r.GetRecoveryState()
	if state.Completed {
		t.Error("should start with Completed=false")
	}

	// Verify default config via ShouldRetry
	if !r.ShouldRetry(rateLimitErr()) {
		t.Error("default config should retry rate limit errors")
	}
	if r.ShouldRetry(authErr()) {
		t.Error("default config should NOT retry auth errors")
	}
}

func TestRetryRecovery_CustomConfig(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{
		MaxRetries:           1,
		RetryDelay:           50 * time.Millisecond,
		RecoverableErrors:    []string{"transient"},
		NonRecoverableErrors: []string{"always_fail"},
	})

	// Should retry on custom recoverable pattern
	if !r.ShouldRetry(fmt.Errorf("transient failure")) {
		t.Error("should retry on custom recoverable pattern")
	}

	// Should not retry on custom non-recoverable pattern
	if r.ShouldRetry(fmt.Errorf("always fail retry")) {
		t.Error("should not retry on custom non-recoverable pattern")
	}
}

// -----------------------------------------------------------------------
// TestShouldRetry
// -----------------------------------------------------------------------

func TestShouldRetry_RateLimitReturnsTrue(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{})
	if !r.ShouldRetry(rateLimitErr()) {
		t.Error("ShouldRetry should return true for RateLimitError")
	}
}

func TestShouldRetry_NonRecoverableReturnsFalse(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{})
	if r.ShouldRetry(authErr()) {
		t.Error("ShouldRetry should return false for auth error")
	}
}

func TestShouldRetry_NilReturnsFalse(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{})
	if r.ShouldRetry(nil) {
		t.Error("ShouldRetry(nil) should return false")
	}
}

func TestShouldRetry_TimeoutReturnsTrue(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{})
	if !r.ShouldRetry(timeoutErr()) {
		t.Error("ShouldRetry should return true for DeadlineExceeded")
	}
}

func TestShouldRetry_NetworkReturnsTrue(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{})
	if !r.ShouldRetry(networkErr()) {
		t.Error("ShouldRetry should return true for connection refused")
	}
}

func TestShouldRetry_InvalidRequestReturnsFalse(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{})
	if r.ShouldRetry(invalidReqErr()) {
		t.Error("ShouldRetry should return false for invalid parameter error")
	}
}

func TestShouldRetry_502ReturnsTrue(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{})
	if !r.ShouldRetry(recoverableHTTP502()) {
		t.Error("ShouldRetry should return true for 502 error")
	}
}

func TestShouldRetry_NonRecoverablePatternPriority(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{
		// "timeout" is in both lists but non-recovarable should win.
		RecoverableErrors:    []string{"timeout"},
		NonRecoverableErrors: []string{"timeout"},
	})
	// "timeout" appears in error message
	if r.ShouldRetry(fmt.Errorf("operation timed out with timeout")) {
		t.Error("non-recoverable pattern should take priority")
	}
}

func TestShouldRetry_CaseInsensitive(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{
		RecoverableErrors:    []string{"RATELIMIT"},
		NonRecoverableErrors: nil,
	})
	if !r.ShouldRetry(fmt.Errorf("rate limit exceeded")) {
		t.Error("ShouldRetry pattern matching should be case-insensitive")
	}
}

// -----------------------------------------------------------------------
// TestReset
// -----------------------------------------------------------------------

func TestRetryRecovery_Reset(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{})
	r.state.CurrentRetry = 2
	r.state.LastError = "test error"
	r.state.Completed = true

	r.Reset()

	state := r.GetRecoveryState()
	if state.CurrentRetry != 0 {
		t.Errorf("after reset: CurrentRetry=%d, want 0", state.CurrentRetry)
	}
	if state.LastError != "" {
		t.Errorf("after reset: LastError=%q, want empty", state.LastError)
	}
	if state.Completed {
		t.Error("after reset: Completed should be false")
	}
}

// -----------------------------------------------------------------------
// TestExecuteWithRetry - Success on first attempt
// -----------------------------------------------------------------------

func TestExecuteWithRetry_SuccessFirstAttempt(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{
		MaxRetries: 3,
		RetryDelay: time.Second,
	})
	callCount := 0
	result, err := r.ExecuteWithRetry(context.Background(), "web_search", func() (any, error) {
		callCount++
		return "search result", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "search result" {
		t.Errorf("got %v, want search result", result)
	}
	if callCount != 1 {
		t.Errorf("called %d times, want 1", callCount)
	}
	state := r.GetRecoveryState()
	if !state.Completed {
		t.Error("should be completed")
	}
}

// -----------------------------------------------------------------------
// TestRetryRecovery_RateLimitRetry - retries on rate limit
// -----------------------------------------------------------------------

func TestRetryRecovery_RateLimitRetry(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{
		MaxRetries: 3,
		RetryDelay: 50 * time.Millisecond,
	})
	callCount := 0

	result, err := r.ExecuteWithRetry(context.Background(), "web_search", func() (any, error) {
		callCount++
		if callCount < 3 {
			return nil, rateLimitErr()
		}
		return "recovered", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "recovered" {
		t.Errorf("got %v, want recovered", result)
	}
	if callCount != 3 {
		t.Errorf("called %d times, want 3", callCount)
	}
	state := r.GetRecoveryState()
	if !state.Completed {
		t.Error("should be completed after recovery")
	}
	if state.RecoveryAttempts != 2 {
		t.Errorf("RecoveryAttempts=%d, want 2 (2 retries then success)", state.RecoveryAttempts)
	}
}

// -----------------------------------------------------------------------
// TestRetryRecovery_NonRecoverableSkip - no retry on auth error
// -----------------------------------------------------------------------

func TestRetryRecovery_NonRecoverableSkip(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{
		MaxRetries: 3,
		RetryDelay: time.Second,
	})
	callCount := 0

	_, err := r.ExecuteWithRetry(context.Background(), "web_search", func() (any, error) {
		callCount++
		return nil, authErr()
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 1 {
		t.Errorf("called %d times, want 1 (non-recoverable should not retry)", callCount)
	}
	state := r.GetRecoveryState()
	if !state.Final {
		t.Error("should be final")
	}
	if state.CurrentRetry != 0 {
		t.Errorf("CurrentRetry=%d, want 0 (no retries attempted)", state.CurrentRetry)
	}
}

// -----------------------------------------------------------------------
// TestRetryRecovery_ExponentialBackoff - delay increases
// -----------------------------------------------------------------------

func TestRetryRecovery_ExponentialBackoff(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{
		MaxRetries: 10,                    // high; we check delay progression
		RetryDelay: 10 * time.Millisecond, // fast for tests
	})

	callCount := 0
	start := time.Now()

	_, err := r.ExecuteWithRetry(context.Background(), "web_search", func() (any, error) {
		callCount++
		if callCount <= 3 {
			return nil, rateLimitErr()
		}
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Total time should reflect backoff delays: 10ms + 20ms + 40ms = 70ms + some overhead
	// Minimum total wait is ~70ms
	elapsed := time.Since(start)
	minDelay := 10*time.Millisecond + 20*time.Millisecond + 40*time.Millisecond
	if elapsed < minDelay {
		t.Errorf("elapsed %v, want at least %v for exponential backoff", elapsed, minDelay)
	}
	if callCount != 4 {
		t.Errorf("called %d times, want 4", callCount)
	}

	state := r.GetRecoveryState()
	// After success, the last recorded retry state should show attempts progressing
	if !state.Completed {
		t.Error("should be completed")
	}
}

// -----------------------------------------------------------------------
// TestRetryRecovery_MaxRetriesExceeded - gives up after N attempts
// -----------------------------------------------------------------------

func TestRetryRecovery_MaxRetriesExceeded(t *testing.T) {
	// Use Retryable wrapper so ShouldRetry recognizes the error.
	r := NewRetryRecovery(RecoveryConfig{
		MaxRetries: 2,
		RetryDelay: 10 * time.Millisecond,
	})
	callCount := 0

	_, err := r.ExecuteWithRetry(context.Background(), "web_search", func() (any, error) {
		callCount++
		return nil, Retryable(errors.New("transient failure"), true, 0, "transient")
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 1 initial + 2 retries = 3 total calls
	if callCount != 3 {
		t.Errorf("called %d times, want 3 (1 initial + 2 retries)", callCount)
	}
	state := r.GetRecoveryState()
	if !state.Final {
		t.Error("should be final (max retries exceeded)")
	}
	if state.CurrentRetry != 2 {
		t.Errorf("CurrentRetry=%d, want 2", state.CurrentRetry)
	}
	if state.RecoveryAttempts != 2 {
		t.Errorf("RecoveryAttempts=%d, want 2", state.RecoveryAttempts)
	}
}

// -----------------------------------------------------------------------
// TestExecuteWithRetry_ContextCancelled
// -----------------------------------------------------------------------

func TestRetryRecovery_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	r := NewRetryRecovery(RecoveryConfig{
		MaxRetries: 10,
		RetryDelay: 20 * time.Millisecond, // short enough for quick retry
	})
	callCount := 0

	_, err := r.ExecuteWithRetry(ctx, "web_search", func() (any, error) {
		callCount++
		return nil, Retryable(errors.New("transient"), true, 0, "transient")
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount <= 1 {
		t.Errorf("called %d times, expected at least 2 (initial + some retries before cancellation)", callCount)
	}
	state := r.GetRecoveryState()
	if !state.Final {
		t.Error("should be final on context cancelled")
	}
}

// -----------------------------------------------------------------------
// TestErrorTypeName
// -----------------------------------------------------------------------

func TestErrorTypeName(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "none"},
		{"rate_limit", rateLimitErr(), "rate_limit"},
		{"auth", authErr(), "auth_error"},
		{"deadline", timeoutErr(), "timeout"},
		{"http_502", recoverableHTTP502(), "unknown"}, // Retryable wrapper loses HTTPError type
		{"unknown", errors.New("some error"), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorTypeName(tt.err)
			if got != tt.want {
				t.Errorf("errorTypeName(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestBackoffDuration
// -----------------------------------------------------------------------

func TestBackoffDuration(t *testing.T) {
	base := 100 * time.Millisecond
	if got := backoffDuration(base, 0); got != base {
		t.Errorf("attempt 0: got %v, want %v", got, base)
	}
	if got := backoffDuration(base, 1); got != 2*base {
		t.Errorf("attempt 1: got %v, want %v", got, 2*base)
	}
	if got := backoffDuration(base, 2); got != 4*base {
		t.Errorf("attempt 2: got %v, want %v", got, 4*base)
	}
	if got := backoffDuration(base, 10); got != 30*time.Second {
		t.Errorf("attempt 10 capped: got %v, want 30s", got)
	}
}

// -----------------------------------------------------------------------
// TestRetryRecovery_ConcurrentAccess
// -----------------------------------------------------------------------

func TestRetryRecovery_ConcurrentAccess(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{
		MaxRetries: 5,
		RetryDelay: 10 * time.Millisecond,
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = r.GetRecoveryState()
			r.Reset()
			_ = r.ShouldRetry(fmt.Errorf("test"))
		}(i)
	}
	wg.Wait()
}

// -----------------------------------------------------------------------
// TestDefaultRecoveryConfig
// -----------------------------------------------------------------------

func TestDefaultRecoveryConfig_IsNonZero(t *testing.T) {
	cfg := DefaultRecoveryConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries: got %d, want 3", cfg.MaxRetries)
	}
	if cfg.RetryDelay != time.Second {
		t.Errorf("RetryDelay: got %v, want 1s", cfg.RetryDelay)
	}
	if len(cfg.RecoverableErrors) == 0 {
		t.Error("RecoverableErrors should not be empty")
	}
	if len(cfg.NonRecoverableErrors) == 0 {
		t.Error("NonRecoverableErrors should not be empty")
	}
}

// -----------------------------------------------------------------------
// TestErrorTypeName_HTTPError
// -----------------------------------------------------------------------

func TestErrorTypeName_HTTPError(t *testing.T) {
	tests := []struct {
		statusCode int
		want       string
	}{
		{401, "auth_error"},
		{403, "auth_error"},
		{429, "rate_limit"},
		{500, "http_500"},
		{502, "http_502"},
		{503, "http_503"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("HTTP_%d", tt.statusCode), func(t *testing.T) {
			httpErr := &HTTPError{
				StatusCode: tt.statusCode,
			}
			got := errorTypeName(httpErr)
			if got != tt.want {
				t.Errorf("errorTypeName(HTTPError{%d}) = %q, want %q", tt.statusCode, got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestRetryRecovery_ConcurrentExecuteWithRetry
// -----------------------------------------------------------------------

func TestRetryRecovery_ConcurrentCalls(t *testing.T) {
	r := NewRetryRecovery(RecoveryConfig{
		MaxRetries: 1,
		RetryDelay: 10 * time.Millisecond,
	})

	var wg sync.WaitGroup
	results := make([]any, 10)
	errs := make([]error, 10)

	for i := 0; i < 10; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = r.ExecuteWithRetry(context.Background(), "test", func() (any, error) {
				return i, nil
			})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: unexpected error: %v", i, err)
		}
		if results[i] != i {
			t.Errorf("call %d: result=%d, want %d", i, results[i], i)
		}
	}
}
