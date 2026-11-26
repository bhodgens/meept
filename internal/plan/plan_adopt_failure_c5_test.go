package plan

// C-5 / parent ruling D5 pins (tree 20261125-plan-contract-quickplan leaf 02
// leg 1): the quick_plan adopt-failure posture.
//
// When the dispatcher's AdoptTask retry pair both fail, MarkAdoptFailure
// stamps an adopt-prefixed SynthesisError on the APPROVED, UNBOUND row and
// the turn proceeds with the task running unlinked. The synthesis
// reclaimer's approved-scan sees exactly this shape (approved + no
// task_id) — and MUST skip it: re-driving Synthesize on a zero-phase
// quick_plan brief would complete the contract while its real task is
// still executing (the M11 lie). An ordinary SynthesisError row is not
// this posture and still re-drives.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// newAdoptGateManager builds a temp-store PlanManager with a counting task
// creator — the creator count is the no-re-drive oracle (a re-drive on a
// phased plan creates tasks; a zero-phase Synthesize would also silently
// COMPLETE the plan, which the state assertions below catch).
func newAdoptGateManager(t *testing.T) (*PlanManager, *SQLiteStore, *countingCreator) {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "adopt-fail.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := config.PlansConfig{
		Mode:    "threshold",
		Storage: config.PlansStorageConfig{DefaultPath: "docs/plans"},
	}
	creator := &countingCreator{}
	return NewPlanManager(store, nil, cfg, creator, logger), store, creator
}

func TestMarkAdoptFailure_StampsPrefixKeepsState(t *testing.T) {
	mgr, store, _ := newAdoptGateManager(t)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Quick brief", "plan the thing then do it", "", "", "sess-adopt-fail")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitAndApproveNoSynthesis(ctx, p.ID); err != nil {
		t.Fatalf("SubmitAndApproveNoSynthesis: %v", err)
	}

	adoptErr := errors.New("synthetic adopt outage")
	if err := mgr.MarkAdoptFailure(ctx, p.ID, adoptErr); err != nil {
		t.Fatalf("MarkAdoptFailure: %v", err)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	// The row STAYS approved (D5): never parked, never synthesizing.
	if got.State != StateApproved {
		t.Errorf("state after MarkAdoptFailure = %q, want %q (row stays approved)", got.State, StateApproved)
	}
	if got.TaskID != "" {
		t.Errorf("task_id = %q, want empty (the binding failed)", got.TaskID)
	}
	if !strings.HasPrefix(got.SynthesisError, AdoptFailurePrefix) {
		t.Errorf("synthesis_error = %q, want %q prefix", got.SynthesisError, AdoptFailurePrefix)
	}
	if !strings.Contains(got.SynthesisError, adoptErr.Error()) {
		t.Errorf("synthesis_error = %q, must carry the adopt error text", got.SynthesisError)
	}
	// nil error = no-op; empty id is refused, not panicked.
	if err := mgr.MarkAdoptFailure(ctx, p.ID, nil); err != nil {
		t.Errorf("MarkAdoptFailure(nil err) = %v, want nil no-op", err)
	}
	if err := mgr.MarkAdoptFailure(ctx, "", adoptErr); err == nil {
		t.Error("MarkAdoptFailure(empty id) = nil, want error")
	}
}

func TestSynthesisReclaimSkipsAdoptFailureRows(t *testing.T) {
	mgr, store, creator := newAdoptGateManager(t)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Degraded brief", "quickplan zero-phase brief", "", "", "sess-adopt-skip")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitAndApproveNoSynthesis(ctx, p.ID); err != nil {
		t.Fatalf("SubmitAndApproveNoSynthesis: %v", err)
	}
	if err := mgr.MarkAdoptFailure(ctx, p.ID, errors.New("synthetic adopt outage")); err != nil {
		t.Fatalf("MarkAdoptFailure: %v", err)
	}

	counts, err := mgr.runSynthesisReclaimPass(ctx)
	if err != nil {
		t.Fatalf("runSynthesisReclaimPass: %v", err)
	}
	// The pin: the pass ISSUES NO re-drive for the degraded row. A
	// re-driven zero-phase brief would Synthesize straight to completed —
	// the M11 lie this guard exists to prevent.
	if counts.redriven != 0 {
		t.Errorf("redriven = %d, want 0 (adopt-failure rows must be skipped)", counts.redriven)
	}
	if counts.skipped != 1 {
		t.Errorf("skipped = %d, want 1", counts.skipped)
	}
	if parents, _ := creator.counts(); parents != 0 {
		t.Errorf("task creator calls = %d, want 0 (no re-expansion of a degraded contract)", parents)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateApproved {
		t.Errorf("state after reclaim pass = %q, want %q (untouched)", got.State, StateApproved)
	}
	if !strings.HasPrefix(got.SynthesisError, AdoptFailurePrefix) {
		t.Errorf("synthesis_error = %q, want the adopt prefix preserved", got.SynthesisError)
	}
}

func TestSynthesisReclaimRedrivesOrdinarySynthesisError(t *testing.T) {
	mgr, _, creator := newAdoptGateManager(t)
	ctx := context.Background()

	// A phased approved row carrying a PLAIN synthesis error (the shape
	// ApprovePlan's Synthesize failure leaves): the reclaim must still
	// re-drive it — the C-5 skip is prefix-scoped, not error-scoped.
	p := draftPlanWithPhase(t, mgr)
	if err := mgr.store.SetPlanState(ctx, p.ID, StateApproved); err != nil {
		t.Fatalf("set approved: %v", err)
	}
	if err := mgr.updatePlanColumns(ctx, p.ID, func(pl *Plan) {
		pl.SynthesisError = "plain expansion failure"
	}); err != nil {
		t.Fatalf("stamp plain error: %v", err)
	}

	counts, err := mgr.runSynthesisReclaimPass(ctx)
	if err != nil {
		t.Fatalf("runSynthesisReclaimPass: %v", err)
	}
	if counts.redriven != 1 {
		t.Errorf("redriven = %d, want 1 (a plain synthesis error still re-drives)", counts.redriven)
	}
	if counts.skipped != 0 {
		t.Errorf("skipped = %d, want 0 (no adopt prefix on this row)", counts.skipped)
	}
	if parents, _ := creator.counts(); parents != 1 {
		t.Errorf("task creator parent calls = %d, want 1 (the re-drive expanded)", parents)
	}
}
