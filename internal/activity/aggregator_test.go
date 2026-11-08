package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/task"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// fakes for the narrow seams (unit-level; real-store adapters are covered by
// the adapters_test.go cross-store tests against sandbox sqlite DBs)
// ---------------------------------------------------------------------------

type fakeTasks struct{ rows []TaskRow }

func (f fakeTasks) ListTasks() ([]TaskRow, error) { return f.rows, nil }

type fakeSteps struct{ rows []StepRow }

func (f fakeSteps) ListSteps() ([]StepRow, error) { return f.rows, nil }

type fakeSessions struct {
	rows []SessionRow
	msgs map[string][]SessionMessage
}

func (f fakeSessions) List() ([]SessionRow, error) { return f.rows, nil }

func (f fakeSessions) Messages(sessionID string, offset, limit int) ([]SessionMessage, error) {
	return f.msgs[sessionID], nil
}

type fakeChain struct{ rows []ChainRecord }

func (f fakeChain) ListRecords(ctx context.Context, limit int) ([]ChainRecord, error) {
	return f.rows, nil
}

type fakeFindings struct{ rows []FindingRow }

func (f fakeFindings) ListFindings(ctx context.Context, employeeID, severity string, since time.Time, limit int) ([]FindingRow, error) {
	return f.rows, nil
}

func base() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }

