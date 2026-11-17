package telnyx

// ============================================================================
// 00T-send gate (§R2) — RateLimitError parsing on the SendSMS2 path.
// Ported request-capture semantics from strategist-usa
// pkg/services/telnyx_rate_limit.go:29-80 + telnyx_provider.go:307-318
// (429 -> typed rate-limit error) and :340-353 (>=400 -> status-code
// error, body snippet truncated for PII hygiene).
//
// Pinned shape (master.md §3 "Send hardening", FROZEN; task pins add the
// Remaining field the port reads from the budget headers):
//
//	type RateLimitError struct {
//	    RetryAfter time.Duration // parsed Retry-After (secs OR HTTP-date)
//	    Remaining  int           // X-RateLimit-Remaining; -1 when absent
//	}
//	func (*RateLimitError) Error() string // pinned exact prefix: "telnyx rate limited"
//
// Surfaced on HTTP 429 via errors.As on the error returned by
// SendSMS2 (the impl may wrap it; errors.As must still find the
// *RateLimitError — never fmt.Sprintf the text into existence).
//
// Pinned clock seam (task: HTTP-date form needs an injectable clock):
// Option WithMessagingClock(func() time.Time) on the messaging Client —
// mirrors SMSAttemptConfig.Clock. Used to resolve the HTTP-date branch
// (RetryAfter = date - clock()); delta-seconds needs no clock.
//
// Retry-After branch table (port, telnyx_rate_limit.go:71-81):
//   - "NN" (delta-seconds, >= 0)          -> NNs
//   - HTTP-date (RFC1123, http.ParseTime) -> date.Sub(clock()); PAST
//     dates clamp to 0, never negative (a negative backoff sleeps nowhere)
//   - absent or unparseable garbage       -> 0 (caller falls back to its
//     own exponential backoff)
// Remaining precedence (:61-69): X-RateLimit-Remaining (legacy Telnyx)
// > RateLimit-Remaining (RFC 9331) > -1 when both absent.
// ============================================================================

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// rateBase is the fixed clock used for HTTP-date assertions — the
// gate pins RetryAfter as (header date - clock), never wall-now.
var rateBase = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func rateClock() time.Time { return rateBase }

// newRateClient dials the send2 stub with a canned error response and
// the pinned injectable clock. respCode/respHdr carry the 429 shape.
func newRateClient(t *testing.T, respCode int, respHdr map[string]string) (*Client, *send2Stub) {
	t.Helper()
	stub := &send2Stub{
		respCode: respCode,
		respBody: `{"errors":[{"code":"ONE_TOO_MANY_REQUESTS","description":"slow down"}]}`,
		respHdr:  respHdr,
	}
	c := newSend2Client(t, stub, WithMessagingClock(rateClock))
	return c, stub
}

func rateReq() SendRequest {
	return SendRequest{To: "+15555510099", Text: "rate limited probe"}
}

// asRateLimit runs SendSMS2 against a stub and extracts the
// *RateLimitError the caller must be able to errors.As out of err.
func asRateLimit(t *testing.T, respCode int, hdr map[string]string) (*RateLimitError, error) {
	t.Helper()
	c, _ := newRateClient(t, respCode, hdr)
	_, err := c.SendSMS2(context.Background(), rateReq())
	if err == nil {
		t.Fatalf("SendSMS2 against %d must error", respCode)
	}
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("errors.As(*RateLimitError) failed on 429; err = %v", err)
	}
	return rle, err
}

// §R2 — delta-seconds form (the common Telnyx shape): "Retry-After: 30"
// -> RetryAfter == 30s. The string "0" must map to exactly 0 (no header
// fallback confusion), and a negative "-5" is NOT a valid delta-seconds
// per the port (secs >= 0 guard, :73) -> 0.
func TestSendSMS2RateLimitDeltaSeconds(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"30", 30 * time.Second},
		{"1", 1 * time.Second},
		{"0", 0},
		{"-5", 0}, // port guard: secs >= 0 only
	} {
		rle, err := asRateLimit(t, http.StatusTooManyRequests, map[string]string{"Retry-After": tc.header})
		if rle.RetryAfter != tc.want {
			t.Errorf("Retry-After %q: RetryAfter = %v, want %v (err=%v)", tc.header, rle.RetryAfter, tc.want, err)
		}
	}
}

// §R2 — HTTP-date form with the injected clock (port :75-79, adjusted to
// clock(): 45s ahead of rateBase -> exactly 45s; a PAST date clamps to 0
// (port only assigns when d > 0); an unparseable value leaves 0 so the
// caller applies its own backoff.
func TestSendSMS2RateLimitHTTPDate(t *testing.T) {
	future := rateBase.Add(45 * time.Second).UTC().Format(http.TimeFormat)
	rle, _ := asRateLimit(t, http.StatusTooManyRequests, map[string]string{"Retry-After": future})
	if rle.RetryAfter != 45*time.Second {
		t.Errorf("Retry-After %q with fixed clock: RetryAfter = %v, want 45s", future, rle.RetryAfter)
	}

	past := rateBase.Add(-10 * time.Second).UTC().Format(http.TimeFormat)
	rle2, _ := asRateLimit(t, http.StatusTooManyRequests, map[string]string{"Retry-After": past})
	if rle2.RetryAfter != 0 {
		t.Errorf("past HTTP-date %q: RetryAfter = %v, want clamped 0 (never negative)", past, rle2.RetryAfter)
	}

	junk := "sometime-next-tuesday"
	rle3, _ := asRateLimit(t, http.StatusTooManyRequests, map[string]string{"Retry-After": junk})
	if rle3.RetryAfter != 0 {
		t.Errorf("garbage Retry-After %q: RetryAfter = %v, want 0 fallback", junk, rle3.RetryAfter)
	}
}

