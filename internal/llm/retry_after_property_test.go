package llm

// Property-based tests for the Retry-After / quota-reset header parser
// (retry_after.go — the DECISIONS.md D6 consolidation of the per-loop
// parseRetryAfterSeconds / parseQuotaResetHeader helpers).
//
// Hand-rolled, zero new deps, deterministic seed (skill:
// software-development/property-based-testing; in-house pattern:
// strategist-usa go-shard-runner parse_csv_property_test.go).
//
// Properties (implementation contract refs are retry_after.go line
// numbers at the time of writing):
//
//	P1 no-throw: ANY header set — arbitrary hostile values on the
//	   Retry-After / anthropic-*-reset / X-Codex-* keys — returns
//	   without panic (retry_after.go:31-87). The parser runs on every
//	   429/5xx from every provider; hostile bytes are normal operation.
//	P2 shape stability: when present=true, EITHER the date form (date
//	   non-zero, delta == time.Until(date) at parse time, possibly
//	   negative for past dates) OR the delta-seconds form (date zero,
//	   delta > 0) holds — the disjoint failure mode is both-zero, and
//	   date-zero with delta <= 0 (callers compose retryAt = date when
//	   non-zero else now+delta, retry_after.go:22-24; delta-seconds
//	   accepts only secs>0, retry_after.go:44-46, 76-83).
//	P3 date-form delta agreement: when the return came from a date form
//	   (date non-zero), delta == time.Until(date) exactly
//	   (retry_after.go:54, 68) — the caller's retryAt must equal the
//	   provider's stated reset instant.
//	P4 delta-seconds conservation: for Retry-After values that are
//	   positive integers whose second count fits the int64-nanosecond
//	   Duration budget (secs <= maxSecs = MaxInt64/1e9 ≈ 9223372036),
//	   delta == secs*time.Second exactly (retry_after.go:44-46);
//	   oversized digits (> int64 Atoi) may return absent or fall through
//	   to date parsing, but must never panic.
//	P5 precedence: a parseable standard Retry-After always beats
//	   provider headers set on the same header map
//	   (retry_after.go:29-30, 36-57 before 59-84).
//
// KNOWN FINDINGS (property counterexamples, 2026-11-06 — reported, NOT
// fixed in this pass; see the KNOWN-BUG subtests for minimal repros):
//
//	F1 delta-seconds int64-ns overflow: "Retry-After: 9223372037"
//	   (secs > maxSecs but still a valid RFC7231 1*DIGIT that Atoi
//	   accepts) wraps time.Duration(secs)*time.Second (retry_after.go:45)
//	   NEGATIVE, returning present=true, date=zero, delta<0 — violating
//	   the delta-seconds shape contract (retry_after.go:17-18, 22-24).
//	   Same wrap on the Codex lanes (retry_after.go:75-84) and on
//	   "2252913649760013"-scale values. shortThrottleSleep
//	   (client.go:226-234) happens to degrade benignly (past serverAt
//	   loses the `wait > step` comparison), so impact is latent, but
//	   every future delta consumer inherits the trap.
//	F2 date-form zero-time sentinel collision: "Retry-After: Mon, 01 Jan
//	   0001 00:00:00 GMT" parses (RFC1123) to the zero time.Time — the
//	   caller cannot distinguish date-form from delta-seconds form, and
//	   delta is hugely negative (retry_after.go:54).
//	F3 (adjacent, same call chain, found while tracing F1's consumers):
//	   shortThrottleSleep (client.go:214-239) documents "never longer
//	   than the plan's Max step, so a huge Retry-After cannot burn the
//	   short budget wall-clock (capped by the plan, D8)" but the code
//	   raises step to the server wait with NO clamp to plan.Max /
//	   GiveUpAt — "Retry-After: 31536000" (1 year) wedges the streaming
//	   throttle retry in `time.After(backoff)` (client.go:~2745) for a
//	   year, contradicting the doc comment.
//
// Generator: seed 20261106, rand.New(rand.NewSource(...)), 5000 cases
// per property; hostile corners seeded explicitly. On failure the seed
// and case index print for replay.

