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
		// Version/abbreviation periods are points INSIDE a token, not clause
		// boundaries (wave-3 regression review of 9af23f86): the period
		// between "1" and "2" used to cut the clause to "run the check on
		// v1", moving a recall predicate out of the judged clause, and the
		// sentence period after a version was confused with the version's
		// own points.
		{"run the check on v1.2 is the file created?", "run the check on v1.2 is the file created"},
		{"deploy 1.2. did the change get made?", "deploy 1.2"},
		{"roll out v2.0.10. was the file created?", "roll out v2.0.10"},
		{"check e.g. the file, did the change get made?", "check e.g. the file"},
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

// recallArbitrationVerdict drives the REAL classifyIntent chain with a canned
// verdict for that input and returns whatever the arbitration produced. The
// arbitration — not the classifier — decides the route.
func recallArbitrationVerdict(t *testing.T, input, verdict string) *Intent {
	t.Helper()
	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `{"intent":"`+verdict+`","confidence":0.9}`),
	})
	intent, err := d.classifyIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if err != nil {
		t.Fatalf("classifyIntent(%q): %v", input, err)
	}
	if intent == nil {
		t.Fatalf("classifyIntent(%q) returned nil", input)
	}
	return intent
}

// assertRecallOverride requires the session-aware recall route.
func assertRecallOverride(t *testing.T, input, verdict string) {
	t.Helper()
	intent := recallArbitrationVerdict(t, input, verdict)
	if intent.Type != string(IntentRecall) || intent.Method != "platform_recall_arbitration" {
		t.Fatalf("%s verdict on %q: intent = %q (method %q), want recall via platform_recall_arbitration — the recall match was narrowed away and the untrusted verdict survived",
			verdict, input, intent.Type, intent.Method)
	}
}

// assertNotRecall requires the recall match to have been narrowed away.
func assertNotRecall(t *testing.T, input, verdict string) {
	t.Helper()
	intent := recallArbitrationVerdict(t, input, verdict)
	if intent.Type == string(IntentRecall) || intent.Method == "platform_recall_arbitration" {
		t.Fatalf("%s verdict on %q: intent = %q (method %q), want the narrowing to keep the recall route off (the imperative work request was swallowed)",
			verdict, input, intent.Type, intent.Method)
	}
}

// TestRecallArbitration_SeparatorBeforeStatusPhraseStillRecalls pins the
// arbitration reversal. Each of these is a genuine recall/report question whose
// status phrase sits AFTER a conversational separator. The wave-2 narrowing
// judged only the leading clause, so the recall match died and the untrusted
// platform verdict survived. The classifier returns platform for every input,
// so the arbitration — not the classifier — decides.
//
// Wave-3 review of 9af23f86: this pin used to FATAL whenever an input opened
// with an imperative verb, which excluded exactly the pronoun-lead-in family
// the narrowing still broke ("update me, did the file get created?") and left
// the rows it did cover unable to detect that breakage. An imperative head is
// now admitted when it is a recall lead-in (pronoun object) or when the status
// question has its own clause; those rows are part of the table.
func TestRecallArbitration_SeparatorBeforeStatusPhraseStillRecalls(t *testing.T) {
	for _, input := range []string{
		"hey, did the change get made?",
		"quick question, is the task done?",
		"ok, what files did you create?",
		"did the build pass, and was the file created?",
		"check the log at /tmp/x,y.log, did the change get made?",
		"what files did you make for me? where is the file?",
		// Pronoun lead-ins: the imperative-looking head IS the recall
		// request's own lead-in, so narrowing to it deletes the question.
		"update me, did the file get created?",
		"please update me, did the file get created?",
		"run me through it, did the change get made?",
		"build me a summary, was the file created?",
		"make me a report, did the change get made?",
		// Non-pronoun object, but the status question is its own clause.
		"fix the test, did the file get created?",
		"set up the report, is the task done?",
	} {
		t.Run(input, func(t *testing.T) {
			// Precondition: the whole input IS a recall question, and nothing
			// licenses narrowing it away: the head is not an imperative, or
			// the head is the recall request's own lead-in (pronoun object),
			// or the status question forms its own clause. A row whose head
			// is a plain imperative with no status clause of its own would
			// legitimately narrow.
			if !isWorkStatusRecall(input) && !isSecondPersonWorkRecall(input) {
				t.Fatalf("precondition: %q must read as a whole-input recall question", input)
			}
			if hasLeadingImperativeVerb(input) && !hasPronounObjectImperativeHead(input) && !hasInterrogativeRecallClause(input) {
				t.Fatalf("precondition: %q opens with an imperative head that is not a recall lead-in and carries no status clause of its own; narrowing would be correct", input)
			}
			assertRecallOverride(t, input, "platform")
		})
	}
}

