package metrics

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// seedAgentTaskSchema creates the agent_task_outcomes/agent_states tables the
// TaskCollector owns, so QueryAgentUsage can be exercised against a store that
// only ran Store.initSchema.
const testAgentTaskDDL = `
CREATE TABLE IF NOT EXISTS agent_task_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    success BOOLEAN
)`

func TestModelUsageSince_AggregatesWindowAndOrders(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	// prov-a/m1: 2 calls, 300 in / 30 out, latency 100+300 -> avg 200.
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1",
		TokensSent: 100, TokensRecv: 10, LatencyMs: 100})
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1",
		TokensSent: 200, TokensRecv: 20, LatencyMs: 300})
	// prov-a/m3 and prov-b/m2: one call each, tie broken by id ASC.
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m3",
		TokensSent: 1000, TokensRecv: 100, LatencyMs: 5})
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-b", ModelID: "m2",
		TokensSent: 50, TokensRecv: 5, LatencyMs: 10})
	// Outside the 24h window: must be excluded.
	s.RecordLLMCall(LLMCallRecord{Timestamp: now.Add(-48 * time.Hour), Provider: "prov-a", ModelID: "m1",
		TokensSent: 999999, TokensRecv: 999999})

	rows, err := s.ModelUsageSince(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ModelUsageSince: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %+v", len(rows), rows)
	}
	first := rows[0]
	if first.ID != "prov-a/m1" || first.Calls != 2 || first.TokensIn != 300 ||
		first.TokensOut != 30 || first.AvgLatencyMs != 200 {
		t.Errorf("row[0] = %+v, want prov-a/m1 calls=2 in=300 out=30 avg=200", first)
	}
	// calls desc, then id asc: prov-a/m3 before prov-b/m2.
	if rows[1].ID != "prov-a/m3" || rows[2].ID != "prov-b/m2" {
		t.Errorf("ordering = [%s, %s], want [prov-a/m3, prov-b/m2]", rows[1].ID, rows[2].ID)
	}

	totals := SumModelUsage(rows)
	if totals.Calls != 4 || totals.TokensIn != 1350 || totals.TokensOut != 135 {
		t.Errorf("totals = %+v, want calls=4 in=1350 out=135", totals)
	}

	// A store with no llm_calls rows returns a non-nil empty slice.
	empty := newTestStore(t)
	rows, err = empty.ModelUsageSince(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ModelUsageSince (empty): %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("empty store: got %v (nil=%v), want non-nil empty slice", rows, rows == nil)
	}
}

func TestAgentUsageSince_UnionCountsAndState(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	if _, err := s.DB().Exec(testAgentTaskDDL); err != nil {
		t.Fatalf("create agent_task_outcomes: %v", err)
	}
	// chat: 2 completed, 1 failed. coder: 1 completed.
	seed := []struct {
		task, agent string
		success     int
	}{
		{"t1", "chat", 1}, {"t2", "chat", 1}, {"t3", "chat", 0}, {"t4", "coder", 1},
	}
	for _, r := range seed {
		if _, err := s.DB().Exec(
			`INSERT INTO agent_task_outcomes (task_id, agent_id, success) VALUES (?, ?, ?)`,
			r.task, r.agent, r.success); err != nil {
			t.Fatalf("insert outcome: %v", err)
		}
	}
	// idle-agent has only LLM calls, no task outcomes: rows must still appear
	// with zero counts.
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1", AgentID: "idle-agent"})

	// chat has a persisted state snapshot.
	if _, err := s.DB().Exec(
		`CREATE TABLE IF NOT EXISTS agent_states (agent_id TEXT PRIMARY KEY, state_data TEXT NOT NULL, updated_at DATETIME NOT NULL)`); err != nil {
		t.Fatalf("create agent_states: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO agent_states (agent_id, state_data, updated_at) VALUES (?, ?, ?)`,
		"chat", `{"current_state":"idle","is_active":false}`, now.UTC()); err != nil {
		t.Fatalf("insert agent state: %v", err)
	}

	rows, err := s.AgentUsageSince(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("AgentUsageSince: %v", err)
	}
	byID := map[string]AgentUsage{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if len(rows) != 3 {
		t.Fatalf("got %d agents, want 3: %+v", len(rows), rows)
	}
	if got := byID["chat"]; got.TasksCompleted != 2 || got.TasksFailed != 1 || got.State != "idle" {
		t.Errorf("chat = %+v, want completed=2 failed=1 state=idle", got)
	}
	if got := byID["coder"]; got.TasksCompleted != 1 || got.TasksFailed != 0 || got.State != "" {
		t.Errorf("coder = %+v, want completed=1 failed=0 state=\"\"", got)
	}
	if got := byID["idle-agent"]; got.TasksCompleted != 0 || got.TasksFailed != 0 {
		t.Errorf("idle-agent = %+v, want completed=0 failed=0", got)
	}
	// Sorted by id ascending.
	if rows[0].ID != "chat" || rows[1].ID != "coder" || rows[2].ID != "idle-agent" {
		t.Errorf("ordering = [%s, %s, %s], want [chat, coder, idle-agent]", rows[0].ID, rows[1].ID, rows[2].ID)
	}
}

// TestQueryAgentUsage_MissingTablesTreatedAsEmpty verifies a bare metrics.db
// (no agent_task_outcomes) still yields the llm_calls-derived agents.
func TestQueryAgentUsage_MissingTablesTreatedAsEmpty(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "p", ModelID: "m", AgentID: "solo"})

	rows, err := s.AgentUsageSince(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("AgentUsageSince: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "solo" || rows[0].TasksCompleted != 0 {
		t.Fatalf("rows = %+v, want one agent 'solo' with zero counts", rows)
	}
}

// TestReadOnlyStore_QueriesExistingDatabase proves the HTTP fallback path: a
// read-only handle on a live metrics.db returns the same aggregates.
func TestReadOnlyStore_QueriesExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.db")
	store, err := NewStore(&StoreConfig{DatabasePath: path, BatchSize: 1, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	now := time.Now()
	store.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-ro", ModelID: "m1",
		AgentID: "chat", TokensSent: 7, TokensRecv: 3, LatencyMs: 42})

	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer ro.Close()

	models, err := ro.ModelUsageSince(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ro.ModelUsageSince: %v", err)
	}
	if len(models) != 1 || models[0].ID != "prov-ro/m1" || models[0].TokensIn != 7 {
		t.Fatalf("models = %+v, want one prov-ro/m1 row in=7", models)
	}
	agents, err := ro.AgentUsageSince(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ro.AgentUsageSince: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "chat" {
		t.Fatalf("agents = %+v, want one 'chat' agent", agents)
	}
}

// TestOpenReadOnly_MissingFile verifies a missing database is an error (which
// the HTTP layer renders as empty usage arrays), not a panic.
func TestOpenReadOnly_MissingFile(t *testing.T) {
	if ro, err := OpenReadOnly(filepath.Join(t.TempDir(), "nope.db")); err == nil {
		_ = ro.Close()
		t.Fatal("OpenReadOnly on a missing file succeeded, want error")
	}
}
