package sms

// 04-replies-consent/00T §K1/§K2 — reply classifier + PendingConfirms
// registry gate (TEST-ONLY file; nothing here is production code).
//
// Compile-RED until internal/sms/reply.go (leaf 04/01) lands. RED is the
// gate's posture: every failure must be an undefined-symbol error on the
// frozen shapes below, never a logic bug in this file.
//
// FROZEN SHAPES (master.md §7, docs/plans/20261110-telnyx-texting/
// 04-replies-consent/01-reply.md):
//
//	type ReplyAction string
//	const ReplyStop / ReplyStart / ReplyHelp / ReplyYes / ReplyOther ReplyAction
//	func ClassifyReply(text string) ReplyAction
//
//	type Pending struct {
//	    Ref          string
//	    To           string
//	    Text         string
//	    RegisteredAt time.Time
//	}
//	type PendingConfirms struct{ ... }
//	func NewPendingConfirms(clock func() time.Time, ttl time.Duration) *PendingConfirms
//	func (p *PendingConfirms) Register(ref, to, text string)
//	func (p *PendingConfirms) Resolve(to string) (*Pending, bool)
//
// The ReplyAction constant STRING VALUES are deliberately NOT pinned (the
// five identifiers are the contract; daemon glue compares against the
// constants). ClassifyReply is a pure function: never mutates input,
// never panics on junk. NewPendingConfirms(clock, ttl): clock nil =>
// time.Now; the gate always passes a positive ttl (ttl<=0 behavior is
// undefined and unasserted).
//
// Keyword-source note (§K1): the STOP/START/HELP families are ports of
// strategist-usa pkg/services/consent_service.go (owner's own repo, no
// third-party license; per the branch provenance header). Every case
// below cites the source regex line. Adaptations to the strategist
// original, each asserted explicitly:
//
//	A1. YES splits out of the START family. strategist startRegex
//	    (consent_service.go:143) lists YES inside START; meept's #132
//	    dispatch-ack needs YES as its OWN action (master §7: "YES = exact
//	    yes/confirm/done/ok"), so ClassifyReply("yes") must be ReplyYes,
//	    NOT ReplyStart. confirm/done/ok are meept additions (master §7),
//	    same exact-token posture.
//	A2. "noneed" is a master §7 token with no strategist regex; the impl
//	    adds NONEED as an exact STOP token.
//	A3. Anchored full-string match posture retained: consent keywords are
//	    whole-message commands, so prose that merely CONTAINS a keyword
//	    ("I want to stop later") stays ReplyOther.
//
// §K2 number-compare decision (01-reply.md left it open: "duplicate
// minimal normalizer OR exact-string match — pin choice in report"):
// this gate pins the NORMALIZED compare — Register/Resolve canonicalize
// through a sms-local, stdlib-only digit normalizer with the same
// semantics as strategist normalizeE164 (consent_service.go:297-325:
// strip non-digits, 10 bare digits => +1XXXXXXXXXX, leading 1 kept).
// internal/sms must NOT import internal/voice/telnyx (leaf-01 doc:
// cycle risk with daemon), hence local duplication.
//
// Test phone numbers are built with digit concatenation (rplSmsPhone) so
// no PII-redaction layer can rewrite literals mid-gate, same posture as
// the prior sms/arming gates.

import (
	"strings"
	"testing"
	"time"
)

// rplSmsPhone builds "+1555000NNNN" from a 4-digit suffix via
// concatenation (redaction-proof; collision-free name vs store_test.go's
// phone()).
func rplSmsPhone(suffix string) string {
	return "+" + strings.Join([]string{"1", "555", "000", suffix}, "")
}

// --- §K1 ClassifyReply --------------------------------------------------