// T1: types + constructor with nil-handle degradation.
func TestNewAggregator_AllNilHandles_ReturnsEmptyResult(t *testing.T) {
	a := newAggregator(nil, nil, nil, nil, nil, Options{})
	recs, total, err := a.Query(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Query with all-nil handles: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(recs) != 0 {
		t.Errorf("records = %d, want 0", len(recs))
	}
}

// T1: C1 JSON tags byte-identical to the master contract.
func TestActivityRecord_JSONShape(t *testing.T) {
	at := base()
	rec := ActivityRecord{
		Kind:    KindTaskStep,
		At:      at,
		AgentID: "agent-1",
		TaskID:  "task-1",
		StepID:  "step-1",
		State:   "completed",
		Summary: "did the thing",
		Detail:  map[string]string{"task_name": "build"},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"kind":"task_step","at":"` + at.UTC().Format(time.RFC3339Nano) + `","agent_id":"agent-1","task_id":"task-1","step_id":"step-1","state":"completed","summary":"did the thing","detail":{"task_name":"build"}}`
	if string(b) != want {
		t.Errorf("json shape mismatch:\n got %s\nwant %s", b, want)
	}
}

// T2/T3: task_step + session_message + audit_record + finding mappers merge
// into a chrono-ordered stream with correct Kind/At/IDs/Summary.
func TestAggregator_MergesAllKinds_Chrono(t *testing.T) {
	t0 := base()
	steps := fakeSteps{rows: []StepRow{{
		ID: "step-2", TaskID: "task-1", Description: "second step",
		AgentID: "coder", State: "running", UpdatedAt: t0.Add(2 * time.Hour),
	}, {
		ID: "step-1", TaskID: "task-1", Description: "first step",
		AgentID: "coder", State: "completed", Result: `{"claims":["shipped it"],"evidence":[]}`,
		UpdatedAt: t0.Add(1 * time.Hour),
	}}}
	tasks := fakeTasks{rows: []TaskRow{{ID: "task-1", Name: "build feature", AssignedAgent: "coder"}}}
	sessions := fakeSessions{
		rows: []SessionRow{{ID: "sess-1", EmployeeID: "emp-1"}},
		msgs: map[string][]SessionMessage{
			"sess-1": {
				{SessionID: "sess-1", Role: "user", Content: "hello there", Timestamp: t0},
				{SessionID: "sess-1", Role: "assistant", AgentID: "emp-1", Content: `{"claims":["done"],"evidence":[{"description":"file written"}]}`, Timestamp: t0.Add(3 * time.Hour)},
			},
		},
	}
	chain := fakeChain{rows: []ChainRecord{{
		Seq: 1, Type: "plan.approve", EmployeeID: "emp-1",
		Payload: map[string]string{"action": "plan.approve", "plan_id": "plan-1"},
		At:      t0.Add(30 * time.Minute),
	}}}
	findings := fakeFindings{rows: []FindingRow{{
		ID: "f-1", EmployeeID: "emp-1", PlanID: "plan-1", Severity: "warning",
		ViolatedRule: "no destructive commands", DetectedAt: t0.Add(4 * time.Hour),
	}}}

	a := newAggregator(tasks, steps, sessions, chain, findings, Options{})
	recs, total, err := a.Query(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// 2 steps + 2 messages + 1 chain record + 1 finding = 6 records.
	if total != 6 || len(recs) != 6 {
		t.Fatalf("total=%d len=%d, want 6/6", total, len(recs))
	}
	// Chronological order: user msg(0h), audit(0.5h), step-1(1h),
	// step-2(2h), assistant msg(3h), finding(4h).
	wantKinds := []string{KindSessionMessage, KindAuditRecord, KindTaskStep, KindTaskStep, KindSessionMessage, KindAuditRecord}
	for i, wk := range wantKinds {
		if recs[i].Kind != wk {
			t.Errorf("rec[%d].Kind = %q, want %q", i, recs[i].Kind, wk)
		}
	}
	// Envelope stripping: step-1's claims become prose.
	if recs[2].Summary != "shipped it" {
		t.Errorf("step summary = %q, want %q", recs[2].Summary, "shipped it")
	}
	// Assistant envelope message recovered to prose.
	if recs[4].Summary != "done file written" {
		t.Errorf("assistant summary = %q, want %q", recs[4].Summary, "done file written")
	}
	// Finding carries severity + own plan_id.
	if recs[5].Severity != "warning" || recs[5].PlanID != "plan-1" {
		t.Errorf("finding severity/plan = %q/%q", recs[5].Severity, recs[5].PlanID)
	}
	// Agent fallback: step record inherits task's assigned agent.
	if recs[3].AgentID != "coder" {
		t.Errorf("step agent = %q, want coder", recs[3].AgentID)
	}
}

// T2: summaries are bounded to 200 runes.
func TestAggregator_SummaryBounded(t *testing.T) {
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'x'
	}
	steps := fakeSteps{rows: []StepRow{{
		ID: "s1", TaskID: "t1", Description: string(long),
		State: "pending", UpdatedAt: base(),
	}}}
	a := newAggregator(fakeTasks{}, steps, nil, nil, nil, Options{})
	recs, _, err := a.Query(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := len([]rune(recs[0].Summary)); got > 200 {
		t.Errorf("summary len = %d runes, want <= 200", got)
	}
}

// T3: plan sort resolves plan -> tasks via the injected resolver; findings
// carry their own plan_id.
func TestAggregator_PlanSort_GroupsByResolvedPlan(t *testing.T) {
	t0 := base()
	steps := fakeSteps{rows: []StepRow{{
		ID: "s1", TaskID: "task-a", Description: "a-step", State: "pending",
		UpdatedAt: t0.Add(1 * time.Hour),
	}, {
		ID: "s2", TaskID: "task-b", Description: "b-step", State: "pending",
		UpdatedAt: t0.Add(2 * time.Hour),
	}}}
	findings := fakeFindings{rows: []FindingRow{{
		ID: "f-1", EmployeeID: "emp-1", PlanID: "plan-9", Severity: "info",
		ViolatedRule: "late review", DetectedAt: t0.Add(30 * time.Minute),
	}}}
	resolver := func(ctx context.Context, planID string) ([]string, error) {
		if planID == "plan-7" {
			return []string{"task-a", "task-b"}, nil
		}
		return nil, nil
	}
	a := newAggregator(fakeTasks{}, steps, nil, nil, findings, Options{PlanResolver: resolver})
	recs, total, err := a.Query(context.Background(), Query{Sort: SortPlan, PlanID: "plan-7"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// total counts records matching the FILTERS: the plan-9 finding is
	// filtered out, so 2 stamped task_step records remain.
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(recs) != 2 {
		t.Fatalf("len = %d, want 2 (finding plan-9 filtered out by PlanID filter)", len(recs))
	}
	for _, r := range recs {
		if r.PlanID != "plan-7" {
			t.Errorf("record PlanID = %q, want plan-7", r.PlanID)
		}
	}
	if recs[0].StepID != "s1" || recs[1].StepID != "s2" {
		t.Errorf("within-group order wrong: %s then %s", recs[0].StepID, recs[1].StepID)
	}

	// Without the PlanID filter the plan sort groups plan-9's finding LAST
	// (empty plan id would be last, but it carries plan-9; "plan-7" <
	// "plan-9" so plan-7 group first, then plan-9).
	all, total, err := a.Query(context.Background(), Query{Sort: SortPlan})
	if err != nil {
		t.Fatalf("Query all: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("all total/len = %d/%d, want 3/3", total, len(all))
	}
	if all[2].PlanID != "plan-9" {
		t.Errorf("last group PlanID = %q, want plan-9", all[2].PlanID)
	}
}

// T3: plan sort puts records with no plan last.
func TestAggregator_PlanSort_EmptyPlanLast(t *testing.T) {
	t0 := base()
	steps := fakeSteps{rows: []StepRow{{
		ID: "s1", TaskID: "task-a", Description: "a", State: "pending",
		UpdatedAt: t0,
	}, {
		ID: "s2", TaskID: "task-orphan", Description: "orphan", State: "pending",
		UpdatedAt: t0,
	}}}
	resolver := func(ctx context.Context, planID string) ([]string, error) {
		if planID == "plan-1" {
			return []string{"task-a"}, nil
		}
		return nil, nil
	}
	a := newAggregator(fakeTasks{}, steps, nil, nil, nil, Options{PlanResolver: resolver})
	recs, _, err := a.Query(context.Background(), Query{Sort: SortPlan, PlanID: "plan-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 1 || recs[0].StepID != "s1" {
		t.Fatalf("want only s1, got %+v", recs)
	}
}

// T3: task sort groups by TaskID, empty TaskID last, At ascending within
// group.
func TestAggregator_TaskSort(t *testing.T) {
	t0 := base()
	steps := fakeSteps{rows: []StepRow{{
		ID: "s-b2", TaskID: "task-b", Description: "b2", State: "pending",
		UpdatedAt: t0.Add(2 * time.Hour),
	}, {
		ID: "s-none", Description: "no task", State: "pending",
		UpdatedAt: t0,
	}, {
		ID: "s-b1", TaskID: "task-b", Description: "b1", State: "pending",
		UpdatedAt: t0.Add(1 * time.Hour),
	}, {
		ID: "s-a1", TaskID: "task-a", Description: "a1", State: "pending",
		UpdatedAt: t0.Add(3 * time.Hour),
	}}}
	a := newAggregator(fakeTasks{}, steps, nil, nil, nil, Options{})
	recs, total, err := a.Query(context.Background(), Query{Sort: SortTask})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 4 || len(recs) != 4 {
		t.Fatalf("total/len = %d/%d, want 4/4", total, len(recs))
	}
	want := []string{"s-a1", "s-b1", "s-b2", "s-none"}
	for i, w := range want {
		if recs[i].StepID != w {
			t.Errorf("rec[%d] = %s, want %s", i, recs[i].StepID, w)
		}
	}
}

// T4: filters and pagination with total-before-pagination.
func TestAggregator_FiltersAndPagination(t *testing.T) {
	t0 := base()
	var steps []StepRow
	for i := 0; i < 25; i++ {
		steps = append(steps, StepRow{
			ID:          "s" + itoa(uint64(i)),
			TaskID:      "task-1",
			Description: "step",
			AgentID:     "coder",
			State:       "pending",
			UpdatedAt:   t0.Add(time.Duration(i) * time.Minute),
		})
	}
	a := newAggregator(fakeTasks{}, fakeSteps{rows: steps}, nil, nil, nil, Options{})

	// Pagination: limit 10, offset 10 -> 10 records, total 25.
	recs, total, err := a.Query(context.Background(), Query{Limit: 10, Offset: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(recs) != 10 || total != 25 {
		t.Errorf("len/total = %d/%d, want 10/25", len(recs), total)
	}
	if recs[0].StepID != "s10" || recs[9].StepID != "s19" {
		t.Errorf("page wrong: first=%s last=%s, want s10/s19", recs[0].StepID, recs[9].StepID)
	}

	// Offset beyond the end: empty page, total intact.
	recs, total, err = a.Query(context.Background(), Query{Limit: 10, Offset: 100})
	if err != nil || total != 25 || len(recs) != 0 {
		t.Errorf("past-end page: len/total/err = %d/%d/%v, want 0/25/nil", len(recs), total, err)
	}

	// Agent filter: nothing matches -> empty.
	recs, total, err = a.Query(context.Background(), Query{AgentID: "nope"})
	if err != nil || total != 0 || len(recs) != 0 {
		t.Errorf("agent filter: len/total/err = %d/%d/%v, want 0/0/nil", len(recs), total, err)
	}

	// Since/Until window: only the first 10 minutes.
	recs, total, err = a.Query(context.Background(), Query{
		Since: t0, Until: t0.Add(9 * time.Minute),
	})
	if err != nil || total != 10 {
		t.Errorf("window: len/total/err = %d/%d/%v, want 10/10/nil", len(recs), total, err)
	}

	// Kinds filter: no matching kind.
	recs, total, err = a.Query(context.Background(), Query{Kinds: []string{KindSessionMessage}})
	if err != nil || total != 0 {
		t.Errorf("kinds filter: len/total = %d/%d, want 0/0", len(recs), total)
	}
}

// TestAggregator_SandboxStores_TaskStepsFromRealDB seeds a real task
// registry and asserts steps flow through the real adapters in chrono
// order with envelope-stripped summaries.
func TestAggregator_SandboxStores_TaskStepsFromRealDB(t *testing.T) {
	ctx := context.Background()
	reg, err := task.NewRegistry(t.TempDir()+"/tasks.db", bus.New(nil, nil), slog.Default())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })

	tsk, err := reg.Create(ctx, "build widget", "a task")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	stepStore := reg.StepStore()
	s1 := task.NewTaskStep(tsk.ID, "first step", 1)
	if err := stepStore.Create(s1); err != nil {
		t.Fatalf("create step 1: %v", err)
	}
	s2 := task.NewTaskStep(tsk.ID, "second step", 2)
	s2.Result = `{"claims":["widget built"],"evidence":[]}`
	if err := stepStore.Create(s2); err != nil {
		t.Fatalf("create step 2: %v", err)
	}
	// Force distinct updated_at ordering.
	later := time.Now().UTC().Add(2 * time.Second)
	s2.UpdatedAt = later
	if err := stepStore.Update(s2); err != nil {
		t.Fatalf("update step 2: %v", err)
	}

	a := newAggregator(NewTaskRegistryAdapter(reg), NewStepStoreAdapter(stepStore), nil, nil, nil, Options{})
	recs, total, err := a.Query(ctx, Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 2 || len(recs) != 2 {
		t.Fatalf("total/len = %d/%d, want 2/2", total, len(recs))
	}
	if recs[0].StepID != s1.ID || recs[1].StepID != s2.ID {
		t.Errorf("order wrong: %s, %s", recs[0].StepID, recs[1].StepID)
	}
	if recs[0].TaskID != tsk.ID || recs[0].Summary != "first step" {
		t.Errorf("rec0 task/summary = %s/%q", recs[0].TaskID, recs[0].Summary)
	}
	if recs[1].Summary != "widget built" {
		t.Errorf("rec1 summary = %q, want envelope-stripped prose", recs[1].Summary)
	}
	if recs[0].Detail["task_name"] != "build widget" {
		t.Errorf("task_name detail = %q", recs[0].Detail["task_name"])
	}
}

// TestAggregator_SandboxStores_AuditChainAndFindings seeds a real
// auditlog store and employee audit store and asserts both map with
// severity/plan carried.
func TestAggregator_SandboxStores_AuditChainAndFindings(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	chainStore, err := auditlog.OpenStore(dir+"/audit.db", slog.Default())
	if err != nil {
		t.Fatalf("auditlog: %v", err)
	}
	t.Cleanup(func() { chainStore.Close() })
	at := base()
	if _, err := chainStore.Append(ctx, auditlog.Record{
		Type: "plan.approve", EmployeeID: "emp-1", At: at,
		Payload: map[string]any{"action": "plan.approve", "plan_id": "plan-1", "summary": "plan approved"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	auditStore, err := employee.NewAuditStore(dir + "/findings.db")
	if err != nil {
		t.Fatalf("audit store: %v", err)
	}
	t.Cleanup(func() { auditStore.Close() })
	if err := auditStore.Create(ctx, employee.AuditFinding{
		EmployeeID:   "emp-1",
		PlanID:       "plan-1",
		Severity:     employee.SeverityWarning,
		Checkpoint:   employee.CheckpointPostTurn,
		ViolatedRule: "skipped tests",
		DetectedAt:   at.Add(time.Hour),
	}); err != nil {
		t.Fatalf("finding create: %v", err)
	}

	a := newAggregator(nil, nil, nil, NewChainStoreAdapter(chainStore), NewAuditStoreAdapter(auditStore), Options{})
	recs, total, err := a.Query(ctx, Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 2 || len(recs) != 2 {
		t.Fatalf("total/len = %d/%d, want 2/2", total, len(recs))
	}
	if recs[0].Kind != KindAuditRecord || recs[0].Summary != "plan approved" {
		t.Errorf("rec0 = %s/%q", recs[0].Kind, recs[0].Summary)
	}
	if recs[1].Severity != "warning" || recs[1].PlanID != "plan-1" || recs[1].AgentID != "emp-1" {
		t.Errorf("finding rec = sev:%s plan:%s agent:%s", recs[1].Severity, recs[1].PlanID, recs[1].AgentID)
	}
	if recs[1].State != "open" {
		t.Errorf("finding state = %q, want open", recs[1].State)
	}
}

// TestAggregator_SandboxStores_SessionMessages exercises the session
// adapter against a real SQLiteStore.
func TestAggregator_SandboxStores_SessionMessages(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sessStore, err := newSessionSQLiteStore(dir + "/sessions.db")
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	t.Cleanup(func() { sessStore.Close() })

	sess, err := sessStore.Create("chat")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t0 := base()
	if err := sessStore.SaveMessages(sess.ID, []SessionMsg{
		{Role: "user", Content: "what changed?", Timestamp: t0},
		{Role: "assistant", AgentID: "emp-2", Content: `{"claims":["all green"],"evidence":[]}`, Timestamp: t0.Add(time.Minute)},
	}); err != nil {
		t.Fatalf("save messages: %v", err)
	}

	a := newAggregator(nil, nil, NewSessionStoreAdapter(sessStore), nil, nil, Options{})
	recs, total, err := a.Query(ctx, Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 2 || len(recs) != 2 {
		t.Fatalf("total/len = %d/%d, want 2/2", total, len(recs))
	}
	if recs[0].SessionID != sess.ID || recs[0].Summary != "what changed?" {
		t.Errorf("rec0 = sess:%s summary:%q", recs[0].SessionID, recs[0].Summary)
	}
	if recs[1].AgentID != "emp-2" || recs[1].Summary != "all green" {
		t.Errorf("rec1 = agent:%s summary:%q", recs[1].AgentID, recs[1].Summary)
	}
}

// keep the sql import referenced for adapters' driver registration symmetry
var _ = sql.ErrNoRows

// T1 (contract constructor): NewAggregator with the concrete handle types,
// ALL nil, degrades to an empty result and no error.
func TestNewAggregator_ContractConstructor_AllNilHandles(t *testing.T) {
	a := NewAggregator(nil, nil, nil, nil, nil, Options{})
	recs, total, err := a.Query(context.Background(), Query{Sort: SortChrono})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 0 || len(recs) != 0 {
		t.Errorf("total/len = %d/%d, want 0/0", total, len(recs))
	}
}