// TestRecallArbitration_PronounLeadInKeepsRecall is the item-level pin for the
// wave-3 recall loss (9af23f86) on BOTH untrusted arms — git, which is the
// worse one: the imperative salvage covers only platform/schedule, so a
// surviving git verdict reaches async git dispatch and the committer runs
// contextless. Each row here opened with an execution verb (update/run/build/
// make/fix/set) whose object is the recall request's own audience, so the old
// gate narrowed to "update me" / "fix the test" and the verdict survived. The
// rows with a non-pronoun object ("fix the test", "set up the report") are
// kept by the second rule: their status question forms its own clause.
func TestRecallArbitration_PronounLeadInKeepsRecall(t *testing.T) {
	for _, input := range []string{
		"update me, did the file get created?",
		"please update me, did the file get created?",
		"run me through it, did the change get made?",
		"build me a summary, was the file created?",
		"make me a report, did the change get made?",
		"fix the test, did the file get created?",
		"set up the report, is the task done?",
	} {
		for _, verdict := range []string{"git", "platform", "schedule"} {
			t.Run(verdict+"/"+input, func(t *testing.T) {
				// Preconditions: the whole input is a recall question, and
				// the narrowing predicate itself must refuse (this is the
				// unit-level half of the pin — the old gate narrowed on
				// hasLeadingImperativeVerb alone, which is true here).
				if !isWorkStatusRecall(input) && !isSecondPersonWorkRecall(input) {
					t.Fatalf("precondition: %q must read as a whole-input recall question", input)
				}
				switch verdict {
				case "git":
					if inputContainsGitVerb(input) {
						t.Fatalf("precondition: %q must carry no git verb, or the git arm never arbitrates", input)
					}
				case "schedule":
					if hasTimeSignal(input) {
						t.Fatalf("precondition: %q must carry no time signal, or the schedule arm never arbitrates", input)
					}
				}
				if narrowsRecallToLeadingClause(input) {
					t.Fatalf("narrowsRecallToLeadingClause(%q) = true, want false: this head is a recall lead-in, and narrowing to %q deletes the question",
						input, leadingRecallClause(input))
				}
				assertRecallOverride(t, input, verdict)
			})
		}
	}
}

// TestRecallArbitration_CommonImperativeVerbsAreRecognized pins the second
// wave-3 defect: the recall narrowing keyed on hasLeadingImperativeVerb's
// hand-maintained 22-verb list, so any imperative verb the list forgot
// (check/test/verify/review/look/tell/show/remind/validate/inspect/…) left the
// narrowing off and the imperative work request stayed swallowed as a
// work-status question. Recognition is structural now (a closed class of
// non-imperative openers, so no verb can be missing); this table is the pin
// that would have caught the old membership test — it fails for every verb
// missing from such a list.
func TestRecallArbitration_CommonImperativeVerbsAreRecognized(t *testing.T) {
	for _, verb := range []string{
		"check", "test", "verify", "review", "look", "tell", "show", "remind",
		"validate", "inspect", "implement", "refactor", "audit", "benchmark",
		"profile", "summarize", "document", "wire", "port", "migrate",
		"upgrade", "trace", "debug", "lint", "format",
	} {
		input := verb + " the endpoint. check that the response is this format"
		t.Run(verb, func(t *testing.T) {
			// Precondition: the tail clause alone is what makes the whole
			// input read as a work-status question.
			if !isWorkStatusRecall(input) {
				t.Fatalf("precondition: %q must read as a whole-input work-status question", input)
			}
			if !hasImperativeHead(input) {
				t.Fatalf("hasImperativeHead(%q) = false; %q is missing from the imperative recognition", input, verb)
			}
			if !narrowsRecallToLeadingClause(input) {
				t.Fatalf("narrowsRecallToLeadingClause(%q) = false, want true: this is an imperative work request whose status clause is in the tail", input)
			}
			assertNotRecall(t, input, "platform")
		})
	}
}

