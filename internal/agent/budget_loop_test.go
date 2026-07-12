package agent

import (
	"testing"
)

// TestAgentLoop_BudgetHierarchyInitialized verifies that NewAgentLoop
// initializes the budgetHierarchy field and that GetBudgetStatus returns a
// non-nil snapshot with the expected task-level total.
func TestAgentLoop_BudgetHierarchyInitialized(t *testing.T) {
	loop := NewAgentLoop("test-budget-init", "/tmp/test")
	if loop == nil {
		t.Fatal("NewAgentLoop returned nil")
	}

	status := loop.GetBudgetStatus()
	if status == nil {
		t.Fatal("GetBudgetStatus() returned nil; expected non-nil status")
	}

	if status.Task.Total != 100000 {
		t.Errorf("task total = %d, want 100000", status.Task.Total)
	}

	// Verify the "default" phase exists.
	phase, ok := status.Phases["default"]
	if !ok {
		t.Fatal("expected 'default' phase in budget status")
	}
	if phase.Total != 90000 {
		t.Errorf("default phase total = %d, want 90000", phase.Total)
	}

	// Turn budget should have been created by SelectPhaseBudget("default").
	if status.Turn.Total <= 0 {
		t.Errorf("turn budget total = %d, expected > 0", status.Turn.Total)
	}
}

// TestAgentLoop_BudgetHierarchy_RecordUsage verifies that recording usage
// through the hierarchy is reflected in GetBudgetStatus().
func TestAgentLoop_BudgetHierarchy_RecordUsage(t *testing.T) {
	loop := NewAgentLoop("test-budget-record", "/tmp/test")

	// Record usage directly on the hierarchy.
	loop.budgetHierarchy.RecordUsage(500)

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
