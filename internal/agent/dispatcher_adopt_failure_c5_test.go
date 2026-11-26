package agent

// C-5 (parent ruling D5) + L1 (bughunt 2026-11-24 LOW) dispatcher pins —
// plan-contract tree 20261125-plan-contract-quickplan, leaf 02 leg 1.
//
// C-5: the quick_plan branch retries a failed AdoptTask exactly ONCE; the
// second failure stamps an adopt-prefixed SynthesisError on the contract
// row (internal/plan MarkAdoptFailure + the reclaimer skip) and the turn
// PROCEEDS with the created task live — the row degrades, the work does
// not. A transient failure that heals on retry adopts normally and stamps
// nothing.
//
// L1: the /plan pure-toggle early return carries an Intent so the
// handler's provenance seam fires, while ShouldDispatchAsync stays false
// (Response != "" short-circuit) — the toggle must never dispatch an
// async turn.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
)

// claimFailingStore wraps the real plan store and forces
// ClaimPlanSynthesis — the one AdoptTask write that models a transient
// adoption outage — to fail a scripted number of times before passing
// through. Every other PlanStore method stays byte-identical via
// embedding.
type claimFailingStore struct {
	*plan.SQLiteStore
	mu       sync.Mutex
	failFor  int // number of leading ClaimPlanSynthesis calls to fail
	attempts int // ClaimPlanSynthesis calls seen
}

func (s *claimFailingStore) ClaimPlanSynthesis(ctx context.Context, id string, lease time.Duration) (bool, error) {
	s.mu.Lock()
	s.attempts++
	n := s.attempts
	s.mu.Unlock()
	if n <= s.failFor {
		return false, errors.New("synthetic adoption outage")
	}
	return s.SQLiteStore.ClaimPlanSynthesis(ctx, id, lease)
}

func (s *claimFailingStore) claimedAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

func newAdoptFailureDispatcher(t *testing.T, failFor int) (*Dispatcher, *claimFailingStore, func() int) {
	t.Helper()
	logger := digestTestLogger()
	cs := newCaptureServer(t, `{}`)
	d := newDigestCaptureDispatcher(t, cs)

	raw, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	wrapped := &claimFailingStore{SQLiteStore: raw, failFor: failFor}

	cfg := config.PlansConfig{Mode: "always"}
	d.SetPlanManager(plan.NewPlanManager(wrapped, nil, cfg, nil, logger))

	countTasks := func() int {
		all, err := d.taskStore.List(nil, 1000)
		if err != nil {
			t.Fatalf("task store list: %v", err)
		}
		return len(all)
	}
	return d, wrapped, countTasks
}

func quickPlanIntent() *Intent {
	return &Intent{
		Type:             string(IntentQuickPlan),
		Confidence:       0.8,
		AgentType:        "orchestrator",
		Summary:          "ship the migration",
		RequiresPlanning: true,
	}
}

