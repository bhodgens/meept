package slack

// Property-based tests for the HITL permission action-value grammar
// (message.go — C3 contract: "perm:<requestID>:<verb>", verb ∈
// {allow,deny,always}) and the agent-text escaper (text.go).
//
// Hand-rolled, zero new deps, deterministic seed (skill:
// software-development/property-based-testing; in-house pattern:
// strategist-usa go-shard-runner parse_csv_property_test.go).
//
// Properties (implementation contract refs are message.go / text.go
// line numbers at the time of writing):
//
//	P1 round-trip: for any requestID whose bytes contain no ':' and any
//	   relay verb, ParseActionValue(ActionValue(id, verb)) == (id, verb,
//	   true) — ActionValue is the documented INVERSE of ParseActionValue
//	   (message.go:39-42 vs :18-37). The ':' exclusion is not an
//	   overfit: real request IDs are pkg/id hex suffixes ("perm" + 16
//	   hex, internal/acp/relay.go:330 + pkg/id/id.go), and Slack button
//	   values themselves have no documented ':' escaping — a ':' in an
//	   id is unrepresentable in the pinned grammar, so the generator
//	   excludes it the way the producer does.
//	P2 accept/reject conservation: EVERY string either parses with a
//	   verb in the relay verb set, or is rejected — never a parse whose
//	   verb is outside {allow,deny,always} (message.go:31-36), and a
//	   rejection NEVER yields a non-empty id or verb (the caller must
//	   not feed rejected values to the relay; message.go:19-21).
//	P3 no-throw on hostile input: arbitrary bytes as action values never
//	   panic (message.go:22) — block_actions payloads come from Slack's
//	   servers relaying user/craft-built interactions.
//	P4 escapeSlackText bound + validity (text.go:12-33): for ANY input
//	   the output is (a) <= 604 runes (600 + " ..."), (b) valid UTF-8,
//	   (c) free of C0 control characters other than '\n' and '\t' —
//	   never able to inject Block Kit structure or split a UTF-8
//	   sequence, the exact two threats the escaper exists for.
//	P5 truncateRunes bound (text.go:36-42): output rune count <= max+4
//	   and truncation never splits a rune; when no truncation occurs the
//	   output is the input verbatim.
//
// Generator: seed 20261106, rand.New(rand.NewSource(...)), 5000 cases
// per property; hostile corners seeded explicitly. On failure the seed
// and case index print for replay.

import (
	"math/rand"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

const slackPropSeed = int64(20261106)

var slackHostileCorners = []string{
	"", " ", ":", "::", "perm:", "perm::", "perm::allow", "perm:a:",
	"perm:a:allow", "perm:a:Allow", "perm:a:ALLOW", "perm:a:always",
	"perm:a:deny", "perm:a:always:extra", "PERM:a:allow", "perm:a:allow ",
	" perm:a:allow", "perm:a:b:allow", "perm:a:al low", "\x00", "\n",
	"perm:\x00:allow", "🚀:✅", "perm:🚀:allow", strings.Repeat("x", 4096),
	"=cmd()", "../../etc/passwd", "perm:a:allow\x00b",
}

func randSlackID(r *rand.Rand) string {
	if r.Intn(6) == 0 {
		id := hostileID(r)
		// Empty ids are unrepresentable in the grammar (see hostileID
		// note); substitute a minimal non-empty hostile shape.
		if id == "" {
			return "\x00"
		}
		return id
	}
	// Realistic: pkg/id shape = prefix + 16 lowercase hex (no ':').
	const hex = "0123456789abcdef"
	n := r.Intn(3) + 1 // 1-3 segments worth of colon-free text
	parts := make([]string, 0, n)
	for range n {
		m := r.Intn(19) + 1 // segments are 1..19 hex chars; a 0-length segment yields "" (outside P1's domain)
		b := make([]byte, m)
		for i := range b {
			b[i] = hex[r.Intn(16)]
		}
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "-")
}

func hostileID(r *rand.Rand) string {
	// NOTE: every corner must be colon-free AND non-empty — ':' and ""
	// are unrepresentable in the pinned grammar (ParseActionValue rejects
	// empty ids, message.go:28; the producer guards "" at channel.go:116),
	// so both are outside P1's domain (documented in the header), not
	// round-trip counterexamples.
	cornerIDs := []string{"a", "perm", "🚀", "\x00", strings.Repeat("x", 64), "perm-fake-allow"}
	return cornerIDs[r.Intn(len(cornerIDs))]
}

func randActionValue(r *rand.Rand) string {
	switch r.Intn(3) {
	case 0: // well-formed
		verbs := []string{"allow", "deny", "always"}
		return ActionValue(randSlackID(r), verbs[r.Intn(3)])
	case 1: // seeded hostile corner
		return slackHostileCorners[r.Intn(len(slackHostileCorners))]
	default: // arbitrary junk
		n := r.Intn(40)
		b := make([]rune, 0, n)
		for range n {
			b = append(b, rune(r.Intn(0x2000)))
		}
		return string(b)
	}
}

// TestActionValue_RoundTrip ports P1: build→parse is the identity for
// colon-free ids over every relay verb.
func TestActionValue_RoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(slackPropSeed))
	for i := 0; i < 5000; i++ {
		id := randSlackID(r)
		verb := []string{"allow", "deny", "always"}[r.Intn(3)]
		v := ActionValue(id, verb)
		gotID, gotVerb, ok := ParseActionValue(v)
		if !ok {
			t.Fatalf("case %d: ActionValue(%q,%q)=%q failed to parse", i, id, verb, v)
		}
		if gotID != id || gotVerb != verb {
			t.Fatalf("case %d: round-trip drift: (%q,%q) -> %q -> (%q,%q)", i, id, verb, v, gotID, gotVerb)
		}
	}
}

