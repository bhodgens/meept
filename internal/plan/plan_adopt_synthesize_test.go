package plan

// Plan-contract quickplan pins (tree 20261125-plan-contract-quickplan leaf
// 01, master C-1/C-2, decisions D1/D2; closes bughunt 2026-11-24 M11).
//
// X2 linkage shape: the quick_plan decomposition happens in the STRATEGIC
// PLANNER task (dispatcher d.createTask), not in Synthesize — and Synthesize
// on a zero-phase draft completes the plan instantly (the M11 zombie/lie
// trap). So the dispatcher creates the task FIRST and the plan machine ADOPTS
// it: SubmitAndApproveNoSynthesis (submit+approve CAS chain WITHOUT calling
// Synthesize), then AdoptTask (claim-guarded task_id empty -> set, maps
// populated, approved -> synthesizing -> executing). OnTaskCompleted then
// rolls the plan up from its bound task (D2) through the existing
// taskPlanMap machinery.

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// newSharedStoreManager builds a PlanManager over a caller-owned SQLite store
// so a second manager can be built over the same file (restart parity).
func newSharedStoreManager(t *testing.T, store *SQLiteStore, requireApproval bool) *PlanManager {
	t.Helper()
	cfg := config.PlansConfig{
		Mode:      "threshold",
		Threshold: config.PlansThresholdConfig{MinSteps: 3},
		Storage:   config.PlansStorageConfig{DefaultPath: "docs/plans"},
		Approval:  config.PlansApprovalConfig{RequireApproval: requireApproval, MaxRevisions: 3},
	}
	return NewPlanManager(store, nil, cfg, nil, slog.New(slog.DiscardHandler))
}

// TestSubmitForApproval_ParksRegardlessOfConfig pins C-3's true posture:
// approval_requested parks the row at pending_approval even when the config
// says auto-approve — the gate is intent-driven, never config-driven.
func TestSubmitForApproval_ParksRegardlessOfConfig(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "adopt.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// RequireApproval=false: SubmitPlan would auto-approve here.
	mgr := newSharedStoreManager(t, store, false)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Quick plan park", "implement only after I approve", "proj-1", t.TempDir(), "sess-park")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitForApproval(ctx, p.ID); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StatePendingApproval {
		t.Errorf("state after SubmitForApproval: got %q, want %q", got.State, StatePendingApproval)
	}
	// Idempotent re-submit on an already-parked row: no-op success.
	if err := mgr.SubmitForApproval(ctx, p.ID); err != nil {
		t.Errorf("SubmitForApproval on parked row: got %v, want nil", err)
	}
}

// TestAdoptTask_BindsTaskRollsupAndLeavesNoZombie is the M11 core pin: a
// quick_plan-shaped draft (ZERO phases) reaches executing with TaskID set,
// no pending_approval zombie row survives, NO synthesis tasks are created
// (the planner task is the single primary task), and OnTaskCompleted rolls
// the plan to completed through the adopted mapping (D2).
func TestAdoptTask_BindsTaskRollsupAndLeavesNoZombie(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "adopt.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mgr := newSharedStoreManager(t, store, true)
	creator := &countingCreator{}
	mgr.taskCreator = creator
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Refactor auth end to end", "plan the refactor then do it", "proj-1", t.TempDir(), "sess-adopt")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	if err := mgr.SubmitAndApproveNoSynthesis(ctx, p.ID); err != nil {
		t.Fatalf("SubmitAndApproveNoSynthesis: %v", err)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan after submit+approve: %v", err)
	}
	if got.State != StateApproved {
		t.Fatalf("state after SubmitAndApproveNoSynthesis: got %q, want %q", got.State, StateApproved)
	}
	// The no-synthesis posture must not have run the zero-phase completion
	// guard: an approved row, not a silently completed one.
	if got.State == StateCompleted {
		t.Fatal("SubmitAndApproveNoSynthesis completed the zero-phase draft (Synthesize trap)")
	}
	// Exactly one approved signoff recorded by=system.
	signoffs, err := store.GetSignoffs(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetSignoffs: %v", err)
	}
	approved := 0
	for _, so := range signoffs {
		if so.Action == "approved" && so.By == "system" {
			approved++
		}
	}
	if approved != 1 {
		t.Errorf("approved-by-system signoffs = %d, want 1", approved)
	}

	// Adopt the planner-created task.
	if err := mgr.AdoptTask(ctx, p.ID, "task-planner-1"); err != nil {
		t.Fatalf("AdoptTask: %v", err)
	}
	got, err = store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan after adopt: %v", err)
	}
	if got.State != StateExecuting {
		t.Errorf("state after AdoptTask: got %q, want %q", got.State, StateExecuting)
	}
	if got.TaskID != "task-planner-1" {
		t.Errorf("TaskID after AdoptTask: got %q, want %q", got.TaskID, "task-planner-1")
	}

	// No zombie pending_approval row left behind anywhere.
	parked, err := store.ListPlansByState(ctx, StatePendingApproval, 100)
	if err != nil {
		t.Fatalf("ListPlansByState: %v", err)
	}
	for _, z := range parked {
		t.Errorf("zombie pending_approval row after adopt: plan %s", z.ID)
	}

	// Synthesize never ran: the planner task is the ONLY task (no
	// synthesis-created parent/child hierarchy).
	if parents, children := creator.counts(); parents != 0 || children != 0 {
		t.Errorf("AdoptTask ran synthesis (parents=%d children=%d); X2 adopts without expanding", parents, children)
	}

	// D2 rollup: the bound task completing completes the plan.
	if err := mgr.OnTaskCompleted(ctx, "task-planner-1", "completed"); err != nil {
		t.Fatalf("OnTaskCompleted: %v", err)
	}
	got, err = store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan after task completion: %v", err)
	}
	if got.State != StateCompleted {
		t.Errorf("plan state after OnTaskCompleted: got %q, want %q (D2 rollup)", got.State, StateCompleted)
	}
}

