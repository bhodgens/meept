package llm

import (
	"context"
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
	ProviderID    string
	ModelID       string
	RetryAfter    time.Duration
	LimitType     string         // e.g., "tpm_uncached", "rpm", "concurrent"
	RetryStrategy *RetryStrategy // Structured backoff advice from provider
	LimitBudget   *LimitBudget   // Current vs. limit values
	Cause         error
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

func (e *RateLimitError) UserMessage() string {
	parts := []string{"rate limit hit"}
	if e.ModelID != "" {
		parts = append(parts, fmt.Sprintf("on %s", e.ModelID))
	}
	if e.LimitType != "" {
		parts = append(parts, fmt.Sprintf("(%s limit)", e.LimitType))
	}
	if e.RetryAfter > 0 {
		parts = append(parts, fmt.Sprintf("— retrying in %s", e.RetryAfter.Round(time.Second)))
	}
	return strings.Join(parts, " ")
}

func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := errors.AsType[*RateLimitError](err); ok {
		return true
	}
	if apiErr, ok := errors.AsType[*APIError](err); ok {
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
	if rlErr, ok := errors.AsType[*RateLimitError](err); ok {
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

// UserMessage returns a human-readable error message from any LLM error type.
// It unwraps the error chain and tries each known type.
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	// Try QuotaResetError first (most specific; wraps a 429 APIError so it
	// must be checked before RateLimitError/APIError).
	if quotaErr, ok := errors.AsType[*QuotaResetError](err); ok {
		return quotaErr.UserMessage()
	}
	// ThrottleGiveUpError (tree 03 leaf 02, D8): abandoned-throttle surface,
	// checked before RateLimitError (it describes the same 429 family).
	if throttleGiveUpErr, ok := errors.AsType[*ThrottleGiveUpError](err); ok {
		return throttleGiveUpErr.UserMessage()
	}
	// Try RateLimitError (most specific)
	if rlErr, ok := errors.AsType[*RateLimitError](err); ok {
		return rlErr.UserMessage()
	}
	// Try BudgetExceededError
	if budgetErr, ok := errors.AsType[*BudgetExceededError](err); ok {
		return budgetErr.UserMessage()
	}
	// Try APIError
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		return apiErr.UserMessage()
	}
	// Try ClientError
	if clientErr, ok := errors.AsType[*ClientError](err); ok {
		return clientErr.UserMessage()
	}
	// Fallback
	return err.Error()
}

// IsRateLimitErrorMessage is a string-only fallback for rate-limit detection.
//
// Deprecated: Use errcls.IsRateLimit(err) with a structured error value.
// This function is retained for callers that only have the serialized
// error string (e.g. errors deserialized from the message bus). It will
// not be removed until all such callers are migrated to pass the original
// error value.
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
	if nonRetryableErr, ok := errors.AsType[NonRetryableError](err); ok {
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
	Type         string        // "tpm_uncached", "rpm", etc.
	InitialDelay time.Duration // Provider-suggested initial delay
	MaxDelay     time.Duration // Provider-suggested max delay
	Backoff      string        // "exponential", "linear", "fixed"
	BackoffBase  float64       // Exponential base (e.g., 2.0)
	UseJitter    bool          // Provider recommends jitter
}

// LimitBudget holds current usage vs. allowed limits.
type LimitBudget struct {
	Used   int    // Current usage (e.g., 289280 tokens)
	Limit  int    // Maximum allowed (e.g., 200000 tokens)
	Window string // e.g., "per_minute", "per_day"
}

// ProviderError is a provider-agnostic structured error.
type ProviderError struct {
	Type          string        // "rate_limit_error", "authentication_error", etc.
	Code          string        // "tpm_uncached_exceeded", "insufficient_quota", etc.
	Message       string        // Human-readable message
	Retriable     bool          // Whether the provider says retry is worthwhile
	RetryAfter    time.Duration // Explicit retry delay
	RetryStrategy *RetryStrategy
	LimitBudget   *LimitBudget
}

func (d *ProviderError) Error() string {
	if d.Message != "" {
		return fmt.Sprintf("%s: %s", d.Type, d.Message)
	}
	return d.Type
}

// ThrottleBackoffError reports sustained provider throttling that exceeded
// the short in-loop retry budget (tree 02 leaf 03, DECISIONS.md D4/D8): a
// bare 429/503 load-shedding episode is NOT quota and NOT a dead model, so
// after the short retries burn out the loop returns this instead of a
// ClientError. RetryAt is the earliest future attempt time from the
// BackoffPlan (server Retry-After honored when later, capped by the plan
// horizon), letting the caller (agent loop, tree 03 parking) park the turn
// until then. Implements error + Unwrap so errors.As traverses the cause.
type ThrottleBackoffError struct {
	ProviderID string
	ModelID    string
	RetryAt    time.Time
	Attempt    int
	Cause      error
}

func (e *ThrottleBackoffError) Error() string {
	msg := fmt.Sprintf("throttled by provider %s model %s after %d attempts; retry at %s",
		e.ProviderID, e.ModelID, e.Attempt, e.RetryAt.Format(time.RFC3339))
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *ThrottleBackoffError) Unwrap() error {
	return e.Cause
}

// AsThrottleBackoffError returns the *ThrottleBackoffError in err's chain,
// mirroring AsQuotaResetError (errors_quota.go). Tree 03 leaf 03 consumes it
// to route park decisions off the verdict class.
func AsThrottleBackoffError(err error) (*ThrottleBackoffError, bool) {
	if err == nil {
		return nil, false
	}
	return errors.AsType[*ThrottleBackoffError](err)
}

// ThrottleGiveUpError is the D8 give-up surface for provider throttling
// (tree 03 leaf 02): the turn hit a ThrottleBackoffError but waiting until
// the backoff plan's next attempt would exceed the parking MaxWait cap, so
// the turn is abandoned rather than parked. The queue/goal policy applies
// its own retry policy on top (D8). Non-retryable: an abandoned turn never
// re-enters any retry loop.
type ThrottleGiveUpError struct {
	ProviderID string
	ModelID    string
	Waited     time.Duration // the wait that would have been required
}

func (e *ThrottleGiveUpError) Error() string {
	return fmt.Sprintf("provider %s throttled for %s (model %s) — turn abandoned: wait exceeds max wait",
		e.ProviderID, formatDuration(e.Waited), e.ModelID)
}

// UserMessage renders the D8 wording: provider, waited duration, next step.
// Mirrors QuotaResetError.UserMessage (errors_quota.go).
func (e *ThrottleGiveUpError) UserMessage() string {
	parts := []string{fmt.Sprintf("provider %s throttled for %s", e.ProviderID, formatDuration(e.Waited))}
	if e.ModelID != "" {
		parts = append(parts, fmt.Sprintf("on %s", e.ModelID))
	}
	parts = append(parts, "turn abandoned; queue/goal policy applies")
	return strings.Join(parts, " — ")
}

// NonRetryable returns true so no retry loop re-enters an abandoned turn.
func (e *ThrottleGiveUpError) NonRetryable() bool {
	return true
}

var _ NonRetryableError = (*ThrottleGiveUpError)(nil)

// AsThrottleGiveUpError returns the *ThrottleGiveUpError in err's chain,
// mirroring AsThrottleBackoffError.
func AsThrottleGiveUpError(err error) (*ThrottleGiveUpError, bool) {
	if err == nil {
		return nil, false
	}
	return errors.AsType[*ThrottleGiveUpError](err)
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
	Type          string                `json:"type"`
	Code          string                `json:"code"`
	Message       string                `json:"message"`
	RetryAfter    float64               `json:"retry_after"`
	RetryStrategy *openRouterInnerRetry `json:"retry_strategy"`
	Retriable     bool                  `json:"retriable"`
	Context       *openRouterInnerCtx   `json:"context"`
}

type openRouterInnerRetry struct {
	Type              string  `json:"type"`
	SuggestedInitialS float64 `json:"suggested_initial_delay_s"`
	MaxDelayS         float64 `json:"max_delay_s"`
	Backoff           string  `json:"backoff"`
	BackoffBase       float64 `json:"backoff_base"`
	Jitter            bool    `json:"jitter"`
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
// The json.Unmarshal errors are intentional nils: an unparseable body simply
// doesn't match the format this parser probes for (callers fall through to
// the next parser in the chain), so the parse error itself is not a failure
// worth surfacing.
func ParseOpenRouterError(body []byte) *ProviderError {
	// Try parsing outer envelope
	var outer openRouterOuter
	if err := json.Unmarshal(body, &outer); err != nil || outer.Error == nil || outer.Error.Message == "" {
		_ = err
		return nil //nolint:nilerr // probe parser: unparseable body = format mismatch, by contract
	}

	msg := outer.Error.Message

	// Look for the inner JSON pattern: after "429): " or "): {"
	innerJSON := extractInnerJSON(msg)
	if innerJSON == "" {
		return nil //nolint:nilerr // not the expected OpenRouter shape; caller probes the next parser
	}

	var inner openRouterInner
	if err := json.Unmarshal([]byte(innerJSON), &inner); err != nil {
		return nil //nolint:nilerr // inner body isn't JSON — format mismatch, by contract
	}

	detail := &ProviderError{
		Type:       inner.Error.Type,
		Code:       inner.Error.Code,
		Message:    inner.Error.Message,
		Retriable:  inner.Error.Retriable,
		RetryAfter: time.Duration(inner.Error.RetryAfter * float64(time.Second)),
	}

	if inner.Error.RetryStrategy != nil {
		detail.RetryStrategy = &RetryStrategy{
			Type:         inner.Error.RetryStrategy.Type,
			InitialDelay: time.Duration(inner.Error.RetryStrategy.SuggestedInitialS * float64(time.Second)),
			MaxDelay:     time.Duration(inner.Error.RetryStrategy.MaxDelayS * float64(time.Second)),
			Backoff:      inner.Error.RetryStrategy.Backoff,
			BackoffBase:  inner.Error.RetryStrategy.BackoffBase,
			UseJitter:    inner.Error.RetryStrategy.Jitter,
		}
	}

	if inner.Error.Context != nil {
		detail.LimitBudget = &LimitBudget{
			Used:   inner.Error.Context.TPMWindowToks,
			Limit:  inner.Error.Context.TPMLimit,
			Window: inner.Error.Context.LimitType,
		}
		// Use context limit_type for strategy type if retry_strategy was also parsed
		if inner.Error.Context.LimitType != "" && detail.RetryStrategy != nil {
			detail.RetryStrategy.Type = inner.Error.Context.LimitType
		}
	}

	return detail
}

// ParseGenericProviderError tries to parse a generic {error:{type,message,code}} JSON body.
// Returns nil if the body does not match this format. The json.Unmarshal
// error is an intentional nil: an unparseable body is a format mismatch
// (the caller probes the next parser), not a failure to surface.
func ParseGenericProviderError(body []byte) *ProviderError {
	var parsed genericProviderError
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == nil {
		_ = err
		return nil //nolint:nilerr // probe parser: unparseable body = format mismatch, by contract
	}

	if parsed.Error.Type == "" && parsed.Error.Code == "" && parsed.Error.Message == "" {
		return nil
	}

	return &ProviderError{
		Type:    parsed.Error.Type,
		Code:    parsed.Error.Code,
		Message: parsed.Error.Message,
	}
}

// ParseRateLimitBody attempts to parse a 429 response body into a ProviderError.
// It tries OpenRouter format first, then generic JSON, and falls back to nil.
func ParseRateLimitBody(body []byte) *ProviderError {
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
		// Full jitter: uniform random in [0, delay]. math/rand/v2 is the
		// right generator here — jitter only spreads retry timing to avoid
		// thundering herds; it has no security requirement (G404).
		return time.Duration(rand.Int64N(int64(delay) + 1)) //nolint:gosec // G404: retry jitter, not security-sensitive
	}
	return delay
}

// ClassificationFailureKind categorizes why LLM classification failed.
type ClassificationFailureKind string

const (
	ClassificationFailureEmptyResponse ClassificationFailureKind = "empty_response"
	ClassificationFailureUnavailable   ClassificationFailureKind = "model_unavailable"
	ClassificationFailureBudget        ClassificationFailureKind = "budget_exhausted"
	ClassificationFailureTimeout       ClassificationFailureKind = "timeout"
	ClassificationFailureUnknown       ClassificationFailureKind = "unknown"
)

// ClassifyClassificationFailure determines the failure kind from an error.
// Uses errors.As/Is for structured error types; falls back to substring
// matching only for errors that lack a structured type (e.g. empty response).
func ClassifyClassificationFailure(err error) ClassificationFailureKind {
	if err == nil {
		return ClassificationFailureUnknown
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClassificationFailureTimeout
	}
	if _, ok := errors.AsType[*BudgetExceededError](err); ok {
		return ClassificationFailureBudget
	}
	if _, ok := errors.AsType[*CapabilityError](err); ok {
		return ClassificationFailureUnavailable
	}
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		switch {
		case apiErr.StatusCode == 429:
			return ClassificationFailureUnavailable // rate limited
		case apiErr.StatusCode >= 500:
			return ClassificationFailureUnavailable
		}
	}
	// EmptyResponse is still string-based because no structured type exists.
	// See follow-up: define *EmptyResponseError.
	msg := err.Error()
	if strings.Contains(msg, "no choices in response") || strings.Contains(msg, "empty content") {
		return ClassificationFailureEmptyResponse
	}
	return ClassificationFailureUnknown
}

// ClassificationUserGuidance returns a user-friendly message explaining
// why classification failed and what the user can do about it.
func ClassificationUserGuidance(err error) string {
	kind := ClassifyClassificationFailure(err)
	switch kind {
	case ClassificationFailureEmptyResponse:
		return "The AI model returned an empty response. Please try rephrasing your message."
	case ClassificationFailureUnavailable:
		return "No models are currently available for this task. Check your model configuration or try again later."
	case ClassificationFailureBudget:
		return "The budget for this session has been exhausted. Consider increasing the budget or starting a new session."
	case ClassificationFailureTimeout:
		return "The request timed out while waiting for the AI to respond. Try again with a shorter message or simpler task."
	default:
		return "An unexpected error occurred during processing. Please try again."
	}
}
