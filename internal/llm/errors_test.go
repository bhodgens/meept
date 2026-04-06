package llm

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestRateLimitError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *RateLimitError
		want string
	}{
		{
			name: "with retry-after and cause",
			err: &RateLimitError{
				ProviderID: "anthropic",
				ModelID:    "claude-opus-4-6",
				RetryAfter: 30 * time.Second,
				Cause:      errors.New("boom"),
			},
			want: "rate limit exceeded: provider=anthropic model=claude-opus-4-6, retry-after=30s: boom",
		},
		{
			name: "no retry-after",
			err: &RateLimitError{
				ProviderID: "openai",
				ModelID:    "gpt-4",
			},
			want: "rate limit exceeded: provider=openai model=gpt-4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsRateLimitError(t *testing.T) {
	apiErr429 := &APIError{StatusCode: http.StatusTooManyRequests, Detail: "slow down"}
	apiErr500 := &APIError{StatusCode: http.StatusInternalServerError, Detail: "boom"}
	rlErr := &RateLimitError{ProviderID: "x", ModelID: "y"}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("nope"), false},
		{"RateLimitError", rlErr, true},
		{"APIError 429", apiErr429, true},
		{"APIError 500", apiErr500, false},
		{"wrapped RateLimitError", fmt.Errorf("wrap: %w", rlErr), true},
		{"wrapped APIError 429", fmt.Errorf("wrap: %w", apiErr429), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRateLimitError(tt.err); got != tt.want {
				t.Errorf("IsRateLimitError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestAsRateLimitError(t *testing.T) {
	rlErr := &RateLimitError{ProviderID: "p", ModelID: "m"}
	if got, ok := AsRateLimitError(rlErr, "ignored", "ignored"); !ok || got != rlErr {
		t.Errorf("expected to return original RateLimitError")
	}

	apiErr := &APIError{StatusCode: http.StatusTooManyRequests}
	got, ok := AsRateLimitError(apiErr, "anthropic", "claude")
	if !ok || got == nil {
		t.Fatalf("expected conversion from APIError 429")
	}
	if got.ProviderID != "anthropic" || got.ModelID != "claude" {
		t.Errorf("provider/model not propagated: %+v", got)
	}

	if _, ok := AsRateLimitError(errors.New("nope"), "p", "m"); ok {
		t.Errorf("expected false for plain error")
	}
	if _, ok := AsRateLimitError(nil, "p", "m"); ok {
		t.Errorf("expected false for nil error")
	}
}

func TestIsRateLimitErrorMessage(t *testing.T) {
	positives := []string{
		"rate limit exceeded",
		"HTTP 429 Too Many Requests",
		"too many requests",
		"quota exceeded",
		"rate_limit_error",
		"60 requests per minute",
		"5 api calls per second",
		"rpm limit reached",
		"tpm limit reached",
		"too many concurrent requests",
	}
	for _, s := range positives {
		if !IsRateLimitErrorMessage(s) {
			t.Errorf("expected %q to be detected", s)
		}
	}
	negatives := []string{"", "internal server error", "context cancelled", "permission denied"}
	for _, s := range negatives {
		if IsRateLimitErrorMessage(s) {
			t.Errorf("expected %q to NOT be detected", s)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("empty header should return 0, got %v", got)
	}
	if got := parseRetryAfter("30"); got != 30*time.Second {
		t.Errorf("seconds parsing failed: got %v", got)
	}
	if got := parseRetryAfter("invalid"); got != 0 {
		t.Errorf("invalid header should return 0, got %v", got)
	}
	// HTTP-date in the past
	past := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC1123)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("past date should return 0, got %v", got)
	}
	// HTTP-date in the future, capped at 5m
	future := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC1123)
	got := parseRetryAfter(future)
	if got > 5*time.Minute || got <= 0 {
		t.Errorf("future date should be capped at 5m, got %v", got)
	}
}
