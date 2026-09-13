package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// Pins for the e2e T1 compound-misroute fix (2026-09-10).
//
// Run 1 failure chain: "create a file named hello.txt …, then tell me the
// full path" carried the " then " compound-signal word, the multi-intent
// classifiers returned [code, chat], DetectCompound fired, the request
// entered the spec_pair flow, the (then-unwired) pair manager failed, and
// the silent fallback executed a contextless step on the chat agent — the
// reply was "I understand. How may I assist you today?" and no file was
// ever created.
//
// Every test below drives the REAL production function
// (Dispatcher.classifyMultiIntent / DetectCompound / routeCompoundWithModel).
// An earlier revision of this file defined a private
// collapseWorkPlusReport copy of the arbitration core: it asserted the
// OPPOSITE rule (Report/Search/Analyze counted as chat-like), had no
// production caller, and stayed green if the shipped collapse was deleted.
// The copy is gone — a green test here must mean the production collapse
// still behaves.

// newCompoundTestDispatcher drives the multi-intent path with ONLY the LLM
// verdict: the keyword classifier is cleared so its own half-confidence
// matches cannot blur which producer decided the collapse.
func newCompoundTestDispatcher(t *testing.T, verdict string) *Dispatcher {
	t.Helper()
	d := NewDispatcher(DispatcherConfig{
		Logger:           testLogger(),
		ClassifierClient: newClassifierJSONServer(t, verdict),
	})
	if d.llmClassifier == nil {
		t.Fatal("dispatcher built without an LLM classifier")
	}
	d.keywordClassifier = nil
	return d
}

// Layer 1: the collapse shape (one work intent + chat tag-along) collapses
// to single-intent routing, through the real classifyMultiIntent.
func TestClassifyMultiIntent_WorkPlusChatCollapses(t *testing.T) {
	d := newCompoundTestDispatcher(t, `[{"intent":"code","confidence":0.9},{"intent":"chat","confidence":0.8}]`)

	// Same shape the multi-intent classifier produced for e2e T1 run 1:
	// code for "create a file…", chat for the "tell me" tail. Long enough
	// (>= compoundKeywordThreshold) and carrying " then ".
	const input = "create a file named hello.txt in the current directory containing the word hello, then tell me the full path to it please"
	if !hasCompoundSignalWords(input) {
		t.Fatalf("precondition: %q has no compound signal", input)
	}

	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if multi.IsCompound {
		t.Fatalf("code+chat must collapse to the single work intent; got compound. intents=%s",
			intentsDebug(multi.Intents))
	}
	if multi.CompoundType != "" {
		t.Errorf("CompoundType = %q after collapse, want empty", multi.CompoundType)
	}
}

// Layer 1b: genuine multi-work requests never collapse.
func TestClassifyMultiIntent_TwoWorkIntentsStayCompound(t *testing.T) {
	d := newCompoundTestDispatcher(t, `[{"intent":"code","confidence":0.9},{"intent":"debug","confidence":0.8}]`)

	const input = "refactor the module and then debug the failing tests in the repository so the whole suite is green"
	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})

	if !multi.IsCompound {
		t.Fatalf("code+debug is a genuine two-work compound and must not collapse. intents=%s",
			intentsDebug(multi.Intents))
	}
}

// Layer 1c: sub-threshold intents do not count toward the chat-like tally — a
// strong work intent + a weak (below DetectCompound's own 0.5 floor) chat
// detection leaves two work intents compound.
func TestClassifyMultiIntent_SubthresholdChatDoesNotCollapse(t *testing.T) {
	d := newCompoundTestDispatcher(t,
		`[{"intent":"code","confidence":0.9},{"intent":"chat","confidence":0.3},{"intent":"git","confidence":0.8}]`)

	const input = "refactor the module and then commit the changes to the repository so the branch is up to date"
	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})

	if !multi.IsCompound {
		t.Fatalf("sub-threshold chat must not trigger the collapse; code+git stay compound. intents=%s",
			intentsDebug(multi.Intents))
	}
}

// Layer 1d (integration through classifyMultiIntent): without an LLM
// classifier the keyword arms alone decide; a pure work-plus-report phrasing
// whose keyword intents are all below the 0.5 floor must NOT become compound
// (this is the deterministic half of the run-1 misroute: keyword confidence
// halving left [code@0.4, chat@0.3], and only the LLM's high-confidence chat
// tag-along pushed it over the edge — the collapse layer absorbs exactly
// that shape when it happens).
func TestClassifyMultiIntent_KeywordOnlyWorkPlusReport(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{})

	input := "create a file named hello.txt in the current directory containing the word hello, then tell me the full path"

	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{})

	if multi.IsCompound {
		t.Fatalf("keyword-only work-plus-report classified compound; intents=%s", intentsDebug(multi.Intents))
	}
	if multi.CompoundType != "" {
		t.Errorf("CompoundType = %q, want empty", multi.CompoundType)
	}
}

