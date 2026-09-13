package agent

import "testing"

// TestAgentLoopNoHierarchyWhenBudgetOff pins the loop-level side of the single
// global budget switch: a loop is constructed with no hierarchy, and
// SetBudgetConfig with a non-positive total - what the daemon passes when
// llm.budget.enabled is false or agent.budget.total is 0 - never creates one.
// SetBudgetConfig only builds a BudgetHierarchy when total > 0.
func TestAgentLoopNoHierarchyWhenBudgetOff(t *testing.T) {
	loop := NewAgentLoop("test-budget-ssot-off", "/tmp/test")
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}

	if status := loop.GetBudgetStatus(); status != nil {
		t.Fatalf("new AgentLoop has a budget hierarchy (%+v); the disabled posture must be none", status)
	}

	loop.SetBudgetConfig(0, BudgetHierarchyOptions{})
	if status := loop.GetBudgetStatus(); status != nil {
		t.Errorf("SetBudgetConfig(0) created a hierarchy (%+v); want none for a zero total", status)
	}
}
