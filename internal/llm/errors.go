package llm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// NonRetryableError marks errors that should not be retried.
// Budget exhaustion (BudgetExceededError) is a non-retryable error.
type NonRetryableError interface {
	error
	NonRetryable() bool
}

// RateLimitError is returned when a rate limit (HTTP 429) is encountered.
type RateLimitError struct {
	ProviderID string
	ModelID    string
	RetryAfter time.Duration
	Cause      error
}

func (e *RateLimitError) Error() string {
	retryMsg := ""
	if e.RetryAfter > 0 {
		retryMsg = fmt.Sprintf(", retry-after=%s", e.RetryAfter.Round(time.Second))
	}
	if e.Cause != nil {
		return fmt.Sprintf("rate limit exceeded: provider=%s model=%s%s: %v", e.ProviderID, e.ModelID, retryMsg, e.Cause)
	}
	return fmt.Sprintf("rate limit exceeded: provider=%s model=%s%s", e.ProviderID, e.ModelID, retryMsg)
}

func (e *RateLimitError) Unwrap() error {
	return e.Cause
}

func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests
	}
	if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
		unwrapped := unwrapper.Unwrap()
		if unwrapped != nil {
			return IsRateLimitError(unwrapped)
		}
	}
	return false
}

func AsRateLimitError(err error, providerID, modelID string) (*RateLimitError, bool) {
	if err == nil {
		return nil, false
	}
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		return rlErr, true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{
			ProviderID: providerID,
			ModelID:    modelID,
			Cause:      apiErr,
		}, true
	}
	return nil, false
}

func IsRateLimitErrorMessage(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "429") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "quota exceeded") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "requests per") ||
		strings.Contains(lower, "api calls per") ||
		strings.Contains(lower, "rpm limit") ||
		strings.Contains(lower, "tpm limit") ||
		strings.Contains(lower, "concurrent requests")
}

// IsNonRetryable checks if an error is non-retryable.
func IsNonRetryable(err error) bool {
	if err == nil {
		return false
	}
	var nonRetryableErr NonRetryableError
	if errors.As(err, &nonRetryableErr) {
		return nonRetryableErr.NonRetryable()
	}
	return false
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if sec, err := parseRetryAfterSeconds(header); err == nil && sec > 0 {
		return sec
	}
	return parseRetryAfterDate(header)
}

func parseRetryAfterSeconds(header string) (time.Duration, error) {
	var seconds int
	n, err := fmt.Sscanf(header, "%d", &seconds)
	if err != nil || n != 1 || seconds <= 0 {
		return 0, fmt.Errorf("invalid seconds format")
	}
	return time.Duration(seconds) * time.Second, nil
}

func parseRetryAfterDate(header string) time.Duration {
	formats := []string{time.RFC1123, time.RFC3339}
	for _, format := range formats {
		t, err := time.Parse(format, header)
		if err == nil {
			duration := time.Until(t)
			if duration < 0 {
				return 0
			}
			if duration > 5*time.Minute {
				duration = 5 * time.Minute
			}
			return duration
		}
	}
	return 0
}

