package agent

// 02-routing TEST-GATE T1 (W8; audit-routing R1/R2; master matrix F6).
//
// The git-verb agreement veto is gated on hasLeadingImperativeVerb
// (dispatcher.go, "git verdict vetoed" branch). The lexical bias it
// defends against is SHAPE-INDEPENDENT: a small classifier scores a
// git verdict on edit-shaped wording whether or not the input opens
// with an imperative verb. These pins drive the REAL classifyIntent
// with canned git verdicts over the FROZEN usability-probe prompts
// (testdata/usability_prompts.json, verbatim from
// /tmp/usability-rig/p1_naive.py + p6_control.py - never reword).
//
// RED today: the question-shaped / declarative / non-leading-imperative
// edit inputs keep the git verdict (the veto conjunct never fires).
// GREEN after 02-routing/01 drops the shape gate.
//
// Over-veto guards run both directions: genuine git-verb inputs keep
// their verdict, work-status recall keeps its F41 priority (recall
// matchers FIRST), and the disabled-veto escape hatch stays byte-identical.

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// usabilityPromptFixture mirrors testdata/usability_prompts.json.
type usabilityPromptFixture struct {
	P1Naive   []string `json:"p1_naive"`
	P6Control []string `json:"p6_control"`
}

func loadUsabilityPrompts(t *testing.T) usabilityPromptFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/usability_prompts.json")
	if err != nil {
		t.Fatalf("read frozen usability prompts: %v", err)
	}
	var f usabilityPromptFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse frozen usability prompts: %v", err)
	}
	if len(f.P1Naive) != 4 || len(f.P6Control) != 3 {
		t.Fatalf("fixture drift: p1=%d p6=%d turns, want 4/3 (verbatim probe copy)",
			len(f.P1Naive), len(f.P6Control))
	}
	return f
}

const cannedGitVerdict = `{"intent":"git","confidence":0.91,"agent_type":"committer"}`

// vetoShapeMemCtx is the minimal memory context the classifier chain reads.
func vetoShapeMemCtx() *MemoryContext {
	return &MemoryContext{IntentCounts: map[string]int{}}
}

// requireNotGitLane asserts the canned git verdict did NOT survive: the
// chain must have continued past the veto to a non-git route.
func requireNotGitLane(t *testing.T, input string, intent *Intent) {
	t.Helper()
	if intent == nil {
		t.Fatalf("nil intent for %q: the chain produced no route", input)
	}
	if intent.Type == string(IntentGit) {
		t.Errorf("input %q kept the git verdict (method %q): the veto is gated on input shape; a git-verb-free edit request must never reach the contextless committer",
			input, intent.Method)
	}
	if intent.AgentType == config.AgentIDCommitter {
		t.Errorf("input %q routed to committer (intent %q): edit work belongs to a file-capable executor", input, intent.Type)
	}
}

// TestGitVetoShapeIndependent_FrozenP1EditPrompt is the mutation-discipline
// pin: the CURRENT git verdict SURVIVES classification of the frozen p1 T2
// edit prompt ("Now change hello.txt ...") before the fix. The prompt reads
// as an imperative to hasLeadingImperativeVerb ("now" lead-in + "change"),
// but "change" is absent from the veto's verb vocabulary, so the veto
// conjunct never fires and the git costume routes the edit to the committer.
// This subtest documents the live defect; the widening in 02-routing/01
// flips it, and the whole test then passes with zero gate edits.
func TestGitVetoShapeIndependent_FrozenP1EditPrompt(t *testing.T) {
	f := loadUsabilityPrompts(t)
	input := f.P1Naive[1]

	if inputContainsGitVerb(input) {
		t.Fatalf("fixture precondition: %q must carry no git verb", input)
	}

	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, cannedGitVerdict),
	})
	intent, err := d.classifyIntent(context.Background(), input, vetoShapeMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}

	// Mutation discipline: show the CURRENT verdict surviving on the frozen
	// edit prompt. Today this holds (the shape-gated veto lets git through);
	// after the widening the survival check itself must fail, which is the
	// documented flip - the survival arm below reports it as the defect it
	// was, while the routing arm carries the GREEN contract.
	survived := intent != nil && intent.Type == string(IntentGit)
	t.Logf("current-behavior probe: git verdict survived frozen p1 edit prompt = %v (method %q)",
		survived, func() string {
			if intent == nil {
				return "<nil>"
			}
			return intent.Method
		}())

	requireNotGitLane(t, input, intent)
}

