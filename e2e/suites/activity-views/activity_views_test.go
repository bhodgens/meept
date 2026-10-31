//go:build e2e

// Suite activity-views: hermetic live gate for the agent activity surface.
//
// A real meept-daemon boots over scratch stores seeded through the REAL
// on-disk store constructors (pre-boot hook opens tasks.db, plans.db,
// employees/audit.db and employees/audit_chain.db at the exact paths the
// boot resolves, writes fixtures, and closes every handle before boot).
// The suite then drives the REAL RPC surface on the daemon's Unix socket:
//
//	activity.query  sort=chrono|task|plan over the seeded cross-store data
//	activity.recent live tail: reflects bus events published through the
//	                daemon's own bus (RPC bus.publish -> real MessageBus
//	                -> real ring bridge). Models-less rig: a live chat
//	                turn is impractical, so the bus event is produced
//	                synthetically through the daemon process itself, per
//	                the leaf's sanctioned fallback.
//	activity.overview  per-agent rollup incl. drift_score + open findings
//	activity.findings  severity filter over the seeded findings
//	agents.review      drift score in [0,1] for a real hired employee
//
// No cloud endpoints: models.json5 points at the harness fake LLM, the
// classifier boot gate is explicitly disarmed, and the HTTP listener is
// remapped to a probed free port by the harness.
package activityviews

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// Fixture timeline (UTC). All seeded At stamps derive from this base so
// every sort mode has a deterministic expected order.
//
//	base+10m  step b1  (task-b, emp-b, completed)
//	base+25m  step a1  (task-a, emp-a, completed)
//	base+30m  step a2  (task-a, emp-a, completed)
//	base+40m  step c1  (task-c, emp-b, pending)
//	base+50m  finding act-find-crit (emp-a, severity critical, plan-alpha)
//	base+60m  finding act-find-warn (emp-b, severity warning, no plan)
//	base+70m  chain record (emp-a, plan_id act-plan-beta)
//
// Inverted plan linkage (plans.task_id -> tasks.id): act-plan-alpha ->
// act-task-b and act-plan-beta -> act-task-a. This makes the task and
// plan sort modes produce DIFFERENT sequences:
//
//	chrono: b1, a1, a2, c1
//	task:   a1, a2        (act-task-a group)
//	        b1            (act-task-b group)
//	        c1            (empty-group last)
//	plan:   b1            (act-plan-alpha group)
//	        a1, a2        (act-plan-beta group)
//	        c1            (empty-group last)
const (
	taskA = "act-task-a"
	taskB = "act-task-b"
	taskC = "act-task-c"

	stepA1 = "act-step-a1"
	stepA2 = "act-step-a2"
	stepB1 = "act-step-b1"
	stepC1 = "act-step-c1"

	planAlpha = "act-plan-alpha"
	planBeta  = "act-plan-beta"

	findCrit = "act-find-crit"
	findWarn = "act-find-warn"

	empA = "emp-a"
	empB = "emp-b"
)

func fixtureBase() time.Time {
	return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
}

