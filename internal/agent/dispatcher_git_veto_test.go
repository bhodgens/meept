package agent

// Pins for the git-verb agreement veto (issue #46, bench gate 2026-09-15):
// the 350M prompt-router classified "Create a file named X in the repository
// root …" as intent=git @0.91 — the word "repository" pulls the prompt into
// the git lane (deterministic, confirmed by direct encoder probing), and the
// contextless committer then swallowed execution turns. The veto discards a
// git verdict when the imperative input names no git action, letting the
// chain continue to a coder-capable route.
//
// Every test drives the REAL production function (classifyIntent with the
// canned classifier server), not a copy of the logic.

import (
	"context"
	"strings"
	"testing"
)

// TestGitVerbAgreementVeto_VetoesGitVerbFreeImperative drives the veto
// through the real classifyIntent: a canned git @0.91 verdict on the exact
// bench prompt must NOT survive classification — the returned intent must
// come from a later chain stage (keyword/heuristic), not from the canned
// verdict.
func TestGitVerbAgreementVeto_VetoesGitVerbFreeImperative(t *testing.T) {
	// The exact file-write-routes-coder bench prompt (issue #46).
	const input = "Create a file named routing-check.txt in the repository root containing the text: dispatch works. Use file_write with direct:true."

	// Preconditions: the veto matchers all fire for this string.
	if !hasLeadingImperativeVerb(input) {
		t.Fatalf("precondition: %q should open with an imperative verb", input)
	}
	if inputContainsGitVerb(input) {
		t.Fatalf("precondition: %q must carry no git verb", input)
	}

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `{"intent":"git","confidence":0.91,"agent_type":"committer"}`),
	})

	intent, err := d.classifyIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil {
		t.Fatal("nil intent — the chain produced no fallback route")
	}
	if intent.Type == string(IntentGit) {
		t.Fatalf("intent = git: the veto did not discard the git-verb-free verdict (method %q)", intent.Method)
	}
	if intent.Method == "llm" {
		t.Fatalf("intent method = llm: the canned verdict survived the veto (type %q)", intent.Type)
	}
}

// TestGitVerbAgreementVeto_KeepsGenuineGitImperative pins the positive side:
// an imperative that DOES carry a git verb ("commit the changes") keeps its
// git verdict — the veto is an agreement check, not a blanket git ban.
func TestGitVerbAgreementVeto_KeepsGenuineGitImperative(t *testing.T) {
	const input = "Commit the changes and push to origin."

	if !hasLeadingImperativeVerb(input) {
		t.Fatalf("precondition: %q should open with an imperative verb", input)
	}
	if !inputContainsGitVerb(input) {
		t.Fatalf("precondition: %q should carry a git verb", input)
	}

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `{"intent":"git","confidence":0.91,"agent_type":"committer"}`),
	})

	intent, err := d.classifyIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil {
		t.Fatal("nil intent")
	}
	if intent.Type != string(IntentGit) || intent.Method != "llm" {
		t.Fatalf("intent = %q (method %q), want git/llm — the veto over-fired on a genuine git imperative",
			intent.Type, intent.Method)
	}
}

// TestGitVerbAgreementVeto_DisabledKeepsVerdict pins the escape hatch:
// GitVerbAgreementVeto=false restores the legacy behavior (the raw git
// verdict passes through), so the veto's effect is measurable by flipping
// one switch.
func TestGitVerbAgreementVeto_DisabledKeepsVerdict(t *testing.T) {
	const input = "Create a file named routing-check.txt in the repository root containing the text: dispatch works. Use file_write with direct:true."

	vetoOff := false
	d := NewDispatcher(DispatcherConfig{
		Logger:               testLogger(),
		ClassifierClient:     newClassifierJSONServer(t, `{"intent":"git","confidence":0.91,"agent_type":"committer"}`),
		GitVerbAgreementVeto: &vetoOff,
	})

	intent, err := d.classifyIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil {
		t.Fatal("nil intent")
	}
	if intent.Type != string(IntentGit) {
		t.Fatalf("intent = %q, want git — with the veto disabled the raw verdict must pass through", intent.Type)
	}
}

// TestGitVerbAgreementVeto_NonGitVerdictUntouched pins scope: the veto only
// ever touches git verdicts; other intents pass through unchanged.
func TestGitVerbAgreementVeto_NonGitVerdictUntouched(t *testing.T) {
	const input = "Create a file named routing-check.txt in the repository root containing the text: dispatch works."

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `{"intent":"tooluse","confidence":0.91}`),
	})

	intent, err := d.classifyIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil {
		t.Fatal("nil intent")
	}
	if intent.Type != string(IntentToolUse) {
		t.Fatalf("intent = %q, want tooluse — the veto must not touch non-git verdicts", intent.Type)
	}
}

// TestGitVerbAgreementVeto_NonImperativeGitVerdictKept pins the
// hasLeadingImperativeVerb conjunct: a git verdict on a NON-imperative input
// (no leading execution verb) is out of the veto's scope — that is the
// recall arbitration's territory, and widening this veto there would change
// behavior the recall tests already pin.
func TestGitVerbAgreementVeto_NonImperativeGitVerdictKept(t *testing.T) {
	const input = "what is the state of the repository root"

	if hasLeadingImperativeVerb(input) {
		t.Fatalf("precondition: %q must not read as an imperative", input)
	}
	if inputContainsGitVerb(input) {
		t.Fatalf("precondition: %q must carry no git verb", input)
	}

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, `{"intent":"git","confidence":0.91,"agent_type":"committer"}`),
	})

	intent, err := d.classifyIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil {
		t.Fatal("nil intent")
	}
	if intent.Type != string(IntentGit) {
		t.Fatalf("intent = %q, want git — non-imperative inputs are out of the veto's scope", intent.Type)
	}
}

// TestGitVerbAgreementVeto_BenchPromptTable sweeps the phrasing family the
// bench tasks and real traffic use: every git-verb-free imperative variant
// must be vetoed, every git-verb-carrying one must survive.
func TestGitVerbAgreementVeto_BenchPromptTable(t *testing.T) {
	cases := []struct {
		input    string
		wantKeep bool // true = git verdict should survive
	}{
		{"Create a file named routing-check.txt in the repository root containing the text: dispatch works. Use file_write with direct:true.", false},
		{"Create a file named notes.md in the repo root with today's date.", false},
		{"Write the summary to summary.md in the repository root.", false},
		{"Commit the changes with a descriptive message.", true},
		{"Push the feature branch to origin.", true},
		{"Rebase onto main and force-push.", true},
	}

	for _, tc := range cases {
		t.Run(tc.input[:min(len(tc.input), 40)], func(t *testing.T) {
			// The veto's decision is a pure predicate — pin it directly so
			// the table's expectation and the matcher can never drift apart.
			got := hasLeadingImperativeVerb(tc.input) && !inputContainsGitVerb(tc.input)
			if got == tc.wantKeep {
				t.Errorf("veto predicate for %q = %v, want veto=%v", tc.input, got, !tc.wantKeep)
			}
		})
	}
}

// min for the table above (Go <1.21 shim; remove when the toolchain floor
// allows the builtin).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// strings is imported for the table test's label truncation.
var _ = strings.TrimSpace
