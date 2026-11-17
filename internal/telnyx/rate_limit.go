// Typed rate-limit error for the Telnyx messaging send path
// (telnyx-texting plan, master.md §3 "Send hardening", §R2).
//
// Ported from strategist-usa pkg/services/telnyx_rate_limit.go:29-80
// (owner's own repo, no third-party license; adaptations per the plan's
// provenance note: zap->slog not needed here — the type is pure data,
// the clock is injected via Client.clock instead of wall time.Until).
package telnyx

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// RateLimitError is returned by SendSMS2 when the Telnyx API responds
// with HTTP 429 (Too Many Requests). Callers detect it via errors.As and
// apply the suggested backoff before retrying, rather than burning
// generic exponential retries on a server-provided window.
//
// Fields (shape FROZEN by the 00T gate, rate_limit_test.go):
//   - RetryAfter: duration parsed from the Retry-After header — delta
//     seconds ("30") or HTTP-date (resolved against the client clock;
//     past dates clamp to 0, never negative). Zero when the header is
//     absent or unparseable — callers then fall back to their own
//     exponential backoff.
//   - Remaining: per-window remaining requests from the budget headers;
//     -1 when both are absent.
type RateLimitError struct {
	RetryAfter time.Duration // parsed Retry-After (secs OR HTTP-date)
	Remaining  int           // X-RateLimit-Remaining; -1 when absent
}

// Error carries the pinned prefix "telnyx rate limited" (exact, so log
// greps/panels on the send leg stay stable across the port) plus the
// parsed window for ops context.
func (e *RateLimitError) Error() string {
	return fmt.Sprintf("telnyx rate limited: retry_after=%s remaining=%d", e.RetryAfter, e.Remaining)
}

// newRateLimitError builds a *RateLimitError from the response headers —
// ported from strategist-usa pkg/services/telnyx_rate_limit.go:47-80:
// the HTTP-date branch is adjusted to use the injected clock (clock())
// instead of time.Until per the gate's pinned clock seam; everything
// else is the port's precedence.
//
// Header precedence for remaining (port :61-69):
//
//	X-RateLimit-Remaining (legacy Telnyx) > RateLimit-Remaining (RFC 9331) > -1
//
// Retry-After (port :71-81) supports both delta-seconds and HTTP-date
// per RFC 7231 §7.1.3; the HTTP-date branch keeps zero on parse failure
// or a past date so the caller can apply its own exponential backoff.
func newRateLimitError(h http.Header, clock func() time.Time) *RateLimitError {
	err := &RateLimitError{Remaining: -1}

	if v := h.Get("X-RateLimit-Remaining"); v != "" {
		if n, parseErr := strconv.Atoi(v); parseErr == nil {
			err.Remaining = n
		}
	} else if v := h.Get("RateLimit-Remaining"); v != "" {
		if n, parseErr := strconv.Atoi(v); parseErr == nil {
			err.Remaining = n
		}
	}

	if v := h.Get("Retry-After"); v != "" {
		// Try delta-seconds first (most common from Telnyx); the port
		// guards secs >= 0 (:73) — a negative "-5" is not a valid wait.
		if secs, parseErr := strconv.Atoi(v); parseErr == nil && secs >= 0 {
			err.RetryAfter = time.Duration(secs) * time.Second
		} else if t, parseErr := http.ParseTime(v); parseErr == nil {
			// HTTP-date form, resolved against the injected clock
			// (port :75-79, time.Until -> date.Sub(clock())); only
			// assign when the delta is positive — a negative backoff
			// sleeps nowhere.
			now := time.Now()
			if clock != nil {
				now = clock()
			}
			if d := t.Sub(now); d > 0 {
				err.RetryAfter = d
			}
		}
	}

	return err
}
