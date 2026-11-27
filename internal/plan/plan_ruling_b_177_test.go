package plan

// Issue #177 owner ruling B pins (2026-11-26): an operator approve of a
// PARKED QUICK_PLAN BRIEF must route the row into EXECUTION, never through
// Synthesize's zero-phase completion guard. The brief carries zero phases
// by construction, so the old ApprovePlan -> Synthesize tail completed it
// instantly WITHOUT ever running it (the M11 lie at approve time, pinned
// by e2e scenario plan-lifecycle-09 pre-ruling).
//
// Ruling B shape:
//   - SubmitForApproval(..., QuickPlanKind) stamps plans.kind at park time
//     (pin in plan_adopt_synthesize_test.go); plain parks keep "".
//   - ApprovePlan of a tagged, task-less row: approval commits (CAS +
//     signoff + columns + plan.approved exactly as before), then the
//     resumer seam runs the execution half (createTask + AdoptTask +
//     orchestrator dispatch, wired by the agent layer). Synthesize is
//     NEVER invoked on the tagged row.
//   - No resumer wired: the row STAYS approved — never completed. That is
//     the honest degradation (C-5/D5 family), and the synthesis reclaimer
//     kind-skip plus Synthesize's own kind-guard keep every silent
//     expansion path off tagged rows.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// newRulingBManager builds a temp-store PlanManager with the shared-store
// rig config (no bus, caller-owned creator count as the synthesis oracle).
func newRulingBManager(t *testing.T) (*PlanManager, *SQLiteStore, *countingCreator) {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "rulingb.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := config.PlansConfig{
		Mode:     "threshold",
		Storage:  config.PlansStorageConfig{DefaultPath: "docs/plans"},
		Approval: config.PlansApprovalConfig{RequireApproval: true, MaxRevisions: 3},
	}
	creator := &countingCreator{}
	return NewPlanManager(store, nil, cfg, creator, logger), store, creator
}

// resumerRecorder is a thread-safe resumer spy for the seam pins.
type resumerRecorder struct {
	mu      sync.Mutex
	calls   []string
	errOn   error
	block   chan struct{} // when non-nil, the resumer waits on it (async posture probe)
	entered chan struct{}
}

func (r *resumerRecorder) fn(ctx context.Context, planID string) error {
	r.mu.Lock()
	r.calls = append(r.calls, planID)
	err := r.errOn
	r.mu.Unlock()
	if r.entered != nil {
		select {
		case r.entered <- struct{}{}:
		default:
		}
	}
	if r.block != nil {
		<-r.block
	}
	return err
}