// seedStores opens the REAL stores at the paths the daemon boot resolves
// (harness pre-boot hook contract), writes the fixtures, and closes every
// handle before returning so the boot never sees a foreign lock.
func seedStores(st *harness.Stack) error {
	ctx := context.Background()
	base := fixtureBase()
	logger := slog.Default()

	// tasks.db: tasks + steps (one database file, shared schema history).
	// The registry needs a real bus (its AmendmentManager subscribes at
	// construction); a scratch bus with no subscribers is fine - the
	// seeding publishes are fire-and-forget and the handle closes below.
	reg, err := task.NewRegistry(filepath.Join(st.StateDir, "tasks.db"), bus.New(nil, logger), logger)
	if err != nil {
		return err
	}
	ts := reg.Store()
	for _, tk := range []struct {
		id   string
		name string
	}{
		{taskA, "alpha carrier task"},
		{taskB, "beta carrier task"},
		{taskC, "planless task"},
	} {
		if err := ts.Create(&task.Task{
			ID:        tk.id,
			Name:      tk.name,
			State:     task.StatePending,
			CreatedAt: base,
			UpdatedAt: base,
		}); err != nil {
			return fmt.Errorf("seed: create task %s: %w", tk.id, err)
		}
	}
	steps := reg.StepStore()
	seedStep := func(id, taskID, desc, agent string, state task.StepState, at time.Time) error {
		return steps.Create(&task.TaskStep{
			ID:          id,
			TaskID:      taskID,
			Description: desc,
			AgentID:     agent,
			State:       state,
			Sequence:    0,
			CreatedAt:   at,
			UpdatedAt:   at,
		})
	}
	for _, s := range []struct {
		id, taskID, desc, agent string
		state                   task.StepState
		at                      time.Time
	}{
		{stepB1, taskB, "beta first step", empB, task.StepCompleted, base.Add(10 * time.Minute)},
		{stepA1, taskA, "alpha first step", empA, task.StepCompleted, base.Add(25 * time.Minute)},
		{stepA2, taskA, "alpha second step", empA, task.StepCompleted, base.Add(30 * time.Minute)},
		{stepC1, taskC, "planless step", empB, task.StepPending, base.Add(40 * time.Minute)},
	} {
		if err := seedStep(s.id, s.taskID, s.desc, s.agent, s.state, s.at); err != nil {
			return fmt.Errorf("seed: create step %s: %w", s.id, err)
		}
	}
	if err := reg.Close(); err != nil {
		return fmt.Errorf("seed: close task registry: %w", err)
	}

	// plans.db: inverted linkage rows (plans.task_id points at the parent
	// task). plan-alpha owns task-b; plan-beta owns task-a.
	pstore, err := plan.NewSQLiteStore(filepath.Join(st.StateDir, "plans.db"), logger)
	if err != nil {
		return fmt.Errorf("seed: open plan store: %w", err)
	}
	for _, p := range []struct {
		id    string
		title string
		task  string
	}{
		{planAlpha, "alpha plan", taskB},
		{planBeta, "beta plan", taskA},
	} {
		if err := pstore.CreatePlan(ctx, &plan.Plan{
			ID:        p.id,
			Title:     p.title,
			FilePath:  filepath.Join(st.Work, p.id+".md"),
			State:     plan.StateApproved,
			TaskID:    p.task,
			CreatedAt: base,
			UpdatedAt: base,
		}); err != nil {
			return fmt.Errorf("seed: create plan %s: %w", p.id, err)
		}
	}
	if err := pstore.Close(); err != nil {
		return fmt.Errorf("seed: close plan store: %w", err)
	}

	// employees/: findings (audit.db) + hash chain (audit_chain.db), the
	// standalone-layout paths employee wiring resolves when bots are
	// disabled and employees are enabled (the config default).
	empDir := filepath.Join(st.StateDir, "employees")
	if err := os.MkdirAll(empDir, 0o755); err != nil {
		return fmt.Errorf("seed: mkdir employees dir: %w", err)
	}
	auditStore, err := employee.NewAuditStore(filepath.Join(empDir, "audit.db"))
	if err != nil {
		return fmt.Errorf("seed: open audit store: %w", err)
	}
	seedFinding := func(id, empID, planID string, sev employee.AuditSeverity, rule string, at time.Time) error {
		return auditStore.Create(ctx, employee.AuditFinding{
			ID:           id,
			EmployeeID:   empID,
			PlanID:       planID,
			Severity:     sev,
			Checkpoint:   employee.CheckpointPeriodic,
			ViolatedRule: rule,
			Evidence:     "seeded e2e evidence for " + id,
			DetectedAt:   at,
		})
	}
	for _, f := range []struct {
		id, empID, planID, rule string
		sev                     employee.AuditSeverity
		at                      time.Time
	}{
		{findCrit, empA, planAlpha, "unapproved deletion", employee.SeverityCritical, base.Add(50 * time.Minute)},
		{findWarn, empB, "", "late review", employee.SeverityWarning, base.Add(60 * time.Minute)},
	} {
		if err := seedFinding(f.id, f.empID, f.planID, f.sev, f.rule, f.at); err != nil {
			return fmt.Errorf("seed: create finding %s: %w", f.id, err)
		}
	}
	if err := auditStore.Close(); err != nil {
		return fmt.Errorf("seed: close audit store: %w", err)
	}

	chain, err := auditlog.OpenStore(filepath.Join(empDir, "audit_chain.db"), logger)
	if err != nil {
		return fmt.Errorf("seed: open audit chain: %w", err)
	}
	if _, err := chain.Append(ctx, auditlog.Record{
		Type:       "plan.approve",
		EmployeeID: empA,
		Payload: map[string]any{
			"action":  "plan.approve",
			"plan_id": planBeta,
			"summary": "seeded chain approval of beta",
		},
		At: base.Add(70 * time.Minute),
	}); err != nil {
		return fmt.Errorf("seed: append chain record: %w", err)
	}
	if err := chain.Close(); err != nil {
		return fmt.Errorf("seed: close audit chain: %w", err)
	}
	return nil
}

