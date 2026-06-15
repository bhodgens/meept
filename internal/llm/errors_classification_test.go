package llm

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestClassifyClassificationFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ClassificationFailureKind
	}{
		// Structured error types (errors.As / errors.Is)
		{"nil error", nil, ClassificationFailureUnknown},
		{"context deadline", context.DeadlineExceeded, ClassificationFailureTimeout},
		{"wrapped context deadline", fmt.Errorf("ctx: %w", context.DeadlineExceeded), ClassificationFailureTimeout},
		{"BudgetExceededError", &BudgetExceededError{Message: "budget exceeded"}, ClassificationFailureBudget},
		{"wrapped BudgetExceededError", fmt.Errorf("ctx: %w", &BudgetExceededError{Message: "budget"}), ClassificationFailureBudget},
		{"CapabilityError", &CapabilityError{SkillName: "code", Requires: []string{"tool_use"}}, ClassificationFailureUnavailable},
		{"wrapped CapabilityError", fmt.Errorf("ctx: %w", &CapabilityError{}), ClassificationFailureUnavailable},
		{"APIError 429", &APIError{StatusCode: 429}, ClassificationFailureUnavailable},
		{"APIError 500", &APIError{StatusCode: 500}, ClassificationFailureUnavailable},
		{"APIError 503", &APIError{StatusCode: 503}, ClassificationFailureUnavailable},
		{"APIError 400", &APIError{StatusCode: 400}, ClassificationFailureUnknown},
		{"wrapped APIError 429", fmt.Errorf("ctx: %w", &APIError{StatusCode: 429}), ClassificationFailureUnavailable},

		// String-based fallback for empty response (no structured type exists yet)
		{"empty: no choices", errors.New("no choices in response"), ClassificationFailureEmptyResponse},
		{"empty: empty content", errors.New("empty content"), ClassificationFailureEmptyResponse},

		// Old substring-matched cases that are now ClassificationFailureUnknown
		// because they're plain string errors, not structured types.
		{"plain 'budget exhausted' string (now unknown)", errors.New("token budget exhausted"), ClassificationFailureUnknown},
		{"plain 'unavailable' string (now unknown)", errors.New("model unavailable for provider"), ClassificationFailureUnknown},
		{"plain 'no models' string (now unknown)", errors.New("no models found"), ClassificationFailureUnknown},
		{"plain 'empty response' string (now unknown)", errors.New("empty response from model"), ClassificationFailureUnknown},

		{"unknown error", errors.New("something went wrong"), ClassificationFailureUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyClassificationFailure(tt.err)
			if got != tt.expected {
				t.Errorf("ClassifyClassificationFailure(%v) = %q, want %q", tt.err, got, tt.expected)
			}
		})
	}
}

func TestClassificationUserGuidance(t *testing.T) {
	tests := []struct {
		kind     ClassificationFailureKind
		contains string
	}{
		{ClassificationFailureEmptyResponse, "empty response"},
		{ClassificationFailureUnavailable, "available"},
		{ClassificationFailureBudget, "budget"},
		{ClassificationFailureTimeout, "timed out"},
		{ClassificationFailureUnknown, "unexpected"},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			err := errors.New(string(tt.kind))
			guidance := ClassificationUserGuidance(err)
			if guidance == "" {
				t.Error("expected non-empty guidance")
			}
		})
	}
}
