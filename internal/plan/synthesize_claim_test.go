package plan

// Bughunt 2026-10-08 — synthesis runaway pins.
//
// The incident: PlanManager.Synthesize created the parent task and only ~20
// lines later recorded plan.task_id. Any failure in between (a failed
// CreateTaskStep, the depends_on resolution pass, a daemon restart) returned
// early with the task created and task_id never written. The plan was left at
// draft + empty task_id — indistinguishable from a plan that had never been
// expanded, so the TaskID idempotency guard could not fire. With plan approval
// auto-granted (RequireApproval=false), the next SubmitPlan re-fired the state
// CAS and Synthesize created a fresh parent task plus one child task per phase,
// every time.
//
// 797 draft plans x that loop = 20.7M tasks in ~/.meept/tasks.db at ~14k/s,
// and a 145 GB meept.log that was ~100% "Task created" lines.
//
// The fix claims the plan into StateSynthesizing with a LEASE before any task is
// created, so "expanded but died" is a visible state rather than an invisible
// one, and bounds retries so a plan that cannot expand is parked instead of
// re-entered forever.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

// countingCreator records every CreateTask call so a test can prove how many
// times a plan was expanded. failParentAt makes the Nth parent create fail, which
// models the mid-expansion crash that started the loop.
type countingCreator struct {
	mu            sync.Mutex
	parents       int
	children      int
	failParentAt  int // 1-based parent index to fail; 0 = never
	failChildAt   int // 1-based child index to fail; 0 = never
	alwaysFailPar bool
}

func (c *countingCreator) CreateTask(ctx context.Context, name, description string) (*task.Task, error) {
	// A phase child is labelled "Phase N: <name>"; the parent is the plan title.
	// Counting them in ONE bucket (not two) is what makes "how many times was this
	// plan expanded" a single number — the quantity the runaway was measured in.
	isChild := strings.Contains(name, "Phase ")
	c.mu.Lock()
	if isChild {
		c.children++
	} else {
		c.parents++
	}
	n := c.parents
	fail := c.alwaysFailPar ||
		(!isChild && c.failParentAt != 0 && n == c.failParentAt) ||
		(isChild && c.failChildAt != 0 && c.children == c.failChildAt)
	c.mu.Unlock()
	if fail {
		return nil, errSyntheticCreate
	}
	return task.NewTask(name, description), nil
}

func (c *countingCreator) CreateTaskStep(_ context.Context, _, _ string, _ int) (*task.TaskStep, error) {
	return task.NewTaskStep("t", "s", 0), nil
}

func (c *countingCreator) UpdateTaskStep(context.Context, *task.TaskStep) error { return nil }
func (c *countingCreator) LinkSession(context.Context, string, string) error    { return nil }
func (c *countingCreator) SetTaskJobCount(context.Context, string, int) error   { return nil }
func (c *countingCreator) ScheduleSteps(context.Context, string) error          { return nil }

func (c *countingCreator) counts() (parents, children int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.parents, c.children
}

var errSyntheticCreate = &syntheticCreateError{}

type syntheticCreateError struct{}

func (*syntheticCreateError) Error() string { return "synthetic CreateTask failure" }

// setupClaimManager builds a real PlanManager over a temp SQLite store with a
// counting task creator. Approval is left at the default (not required), which is
// the configuration that made the incident reachable.
func setupClaimManager(t *testing.T) (*PlanManager, *countingCreator) {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "claim.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.PlansConfig{
		Mode:      "threshold",
		Threshold: config.PlansThresholdConfig{MinSteps: 3},
		Storage:   config.PlansStorageConfig{DefaultPath: "docs/plans"},
		// RequireApproval deliberately FALSE: this is the auto-approve path that
		// let every re-expansion re-enter through SubmitPlan.
		Approval: config.PlansApprovalConfig{RequireApproval: false, MaxRevisions: 3},
	}
	creator := &countingCreator{}
	mgr := NewPlanManager(store, nil, cfg, creator, logger)
	// Synthesize reads the plan markdown from FilePath; give it a real,
	// parseable file with one phase so each pass would create parent+child.
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")
	body := "# Plan: runaway subject\n\n## Summary\n\nsubject\n\n" +
		"## Phase 1: Phase A [pending]\n\n" +
		"### Steps\n\n1. do the thing\n"
	if err := os.WriteFile(mdPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write plan.md: %v", err)
	}
	return mgr, creator
}

// draftPlanWithPhase persists a draft plan with one phase and a parseable
// markdown file — the shape that made each expansion create TWO tasks.
func draftPlanWithPhase(t *testing.T, mgr *PlanManager) *Plan {
	t.Helper()
	ctx := t.Context()
	p := NewPlan("runaway subject", "d", "", "", "")
	if err := mgr.store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	ph := NewPlanPhase(p.ID, "Phase A", 1, 1)
	if err := mgr.store.CreatePhase(ctx, ph); err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	if err := mgr.store.SetPlanState(ctx, p.ID, StateDraft); err != nil {
		t.Fatalf("SetPlanState draft: %v", err)
	}
	return p
}

