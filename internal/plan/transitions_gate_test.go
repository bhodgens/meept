package plan

// Gate tests for branch 03-plans-goals, leaf 00T: plan CAS primitive (C-P1),
// transition table + guarded entry (C-P4), manager migration race, phase
// failure/confirm writers, and hygiene pins (F7/F8). TESTS ONLY — written
// against NOT-YET-EXISTING symbols of leaves 02/03/04/06. Per-test RED
// reasons are documented at each test. Audit refs: M-6, M-7, F7, F8.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// ---------------------------------------------------------------------------
// C-P1: store-level CAS primitive
// ---------------------------------------------------------------------------

// TestUpdatePlanStateWhere pins contract C-P1 (leaf 03):
//
//	func (s *SQLiteStore) UpdatePlanStateWhere(ctx, id string, from []PlanState, to PlanState) (bool, error)
//
// Legal from-set match -> (true, nil) + state moved; from-set mismatch ->
// (false, nil) with the row UNCHANGED; missing row -> (false, nil), never an
// error (0 rows = lost race; error only on DB failure).
//
// RED: SQLiteStore.UpdatePlanStateWhere undefined (compile failure).
func TestUpdatePlanStateWhere(t *testing.T) {
	ctx := t.Context()
	store := setupTestStore(t)

	p := newTestPlan("cas-primitive")
	p.State = StatePendingApproval
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Legal from-set: exact current state.
	won, err := store.UpdatePlanStateWhere(ctx, p.ID, []PlanState{StatePendingApproval}, StateApproved)
	if err != nil {
		t.Fatalf("legal CAS errored: %v", err)
	}
	if !won {
		t.Fatal("legal CAS lost: from-set matches the row state")
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil || got.State != StateApproved {
		t.Fatalf("state after winning CAS: %v (err %v), want approved", got.State, err)
	}

	// Multi-value from-set is an IN(...) match, not equality on one state.
	p2 := newTestPlan("cas-fromset")
	p2.State = StateFailed
	if err := store.CreatePlan(ctx, p2); err != nil {
		t.Fatalf("CreatePlan p2: %v", err)
	}
	won, err = store.UpdatePlanStateWhere(ctx, p2.ID, []PlanState{StateCompleted, StateFailed, StateCancelled}, StatePlanning)
	if err != nil || !won {
		t.Fatalf("from-set CAS: won=%v err=%v, want (true, nil)", won, err)
	}

	// Mismatch: row already moved out of every listed from-state.
	won, err = store.UpdatePlanStateWhere(ctx, p2.ID, []PlanState{StateDraft}, StateExecuting)
	if err != nil {
		t.Fatalf("mismatched CAS must NOT error (0 rows is a lost race, not a DB failure): %v", err)
	}
	if won {
		t.Error("mismatched from-set must return false")
	}
	got2, err := store.GetPlan(ctx, p2.ID)
	if err != nil {
		t.Fatalf("GetPlan p2: %v", err)
	}
	if got2.State != StatePlanning {
		t.Errorf("state changed on a lost CAS: got %s, want planning (unchanged)", got2.State)
	}

	// Missing row: (false, nil), never an error.
	won, err = store.UpdatePlanStateWhere(ctx, "plan-does-not-exist", []PlanState{StateDraft}, StateApproved)
	if err != nil {
		t.Errorf("missing row must not error: %v", err)
	}
	if won {
		t.Error("missing row CAS must report false")
	}
}

// ---------------------------------------------------------------------------
// C-P4: transition table + single guarded entry point
// ---------------------------------------------------------------------------

// allPlanStates enumerates every PlanState enum value — the table must have a
// row for each so an illegal transition is unrepresentable, not merely
// unguarded.
var allPlanStates = []PlanState{
	StatePlanning, StateDraft, StatePendingApproval, StateApproved,
	StateSynthesizing, StateExecuting, StateCompleted, StateConfirmed,
	StateCancelled, StateFailed,
}

func gateTableAllows(from, to PlanState) bool {
	for _, t := range planTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// TestPlanTransitionTable pins contract C-P4 (leaf 04, table-driven design
// amendment): the package-level planTransitions map is the COMPLETE, single
// source of legal plan transitions, and manager transitions flow through the
// table-driven transition() entry — NOT per-method hand-rolled guards.
//
// Asserts: every enum has a row; terminal states (confirmed) have an empty
// to-set; each audited edge is a member (machine == table); non-members
// (e.g. draft->executing) are absent AND refused by transition() without
// mutating the row.
//
// RED: planTransitions undefined + (*PlanManager).transition undefined
// (compile failure — both symbols land in leaf 04).
func TestPlanTransitionTable(t *testing.T) {
	ctx := t.Context()

	// (1) Completeness: every enum value has a row (terminal states included
	// as explicit empty entries, not missing keys).
	for _, s := range allPlanStates {
		if _, ok := planTransitions[s]; !ok {
			t.Errorf("planTransitions has no row for state %q", s)
		}
	}
	// (2) Terminal to-sets.
	if len(planTransitions[StateConfirmed]) != 0 {
		t.Errorf("confirmed must be terminal: to-set = %v, want empty", planTransitions[StateConfirmed])
	}
	if len(planTransitions[StatePlanning]) != 0 {
		t.Errorf("planning is a creation-entry state with no forward edges: to-set = %v, want empty", planTransitions[StatePlanning])
	}

	// (3) Every audited edge is a member — the audited state machine == table.
	audited := []struct{ from, to PlanState }{
		{StateDraft, StatePendingApproval},
		{StateDraft, StatePlanning},
		{StateDraft, StateCancelled},
		{StatePendingApproval, StateApproved},
		{StatePendingApproval, StateCancelled},
		{StatePendingApproval, StatePlanning},
		{StateApproved, StateSynthesizing},
		{StateApproved, StateCancelled},
		{StateSynthesizing, StateExecuting},
		{StateSynthesizing, StateDraft},
		{StateSynthesizing, StateFailed},
		{StateSynthesizing, StateApproved},
		{StateExecuting, StateCompleted},
		{StateExecuting, StateCancelled},
		{StateExecuting, StateFailed},
		{StateCompleted, StateConfirmed},
		{StateFailed, StateConfirmed},
		{StateFailed, StatePlanning},
		{StateCancelled, StateConfirmed},
		{StateCancelled, StatePlanning},
	}
	for _, e := range audited {
		if !gateTableAllows(e.from, e.to) {
			t.Errorf("audited edge missing from table: %s -> %s", e.from, e.to)
		}
	}

	// (4) Non-members are absent.
	nonMembers := []struct{ from, to PlanState }{
		{StateDraft, StateExecuting},
		{StateDraft, StateConfirmed},
		{StatePendingApproval, StateExecuting},
		{StateApproved, StateCompleted},
		{StateExecuting, StateApproved},
		{StateCompleted, StateExecuting},
		{StateConfirmed, StateDraft},
		{StateConfirmed, StateCancelled},
		{StatePlanning, StateDraft},
	}
	for _, e := range nonMembers {
		if gateTableAllows(e.from, e.to) {
			t.Errorf("illegal edge present in table: %s -> %s", e.from, e.to)
		}
	}

	// (5) transition() is the guarded entry: legal edge applies via the CAS,
	// non-member is refused and the row stays untouched.
	mgr := setupTestManager(t)
	p := NewPlan("table-entry", "d", "", "", "")
	p.State = StateDraft
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.transition(ctx, p.ID, StateExecuting); err == nil {
		t.Error("transition(draft -> executing) must be refused: not a table member")
	}
	if got, err := mgr.store.GetPlan(ctx, p.ID); err != nil {
		t.Fatalf("GetPlan after refusal: %v", err)
	} else if got.State != StateDraft {
		t.Errorf("refused transition mutated the row: state = %s, want draft", got.State)
	}
	if err := mgr.transition(ctx, p.ID, StatePendingApproval); err != nil {
		t.Errorf("transition(draft -> pending_approval) is a table member, want applied: %v", err)
	}
	if got, err := mgr.store.GetPlan(ctx, p.ID); err != nil {
		t.Fatalf("GetPlan: %v", err)
	} else if got.State != StatePendingApproval {
		t.Errorf("state after legal transition = %s, want pending_approval", got.State)
	}
}

// ---------------------------------------------------------------------------
// Manager CAS race (leaf 04 T2): exactly-one-winner under TOCTOU interleave
// ---------------------------------------------------------------------------

// gateTOCTOUStore decorates a PlanStore so the FIRST GetPlan blocks until a
// second arrives — forcing both racing managers to observe the same
// pre-transition state before either writes. That is the exact interleave the
// hand-rolled read-check-write (today's CancelPlan/ConfirmPlan) loses. A 2s
// bound keeps the gate from ever deadlocking the suite.
type gateTOCTOUStore struct {
	PlanStore
	mu      sync.Mutex
	arrived int
	release chan struct{}
}

func newGateTOCTOUStore(inner PlanStore) *gateTOCTOUStore {
	return &gateTOCTOUStore{PlanStore: inner, release: make(chan struct{}, 1)}
}

func (g *gateTOCTOUStore) GetPlan(ctx context.Context, id string) (*Plan, error) {
	g.mu.Lock()
	g.arrived++
	first := g.arrived == 1
	g.mu.Unlock()
	if first {
		select {
		case <-g.release:
		case <-time.After(2 * time.Second):
		}
	} else {
		select {
		case g.release <- struct{}{}:
		default:
		}
	}
	return g.PlanStore.GetPlan(ctx, id)
}

// TestManagerTransitionsUseCAS pins the migration race shape: concurrent
// CancelPlan vs ConfirmPlan on a COMPLETED plan, interleaved so both read
// `completed` before either writes, must produce exactly one winner — the
// loser gets a typed not-modifiable error — and the final row must equal the
// winner's state.
//
// Behavioral RED at HEAD: CancelPlan (SetPlanState, unconditional) and
// ConfirmPlan (full-row UpdatePlan from a stale in-memory copy) BOTH return
// nil — last-writer-wins, one caller's verdict silently clobbered. After
// leaf 04 every transition is transition()->UpdatePlanStateWhere(from) and
// the loser's CAS affects 0 rows -> typed error.
func TestManagerTransitionsUseCAS(t *testing.T) {
	ctx := t.Context()
	sqlite := setupTestStore(t)
	gated := newGateTOCTOUStore(sqlite)

	p := newTestPlan("cancel-vs-confirm")
	p.State = StateCompleted
	if err := sqlite.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	mgrCancel := NewPlanManager(gated, nil, gateCfg(), nil, discardGateLogger())
	mgrConfirm := NewPlanManager(gated, nil, gateCfg(), nil, discardGateLogger())

	start := make(chan struct{})
	var cancelErr, confirmErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		cancelErr = mgrCancel.CancelPlan(ctx, p.ID, "operator stop")
	}()
	go func() {
		defer wg.Done()
		<-start
		confirmErr = mgrConfirm.ConfirmPlan(ctx, p.ID, "", "operator ok")
	}()
	close(start)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("cancel/confirm race did not settle within 20s")
	}
	// Let any straggler file-update goroutines settle before reading the row.
	time.Sleep(50 * time.Millisecond)

	final, err := sqlite.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if final.State != StateCancelled && final.State != StateConfirmed {
		t.Fatalf("final state %s is neither racer's target", final.State)
	}

	successes := 0
	if cancelErr == nil {
		successes++
	}
	if confirmErr == nil {
		successes++
	}
	if successes != 1 {
		t.Errorf("exactly one racer must win: successes=%d (cancelErr=%v confirmErr=%v) — "+
			"two nil errors means the loser's write was silently clobbered (last-writer-wins); "+
			"the CAS migration (leaf 04) must return a typed error to the loser",
			successes, cancelErr, confirmErr)
		return
	}
	// Winner consistency: the caller that reported success owns the final row.
	if cancelErr == nil && final.State != StateCancelled {
		t.Errorf("CancelPlan reported success but final state = %s, want cancelled", final.State)
	}
	if confirmErr == nil && final.State != StateConfirmed {
		t.Errorf("ConfirmPlan reported success but final state = %s, want confirmed", final.State)
	}
	// The loser's error is typed-not-modifiable: the row EXISTS (not
	// ErrPlanNotFound) and the store did not fail (not a DB error).
	loser := cancelErr
	if loser == nil {
		loser = confirmErr
	}
	if errors.Is(loser, ErrPlanNotFound) {
		t.Errorf("lost race must surface a not-modifiable error, not ErrPlanNotFound: %v", loser)
	}
}

