package agent

// Pins for the wave-2 regression in the platform/schedule/git-vs-recall
// arbitration (dispatcher.go) and the byte-arithmetic panic in
// leadingRecallClause:
//
//   - wave-2 narrowed the recall match to leadingRecallClause(input)
//     UNCONDITIONALLY, so any separator before the status phrase killed the
//     recall match and let the untrusted platform/git/schedule verdict survive
//     (the F40/run-10 contextless-committer failure and the run-5 A2 roster
//     failure the guard exists to close).
//   - leadingRecallClause computed the separator index in a LOWERCASED copy and
//     sliced the ORIGINAL, which panics when ToLower changes byte length and
//     returns broken UTF-8 when it shortens.

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestLeadingRecallClause_Separators pins the clause-cut boundaries, including
// the sentence separators ('.', '?', '!') the wave left out: with only commas,
// semicolons and conjunction phrases, "implement the endpoint. check that the
// response is this format" had no boundary before its status clause and was
// swallowed into chat recall.
func TestLeadingRecallClause_Separators(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"implement the endpoint. check that the response is this format", "implement the endpoint"},
		{"did the build pass, and was the file created?", "did the build pass"},
		{"hey, did the change get made?", "hey"},
		{"quick question, is the task done?", "quick question"},
		{"check the log at /tmp/x,y.log, did the change get made?", "check the log at /tmp/x"},
		{"is it done? where is the file?", "is it done"},
		{"write it now! then tell me", "write it now"},
		{"first do this; then do that", "first do this"},
		{"do a THEN do b", "do a"},
		{"do a and do b", "do a"},
		{"do a but not b", "do a"},
		{"update me: did the file get created?", "update me: did the file get created"}, // ':' is not a boundary (F41); the trailing '?' is
		{"no boundary in this string", "no boundary in this string"},
	} {
		if got := leadingRecallClause(tc.in); got != tc.want {
			t.Errorf("leadingRecallClause(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestLeadingRecallClause_NoByteArithmeticPanic pins the panic/broken-UTF-8
// class: the previous implementation found the separator index in
// strings.ToLower(input) and sliced input at that index. U+023A ("Ⱥ") lowers to
// the 3-byte U+2C65, so 40 copies push the separator index past the original
// length; U+212A ("K" KELVIN SIGN) lowers to the 1-byte "k", so the slice lands
// mid-rune. The clause must be computed without ever mapping lowered-byte
// indices onto the original.
func TestLeadingRecallClause_NoByteArithmeticPanic(t *testing.T) {
	// Expands on lowering: 40 × U+023A (2 bytes) → 40 × U+2C65 (3 bytes), so a
	// lowered index of 120 exceeds the original length of 106.
	expanding := strings.Repeat("\u023a", 40) + ", did the change get made?"
	wantExpanding := strings.Repeat("\u023a", 40)

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("leadingRecallClause panicked on multi-byte input: %v", r)
			}
		}()
		got := leadingRecallClause(expanding)
		if got != wantExpanding {
			t.Errorf("leadingRecallClause(expanding) = %q, want %q", got, wantExpanding)
		}
		if !utf8.ValidString(got) {
			t.Errorf("leadingRecallClause(expanding) = %q, not valid UTF-8", got)
		}
	}()

	// Shortens on lowering: U+212A → "k", so a lowered index of 2 lands one
	// byte into a 3-byte rune.
	shortening := "K\u212a, did the change get made?"
	got := leadingRecallClause(shortening)
	if got != "K\u212a" {
		t.Errorf("leadingRecallClause(shortening) = %q, want %q", got, "K\u212a")
	}
	if !utf8.ValidString(got) {
		t.Errorf("leadingRecallClause(shortening) = %q, not valid UTF-8", got)
	}
}

// TestRecallArbitration_SeparatorBeforeStatusPhraseStillRecalls pins the
// arbitration reversal. Each of these is a genuine recall/report question whose
// status phrase sits AFTER a conversational separator. The wave-2 narrowing
// judged only the leading clause, so the recall match died and the untrusted
// platform verdict survived. The classifier returns platform for every input,
// so the arbitration — not the classifier — decides.
func TestRecallArbitration_SeparatorBeforeStatusPhraseStillRecalls(t *testing.T) {
	for _, input := range []string{
		"hey, did the change get made?",
		"quick question, is the task done?",
		"ok, what files did you create?",
		"did the build pass, and was the file created?",
		"check the log at /tmp/x,y.log, did the change get made?",
		"what files did you make for me? where is the file?",
	} {
		t.Run(input, func(t *testing.T) {
			// Precondition: the whole input IS a recall question and does NOT
			// open with an imperative verb, so the whole-input match must stand.
			if !isWorkStatusRecall(input) && !isSecondPersonWorkRecall(input) {
				t.Fatalf("precondition: %q must read as a whole-input recall question", input)
			}
			if hasLeadingImperativeVerb(input) {
				t.Fatalf("precondition: %q must not open with an imperative verb", input)
			}

			d := NewDispatcher(DispatcherConfig{
				Logger:           testLogger(),
				ClassifierClient: newClassifierJSONServer(t, `{"intent":"platform","confidence":0.9}`),
			})
			intent, err := d.classifyIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
			if err != nil {
				t.Fatalf("classifyIntent: %v", err)
			}
			if intent == nil {
				t.Fatal("nil intent")
			}
			if intent.Type != string(IntentRecall) || intent.Method != "platform_recall_arbitration" {
				t.Fatalf("intent = %q (method %q), want recall via platform_recall_arbitration — a separator before the status phrase killed the recall match and the untrusted platform verdict survived",
					intent.Type, intent.Method)
			}
		})
	}
}

// TestRecallArbitration_PeriodSeparatorDoesNotSwallowImperative is the other
// direction of the clause-boundary fix: "implement the endpoint. check that the
// response is this format" is an imperative work request whose TAIL clause reads
// as a work-status question. The whole-input match fires, so the input must be
// narrowed to its leading clause — and that only happens when '.' is a boundary.
func TestRecallArbitration_PeriodSeparatorDoesNotSwallowImperative(t *testing.T) {
	const input = "implement the endpoint. check that the response is this format"

	// Preconditions: the whole input reads as a work-status question and opens
	// with an imperative verb; only the leading clause does not.
	if !isWorkStatusRecall(input) {
		t.Fatalf("precondition: %q must read as a whole-input work-status question", input)
	}
	if !hasLeadingImperativeVerb(input) {
		t.Fatalf("precondition: %q must open with an imperative verb", input)
	}

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `{"intent":"platform","confidence":0.9}`),
	})
	intent, err := d.classifyIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil {
		t.Fatal("nil intent")
	}
	if intent.Type == string(IntentRecall) || intent.Method == "platform_recall_arbitration" {
		t.Fatalf("imperative work request swallowed into chat recall: intent=%q method=%q ('.' must be a clause boundary)",
			intent.Type, intent.Method)
	}
}
