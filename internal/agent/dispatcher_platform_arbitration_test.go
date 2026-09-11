package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// Pins for the e2e run-2 failures (2026-09-10): the 8B classifier scored
// "create a file named hello.txt…" as intent=platform @0.9 and the platform
// branch dumped the agent roster (A2 roster failure) — an execution request
// answered with an introspection page.

func TestHasLeadingImperativeVerb(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "create imperative", input: "create a file named hello.txt in the current directory containing the word hello, then tell me the full path", want: true},
		{name: "write imperative", input: "Write a function that sorts a list", want: true},
		{name: "make imperative", input: "make it beep when it opens", want: true},
		{name: "fix imperative", input: "fix the login bug", want: true},
		{name: "polite lead-in", input: "please create a branch for this", want: true},
		{name: "introspection question", input: "what can you do?", want: false},
		{name: "capabilities question", input: "what are your capabilities", want: false},
		{name: "tools question", input: "what tools do you have access to", want: false},
		{name: "verb not leading", input: "what can you do to create a file?", want: false},
		{name: "empty", input: "", want: false},
		{name: "greeting", input: "hello there", want: false},
		{name: "status question", input: "is this working?", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasLeadingImperativeVerb(tt.input); got != tt.want {
				t.Errorf("hasLeadingImperativeVerb(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// The arbitration helper must agree with heuristicFallback's own code-keyword
// list on the canonical create-a-file phrasing, so that after the platform
// verdict is discarded the keyword/heuristic chain reliably picks up the
// code intent (no routing gap between the two layers).
func TestPlatformArbitration_FallbackChainCatchesCreateFile(t *testing.T) {
	input := "create a file named hello.txt in the current directory containing the word hello, then tell me the full path"

	if !hasLeadingImperativeVerb(input) {
		t.Fatal("precondition: input is imperative; arbitration would not fire")
	}

	intent := heuristicFallback(input)
	if intent == nil {
		t.Fatal("heuristicFallback returned nil for create-a-file phrasing; platform arbitration would leave nothing to route")
	}
	if intent.Type != string(IntentCode) {
		t.Errorf("heuristicFallback intent = %q, want code", intent.Type)
	}
	if intent.AgentType != config.AgentIDCoder {
		t.Errorf("heuristicFallback agent = %q, want coder", intent.AgentType)
	}
}

// Keyword classifier parity: "create a file" must classify code even before
// heuristicFallback is reached.
func TestPlatformArbitration_KeywordClassifierCatchesCreateFile(t *testing.T) {
	kc := &KeywordClassifier{}
	intent, err := kc.Classify(nil, "create a file named hello.txt containing hello", nil)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if intent == nil {
		t.Fatal("keyword classifier returned nil for create-a-file phrasing")
	}
	if intent.Type != string(IntentCode) {
		t.Errorf("keyword intent = %q, want code", intent.Type)
	}
	if !strings.Contains(strings.ToLower("create a file"), "create a file") {
		t.Fatal("sanity: substring check broken")
	}
}

// Run-5 T4 (2026-09-10): "what files did you make for me?" scored
// platform @0.9 → roster dump. A second-person past-tense work question is
// recall about the assistant's OWN actions, not platform introspection.
func TestIsSecondPersonWorkRecall(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "run-5 T4 exact", input: "what files did you make for me?", want: true},
		{name: "did you create", input: "did you create the file?", want: true},
		{name: "what did you write", input: "what did you write?", want: true},
		{name: "have you fixed", input: "have you fixed the bug yet", want: true},
		{name: "where did you put it", input: "where did you put the file", want: true},
		// Not recall:
		{name: "introspection", input: "what can you do?", want: false},
		{name: "hypothetical", input: "what files should I make?", want: false},
		{name: "third person", input: "what files did the team make?", want: false},
		{name: "greeting", input: "hello", want: false},
		{name: "empty", input: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSecondPersonWorkRecall(tt.input); got != tt.want {
				t.Errorf("isSecondPersonWorkRecall(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// Run-7 T3 (2026-09-11, rkl3Th): "did the change get made? where is the
// file?" scored platform @0.9 → introspection → catalog fallback reply; A5
// continuity failed even though the session digest carried T1's artifact.
// A yes/no WORK-STATUS question (no "you" required — the user asks about
// the work, not the assistant) is recall, never platform introspection.
func TestIsWorkStatusRecall(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "run-7 T3 exact", input: "did the change get made? where is the file?", want: true},
		{name: "did the file get created", input: "did the file get created?", want: true},
		{name: "was the change made", input: "was the change made?", want: true},
		{name: "is the task done", input: "is the task done?", want: true},
		{name: "did it work", input: "did it work?", want: true},
		{name: "are the files there", input: "are the files there", want: true},
		{name: "has the edit landed", input: "has the edit landed", want: true},
		// Not work-status:
		{name: "bare is it", input: "is it?", want: false},
		{name: "introspection", input: "is the platform up?", want: false},
		{name: "imperative", input: "create a file named hello.txt", want: false},
		{name: "greeting", input: "hello", want: false},
		{name: "empty", input: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWorkStatusRecall(tt.input); got != tt.want {
				t.Errorf("isWorkStatusRecall(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// "is the platform up?" must stay FALSE (introspection) while "is the task
// done?" is TRUE (recall) — the work-noun window is what separates them.
func TestIsWorkStatusRecall_WorkNounRequired(t *testing.T) {
	if isWorkStatusRecall("is the platform up") {
		t.Error("platform introspection matched as work-status recall")
	}
}

// Run-10 (2026-09-11 YJ7oSn): the 8B scored the same T3 phrasing
// intent=git @0.9 — a git verdict with no git verb is not credible; the
// recall arbitration extends to it. The verb check is what keeps real git
// requests ("commit the changes", "merge the branch") routable to the
// committer even when they also contain work-status nouns.
func TestInputContainsGitVerb(t *testing.T) {
	no := []string{
		"did the change get made? where is the file?",
		"was the file created?",
		"what files did you make for me?",
		"create a file named hello.txt",
		"",
	}
	for _, in := range no {
		if inputContainsGitVerb(in) {
			t.Errorf("inputContainsGitVerb(%q) = true, want false", in)
		}
	}
	yes := []string{
		"commit the changes",
		"push to origin",
		"merge the branch",
		"rebase onto main",
		"checkout the feature branch",
		"stash my work",
	}
	for _, in := range yes {
		if !inputContainsGitVerb(in) {
			t.Errorf("inputContainsGitVerb(%q) = false, want true", in)
		}
	}
}

// Run-8 (2026-09-10): the 8B scored the SAME T1 phrasing intent=schedule
// @0.8 with zero time references. A schedule verdict without time signals
// is not credible; the arbitration extends to it.
func TestHasTimeSignal(t *testing.T) {
	yes := []string{
		"remind me to call mom tomorrow",
		"set a timer for 5 minutes",
		"schedule a meeting at 3pm",
		"create a file next week",
		"alarm for 7 am",
	}
	no := []string{
		"create a file named hello.txt in the current directory containing the word hello, then tell me the full path",
		"fix the login bug",
		"what files did you make for me?",
	}
	for _, s := range yes {
		if !hasTimeSignal(s) {
			t.Errorf("hasTimeSignal(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if hasTimeSignal(s) {
			t.Errorf("hasTimeSignal(%q) = true, want false", s)
		}
	}
}
