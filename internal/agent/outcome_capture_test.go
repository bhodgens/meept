package agent

// Signal A + Signal B outcome-capture tests (classifier-outcome-loop leaf 03).
//
// Signal A: dispatcher recordDispatch resolves the prior pending row for the
// session after every INSERT -- same agent -> 'ok', different agent within
// reRouteWindow -> 'corrected', non-classified current dispatch -> 'ok' only.
//
// Signal B: the tactical Escalate site and the strategic quickplan fallback
// call MarkTaskFailedReplan; both are nil-guarded no-ops without a metrics
// store.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/task"
)

func newOutcomeCaptureTestDispatcher(t *testing.T) (*Dispatcher, *metrics.Store) {
	t.Helper()
	s, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	d := NewDispatcher(DispatcherConfig{})
	d.SetMetricsStore(s)
	return d, s
}

// TestRecordDispatch_SignalASequencing covers the leaf's dispatcher-level
// scenario: llm/code -> llm/review -> llm/code. Adjacent same-session
// dispatches on different agents within reRouteWindow resolve the prior row
// 'corrected'; same-agent pairs resolve it 'ok'.
//
// Note on expected outcomes: the mechanical SQL contract (leaf "Interface
// Contracts", authoritative over the prose paraphrase) pairs EACH dispatch
// with its immediately prior row. The sequence pairs are (1->2: code !=
// review) and (2->3: review != code), so turn 1 AND turn 2 both resolve
// 'corrected' (corrected_agent = the agent that displaced them). The
// same-agent 'ok' resolution is pinned separately in
// TestRecordDispatch_SignalASameAgentOk below.
func TestRecordDispatch_SignalASequencing(t *testing.T) {
	d, store := newOutcomeCaptureTestDispatcher(t)

	mk := func(agent, method string) *DispatchResult {
		return &DispatchResult{
			AgentID: agent,
			Intent:  &Intent{Type: "code", Confidence: 0.9, Method: method},
		}
	}

	// Turn 1: llm/code (pending).
	d.RecordDispatch("sess-seq", "route_to_agent", "fix the build", mk("llm/code", "llm"), false, nil)
	// Turn 2: llm/review (different agent, 1 turn apart) -> turn 1 corrected.
	d.RecordDispatch("sess-seq", "route_to_agent", "now review it", mk("llm/review", "llm"), false, nil)
	// Turn 3: llm/code (different agent from turn 2, 1 turn apart) -> turn 2 corrected.
	d.RecordDispatch("sess-seq", "route_to_agent", "code it again", mk("llm/code", "llm"), false, nil)

	type row struct {
		TurnNo         int    `db:"turn_no"`
		Outcome        string `db:"outcome"`
		CorrectedAgent string `db:"corrected_agent"`
	}
	var rows []row
	if err := store.DB().Select(&rows,
		`SELECT turn_no, outcome, corrected_agent FROM dispatch_log
		 WHERE session_id = 'sess-seq' ORDER BY turn_no`); err != nil {
		t.Fatalf("select rows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	want := []row{
		{TurnNo: 1, Outcome: "corrected", CorrectedAgent: "llm/review"},
		{TurnNo: 2, Outcome: "corrected", CorrectedAgent: "llm/code"},
		{TurnNo: 3, Outcome: "pending", CorrectedAgent: ""}, // last row stays pending
	}
	for i, w := range want {
		got := rows[i]
		if got.TurnNo != w.TurnNo || got.Outcome != w.Outcome || got.CorrectedAgent != w.CorrectedAgent {
			t.Errorf("row %d = %+v, want %+v", i+1, got, w)
		}
	}
}

// TestRecordDispatch_SignalASameAgentOk pins the 'ok' half of Signal A at the
// dispatcher level: a same-agent follow-up resolves the prior row 'ok'.
func TestRecordDispatch_SignalASameAgentOk(t *testing.T) {
	d, store := newOutcomeCaptureTestDispatcher(t)

	mk := func(agent string) *DispatchResult {
		return &DispatchResult{
			AgentID: agent,
			Intent:  &Intent{Type: "code", Confidence: 0.9, Method: "llm"},
		}
	}

	d.RecordDispatch("sess-same", "route_to_agent", "fix the build", mk("llm/code"), false, nil)
	d.RecordDispatch("sess-same", "route_to_agent", "keep going", mk("llm/code"), false, nil)

	type row struct {
		TurnNo         int    `db:"turn_no"`
		Outcome        string `db:"outcome"`
		CorrectedAgent string `db:"corrected_agent"`
	}
	var rows []row
	if err := store.DB().Select(&rows,
		`SELECT turn_no, outcome, corrected_agent FROM dispatch_log
		 WHERE session_id = 'sess-same' ORDER BY turn_no`); err != nil {
		t.Fatalf("select rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Outcome != "ok" || rows[0].CorrectedAgent != "" {
		t.Errorf("row 1 = %+v, want {ok, corrected_agent empty}", rows[0])
	}
	if rows[1].Outcome != "pending" {
		t.Errorf("row 2 outcome = %q, want pending (last row unresolved)", rows[1].Outcome)
	}
}

// TestRecordDispatch_SignalANilStoreNoOp pins the metricsStore-nil no-op
// invariant: recordDispatch without a wired store must not panic (Signal A
// rides inside the existing non-nil block).
func TestRecordDispatch_SignalANilStoreNoOp(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{})
	d.RecordDispatch("sess-nil", "route_to_agent", "hello",
		&DispatchResult{AgentID: "chat", Intent: &Intent{Method: "llm"}}, false, nil)
}

// TestTacticalSignalB_MarksFailedReplan verifies Signal B at the Escalate
// site: OnJobFailed on a step with an escalationManager wired flips the
// task's pending dispatch row to 'failed_replan'.
func TestTacticalSignalB_MarksFailedReplan(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	s, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ts.SetMetricsStore(s)
	ts.escalationManager = NewEscalationManager(EscalationManagerConfig{
		Config:    DefaultEscalationConfig(),
		TaskStore: ts.taskStore,
		Bus:       msgBus,
	})

	parent := newTestTask("task-signalb", "signal b escalation test")
	parent.TotalJobs = 1
	if err := ts.taskStore.Create(parent); err != nil {
		t.Fatalf("create task: %v", err)
	}
	step := task.NewTaskStep(parent.ID, "doomed step", 0)
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, "job-signalb"); err != nil {
		t.Fatalf("set job id: %v", err)
	}

	// The dispatch row that routed this task is still pending.
	s.RecordDispatch(metrics.DispatchEntry{
		SessionID: "sess-signalb", TaskID: parent.ID,
		AgentID: "llm/code", ClassifierMethod: "llm", TurnNo: 1, Outcome: "pending",
	})

	if err := ts.OnJobFailed(context.Background(), "job-signalb", "terminal step failure"); err != nil {
		t.Fatalf("OnJobFailed: %v", err)
	}

	var outcome string
	if err := s.DB().Get(&outcome,
		`SELECT outcome FROM dispatch_log WHERE task_id = ?`, parent.ID); err != nil {
		t.Fatalf("get outcome: %v", err)
	}
	if outcome != "failed_replan" {
		t.Errorf("outcome = %q, want failed_replan", outcome)
	}
}

