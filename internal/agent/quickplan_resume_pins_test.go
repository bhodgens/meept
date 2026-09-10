package agent

// Pins for audit 2026-09-10 M3 (fixer C): the strategic-side half of the
// quickplan-resume mode carry-through. routeToPlan (dispatcher.go) never
// synthesizes a Mode for its plan requests; when such a request names the
// quickplan intent, inferLegacyMode must derive "quick_plan" from the
// intent instead of falling through to the input-length heuristic —
// otherwise the preserved quick_plan mode is silently lost at the
// strategic layer. See the fixer report for the dispatcher-side half
// (routeToPlan drafts terminate as direct_response and never reach the
// planner) left as a parent follow-up.

import "testing"

// TestInferLegacyMode_QuickPlanIntent pins the quickplan case added to
// inferLegacyMode: a Mode-less PlanRequest carrying the quickplan intent
// plans as quick_plan regardless of input length (both the sub-threshold
// and super-threshold paths), matching IntentQuickPlan.SuggestedMode().
func TestInferLegacyMode_QuickPlanIntent(t *testing.T) {
	sp := &StrategicPlanner{simpleInputMaxChars: 100, pairInputMinChars: 200}

	for _, input := range []string{
		"fix the flaky test", // short: previously fell to "direct"
		"carry out the migration plan end to " + // long: previously fell to "plan"
			"end, splitting the schema changes into separately verifiable batches",
	} {
		req := PlanRequest{Intent: string(IntentQuickPlan), Input: input}
		if got := sp.inferLegacyMode(req); got != "quick_plan" {
			t.Errorf("inferLegacyMode(quickplan, len=%d) = %q, want quick_plan",
				len(input), got)
		}
	}

	// The derived mode must equal the intent's own SuggestedMode contract
	// (intent_quickplan_test.go pins the same value at the method level).
	if want := IntentQuickPlan.SuggestedMode(); want != "quick_plan" {
		t.Fatalf("IntentQuickPlan.SuggestedMode() changed: %q", want)
	}
}

// TestInferLegacyMode_QuickPlanExplicitModeUnchanged guards the ordering:
// inferLegacyMode only runs when req.Mode is empty (Plan at strategic.go),
// so an explicit mode is never overridden. These are planner-level
// expectations about the call site, pinned here so a future edit that
// moves the Mode check after inferLegacyMode fails loudly.
func TestInferLegacyMode_QuickPlanExplicitModeUnchanged(t *testing.T) {
	sp := &StrategicPlanner{simpleInputMaxChars: 100, pairInputMinChars: 200}

	// The quickplan case must not swallow other intents: compound and
	// plan keep their legacy modes.
	if got := sp.inferLegacyMode(PlanRequest{Intent: string(IntentCompound), IsCompound: true}); got != "spec_pair" {
		t.Errorf("inferLegacyMode(compound) = %q, want spec_pair", got)
	}
	if got := sp.inferLegacyMode(PlanRequest{Intent: string(IntentPlan)}); got != "spec_plan" {
		t.Errorf("inferLegacyMode(plan) = %q, want spec_plan", got)
	}
	// Non-quickplan, non-compound short inputs still degrade to direct.
	if got := sp.inferLegacyMode(PlanRequest{Intent: string(IntentCode), Input: "hi"}); got != "direct" {
		t.Errorf("inferLegacyMode(code, short) = %q, want direct", got)
	}
}