// TestRouteToPlan_QuickPlanAdoptTransientHealsOnRetry pins the D5 retry
// arm: the first AdoptTask call fails, the dispatcher retries, the second
// succeeds — the row reaches executing, NO SynthesisError is stamped, and
// exactly two adoption claims were spent.
func TestRouteToPlan_QuickPlanAdoptTransientHealsOnRetry(t *testing.T) {
	d, store, countTasks := newAdoptFailureDispatcher(t, 1)
	ctx := context.Background()

	res, err := d.routeToPlan(ctx, "ship the migration end to end", quickPlanIntent(), "sess-c5-heal")
	if err != nil {
		t.Fatalf("routeToPlan: %v", err)
	}
	if res.Task == nil {
		t.Fatal("quick_plan produced no task")
	}
	if n := countTasks(); n != 1 {
		t.Errorf("task rows = %d, want 1", n)
	}
	if got := store.claimedAttempts(); got != 2 {
		t.Errorf("adoption claims = %d, want exactly 2 (one retry, D5)", got)
	}
	got, err := store.SQLiteStore.GetPlan(ctx, res.Plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != plan.StateExecuting {
		t.Errorf("plan state = %q, want %q after healed retry", got.State, plan.StateExecuting)
	}
	if got.TaskID != res.Task.ID {
		t.Errorf("plan.TaskID = %q, want adopted %q", got.TaskID, res.Task.ID)
	}
	if got.SynthesisError != "" {
		t.Errorf("synthesis_error = %q, want empty (retry succeeded)", got.SynthesisError)
	}
}

// TestRouteToPlan_QuickPlanAdoptDoubleFailureStampsAndProceeds pins the D5
// degraded arm: both adopt attempts fail. The dispatcher must (a) retry
// exactly once, (b) stamp the adopt-prefixed SynthesisError on the row,
// (c) keep the row APPROVED (the reclaimer's approved-scan skip is pinned
// plan-side in plan_adopt_failure_c5_test.go), and (d) PROCEED: the
// DispatchResult carries the live task and an empty Response (async ack),
// the turn is never failed by a degraded contract row.
func TestRouteToPlan_QuickPlanAdoptDoubleFailureStampsAndProceeds(t *testing.T) {
	d, store, countTasks := newAdoptFailureDispatcher(t, 2)
	ctx := context.Background()

	res, err := d.routeToPlan(ctx, "ship the migration end to end", quickPlanIntent(), "sess-c5-stamp")
	if err != nil {
		t.Fatalf("routeToPlan must not fail on adopt outage: %v", err)
	}
	if res.Task == nil {
		t.Fatal("degraded adopt dropped the live task; the turn proceeds (D5)")
	}
	if n := countTasks(); n != 1 {
		t.Errorf("task rows = %d, want 1", n)
	}
	if got := store.claimedAttempts(); got != 2 {
		t.Errorf("adoption claims = %d, want exactly 2 (one retry, no stamping-path re-claim)", got)
	}
	got, err := store.SQLiteStore.GetPlan(ctx, res.Plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != plan.StateApproved {
		t.Errorf("plan state = %q, want %q (row stays approved, D5)", got.State, plan.StateApproved)
	}
	if got.TaskID != "" {
		t.Errorf("plan.TaskID = %q, want empty (binding failed)", got.TaskID)
	}
	if !strings.HasPrefix(got.SynthesisError, plan.AdoptFailurePrefix) {
		t.Errorf("synthesis_error = %q, want %q prefix", got.SynthesisError, plan.AdoptFailurePrefix)
	}
	// The row stays the ONLY plan; no second contract row was minted.
	all, err := store.SQLiteStore.ListPlans(ctx, "", 100)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("plan rows = %d, want 1", len(all))
	}
}

// --- L1: /plan pure toggle provenance threading -------------------------

// stubPlanModeController consumes /plan as a toggle with a fixed reply.
type stubPlanModeController struct {
	steer, reply string
	consumed     bool
}

func (s stubPlanModeController) HandlePlanCommand(sessionID, input string) (string, string, bool) {
	return s.steer, s.reply, s.consumed
}

func TestPlanToggleCarriesIntentNoAsyncDispatch(t *testing.T) {
	cs := newCaptureServer(t, `{}`)
	d := newDigestCaptureDispatcher(t, cs)
	d.SetPlanModeController(stubPlanModeController{reply: "plan mode on", consumed: true})

	res, err := d.ClassifyAndRoute(context.Background(), "/plan", "sess-l1-toggle", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(/plan): %v", err)
	}
	// Reply preserved byte-identically.
	if res.Response != "plan mode on" {
		t.Errorf("Response = %q, want %q", res.Response, "plan mode on")
	}
	// L1: the intent now rides along so handler.go:1721 attaches
	// provenance Meta (the legacy force-create path's intent shape).
	if res.Intent == nil {
		t.Fatal("toggle result lost the Intent; provenance seam stays dead")
	}
	if res.Intent.Type != string(IntentPlan) {
		t.Errorf("intent type = %q, want %q", res.Intent.Type, string(IntentPlan))
	}
	if res.Intent.Method == "" {
		t.Error("intent Method empty; provenance would claim an unknown classifier lane")
	}
	if res.AgentID != config.AgentIDPlanner {
		t.Errorf("AgentID = %q, want %q (legacy toggle path's agent)", res.AgentID, config.AgentIDPlanner)
	}
	// THE SAFETY PIN: the async gate (handler.go:1340) must stay shut —
	// ShouldDispatchAsync short-circuits on Response != "" and the
	// direct_response case (handler.go:1287) matches FIRST.
	if d.ShouldDispatchAsync(res) {
		t.Error("toggle would dispatch an async turn; the reply must stay direct")
	}
	// The handler's provenance helper (READ-ONLY call, no handler edit)
	// now yields metadata for this turn instead of being skipped.
	meta := provenanceMeta(res.Intent)
	if meta["classification_method"] != res.Intent.Method {
		t.Errorf("provenance method = %q, want %q", meta["classification_method"], res.Intent.Method)
	}
	// No degraded note is appended: the method is not a degraded lane.
	if strings.Contains(res.Response, "classifier") {
		t.Errorf("degraded note leaked into toggle reply: %q", res.Response)
	}
	// No plan row and no task were created by the toggle.
	if res.Plan != nil {
		t.Error("toggle created a plan row; pure-toggle must not force-create")
	}
	if res.Task != nil {
		t.Error("toggle created a task; pure-toggle must not dispatch work")
	}
}
