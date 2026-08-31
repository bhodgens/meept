package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// QuotaResetError is a recoverable-by-waiting provider cap: a subscription
// usage window, plan quota, or billing exhaustion. Distinct from
// RateLimitError (seconds-scale backoff). The client retry loop must NOT
// short-retry it; the broker treats it as rotate-and-block.
type QuotaResetError struct {
	ProviderID string
	ModelID    string
	Code       string        // "usage_limit_reached", "insufficient_quota", "quota_exceeded", etc.
	Message    string        // raw body detail, truncated to 500 chars
	ResetAt    time.Time     // absolute reset time; zero = unknown
	RetryAfter time.Duration // derived wait; min(ResetAt-now, MaxWait) when known
	MaxWait    time.Duration // upper bound from config (default 24h)
	StatusCode int           // 429 or 402
	Cause      error
}

func (e *QuotaResetError) Error() string {
	msg := fmt.Sprintf("quota limit exceeded: provider=%s model=%s code=%s", e.ProviderID, e.ModelID, e.Code)
	if !e.ResetAt.IsZero() {
		msg += fmt.Sprintf(" resets_at=%s", e.ResetAt.Format(time.RFC3339))
	}
	if e.Cause != nil {
		msg += fmt.Sprintf(": %v", e.Cause)
	}
	return msg
}

func (e *QuotaResetError) UserMessage() string {
	parts := []string{"quota limit reached"}
	if e.ModelID != "" {
		parts = append(parts, fmt.Sprintf("on %s", e.ModelID))
	}
	if !e.ResetAt.IsZero() {
		wait := time.Until(e.ResetAt)
		if wait > 0 && wait < e.MaxWait {
			parts = append(parts, fmt.Sprintf("resets in %s", formatDuration(wait)))
		} else if !e.ResetAt.IsZero() {
			parts = append(parts, fmt.Sprintf("resets at %s", e.ResetAt.Format("15:04")))
		}
	}
	if e.Message != "" {
		msg := e.Message
		if len(msg) > 100 {
			msg = msg[:100] + "..."
		}
		parts = append(parts, msg)
	}
	return strings.Join(parts, " ")
}

func (e *QuotaResetError) Unwrap() error {
	return e.Cause
}

// NonRetryable returns true so the client short-retry loop exits immediately.
func (e *QuotaResetError) NonRetryable() bool {
	return true
}

var _ NonRetryableError = (*QuotaResetError)(nil)

// QuotaBlockStatus is a snapshot of an active quota block for broker/query APIs.
type QuotaBlockStatus struct {
	ProviderID    string
	ModelID       string
	CredentialKey string
	Code          string
	ResetAt       time.Time
	Remaining     time.Duration // time until reset; zero if unknown
}

// IsQuotaResetError returns true if err is (or wraps) a QuotaResetError.
func IsQuotaResetError(err error) bool {
	if err == nil {
		return false
	}
	var qe *QuotaResetError
	return errors.As(err, &qe)
}

func AsQuotaResetError(err error) (*QuotaResetError, bool) {
	if err == nil {
		return nil, false
	}
	var qe *QuotaResetError
	if errors.As(err, &qe) {
		return qe, true
	}
	return nil, false
}

// QuotaContext carries metadata for parsing quota responses.
type QuotaContext struct {
	ProviderID string
	ModelID    string
	MaxWait    time.Duration
}

