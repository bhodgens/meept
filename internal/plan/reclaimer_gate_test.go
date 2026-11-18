package plan

// Gate tests for branch 03-plans-goals, leaf 00T (plan/goal remediation).
// TESTS ONLY — written against the NOT-YET-EXISTING symbols of leaf 01
// (C-P2: StartSynthesisReclaimer). They are expected to fail RED for
// missing-implementation reasons (compile: undefined symbols, or behavior
// not implemented) until the implementation leaves land. Audit refs: M-8, M-9.

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// gateCfg is the minimal PlansConfig for a restart-rig manager: no approval,
// no task creator needed for mapping tests.
func gateCfg() config.PlansConfig {
	return config.PlansConfig{
		Mode:    "threshold",
		Storage: config.PlansStorageConfig{DefaultPath: "docs/plans"},
	}
}

// gateWaitForPlanState polls the manager's store until the plan leaves
// `from` (or hits timeout). Used by the reclaimer tests: the contract is
// "reaches executing/failed within the attempt bound", so the observable is
// the store row, not an injected fake.
func gateWaitForPlanState(t *testing.T, mgr *PlanManager, planID string, want PlanState, timeout time.Duration) *Plan {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last PlanState
	for time.Now().Before(deadline) {
		p, err := mgr.store.GetPlan(context.Background(), planID)
		if err != nil {
			t.Fatalf("gateWaitForPlanState get plan: %v", err)
		}
		last = p.State
		if p.State == want {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("plan %s did not reach %s within %s (stuck in %s) — reclaimer contract violated",
		planID, want, timeout, last)
	return nil
}

// backdateSynthesisLease models "the daemon died mid-expansion": the lease ON
// THE ROW is expired. Same raw-SQL idiom as the existing
// TestClaimPlanSynthesis_ExpiredLeaseIsReclaimable rig.
func backdateSynthesisLease(t *testing.T, mgr *PlanManager, planID string) {
	t.Helper()
	sqlite, ok := mgr.store.(*SQLiteStore)
	if !ok {
		t.Fatalf("reclaimer gate needs the SQLite store rig, got %T", mgr.store)
	}
	expired := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, err := sqlite.db.ExecContext(context.Background(),
		`UPDATE plans SET synthesis_lease_until = ? WHERE id = ?`, expired, planID); err != nil {
		t.Fatalf("backdate lease: %v", err)
	}
}

// TestSynthesisReclaimerRedrivesApproved pins M-8/M-9 (C-P2, re-drive half):
// a synthesizing plan whose lease expired is reclaimed to approved AND the
// reclaimer re-invokes Synthesize on it. Today RecoverStrandedSynthesis only
// flips the row to approved — nothing ever calls Synthesize again, so the
// plan would sit at approved forever. The observable proof of a re-drive is
// the plan reaching executing with exactly one parent task created.
//
// Sub-case (attempt bound): a reclaim-target plan that is ALREADY at
// MaxSynthesisAttempts-1 and whose expansion fails must be parked to failed,
// not retried forever — "within the attempt bound" (MaxSynthesisAttempts,
// manager.go).
//
// RED: PlanManager.StartSynthesisReclaimer is undefined (compile failure —
// the C-P2 symbol lands in leaf 01).
func TestSynthesisReclaimerRedrivesApproved(t *testing.T) {
	ctx := t.Context()

	// --- sub-case A: within-bound plan is re-driven to executing.
	mgr, creator := setupClaimManager(t)
	p := draftPlanWithPhase(t, mgr)
	if err := mgr.store.SetPlanState(ctx, p.ID, StateApproved); err != nil {
		t.Fatalf("set approved: %v", err)
	}
	won, err := mgr.store.ClaimPlanSynthesis(ctx, p.ID, time.Second)
	if err != nil || !won {
		t.Fatalf("claim for synthesis: won=%v err=%v", won, err)
	}
	backdateSynthesisLease(t, mgr, p.ID)

	mgr.StartSynthesisReclaimer(ctx, 25*time.Millisecond)

	got := gateWaitForPlanState(t, mgr, p.ID, StateExecuting, 10*time.Second)
	if got.TaskID == "" {
		t.Error("reclaimed-to-approved plan reached executing without a recorded parent task_id")
	}
	if parents, _ := creator.counts(); parents != 1 {
		t.Errorf("re-drive must expand the plan exactly once: parents=%d, want 1", parents)
	}
	// The reclaim must discharge the lease (the claim CAS remains the
	// single-winner guard, so a second pass cannot double-expand).
	if fresh, err := mgr.store.GetPlan(ctx, p.ID); err != nil {
		t.Fatalf("re-read: %v", err)
	} else if fresh.SynthesisLeaseUntil != nil {
		t.Error("reclaimed plan still carries a synthesis lease after re-drive")
	}

	// --- sub-case B: attempt-exhausted plan is parked failed, not spun.
	mgr2, creator2 := setupClaimManager(t)
	creator2.alwaysFailPar = true
	p2 := draftPlanWithPhase(t, mgr2)
	if err := mgr2.store.SetPlanState(ctx, p2.ID, StateApproved); err != nil {
		t.Fatalf("set approved (b): %v", err)
	}
	if won, err := mgr2.store.ClaimPlanSynthesis(ctx, p2.ID, time.Second); err != nil || !won {
		t.Fatalf("claim (b): won=%v err=%v", won, err)
	}
	sqlite2, ok := mgr2.store.(*SQLiteStore)
	if !ok {
		t.Fatalf("sqlite rig (b): %T", mgr2.store)
	}
	// Pre-load the attempt counter to one below the park threshold so the
	// re-driven pass crosses MaxSynthesisAttempts and must park.
	if _, err := sqlite2.db.ExecContext(ctx,
		`UPDATE plans SET synthesis_attempts = ? WHERE id = ?`,
		MaxSynthesisAttempts-1, p2.ID); err != nil {
		t.Fatalf("pre-load attempts: %v", err)
	}
	backdateSynthesisLease(t, mgr2, p2.ID)

	mgr2.StartSynthesisReclaimer(ctx, 25*time.Millisecond)

	got2 := gateWaitForPlanState(t, mgr2, p2.ID, StateFailed, 10*time.Second)
	if got2.SynthesisAttempts < MaxSynthesisAttempts {
		t.Errorf("parked plan must record the attempt count that crossed the bound: got %d, want >= %d",
			got2.SynthesisAttempts, MaxSynthesisAttempts)
	}
}

// TestSynthesisReclaimerPeriodic pins the ticker half of C-P2: recovery runs
// PERIODICALLY inside the running process — a plan abandoned while the
// daemon lives is reclaimed and re-driven without any restart. The stranded
// row is created AFTER the reclaimer started, so a startup-only pass
// (today's RecoverStrandedSynthesis wiring) can never catch it.
//
// RED: StartSynthesisReclaimer undefined (compile).
func TestSynthesisReclaimerPeriodic(t *testing.T) {
	ctx := t.Context()

	mgr, _ := setupClaimManager(t)
	mgr.StartSynthesisReclaimer(ctx, 25*time.Millisecond)

	// Mid-run abandonment: claim, then expire the lease after the loop is
	// already running.
	p := draftPlanWithPhase(t, mgr)
	if err := mgr.store.SetPlanState(ctx, p.ID, StateApproved); err != nil {
		t.Fatalf("set approved: %v", err)
	}
	if won, err := mgr.store.ClaimPlanSynthesis(ctx, p.ID, time.Second); err != nil || !won {
		t.Fatalf("claim: won=%v err=%v", won, err)
	}
	// Wait past at least one ticker interval BEFORE expiring, so the only
	// pass that can catch this plan is a periodic one.
	time.Sleep(80 * time.Millisecond)
	backdateSynthesisLease(t, mgr, p.ID)

	gateWaitForPlanState(t, mgr, p.ID, StateExecuting, 10*time.Second)
}

// TestRestoreMappingsCoversSynthesizing pins F12 (leaf 06 T4): after a daemon
// restart, RestoreMappings must rebuild the child-task registrations for
// SYNTHESIZING plans too, not just executing ones. The scenario: the daemon
// died mid-expansion — the plan row sits at synthesizing while a phase
// already persisted its child task_id. A completion callback for that child
// must resolve the mapping and update the phase; today RestoreMappings scans
// ListPlansByState(StateExecuting) only, so the callback is a silent no-op.
//
// Behavioral RED (no undefined symbols): runs as "phase never moved to
// completed" once the package compiles again.
func TestRestoreMappingsCoversSynthesizing(t *testing.T) {
	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "restore.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	p := NewPlan("restore-gap", "d", "", "", "")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	ph := NewPlanPhase(p.ID, "Phase A", 1, 1)
	if err := store.CreatePhase(ctx, ph); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	// The child task was created before the crash; its id is durable on the
	// phase row.
	ph.TaskID = "task-restore-child"
	ph.State = PhaseInProgress
	if err := store.UpdatePhase(ctx, ph); err != nil {
		t.Fatalf("UpdatePhase (persist child task id): %v", err)
	}
	if err := store.SetPlanState(ctx, p.ID, StateSynthesizing); err != nil {
		t.Fatalf("SetPlanState synthesizing: %v", err)
	}

	// "Restart": a fresh manager over the same store, nothing in-memory.
	mgr := NewPlanManager(store, nil, gateCfg(), nil, logger)
	if err := mgr.RestoreMappings(ctx); err != nil {
		t.Fatalf("RestoreMappings: %v", err)
	}
	if err := mgr.OnTaskCompleted(ctx, "task-restore-child", ""); err != nil {
		t.Fatalf("OnTaskCompleted: %v", err)
	}

	phases, err := store.GetPhases(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPhases: %v", err)
	}
	if len(phases) != 1 {
		t.Fatalf("phases = %d, want 1", len(phases))
	}
	if phases[0].State != PhaseCompleted {
		t.Errorf("synthesizing plan's child-task registration did not resolve: phase state = %s, want %s "+
			"(RestoreMappings must cover StateSynthesizing, not only StateExecuting)",
			phases[0].State, PhaseCompleted)
	}
}
