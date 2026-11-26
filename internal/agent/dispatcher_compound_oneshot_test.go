package agent

// Leaf 02 compound-oneshot pins (tree 20261125-plan-contract-quickplan,
// master C-4 + C-2 compound half, owner ruling D4; closes bughunt H6).
//
// D4: a sequential compound ("plan X then implement it") IS quickplan —
// ONE task, oneshot, through the plan-contract lane. Multi-agent parallel
// compounds keep the legacy pair-parent lane byte-identically; the
// spec_pair machinery in strategic.go stays reachable for intent-string
// legacy/test callers, it is simply no longer ENTERED from the compound
// dispatch route.
//
// The oracle pattern is dispatcher_quickplan_plan_contract_test.go's:
// real plan store + task-count over the dispatcher store — any pair-lane
// leak shows up as a compound-typed intent, extra tasks, or NO plan row.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
)

// "plan X and then implement it": plan+code pair, plan lane forces
// CompoundType=sequential via laneForcesSequentialCompound. Over the
// 80-char compoundKeywordThreshold so the real classifyMultiIntent fires.
const sequentialCompoundInput = "plan the authentication refactor for the api gateway and then implement it end to end please"

// "write X and also write Y": two writer lanes, none in the sequential set
// => CompoundType=parallel (the multi-work shape D4 does NOT convert).
const parallelCompoundInput = "write a haiku about the sea and also write a limerick about the mountains for me today"

func newCompoundLaneDispatcher(t *testing.T, verdict string) (*Dispatcher, *plan.SQLiteStore, func() int) {
	t.Helper()
	logger := digestTestLogger()
	cs := newCaptureServer(t, verdict)
	d := newDigestCaptureDispatcher(t, cs)
	// Keyword classifier off: the LLM verdict alone decides the shape, so
	// the pin names the producer (same discipline as
	// newCompoundTestDispatcher).
	d.keywordClassifier = nil

	store, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	d.SetPlanManager(plan.NewPlanManager(store, nil, config.PlansConfig{Mode: "always"}, nil, logger))

	countTasks := func() int {
		all, err := d.taskStore.List(nil, 1000)
		if err != nil {
			t.Fatalf("task store list: %v", err)
		}
		return len(all)
	}
	return d, store, countTasks
}

