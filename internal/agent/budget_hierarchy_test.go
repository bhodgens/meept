package agent

import (
	"testing"
)

func TestBudgetAllocation_Available(t *testing.T) {
	a := NewBudgetAllocation("test", BudgetLevelTurn, 1000)
	if got := a.Available(); got != 1000 {
		t.Errorf("expected 1000 available, got %d", got)
	}

	if !a.Allocate(300) {
		t.Fatal("Allocate(300) should succeed")
	}
	if got := a.Available(); got != 700 {
		t.Errorf("expected 700 available after allocating 300, got %d", got)
	}
}

func TestBudgetAllocation_Reserved(t *testing.T) {
	a := NewBudgetAllocation("test", BudgetLevelPhase, 1000).WithReserved(200)
	if got := a.Available(); got != 800 {
		t.Errorf("expected 800 available (excluding reserved), got %d", got)
	}
	if got := a.AvailableWithReserved(); got != 1000 {
		t.Errorf("expected 1000 available (with reserved), got %d", got)
	}
}

func TestBudgetAllocation_Exhausted(t *testing.T) {
	a := NewBudgetAllocation("test", BudgetLevelTurn, 500)
	// Exhaust the budget (total=500, no reserved, so available=500)
	if !a.Allocate(500) {
		t.Fatal("Allocate(500) should succeed for 500 total with no reserve")
	}
	if !a.IsExhausted() {
		t.Error("expected IsExhausted==true after consuming all non-reserved budget")
	}
	// Further allocation should fail
	if a.Allocate(1) {
		t.Error("Allocate(1) should fail when budget is exhausted")
	}
}

func TestBudgetAllocation_WarningZone(t *testing.T) {
	a := NewBudgetAllocation("test", BudgetLevelPhase, 1000).
		WithWarningThreshold(0.7)
	if !a.Allocate(750) {
		t.Fatal("Allocate(750) should succeed")
	}
	if !a.IsWarningZone() {
		t.Error("expected IsWarningZone==true at 75%% usage with 0.7 threshold")
	}
}

func TestBudgetHierarchy_Creation(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1", "phase2"}, []int{5000, 5000})

	if h.taskBudget == nil {
		t.Fatal("task budget should be created")
	}
	if len(h.phaseBudgets) != 2 {
		t.Fatalf("expected 2 phase budgets, got %d", len(h.phaseBudgets))
	}

	// Verify phases exist
	if _, ok := h.phaseBudgets["phase1"]; !ok {
		t.Error("phase1 should exist in phaseBudgets")
	}
	if _, ok := h.phaseBudgets["phase2"]; !ok {
		t.Error("phase2 should exist in phaseBudgets")
	}

	// Verify task budget configuration
	if h.taskBudget.reservedBudget != 1000 {
		t.Errorf("expected task reserved=1000 (10%% of 10000), got %d", h.taskBudget.reservedBudget)
	}

	// Verify phases are children of task
	children := h.taskBudget.GetChildren()
	if len(children) != 2 {
		t.Errorf("expected 2 children on task budget, got %d", len(children))
	}
}

func TestBudgetHierarchy_SelectPhase(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1"}, []int{5000})
	err := h.SelectPhaseBudget("phase1")
	if err != nil {
		t.Fatalf("SelectPhaseBudget failed: %v", err)
	}

	turnBudget := h.GetTurnBudget()
	if turnBudget <= 0 {
		t.Error("turn budget should be positive after selecting a phase")
	}

	// Verify turn budget is roughly phase.Available() / 10
	// phase1 has 5000 total, no reserved, so available=5000, turn=500
	expectedTurn := 5000 / 10
	if turnBudget != expectedTurn {
		t.Errorf("expected turn budget=%d, got %d", expectedTurn, turnBudget)
	}
}

func TestBudgetHierarchy_SelectPhase_NotFound(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1"}, []int{5000})
	err := h.SelectPhaseBudget("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent phase")
	}
}

func TestBudgetHierarchy_RecordUsage(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1"}, []int{5000})
	if err := h.SelectPhaseBudget("phase1"); err != nil {
		t.Fatalf("SelectPhaseBudget failed: %v", err)
	}

	usedBefore := h.turnBudget.Used()
	h.RecordUsage(100)
	usedAfter := h.turnBudget.Used()

	if usedAfter != usedBefore+100 {
		t.Errorf("expected used to increase by 100 (%d -> %d), got %d", usedBefore, usedBefore+100, usedAfter)
	}
}

func TestBudgetHierarchy_GetStatus(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1", "phase2"}, []int{5000, 5000})

	if err := h.SelectPhaseBudget("phase1"); err != nil {
		t.Fatalf("SelectPhaseBudget failed: %v", err)
	}

	status := h.GetStatus()

	// Task-level checks
	if status.Task.Total != 10000 {
		t.Errorf("expected task total=10000, got %d", status.Task.Total)
	}

	// Phase-level checks
	if len(status.Phases) != 2 {
		t.Errorf("expected 2 phases in status, got %d", len(status.Phases))
	}
	if phase1, ok := status.Phases["phase1"]; !ok {
		t.Error("phase1 missing from status")
	} else if phase1.Total != 5000 {
		t.Errorf("expected phase1 total=5000, got %d", phase1.Total)
	}

	// Turn-level checks
	if status.Turn.Total <= 0 {
		t.Error("turn budget should be positive in status after SelectPhaseBudget")
	}
}
