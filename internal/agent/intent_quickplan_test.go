package agent

import "testing"

// TestIntentQuickPlan pins the quickplan intent contract from leaf 01 of
// docs/plans/quickplan-mode (adjudication record:
// docs/plans/classifier-iteration).
func TestIntentQuickPlan(t *testing.T) {
	it := IntentQuickPlan
	if got := it.SuggestedMode(); got != "quick_plan" {
		t.Errorf("SuggestedMode() = %q, want quick_plan", got)
	}
	if got := it.DefaultAgent(); got != "orchestrator" {
		t.Errorf("DefaultAgent() = %q, want orchestrator", got)
	}
	if got := it.Category(); got != CategoryDefer {
		t.Errorf("Category() = %v, want defer", got)
	}
	if !it.RequiresPlanning() {
		t.Error("RequiresPlanning() = false, want true")
	}
	if !it.ShouldCreateTask() {
		t.Error("ShouldCreateTask() = false, want true")
	}
}