func TestClassifyReplyStopFamily(t *testing.T) {
	// Every positive cites strategist-usa pkg/services/consent_service.go
	// (line of the compiled regex); NONEED cites master.md §7 (A2).
	cases := []struct {
		text string
		src  string // source of the variant
	}{
		// stopRegex :100 — the seven 47 CFR 64.1200(a)(10) per-se keywords
		// plus the strategist extensions.
		{"stop", ":100"},
		{"STOP", ":100"},
		{"Unsubscribe", ":100"},
		{"cancel", ":100"},
		{"end", ":100"},
		{"quit", ":100"},
		{"remove", ":100"},
		{"revoke", ":100"},
		{"unsub", ":100"},
		{"stopall", ":100"},
		{"stop all", ":100"},
		{"remove me", ":100"},
		{"opt out", ":100"},
		{"optout", ":100"},
		// punctuation + emoji tails allowed by the same regex :100.
		{"STOP!!!", ":100"},
		{"stop.🛑", ":100"},
		// stopMisspellingRegex :104.
		{"STIP", ":104"},
		{"stopp", ":104"},
		{"STO", ":104"},
		{"stop", ":104"},
		{"SOTP", ":104"},
		{"DTOP", ":104"},
		{"atop", ":104"},
		// stopWithContextRegex :120 — natural-sentence revocations
		// (the wave-T C5 case is literal).
		{"please stop sending me messages", ":120"},
		{"stop it", ":120"},
		{"opt out of this", ":120"},
		{"unsubscribe from these messages", ":120"},
		// neverAgainRegex :125.
		{"never text me", ":125"},
		{"don't ever contact me", ":125"},
		{"do not send me messages", ":125"},
		// stopEmojiRegex :128.
		{"🛑", ":128"},
		{"🛑🛑", ":128"},
		// stopRepeatedRegex :132.
		{"STOP STOP", ":132"},
		{"stop, stop!", ":132"},
		// implicitOptOutRegex :137.
		{"leave me alone", ":137"},
		{"do not contact", ":137"},
		{"take me off this list", ":137"},
		{"lose my number", ":137"},
		{"report spam", ":137"},
		{"BLOCKED", ":137"},
		// hostileOptOutRegex :140.
		{"F OFF", ":140"},
		{"fuck off", ":140"},
		// master.md §7 STOP family token (A2).
		{"noneed", "master §7"},
		{"NONEED", "master §7"},
	}
	for _, c := range cases {
		if got := ClassifyReply(c.text); got != ReplyStop {
			t.Errorf("ClassifyReply(%q) = %q; want %q (source %s)", c.text, got, ReplyStop, c.src)
		}
	}
}

func TestClassifyReplyCaseTrimNormalisation(t *testing.T) {
	// "upper(trim)" per master §7: surrounding whitespace and any casing
	// normalize before matching.
	for _, text := range []string{"  sToP ", "\tSTOP\n", "  Help  ", "  yEs  "} {
		want := map[string]ReplyAction{
			"  sToP ":  ReplyStop,
			"\tSTOP\n": ReplyStop,
			"  Help  ": ReplyHelp,
			"  yEs  ":  ReplyYes,
		}[text]
		if got := ClassifyReply(text); got != want {
			t.Errorf("ClassifyReply(%q) = %q; want %q", text, got, want)
		}
	}
}

func TestClassifyReplyStartFamily(t *testing.T) {
	// startRegex :143, minus YES (adaptation A1 — YES is its own action).
	for _, text := range []string{"start", "START", "subscribe", "unstop", "resume", "rejoin"} {
		if got := ClassifyReply(text); got != ReplyStart {
			t.Errorf("ClassifyReply(%q) = %q; want %q (consent_service.go:143 port)", text, got, ReplyStart)
		}
	}
}

func TestClassifyReplyYesFamily(t *testing.T) {
	// master §7 exact tokens (dispatch ack #132). "yes" must classify
	// ReplyYes even though strategist :143 folds it into START (A1).
	for _, text := range []string{"yes", "YES", "Yes", "confirm", "done", "ok", "OK"} {
		if got := ClassifyReply(text); got != ReplyYes {
			t.Errorf("ClassifyReply(%q) = %q; want %q", text, got, ReplyYes)
		}
	}
}

func TestClassifyReplyHelpFamily(t *testing.T) {
	// helpRegex :146.
	for _, text := range []string{"help", "HELP", "info", "support", "assistance"} {
		if got := ClassifyReply(text); got != ReplyHelp {
			t.Errorf("ClassifyReply(%q) = %q; want %q (consent_service.go:146 port)", text, got, ReplyHelp)
		}
	}
}

func TestClassifyReplyOther(t *testing.T) {
	cases := []struct {
		text string
		src  string
	}{
		{"", "empty"},
		{"   ", "whitespace-only"},
		{"hey there", "prose"},
		{"are you free friday", "prose"},
		// A3: anchored match posture — keyword-substring prose stays Other.
		{"I want to stop later", "anchored vs :120 (no filler-prefix match)"},
		{"what time does the show end", "anchored vs :100 (END not whole msg)"},
		{"stopwatch", ":100 anchored (STOP+suffix token)"},
		{"can you confirm tomorrow", "YES list is exact-token only"},
		// imessageReactionRegex :149: tapbacks are skipped as commands in
		// strategist (ParseCommand "" pre-check :179) — meept maps that to
		// ReplyOther (no consent action).
		{`Liked "great"`, ":149 reaction skip"},
		{"1", "numeric"},
		{"🛑🛑 🛑", ":128 emoji run with interior space (not ^emoji+$)"},
	}
	for _, c := range cases {
		if got := ClassifyReply(c.text); got != ReplyOther {
			t.Errorf("ClassifyReply(%q) = %q; want %q (%s)", c.text, got, ReplyOther, c.src)
		}
	}
}

