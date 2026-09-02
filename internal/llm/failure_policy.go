package llm

import (
	"net/http"
	"strings"
	"time"
)

// FailurePolicyConfig mirrors the canonical config.FailurePolicyConfig
// (llm.failure_policy knobs, tree 02 leaf 02). Defined locally to avoid the
// internal/llm -> internal/config import cycle (internal/config transitively
// imports internal/llm); the canonical type satisfies this shape 1:1, and
// internal/config maps onto it the same way llm.ConfigFromSchema maps
// QuotaRetryConfig onto QuotaWaitConfig (resolver_direct.go precedent).
type FailurePolicyConfig struct {
	// Horizon is the give-up cap: DefaultBackoffPlan sets
	// GiveUpAt = now + Horizon (D8: 24h default; on cap the turn fails).
	Horizon time.Duration
	// BaseThrottle is the first-retry delay for FailureThrottle (D8 30s
	// default) and the fallback base for all other classes.
	BaseThrottle time.Duration
	// BaseQuota402Extra is added to BaseThrottle for FailureQuota (D5:
	// 402 quota waits start "longer by minutes" than the 429 path; 5m).
	BaseQuota402Extra time.Duration
	// PollFloor is the polling floor: once an exponential step would
	// exceed it, all subsequent steps are exactly PollFloor (D8: 1h).
	PollFloor time.Duration
	// ShortRetries is the bounded immediate-retry budget for 5xx in the
	// short loops (D8 leaf-03 consumption; carried on the schema here so
	// leaf 03 only reads it).
	ShortRetries int
	// Pacing gating knobs (D15; tree 02 leaf 05 consumption — carried on
	// the schema here so leaf 05's PacingConfig maps onto it 1:1).
	PacingEnabled          bool
	PacingTarget429PerHour int
	PacingMinInterval      time.Duration
	PacingMaxInterval      time.Duration
}

// BackoffPlan converts a failure class + attempt count into deterministic
// attempt times (tree 02 Contract 2, DECISIONS.md D8). Pure value type;
// injected now at every call; NO jitter — jitter stays at the call sites'
// short sleeps so long-horizon polling is exact hourly.
type BackoffPlan struct {
	Base     time.Duration // first retry delay (throttle 30s; 402 = +5m, D5)
	Max      time.Duration // polling floor once reached (default 1h)
	GiveUpAt time.Time     // now + Horizon (default 24h, llm.failure_policy.horizon)
}

// DefaultBackoffPlan builds the schedule for a failure class at time now
// from cfg. Base selection per DECISIONS.md: FailureQuota adds
// BaseQuota402Extra to BaseThrottle (D5 — 402 quota waits start minutes
// longer than the 429 path); every other class uses BaseThrottle directly.
func DefaultBackoffPlan(class FailureClass, now time.Time, cfg FailurePolicyConfig) BackoffPlan {
	base := cfg.BaseThrottle
	if class == FailureQuota {
		// D5: 402/quota starts with a retry timer longer by minutes.
		base += cfg.BaseQuota402Extra
	}
	return BackoffPlan{
		Base:     base,
		Max:      cfg.PollFloor,
		GiveUpAt: now.Add(cfg.Horizon),
	}
}

// NextAttempt returns the earliest time the next attempt may run:
//
//   - The computed exponential step for attempt (attempt=0 is the FIRST
//     retry): Base * 2^attempt, capped at Max — once a step would exceed
//     Max, the step is exactly Max (D8 polling floor, hourly, exact).
//   - prior (a server-provided retry time, zero = none) wins when it is
//     LATER than the computed step — never retry before the server says —
//     but is capped at GiveUpAt: never wait longer than the horizon (D8).
//   - The result never lands after GiveUpAt.
func (p BackoffPlan) NextAttempt(now time.Time, attempt int, prior time.Time) time.Time {
	if attempt < 0 {
		attempt = 0
	}

	// Exponential growth with an overflow-safe cap at Max: stop doubling
	// as soon as the step reaches Max, then pin every later attempt to
	// exactly Max (D8: hourly polling floor). Shifting only while step <
	// Max keeps 2^attempt from overflowing int/Duration on huge counts.
	step := p.Base
	if step < p.Max {
		for i := 0; i < attempt; i++ {
			step *= 2
			if step >= p.Max {
				step = p.Max
				break
			}
		}
	}
	if step > p.Max {
		// Base itself already exceeds Max (misconfig): clamp.
		step = p.Max
	}
	next := now.Add(step)

	// Honor the server when it demands a longer wait, but never let the
	// wait run past the horizon (D8 cap).
	if prior.After(next) {
		next = prior
	}
	if next.After(p.GiveUpAt) {
		next = p.GiveUpAt
	}
	return next
}

// ShouldGiveUp answers the SCHEDULE question only: has now reached
// GiveUpAt? (D8 cap.) The decision to surface a user-facing error belongs
// to callers — leaf 03's short loops and tree 03's parking layer.
func (p BackoffPlan) ShouldGiveUp(now time.Time) bool {
	return !now.Before(p.GiveUpAt)
}

// FailureClass buckets an LLM-provider error response into the failure-policy
// classes (SHARED-CONVENTIONS §4.1; DECISIONS.md D4: throttle and quota are
// different classes with different horizons under one handler).
type FailureClass int

const (
	FailureNone        FailureClass = iota // not a failure
	FailureThrottle                        // 429/503, provider load; long-horizon backoff (D7)
	FailureQuota                           // 429/402 with quota-shaped signal; park until reset
	FailureServerError                     // 5xx; bounded retry
	FailureFatal                           // 4xx except 402/429; no retry
)

