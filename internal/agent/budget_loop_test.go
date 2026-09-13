package agent

import (
	"testing"
)

// TestAgentLoop_BudgetHierarchyDisabledByDefault verifies that NewAgentLoop
// does NOT wire a hierarchical budget. Budgets are opt-in: a default install
// must have no task/phase/turn allocation that can warn, park, or cut a turn
// off. The nil hierarchy is the supported state.
func TestAgentLoop_BudgetHierarchyDisabledByDefault(t *testing.T) {
	loop := NewAgentLoop("test-budget-init", "/tmp/test")
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}

	if loop.budgetHierarchy != nil {
		t.Fatal("NewAgentLoop wired a budget hierarchy; budgets must be opt-in (see agent.budget.enabled)")
	}

	if status := loop.GetBudgetStatus(); status != nil {
		t.Errorf("GetBudgetStatus() = %+v, want nil when no budget is configured", status)
	}
}

// TestAgentLoop_BudgetHierarchyEnabledByConfig verifies that SetBudgetConfig is
// what turns the hierarchy on. The daemon calls it only when
// agent.budget.enabled is true AND a positive total is configured.
func TestAgentLoop_BudgetHierarchyEnabledByConfig(t *testing.T) {
	loop := NewAgentLoop("test-budget-config", "/tmp/test")
	loop.SetBudgetConfig(100000, BudgetHierarchyOptions{})

	status := loop.GetBudgetStatus()
	if status == nil {
		t.Fatal("GetBudgetStatus() returned nil after SetBudgetConfig")
	}
	if status.Task.Total != 100000 {
		t.Errorf("task total = %d, want 100000", status.Task.Total)
	}

	// Turn budget is created by the phase selection SetBudgetConfig performs.
	if status.Turn.Total <= 0 {
		t.Errorf("turn budget total = %d, expected > 0", status.Turn.Total)
	}
}

// TestAgentLoop_BudgetHierarchy_RecordUsage verifies that recording usage
// through a CONFIGURED hierarchy is reflected in GetBudgetStatus().
func TestAgentLoop_BudgetHierarchy_RecordUsage(t *testing.T) {
	loop := NewAgentLoop("test-budget-record", "/tmp/test")
	loop.SetBudgetConfig(100000, BudgetHierarchyOptions{})
	if loop.budgetHierarchy == nil {
		t.Fatal("budget hierarchy not wired by SetBudgetConfig")
	}

	// Record usage directly on the hierarchy.
	loop.budgetHierarchy.RecordUsage(500, "default")

	status := loop.GetBudgetStatus()
	if status == nil {
		t.Fatal("GetBudgetStatus() returned nil")
	}

	// The turn budget should reflect at least 500 tokens used.
	// (It may be more if Allocate fails partially, but 500 is the minimum.)
	if status.Turn.Used < 500 {
		t.Errorf("turn used = %d, expected >= 500", status.Turn.Used)
	}
}

// TestAgentLoop_GetBudgetStatusNilGuards verifies that GetBudgetStatus returns
// nil without panicking when budgetHierarchy is not initialized.
func TestAgentLoop_GetBudgetStatusNilGuards(t *testing.T) {
	loop := NewAgentLoop("test-budget-nil", "/tmp/test")

	// Simulate uninitialized hierarchy.
	loop.budgetHierarchy = nil

	status := loop.GetBudgetStatus()
	if status != nil {
		t.Errorf("expected nil status when budgetHierarchy is nil, got %+v", status)
	}
}
