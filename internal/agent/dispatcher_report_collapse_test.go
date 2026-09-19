package agent

import (
	"context"
	"testing"
)

// Pins for the report-readback collapse arm in classifyMultiIntent (leaf 04 of
// the 2026-09-18 tool-boundary-hardening plan). The chatLike collapse arm
// (2026-09-10) deliberately excluded report (bughunt F42: "write a summary and
// a report" is two deliverables); the READBACK flavor — "then tell me the full
// path" — still went compound in e2e T1 (intents=4 type=parallel) and routed a
// one-file task into a planner pair session. These pins drive the REAL
// classifyMultiIntent with stub LLM verdicts in the fixture style of
// dispatcher_compound_misroute_test.go.

// TestClassifyMultiIntent_ReportReadbackCollapses pins the T1 prompt VERBATIM:
// code@0.9 + report@0.8 (plus sub-floor chat noise, mirroring the intents=4
// real-run count) must collapse to a single actionable intent because the
// report clause references the action's output ("the full path").
func TestClassifyMultiIntent_ReportReadbackCollapses(t *testing.T) {
	d := newCompoundTestDispatcher(t,
		`[{"intent":"code","confidence":0.9},{"intent":"report","confidence":0.8},{"intent":"chat","confidence":0.3}]`)

	// T1 prompt verbatim (2026-09-18 e2e): long enough for
	// hasCompoundSignalWords, carries " then ".
	const input = "create a file named hello.txt in the current directory containing the word hello, then tell me the full path"
	if !hasCompoundSignalWords(input) {
		t.Fatalf("precondition: %q has no compound signal (fixture drift)", input)
	}

	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if multi.IsCompound {
		t.Fatalf("report readback must collapse to the single work intent; got compound. intents=%s",
			intentsDebug(multi.Intents))
	}
	if multi.CompoundType != "" {
		t.Errorf("CompoundType = %q after collapse, want empty", multi.CompoundType)
	}
}

// TestClassifyMultiIntent_ReportDeliverableStaysCompound pins the F42
// protection through the full classifyMultiIntent path: a report clause with
// its OWN work verb ("write a report about the findings") is a second
// deliverable — the collapse must NOT fire.
func TestClassifyMultiIntent_ReportDeliverableStaysCompound(t *testing.T) {
	t.Run("comma-and split, own work verb", func(t *testing.T) {
		d := newCompoundTestDispatcher(t,
			`[{"intent":"code","confidence":0.9},{"intent":"report","confidence":0.85}]`)
		const input = "create the config file for the deployment pipeline in the repository, and write a report about the findings today"
		multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
		if !multi.IsCompound {
			t.Fatalf("code+report-deliverable must stay compound (F42). intents=%s", intentsDebug(multi.Intents))
		}
	})
	t.Run("no splittable connector", func(t *testing.T) {
		d := newCompoundTestDispatcher(t,
			`[{"intent":"code","confidence":0.9},{"intent":"report","confidence":0.85}]`)
		const input = "create the config file for the deployment pipeline in the repository and write a detailed report about the findings"
		multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
		if !multi.IsCompound {
			t.Fatalf("code+report-deliverable without a recognized connector must stay compound (F42). intents=%s",
				intentsDebug(multi.Intents))
		}
	})
}

// TestClassifyMultiIntent_ThreeActionableUntouched pins that actionable >= 3
// never enters the readback collapse: three work intents are a genuine
// compound regardless of clause shapes.
func TestClassifyMultiIntent_ThreeActionableUntouched(t *testing.T) {
	d := newCompoundTestDispatcher(t,
		`[{"intent":"code","confidence":0.9},{"intent":"debug","confidence":0.8},{"intent":"git","confidence":0.75}]`)
	const input = "refactor the parser module in the repository, then debug the failing test suite, and commit the final changes"
	if !hasCompoundSignalWords(input) {
		t.Fatalf("precondition: %q has no compound signal (fixture drift)", input)
	}
	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if !multi.IsCompound {
		t.Fatalf("three actionable intents must stay compound. intents=%s", intentsDebug(multi.Intents))
	}
}

// TestClassifyMultiIntent_ReportDominantStaysCompound pins the
// lower-confidence gate: the readback collapse only fires when REPORT is the
// LOWER-confidence of the two actionable intents. A dominant report verdict
// means the classifier believes the report is the request's center of gravity
// — leave the compound verdict alone.
func TestClassifyMultiIntent_ReportDominantStaysCompound(t *testing.T) {
	d := newCompoundTestDispatcher(t,
		`[{"intent":"report","confidence":0.9},{"intent":"code","confidence":0.8}]`)
	const input = "create a file named hello.txt in the current directory containing the word hello, then tell me the full path"
	multi := d.classifyMultiIntent(context.Background(), input, &MemoryContext{IntentCounts: map[string]int{}})
	if !multi.IsCompound {
		t.Fatalf("dominant-report verdict must stay compound (collapse gates on report being lower-confidence). intents=%s",
			intentsDebug(multi.Intents))
	}
}
