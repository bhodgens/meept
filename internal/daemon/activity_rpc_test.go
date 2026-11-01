package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// activityRPCFixture seeds real stores (task registry + steps, session
// memory store, audit chain, employee findings, bot-backed employee
// manager) and registers the activity RPC closures over them. Everything
// lives in a t.TempDir; nothing here touches $MEEPT_HOME.
type activityRPCFixture struct {
	srv     *rpc.Server
	reg     *task.Registry
	steps   *task.StepStore
	sess    session.Store
	chain   *auditlog.Store
	audit   *employee.AuditStore
	mgr     *employee.Manager
	taskIDs []string
}

func newActivityRPCFixture(t *testing.T) *activityRPCFixture {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Hour)

	reg, err := task.NewRegistry(filepath.Join(dir, "tasks.db"), bus.New(nil, nil), nil)
	if err != nil {
		t.Fatalf("task.NewRegistry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	// Two tasks, one step each, distinct agents and timestamps.
	type seededTask struct {
		agent  string
		stepID string
		state  task.StepState
	}
	seeded := []seededTask{
		{agent: "emp-1", stepID: "step-1", state: task.StepRunning},
		{agent: "emp-2", stepID: "step-2", state: task.StepCompleted},
	}
	var taskIDs []string
	for i, s := range seeded {
		tk, err := reg.Create(ctx, fmt.Sprintf("task-%d", i), "seed task")
		if err != nil {
			t.Fatalf("registry.Create: %v", err)
		}
		tk.AssignedAgent = s.agent
		if err := reg.Update(ctx, tk); err != nil {
			t.Fatalf("registry.Update: %v", err)
		}
		taskIDs = append(taskIDs, tk.ID)

		step := &task.TaskStep{
			ID:          s.stepID,
			TaskID:      tk.ID,
			Description: fmt.Sprintf("step %d", i),
			AgentID:     s.agent,
			State:       s.state,
			Sequence:    i,
			CreatedAt:   ts.Add(time.Duration(i) * time.Minute),
			UpdatedAt:   ts.Add(time.Duration(i) * time.Minute),
		}
		if err := reg.StepStore().Create(step); err != nil {
			t.Fatalf("StepStore.Create: %v", err)
		}
	}

	sessions := session.NewMemoryStore(nil)
	sess, err := sessions.Create("seed session")
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := sessions.SaveMessages(sess.ID, []session.Message{{
		SessionID: sess.ID,
		Role:      "user",
		Content:   "hello from the seed fixture",
		Timestamp: ts.Add(5 * time.Minute),
		EntryType: "message",
		BranchID:  "main",
	}, {
		SessionID: sess.ID,
		Role:      "assistant",
		Content:   "seed reply",
		Timestamp: ts.Add(6 * time.Minute),
		EntryType: "message",
		BranchID:  "main",
	}}); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}
	if err := sessions.SetEmployeeID(sess.ID, "emp-1"); err != nil {
		t.Fatalf("SetEmployeeID: %v", err)
	}

	chain, err := auditlog.OpenStore(filepath.Join(dir, "audit_chain.db"), nil)
	if err != nil {
		t.Fatalf("auditlog.OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = chain.Close() })
	if _, err := chain.Append(ctx, auditlog.Record{
		Type:       "seed_event",
		EmployeeID: "emp-1",
		Payload:    map[string]any{"action": "seed", "severity": "info"},
		At:         ts.Add(6 * time.Minute),
	}); err != nil {
		t.Fatalf("chain.Append: %v", err)
	}

	audit, err := employee.NewAuditStore(filepath.Join(dir, "audit.db"))
	if err != nil {
		t.Fatalf("employee.NewAuditStore: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })
	seedFinding := func(id, emp, sev string, resolved bool) {
		t.Helper()
		f := employee.AuditFinding{
			ID:           id,
			EmployeeID:   emp,
			Severity:     employee.AuditSeverity(sev),
			Checkpoint:   employee.CheckpointPostTurn,
			ViolatedRule: "rule-1",
			DetectedAt:   ts.Add(7 * time.Minute),
		}
		if resolved {
			now := time.Now().UTC()
			f.ResolvedAt = &now
			f.Resolution = "acknowledged"
		}
		if err := audit.Create(ctx, f); err != nil {
			t.Fatalf("audit.Create(%s): %v", id, err)
		}
	}
	seedFinding("find-1", "emp-1", "warning", false)
	seedFinding("find-2", "emp-1", "critical", false)
	seedFinding("find-3", "emp-1", "warning", true)
	seedFinding("find-4", "emp-2", "info", false)

	botStore, err := bot.NewStore(filepath.Join(dir, "bots.db"))
	if err != nil {
		t.Fatalf("bot.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	botMgr := bot.NewManager(botStore, nil)
	if err := botMgr.CreateBot(ctx, bot.BotDefinition{
		ID:       "emp-1",
		Name:     "seed employee one",
		Prompt:   "seed prompt",
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("CreateBot(emp-1): %v", err)
	}
	if err := botMgr.CreateBot(ctx, bot.BotDefinition{
		ID:       "emp-2",
		Name:     "seed employee two",
		Prompt:   "seed prompt",
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("CreateBot(emp-2): %v", err)
	}

	mgr := employee.NewManagerWithStores(botMgr, botStore, nil, nil, audit, nil)

	f := &activityRPCFixture{
		srv:     rpc.New(&rpc.Config{SocketPath: ""}, nil, nil),
		reg:     reg,
		steps:   reg.StepStore(),
		sess:    sessions,
		chain:   chain,
		audit:   audit,
		mgr:     mgr,
		taskIDs: taskIDs,
	}
	agg := activity.NewAggregator(reg, f.steps, sessions, chain, audit, activity.Options{})
	ring := activity.NewRing(1000)
	registerActivityRPC(f.srv, agg, ring, mgr, nil)
	return f
}

func callActivity(t *testing.T, srv *rpc.Server, method, params string) (map[string]any, error) {
	t.Helper()
	result, err := srv.CallMethod(context.Background(), method, json.RawMessage(params))
	if err != nil {
		return nil, err
	}
	// JSON round-trip so assertions see the wire shape (typed slices and
	// structs become []any / map[string]any, ints become float64).
	b, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s result not JSON-shaped: %v", method, err)
	}
	return m, nil
}

func mustJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// numAsFloat normalizes a numeric result field (int or float64 — the
// handler returns raw Go values, not JSON round-tripped ones).
func numAsFloat(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	case int64:
		return float64(n)
	default:
		t.Fatalf("numeric field type %T, want int/float64", v)
		return 0
	}
}

// TestActivityRPC_QueryShape covers T1: param decode, sort/filter
// passthrough, and the byte-level C2 result shape
// {"records":[...],"total":N}.
func TestActivityRPC_QueryShape(t *testing.T) {
	f := newActivityRPCFixture(t)

	t.Run("default chrono query returns C2 shape", func(t *testing.T) {
		m, err := callActivity(t, f.srv, "activity.query", `{}`)
		if err != nil {
			t.Fatalf("activity.query: %v", err)
		}
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var shaped struct {
			Records []map[string]any `json:"records"`
			Total   int              `json:"total"`
		}
		if err := json.Unmarshal(raw, &shaped); err != nil {
			t.Fatalf("result does not match C2 shape: %v", err)
		}
		if shaped.Total <= 0 {
			t.Fatalf("total = %d, want > 0", shaped.Total)
		}
		if len(shaped.Records) != shaped.Total {
			t.Fatalf("records = %d, total = %d (no paging: must match)", len(shaped.Records), shaped.Total)
		}
		for _, rec := range shaped.Records {
			for _, key := range []string{"kind", "at", "summary"} {
				if _, ok := rec[key]; !ok {
					t.Errorf("record missing %q key: %v", key, rec)
				}
			}
		}
	})

	t.Run("sort=task groups by task", func(t *testing.T) {
		m, err := callActivity(t, f.srv, "activity.query", `{"sort":"task","kinds":["task_step"]}`)
		if err != nil {
			t.Fatalf("activity.query: %v", err)
		}
		recs, _ := m["records"].([]any)
		if len(recs) != 2 {
			t.Fatalf("records = %d, want 2", len(recs))
		}
		first := recs[0].(map[string]any)
		second := recs[1].(map[string]any)
		id1, _ := first["task_id"].(string)
		id2, _ := second["task_id"].(string)
		if id1 > id2 {
			t.Errorf("records not grouped by task_id: %q before %q", id1, id2)
		}
	})

	t.Run("unknown sort is an error", func(t *testing.T) {
		_, err := callActivity(t, f.srv, "activity.query", `{"sort":"banana"}`)
		if err == nil {
			t.Fatal("unknown sort must error")
		}
		if !strings.Contains(err.Error(), "sort") {
			t.Errorf("error = %v, want it to mention sort", err)
		}
	})

	t.Run("kinds filter narrows records", func(t *testing.T) {
		m, err := callActivity(t, f.srv, "activity.query", `{"kinds":["session_message"]}`)
		if err != nil {
			t.Fatalf("activity.query: %v", err)
		}
		if got := numAsFloat(t, m["total"]); got != 2 {
			t.Fatalf("total = %v, want 2 session messages", m["total"])
		}
	})

	t.Run("limit and offset paginate with total preserved", func(t *testing.T) {
		m, err := callActivity(t, f.srv, "activity.query", `{"limit":1,"offset":1}`)
		if err != nil {
			t.Fatalf("activity.query: %v", err)
		}
		recs, _ := m["records"].([]any)
		if len(recs) != 1 {
			t.Fatalf("records = %d, want 1", len(recs))
		}
		total, _ := m["total"].(float64)
		if total < 2 {
			t.Fatalf("total = %v, want >= 2 (total counts pre-page matches)", m["total"])
		}
	})

	t.Run("invalid since timestamp is an error", func(t *testing.T) {
		_, err := callActivity(t, f.srv, "activity.query", `{"since":"not-a-time"}`)
		if err == nil {
			t.Fatal("invalid since must error")
		}
	})

	t.Run("task_id filter", func(t *testing.T) {
		m, err := callActivity(t, f.srv, "activity.query", fmt.Sprintf(`{"task_id":%q,"kinds":["task_step"]}`, f.taskIDs[0]))
		if err != nil {
			t.Fatalf("activity.query: %v", err)
		}
		recs, _ := m["records"].([]any)
		if len(recs) != 1 {
			t.Fatalf("records = %d, want 1", len(recs))
		}
		first := recs[0].(map[string]any)
		if got, _ := first["task_id"].(string); got != f.taskIDs[0] {
			t.Errorf("task_id = %q, want %q", got, f.taskIDs[0])
		}
	})
}

// TestActivityRPC_RecentAndOverview covers T2: ring tail + dropped counter,
// and per-agent overview aggregation with drift from mgr.Review.
func TestActivityRPC_RecentAndOverview(t *testing.T) {
	t.Run("recent returns limit newest plus dropped", func(t *testing.T) {
		ring := activity.NewRing(10)
		for i := 0; i < 5; i++ {
			ring.Append(activity.ActivityRecord{
				Kind:    activity.KindTaskStep,
				At:      time.Now().UTC().Add(time.Duration(i) * time.Second),
				AgentID: "emp-1",
				Summary: fmt.Sprintf("event %d", i),
			})
		}
		srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, nil)
		registerActivityRPC(srv, nil, ring, nil, nil)

		m, err := callActivity(t, srv, "activity.recent", `{"limit":3}`)
		if err != nil {
			t.Fatalf("activity.recent: %v", err)
		}
		recs, _ := m["records"].([]any)
		if len(recs) != 3 {
			t.Fatalf("records = %d, want 3", len(recs))
		}
		if _, ok := m["dropped"]; !ok {
			t.Fatal("result missing dropped counter")
		}
	})

	t.Run("dropped counts evicted events", func(t *testing.T) {
		ring := activity.NewRing(10)
		for i := 0; i < 12; i++ {
			ring.Append(activity.ActivityRecord{Kind: "bus_event", Summary: "x"})
		}
		srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, nil)
		registerActivityRPC(srv, nil, ring, nil, nil)

		m, err := callActivity(t, srv, "activity.recent", `{"limit":5}`)
		if err != nil {
			t.Fatalf("activity.recent: %v", err)
		}
		if got := numAsFloat(t, m["dropped"]); got != 2 {
			t.Fatalf("dropped = %v, want 2", m["dropped"])
		}
		recs, _ := m["records"].([]any)
		if len(recs) != 5 {
			t.Fatalf("records = %d, want 5", len(recs))
		}
	})

	t.Run("overview aggregates per agent with drift score", func(t *testing.T) {
		f := newActivityRPCFixture(t)
		m, err := callActivity(t, f.srv, "activity.overview", `{}`)
		if err != nil {
			t.Fatalf("activity.overview: %v", err)
		}
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var shaped struct {
			ByAgent []struct {
				AgentID      string    `json:"agent_id"`
				ActiveTasks  int       `json:"active_tasks"`
				LastActivity time.Time `json:"last_activity"`
				DriftScore   float64   `json:"drift_score"`
				OpenFindings int       `json:"open_findings"`
			} `json:"by_agent"`
			GeneratedAt time.Time `json:"generated_at"`
		}
		if err := json.Unmarshal(raw, &shaped); err != nil {
			t.Fatalf("overview shape mismatch: %v", err)
		}
		if shaped.GeneratedAt.IsZero() {
			t.Error("generated_at missing")
		}
		if len(shaped.ByAgent) != 2 {
			t.Fatalf("by_agent entries = %d, want 2", len(shaped.ByAgent))
		}
		byID := map[string]int{}
		for _, a := range shaped.ByAgent {
			byID[a.AgentID] = a.ActiveTasks
			if a.DriftScore < 0 || a.DriftScore > 1 {
				t.Errorf("agent %s drift_score = %v, want in [0,1]", a.AgentID, a.DriftScore)
			}
		}
		// emp-1 has one running step (active) + one completed step.
		if byID["emp-1"] != 1 {
			t.Errorf("emp-1 active_tasks = %d, want 1", byID["emp-1"])
		}
		// emp-2's only step is completed: zero active.
		if byID["emp-2"] != 0 {
			t.Errorf("emp-2 active_tasks = %d, want 0", byID["emp-2"])
		}
		for _, a := range shaped.ByAgent {
			if a.AgentID == "emp-1" && a.OpenFindings != 2 {
				t.Errorf("emp-1 open_findings = %d, want 2", a.OpenFindings)
			}
			if a.AgentID == "emp-1" && a.LastActivity.IsZero() {
				t.Errorf("emp-1 last_activity missing")
			}
		}
	})
}

// TestActivityRPC_FindingsAndReview covers T3: findings filters over the
// real Manager.ListAuditFindings and agents.review drift exposure.
func TestActivityRPC_FindingsAndReview(t *testing.T) {
	f := newActivityRPCFixture(t)

	t.Run("findings by employee", func(t *testing.T) {
		m, err := callActivity(t, f.srv, "activity.findings", `{"employee_id":"emp-1"}`)
		if err != nil {
			t.Fatalf("activity.findings: %v", err)
		}
		if got := numAsFloat(t, m["total"]); got != 3 {
			t.Fatalf("total = %v, want 3", m["total"])
		}
		findings, _ := m["findings"].([]any)
		if len(findings) != 3 {
			t.Fatalf("findings = %d, want 3", len(findings))
		}
	})

	t.Run("findings by severity", func(t *testing.T) {
		m, err := callActivity(t, f.srv, "activity.findings", `{"employee_id":"emp-1","severity":"critical"}`)
		if err != nil {
			t.Fatalf("activity.findings: %v", err)
		}
		if got := numAsFloat(t, m["total"]); got != 1 {
			t.Fatalf("total = %v, want 1", m["total"])
		}
	})

	t.Run("findings resolved filter", func(t *testing.T) {
		m, err := callActivity(t, f.srv, "activity.findings", `{"employee_id":"emp-1","resolved":true}`)
		if err != nil {
			t.Fatalf("activity.findings: %v", err)
		}
		if got := numAsFloat(t, m["total"]); got != 1 {
			t.Fatalf("total = %v, want 1", m["total"])
		}
	})

	t.Run("findings limit and offset", func(t *testing.T) {
		m, err := callActivity(t, f.srv, "activity.findings", `{"employee_id":"emp-1","limit":1,"offset":1}`)
		if err != nil {
			t.Fatalf("activity.findings: %v", err)
		}
		findings, _ := m["findings"].([]any)
		if len(findings) != 1 {
			t.Fatalf("findings = %d, want 1", len(findings))
		}
		if got := numAsFloat(t, m["total"]); got != 3 {
			t.Fatalf("total = %v, want 3 (pre-page)", m["total"])
		}
	})

	t.Run("findings since in the future yields zero", func(t *testing.T) {
		future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
		m, err := callActivity(t, f.srv, "activity.findings", fmt.Sprintf(`{"since":%q}`, future))
		if err != nil {
			t.Fatalf("activity.findings: %v", err)
		}
		if got := numAsFloat(t, m["total"]); got != 0 {
			t.Fatalf("total = %v, want 0", m["total"])
		}
	})

	t.Run("agents.review returns drift score in range", func(t *testing.T) {
		result, err := f.srv.CallMethod(context.Background(), "agents.review", json.RawMessage(`{"employee_id":"emp-1"}`))
		if err != nil {
			t.Fatalf("agents.review: %v", err)
		}
		m := mustJSON(t, result)
		drift, ok := m["drift_score"].(float64)
		if !ok {
			t.Fatalf("drift_score missing or non-numeric: %v", m)
		}
		if drift < 0 || drift > 1 {
			t.Errorf("drift_score = %v, want in [0,1]", drift)
		}
		if _, ok := m["recent_findings"]; !ok {
			t.Error("recent_findings missing from review result")
		}
	})

	t.Run("agents.review requires employee_id", func(t *testing.T) {
		_, err := callActivity(t, f.srv, "agents.review", `{}`)
		if err == nil {
			t.Fatal("empty employee_id must error")
		}
	})

	t.Run("agents.review unknown employee errors", func(t *testing.T) {
		_, err := callActivity(t, f.srv, "agents.review", `{"employee_id":"nobody"}`)
		if err == nil {
			t.Fatal("unknown employee must error")
		}
		if !errors.Is(err, employee.ErrEmployeeNotFound) {
			t.Errorf("error = %v, want ErrEmployeeNotFound", err)
		}
	})
}

// TestActivityRPC_NilHandles covers the nil-safety checklist item: every
// method degrades to a clean error, never a panic.
func TestActivityRPC_NilHandles(t *testing.T) {
	srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, nil)
	registerActivityRPC(srv, nil, nil, nil, nil)

	cases := []struct {
		method string
		params string
	}{
		{"activity.query", `{}`},
		{"activity.recent", `{}`},
		{"activity.overview", `{}`},
		{"activity.findings", `{}`},
		{"agents.review", `{"employee_id":"emp-1"}`},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			_, err := callActivity(t, srv, tc.method, tc.params)
			if err == nil {
				t.Fatalf("%s with nil handles must return an error", tc.method)
			}
		})
	}
}