// TestRecallArbitration_VersionPeriodDoesNotTruncateClause pins item 3 of the
// wave-3 review: the period separator used to cut INSIDE a version/decimal
// token, so "run the check on v1.2 is the file created?" was judged as
// "run the check on v1" — the recall predicate fell outside the narrowed
// clause and a git verdict survived into the contextless committer. The
// abbreviation shapes ("e.g.") are the same class of false boundary.
func TestRecallArbitration_VersionPeriodDoesNotTruncateClause(t *testing.T) {
	// wantLeading pins the clause cut as well: a version token is never split
	// at its own decimal point, and an abbreviation's period is not a
	// boundary either.
	for _, row := range []struct{ input, wantLeading string }{
		{"run the check on v1.2 is the file created?", "run the check on v1.2 is the file created"},
		{"deploy 1.2. did the change get made?", "deploy 1.2"},
		{"roll out v2.0.10. was the file created?", "roll out v2.0.10"},
		{"check e.g. the file, did the change get made?", "check e.g. the file"},
	} {
		t.Run(row.input, func(t *testing.T) {
			if !isWorkStatusRecall(row.input) && !isSecondPersonWorkRecall(row.input) {
				t.Fatalf("precondition: %q must read as a whole-input recall question", row.input)
			}
			if got := leadingRecallClause(row.input); got != row.wantLeading {
				t.Fatalf("leadingRecallClause(%q) = %q, want %q — a version/abbreviation period is not a clause boundary", row.input, got, row.wantLeading)
			}
			if inputContainsGitVerb(row.input) {
				t.Fatalf("precondition: %q must carry no git verb, or the git arm never arbitrates", row.input)
			}
			// The git arm is the one that reached the contextless committer.
			assertRecallOverride(t, row.input, "git")
			assertRecallOverride(t, row.input, "platform")
		})
	}
}

// TestRecallArbitration_PeriodSeparatorDoesNotSwallowImperative is the other
// direction of the clause-boundary fix: "implement the endpoint. check that the
// response is this format" is an imperative work request whose TAIL clause reads
// as a work-status question. The whole-input match fires, so the input must be
// narrowed to its leading clause — and that only happens when '.' is a boundary.
//
// Wave-3 review of 9af23f86: this pin drove only a PLATFORM verdict, so it
// stayed green while the git arm of the same gate had the bug, and only
// platform/schedule were salvaged afterwards. Every verdict arm runs now, so a
// regression in the git arm cannot hide behind the platform arm.
func TestRecallArbitration_PeriodSeparatorDoesNotSwallowImperative(t *testing.T) {
	const input = "implement the endpoint. check that the response is this format"

	// Preconditions: the whole input reads as a work-status question and opens
	// with an imperative head; only the leading clause does not narrow, and
	// the input carries no time signal (so the schedule arm also arbitrates).
	if !isWorkStatusRecall(input) {
		t.Fatalf("precondition: %q must read as a whole-input work-status question", input)
	}
	if !hasLeadingImperativeVerb(input) {
		t.Fatalf("precondition: %q must open with an imperative verb", input)
	}
	if !narrowsRecallToLeadingClause(input) {
		t.Fatalf("precondition: %q must be narrowed to its leading clause", input)
	}
	if hasTimeSignal(input) {
		t.Fatalf("precondition: %q must carry no schedule time signal", input)
	}

	for _, verdict := range []string{"platform", "git", "schedule"} {
		t.Run(verdict, func(t *testing.T) {
			assertNotRecall(t, input, verdict)
		})
	}
}
