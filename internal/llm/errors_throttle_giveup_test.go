package llm

// ThrottleGiveUpError tests (tree 03 leaf 02 Task 1, DECISIONS.md D8): the
// give-up surface when the throttle wait exceeds the parking MaxWait cap.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestThrottleGiveUpError_Error(t *testing.T) {
	err := &ThrottleGiveUpError{
		ProviderID: "prov",
		ModelID:    "model-1",
		Waited:     2 * time.Hour,
	}
	msg := err.Error()
	if !strings.Contains(msg, "prov") || !strings.Contains(msg, "model-1") {
		t.Errorf("Error() = %q, want provider+model", msg)
	}
	if !strings.Contains(msg, "abandoned") {
		t.Errorf("Error() = %q, want the abandonment statement", msg)
	}
}

func TestThrottleGiveUpError_UserMessage(t *testing.T) {
	err := &ThrottleGiveUpError{
		ProviderID: "prov",
		ModelID:    "model-1",
		Waited:     3 * time.Hour,
	}
	msg := err.UserMessage()
	// D8 wording: provider, waited duration, next step.
	if !strings.Contains(msg, "throttled for 3h") {
		t.Errorf("UserMessage() = %q, want the waited duration", msg)
	}
	if !strings.Contains(msg, "turn abandoned") {
		t.Errorf("UserMessage() = %q, want the abandonment statement", msg)
	}
	if !strings.Contains(msg, "queue/goal policy applies") {
		t.Errorf("UserMessage() = %q, want the next step", msg)
	}
}

func TestThrottleGiveUpError_UserMessageNoModel(t *testing.T) {
	err := &ThrottleGiveUpError{ProviderID: "prov", Waited: 90 * time.Minute}
	msg := err.UserMessage()
	if !strings.Contains(msg, "throttled for 1h 30m") {
		t.Errorf("UserMessage() = %q, want truncated minute formatting", msg)
	}
	if strings.Contains(msg, "on ") {
		t.Errorf("UserMessage() = %q, want no model clause when ModelID empty", msg)
	}
}

func TestThrottleGiveUpError_NonRetryable(t *testing.T) {
	err := &ThrottleGiveUpError{ProviderID: "prov", Waited: time.Hour}
	if !err.NonRetryable() {
		t.Error("NonRetryable() = false, want true (abandoned turns never re-enter retry loops)")
	}
	var iface NonRetryableError = err
	if !iface.NonRetryable() {
		t.Error("NonRetryableError interface assertion failed")
	}
}

func TestAsThrottleGiveUpError(t *testing.T) {
	inner := &ThrottleGiveUpError{ProviderID: "p", ModelID: "m", Waited: time.Hour}
	wrapped := fmt.Errorf("turn failed: %w", inner)
	var got *ThrottleGiveUpError
	if !errors.As(wrapped, &got) || got.ModelID != "m" {
		t.Error("errors.As should find ThrottleGiveUpError through wrapping")
	}
	if errors.As(error(&RateLimitError{}), &got) {
		t.Error("RateLimitError must not match ThrottleGiveUpError")
	}
	if _, ok := AsThrottleGiveUpError(wrapped); !ok {
		t.Error("AsThrottleGiveUpError should find the wrapped error")
	}
	if _, ok := AsThrottleGiveUpError(nil); ok {
		t.Error("AsThrottleGiveUpError(nil) must be false")
	}
}

func TestUserMessageThrottleGiveUp(t *testing.T) {
	inner := &ThrottleGiveUpError{ProviderID: "p", ModelID: "m", Waited: 2 * time.Hour}
	wrapped := fmt.Errorf("wrapped: %w", inner)
	msg := UserMessage(wrapped)
	if !strings.Contains(msg, "turn abandoned") {
		t.Errorf("UserMessage(wrapped) = %q, want the ThrottleGiveUpError surface", msg)
	}
}
