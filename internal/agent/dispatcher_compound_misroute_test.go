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
// Three independent layers defend against this shape now; each test below
// pins one.

// collapseWorkPlusReport is the arbitration core extracted from
// classifyMultiIntent (test seam): collapses a compound verdict to single
// when exactly one actionable intent is accompanied by conversational
// tag-alongs. Mirrors dispatcher.go's inline logic — kept in sync by the
// classifyMultiIntent pins below.
func (d *Dispatcher) collapseWorkPlusReport(multi *MultiIntent) bool {
	if !multi.IsCompound {
		return false
	}
	actionable := 0
	chatLike := 0
	var nonChatIntent *Intent
	for _, intent := range multi.Intents {
		if intent.Confidence < compoundIntentConfidenceFloor {
			continue
		}
		switch intent.Type {
		case string(IntentChat), string(IntentPlatform), string(IntentRecall),
			string(IntentReport), string(IntentSearch), string(IntentAnalyze):
			chatLike++
		default:
			actionable++
			if nonChatIntent == nil || intent.Confidence > nonChatIntent.Confidence {
				nonChatIntent = intent
			}
		}
	}
	if actionable == 1 && chatLike >= 1 && nonChatIntent != nil {
		multi.IsCompound = false
		multi.CompoundType = ""
		return true
	}
	return false
}

// Layer 1: the collapse shape (one work intent + chat tag-along) collapses
// to single-intent routing.
func TestCollapseWorkPlusReport_WorkPlusChatCollapses(t *testing.T) {
	d := &Dispatcher{}

	// Simulates exactly what the multi-intent classifiers returned for e2e
	// T1 run 1: code for "create a file…", chat for the "tell me" tail.
	multi := &MultiIntent{
		Intents: []*Intent{
			{Type: string(IntentCode), AgentType: config.AgentIDCoder, Confidence: 0.9},
			{Type: string(IntentChat), AgentType: config.AgentIDChat, Confidence: 0.8},
		},
		Summary: "create a file, then tell me the path",
	}
	multi.DetectCompound()

	if !multi.IsCompound {
		t.Fatal("precondition: DetectCompound marks code+chat compound; collapse layer would be inactive")
	}

	if !d.collapseWorkPlusReport(multi) {
		t.Fatal("collapseWorkPlusReport did not fire for code+chat shape")
	}
	if multi.IsCompound {
		t.Fatal("work-plus-report shape still compound after collapse")
	}
	if multi.CompoundType != "" {
		t.Errorf("CompoundType = %q after collapse, want empty", multi.CompoundType)
	}
}

// Layer 1b: genuine multi-work requests never collapse.
func TestCollapseWorkPlusReport_TwoWorkIntentsStay(t *testing.T) {
	d := &Dispatcher{}

	multi := &MultiIntent{
		Intents: []*Intent{
			{Type: string(IntentCode), AgentType: config.AgentIDCoder, Confidence: 0.9},
			{Type: string(IntentDebug), AgentType: config.AgentIDDebugger, Confidence: 0.8},
		},
		Summary: "refactor and debug",
	}
	multi.DetectCompound()

	if !multi.IsCompound {
		t.Fatal("precondition: code+debug should be compound")
	}
	if d.collapseWorkPlusReport(multi) {
		t.Fatal("two work intents collapsed; collapse must require exactly one actionable intent")
	}
	if !multi.IsCompound {
		t.Fatal("two-work-intent compound collapsed; must stay compound")
	}
}

// Layer 1c: sub-threshold intents do not count toward the chat-like tally —
// a strong work intent + a weak (below DetectCompound's own 0.5 floor) chat
// detection is not a work-plus-report compound in the first place.
func TestCollapseWorkPlusReport_SubthresholdChatDoesNotFire(t *testing.T) {
	d := &Dispatcher{}

	multi := &MultiIntent{
		Intents: []*Intent{
			{Type: string(IntentCode), AgentType: config.AgentIDCoder, Confidence: 0.9},
			{Type: string(IntentChat), AgentType: config.AgentIDChat, Confidence: 0.3},
			{Type: string(IntentGit), AgentType: config.AgentIDCommitter, Confidence: 0.8},
		},
		Summary: "code it and commit",
	}
	multi.DetectCompound()

	if !multi.IsCompound {
		t.Fatal("precondition: code+git should be compound")
	}
	if d.collapseWorkPlusReport(multi) {
		t.Fatal("sub-threshold chat must not trigger the collapse; two work intents stay compound")
	}
	if !multi.IsCompound {
		t.Fatal("compound collapsed via sub-threshold intent; must stay compound")
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