// startStack boots the scratch daemon with the seed hook and the
// models-less scratch arming: classifier boot gate disarmed (the fake
// provider has no lifecycle blocks, but the explicit false keeps the
// models-less rule), HTTP already remapped by the harness.
func startStack(t *testing.T, seed bool) *harness.Stack {
	t.Helper()
	opts := []harness.StartOption{
		harness.WithConfigOverlay(map[string]any{
			"orchestrator.classifier_boot_fail_fast": false,
		}),
	}
	if seed {
		opts = append(opts, harness.WithPreBootHook(seedStores))
	}
	return harness.Start(t, opts...)
}

// dial opens one framed JSON-RPC client on the daemon's Unix socket.
func dial(t *testing.T, s *harness.Stack) *harness.RPCClient {
	t.Helper()
	return harness.DialRPC(t, s.SocketPath)
}

// queryRecords performs one activity.query and decodes the record list.
func queryRecords(t *testing.T, c *harness.RPCClient, params map[string]any) ([]activity.ActivityRecord, int) {
	t.Helper()
	res := c.CallResult("activity.query", params)
	recs := decodeRecords(t, res["records"])
	total, ok := res["total"].(float64)
	if !ok {
		t.Fatalf("activity.query total missing or not a number: %v", res)
	}
	return recs, int(total)
}

func decodeRecords(t *testing.T, v any) []activity.ActivityRecord {
	t.Helper()
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-encode records: %v", err)
	}
	var out []activity.ActivityRecord
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decode records: %v", err)
	}
	return out
}

// stepIDs extracts the StepID list of the task_step records in order.
func stepIDs(t *testing.T, recs []activity.ActivityRecord) []string {
	t.Helper()
	var out []string
	for _, r := range recs {
		if r.Kind != activity.KindTaskStep {
			t.Fatalf("unexpected kind %q in task_step-filtered result: %+v", r.Kind, r)
		}
		out = append(out, r.StepID)
	}
	return out
}

func assertOrder(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s order: got %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s order: got %v, want %v", what, got, want)
		}
	}
}

// assertBootLogArmed verifies config arming through the BOOT LOG LINE (the
// scratch rule: never trust a template file location), here the activity
// wiring confirmation emitted by wireActivityComponents.
func assertBootLogArmed(t *testing.T, s *harness.Stack) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.Work, "daemon.log"))
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	if !strings.Contains(string(data), "activity RPC handlers registered") {
		t.Fatalf("boot log missing \"activity RPC handlers registered\"; tail:\n%s", s.Daemon.LogTail())
	}
}