// TestAdoptTask_RejectsUnapprovedStates pins the state precondition: only an
// approved row may adopt; draft/pending_approval refuse, leaving the row
// untouched (the operator approve path stays intact).
func TestAdoptTask_RejectsUnapprovedStates(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "adopt.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mgr := newSharedStoreManager(t, store, true)
	ctx := context.Background()

	draft, err := mgr.CreatePlan(ctx, "Draft adopt", "d", "proj-1", t.TempDir(), "s")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.AdoptTask(ctx, draft.ID, "task-x"); err == nil {
		t.Error("AdoptTask on a draft plan: got nil error, want rejection")
	} else if !errors.Is(err, ErrPlanNotModifiable) {
		t.Errorf("AdoptTask draft error: got %v, want ErrPlanNotModifiable", err)
	}
	if got, gerr := store.GetPlan(ctx, draft.ID); gerr == nil {
		if got.State != StateDraft || got.TaskID != "" {
			t.Errorf("rejected adopt mutated the row: state=%q task_id=%q", got.State, got.TaskID)
		}
	}

	parked, err := mgr.CreatePlan(ctx, "Parked adopt", "p", "proj-1", t.TempDir(), "s")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitForApproval(ctx, parked.ID); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}
	if err := mgr.AdoptTask(ctx, parked.ID, "task-y"); err == nil {
		t.Error("AdoptTask on a pending_approval plan: got nil error, want rejection")
	}
}

// TestAdoptTask_IdempotentAndForeignTask pins the task_id-empty CAS: a
// same-task re-drive succeeds silently (crash recovery), a foreign task is
// refused once the row is bound, and the bound id never changes.
func TestAdoptTask_IdempotentAndForeignTask(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "adopt.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mgr := newSharedStoreManager(t, store, true)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Idempotent adopt", "i", "proj-1", t.TempDir(), "s")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitAndApproveNoSynthesis(ctx, p.ID); err != nil {
		t.Fatalf("SubmitAndApproveNoSynthesis: %v", err)
	}
	if err := mgr.AdoptTask(ctx, p.ID, "task-a"); err != nil {
		t.Fatalf("AdoptTask(task-a): %v", err)
	}
	if err := mgr.AdoptTask(ctx, p.ID, "task-a"); err != nil {
		t.Errorf("re-adopt same task: got %v, want nil (idempotent)", err)
	}
	if err := mgr.AdoptTask(ctx, p.ID, "task-b"); err == nil {
		t.Error("adopt foreign task over bound row: got nil error, want rejection")
	}
	if got, gerr := store.GetPlan(ctx, p.ID); gerr == nil && got.TaskID != "task-a" {
		t.Errorf("bound task mutated: got %q, want task-a", got.TaskID)
	}
}

// TestAdoptTask_ConcurrentBindSingleWinner pins the lease-backed CAS: two
// adopters with different task ids race on the same approved row and exactly
// one wins; the loser is told cleanly and the row carries one task id.
func TestAdoptTask_ConcurrentBindSingleWinner(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "adopt.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mgr := newSharedStoreManager(t, store, true)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Race adopt", "r", "proj-1", t.TempDir(), "s")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitAndApproveNoSynthesis(ctx, p.ID); err != nil {
		t.Fatalf("SubmitAndApproveNoSynthesis: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := make(map[string]error, 2)
	for _, id := range []string{"task-1", "task-2"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			e := mgr.AdoptTask(ctx, p.ID, id)
			mu.Lock()
			errs[id] = e
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	wins := 0
	for _, e := range errs {
		if e == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("concurrent adopt winners = %d, want exactly 1 (errs=%v)", wins, errs)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateExecuting {
		t.Errorf("state after adopt race: got %q, want %q", got.State, StateExecuting)
	}
	if got.TaskID != "task-1" && got.TaskID != "task-2" {
		t.Errorf("row task_id = %q, want one of the racing ids", got.TaskID)
	}
}

// TestAdoptTask_RestartRestoresRollup pins durable linkage: a fresh manager
// over the same store recovers the taskPlanMap through RestoreMappings so
// OnTaskCompleted still rolls the plan to completed after a restart (the
// Synthesize hierarchy survives restart; the adopted row must too).
func TestAdoptTask_RestartRestoresRollup(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "adopt.db")
	store, err := NewSQLiteStore(dbPath, logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mgr := newSharedStoreManager(t, store, true)
	ctx := context.Background()

	p, err := mgr.CreatePlan(ctx, "Restart adopt", "x", "proj-1", t.TempDir(), "s")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := mgr.SubmitAndApproveNoSynthesis(ctx, p.ID); err != nil {
		t.Fatalf("SubmitAndApproveNoSynthesis: %v", err)
	}
	if err := mgr.AdoptTask(ctx, p.ID, "task-restart"); err != nil {
		t.Fatalf("AdoptTask: %v", err)
	}

	// Fresh manager, same store: only the durable row is known.
	mgr2 := newSharedStoreManager(t, store, true)
	if err := mgr2.RestoreMappings(ctx); err != nil {
		t.Fatalf("RestoreMappings: %v", err)
	}
	if err := mgr2.OnTaskCompleted(ctx, "task-restart", "completed"); err != nil {
		t.Fatalf("OnTaskCompleted after restart: %v", err)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateCompleted {
		t.Errorf("plan state after restart rollup: got %q, want %q", got.State, StateCompleted)
	}
}
