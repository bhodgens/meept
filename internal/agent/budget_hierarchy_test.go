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

func TestBudgetHierarchy_Carryover(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1", "phase2"}, []int{5000, 5000})

	// Record 4000 used in phase1 (so Available = 5000 - 4000 = 1000)
	h.phaseBudgets["phase1"].Allocate(4000)

	availBefore := h.phaseBudgets["phase2"].totalBudget
	if err := h.CarryoverUnused("phase1", "phase2"); err != nil {
		t.Fatalf("CarryoverUnused failed: %v", err)
	}

	// phase2 should have gained ~1000
	availAfter := h.phaseBudgets["phase2"].totalBudget
	if availAfter != availBefore+1000 {
		t.Errorf("expected phase2 total to increase by 1000 (%d -> %d)", availBefore, availAfter)
	}

	// phase1 should be marked as fully consumed
	p1 := h.phaseBudgets["phase1"]
	if p1.usedBudget != p1.totalBudget {
		t.Errorf("expected phase1 usedBudget == totalBudget (%d), got usedBudget=%d", p1.totalBudget, p1.usedBudget)
	}
}

func TestBudgetHierarchy_Carryover_NotAllowed(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1", "phase2"}, []int{5000, 5000})

	// Disable carryover on phase1 (phases default to allowCarryover=true via NewBudgetHierarchy)
	h.phaseBudgets["phase1"].allowCarryover = false

	err := h.CarryoverUnused("phase1", "phase2")
	if err == nil {
		t.Fatal("expected error when carryover not allowed, got nil")
	}
}

func TestBudgetHierarchy_Carryover_NotFound(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1", "phase2"}, []int{5000, 5000})

	err := h.CarryoverUnused("nonexistent", "phase2")
	if err == nil {
		t.Fatal("expected error for non-existent source phase, got nil")
	}

	err = h.CarryoverUnused("phase1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent destination phase, got nil")
	}
}

func TestBudgetHierarchy_BorrowForPhase(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1", "phase2"}, []int{5000, 5000})

	// phase1 fully consumed (requesting phase has 0 available)
	h.phaseBudgets["phase1"].Allocate(5000)
	if !h.phaseBudgets["phase1"].IsExhausted() {
		t.Fatal("phase1 should be exhausted after allocating all 5000")
	}

	// phase2 has borrowing enabled and surplus
	h.phaseBudgets["phase2"].WithBorrowing(true)

	totalBefore := h.phaseBudgets["phase1"].totalBudget
	ok := h.BorrowForPhase("phase1", 1000)
	if !ok {
		t.Fatal("BorrowForPhase should succeed when sibling has surplus and borrowing enabled")
	}

	// phase1 totalBudget should increase by 1000
	totalAfter := h.phaseBudgets["phase1"].totalBudget
	if totalAfter != totalBefore+1000 {
		t.Errorf("expected phase1 total to increase by 1000 (%d -> %d)", totalBefore, totalAfter)
	}

	// phase2 should have 1000 more used
	used2 := h.phaseBudgets["phase2"].Used()
	if used2 != 1000 {
		t.Errorf("expected phase2 used=1000 after lending, got %d", used2)
	}
}

func TestBudgetHierarchy_BorrowForPhase_NoSurplus(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"phase1", "phase2"}, []int{5000, 5000})

	// Exhaust both phases
	h.phaseBudgets["phase1"].Allocate(5000)
	h.phaseBudgets["phase2"].Allocate(5000)
	h.phaseBudgets["phase2"].WithBorrowing(true)

	// Neither has surplus; borrow should fail
	ok := h.BorrowForPhase("phase1", 1000)
	if ok {
		t.Fatal("BorrowForPhase should return false when no sibling has surplus")
	}
}

// TestBudgetHierarchy_RecordUsage_Propagates verifies that RecordUsage
// decrements at all three levels: turn, phase, and task.
func TestBudgetHierarchy_RecordUsage_Propagates(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"p1", "p2"}, []int{5000, 5000})
	if err := h.SelectPhaseBudget("p1"); err != nil {
		t.Fatalf("SelectPhaseBudget failed: %v", err)
	}

	h.RecordUsage(500)

	// Turn level should show 500 used.
	if got := h.turnBudget.Used(); got != 500 {
		t.Errorf("turn used = %d, want 500", got)
	}

	// Phase p1 should show 500 used.
	p1 := h.phaseBudgets["p1"]
	if got := p1.Used(); got != 500 {
		t.Errorf("phase p1 used = %d, want 500", got)
	}

	// Task level should show 500 used.
	if got := h.taskBudget.Used(); got != 500 {
		t.Errorf("task used = %d, want 500", got)
	}

	// Phase p2 (not selected) should show 0 used.
	p2 := h.phaseBudgets["p2"]
	if got := p2.Used(); got != 0 {
		t.Errorf("phase p2 used = %d, want 0", got)
	}

	// Verify GetStatus reflects the same.
	status := h.GetStatus()
	if status.Task.Used != 500 {
		t.Errorf("status task used = %d, want 500", status.Task.Used)
	}
	if status.Phases["p1"].Used != 500 {
		t.Errorf("status phase p1 used = %d, want 500", status.Phases["p1"].Used)
	}
	if status.Phases["p2"].Used != 0 {
		t.Errorf("status phase p2 used = %d, want 0", status.Phases["p2"].Used)
	}
	if status.Turn.Used != 500 {
		t.Errorf("status turn used = %d, want 500", status.Turn.Used)
	}
}