// TestGitVetoShapeIndependent_EditShapes sweeps the shape gap: a canned git
// verdict on a git-verb-free EDIT request must be vetoed regardless of the
// input's grammatical shape - frozen probe wording, question-shaped edits,
// declarative edits, and polite/non-leading imperatives.
func TestGitVetoShapeIndependent_EditShapes(t *testing.T) {
	f := loadUsabilityPrompts(t)

	cases := []struct {
		name  string
		input string
	}{
		{"frozen_p1_edit", f.P1Naive[1]},
		{"frozen_p6_edit", f.P6Control[1]},
		{"question_shaped_edit", "Could you change hello.txt so it says 'hello meept' instead?"},
		{"declarative_edit", "The hello.txt file should say 'hello meept' instead of 'hello world'."},
		{"polite_nonleading_edit", "I would like hello.txt updated so it says 'hello meept'."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if inputContainsGitVerb(tc.input) {
				t.Fatalf("fixture precondition: %q must carry no git verb", tc.input)
			}
			d := NewDispatcher(DispatcherConfig{
				Logger:           testLogger(),
				ClassifierClient: newClassifierJSONServer(t, cannedGitVerdict),
			})
			intent, err := d.classifyIntent(context.Background(), tc.input, vetoShapeMemCtx())
			if err != nil {
				t.Fatalf("classifyIntent: %v", err)
			}
			requireNotGitLane(t, tc.input, intent)
		})
	}
}

// TestGitVetoShapeIndependent_GenuineGitKept pins the over-veto guard: the
// widening must stay an AGREEMENT check. Inputs that name a real git action
// keep their git verdict on every shape, including non-imperative ones.
func TestGitVetoShapeIndependent_GenuineGitKept(t *testing.T) {
	cases := []string{
		"commit my changes",
		"Commit the changes and push to origin.",
		"Could you commit my changes with a descriptive message?",
		"My changes are ready to commit.",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if !inputContainsGitVerb(input) {
				t.Fatalf("fixture precondition: %q must carry a git verb", input)
			}
			d := NewDispatcher(DispatcherConfig{
				Logger:           testLogger(),
				ClassifierClient: newClassifierJSONServer(t, cannedGitVerdict),
			})
			intent, err := d.classifyIntent(context.Background(), input, vetoShapeMemCtx())
			if err != nil {
				t.Fatalf("classifyIntent: %v", err)
			}
			if intent == nil || intent.Type != string(IntentGit) {
				t.Fatalf("intent = %+v, want git kept: the veto over-fired on a genuine git request", intent)
			}
		})
	}
}

// TestGitVetoShapeIndependent_RecallPriorityPreserved pins F41 branch order:
// recall matchers run FIRST. A work-status recall question with a canned git
// verdict routes to the recall lane, not to a widened veto's executor lane.
func TestGitVetoShapeIndependent_RecallPriorityPreserved(t *testing.T) {
	f := loadUsabilityPrompts(t)
	for _, input := range []string{f.P1Naive[2], f.P1Naive[3]} {
		t.Run(input, func(t *testing.T) {
			d := NewDispatcher(DispatcherConfig{
				Logger:           testLogger(),
				ClassifierClient: newClassifierJSONServer(t, cannedGitVerdict),
			})
			intent, err := d.classifyIntent(context.Background(), input, vetoShapeMemCtx())
			if err != nil {
				t.Fatalf("classifyIntent: %v", err)
			}
			if intent == nil {
				t.Fatal("nil intent")
			}
			if intent.Type != string(IntentRecall) || intent.Method != "platform_recall_arbitration" {
				t.Fatalf("intent = %q (method %q), want recall/platform_recall_arbitration: recall must outrank the veto (F41)",
					intent.Type, intent.Method)
			}
		})
	}
}

// TestGitVetoShapeIndependent_DisabledEscapeHatch pins the config escape
// hatch: GitVerbAgreementVeto=false keeps the raw verdict on the frozen
// edit prompt, so the widening's effect stays measurable by one switch.
func TestGitVetoShapeIndependent_DisabledEscapeHatch(t *testing.T) {
	f := loadUsabilityPrompts(t)
	input := f.P1Naive[1]

	vetoOff := false
	d := NewDispatcher(DispatcherConfig{
		Logger:               testLogger(),
		ClassifierClient:     newClassifierJSONServer(t, cannedGitVerdict),
		GitVerbAgreementVeto: &vetoOff,
	})
	intent, err := d.classifyIntent(context.Background(), input, vetoShapeMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent == nil || intent.Type != string(IntentGit) {
		t.Fatalf("intent = %+v, want git: with the veto disabled the raw verdict must pass through", intent)
	}
}