// TestTacticalSignalB_NilStoreNoOp pins the nil-guard: with no metrics store
// wired, the Escalate path must not panic and behavior is unchanged.
func TestTacticalSignalB_NilStoreNoOp(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()
	ts.SetMetricsStore(nil) // typed nil must be ignored by the setter
	ts.escalationManager = NewEscalationManager(EscalationManagerConfig{
		Config:    DefaultEscalationConfig(),
		TaskStore: ts.taskStore,
		Bus:       msgBus,
	})

	parent := newTestTask("task-signalb-nil", "signal b nil-store test")
	parent.TotalJobs = 1
	if err := ts.taskStore.Create(parent); err != nil {
		t.Fatalf("create task: %v", err)
	}
	step := task.NewTaskStep(parent.ID, "doomed step", 0)
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, "job-signalb-nil"); err != nil {
		t.Fatalf("set job id: %v", err)
	}

	if err := ts.OnJobFailed(context.Background(), "job-signalb-nil", "terminal step failure"); err != nil {
		t.Fatalf("OnJobFailed: %v", err)
	}
}

// TestStrategicSignalB_MarksFailedReplan verifies Signal B at the quickplan
// fallback: when planSinglePhase fails (planner agent not registered in the
// registry), the fallback steps are used and the task's pending dispatch row
// flips to 'failed_replan'.
func TestStrategicSignalB_MarksFailedReplan(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	defer taskStore.Close()

	s, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Empty-but-non-nil registry: no "planner" spec registered, so
	// planSinglePhase fails with "agent spec not found" and Plan degrades
	// to createFallbackSteps (the Signal B site).
	registry := NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()})

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:  registry,
		TaskStore: taskStore,
		StepStore: taskStore.StepStore(),
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})
	sp.SetMetricsStore(s)
	sp.SetMetricsStore(nil) // typed nil must be ignored
	sp.SetMetricsStore(s)   // re-wire for real

	tsk := newTestTask("task-strategicb", "quickplan fallback outcome test")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	s.RecordDispatch(metrics.DispatchEntry{
		SessionID: "sess-strategicb", TaskID: tsk.ID,
		AgentID: "llm/code", ClassifierMethod: "llm", TurnNo: 1, Outcome: "pending",
	})

	err = sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-strategicb",
		Input:     "build the thing",
		Intent:    string(IntentQuickPlan), // -> quick_plan -> planSinglePhase -> fallback
		Mode:      "quick_plan",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	var outcome string
	if err := s.DB().Get(&outcome,
		`SELECT outcome FROM dispatch_log WHERE task_id = ?`, tsk.ID); err != nil {
		t.Fatalf("get outcome: %v", err)
	}
	if outcome != "failed_replan" {
		t.Errorf("outcome = %q, want failed_replan", outcome)
	}
}

// TestStrategicSignalB_NilStoreNoOp pins the nil-guard on the strategic
// planner: without a metrics store the fallback path runs unchanged.
func TestStrategicSignalB_NilStoreNoOp(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	defer taskStore.Close()

	registry := NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()})

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:  registry,
		TaskStore: taskStore,
		StepStore: taskStore.StepStore(),
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	tsk := newTestTask("task-strategicb-nil", "quickplan fallback nil-store test")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	err = sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-strategicb-nil",
		Input:     "build the thing",
		Intent:    string(IntentQuickPlan),
		Mode:      "quick_plan",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
}
