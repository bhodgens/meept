package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func TestIsRetryable_NetworkErrors(t *testing.T) {
	// context.DeadlineExceeded is classified as retryable by errcls.
	err := context.DeadlineExceeded
	if !IsRetryable(err) {
		t.Error("context.DeadlineExceeded should be retryable")
	}
}

func TestIsRetryable_HTTPStatuses(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{429, true},
		{502, true},
		{503, true},
		{400, false},
		{401, false},
		{404, false},
	}
	for _, tt := range tests {
		httpErr := &HTTPError{StatusCode: tt.code, Body: "test", URL: "http://example.com"}
		got := IsRetryable(httpErr)
		if got != tt.want {
			t.Errorf("HTTP %d via HTTPError: want retryable=%v, got %v", tt.code, tt.want, got)
		}
		// Also test the helper directly.
		if got := isRetryableHTTPStatus(tt.code); got != tt.want {
			t.Errorf("isRetryableHTTPStatus(%d): want %v, got %v", tt.code, tt.want, got)
		}
	}
}

func TestIsRetryable_RateLimitError(t *testing.T) {
	err := &llm.RateLimitError{
		ProviderID: "test",
		ModelID:    "test-model",
		RetryAfter: 3 * time.Second,
	}
	if !IsRetryable(err) {
		t.Error("llm.RateLimitError should be retryable")
	}
	retryAfter := GetRetryAfter(err)
	if retryAfter != 3*time.Second {
		t.Errorf("GetRetryAfter: want 3s, got %v", retryAfter)
	}
}

func TestIsRetryable_RetryableWrapper(t *testing.T) {
	inner := errors.New("something went wrong")
	wrapped := Retryable(inner, true, 5*time.Second, "test-reason")
	if !IsRetryable(wrapped) {
		t.Error("Retryable(inner, true, ...) should be retryable")
	}
	retryAfter := GetRetryAfter(wrapped)
	if retryAfter != 5*time.Second {
		t.Errorf("GetRetryAfter: want 5s, got %v", retryAfter)
	}

	// Non-retryable wrapper.
	wrappedNo := Retryable(inner, false, 0, "permanent")
	if IsRetryable(wrappedNo) {
		t.Error("Retryable(inner, false, ...) should NOT be retryable")
	}

	// Nil guard.
	if Retryable(nil, true, 0, "") != nil {
		t.Error("Retryable(nil, ...) should return nil")
	}
}

func TestRetryBudget_Exhaustion(t *testing.T) {
	budget := NewRetryBudget(3)
	for i := 0; i < 3; i++ {
		if !budget.TryUse("op1") {
			t.Errorf("TryUse attempt %d should succeed", i+1)
		}
	}
	// 4th attempt should fail.
	if budget.TryUse("op1") {
		t.Error("TryUse should fail after budget exhausted")
	}
	if budget.Remaining() != 0 {
		t.Errorf("Remaining: want 0, got %d", budget.Remaining())
	}
	if budget.Used() != 3 {
		t.Errorf("Used: want 3, got %d", budget.Used())
	}
	if budget.UsedBy("op1") != 3 {
		t.Errorf("UsedBy(op1): want 3, got %d", budget.UsedBy("op1"))
	}
}

func TestRetryBudget_Reset(t *testing.T) {
	budget := NewRetryBudget(2)
	budget.TryUse("a")
	budget.TryUse("a")
	if budget.Remaining() != 0 {
		t.Errorf("after exhausting: Remaining want 0, got %d", budget.Remaining())
	}

	budget.Reset()
	if budget.Remaining() != 2 {
		t.Errorf("after Reset: Remaining want 2, got %d", budget.Remaining())
	}
	if !budget.TryUse("b") {
		t.Error("TryUse should succeed after Reset")
	}
	if budget.UsedBy("a") != 0 {
		t.Errorf("after Reset: UsedBy(a) want 0, got %d", budget.UsedBy("a"))
	}
	if budget.UsedBy("b") != 1 {
		t.Errorf("after Reset + 1 TryUse(b): UsedBy(b) want 1, got %d", budget.UsedBy("b"))
	}
}
