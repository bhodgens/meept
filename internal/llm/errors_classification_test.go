package llm

import (
	"context"
	"errors"
	"testing"
)

func TestClassifyClassificationFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ClassificationFailureKind
	}{
		{"nil error", nil, ClassificationFailureUnknown},
		{"context deadline", context.DeadlineExceeded, ClassificationFailureTimeout},
		{"budget exhausted", errors.New("token budget exhausted"), ClassificationFailureBudget},
		{"unavailable", errors.New("model unavailable for provider"), ClassificationFailureUnavailable},
		{"no models", errors.New("no models found"), ClassificationFailureUnavailable},
		{"empty response", errors.New("empty response from model"), ClassificationFailureEmptyResponse},
		{"no content", errors.New("no content in response"), ClassificationFailureEmptyResponse},
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