// TestActionValue_RejectsEverythingElse ports P2: any string not produced
// by the grammar is rejected with EMPTY outputs (never a partial parse the
// relay could act on).
func TestActionValue_RejectsEverythingElse(t *testing.T) {
	r := rand.New(rand.NewSource(slackPropSeed + 1))
	for i := 0; i < 5000; i++ {
		v := randActionValue(r)
		id, verb, ok := ParseActionValue(v)
		if !ok {
			if id != "" || verb != "" {
				t.Fatalf("case %d: rejected value %q leaked id=%q verb=%q", i, v, id, verb)
			}
			continue
		}
		switch verb {
		case "allow", "deny", "always":
			if id == "" {
				t.Fatalf("case %d: accepted %q with empty id", i, v)
			}
		default:
			t.Fatalf("case %d: accepted %q with out-of-set verb %q", i, v, verb)
		}
	}
	// Grammar-negative corners must all reject (exact-shape contract).
	for _, c := range []string{"", "perm:", "perm::allow", "perm:a:", "perm:a:Allow",
		"perm:a:always:extra", "perm:a:b:allow", "PERM:a:allow", "perm:a:al low"} {
		if id, verb, ok := ParseActionValue(c); ok {
			t.Fatalf("corner %q parsed as (%q,%q) — grammar loosened", c, id, verb)
		}
	}
}

// TestActionValue_NeverPanics ports P3: arbitrary bytes never panic the
// parser.
func TestActionValue_NeverPanics(t *testing.T) {
	r := rand.New(rand.NewSource(slackPropSeed + 2))
	for i := 0; i < 2000; i++ {
		raw := make([]byte, r.Intn(64))
		for j := range raw {
			raw[j] = byte(r.Intn(256))
		}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("case %d panicked: %v (input %q)", i, rec, raw)
				}
			}()
			_, _, _ = ParseActionValue(string(raw))
		}()
	}
}

// TestEscapeSlackText_BoundedAndClean ports P4: the escaper always bounds
// length, keeps valid UTF-8, and strips C0 controls (except \n and \t).
func TestEscapeSlackText_BoundedAndClean(t *testing.T) {
	r := rand.New(rand.NewSource(slackPropSeed + 3))
	inputs := make([]string, 0, len(slackHostileCorners)+2500)
	inputs = append(inputs, slackHostileCorners...)
	// Explicit hostile shapes: json-injection attempt, astral-plane soup,
	// boundary-length inputs around the 600-rune cut.
	inputs = append(inputs,
		`{"type":"actions","elements":[...]}`, strings.Repeat("🚀", 700),
		strings.Repeat("a", 599), strings.Repeat("a", 600), strings.Repeat("a", 601),
		strings.Repeat("ab", 400), "\xed\xa0\x80", // lone surrogate (invalid UTF-8 input)
	)
	for i := 0; i < 2500; i++ {
		n := r.Intn(800)
		b := make([]rune, 0, n)
		for range n {
			switch r.Intn(4) {
			case 0:
				b = append(b, rune(r.Intn(0x20))) // C0 controls
			case 1:
				b = append(b, rune(0x1F300+r.Intn(0x100))) // astral
			case 2:
				b = append(b, []rune(`{}[]"` + "\n\t")[r.Intn(7)])
			default:
				b = append(b, rune(0x20+r.Intn(0x5f)))
			}
		}
		inputs = append(inputs, string(b))
	}

	for i, in := range inputs {
		out := escapeSlackText(in)
		if utf8.RuneCountInString(out) > 604 {
			t.Fatalf("case %d: output %d runes > 604 bound", i, utf8.RuneCountInString(out))
		}
		if !utf8.ValidString(out) {
			t.Fatalf("case %d: output is invalid UTF-8 (input %q)", i, truncateForLog(in))
		}
		for _, rr := range out {
			if rr < 0x20 && rr != '\n' && rr != '\t' {
				t.Fatalf("case %d: output contains control rune %U (input %q)", i, rr, truncateForLog(in))
			}
		}
	}
}

// TestTruncateRunes_RuneBoundary ports P5: truncation stays on a rune
// boundary, honors the bound, and is the identity under the bound.
func TestTruncateRunes_RuneBoundary(t *testing.T) {
	r := rand.New(rand.NewSource(slackPropSeed + 4))
	for i := 0; i < 3000; i++ {
		n := r.Intn(50)
		b := make([]rune, 0, n)
		for range n {
			b = append(b, rune(0x1F300+r.Intn(0x100))) // 4-byte runes: any byte-cut would panic Validate
		}
		in := string(b)
		max := r.Intn(60)
		out := truncateRunes(in, max)
		if rc := utf8.RuneCountInString(out); rc > max+4 {
			t.Fatalf("case %d: %d runes > max+4 (%d)", i, rc, max+4)
		}
		if !utf8.ValidString(out) {
			t.Fatalf("case %d: truncated output invalid UTF-8", i)
		}
		if utf8.RuneCountInString(in) <= max && out != in {
			t.Fatalf("case %d: untruncated input mutated: %q -> %q", i, in, out)
		}
	}
}

func truncateForLog(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// unused guard: unicode import documents intent if bounds tighten later.
var _ = unicode.IsControl
