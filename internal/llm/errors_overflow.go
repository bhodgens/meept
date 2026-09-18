package llm

import (
	"errors"
	"fmt"
	"strings"
)

// contextOverflowBodyMarkers are the CONSERVATIVE error-body markers
// DetectContextOverflowFromBody matches (case-insensitive substring),
// classified at the same body-scan sites DetectRefusalFromBody uses.
// Evidence (2026-09-18 e2e, llama.cpp): HTTP 500
// {"error":{"code":500,"message":"Context size has been exceeded.",
// "type":"server_error"}} — a request-size verdict, not a provider fault.
var contextOverflowBodyMarkers = []string{
	"context size has been exceeded", // llama.cpp / llama-server HTTP 500
	"context_length_exceeded",        // OpenAI error code
	"maximum context length",         // OpenAI / Anthropic prose
}

// contextOverflowMessageMaxLen caps the provider detail carried on
// ContextOverflowError (mirrors errors_refusal.go).
const contextOverflowMessageMaxLen = 500

// ContextOverflowError is a typed provider verdict that the request exceeded
// the model's context window. It is fundamentally different from a 5xx
// server fault: retrying the SAME payload is hopeless — the request only
// shrinks by trimming context. Like QuotaResetError/RefusalError it is
// NonRetryable, so every client short-retry loop must early-exit on it
// BEFORE the retryable-status checks; recovery (aggressive compaction +
// a single retry) is the agent loop's decision, not the transport's.
type ContextOverflowError struct {
	ProviderID string
	ModelID    string
	Message    string // raw body detail, truncated to 500 chars
	StatusCode int    // HTTP status reported by the provider (500/400/...)
	Cause      error
}

func (e *ContextOverflowError) Error() string {
	msg := fmt.Sprintf("context overflow: provider=%s model=%s status=%d", e.ProviderID, e.ModelID, e.StatusCode)
	if e.Cause != nil {
		msg += fmt.Sprintf(": %v", e.Cause)
	}
	return msg
}

func (e *ContextOverflowError) Unwrap() error {
	return e.Cause
}

// NonRetryable returns true so the client short-retry loops exit immediately.
func (e *ContextOverflowError) NonRetryable() bool {
	return true
}

var _ NonRetryableError = (*ContextOverflowError)(nil)

// IsContextOverflowError reports whether err is (or wraps) a
// *ContextOverflowError.
func IsContextOverflowError(err error) bool {
	_, ok := AsContextOverflowError(err)
	return ok
}

// AsContextOverflowError returns the *ContextOverflowError in err's chain,
// mirroring AsQuotaResetError (errors_quota.go).
func AsContextOverflowError(err error) (*ContextOverflowError, bool) {
	if err == nil {
		return nil, false
	}
	return errors.AsType[*ContextOverflowError](err)
}

// truncateContextOverflowMessage caps the stored provider detail at
// contextOverflowMessageMaxLen chars (mirrors errors_refusal.go).
func truncateContextOverflowMessage(msg string) string {
	if len(msg) > contextOverflowMessageMaxLen {
		return msg[:contextOverflowMessageMaxLen]
	}
	return msg
}

// DetectContextOverflowFromBody classifies a provider error body as a
// context overflow when it carries one of the conservative case-insensitive
// markers. Match => *ContextOverflowError with the status code and the
// truncated body detail; no match => nil. Called at the same non-OK
// body-scan sites as DetectRefusalFromBody, placed BEFORE the
// retryable-status-code classification so a 500 overflow body never
// becomes a retryable *APIError.
func DetectContextOverflowFromBody(providerID, modelID string, statusCode int, body string) *ContextOverflowError {
	if body == "" {
		return nil
	}
	lower := strings.ToLower(body)
	for _, marker := range contextOverflowBodyMarkers {
		if strings.Contains(lower, marker) {
			return &ContextOverflowError{
				ProviderID: providerID,
				ModelID:    modelID,
				Message:    truncateContextOverflowMessage(body),
				StatusCode: statusCode,
			}
		}
	}
	return nil
}

// isContextOverflowError is the package-internal alias used by the
// ProviderManager failover switches (mirrors isRefusalError).
func isContextOverflowError(err error) bool {
	return IsContextOverflowError(err)
}
