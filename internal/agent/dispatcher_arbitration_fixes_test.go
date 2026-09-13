package agent

// Pins for the 2026-09-12 routing-arbitration findings:
//   F40 - inputContainsGitVerb matched substrings ("emergency" contains
//         "merge"), so a git verdict wrongly survived recall arbitration.
//   F41 - the leading-imperative branch was tested before the recall
//         branch, so "update me: did the file get created?" never reached
//         the recall route (16f1f8a2/c6e6f336's fix).
//   F42 - the compound collapse counted IntentSearch/IntentAnalyze as
//         chat-like, so "search the web for X and write it to notes.md"
//         collapsed and the search half was silently dropped.
//   F95 - the recall override bypassed recordAgent/recordIntentType.
//
// Every test drives the REAL production function, not a copy.

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestInputContainsGitVerb_WordBoundaries pins F40: git verbs match only as
// whole words (inflections allowed), so ordinary English that merely
// contains the letters is not mistaken for a git action.
func TestInputContainsGitVerb_WordBoundaries(t *testing.T) {
	// Negatives: ordinary English that merely contains the letters of a git
	// verb. One-sided by construction — a positive case must never live in
	// this table (the old table smuggled "did you merge the notes …" in and
	// skipped it, so the table silently stopped testing anything about it).
	for _, in := range []string{
		"did the emergency change get made? where is the file?",
		"what emerged from that session?",
		"was the file created?",
		"the commitment we made",
	} {
		if inputContainsGitVerb(in) {
			t.Errorf("inputContainsGitVerb(%q) = true, want false (substring false positive, F40)", in)
		}
	}
	// Positives: real git verbs, including the exact run-10 work-status
	// string that carries none, plus inflected forms the guard must keep.
	// The vowel-dropping -ing forms ("merging", "rebasing") and the
	// "uncommitted" prefix were the F40 fix's over-narrowing: the bare stem
	// + "ing" suffix matched only the vowel-keeping shape, so these read as
	// git-verb-free and a git recall verdict wrongly survived arbitration.
	for _, in := range []string{
		"commit the changes",
		"push to origin",
		"merge the branch",
		"rebase onto main",
		"checkout the feature branch",
		"stash my work",
		"did the change get committed yet?",
		"is the branch merged?",
		"did you merge the notes into the summary",
		"merging the branch",
		"rebasing onto main",
		"are there uncommitted changes in the tree?",
	} {
		if !inputContainsGitVerb(in) {
			t.Errorf("inputContainsGitVerb(%q) = false, want true", in)
		}
	}
	// The run-10 work-status question (no git verb) must NOT look like git.
	if inputContainsGitVerb("did the change get made? where is the file?") {
		t.Error("the run-10 work-status question must not be read as containing a git verb")
	}
}

// TestRecallArbitration_ImperativePrefixStillRecalls pins F41: a
// status/recall question with an imperative-looking first token outranks the
// imperative override and lands on the session-aware recall route. The
// canned classifier returns schedule, so the arbitration (not the classifier)
// decides the route.
func TestRecallArbitration_ImperativePrefixStillRecalls(t *testing.T) {
	const input = "update me: did the file get created?"

	// Preconditions: both matchers fire for this string, which is exactly
	// why branch ORDER decides the outcome.
	if !hasLeadingImperativeVerb(input) {
		t.Fatalf("precondition: %q should open with an imperative verb", input)
	}
	if !isWorkStatusRecall(input) {
		t.Fatalf("precondition: %q should be a work-status question", input)
	}
	if hasTimeSignal(input) {
		t.Fatalf("precondition: %q must carry no schedule time signal", input)
	}

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `{"intent":"schedule","confidence":0.8}`),
	})

	intent, err := d.classifyIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil {
		t.Fatal("nil intent")
	}
	if intent.Type != string(IntentRecall) {
		t.Fatalf("intent = %q (method %q), want recall — the imperative branch swallowed the recall route (F41)",
			intent.Type, intent.Method)
	}
	if intent.Method != "platform_recall_arbitration" {
		t.Errorf("method = %q, want platform_recall_arbitration", intent.Method)
	}

	// F95: the recall override must feed the shared recording tail.
	stats := d.GetStats()
	if stats.ByMethod["platform_recall_arbitration"] == 0 {
		t.Error("classification method not recorded for the recall override")
	}
	if stats.ByAgent[config.AgentIDChat] == 0 {
		t.Error("recall override did not record the chat agent: by_agent under-counts this arbitration (F95)")
	}
	if stats.ByIntent[string(IntentRecall)] == 0 {
		t.Error("recall override did not record the recall intent: by_intent under-counts this arbitration (F95)")
	}
}