// T1: scratch boot over seeded stores; the REAL RPC answers activity.query
// and the arming is proven through the boot log line.
// Covers activity-views-01.
func TestActivityViews_BootLogAndQuery(t *testing.T) {
	s := startStack(t, true)
	assertBootLogArmed(t, s)
	c := dial(t, s)

	// Given/When/Then from the leaf: seeded tasks.db, activity.query
	// limit=1 returns exactly 1 record; it is the chronologically
	// earliest step (b1 at base+10m).
	recs, total := queryRecords(t, c, map[string]any{"limit": 1})
	if len(recs) != 1 {
		t.Fatalf("activity.query limit=1 returned %d records, want 1", len(recs))
	}
	if recs[0].StepID != stepB1 {
		t.Fatalf("limit=1 chrono head = step %q, want %q (record: %+v)", recs[0].StepID, stepB1, recs[0])
	}
	if total < 4 {
		t.Fatalf("total = %d, want >= 4 seeded task steps", total)
	}
}

// T2: sort modes and filters over the wire.
// Covers activity-views-02.
func TestActivityViews_SortModesAndFilters(t *testing.T) {
	s := startStack(t, true)
	c := dial(t, s)

	// chrono: pure time order over the seeded steps.
	recs, _ := queryRecords(t, c, map[string]any{"sort": "chrono", "kinds": []string{"task_step"}})
	assertOrder(t, "chrono", stepIDs(t, recs), []string{stepB1, stepA1, stepA2, stepC1})

	// task: groups by TaskID ascending, time ascending within group.
	recs, _ = queryRecords(t, c, map[string]any{"sort": "task", "kinds": []string{"task_step"}})
	assertOrder(t, "task", stepIDs(t, recs), []string{stepA1, stepA2, stepB1, stepC1})

	// plan: groups by the RESOLVED PlanID (inverted linkage through the
	// daemon's own PlanManager resolver) ascending, time ascending within
	// group; the planless task sorts last.
	recs, _ = queryRecords(t, c, map[string]any{"sort": "plan", "kinds": []string{"task_step"}})
	assertOrder(t, "plan", stepIDs(t, recs), []string{stepB1, stepA1, stepA2, stepC1})
	// plan stamping must actually have happened (alpha group = task-b).
	planByStep := map[string]string{}
	for _, r := range recs {
		planByStep[r.StepID] = r.PlanID
	}
	if planByStep[stepB1] != planAlpha || planByStep[stepA1] != planBeta || planByStep[stepA2] != planBeta {
		t.Fatalf("plan stamping wrong: %+v", planByStep)
	}
	if planByStep[stepC1] != "" {
		t.Fatalf("planless step carries plan_id %q, want empty", planByStep[stepC1])
	}

	// plan filter: only records belonging to act-plan-beta (its task-a
	// steps, stamped by the resolver).
	recs, _ = queryRecords(t, c, map[string]any{"sort": "plan", "plan_id": planBeta})
	var betaSteps []string
	for _, r := range recs {
		if r.Kind == activity.KindTaskStep {
			betaSteps = append(betaSteps, r.StepID)
			if r.PlanID != planBeta {
				t.Fatalf("step %s stamped plan %q, want %q", r.StepID, r.PlanID, planBeta)
			}
		}
	}
	assertOrder(t, "plan filter", betaSteps, []string{stepA1, stepA2})

	// agent filter over the wire.
	recs, _ = queryRecords(t, c, map[string]any{"kinds": []string{"task_step"}, "agent_id": empA})
	assertOrder(t, "agent filter", stepIDs(t, recs), []string{stepA1, stepA2})

	// kind filter holds: task_step only, despite seeded audit records.
	recs, totalKinds := queryRecords(t, c, map[string]any{"kinds": []string{"task_step"}})
	assertOrder(t, "kind filter", stepIDs(t, recs), []string{stepB1, stepA1, stepA2, stepC1})
	if totalKinds != 4 {
		t.Fatalf("kinds-filtered total = %d, want 4", totalKinds)
	}
}

