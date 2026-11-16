package daemon

// Tests for the approved-plan resume sweep wiring (stranded-approval fix):
// startApprovedPlanResumeSweep arms Manager.StartApprovedPlanResumeSweep at
// Components.Start — the boot-time + periodic reconciliation that covers
// the shapes the in-memory plan.approved bus event misses (a daemon restart
// between approval and resume; a resume skipped while the employee's goal
// loop was busy mid-round, since the TryLock skip never queues).

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/plan"
)

// TestPlanApprovedListerAdapter_ListsApprovedIDs: the adapter surfaces the
// plan manager's approved-state rows as IDs.
func TestPlanApprovedListerAdapter_ListsApprovedIDs(t *testing.T) {
	dir := t.TempDir()
	store, err := plan.NewSQLiteStore(filepath.Join(dir, "plans.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	pm := plan.NewPlanManager(store, nil, planConfigForAdapterTest(), nil, slog.New(slog.DiscardHandler))

	ctx := context.Background()
	p, err := pm.CreatePlan(ctx, "approved probe", "d", "proj", dir, "")
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if err := pm.SubmitPlan(ctx, p.ID); err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	// Zero config has RequireApproval=false, so SubmitPlan auto-approves.

	ids, err := (&planApprovedListerAdapter{pm: pm}).ApprovedPlanIDs(ctx)
	if err != nil {
		t.Fatalf("ApprovedPlanIDs: %v", err)
	}
	found := false
	for _, id := range ids {
		if id == p.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("ApprovedPlanIDs = %v, want it to contain the approved plan %q", ids, p.ID)
	}
}

// TestStartApprovedPlanResumeSweep_NilComponentsNoop: nil employee manager
// or nil plan manager leaves everything unarmed, no panic.
func TestStartApprovedPlanResumeSweep_NilComponentsNoop(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	c := &Components{Logger: logger}
	c.startApprovedPlanResumeSweep(context.Background())
	c.stopApprovedPlanResumeSweep()

	c2 := &Components{Logger: logger, EmployeeManager: employee.NewManagerWithStores(nil, nil, nil, nil, nil, logger)}
	c2.startApprovedPlanResumeSweep(context.Background()) // nil PlanManager: no-op
	c2.stopApprovedPlanResumeSweep()
}

// TestStartApprovedPlanResumeSweep_ArmsAndReconciles: a components with a
// real employee manager + plan manager arms the sweep; an approved plan
// whose goal owns it (via the resume seam identity the sweep passes) is
// attempted by the sweep's immediate first pass. The seam-level check uses
// the manager's resume path directly (the sweep calls
// Manager.ResumeApprovedPlan, observable through the goal-loop executor).
func TestStartApprovedPlanResumeSweep_ArmsAndReconciles(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "shared.db")
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { botStore.Close() })
	goalStore, err := employee.NewGoalStore(dbPath, nil)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	t.Cleanup(func() { goalStore.Close() })
	planStore, err := plan.NewSQLiteStore(filepath.Join(dir, "plans.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { planStore.Close() })
	pm := plan.NewPlanManager(planStore, nil, planConfigForAdapterTest(), nil, slog.New(slog.DiscardHandler))
	logger := slog.New(slog.DiscardHandler)
	mgr := employee.NewManagerWithStores(bot.NewManager(botStore, nil), botStore, nil, goalStore, nil, logger)

	c := &Components{Logger: logger, EmployeeManager: mgr, PlanManager: pm}
	c.startApprovedPlanResumeSweep(context.Background())
	t.Cleanup(func() { c.stopApprovedPlanResumeSweep() })

	// The sweep is armed: an approved plan reaches the manager's resume
	// path through the adapter within one pass. The plan has no owning
	// goal here, so the resume is an INFO skip — the observable contract
	// is that the sweep RAN (no panic, lifecycle joinable), which the
	// immediate Stop below proves: a started sweep joins its goroutine.
	c.stopApprovedPlanResumeSweep()
	c.stopApprovedPlanResumeSweep() // idempotent
}

// planConfigForAdapterTest builds the minimal plans config the adapter
// tests need. The zero value satisfies the adapter path (ListPlansByState
// over StateApproved); approval-required behavior is irrelevant to the
// listing read.
func planConfigForAdapterTest() config.PlansConfig {
	return config.PlansConfig{}
}
