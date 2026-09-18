package llm

import (
	"errors"
	"fmt"
	"strings"
)

// refusalReasonStopReason and refusalReasonFinishReason are the pinned
// signal strings DetectRefusal matches (case-insensitive): Anthropic's
// stop_reason:"refusal" and the OpenAI-compatible finish_reason:
// "content_filter".
const (
	refusalReasonStopReason   = "refusal"
	refusalReasonFinishReason = "content_filter"
)

// refusalBodyMarkers are the CONSERVATIVE error-body markers
// DetectRefusalFromBody matches (case-insensitive substring). Deliberately
// narrow: broad keywords like "policy" alone must never classify a refusal.
var refusalBodyMarkers = []string{
	"safeguards flagged",
	"content policy violation",
	"flagged this message",
	`"content_filter"`, // content_filter as an error-code key
}

// refusalMessageMaxLen caps the provider detail carried on RefusalError.
const refusalMessageMaxLen = 500

// RefusalError is a typed model refusal: the provider (or its safety layer)
// declined to answer. Like QuotaResetError it is NonRetryable — the client
// short-retry loops must exit immediately; surfacing/fallback is the
// caller's decision (plan refusal-fallback).
type RefusalError struct {
	ProviderID   string
	ModelID      string
	Source       string // "finish_reason" | "stop_reason" | "error_body"
	FinishReason string // raw signal: "refusal", "content_filter", or ""
	Message      string // provider detail, truncated to 500 chars
	StatusCode   int    // HTTP status when from an error body; 0 otherwise
	// Usage carries the token usage the provider reported for the refused
	// call (bughunt F12). A refusal still consumed prompt (+ often output)
	// tokens — budget accounting and the metrics.db llm_calls ledger must
	// record them instead of silently dropping the row. Zero value when the
	// provider reported no usage.
	Usage TokenUsage
	Cause error
}

func (e *RefusalError) Error() string {
	reason := e.FinishReason
	if reason == "" {
		reason = "unknown"
	}
	msg := fmt.Sprintf("model refusal: provider=%s model=%s source=%s reason=%s", e.ProviderID, e.ModelID, e.Source, reason)
	if e.Cause != nil {
		msg += fmt.Sprintf(": %v", e.Cause)
	}
	return msg
}

func (e *RefusalError) Unwrap() error {
	return e.Cause
}

// NonRetryable returns true so the client short-retry loops exit immediately.
func (e *RefusalError) NonRetryable() bool {
	return true
}

var _ NonRetryableError = (*RefusalError)(nil)

// isRefusalError reports whether err is (or wraps) a *RefusalError. The
// ProviderManager failover switches use it to keep refusals OUT of the
// health-failure path (bughunt F13): a refusal is the provider's safety
// layer declining, not a provider health problem — no recordFailure, no
// rotation; the agent loop's one-hop fallback policy owns it.
func isRefusalError(err error) bool {
	if err == nil {
		return false
	}
	var re *RefusalError
	return errors.As(err, &re)
}

// truncateRefusalMessage caps the stored provider detail at
// refusalMessageMaxLen chars (mirrors errors_quota.go).
func truncateRefusalMessage(msg string) string {
	if len(msg) > refusalMessageMaxLen {
		return msg[:refusalMessageMaxLen]
	}
	return msg
}

// DetectRefusal maps a finish/stop reason signal onto a *RefusalError.
// Triggers, exact strings, case-insensitive:
//   - "refusal"        => Source "stop_reason"   (Anthropic)
//   - "content_filter" => Source "finish_reason" (OpenAI-compatible)
//
// Anything else (including "") returns nil. Empty modelID is allowed
// (streaming paths may not have it); fill from context when known. The
// provider-reported usage rides along (bughunt F12): a refused call still
// consumed tokens and the usage ledger must record them.
func DetectRefusal(providerID, modelID, finishReason string) *RefusalError {
	switch {
	case strings.EqualFold(finishReason, refusalReasonStopReason):
		return &RefusalError{
			ProviderID:   providerID,
			ModelID:      modelID,
			Source:       "stop_reason",
			FinishReason: finishReason,
		}
	case strings.EqualFold(finishReason, refusalReasonFinishReason):
		return &RefusalError{
			ProviderID:   providerID,
			ModelID:      modelID,
			Source:       "finish_reason",
			FinishReason: finishReason,
		}
	default:
		return nil
	}
}

// AttachUsageToRefusal stamps the provider-reported usage onto a refusal
// error (bughunt F12). Callers record the usage row (recordUsageStore /
// metrics) BEFORE returning the refusal so a refused call still bills its
// consumed tokens; this helper keeps the error itself truthful when a parse
// site has usage in hand but records separately. Nil refusal is a no-op.
func AttachUsageToRefusal(refusal *RefusalError, usage TokenUsage) {
	if refusal != nil {
		refusal.Usage = usage
	}
}

// DetectRefusalFromBody classifies typed safeguard/refusal error-body text
// conservatively. Match => *RefusalError with Source "error_body",
// StatusCode set, and Message carrying the body truncated to 500 chars.
// No match => nil.
func DetectRefusalFromBody(providerID, modelID string, statusCode int, body string) *RefusalError {
	if body == "" {
		return nil
	}
	lower := strings.ToLower(body)
	for _, marker := range refusalBodyMarkers {
		if strings.Contains(lower, marker) {
			return &RefusalError{
				ProviderID: providerID,
				ModelID:    modelID,
				Source:     "error_body",
				Message:    truncateRefusalMessage(body),
				StatusCode: statusCode,
			}
		}
	}
	return nil
}