// ---------------------------------------------------------------------------
// Leaf 02: PhaseFailed / PhaseConfirmed writers
// ---------------------------------------------------------------------------

// TestPhaseFailedWriter pins M-6's failure half: a phase whose failed steps
// reach its total (recorded via the existing IncrementPhaseProgress
// accounting — no parallel counter) must land in PhaseFailed, and the plan
// completion evaluation must treat a failed phase as plan StateFailed instead
// of leaving the plan stuck.
//
// Part A calls the new step-failure entry (the OnStepCompleted counterpart
// leaf 02 T1 adds): RED as an undefined-symbol compile failure.
// Part B is the completion-evaluation half (leaf 02 T2): behavioral RED —
// today completePlan's all-phases check only accepts PhaseCompleted, so a
// plan with [failed, completed] phases stays executing forever.
func TestPhaseFailedWriter(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	// --- Part A: failed_steps >= total_steps -> PhaseFailed.
	p := NewPlan("phase-fail-writer", "d", "", "", "")
	p.State = StateExecuting
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	ph := NewPlanPhase(p.ID, "Phase A", 1, 3)
	ph.State = PhaseInProgress
	ph.TaskID = "task-fail-phase"
	if err := mgr.store.CreatePhase(ctx, ph); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	mgr.mu.Lock()
	mgr.phaseTaskMap["task-fail-phase"] = ph.ID
	mgr.taskPlanMap["task-fail-phase"] = p.ID
	mgr.mu.Unlock()

	// The step-failure signal (the executor seam leaf 02 wires): three failed
	// steps on a total-of-3 phase.
	for i := 0; i < 3; i++ {
		if err := mgr.OnStepFailed(ctx, "task-fail-phase", "step-1"); err != nil {
			t.Fatalf("OnStepFailed %d: %v", i, err)
		}
	}
	phases, err := mgr.store.GetPhases(ctx, p.ID)
	if err != nil || len(phases) != 1 {
		t.Fatalf("GetPhases: %v (n=%d)", err, len(phases))
	}
	if phases[0].FailedSteps != 3 {
		t.Errorf("failed_steps = %d, want 3 (existing IncrementPhaseProgress accounting must carry it)", phases[0].FailedSteps)
	}
	if phases[0].State != PhaseFailed {
		t.Errorf("phase with all steps failed sits in %s, want %s — the PhaseFailed writer is missing",
			phases[0].State, PhaseFailed)
	}

	// --- Part B: a failed phase must fail the plan, not stall it.
	ph2 := NewPlanPhase(p.ID, "Phase B", 2, 1)
	ph2.State = PhaseInProgress
	ph2.TaskID = "task-ok-phase"
	if err := mgr.store.CreatePhase(ctx, ph2); err != nil {
		t.Fatalf("CreatePhase B: %v", err)
	}
	mgr.mu.Lock()
	mgr.phaseTaskMap["task-ok-phase"] = ph2.ID
	mgr.taskPlanMap["task-ok-phase"] = p.ID
	mgr.mu.Unlock()
	// Deterministic pre-condition: phase A failed, phase B in flight.
	if err := mgr.store.SetPhaseState(ctx, ph.ID, PhaseFailed); err != nil {
		t.Fatalf("SetPhaseState A failed: %v", err)
	}
	if err := mgr.store.SetPlanState(ctx, p.ID, StateExecuting); err != nil {
		t.Fatalf("SetPlanState executing: %v", err)
	}

	// Completing the healthy last phase resolves the evaluation: all
	// terminal, but one is FAILED -> the plan must be failed, not completed
	// and not stuck.
	if err := mgr.OnTaskCompleted(ctx, "task-ok-phase"); err != nil {
		t.Fatalf("OnTaskCompleted: %v", err)
	}
	final, err := mgr.store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if final.State != StateFailed {
		t.Errorf("plan with [failed, completed] phases ended %s, want %s "+
			"(completion eval must account for failed phases)", final.State, StateFailed)
	}
}