// TestSynthesize_ClaimPreventsReExpansion is the core pin. Without the claim,
// the second Synthesize on the same plan creates a SECOND parent task — the
// first link in the chain that multiplied.
func TestSynthesize_ClaimPreventsReExpansion(t *testing.T) {
	mgr, creator := setupClaimManager(t)
	p := draftPlanWithPhase(t, mgr)

	if err := mgr.Synthesize(t.Context(), p.ID); err != nil {
		t.Fatalf("first Synthesize: %v", err)
	}
	if parents, _ := creator.counts(); parents != 1 {
		t.Fatalf("first Synthesize created %d parent tasks, want 1", parents)
	}

	// Retries — the auto-approver, a restart, or a duplicate bus event.
	for range 5 {
		if err := mgr.Synthesize(t.Context(), p.ID); err != nil {
			t.Fatalf("repeat Synthesize must not error: %v", err)
		}
	}
	parents, children := creator.counts()
	if parents != 1 {
		t.Errorf("parent tasks after 6 Synthesize calls = %d, want 1 "+
			"(bughunt 2026-10-08: the plan was re-expanded forever)", parents)
	}
	if children != 1 {
		t.Errorf("child tasks after 6 Synthesize calls = %d, want 1", children)
	}
}

// TestSynthesize_FailedExpansionIsRetryableNotDuplicating covers the ACTUAL
// incident shape: synthesis fails PART WAY through, leaving task_id empty. The
// retry must be possible but must not accumulate duplicates.
func TestSynthesize_FailedExpansionIsRetryableNotDuplicating(t *testing.T) {
	mgr, creator := setupClaimManager(t)
	p := draftPlanWithPhase(t, mgr)

	// Fail the FIRST pass's parent create. That is the exact incident shape: the
	// claim is taken, the parent create fails, task_id is never written, and the
	// plan must not be left claimed (which would strand it) nor left looking
	// untouched (which would re-enter the expansion loop).
	creator.failParentAt = 1
	if err := mgr.Synthesize(t.Context(), p.ID); err == nil {
		t.Fatal("expected the injected CreateTask failure to surface")
	}

	stored, err := mgr.store.GetPlan(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if stored.TaskID != "" {
		t.Fatalf("precondition: task_id should be empty after a failed pass, got %q", stored.TaskID)
	}
	if stored.State == StateSynthesizing {
		t.Fatal("plan is still claimed after a failed expansion; the lease was never released")
	}
	if stored.SynthesisAttempts != 1 {
		t.Errorf("synthesis_attempts = %d, want 1 — a failed pass must be counted", stored.SynthesisAttempts)
	}

	// Retry with the failure cleared: claimable again, and it records a task.
	creator.failParentAt = 0
	if err := mgr.Synthesize(t.Context(), p.ID); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	final, err := mgr.store.GetPlan(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("GetPlan after retry: %v", err)
	}
	if final.TaskID == "" {
		t.Error("retry did not record a task_id; the plan would loop again")
	}
	if final.SynthesisLeaseUntil != nil {
		t.Error("successful expansion left a live synthesis lease")
	}
}

// TestSynthesize_ConcurrentClaimsElectOneWinner proves the lease serialises
// expanders, which is what stops two goroutines racing one draft plan into two
// parent tasks.
func TestSynthesize_ConcurrentClaimsElectOneWinner(t *testing.T) {
	mgr, creator := setupClaimManager(t)
	p := draftPlanWithPhase(t, mgr)

	const racers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = mgr.Synthesize(t.Context(), p.ID)
		}()
	}
	close(start)
	wg.Wait()

	parents, _ := creator.counts()
	if parents != 1 {
		t.Errorf("parent tasks after %d concurrent Synthesize calls = %d, want 1 "+
			"(the lease did not serialise expanders)", racers, parents)
	}
}