// TestClassifyAndRoute_SequentialCompoundRidesQuickPlanLane is the D4 core
// pin, end to end through the REAL classifyMultiIntent + compound seam:
// the verdict converts to IntentQuickPlan, ONE task exists, the plan row
// reaches executing bound to it (the C-2 adopt shape), the async gate is
// open with an empty Response (brief-plan ack), and SuggestedMode is
// quick_plan — NOT spec_pair.
func TestClassifyAndRoute_SequentialCompoundRidesQuickPlanLane(t *testing.T) {
	d, pstore, countTasks := newCompoundLaneDispatcher(t,
		`[{"intent":"plan","confidence":0.85},{"intent":"code","confidence":0.8}]`)

	// Precondition: the real detector says sequential compound.
	multi := d.classifyMultiIntent(context.Background(), sequentialCompoundInput, &MemoryContext{})
	if !multi.IsCompound || multi.CompoundType != "sequential" {
		t.Fatalf("harness: compound=%v type=%q, want sequential compound", multi.IsCompound, multi.CompoundType)
	}

	res, err := d.ClassifyAndRoute(context.Background(), sequentialCompoundInput, "sess-d4-seq", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil {
		t.Fatal("nil intent on compound route")
	}
	if res.Intent.Type != string(IntentQuickPlan) {
		t.Errorf("intent type = %q, want %q (D4: sequential compound IS quickplan)", res.Intent.Type, string(IntentQuickPlan))
	}
	if res.Intent.Type == string(IntentCompound) {
		t.Error("legacy pair-parent lane still owns the sequential compound")
	}
	if res.Task == nil {
		t.Fatal("sequential compound produced no task; async gate dead")
	}
	if n := countTasks(); n != 1 {
		t.Errorf("task rows = %d, want exactly 1 (ONE task, oneshot)", n)
	}
	got, err := pstore.GetPlan(context.Background(), res.Plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != plan.StateExecuting {
		t.Errorf("plan state = %q, want %q (adopted contract)", got.State, plan.StateExecuting)
	}
	if got.TaskID != res.Task.ID {
		t.Errorf("plan.TaskID = %q, want %q", got.TaskID, res.Task.ID)
	}
	// The full-input contract survives the conversion: the task
	// description is the WHOLE user input, not a truncated summary.
	if res.Task.Description != sequentialCompoundInput {
		t.Errorf("task description = %q, want full input", res.Task.Description)
	}
	if res.SuggestedMode != "quick_plan" {
		t.Errorf("SuggestedMode = %q, want quick_plan (spec_pair lane must not ride this route)", res.SuggestedMode)
	}
	if res.Response != "" {
		t.Errorf("Response = %q, want empty (async ack, not a text reply)", res.Response)
	}
	if !d.ShouldDispatchAsync(res) {
		t.Error("async gate closed on converted compound; the task would orphan")
	}
	// No spec_pair metadata on the row's task — it is a quickplan task.
	if res.Task.Metadata != nil {
		var meta map[string]any
		if json.Unmarshal(res.Task.Metadata, &meta) == nil {
			if _, ok := meta["compound_type"]; ok {
				t.Errorf("converted task still carries compound_type metadata: %v", meta)
			}
		}
	}
}

// TestClassifyAndRoute_ParallelCompoundKeepsPairParentLane pins the
// unchanged half of C-4: a PARALLEL compound (two independent work lanes)
// still takes the legacy compound parent-task route — compound intent,
// per-intent steps, the task metadata carrying compound_type=parallel —
// and NO plan row is minted by the conversion.
func TestClassifyAndRoute_ParallelCompoundKeepsPairParentLane(t *testing.T) {
	d, pstore, countTasks := newCompoundLaneDispatcher(t,
		`[{"intent":"code","confidence":0.9},{"intent":"debug","confidence":0.85}]`)

	multi := d.classifyMultiIntent(context.Background(), parallelCompoundInput, &MemoryContext{})
	if !multi.IsCompound || multi.CompoundType != "parallel" {
		t.Fatalf("harness: compound=%v type=%q, want parallel compound", multi.IsCompound, multi.CompoundType)
	}

	res, err := d.ClassifyAndRoute(context.Background(), parallelCompoundInput, "sess-d4-par", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res.Intent == nil || res.Intent.Type != string(IntentCompound) {
		t.Fatalf("intent = %+v, want compound (parallel lane unchanged)", res.Intent)
	}
	if res.Task == nil {
		t.Fatal("parallel compound lost its parent task")
	}
	if n := countTasks(); n != 1 {
		t.Errorf("task rows = %d, want 1 parent", n)
	}
	if len(res.Steps) != 2 {
		t.Errorf("steps = %d, want 2 (per-intent step summaries preserved)", len(res.Steps))
	}
	if res.Task.Metadata == nil {
		t.Fatal("parent task lost compound metadata")
	}
	var meta map[string]any
	if err := json.Unmarshal(res.Task.Metadata, &meta); err != nil {
		t.Fatalf("metadata decode: %v", err)
	}
	if meta["compound_type"] != "parallel" {
		t.Errorf("compound_type = %v, want parallel", meta["compound_type"])
	}
	if res.SuggestedMode != "" {
		t.Errorf("SuggestedMode = %q, want empty (legacy lane shape)", res.SuggestedMode)
	}
	// No plan row on the parallel lane.
	all, err := pstore.ListPlans(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("parallel compound minted %d plan rows, want 0", len(all))
	}
}

// TestRouteSequentialCompoundAsQuickPlan_ApprovalRequestedParks pins the
// D3/D4 coexistence on the converted route: the ADJUDICATED
// approval_requested attribute (not phrasing regex) parks the contract at
// pending_approval with NO task, exactly like the plain quick_plan lane.
func TestRouteSequentialCompoundAsQuickPlan_ApprovalRequestedParks(t *testing.T) {
	d, pstore, countTasks := newCompoundLaneDispatcher(t,
		`[{"intent":"plan","confidence":0.85},{"intent":"code","confidence":0.8}]`)

	multi := d.classifyMultiIntent(context.Background(), sequentialCompoundInput, &MemoryContext{})
	if !multi.IsCompound {
		t.Fatal("harness: expected compound")
	}
	analysis := &TrueIntentAnalysis{ApprovalRequested: true}

	res, err := d.routeSequentialCompoundAsQuickPlan(context.Background(), multi,
		sequentialCompoundInput, "sess-d4-park", analysis, nil)
	if err != nil {
		t.Fatalf("routeSequentialCompoundAsQuickPlan: %v", err)
	}
	if res.Task != nil {
		t.Error("approval_requested compound created a task; must park (D3 exception)")
	}
	if n := countTasks(); n != 0 {
		t.Errorf("task rows = %d, want 0 (parked contract)", n)
	}
	got, err := pstore.GetPlan(context.Background(), res.Plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != plan.StatePendingApproval {
		t.Errorf("plan state = %q, want %q", got.State, plan.StatePendingApproval)
	}
	if res.Response == "" {
		t.Error("park posture lost the 'plan created' text reply")
	}
	if !res.Intent.ApprovalRequested {
		t.Error("ApprovalRequested not carried from the analysis onto the converted intent")
	}
}

// TestStrategicSpecPairLaneStillReachable pins the C-4 retention clause:
// the spec_pair lane in strategic.go is NOT deleted — legacy intent-string
// and IsCompound plan callers (orchestrator-internal step executors, TUI/
// RPC plan requests, tests) still resolve mode spec_pair. Only the
// COMPOUND DISPATCH ROUTE stopped entering it (pinned above).
func TestStrategicSpecPairLaneStillReachable(t *testing.T) {
	sp := &StrategicPlanner{logger: testLogger(), simpleInputMaxChars: 100}
	if got := sp.inferLegacyMode(PlanRequest{Intent: string(IntentCompound), IsCompound: true}); got != "spec_pair" {
		t.Errorf("inferLegacyMode(compound) = %q, want spec_pair (lane retained for non-dispatch callers)", got)
	}
	// The IsCompound flag alone (handler-side shape) also lands on the lane.
	if got := sp.inferLegacyMode(PlanRequest{Intent: string(IntentPlan), IsCompound: true}); got != "spec_pair" {
		t.Errorf("inferLegacyMode(IsCompound plan) = %q, want spec_pair", got)
	}
}