// Layer 2: a compound dispatch preserves the FULL user input on the task —
// the step executor's prompt is built from the task description, so a
// truncated ~100-char summary starves the executor of the second clause.
// routeCompoundWithModel is invoked through the dispatch pipeline seam used
// by dispatcher_test.go.
func TestRouteCompoundWithModel_FullInputPreserved(t *testing.T) {
	full := "write a haiku about the sea and also write a limerick about mountains"

	d := NewDispatcher(DispatcherConfig{})

	multi := &MultiIntent{
		Intents: []*Intent{
			{Type: string(IntentWrite), AgentType: config.AgentIDWriter, Confidence: 0.8},
			{Type: string(IntentWrite), AgentType: config.AgentIDWriter, Confidence: 0.6},
		},
		Summary:      extractSummary(full),
		CompoundType: "parallel",
	}
	multi.IsCompound = true

	result, err := d.routeCompoundWithModel(context.Background(), multi, full, "sess-test", nil)
	if err != nil {
		t.Fatalf("routeCompoundWithModel: %v", err)
	}
	if result.Task == nil {
		t.Fatal("nil parent task on compound result")
	}
	if result.Task.Description != full {
		t.Errorf("task description = %q, want full input %q", result.Task.Description, full)
	}
	if result.OriginalInput != full {
		t.Errorf("OriginalInput = %q, want %q", result.OriginalInput, full)
	}
}

func intentsDebug(intents []*Intent) string {
	var sb strings.Builder
	for i, in := range intents {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(in.Type + ":" + in.AgentType)
	}
	return sb.String()
}

// TestDetectCompound_CodePlusWriteStaysParallel pins the CompoundType
// regression the C-0 wave introduced. DetectCompound keyed "sequential" off
// Intent.RequiresPlanning, and the LLM classifier now derives that flag from
// IntentType.RequiresPlanning() — which is true for `code` (the
// async-dispatch rule). So an independent code+write pair flipped from
// parallel to sequential, and the value rides CompoundType → the compound
// task metadata → the strategic planner's PlanRequest. Sequential is the
// PLANNING lanes' shape only.
func TestDetectCompound_CodePlusWriteStaysParallel(t *testing.T) {
	// Exactly what the LLM multi-intent producer emits post-C-0: code now
	// carries RequiresPlanning=true.
	m := &MultiIntent{Intents: []*Intent{
		{Type: string(IntentCode), AgentType: config.AgentIDCoder, Confidence: 0.9, RequiresPlanning: true},
		{Type: string(IntentWrite), AgentType: config.AgentIDWriter, Confidence: 0.8},
	}}
	if !m.DetectCompound() {
		t.Fatal("code+write must be compound")
	}
	if m.CompoundType != "parallel" {
		t.Fatalf("CompoundType = %q, want parallel (an independent code+write pair must not sequence on the async-dispatch flag)", m.CompoundType)
	}

	// A planning lane still sequences the compound (keyword producer's
	// planning column: plan/architect/collaborate).
	m2 := &MultiIntent{Intents: []*Intent{
		{Type: string(IntentCode), AgentType: config.AgentIDCoder, Confidence: 0.9, RequiresPlanning: true},
		{Type: string(IntentPlan), AgentType: config.AgentIDPlanner, Confidence: 0.7, RequiresPlanning: true},
	}}
	if !m2.DetectCompound() {
		t.Fatal("code+plan must be compound")
	}
	if m2.CompoundType != "sequential" {
		t.Fatalf("CompoundType = %q, want sequential for a plan lane", m2.CompoundType)
	}
}

// TestClassifyMultiIntent_LLMCodePlusWriteIsParallel drives the regression
// through the REAL LLM producer: its code lane now carries
// RequiresPlanning=true, and the compound must still read parallel.
func TestClassifyMultiIntent_LLMCodePlusWriteIsParallel(t *testing.T) {
	d := newCompoundTestDispatcher(t, `[{"intent":"code","confidence":0.9},{"intent":"write","confidence":0.9}]`)

	const input = "refactor the parser module and then write a short summary document describing the change for the team"
	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})

	if !multi.IsCompound {
		t.Fatalf("code+write must be compound. intents=%s", intentsDebug(multi.Intents))
	}
	if multi.CompoundType != "parallel" {
		t.Fatalf("CompoundType = %q, want parallel (an LLM-classified code+* compound must not flip sequential via the async flag)", multi.CompoundType)
	}
}
