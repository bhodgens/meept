package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/memory"
)

// Campaign 20260918 phase-2 regression (#52): the 8B scored "using subagents,
// review X for bugs, and correct them as you find them" intent=debug/analyst/plan
// — scatter across 8 destinations, 0/17 landed quickplan. QuickPlanCuePattern
// (the adjudicated orchestration evidence, iter-20) matched 16/20 of those inputs.
// When the LLM lands in the quickplan-scatter set AND the input carries STRONG
// adjudicated quickplan cues, the verdict is a lexical costume: upgrade to quickplan
// (mirrors heuristicFallback's review+correctionClause rule, now applied to the
// LLM-verdict path where the chain never reaches the heuristic).
//
// Provenance of the probe inputs (privacy scrub, decision B): case 1 is the
// verbatim private replay message (case_key 616d64a1c5cc6116); cases 2-3 are
// the DESIGNED corpus rows h18-planexec-001 (this string is also private
// replay case 2c0434047ecad05e — the known, audited corpus↔replay overlap)
// and the quickplan-anchor paraphrase "Implement Tasks 7 and 8…" from the
// campaign's routing-repair set. They pin the shipped cue wire, so the
// verbatim forms stay: the privacy scan excludes this file as a documented
// routing fixture.
func TestRoutingRepairQuickPlanCueUpgrade(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
	memCtx := &MemoryContext{Results: []memory.MemoryResult{}, IntentCounts: map[string]int{}}
	cases := []string{
		"using subagents, review the meept client for bugs, and correct them as you find them.",
		"implement the plan using subagents",
		"Implement Tasks 7 and 8: Add project fields to Session struct and wire ProjectManager into daemon config.",
	}
	for _, input := range cases {
		intent, err := d.classifyIntent(context.Background(), input, memCtx)
		if err != nil {
			t.Fatalf("classifyIntent(%q): %v", input, err)
		}
		if intent == nil {
			t.Fatalf("classifyIntent(%q): nil intent", input)
		}
		if intent.Type != string(IntentQuickPlan) {
			t.Errorf("quickplan cue evidence not honored: got %q (method %q) for %q", intent.Type, intent.Method, input)
		}
	}
}