// publishJob publishes one synthetic bus event through the daemon's REAL
// RPC bus.publish (models-less rig path: the event flows rpc -> real
// MessageBus -> real ring bridge -> real Ring inside the daemon process).
func publishJob(t *testing.T, c *harness.RPCClient, jobID string) {
	t.Helper()
	res := c.CallResult("bus.publish", map[string]any{
		"topic":   "queue.job.completed",
		"payload": json.RawMessage(`{"job_id":"` + jobID + `","worker_id":"` + empA + `"}`),
	})
	if delivered, ok := res["delivered"].(float64); !ok || delivered < 1 {
		t.Fatalf("bus.publish delivered = %v, want >= 1 (ring bridge must be subscribed)", res["delivered"])
	}
}

// fetchRecent performs one activity.recent call and returns the records.
func fetchRecent(t *testing.T, c *harness.RPCClient) ([]activity.ActivityRecord, int) {
	t.Helper()
	res := c.CallResult("activity.recent", map[string]any{"limit": 100})
	recs := decodeRecords(t, res["records"])
	dropped, ok := res["dropped"].(float64)
	if !ok {
		t.Fatalf("activity.recent dropped missing: %v", res)
	}
	return recs, int(dropped)
}

// indexOfJob returns the position of the bus_event record carrying the
// given job id in its detail map.
func indexOfJob(recs []activity.ActivityRecord, jobID string) int {
	for i, r := range recs {
		if r.Kind == "bus_event" && r.Detail["job_id"] == jobID {
			return i
		}
	}
	return -1
}

// T3 (live ring): activity.recent reflects bus events produced through a
// real daemon process, in ring insertion order, with a live dropped counter.
// Covers activity-views-03.
func TestActivityViews_LiveRingRecent(t *testing.T) {
	s := startStack(t, true)
	c := dial(t, s)

	publishJob(t, c, "act-job-1")
	harness.WaitFor(t, 30*time.Second, "act-job-1 in activity.recent", func() bool {
		recs, _ := fetchRecent(t, c)
		return indexOfJob(recs, "act-job-1") >= 0
	})
	publishJob(t, c, "act-job-2")
	harness.WaitFor(t, 30*time.Second, "act-job-2 in activity.recent", func() bool {
		recs, _ := fetchRecent(t, c)
		return indexOfJob(recs, "act-job-2") >= 0
	})

	recs, dropped := fetchRecent(t, c)
	i1, i2 := indexOfJob(recs, "act-job-1"), indexOfJob(recs, "act-job-2")
	if i1 < 0 || i2 < 0 {
		t.Fatalf("live ring lost jobs: i1=%d i2=%d in %d records", i1, i2, len(recs))
	}
	// Ring tail is insertion-ordered (oldest first): job-2 published after
	// job-1 must appear after it.
	if i2 <= i1 {
		t.Fatalf("ring order wrong: job-1 at %d, job-2 at %d (want i2 > i1)", i1, i2)
	}
	for _, r := range recs {
		if r.Kind == "bus_event" && r.Detail["job_id"] == "act-job-1" && r.Summary != "job completed" {
			t.Fatalf("bus record summary = %q, want \"job completed\"", r.Summary)
		}
	}
	if dropped < 0 {
		t.Fatalf("dropped counter = %d, want >= 0", dropped)
	}
}

// hireEmployee creates a real employee through the REAL agents.create RPC
// (same validation path as production hires).
func hireEmployee(t *testing.T, c *harness.RPCClient, id string) {
	t.Helper()
	def := map[string]any{
		"id":      id,
		"name":    id,
		"prompt":  "You are " + id + ", an e2e activity-views employee.",
		"enabled": true,
		"triggers": []map[string]any{
			{"type": "cron", "schedule": "0 4 * * *", "enabled": false},
		},
		"constitution": map[string]any{
			"purpose":       "e2e activity-views probe",
			"role":          "e2e activity-views employee",
			"charter":       "Observe and report. Never delete data.",
			"autonomy_tier": "tier_1_reactive",
			"escalates_to":  []string{"user"},
			"constraints": map[string]any{
				"tools_allowed": []string{},
				"risk_ceiling":  "low",
				"never":         []string{"delete production data"},
			},
			"amendment_policy": map[string]any{
				"self_propose_allowed": false,
				"requires_approval":    true,
				"frozen_fields":        []string{},
			},
			"max_active_plans": 1,
			"version":          1,
			"authored_by":      "user",
		},
	}
	created := c.CallResult("agents.create", def)
	if got, _ := created["id"].(string); got != id {
		t.Fatalf("agents.create id = %v, want %s", created["id"], id)
	}
}

