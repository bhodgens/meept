package voicechild

// Gate tests for contract C6 (master.md, quoted verbatim):
//
//	C6 Normalize: func NormalizeUtterance(text string, preserve
//	func(token string) bool) string in internal/voicechild (exported for
//	tests); idempotent (Normalize(Normalize(x)) == Normalize(x)).
//
// Cases per master Test Gate: "the the the"=>"the"; "yeah yeah
// yeah"=>"yeah"; "um um um"=>"um"; preserve-list keeps unknown proper
// noun; idempotence property over all canonical fixtures. Ported from
// uptractor src/lib/agent/turn.ts normalizeUtterance (branch
// fix/speech-robustness). RED until leaf 03 (NormalizeUtterance
// undefined).

import (
	"strings"
	"testing"
)

// id is the nil-safe preserve hook most callers pass (nothing preserved).
func id(token string) bool { return false }

// keepAnything preserves every token (exercises the preserve seam).
func keepAnything(token string) bool { return true }

func TestNormalizeUtteranceRepeatedWords(t *testing.T) {
	cases := []struct{ in, want string }{
		// Canonical master cases:
		{"the the the", "the"},
		{"yeah yeah yeah", "yeah"},
		{"um um um", "um"},
		// Adjacent duplicates collapse ("the the" -> "the").
		{"the the", "the"},
		// Whitespace runs collapse in the same pass.
		{"the   the  the", "the"},
		// Non-adjacent repeats survive (only runs collapse).
		{"the cat saw the dog", "the cat saw the dog"},
	}
	for _, tc := range cases {
		if got := NormalizeUtterance(tc.in, id); got != tc.want {
			t.Errorf("NormalizeUtterance(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeUtteranceFillerRuns(t *testing.T) {
	cases := []struct{ in, want string }{
		// STT filler runs collapse to one filler token.
		{"um um um", "um"},
		{"yeah yeah yeah um um", "yeah um"},
		{"uh uh uh", "uh"},
		{"ok ok ok", "ok"},
		{"okay okay", "okay"},
		// Mixed filler + content keeps the content.
		{"yeah yeah yeah mark it done", "yeah mark it done"},
	}
	for _, tc := range cases {
		if got := NormalizeUtterance(tc.in, id); got != tc.want {
			t.Errorf("NormalizeUtterance(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeUtterancePreserveListKeepsProperNoun(t *testing.T) {
	// GIVEN a preserve-list hook that recognizes employee context names;
	// WHEN the utterance carries an otherwise-unknown proper noun;
	// THEN the preserve-listed token survives normalization.
	cases := []struct {
		in       string
		preserve func(string) bool
		want     string
	}{
		{
			// Unknown proper noun preserved by the list.
			"tell meadow about the um um update",
			func(tok string) bool { return tok == "meadow" },
			"meadow tell about the update",
		},
		{
			// Garbled STT name fragments survive so the fuzzy match
			// downstream still sees them.
			"mark the ridg done",
			func(tok string) bool { return tok == "ridg" },
			"ridg mark done",
		},
		{
			// Empty preserve-list: noise tokens drop (subject to the
			// action-word/numeric keeps).
			"flurb blurb um um",
			id,
			"",
		},
	}
	for _, tc := range cases {
		if got := NormalizeUtterance(tc.in, tc.preserve); got != tc.want {
			t.Errorf("NormalizeUtterance(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeUtterancePreserveEverything(t *testing.T) {
	// keepAnything must disable all noise-token drops: only run-collapses
	// remain.
	in := "um um um the the flurb"
	want := "um the flurb"
	if got := NormalizeUtterance(in, keepAnything); got != want {
		t.Errorf("NormalizeUtterance(%q, keep-all) = %q, want %q", in, got, want)
	}
}

func TestNormalizeUtteranceKeepsActionWordsAndNumbers(t *testing.T) {
	// Action words and numeric tokens survive even with an empty
	// preserve-list (source turn.ts keeps: done/finished/mark/move/...
	// and ^\d).
	cases := []struct{ in, want string }{
		{"mark it done", "mark done"},
		{"log 40 sheets", "log 40"},
		{"move it to friday", "move friday"},
	}
	for _, tc := range cases {
		if got := NormalizeUtterance(tc.in, id); got != tc.want {
			t.Errorf("NormalizeUtterance(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// normalizeFixtures is every canonical fixture this file asserts; the
// idempotence property runs over ALL of them (plus the raw canonical
// table inputs), so a non-idempotent normalize cannot hide behind one
// lucky case.
var normalizeFixtures = []string{
	"the the the",
	"yeah yeah yeah",
	"um um um",
	"yeah yeah yeah um um",
	"tell meadow about the um um update",
	"mark the ridg done",
	"mark it done",
	"log 40 sheets",
	"Meadow Ridge",
	"summer planes",
	"the cat saw the dog",
	"   ",
	"",
}

func TestNormalizeUtteranceIdempotent(t *testing.T) {
	for _, fx := range normalizeFixtures {
		once := NormalizeUtterance(fx, id)
		twice := NormalizeUtterance(once, id)
		if once != twice {
			t.Errorf("not idempotent on %q: once=%q twice=%q", fx, once, twice)
		}
	}
}

func TestNormalizeUtteranceIdempotentWithPreserve(t *testing.T) {
	// Idempotence must hold under a non-trivial preserve hook too.
	for _, fx := range normalizeFixtures {
		once := NormalizeUtterance(fx, func(tok string) bool { return strings.Contains(tok, "meadow") })
		twice := NormalizeUtterance(once, func(tok string) bool { return strings.Contains(tok, "meadow") })
		if once != twice {
			t.Errorf("not idempotent (preserve) on %q: once=%q twice=%q", fx, once, twice)
		}
	}
}

func TestNormalizeUtteranceDegenerateInputs(t *testing.T) {
	// Whitespace-only and empty inputs normalize to "" without panicking.
	if got := NormalizeUtterance("   ", id); got != "" {
		t.Errorf("whitespace-only = %q, want empty", got)
	}
	if got := NormalizeUtterance("", id); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
	// A nil preserve hook must be tolerated (callers pass nil in simple
	// configs): normalize must not call through a nil func.
	if got := NormalizeUtterance("um um um the the", nil); strings.Contains(got, "the the") {
		t.Errorf("nil preserve hook: %q still carries the collapsed run", got)
	}
}