func (r *resumerRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// TestApprovePlanQuickPlanTagRoutesToResumerNotSynthesis is the core
// ruling-B pin: approving a tagged, unbound row invokes the resumer EXACTLY
// ONCE with the plan id, never touches the task creator (Synthesize), and
// the row does not complete instantly.
func TestApprovePlanQuickPlanTagRoutesToResumerNotSynthesis(t *testing.T) {
	mgr, store, creator := newRulingBManager(t)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Rework behind a gate", "implement only after I approve", "proj-b", t.TempDir(), "sess-b")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitForApproval(ctx, p.ID, QuickPlanKind); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}

	rec := &resumerRecorder{entered: make(chan struct{}, 4)}
	mgr.SetPlanExecutionResumer(rec.fn)

	if err := mgr.ApprovePlan(ctx, p.ID, "sess-b", "operator"); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	// The resumer runs on a bounded goroutine (the approve RPC replies as
	// soon as the approval commits) — wait for it to actually fire.
	select {
	case <-rec.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("execution resumer never invoked for a tagged quick_plan approve")
	}

	// The synthesis oracle: the task creator must have been called ZERO
	// times — Synthesize (and its zero-phase completion guard) never ran.
	if parents, _ := creator.counts(); parents != 0 {
		t.Errorf("task creator calls = %d, want 0 (Synthesize must never run on a tagged row)", parents)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	// Pre-adopt observation: the approval committed; the row is approved
	// (or already executing if the resumer adopted fast) — and NEVER
	// completed with no bound task (the exact ruling-B violation).
	if got.State == StateCompleted {
		t.Fatalf("tagged quick_plan approve COMPLETED the row instantly (state=%s): the zero-phase guard ran — ruling B violated", got.State)
	}
	if got.State != StateApproved && got.State != StateExecuting {
		t.Errorf("state after tagged approve = %q, want approved (resumer owns the -> executing move)", got.State)
	}
	if got.Kind != QuickPlanKind {
		t.Errorf("kind = %q, want %q (tag survived the approve column writes)", got.Kind, QuickPlanKind)
	}
	if got.ApprovedBy != "operator" || got.ApprovedAt == nil {
		t.Errorf("approval columns not stamped: by=%q at=%v", got.ApprovedBy, got.ApprovedAt)
	}
	// Exactly one resumer call, with the right id.
	if rec.count() != 1 || rec.calls[0] != p.ID {
		t.Errorf("resumer calls = %v, want exactly [%s]", rec.calls, p.ID)
	}
	// The signoff + plan.approved committed as on the legacy path.
	signoffs, err := store.GetSignoffs(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetSignoffs: %v", err)
	}
	approved := 0
	for _, s := range signoffs {
		if s.Action == "approved" && s.By == "operator" {
			approved++
		}
	}
	if approved != 1 {
		t.Errorf("approved signoffs by=operator = %d, want 1", approved)
	}
}

// TestApprovePlanNoResumerKeepsRowApprovedNotCompleted pins the nil-resumer
// degradation: the tagged row's approve commits and the row WAITS at
// approved. The old instant-complete (state completed, zero phases, no
// task) must NOT happen even when nobody wired the execution half.
func TestApprovePlanNoResumerKeepsRowApprovedNotCompleted(t *testing.T) {
	mgr, store, creator := newRulingBManager(t)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Ungated brief", "plan the rework", "proj-b2", t.TempDir(), "sess-b2")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitForApproval(ctx, p.ID, QuickPlanKind); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}
	mgr.execMu.RLock()
	wired := mgr.planExecResumer != nil
	mgr.execMu.RUnlock()
	if wired {
		t.Fatal("fresh manager reports a resumer: zero-value seam is dirty")
	}
	// Setter nil-guard pin (AGENTS rule): nil never clobbers, never panics.
	mgr.SetPlanExecutionResumer(nil)
	mgr.execMu.RLock()
	wired = mgr.planExecResumer != nil
	mgr.execMu.RUnlock()
	if wired {
		t.Error("SetPlanExecutionResumer(nil) installed something: setter not nil-guarded")
	}

	if err := mgr.ApprovePlan(ctx, p.ID, "", "operator"); err != nil {
		t.Fatalf("ApprovePlan without a resumer: %v", err)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State == StateCompleted {
		t.Fatal("unwired-resumer approve completed the zero-phase brief (the #177 bug)")
	}
	if got.State != StateApproved {
		t.Errorf("state = %q, want approved (honest degradation waits for an execution driver)", got.State)
	}
	if parents, _ := creator.counts(); parents != 0 {
		t.Errorf("task creator calls = %d, want 0 (no synthesis on the tagged path)", parents)
	}

	// The reclaimer must NOT rescue it through Synthesize: a tagged
	// approved+unbound row is skipped, not expanded (no redrive, no
	// completion).
	counts, err := mgr.runSynthesisReclaimPass(ctx)
	if err != nil {
		t.Fatalf("runSynthesisReclaimPass: %v", err)
	}
	if counts.redriven != 0 {
		t.Errorf("reclaimer redriven = %d, want 0 (kind-skip)", counts.redriven)
	}
	if counts.skipped < 1 {
		t.Errorf("reclaimer skipped = %d, want >=1 (the tagged row counted as left-alone)", counts.skipped)
	}
	if got2, gerr := store.GetPlan(ctx, p.ID); gerr != nil {
		t.Fatalf("re-read after reclaim: %v", gerr)
	} else if got2.State == StateCompleted {
		t.Fatal("reclaimer completed the tagged brief via zero-phase Synthesize (kind-skip is dead)")
	} else if got2.State != StateApproved {
		t.Errorf("state after reclaim pass = %q, want approved (untouched)", got2.State)
	}
	if parents, _ := creator.counts(); parents != 0 {
		t.Errorf("task creator calls after reclaim = %d, want 0", parents)
	}
}

// TestReclaimerReDrivesApprovedUnboundQuickPlanViaResumer pins M1
// (bughunt 2026-11-27): an approved, unbound, tagged row stranded by a
// resumer failure is RE-INVOKED through the execution resumer by the
// reclaimer pass — no longer a dead end. The adopt-failure-stamped shape
// (C-5) stays skipped, and an unwired resumer keeps the honest wait.
func TestReclaimerReDrivesApprovedUnboundQuickPlanViaResumer(t *testing.T) {
	mgr, store, creator := newRulingBManager(t)
	ctx := context.Background()

	// (1) Stranded-by-resumer-failure shape: approve commits, the resumer
	// errors, the row stays approved + unbound. The next reclaim pass
	// must re-invoke the resumer.
	p, err := mgr.CreatePlan(ctx, "Stranded brief", "plan the rework", "proj-m1", t.TempDir(), "sess-m1")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitForApproval(ctx, p.ID, QuickPlanKind); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}
	rec := &resumerRecorder{errOn: errors.New("resumer down"), entered: make(chan struct{}, 8)}
	mgr.SetPlanExecutionResumer(rec.fn)
	if err := mgr.ApprovePlan(ctx, p.ID, "sess-m1", "operator"); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	// The resumer runs on a bounded goroutine — wait for the first fire.
	select {
	case <-rec.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("resumer never fired on approve; pin precondition broken")
	}
	if got, gerr := store.GetPlan(ctx, p.ID); gerr != nil || got.State != StateApproved || got.TaskID != "" {
		t.Fatalf("precondition: state=%s err=%v, want approved+unbound after resumer failure", got.State, gerr)
	}
	firstCalls := rec.count()

	counts, err := mgr.runSynthesisReclaimPass(ctx)
	if err != nil {
		t.Fatalf("runSynthesisReclaimPass: %v", err)
	}
	if counts.redriven != 1 {
		t.Errorf("reclaimer redriven = %d, want 1 (the stranded tagged row)", counts.redriven)
	}
	if rec.count() != firstCalls+1 {
		t.Errorf("resumer calls = %d, want %d+1 (re-invoked once by the reclaimer)", rec.count(), firstCalls)
	}
	if parents, _ := creator.counts(); parents != 0 {
		t.Errorf("task creator calls = %d, want 0 (re-drive rides the resumer, never Synthesize)", parents)
	}

	// (2) C-5 adopt-failure shape stays skipped: stamp the row and the
	// pass must not re-invoke the resumer for it.
	if err := mgr.MarkAdoptFailure(ctx, p.ID, errors.New("binding refused twice")); err != nil {
		t.Fatalf("MarkAdoptFailure: %v", err)
	}
	before := rec.count()
	counts, err = mgr.runSynthesisReclaimPass(ctx)
	if err != nil {
		t.Fatalf("runSynthesisReclaimPass(stamped): %v", err)
	}
	if counts.redriven != 0 {
		t.Errorf("reclaimer redriven (stamped row) = %d, want 0 (C-5 skip)", counts.redriven)
	}
	if rec.count() != before {
		t.Errorf("resumer fired for an adopt-failure-stamped row: %d -> %d", before, rec.count())
	}

	// (3) Unwired resumer keeps the honest approved-unbound wait (no panic,
	// no synthesis).
	mgr.SetPlanExecutionResumer(nil)
	if _, err := mgr.runSynthesisReclaimPass(ctx); err != nil {
		t.Fatalf("runSynthesisReclaimPass(unwired): %v", err)
	}
	if parents, _ := creator.counts(); parents != 0 {
		t.Errorf("task creator calls after unwired pass = %d, want 0", parents)
	}
}

// TestSynthesizeKindGuardRefusesQuickPlanRows pins the last-line guard: a
// DIRECT Synthesize call (any caller) on a tagged row is a silent no-op —
// the row stays put and no zero-phase completion can fire, even if a
// future caller forgets the ApprovePlan branch.
func TestSynthesizeKindGuardRefusesQuickPlanRows(t *testing.T) {
	mgr, store, creator := newRulingBManager(t)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Direct synth probe", "brief", "proj-b3", t.TempDir(), "sess-b3")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitAndApproveNoSynthesis(ctx, p.ID); err != nil {
		t.Fatalf("SubmitAndApproveNoSynthesis: %v", err)
	}
	// The auto-approve lane tags its own contract (posture symmetry).
	if got, gerr := store.GetPlan(ctx, p.ID); gerr != nil {
		t.Fatalf("GetPlan: %v", gerr)
	} else if got.Kind != QuickPlanKind {
		t.Errorf("SubmitAndApproveNoSynthesis row kind = %q, want %q", got.Kind, QuickPlanKind)
	}

	if err := mgr.Synthesize(ctx, p.ID); err != nil {
		t.Fatalf("Synthesize(kind-tagged): %v, want nil no-op", err)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan after synth: %v", err)
	}
	if got.State != StateApproved {
		t.Errorf("state after direct Synthesize = %q, want approved (guard refused the run)", got.State)
	}
	if parents, _ := creator.counts(); parents != 0 {
		t.Errorf("task creator calls = %d, want 0", parents)
	}
}

// TestApprovePlanUntaggedStillSynthesizes pins the OTHER half of the
// branch: a NON-quick-plan row (plain docs-plan park, kind "") keeps the
// legacy approve -> Synthesize tail byte-for-byte (the zero-phase path
// stays legal for docs plans — ruling B scopes to tagged briefs only).
func TestApprovePlanUntaggedStillSynthesizes(t *testing.T) {
	mgr, store, creator := newRulingBManager(t)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Docs plan", "plain docs plan", "proj-b4", t.TempDir(), "sess-b4")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	// Seed one phase with steps so the LEGAL zero-or-more synthesis path
	// has something to expand (the re-drive oracle), then park UNtagged.
	if err := mgr.store.CreatePhase(ctx, NewPlanPhase(p.ID, "Phase 1", 1, 2)); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	if err := mgr.SubmitForApproval(ctx, p.ID, ""); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}
	if got, gerr := store.GetPlan(ctx, p.ID); gerr != nil || got.Kind != "" {
		t.Fatalf("non-quick-plan park must carry NO tag: kind=%q err=%v", got.State, gerr)
	}

	// A resumer IS wired: it must NOT fire for untagged rows — the tag,
	// not the seam's presence, gates the branch.
	rec := &resumerRecorder{}
	mgr.SetPlanExecutionResumer(rec.fn)

	if err := mgr.ApprovePlan(ctx, p.ID, "sess-b4", "reviewer"); err != nil {
		t.Fatalf("ApprovePlan(untagged): %v", err)
	}
	if rec.count() != 0 {
		t.Errorf("resumer fired for an untagged approve: calls=%v, want none", rec.calls)
	}
	// Synthesis ran: the parent task was created (creator oracle) and the
	// row left approved (expanded), exactly the pre-ruling shape.
	harvest := 0
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && harvest == 0 {
		if parents, _ := creator.counts(); parents > 0 {
			harvest = parents
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if harvest == 0 {
		t.Error("untagged approve did not run Synthesize (no parent task created): the legacy tail regressed")
	}
}

// TestApprovePlanForExecutionSkipsSynthesis pins the approve primitive:
// pending_approval -> approved with signoff/columns/event and NO synthesis,
// idempotent on re-drive, refuses other states.
func TestApprovePlanForExecutionSkipsSynthesis(t *testing.T) {
	mgr, store, creator := newRulingBManager(t)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Primitive probe", "brief", "proj-b5", t.TempDir(), "sess-b5")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.store.CreatePhase(ctx, NewPlanPhase(p.ID, "Phase 1", 1, 2)); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	if err := mgr.SubmitForApproval(ctx, p.ID, QuickPlanKind); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}

	if err := mgr.ApprovePlanForExecution(ctx, p.ID, "operator", "sess-b5"); err != nil {
		t.Fatalf("ApprovePlanForExecution: %v", err)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateApproved {
		t.Errorf("state = %q, want approved", got.State)
	}
	if parents, _ := creator.counts(); parents != 0 {
		t.Errorf("creator calls = %d, want 0 (no synthesis in the primitive)", parents)
	}
	// Idempotent re-drive.
	if err := mgr.ApprovePlanForExecution(ctx, p.ID, "operator", "sess-b5"); err != nil {
		t.Errorf("ApprovePlanForExecution re-drive: got %v, want nil", err)
	}
	// Foreign state refused with ErrPlanNotModifiable (cancelled row).
	q, err := mgr.CreatePlan(ctx, "Cancelled probe", "x", "proj-b5", t.TempDir(), "sess-b5")
	if err != nil {
		t.Fatalf("CreatePlan(q): %v", err)
	}
	if err := mgr.store.SetPlanState(ctx, q.ID, StateCancelled); err != nil {
		t.Fatalf("force cancelled: %v", err)
	}
	if err := mgr.ApprovePlanForExecution(ctx, q.ID, "operator", ""); err == nil {
		t.Error("ApprovePlanForExecution(cancelled): got nil, want rejection")
	} else if !errorsIsNotModifiable(err) {
		t.Errorf("error = %v, want ErrPlanNotModifiable", err)
	}
}

func errorsIsNotModifiable(err error) bool {
	return errors.Is(err, ErrPlanNotModifiable)
}