// T3 (overview + findings + review): the drift and findings RPC surface
// over the seeded stores and hired employees.
// Covers activity-views-04.
func TestActivityViews_OverviewFindingsReview(t *testing.T) {
	s := startStack(t, true)
	c := dial(t, s)

	hireEmployee(t, c, empA)
	hireEmployee(t, c, empB)

	// activity.overview: by_agent carries drift_score per seeded employee
	// once the rollup includes the seeded activity. Poll with a deadline;
	// fixed sleeps are a defect under parallel-suite load.
	var rowA map[string]any
	harness.WaitFor(t, 30*time.Second, "emp-a row in activity.overview", func() bool {
		res := c.CallResult("activity.overview", nil)
		byAgent, ok := res["by_agent"].([]any)
		if !ok {
			return false
		}
		for _, raw := range byAgent {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if id, _ := row["agent_id"].(string); id == empA {
				rowA = row
				return true
			}
		}
		return false
	})
	drift, ok := rowA["drift_score"].(float64)
	if !ok {
		t.Fatalf("overview emp-a drift_score missing: %v", rowA)
	}
	if drift < 0 || drift > 1 {
		t.Fatalf("overview emp-a drift_score = %v, want in [0,1]", drift)
	}
	if open, _ := rowA["open_findings"].(float64); int(open) != 1 {
		t.Fatalf("overview emp-a open_findings = %v, want 1 (the seeded critical finding)", rowA["open_findings"])
	}
	if last, _ := rowA["last_activity"].(string); last == "" {
		t.Fatalf("overview emp-a last_activity empty: %v", rowA)
	}

	// activity.findings severity filter: only the critical finding returns.
	res := c.CallResult("activity.findings", map[string]any{"severity": "critical"})
	findings, ok := res["findings"].([]any)
	if !ok {
		t.Fatalf("activity.findings findings missing: %v", res)
	}
	if total, _ := res["total"].(float64); int(total) != 1 || len(findings) != 1 {
		t.Fatalf("severity=critical returned total=%v len=%d, want 1/1", res["total"], len(findings))
	}
	f0, _ := findings[0].(map[string]any)
	if id, _ := f0["id"].(string); id != findCrit {
		t.Fatalf("severity=critical returned finding %v, want %s", f0["id"], findCrit)
	}
	if sev, _ := f0["severity"].(string); sev != "critical" {
		t.Fatalf("finding severity = %q, want critical", sev)
	}

	// Unfiltered: both seeded findings.
	res = c.CallResult("activity.findings", map[string]any{})
	if total, _ := res["total"].(float64); int(total) != 2 {
		t.Fatalf("unfiltered findings total = %v, want 2", res["total"])
	}

	// agents.review (C4, first exposure of Manager.Review over RPC):
	// drift in [0,1] for a real hired employee.
	review := c.CallResult("agents.review", map[string]any{"employee_id": empA})
	rd, ok := review["drift_score"].(float64)
	if !ok {
		t.Fatalf("agents.review drift_score missing: %v", review)
	}
	if rd < 0 || rd > 1 {
		t.Fatalf("agents.review drift_score = %v, want in [0,1]", rd)
	}
	emp, ok := review["employee"].(map[string]any)
	if !ok {
		t.Fatalf("agents.review employee missing: %v", review)
	}
	if id, _ := emp["id"].(string); id != empA {
		t.Fatalf("agents.review employee.id = %v, want %s", emp["id"], empA)
	}
	if _, ok := review["recent_findings"].([]any); !ok {
		t.Fatalf("agents.review recent_findings missing: %v", review)
	}
}