// §R2 — absent Retry-After -> zero duration (caller falls back to
// exponential backoff; a fabricated default wait would be a contract
// invention).
func TestSendSMS2RateLimitNoRetryAfterHeader(t *testing.T) {
	rle, _ := asRateLimit(t, http.StatusTooManyRequests, nil)
	if rle.RetryAfter != 0 {
		t.Errorf("no Retry-After header: RetryAfter = %v, want 0", rle.RetryAfter)
	}
}

// §R2 — X-RateLimit-Remaining captured, precedence per the port:
// legacy X- header wins over RFC 9331 RateLimit-Remaining; "0" is a
// REAL value (not absent); -1 only when both headers are missing.
func TestSendSMS2RateLimitRemaining(t *testing.T) {
	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"x-header", map[string]string{"X-RateLimit-Remaining": "7"}, 7},
		{"x-zero", map[string]string{"X-RateLimit-Remaining": "0"}, 0},
		{"rfc9331-only", map[string]string{"RateLimit-Remaining": "3"}, 3},
		{"x-beats-rfc", map[string]string{"X-RateLimit-Remaining": "9", "RateLimit-Remaining": "4"}, 9},
		{"absent", map[string]string{"Retry-After": "5"}, -1},
	}
	for _, tc := range cases {
		rle, _ := asRateLimit(t, http.StatusTooManyRequests, tc.hdr)
		if rle.Remaining != tc.want {
			t.Errorf("%s: Remaining = %d, want %d", tc.name, rle.Remaining, tc.want)
		}
	}
}

// §R2 — Error() text: pinned exact prefix "telnyx rate limited" so log
// greps/panels on the send leg stay stable across the port.
func TestRateLimitErrorText(t *testing.T) {
	e := &RateLimitError{RetryAfter: 30 * time.Second, Remaining: 0}
	msg := e.Error()
	if !strings.HasPrefix(msg, "telnyx rate limited") {
		t.Errorf("RateLimitError.Error() = %q, want prefix %q", msg, "telnyx rate limited")
	}
	// Ops context: the parsed window must be visible in the text.
	if !strings.Contains(msg, "30s") {
		t.Errorf("Error() %q does not surface RetryAfter 30s", msg)
	}
}

// §R2 — non-429 >=400 errors: the message carries the STATUS CODE plus a
// body snippet TRUNCATED to 512 bytes (PII hygiene — strategist
// :340-353 never interpolates the full body). Window pinned with a
// ~2KB body and two markers: an IN marker starting at byte 500 must
// SURVIVE (proves the snippet is at least the first ~500B — a 64B stub
// would starve ops), an OUT marker starting at byte 512 must be ABSENT
// (proves the cut happened — no full-body PII leak). 402 (non-429) must
// NOT surface as RateLimitError.
func TestSendSMS2ErrorBodySnippetTruncated(t *testing.T) {
	const (
		head = "PII_HEAD"   // offset 0 — always inside any sane snippet
		in   = "IN500OK"    // offset 500 — inside the first 512B
		out  = "OUT512NOPE" // offset 512 — past the cap, must be cut
	)
	// Offsets are exact: head@0, in@500, out@512, total ~2KB.
	body := head +
		strings.Repeat("a", 500-len(head)) + in +
		strings.Repeat("b", 512-(500+len(in))) + out +
		strings.Repeat("c", 2048-512-len(out))

	stub := &send2Stub{
		respCode: http.StatusPaymentRequired,
		respBody: body,
	}
	client := newSend2Client(t, stub)
	_, err := client.SendSMS2(context.Background(), rateReq())
	if err == nil {
		t.Fatal("SendSMS2 against 402 must error")
	}
	msg := err.Error()

	if !strings.Contains(msg, "402") {
		t.Errorf("error %q lacks the status code 402", msg)
	}
	if !strings.Contains(msg, head) {
		t.Errorf("error snippet dropped the body head %q: %q", head, msg)
	}
	if !strings.Contains(msg, in) {
		t.Errorf("error snippet cut BEFORE the marker at offset 500 (%q present? no): snippet is under the pinned 512B window — ops loses the tail of the real error JSON", in)
	}
	if strings.Contains(msg, out) {
		t.Errorf("error carries the marker at offset 512 (%q) — body was NOT truncated to 512B (PII leak)", out)
	}
	if strings.Contains(msg, body) {
		t.Error("error carries the FULL 2KB body; snippet must be truncated")
	}
	// Non-429 is a plain error, not the typed rate-limit one — a worker
	// must not back off on a payment-required.
	var rle *RateLimitError
	if errors.As(err, &rle) {
		t.Errorf("402 surfaced as *RateLimitError; only 429 is rate limiting")
	}
}