// TestPhaseConfirmedWriter pins M-6's confirm half (leaf 02 T3): ConfirmPlan
// must cascade PhaseConfirmed to the plan's terminal phases. Today
// ConfirmPlan touches only the plan row — phase rows keep their pre-confirm
// state forever.
//
// Behavioral RED (no new symbols): phases stay completed after confirm.
func TestPhaseConfirmedWriter(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	p := NewPlan("phase-confirm-cascade", "d", "", "", "")
	p.State = StateCompleted
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	ids := make([]string, 0, 2)
	for seq := 1; seq <= 2; seq++ {
		ph := NewPlanPhase(p.ID, "Phase done", seq, 1)
		ph.State = PhaseCompleted
		if err := mgr.store.CreatePhase(ctx, ph); err != nil {
			t.Fatalf("CreatePhase: %v", err)
		}
		ids = append(ids, ph.ID)
	}

	if err := mgr.ConfirmPlan(ctx, p.ID, "", "reviewer"); err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	phases, err := mgr.store.GetPhases(ctx, p.ID)
	if err != nil || len(phases) != 2 {
		t.Fatalf("GetPhases: %v (n=%d)", err, len(phases))
	}
	for _, ph := range phases {
		if ph.State != PhaseConfirmed {
			t.Errorf("phase %s is %s after ConfirmPlan, want %s — confirm does not cascade to terminal phases",
				ph.ID, ph.State, PhaseConfirmed)
		}
	}
}

