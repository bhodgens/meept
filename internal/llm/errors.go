package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
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

// ContextSizeExceededError is returned when the context size exceeds the model limit.
type ContextSizeExceededError struct {
	Estimated   int      // Estimated token count
	ModelLimit  int      // Model's context window limit
	Suggestions []string // Suggestions for resolving the issue
}

func (e *ContextSizeExceededError) Error() string {
	return fmt.Sprintf("context size (%d tokens) exceeds model limit (%d tokens)", e.Estimated, e.ModelLimit)
}

func (e *ContextSizeExceededError) SuggestionsString() string {
	if len(e.Suggestions) == 0 {
		return ""
	}
	var s strings.Builder
	s.WriteString("\nSuggestions:\n")
	for i, sug := range e.Suggestions {
		fmt.Fprintf(&s, "  %d. %s\n", i+1, sug)
	}
	return s.String()
}

// NonRetryable marks ContextSizeExceededError as non-retryable
func (e *ContextSizeExceededError) NonRetryable() bool {
	return true
}

// Ensure ContextSizeExceededError implements NonRetryableError
var _ NonRetryableError = (*ContextSizeExceededError)(nil)

// RateLimitError is returned when a rate limit (HTTP 429) is encountered.
type RateLimitError struct {
	ProviderID   string
	ModelID      string
	RetryAfter   time.Duration
	LimitType    string         // e.g., "tpm_uncached", "rpm", "concurrent"
	RetryStrategy *RetryStrategy // Structured backoff advice from provider
	LimitBudget   *LimitBudget  // Current vs. limit values
	Cause        error
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

// RetryStrategy holds structured backoff advice from a provider.
type RetryStrategy struct {
	Type        string        // "tpm_uncached", "rpm", etc.
	InitialDelay time.Duration // Provider-suggested initial delay
	MaxDelay    time.Duration // Provider-suggested max delay
	Backoff     string        // "exponential", "linear", "fixed"
	BackoffBase float64       // Exponential base (e.g., 2.0)
	UseJitter   bool          // Provider recommends jitter
}

// LimitBudget holds current usage vs. allowed limits.
type LimitBudget struct {
	Used   int    // Current usage (e.g., 289280 tokens)
	Limit  int    // Maximum allowed (e.g., 200000 tokens)
	Window string // e.g., "per_minute", "per_day"
}

// ProviderErrorDetail is a provider-agnostic structured error.
type ProviderErrorDetail struct {
	Type          string         // "rate_limit_error", "authentication_error", etc.
	Code          string         // "tpm_uncached_exceeded", "insufficient_quota", etc.
	Message       string         // Human-readable message
	Retriable     bool           // Whether the provider says retry is worthwhile
	RetryAfter    time.Duration  // Explicit retry delay
	RetryStrategy *RetryStrategy
	LimitBudget   *LimitBudget
}

func (d *ProviderErrorDetail) Error() string {
	if d.Message != "" {
		return fmt.Sprintf("%s: %s", d.Type, d.Message)
	}
	return d.Type
}

// --- Provider-specific error parsers ---

// openRouterOuter represents the outer JSON envelope from OpenRouter.
type openRouterOuter struct {
	Error *openRouterOuterError `json:"error"`
}

type openRouterOuterError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// openRouterInner represents the inner JSON embedded in the error message.
type openRouterInner struct {
	Error openRouterInnerError `json:"error"`
}

type openRouterInnerError struct {
	Type         string                `json:"type"`
	Code         string                `json:"code"`
	Message      string                `json:"message"`
	RetryAfter   float64               `json:"retry_after"`
	RetryStrategy *openRouterInnerRetry `json:"retry_strategy"`
	Retriable    bool                  `json:"retriable"`
	Context      *openRouterInnerCtx    `json:"context"`
}

type openRouterInnerRetry struct {
	Type                string  `json:"type"`
	SuggestedInitialS   float64 `json:"suggested_initial_delay_s"`
	MaxDelayS           float64 `json:"max_delay_s"`
	Backoff             string  `json:"backoff"`
	BackoffBase         float64 `json:"backoff_base"`
	Jitter              bool    `json:"jitter"`
}

type openRouterInnerCtx struct {
	Budget        int    `json:"budget"`
	InFlight      int    `json:"in_flight"`
	Model         string `json:"model"`
	LimitType     string `json:"limit_type"`
	TPMWindowToks int    `json:"tpm_window_tokens"`
	TPMLimit      int    `json:"tpm_limit"`
}

// genericProviderError represents a simple {error:{type,message,code}} JSON body.
type genericProviderError struct {
	Error *genericProviderInner `json:"error"`
}

type genericProviderInner struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// ParseOpenRouterError extracts structured error info from OpenRouter-style JSON bodies.
// Returns nil if the body does not match the expected OpenRouter format.
func ParseOpenRouterError(body []byte) *ProviderErrorDetail {
	// Try parsing outer envelope
	var outer openRouterOuter
	if err := json.Unmarshal(body, &outer); err != nil || outer.Error == nil || outer.Error.Message == "" {
		return nil
	}

	msg := outer.Error.Message

	// Look for the inner JSON pattern: after "429): " or "): {"
	innerJSON := extractInnerJSON(msg)
	if innerJSON == "" {
		return nil
	}

	var inner openRouterInner
	if err := json.Unmarshal([]byte(innerJSON), &inner); err != nil {
		return nil
	}

	detail := &ProviderErrorDetail{
		Type:      inner.Error.Type,
		Code:      inner.Error.Code,
		Message:   inner.Error.Message,
		Retriable: inner.Error.Retriable,
		RetryAfter: time.Duration(inner.Error.RetryAfter * float64(time.Second)),
	}

	if inner.Error.RetryStrategy != nil {
		detail.RetryStrategy = &RetryStrategy{
			Type:        inner.Error.RetryStrategy.Type,
			InitialDelay: time.Duration(inner.Error.RetryStrategy.SuggestedInitialS * float64(time.Second)),
			MaxDelay:    time.Duration(inner.Error.RetryStrategy.MaxDelayS * float64(time.Second)),
			Backoff:     inner.Error.RetryStrategy.Backoff,
			BackoffBase: inner.Error.RetryStrategy.BackoffBase,
			UseJitter:   inner.Error.RetryStrategy.Jitter,
		}
	}

	if inner.Error.Context != nil {
		detail.LimitBudget = &LimitBudget{
			Used:   inner.Error.Context.TPMWindowToks,
			Limit:  inner.Error.Context.TPMLimit,
			Window: inner.Error.Context.LimitType,
		}
		// Use context limit_type for LimitType if available
		if inner.Error.Context.LimitType != "" {
			detail.RetryStrategy.Type = inner.Error.Context.LimitType
		}
	}

	return detail
}

// ParseGenericProviderError tries to parse a generic {error:{type,message,code}} JSON body.
// Returns nil if the body does not match this format.
func ParseGenericProviderError(body []byte) *ProviderErrorDetail {
	var parsed genericProviderError
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == nil {
		return nil
	}

	if parsed.Error.Type == "" && parsed.Error.Code == "" && parsed.Error.Message == "" {
		return nil
	}

	return &ProviderErrorDetail{
		Type:    parsed.Error.Type,
		Code:    parsed.Error.Code,
		Message: parsed.Error.Message,
	}
}

// ParseRateLimitBody attempts to parse a 429 response body into a ProviderErrorDetail.
// It tries OpenRouter format first, then generic JSON, and falls back to nil.
func ParseRateLimitBody(body []byte) *ProviderErrorDetail {
	if detail := ParseOpenRouterError(body); detail != nil {
		return detail
	}
	if detail := ParseGenericProviderError(body); detail != nil {
		return detail
	}
	return nil
}

// extractInnerJSON finds an inner JSON object string within a message.
// OpenRouter embeds the provider error JSON inside the outer error message.
// Pattern: "429): {\"error\":{...}}" or similar prefix followed by JSON.
func extractInnerJSON(msg string) string {
	// Try to find JSON after a "): " pattern (e.g., "429): {" or "error): {")
	for _, prefix := range []string{"): ", "){"} {
		idx := strings.LastIndex(msg, prefix)
		if idx == -1 {
			continue
		}
		var candidate string
		if prefix == "){" {
			// Skip past the ")" to get just the "{"
			candidate = msg[idx+1:]
		} else {
			// Skip past the ": "
			candidate = msg[idx+len(prefix):]
		}
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate, "{") {
			return candidate
		}
	}

	// Fallback: try to find the first '{' that starts a valid JSON object
	braceIdx := strings.Index(msg, "{")
	if braceIdx >= 0 {
		return msg[braceIdx:]
	}

	return ""
}

// BackoffWithJitter computes a backoff duration with optional full jitter.
// If useJitter is true, returns a uniform random duration in [0, delay].
// If useJitter is false, returns min(delay, maxDelay).
func BackoffWithJitter(delay time.Duration, maxDelay time.Duration, useJitter bool) time.Duration {
	if delay <= 0 {
		return 0
	}
	// Cap at maxDelay
	if maxDelay > 0 && delay > maxDelay {
		delay = maxDelay
	}
	if useJitter {
		// Full jitter: uniform random in [0, delay]
		return time.Duration(rand.Int64N(int64(delay) + 1))
	}
	return delay
}