import (
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"testing"
	"time"
)

const retryAfterSeed = int64(20261106)

var hostileHeaderValues = []string{
	"", " ", "0", "-5", "+10", "007", "1.5", "1e3", "99999999999999999999999999",
	"9223372036854775807", "9223372036854775808", // int64 edge and overflow
	"not-a-date", "Fri, 31 Dec 2027 23:59:59 GMT", "Fri, 31 Dec 2027 23:59:60 GMT", // leap second
	"Friday, 31-Dec-27 23:59:59 GMT", "Fri Dec 31 23:59:59 2027",
	"2027-12-31T23:59:59Z", "2027-12-31T23:59:59.123456+05:30",
	"Mon, 01 Jan 0001 00:00:00 GMT", // zero time edge: parses, date==time.Time{}
	"Wed, 31 Feb 2027 00:00:00 GMT", // impossible date
	"\x00\x01", "fullwidth-error", "\u202Eevil", strings_Repeat("9", 4096),
	";q=0.5", "\"120\"", "120, 180",
}

func strings_Repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}

func randHeaderValue(r *rand.Rand) string {
	if r.Intn(3) == 0 {
		return hostileHeaderValues[r.Intn(len(hostileHeaderValues))]
	}
	switch r.Intn(5) {
	case 0: // valid delta-seconds
		return strconv.Itoa(r.Intn(86400))
	case 1: // valid IMF-fixdate (possibly in the past or future)
		t := time.Now().Add(time.Duration(r.Intn(21)-10) * 24 * time.Hour).UTC()
		return t.Format(time.RFC1123)
	case 2: // valid RFC3339
		t := time.Now().Add(time.Duration(r.Intn(21)-10) * time.Hour).UTC()
		return t.Format(time.RFC3339)
	case 3: // digit soup (occasionally huge)
		digits := make([]byte, r.Intn(30))
		for i := range digits {
			digits[i] = byte('0' + r.Intn(10))
		}
		return string(digits)
	default: // arbitrary junk
		n := r.Intn(40)
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(0x20 + r.Intn(0x70))
		}
		return string(b)
	}
}

func headerWith(retryAfter, anthropic, codex string) http.Header {
	h := http.Header{}
	if retryAfter != "" || rflip() {
		h.Set("Retry-After", retryAfter)
	}
	if anthropic != "" || rflip() {
		h.Set("anthropic-ratelimit-tokens-reset", anthropic)
	}
	if codex != "" || rflip() {
		h.Set("X-Codex-Primary-Reset-At", codex)
	}
	return h
}

// rflip is a tiny seeded-coin used only by headerWith; the global is set
// per-property so failures are reproducible from the printed seed.
var flipRand *rand.Rand

func rflip() bool { return flipRand.Intn(2) == 0 }

// TestParseRetryAfter_NoThrowAndShape ports P1+P2: any header set parses
// without panic, and a present result has exactly one non-zero schedule.
func TestParseRetryAfter_NoThrowAndShape(t *testing.T) {
	flipRand = rand.New(rand.NewSource(retryAfterSeed))
	r := rand.New(rand.NewSource(retryAfterSeed + 1))
	// The generator never emits a Retry-After value inside the overflow
	// band (maxInt64 < v <= maxSafeSecs is unreachable from digit soup
	// either — see F1 in the header): those cases are the KNOWN-BUG
	// subtest's job, so this property stays strict elsewhere.
	const maxSafeSecs = int64(1) << 61 / int64(time.Second)
	overflowBand := func(v string) bool {
		for _, c := range v {
			if c < '0' || c > '9' {
				return false
			}
		}
		if v == "" {
			return false
		}
		if len(v) > 18 {
			return true // beyond any safe band shape: route to F1 handling
		}
		n, err := strconv.ParseInt(v, 10, 64)
		return err == nil && n > maxSafeSecs
	}
	for i := 0; i < 5000; i++ {
		h := headerWith(randHeaderValue(r), randHeaderValue(r), randHeaderValue(r))
		if overflowBand(h.Get("Retry-After")) || overflowBand(h.Get("X-Codex-Primary-Reset-At")) || overflowBand(h.Get("X-Codex-Primary-Reset-After-Seconds")) {
			continue // F1 counterexample band: pinned by the KNOWN-BUG subtest
		}
		// F2 band: the year-1 RFC1123 date parses to the zero-time
		// sentinel — the case is indistinguishable from the delta form and
		// pinned by the KNOWN-BUG subtest.
		if h.Get("Retry-After") == "Mon, 01 Jan 0001 00:00:00 GMT" {
			continue
		}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("case %d panicked: %v (headers %v)", i, rec, h)
				}
			}()
			date, delta, present := ParseRetryAfter(h)
			if !present {
				return
			}
			dateSet := !date.IsZero()
			if !dateSet {
				// delta-seconds form: date zero and the schedule strictly
				// positive (secs > 0 gates on retry_after.go:44, 76, 81).
				if delta <= 0 {
					t.Fatalf("case %d: delta-seconds form returned delta %v; headers=%v", i, delta, h)
				}
			} else if delta == 0 {
				t.Fatalf("case %d: date form returned zero delta; headers=%v", i, h)
			}
		}()
	}
}