// TestBudgetHierarchy_PhaseExhaustionTriggers verifies that when a phase's
// budget is exhausted, IsWarningZone/IsExhausted on that phase returns true.
func TestBudgetHierarchy_PhaseExhaustionTriggers(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"p1", "p2"}, []int{1000, 5000})
	if err := h.SelectPhaseBudget("p1"); err != nil {
		t.Fatalf("SelectPhaseBudget failed: %v", err)
	}

	// Exhaust p1 by recording all available budget. p1 has 1000 total, no
	// reserved, so Available = 1000.
	h.RecordUsage(1000)

	p1 := h.phaseBudgets["p1"]
	if !p1.IsExhausted() {
		t.Error("expected phase p1 IsExhausted==true after full allocation")
	}
	// At 100% usage, should also be in warning zone (threshold 0.8).
	if !p1.IsWarningZone() {
		t.Error("expected phase p1 IsWarningZone==true at 100% usage")
	}
}

// TestNewBudgetHierarchy_AutoDistribution verifies that auto-distributed phase
// budgets do not exceed the non-reserved pool when some phases have explicit
// budgets.
func TestNewBudgetHierarchy_AutoDistribution(t *testing.T) {
	// taskBudget=100000, reserved=10000 (10%), non-reserved pool=90000.
	// Explicit: a=50000. Auto: b, c.
	// Remaining = 90000 - 50000 = 40000. Each auto gets 40000/2 = 20000.
	// Total = 50000 + 20000 + 20000 = 90000 (does not exceed pool).
	h := NewBudgetHierarchy(100000, []string{"a", "b", "c"}, []int{50000, 0, 0})

	a := h.phaseBudgets["a"]
	b := h.phaseBudgets["b"]
	c := h.phaseBudgets["c"]

	if a.totalBudget != 50000 {
		t.Errorf("phase a total = %d, want 50000", a.totalBudget)
	}
	if b.totalBudget != 20000 {
		t.Errorf("phase b total = %d, want 20000", b.totalBudget)
	}
	if c.totalBudget != 20000 {
		t.Errorf("phase c total = %d, want 20000", c.totalBudget)
	}

	sum := a.totalBudget + b.totalBudget + c.totalBudget
	nonReserved := 100000 - 100000/10 // 90000
	if sum > nonReserved {
		t.Errorf("phase budget sum %d exceeds non-reserved pool %d", sum, nonReserved)
	}
}

// TestBudgetHierarchy_AutoBorrowOnExhaustion verifies that when a phase's
// allocation fails during RecordUsage and the phase has borrowing enabled,
// auto-borrow kicks in and the allocation succeeds.
func TestBudgetHierarchy_AutoBorrowOnExhaustion(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"p1", "p2"}, []int{1000, 5000})

	// Enable borrowing on p2 so it can lend to p1.
	h.phaseBudgets["p2"].WithBorrowing(true)
	// Enable borrowing on p1 so auto-borrow triggers when p1 is exhausted.
	h.phaseBudgets["p1"].WithBorrowing(true)

	if err := h.SelectPhaseBudget("p1"); err != nil {
		t.Fatalf("SelectPhaseBudget failed: %v", err)
	}

	// p1 has 1000 total. Record 600 usage (under limit).
	h.RecordUsage(600)
	p1 := h.phaseBudgets["p1"]
	if got := p1.Used(); got != 600 {
		t.Errorf("after 600 usage, p1 used = %d, want 600", got)
	}

	// Record 500 more. p1 only has 400 available. Auto-borrow should kick in
	// (borrowing 500 from p2 which has 5000), then the phase allocation succeeds.
	h.RecordUsage(500)
	if got := p1.Used(); got != 1100 {
		t.Errorf("after auto-borrow + 500 more, p1 used = %d, want 1100", got)
	}

	// p2 should have lent 500 (usedBudget increased by 500).
	p2 := h.phaseBudgets["p2"]
	if got := p2.Used(); got != 500 {
		t.Errorf("p2 used after lending = %d, want 500", got)
	}
}

// TestBudgetHierarchy_AdvancePhase verifies that AdvancePhase carries over
// unused budget and selects the new phase.
func TestBudgetHierarchy_AdvancePhase(t *testing.T) {
	h := NewBudgetHierarchy(10000, []string{"p1", "p2"}, []int{5000, 5000})
	if err := h.SelectPhaseBudget("p1"); err != nil {
		t.Fatalf("SelectPhaseBudget(p1) failed: %v", err)
	}

	// Use 3000 of p1's 5000 budget via RecordUsage.
	h.RecordUsage(3000)

	// p1 should have 2000 available.
	p1 := h.phaseBudgets["p1"]
	if got := p1.Available(); got != 2000 {
		t.Fatalf("p1 available before advance = %d, want 2000", got)
	}

	// Advance to p2 — should carry over p1's 2000 unused to p2.
	if err := h.AdvancePhase("p2"); err != nil {
		t.Fatalf("AdvancePhase failed: %v", err)
	}

	// p2 should have gained 2000 from carryover.
	p2 := h.phaseBudgets["p2"]
	if got := p2.totalBudget; got != 7000 {
		t.Errorf("p2 totalBudget after carryover = %d, want 7000", got)
	}

	// p1 should be fully consumed.
	if got := p1.Used(); got != p1.totalBudget {
		t.Errorf("p1 used = %d, want %d (fully consumed)", got, p1.totalBudget)
	}

	// currentPhase should now be p2.
	if h.currentPhase != "p2" {
		t.Errorf("currentPhase = %q, want \"p2\"", h.currentPhase)
	}

	// Turn budget should be positive (p2 now has 7000, turn = 700).
	if got := h.GetTurnBudget(); got <= 0 {
		t.Errorf("turn budget after advance = %d, want > 0", got)
	}
}