// ---------------------------------------------------------------------------
// Leaf 06 hygiene: F7 (revise accepts failed), F8 (cancel cascades phases)
// ---------------------------------------------------------------------------

// TestRevisePlanAcceptsFailed pins F7 (audit leaf 06 T1; the failed->planning
// table row lands in leaf 04): a failed plan is revisable — RevisePlan must
// send it to planning instead of refusing. (Failed plans are also
// confirmable; confirm-vs-revise is the operator's choice.)
//
// Behavioral RED at HEAD: RevisePlan's guard accepts only pending_approval
// and cancelled.
func TestRevisePlanAcceptsFailed(t *testing.T) {
	ctx := t.Context()
	mgr := setupTestManager(t)

	p := NewPlan("revise-from-failed", "d", "", "", "")
	p.State = StateFailed
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.RevisePlan(ctx, p.ID, "sess-1", "try a different approach"); err != nil {
		t.Fatalf("RevisePlan on a failed plan must succeed (F7): %v", err)
	}
	got, err := mgr.store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StatePlanning {
		t.Errorf("failed plan after RevisePlan: state = %s, want %s", got.State, StatePlanning)
	}
}

// gateCancelCreator records task-level cancellations so the test can prove
// CancelPlan reaches the existing task-cancel machinery (leaf 06: "live child
// tasks cancelled via existing task-cancel path"), rather than flipping only
// the plan row. If the implementation widens TaskCreator with a CancelTask
// method, this fake satisfies it; today nothing calls it.
type gateCancelCreator struct {
	mu      sync.Mutex
	cancels []string
}