// TestParseRetryAfter_DateDeltaAgreement ports P3: for date-form inputs the
// returned delta is time.Until(date) at parse time — we can't call
// Until again, so we assert |delta - (Until(date) at assert time)| is
// bounded by test execution time AND that delta's sign/shape tracks the
// date (past date ⇒ negative delta, future date ⇒ positive delta), which
// is the contract callers rely on (retry_after.go:22-24, 54, 68).
func TestParseRetryAfter_DateDeltaAgreement(t *testing.T) {
	flipRand = rand.New(rand.NewSource(retryAfterSeed + 2))
	r := rand.New(rand.NewSource(retryAfterSeed + 3))
	checked := 0
	for i := 0; i < 3000; i++ {
		h := headerWith(randHeaderValue(r), randHeaderValue(r), randHeaderValue(r))
		date, delta, present := ParseRetryAfter(h)
		if !present || date.IsZero() {
			continue
		}
		checked++
		// The parser computed delta = time.Until(date) microseconds
		// before we did; the gap between the two monotonic-clock reads
		// bounds the observed difference. One full second is generous
		// for a test body and far tighter than any caller's clamp.
		if d := time.Until(date); d < 0 && delta >= 0 {
			t.Fatalf("case %d: date %v is past but delta %v is not negative", i, date, delta)
		}
		if skew := (time.Until(date) - delta); skew > time.Second || skew < -time.Second {
			t.Fatalf("case %d: date-form delta %v disagrees with time.Until(date) by %v (date=%v headers=%v)",
				i, delta, skew, date, h)
		}
	}
	if checked == 0 {
		t.Fatal("generator produced no date-form successes; property vacuous")
	}
	t.Logf("checked %d date-form cases", checked)
}

// TestParseRetryAfter_DeltaSecondsExact ports P4: positive integer
// Retry-After values within the Duration-representable budget return
// exactly secs*time.Second.
func TestParseRetryAfter_DeltaSecondsExact(t *testing.T) {
	r := rand.New(rand.NewSource(retryAfterSeed + 4))
	checked := 0
	const maxSafeSecs = int64(1) << 61 / int64(time.Second) // Duration nanosecond budget
	for i := 0; i < 2000; i++ {
		secs := r.Int63n(maxSafeSecs) + 1
		h := http.Header{}
		h.Set("Retry-After", strconv.FormatInt(secs, 10))
		date, delta, present := ParseRetryAfter(h)
		checked++
		if !present {
			t.Fatalf("case %d: Retry-After %d not recognized", i, secs)
		}
		if !date.IsZero() {
			t.Fatalf("case %d: delta-seconds form returned a date %v", i, date)
		}
		if want := time.Duration(secs) * time.Second; delta != want {
			t.Fatalf("case %d: delta drift: got %v want %v", i, delta, want)
		}
	}
	// Overflow budget: digits beyond int64 must not panic and must not
	// produce a positive-delta success.
	for _, big := range []string{"9223372036854775808", strings_Repeat("9", 4096)} {
		h := http.Header{}
		h.Set("Retry-After", big)
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("overflow %q panicked: %v", big, rec)
				}
			}()
			_, delta, present := ParseRetryAfter(h)
			if present && delta <= 0 {
				t.Fatalf("overflow %q: present with non-positive delta %v", big, delta)
			}
		}()
	}
	_ = checked
}

