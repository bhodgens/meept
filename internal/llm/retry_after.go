package llm

import (
	"net/http"
	"strconv"
	"time"
)

// ParseRetryAfter extracts a retry schedule from an HTTP error response's
// headers in one place (DECISIONS.md D6: fully implement the RFC7231
// HTTP-date forms + the RFC7231/RFC3339 delta-seconds form; consolidation
// of the per-loop parseRetryAfterSeconds / parseQuotaResetHeader helpers).
//
// Returns:
//   - date: the parsed absolute instant for date-form headers and
//     date-valued provider headers (zero for delta-seconds headers);
//   - delta: the parsed relative duration for delta-seconds headers
//     (zero for date forms; time.Until(date) for date forms, which is
//     negative for past dates);
//   - present: whether any recognizable header was found.
//
// Callers compose the schedule as: retryAt = date when non-zero, else
// now+delta. A date in the past yields a negative delta — the caller
// clamps (leaf rule).
//
// Try order is spec order first — delta-seconds, IMF-fixdate, RFC850,
// asctime, then RFC3339 — followed by the provider-specific reset headers
// preserved verbatim from parseQuotaResetHeader
// (anthropic-ratelimit-*-reset, X-Codex-*). A standard Retry-After always
// beats a provider header.
func ParseRetryAfter(header http.Header) (date time.Time, delta time.Duration, present bool) {
	if header == nil {
		return time.Time{}, 0, false
	}

	// 1. Standard Retry-After: the RFC7231 forms in spec order (D6).
	// First match wins; a parse failure falls through to the next
	// candidate form by design (leap seconds etc. are out of scope).
	if v := header.Get("Retry-After"); v != "" {
		// delta-seconds is 1*DIGIT over the WHOLE value (RFC7231
		// §7.1.3). strconv is strict, unlike parseRetryAfterSeconds'
		// fmt.Sscanf, whose prefix match would misread an RFC3339 date
		// as a huge delta — buggy senders mix forms (leaf Goal 1).
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Time{}, time.Duration(secs) * time.Second, true
		}
		for _, format := range []string{
			time.RFC1123,               // IMF-fixdate: "Fri, 31 Dec 2027 23:59:59 GMT" (RFC7231 REQUIRED)
			time.RFC850,                // obsolete: "Friday, 31-Dec-27 23:59:59 GMT"
			"Mon Jan _2 15:04:05 2006", // obsolete asctime: "Fri Dec 31 23:59:59 2027"
			time.RFC3339,               // tolerated: provider implementations send RFC3339 dates
		} {
			if t, err := time.Parse(format, v); err == nil {
				return t, time.Until(t), true
			}
		}
	}

	// 2. Anthropic ratelimit headers (RFC3339 dates). Membership and
	// semantics preserved verbatim from parseQuotaResetHeader.
	for _, h := range []string{
		"anthropic-ratelimit-tokens-reset",
		"anthropic-ratelimit-requests-reset",
		"anthropic-ratelimit-concurrent-reset",
	} {
		if v := header.Get(h); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t, time.Until(t), true
			}
		}
	}

	// 3. Codex headers (delta seconds, same lax helper as
	// parseQuotaResetHeader — behavior preserved verbatim).
	if v := header.Get("X-Codex-Primary-Reset-At"); v != "" {
		if secs, err := parseRetryAfterSeconds(v); err == nil && secs > 0 {
			return time.Time{}, secs, true
		}
	}
	if v := header.Get("X-Codex-Primary-Reset-After-Seconds"); v != "" {
		if secs, err := parseRetryAfterSeconds(v); err == nil && secs > 0 {
			return time.Time{}, secs, true
		}
	}

	return time.Time{}, 0, false
}