func (c *gateCancelCreator) CreateTask(ctx context.Context, name, description string) (*task.Task, error) {
	return task.NewTask(name, description), nil
}
func (c *gateCancelCreator) CreateTaskStep(_ context.Context, _, _ string, _ int) (*task.TaskStep, error) {
	return task.NewTaskStep("t", "s", 0), nil
}
func (c *gateCancelCreator) UpdateTaskStep(context.Context, *task.TaskStep) error { return nil }
func (c *gateCancelCreator) LinkSession(context.Context, string, string) error    { return nil }
func (c *gateCancelCreator) SetTaskJobCount(context.Context, string, int) error   { return nil }
func (c *gateCancelCreator) ScheduleSteps(context.Context, string) error          { return nil }

// CancelTask — the seam leaf 06's cascade uses. Recorded, never called at HEAD.
func (c *gateCancelCreator) CancelTask(ctx context.Context, taskID, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancels = append(c.cancels, taskID)
	return nil
}

func (c *gateCancelCreator) cancelled() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.cancels))
	copy(out, c.cancels)
	return out
}

// TestCancelCancelsPhases pins F8: CancelPlan on an EXECUTING plan must
// cascade — no phase may remain in_progress after cancel (phases reset to
// 'pending'/terminal per leaf 06's Q2 note), and the live child tasks must
// reach the existing task-cancel machinery. Today CancelPlan writes only the
// plan row: phases are orphaned in in_progress forever and no task is
// cancelled.
//
// Behavioral RED at HEAD (compiles clean — only observable behavior is
// missing).
func TestCancelCancelsPhases(t *testing.T) {
	ctx := t.Context()
	sqlite := setupTestStore(t)
	creator := &gateCancelCreator{}
	mgr := NewPlanManager(sqlite, nil, gateCfg(), creator, discardGateLogger())

	p := NewPlan("cancel-cascade", "d", "", "", "")
	p.State = StateExecuting
	p.TaskID = "task-cancel-parent"
	if err := sqlite.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	phA := NewPlanPhase(p.ID, "Running", 1, 2)
	phA.State = PhaseInProgress
	phA.TaskID = "task-cancel-child-a"
	phB := NewPlanPhase(p.ID, "Queued", 2, 2)
	phB.State = PhasePending
	phB.TaskID = "task-cancel-child-b"
	for _, ph := range []*PlanPhase{phA, phB} {
		if err := sqlite.CreatePhase(ctx, ph); err != nil {
			t.Fatalf("CreatePhase %s: %v", ph.Name, err)
		}
	}

	if err := mgr.CancelPlan(ctx, p.ID, "operator stop"); err != nil {
		t.Fatalf("CancelPlan: %v", err)
	}

	phases, err := sqlite.GetPhases(ctx, p.ID)
	if err != nil || len(phases) != 2 {
		t.Fatalf("GetPhases: %v (n=%d)", err, len(phases))
	}
	for _, ph := range phases {
		if ph.State == PhaseInProgress {
			t.Errorf("phase %s (%s) left in_progress after CancelPlan: F8 requires the cascade "+
				"to reset live phases (pending or terminal, per leaf 06 Q2 note)", ph.ID, ph.Name)
		}
	}
	if len(creator.cancelled()) == 0 {
		// Advisory: the plan-row + phase cascade above is the observable
		// package-internal pin; the exact task-cancel seam (a TaskCreator
		// method vs an optional interface) is leaf 06's implementation call.
		// The fake implements TaskCreator(ctx, taskID, reason); if the seam
		// differs, the orchestrator wires the recording here.
		t.Errorf("CancelPlan reached no task-cancel machinery on the creator: the executing plan's " +
			"live child tasks (task-cancel-child-a/b) must receive the existing cancellation (F8)")
	}
}

// discardGateLogger silences gate-test manager logs (Go 1.27 slog.DiscardHandler).
func discardGateLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