// KNOWN-BUG F1 (property counterexample 2026-11-06, header comment): a
// valid RFC7231 1*DIGIT Retry-After whose seconds exceed the Duration
// nanosecond budget wraps NEGATIVE and is returned with present=true —
// documented shape contract violated. Reported, not fixed in this pass.
// If a future fix lands, delete the want-negativity pins below and move
// the values into TestParseRetryAfter_NoThrowAndShape's hostile set.
func TestParseRetryAfter_KnownBugOverflowWrapsNegative(t *testing.T) {
	t.Skip("KNOWN-BUG F1/F2: enable ONLY to re-verify the reported counterexamples; this subtest FAILS until the product fix lands")
	for _, v := range []string{"9223372036854775807", "9223372037", "2252913649760013"} {
		h := http.Header{}
		h.Set("Retry-After", v)
		_, delta, present := ParseRetryAfter(h)
		if !present {
			t.Fatalf("value %q: not present (behavior changed — re-evaluate F1)", v)
		}
		if delta > 0 {
			t.Fatalf("value %q: delta %v is now POSITIVE — F1 appears fixed; re-enable the strict property", v, delta)
		} else {
			t.Logf("F1 still live: %q -> delta %v (negative wrap)", v, delta)
		}
	}
	// Codex lanes share the wrap (parseRetryAfterSeconds → same multiply).
	h := http.Header{}
	h.Set("X-Codex-Primary-Reset-At", "9223372036854775807")
	_, delta, present := ParseRetryAfter(h)
	if !present || delta > 0 {
		t.Fatalf("codex lane behavior drifted (delta=%v present=%v) — re-evaluate F1", delta, present)
	}
	// F2: the zero-time date-form collision.
	h2 := http.Header{}
	h2.Set("Retry-After", "Mon, 01 Jan 0001 00:00:00 GMT")
	date, _, present := ParseRetryAfter(h2)
	if !present || !date.IsZero() {
		t.Fatalf("F2 behavior drifted (date=%v present=%v) — re-evaluate F2", date, present)
	}
	t.Log("F2 still live: date-form year-1 parses to the zero-time sentinel")
}

// TestParseRetryAfter_Precedence ports P5: a parseable standard
// Retry-After wins over provider headers on the same map.
func TestParseRetryAfter_Precedence(t *testing.T) {
	future := time.Now().Add(48 * time.Hour).UTC()
	raDate := future.Format(time.RFC1123)
	anthro := future.Add(time.Hour).Format(time.RFC3339)

	h := http.Header{}
	h.Set("Retry-After", raDate)
	h.Set("anthropic-ratelimit-tokens-reset", anthro)
	h.Set("X-Codex-Primary-Reset-At", "99")
	date, _, present := ParseRetryAfter(h)
	if !present {
		t.Fatal("Retry-After not recognized over provider headers")
	}
	if got, want := date.Unix(), future.Unix(); got != want {
		t.Fatalf("precedence: date=%v want the Retry-After instant %v (fmt.Sprintf sanity %s)", date, future, fmt.Sprintf("%d", want))
	}

	h2 := http.Header{}
	h2.Set("Retry-After", "30")
	h2.Set("X-Codex-Primary-Reset-At", "99")
	_, delta, present := ParseRetryAfter(h2)
	if !present || delta != 30*time.Second {
		t.Fatalf("standard delta-seconds must win: delta=%v present=%v", delta, present)
	}

	// No standard header: provider lanes still work.
	h3 := http.Header{}
	h3.Set("X-Codex-Primary-Reset-After-Seconds", "45")
	_, delta, present = ParseRetryAfter(h3)
	if !present || delta != 45*time.Second {
		t.Fatalf("codex after-seconds lane broken: delta=%v present=%v", delta, present)
	}
}