// --- §K2 PendingConfirms ------------------------------------------------

func TestPendingConfirmsRegisterResolveRoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 10, 15, 4, 0, 0, time.UTC)
	cur := now
	p := NewPendingConfirms(func() time.Time { return cur }, 10*time.Minute)

	ref, to, text := "dispatch-77", rplSmsPhone("1234"), "coffee at 3?"
	p.Register(ref, to, text)

	got, ok := p.Resolve(to)
	if !ok || got == nil {
		t.Fatalf("Resolve(%s) = (%v,%v); want the registered pending", to, got, ok)
	}
	if got.Ref != ref || got.Text != text {
		t.Fatalf("pending = %+v; want Ref=%q Text=%q", got, ref, text)
	}
	if !got.RegisteredAt.Equal(now) {
		t.Fatalf("RegisteredAt = %v; want the clock value at Register (%v)", got.RegisteredAt, now)
	}
}

func TestPendingConfirmsOneShot(t *testing.T) {
	p := NewPendingConfirms(nil, time.Hour) // nil clock => time.Now (shape pin)
	to := rplSmsPhone("2345")
	p.Register("dispatch-88", to, "ack me")

	if _, ok := p.Resolve(to); !ok {
		t.Fatal("first Resolve = false; want the pending")
	}
	got, ok := p.Resolve(to)
	if ok || got != nil {
		t.Fatalf("second Resolve = (%v,%v); want (nil,false) — one-shot consume", got, ok)
	}
}

func TestPendingConfirmsTTLExpiry(t *testing.T) {
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	ttl := 10 * time.Minute
	cur := base
	p := NewPendingConfirms(func() time.Time { return cur }, ttl)
	to := rplSmsPhone("3456")
	p.Register("dispatch-99", to, "still warm")

	cur = base.Add(ttl - time.Second)
	if _, ok := p.Resolve(to); !ok {
		t.Fatal("Resolve inside the TTL window returned false; want the pending")
	}

	// Fresh registry: expiry is per-entry from RegisteredAt (clock seam).
	cur = base
	p2 := NewPendingConfirms(func() time.Time { return cur }, ttl)
	p2.Register("dispatch-100", to, "will expire")
	cur = base.Add(ttl + time.Second)
	if got, ok := p2.Resolve(to); ok || got != nil {
		t.Fatalf("Resolve after TTL = (%v,%v); want (nil,false)", got, ok)
	}
}

func TestPendingConfirmsNormalizedNumberCompare(t *testing.T) {
	// §K2 pin: Register stores E.164 "+15550001234"; Resolve accepts the
	// display-shape variants via the sms-local digit normalizer (strategist
	// normalizeE164 semantics, consent_service.go:297-325; adaptation A4:
	// LOCAL copy — sms must not import voice/telnyx, cycle risk).
	e164 := rplSmsPhone("1234")
	variants := []string{
		" (555) 000-1234 ", // formatted display shape
		"5550001234",       // bare 10-digit => +1 prefix
		"15550001234",      // 11 digits leading-1
		"+1.555.000.1234",  // dotted
		e164,               // exact E.164
	}
	for _, v := range variants {
		p := NewPendingConfirms(nil, time.Hour)
		p.Register("dispatch-v", e164, "variant probe")
		got, ok := p.Resolve(v)
		if !ok || got == nil || got.Ref != "dispatch-v" {
			t.Errorf("Register(%q) then Resolve(%q) = (%v,%v); want the pending (normalized compare)", e164, v, got, ok)
		}
	}

	// Different digits never cross-resolve.
	p := NewPendingConfirms(nil, time.Hour)
	p.Register("dispatch-x", e164, "must not leak")
	if _, ok := p.Resolve(rplSmsPhone("9999")); ok {
		t.Fatal("Resolve of an unregistered number returned true")
	}
	if _, ok := p.Resolve("+15550001235"); ok {
		t.Fatal("Resolve adjacent-by-one number returned true; normalization too lossy")
	}
}