// PolicyVerdict is the single decision returned for any provider error
// (SHARED-CONVENTIONS §4.1 Contract 1).
type PolicyVerdict struct {
	Class   FailureClass
	RetryAt time.Time // earliest next attempt; zero = no retry scheduled
	Park    bool      // true = park the turn, release the agent slot
	GiveUp  bool      // true = surface a user-facing failure now
	Reason  string    // machine-readable
}

// Frozen D7 quota-signal keyword list (DECISIONS.md D7, ratified via Q4:
// keywords scan header NAMES and body text; structured body codes are
// handled separately by parseQuotaBody). Extending this list requires a
// DECISIONS.md amendment — do not grow it in place.
var (
	// quotaHeaderKeywords match header NAMES, case-insensitive substrings.
	quotaHeaderKeywords = []string{"quota", "usage"}

	// quotaBodyKeywords match lowercased body text, case-insensitive substrings.
	quotaBodyKeywords = []string{
		"quota",
		"usage limit",
		"usage_limit",
		"plan limit",
		"rate plan",
		"subscription limit",
	}
)

// Classify is the ONE entry point all clients call for any non-2xx response
// or transport error (DECISIONS.md D4). It generalizes the frozen
// classifyQuotaDecision (errors_quota.go, whose tests stay green and whose
// call sites leaf 03 rewires) with the D7 keyword buckets.
//
// Semantics:
//   - 402 is ALWAYS quota (D5), reason "status_402".
//   - 429 with a quota signal (structured body code, quota-window reset
//     header, or D7 keyword) is quota; a bare 429 WITHOUT any quota signal
//     is throttle so spurious provider-load 429s never inherit quota-length
//     delays (D7 core).
//   - 5xx is server error (bounded retry, not park); a 503 Retry-After is
//     schedule input for leaf 02, not class input.
//   - Other 4xx is fatal.
//   - Everything else (2xx, transport/redirect codes the caller already
//     handles) is none, with Reason "".
//
// Leaf scope: RetryAt is always zero, Park and GiveUp always false —
// schedules are tree 02 leaf 02's job (D8) and parking is tree 03's (D9).
// now is accepted for schedule symmetry with leaf 02 and future clock
// injection; classification itself is time-independent today.
func Classify(statusCode int, header http.Header, body []byte, now time.Time) PolicyVerdict {
	switch {
	case statusCode == http.StatusPaymentRequired:
		// D5: 402 is treated like quota exhaustion regardless of body.
		return PolicyVerdict{Class: FailureQuota, Reason: "status_402"}

	case statusCode == http.StatusTooManyRequests:
		if reason, ok := quotaSignal(header, body); ok {
			return PolicyVerdict{Class: FailureQuota, Reason: reason}
		}
		// D7 core: a 429 without any quota signal is provider-load
		// throttling, NEVER quota.
		return PolicyVerdict{Class: FailureThrottle, Reason: "throttle_no_quota_signal"}

	case statusCode >= 500:
		// All 5xx: bounded retry by the caller, never quota-parked.
		return PolicyVerdict{Class: FailureServerError, Reason: "server_error"}

	case statusCode >= 400:
		// Remaining 4xx: client/config problem, no retry.
		return PolicyVerdict{Class: FailureFatal, Reason: "client_error"}

	default:
		// 2xx plus transport/redirect classes (0, 1xx, 3xx): not a failure
		// for this policy.
		return PolicyVerdict{Class: FailureNone, Reason: ""}
	}
}

// quotaSignal reports whether a 429 response carries a quota-indicating
// signal (DECISIONS.md D7) and returns the frozen Reason naming it.
// Detection precedence: structured body shape, quota/usage header names
// (Q4), quota-window reset horizon, then body keyword text.
func quotaSignal(header http.Header, body []byte) (string, bool) {
	// 1. Structured quota shapes via the same parser and the same
	// non-empty condition the frozen classifyQuotaDecision uses, so the
	// parity corpus (usage_limit_reached, insufficient_quota, quota_exceeded,
	// payment_required, resets_at horizons) classifies identically.
	if qb := parseQuotaBody(body); qb != nil && (qb.Code != "" || !qb.ResetAt.IsZero()) {
		return "structured_quota_body", true
	}

	if header != nil {
		// 2. D7 keyword rule on header NAMES (Q4): any header whose name
		// contains "quota" or "usage" (case-insensitive).
		for name := range header {
			lower := strings.ToLower(name)
			for _, kw := range quotaHeaderKeywords {
				if strings.Contains(lower, kw) {
					return "quota_keyword_header", true
				}
			}
		}

		// 3. Quota-window reset horizon WITHOUT a keyword name: Codex
		// usage-window resets (same headers ParseQuotaResponse treats as
		// quota ResetAt). Deliberately NOT a signal:
		//   - Retry-After: it is the throttle schedule input, not a quota
		//     signal — a Retry-After alone must never bucket a 429 as quota
		//     (D7; leaf rule).
		//   - anthropic-ratelimit-*-reset: short-cycle rate-limit windows,
		//     not quota (leaf Notes).
		// parseQuotaResetHeader cannot be reused here because it treats
		// Retry-After and Anthropic headers as reset sources.
		for _, h := range []string{"X-Codex-Primary-Reset-At", "X-Codex-Primary-Reset-After-Seconds"} {
			if v := header.Get(h); v != "" {
				if secs, err := parseRetryAfterSeconds(v); err == nil && secs > 0 {
					return "quota_reset_header", true
				}
			}
		}
	}

	// 4. D7 keyword rule on body text (Q4).
	if len(body) > 0 {
		lower := strings.ToLower(string(body))
		for _, kw := range quotaBodyKeywords {
			if strings.Contains(lower, kw) {
				return "quota_keyword_body", true
			}
		}
	}

	return "", false
}