// formatDuration formats a duration for user-facing display. Values are
// truncated to the minute (never rounded up): a 90s wait shows as "1m", so
// the user is never told more time remains than actually does.
func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Minute)
	if d >= time.Hour {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// ParseQuotaResponse classifies an HTTP error response as a quota reset.
// statusCode must be 429 or 402; anything else returns nil.
// Precedence: structured body fields -> rate-limit headers -> nil.
// Unknown reset time => zero ResetAt (caller applies config default estimate).
func ParseQuotaResponse(statusCode int, header http.Header, body []byte, known QuotaContext) *QuotaResetError {
	if statusCode != http.StatusTooManyRequests && statusCode != http.StatusPaymentRequired {
		return nil
	}

	e := &QuotaResetError{
		ProviderID: known.ProviderID,
		ModelID:    known.ModelID,
		StatusCode: statusCode,
		MaxWait:    known.MaxWait,
	}

	// Try structured body parsing first
	if len(body) > 0 {
		if parsed := parseQuotaBody(body); parsed != nil {
			e.Code = parsed.Code
			e.Message = parsed.Message
			if !parsed.ResetAt.IsZero() {
				e.ResetAt = parsed.ResetAt
			}
		}
	}

	// Fall back to headers if body didn't provide reset time
	if e.ResetAt.IsZero() {
		if resetAt := parseQuotaResetHeader(header); !resetAt.IsZero() {
			e.ResetAt = resetAt
		}
	}

	// Compute RetryAfter
	if !e.ResetAt.IsZero() && e.MaxWait > 0 {
		wait := time.Until(e.ResetAt)
		if wait > 0 {
			if wait > e.MaxWait {
				wait = e.MaxWait
			}
			e.RetryAfter = wait
		}
	}

	// If we have a Code from body or can infer from status, keep it
	if e.Code == "" {
		if statusCode == http.StatusPaymentRequired {
			e.Code = "payment_required"
		} else {
			e.Code = "quota_exceeded"
		}
	}

	// Truncate message
	if len(e.Message) > 500 {
		e.Message = e.Message[:500]
	}

	return e
}

// quotaBody holds parsed structured quota fields.
type quotaBody struct {
	Code    string
	Message string
	ResetAt time.Time
}

func parseQuotaBody(body []byte) *quotaBody {
	// Try direct JSON first
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		// Try extracting inner JSON (OpenRouter pattern)
		msg := string(body)
		if inner := extractInnerJSON(msg); inner != "" {
			if err := json.Unmarshal([]byte(inner), &raw); err != nil {
				return nil
			}
		} else {
			return nil
		}
	}

	b := &quotaBody{}

	// OpenAI/Codex shape: {"error":{"type":"usage_limit_reached","resets_at":...}}
	if errObj, ok := raw["error"].(map[string]any); ok {
		if typ, ok := errObj["type"].(string); ok {
			b.Code = typ
		}
		if code, ok := errObj["code"].(string); ok && b.Code == "" {
			b.Code = code
		}
		if msg, ok := errObj["message"].(string); ok {
			b.Message = msg
		}
		// resets_at can be unix seconds (float or int)
		if resetsAt, ok := errObj["resets_at"].(float64); ok {
			b.ResetAt = time.Unix(int64(resetsAt), 0)
		} else if resetsAtStr, ok := errObj["resets_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, resetsAtStr); err == nil {
				b.ResetAt = t
			}
		}
		return b
	}

	// Gemini/Anthropic nested error shape
	if errObj, ok := raw["error"].(map[string]any); ok {
		if errMsg, ok := errObj["message"].(string); ok && b.Message == "" {
			b.Message = errMsg
		}
		if errType, ok := errObj["type"].(string); ok && b.Code == "" {
			b.Code = errType
		}
		// Check for quota_exceeded type
		if b.Code == "rate_limit_error" && strings.Contains(b.Message, "quota") {
			b.Code = "quota_exceeded"
		}
		return b
	}

	// Flat shape with code field (Tencent)
	if code, ok := raw["code"].(float64); ok {
		b.Code = fmt.Sprintf("%.0f", code)
	} else if code, ok := raw["code"].(string); ok {
		b.Code = code
	}
	if msg, ok := raw["message"].(string); ok {
		b.Message = msg
	}
	if status, ok := raw["status"].(string); ok && b.Code == "" {
		b.Code = strings.ToLower(status)
	}

	return b
}

func parseQuotaResetHeader(header http.Header) time.Time {
	// Try standard Retry-After as date
	if retryAfter := header.Get("Retry-After"); retryAfter != "" {
		// Try as seconds first
		if dur, err := parseRetryAfterSeconds(retryAfter); err == nil && dur > 0 {
			return time.Now().Add(dur)
		}
		// Try as date
		for _, format := range []string{time.RFC1123, time.RFC3339} {
			if t, err := time.Parse(format, retryAfter); err == nil {
				return t
			}
		}
	}

	// Anthropic ratelimit headers
	for _, h := range []string{
		"anthropic-ratelimit-tokens-reset",
		"anthropic-ratelimit-requests-reset",
		"anthropic-ratelimit-concurrent-reset",
	} {
		if v := header.Get(h); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t
			}
		}
	}

	// Codex headers
	if v := header.Get("X-Codex-Primary-Reset-At"); v != "" {
		if secs, err := parseRetryAfterSeconds(v); err == nil && secs > 0 {
			return time.Now().Add(secs)
		}
	}
	if v := header.Get("X-Codex-Primary-Reset-After-Seconds"); v != "" {
		if secs, err := parseRetryAfterSeconds(v); err == nil && secs > 0 {
			return time.Now().Add(secs)
		}
	}

	return time.Time{}
}

// QuotaCredentialKey returns a stable identity for a provider credential:
//
//	literal apiKey  -> providerID + ":key:" + first 12 hex of sha256(apiKey)
//	env-based key   -> providerID + ":env:" + envVarName
//	OAuth provider  -> providerID + ":oauth:" + OAuthProvider
//	nothing identifiable -> providerID + ":default"
func QuotaCredentialKey(providerID string, cfg *ModelConfig) string {
	switch {
	case cfg.OAuthProvider != "":
		return fmt.Sprintf("%s:oauth:%s", providerID, cfg.OAuthProvider)
	case cfg.APIKey != "":
		hash := sha256.Sum256([]byte(cfg.APIKey))
		return fmt.Sprintf("%s:key:%s", providerID, hex.EncodeToString(hash[:])[:12])
	default:
		return fmt.Sprintf("%s:default", providerID)
	}
}