// TestRecallArbitration_GenuineImperativeStillOverridden is the negative
// control for F41: a real imperative platform verdict is still discarded
// (the reorder must not revive the A2 roster-dump failure).
func TestRecallArbitration_GenuineImperativeStillOverridden(t *testing.T) {
	const input = "create a file named hello.txt in the current directory containing the word hello"

	if !hasLeadingImperativeVerb(input) {
		t.Fatalf("precondition: %q should open with an imperative verb", input)
	}
	if isWorkStatusRecall(input) || isSecondPersonWorkRecall(input) {
		t.Fatalf("precondition: %q must not be a recall question", input)
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
	if intent.Type == string(IntentPlatform) {
		t.Fatalf("platform verdict on an imperative survived arbitration: A2 roster-dump regression")
	}
	// The platform verdict is discarded and the chain falls through to the
	// keyword/heuristic layer, which routes the verb to an executor.
	if intent.Type != string(IntentCode) {
		t.Errorf("intent = %q, want code from the fallback chain after the platform verdict is discarded", intent.Type)
	}
}

// TestClassifyMultiIntent_SearchPlusWriteStaysCompound pins F42: a search
// lane paired with a write lane is a genuine two-item request and must not
// collapse (the previous chat-like arm swallowed the search).
func TestClassifyMultiIntent_SearchPlusWriteStaysCompound(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `[{"intent":"search","confidence":0.9},{"intent":"write","confidence":0.9}]`),
	})
	const input = "search the web for the latest Go release notes and write them all into notes.md so I can read them later"

	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if !multi.IsCompound {
		t.Fatalf("search+write collapsed to a single intent; the search half is dropped (F42). intents=%s",
			intentsDebug(multi.Intents))
	}
	if multi.CompoundType == "" {
		t.Error("compound but CompoundType is empty")
	}
}

// TestClassifyMultiIntent_AnalyzePlusWriteStaysCompound pins the second half
// of F42: analyze is a work lane (routes to the analyst), not a tag-along.
func TestClassifyMultiIntent_AnalyzePlusWriteStaysCompound(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `[{"intent":"analyze","confidence":0.9},{"intent":"write","confidence":0.9}]`),
	})
	const input = "analyze the tradeoffs between the two caching designs and write a short summary into design-notes.md now"

	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if !multi.IsCompound {
		t.Fatalf("analyze+write collapsed to a single intent; the analyze half is dropped (F42). intents=%s",
			intentsDebug(multi.Intents))
	}
}

// TestClassifyMultiIntent_WorkPlusChatStillCollapses is the F42 negative
// control: a work intent with a genuinely conversational tag-along still
// collapses (the documented work-plus-report behavior is preserved).
func TestClassifyMultiIntent_WorkPlusChatStillCollapses(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `[{"intent":"code","confidence":0.9},{"intent":"chat","confidence":0.8}]`),
	})
	const input = "create a file named hello.txt in the current directory and then tell me the full path to it please now"

	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if multi.IsCompound {
		t.Fatalf("code+chat should collapse to the single work intent; got compound. intents=%s",
			intentsDebug(multi.Intents))
	}
}

// TestRecallArbitration_StatusTailDoesNotSwallowImperative pins the
// regression the F41 reorder introduced. The recall branch now runs BEFORE
// the imperative branch, and the recall matchers are position-independent
// (isWorkStatusRecall scans every field for a status predicate plus a nearby
// work noun). So an IMPERATIVE work request whose status clause sits in the
// tail satisfied the recall matcher and was arbitrated to chat recall — the
// work was dropped. The recall matchers must judge the LEADING clause only.
func TestRecallArbitration_StatusTailDoesNotSwallowImperative(t *testing.T) {
	inputs := []string{
		"implement the endpoint and check that the response is this format",
		"refactor the module and let me know if there are changes to the API",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			// Preconditions: the WHOLE input is a work-status question (the
			// position-independent match the regression exploited) and opens
			// with an imperative verb; the LEADING clause alone is neither.
			if !isWorkStatusRecall(input) {
				t.Fatalf("precondition: %q must read as a whole-input work-status question", input)
			}
			if !hasLeadingImperativeVerb(input) {
				t.Fatalf("precondition: %q must open with an imperative verb", input)
			}
			leading := leadingRecallClause(input)
			if isWorkStatusRecall(leading) || isSecondPersonWorkRecall(leading) {
				t.Fatalf("precondition: the leading clause %q of %q must not be a recall question", leading, input)
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
				t.Fatalf("imperative work request swallowed into chat recall: intent=%q method=%q", intent.Type, intent.Method)
			}
		})
	}
}