// TestSynthesize_PersistentFailureIsParkedNotRetriedForever pins the retry
// BOUND. A plan that cannot be expanded must reach a terminal state after
// MaxSynthesisAttempts, or the same unbounded pump just runs one plan at a time.
func TestSynthesize_PersistentFailureIsParkedNotRetriedForever(t *testing.T) {
	mgr, creator := setupClaimManager(t)
	p := draftPlanWithPhase(t, mgr)
	creator.alwaysFailPar = true

	for range MaxSynthesisAttempts + 2 {
		_ = mgr.Synthesize(t.Context(), p.ID)
	}

	stored, err := mgr.store.GetPlan(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if stored.State != StateFailed {
		t.Errorf("state after %d failed passes = %q, want %q (a plan that cannot be "+
			"synthesised must be parked, not re-entered forever)",
			MaxSynthesisAttempts+2, stored.State, StateFailed)
	}
	if stored.SynthesisAttempts < MaxSynthesisAttempts {
		t.Errorf("synthesis_attempts = %d, want >= %d", stored.SynthesisAttempts, MaxSynthesisAttempts)
	}
}

// TestClaimPlanSynthesis_ExpiredLeaseIsReclaimable proves a dead claim cannot
// strand a plan forever: once the lease expires the plan is claimable again.
func TestClaimPlanSynthesis_ExpiredLeaseIsReclaimable(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "lease.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()
	ctx := t.Context()

	p := NewPlan("leased", "d", "", "", "")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := store.SetPlanState(ctx, p.ID, StateDraft); err != nil {
		t.Fatalf("SetPlanState: %v", err)
	}

	ok, err := store.ClaimPlanSynthesis(ctx, p.ID, time.Hour)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	// A refused claim does not re-stamp the row, so the lease on it is still the
	// one-hour lease above. A second claim must therefore be refused.
	ok, err = store.ClaimPlanSynthesis(ctx, p.ID, time.Hour)
	if err != nil {
		t.Fatalf("second claim errored: %v", err)
	}
	if ok {
		t.Error("a LIVE lease must block a second claim")
	}

	// Model "the daemon died mid-expansion": the lease ON THE ROW expires.
	// Backdate it rather than sleeping, so the test is deterministic and fast.
	expired := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, err := store.db.ExecContext(ctx,
		`UPDATE plans SET synthesis_lease_until = ? WHERE id = ?`, expired, p.ID); err != nil {
		t.Fatalf("backdate lease: %v", err)
	}
	ok, err = store.ClaimPlanSynthesis(ctx, p.ID, time.Hour)
	if err != nil {
		t.Fatalf("reclaim after expiry: %v", err)
	}
	if !ok {
		t.Error("an EXPIRED lease must be re-claimable, or a dead claim strands the plan")
	}
}

// TestClaimPlanSynthesis_RefusesAlreadyExpanded guards the other direction: a
// plan that already carries a task must never be claimable again.
func TestClaimPlanSynthesis_RefusesAlreadyExpanded(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "done.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()
	ctx := t.Context()

	p := NewPlan("done", "d", "", "", "")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	p.TaskID = "task-existing"
	p.State = StateExecuting
	if err := store.UpdatePlan(ctx, p); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	ok, err := store.ClaimPlanSynthesis(ctx, p.ID, time.Hour)
	if err != nil {
		t.Fatalf("claim on an expanded plan: %v", err)
	}
	if ok {
		t.Error("a plan that already has a task_id must not be claimable")
	}
}

// TestSynthesize_ClaimBlocksReExpansionWhenTaskIDEmpty pins the property the
// task_id guard CANNOT provide.
//
// The obvious repeat-Synthesize test is masked: after a SUCCESSFUL pass the
// plan carries task_id, so the pre-existing guard short-circuits and the claim
// looks redundant. The claim exists for the case where task_id is EMPTY because
// a previous pass died — which is precisely the incident state (797 draft plans,
// every one with an empty task_id after a mid-pass failure).
//
// This test therefore drives the plan to the incident state directly: claim it,
// clear task_id, leave it draft with the lease released. The claim must then
// refuse re-expansion while a live lease exists. Without the claim, a plan in
// that state expands again on the very next call — and that is the 20.7M-task
// pump.
func TestSynthesize_ClaimBlocksReExpansionWhenTaskIDEmpty(t *testing.T) {
	mgr, creator := setupClaimManager(t)
	p := draftPlanWithPhase(t, mgr)
	ctx := t.Context()

	// Reach the incident state: task created, task_id never persisted, plan back
	// in draft with no live lease.
	if ok, err := mgr.store.ClaimPlanSynthesis(ctx, p.ID, 0); err != nil || !ok {
		t.Fatalf("setup claim: ok=%v err=%v", ok, err)
	}
	sqlStore, ok := mgr.store.(*SQLiteStore)
	if !ok {
		t.Fatalf("expected a *SQLiteStore, got %T", mgr.store)
	}
	if _, err := sqlStore.db.ExecContext(ctx,
		`UPDATE plans SET task_id = NULL, synthesis_lease_until = NULL, state = ? WHERE id = ?`,
		string(StateDraft), p.ID); err != nil {
		t.Fatalf("drive incident state: %v", err)
	}

	// The plan now looks exactly like the 797 that drove the runaway. A claim
	// held by another expander must block re-entry.
	if ok, err := mgr.store.ClaimPlanSynthesis(ctx, p.ID, time.Hour); err != nil || !ok {
		t.Fatalf("setup: claim for the live-lease window: ok=%v err=%v", ok, err)
	}
	before, _ := creator.counts()
	if err := mgr.Synthesize(ctx, p.ID); err != nil {
		t.Fatalf("Synthesize under a live foreign claim must not error: %v", err)
	}
	after, _ := creator.counts()
	if after != before {
		t.Errorf("a plan claimed by another expander was expanded anyway: parents %d -> %d "+
			"(bughunt 2026-10-08: this is the 20.7M-task pump)", before, after)
	}
}
