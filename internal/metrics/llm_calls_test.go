package metrics

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(&StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour, // disable background flush; llm_calls writes are direct
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestRecordLLMCall_WindowedAggregation inserts calls across providers and
// agents, then verifies the windowed query returns correct sums per
// provider and per provider+agent.
func TestRecordLLMCall_WindowedAggregation(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	// provider-a / coder: 2 calls, sent 100+200, recv 10+20, cached 50+0
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1", AgentID: "coder",
		TokensSent: 100, TokensRecv: 10, TokensCached: 50, LatencyMs: 100})
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1", AgentID: "coder",
		TokensSent: 200, TokensRecv: 20, TokensCached: 0, LatencyMs: 300})
	// provider-a / researcher: 1 call
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1", AgentID: "researcher",
		TokensSent: 500, TokensRecv: 5, LatencyMs: 200})
	// provider-b / coder: 1 call + 1 error call
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-b", ModelID: "m2", AgentID: "coder",
		TokensSent: 1000, TokensRecv: 100, LatencyMs: 50})
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-b", ModelID: "m2", AgentID: "coder",
		IsError: true, ErrorMessage: "boom", LatencyMs: 5})
	// outside the window: must be excluded
	s.RecordLLMCall(LLMCallRecord{Timestamp: now.Add(-48 * time.Hour), Provider: "prov-a", ModelID: "m1", AgentID: "coder",
		TokensSent: 999999, TokensRecv: 999999})

	// Per-provider window (6 in-window calls)
	rows, err := s.QueryLLMCallUsage(now.Add(-time.Hour), now.Add(time.Hour), false)
	if err != nil {
		t.Fatalf("QueryLLMCallUsage: %v", err)
	}
	byProv := map[string]LLMUsageRow{}
	for _, r := range rows {
		byProv[r.Provider] = r
	}
	if len(rows) != 2 {
		t.Fatalf("got %d provider rows, want 2: %+v", len(rows), rows)
	}
	a := byProv["prov-a"]
	if a.Calls != 3 || a.TokensSent != 800 || a.TokensRecv != 35 || a.TokensCached != 50 || a.Errors != 0 {
		t.Errorf("prov-a: got %+v", a)
	}
	b := byProv["prov-b"]
	if b.Calls != 2 || b.TokensSent != 1000 || b.TokensRecv != 100 || b.Errors != 1 {
		t.Errorf("prov-b: got %+v", b)
	}

	// Per provider+agent window
	rows, err = s.QueryLLMCallUsage(now.Add(-time.Hour), now.Add(time.Hour), true)
	if err != nil {
		t.Fatalf("QueryLLMCallUsage(grouped): %v", err)
	}
	byPair := map[string]LLMUsageRow{}
	for _, r := range rows {
		byPair[r.Provider+"/"+r.AgentID] = r
	}
	if len(rows) != 3 {
		t.Fatalf("got %d grouped rows, want 3", len(rows))
	}
	if r := byPair["prov-a/coder"]; r.Calls != 2 || r.TokensSent != 300 || r.TokensRecv != 30 {
		t.Errorf("prov-a/coder: got %+v", r)
	}
	if r := byPair["prov-a/researcher"]; r.Calls != 1 || r.TokensSent != 500 {
		t.Errorf("prov-a/researcher: got %+v", r)
	}
	if r := byPair["prov-b/coder"]; r.Calls != 2 || r.Errors != 1 {
		t.Errorf("prov-b/coder: got %+v", r)
	}
}

// TestRecordLLMCall_RollupUpdatesModelPerformance verifies the
// model_performance UPSERT accumulates correctly per hour bucket and agent.
func TestRecordLLMCall_RollupUpdatesModelPerformance(t *testing.T) {
	s := newTestStore(t)
	// Anchor inside one hour bucket: a raw time.Now() within minutes of
	// the hour boundary puts the second call (now+2min) in the NEXT
	// bucket, splitting the rollup across two rows (CI ran at 20:59 UTC).
	now := time.Now().Truncate(time.Hour).Add(time.Minute)

	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1", AgentID: "coder",
		TokensSent: 100, TokensRecv: 10, LatencyMs: 100})
	s.RecordLLMCall(LLMCallRecord{Timestamp: now.Add(2 * time.Minute), Provider: "prov-a", ModelID: "m1", AgentID: "coder",
		TokensSent: 300, TokensRecv: 30, LatencyMs: 200})

	var rec ModelPerformanceRecord
	err := s.db.Get(&rec,
		`SELECT model_id, provider, agent_id, total_requests, total_errors, avg_latency_ms, avg_tokens_in, avg_tokens_out
		 FROM model_performance WHERE model_id='m1' AND provider='prov-a' AND agent_id='coder'`)
	if err != nil {
		t.Fatalf("rollup row missing: %v", err)
	}
	if rec.TotalRequests != 2 || rec.TotalErrors != 0 {
		t.Errorf("totals: got %+v", rec)
	}
	if rec.AvgLatencyMs != 150 || rec.AvgTokensIn != 200 || rec.AvgTokensOut != 20 {
		t.Errorf("averages: got %+v", rec)
	}
}

// TestModelPerformanceMigration_OldDBUpgrades creates a database with the
// legacy model_performance schema (no agent_id, old UNIQUE constraint),
// closes it, reopens through the Store (which runs initSchema + migration),
// and verifies agent-scoped rollups work.
func TestModelPerformanceMigration_OldDBUpgrades(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "metrics.db")

	// Create a legacy-schema store, then hand it a legacy rollup row.
	legacy, err := NewStore(&StoreConfig{DatabasePath: dbPath, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewStore(legacy): %v", err)
	}
	// Drop and recreate the table with the pre-migration shape.
	if _, err := legacy.db.Exec(`DROP TABLE model_performance`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := legacy.db.Exec(`CREATE TABLE model_performance (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		model_id        TEXT NOT NULL,
		provider        TEXT NOT NULL DEFAULT '',
		total_requests  INTEGER NOT NULL DEFAULT 0,
		total_errors    INTEGER NOT NULL DEFAULT 0,
		avg_latency_ms  REAL NOT NULL DEFAULT 0,
		avg_tokens_in   REAL NOT NULL DEFAULT 0,
		avg_tokens_out  REAL NOT NULL DEFAULT 0,
		period_start    TEXT NOT NULL,
		period_end      TEXT NOT NULL,
		updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
		UNIQUE(model_id, provider, period_start)
	)`); err != nil {
		t.Fatalf("recreate legacy: %v", err)
	}
	if _, err := legacy.db.Exec(`INSERT INTO model_performance
		(model_id, provider, total_requests, total_errors, period_start, period_end)
		VALUES ('old-model', 'old-prov', 7, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:59:59Z')`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	// Legacy schema also lacks llm_calls; drop it to simulate the oldest DBs.
	if _, err := legacy.db.Exec(`DROP TABLE llm_calls`); err != nil {
		t.Fatalf("drop llm_calls: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	// Reopen: initSchema must tolerate the old table (CREATE IF NOT EXISTS
	// is a no-op) and the migration must add agent_id back.
	upgraded, err := NewStore(&StoreConfig{DatabasePath: dbPath, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewStore(upgrade): %v", err)
	}
	defer func() { _ = upgraded.Close() }()

	// agent_id column exists and legacy data survived.
	var agentID string
	if err := upgraded.db.QueryRow(
		`SELECT agent_id FROM model_performance WHERE model_id='old-model'`).Scan(&agentID); err != nil {
		t.Fatalf("legacy row survived: %v", err)
	}
	if agentID != "" {
		t.Errorf("legacy agent_id = %q, want empty", agentID)
	}

	// New agent-scoped rollups work on the upgraded schema.
	now := time.Now()
	upgraded.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1", AgentID: "coder",
		TokensSent: 42, TokensRecv: 4, LatencyMs: 10})
	var totalReqs int
	if err := upgraded.db.QueryRow(
		`SELECT total_requests FROM model_performance WHERE model_id='m1' AND agent_id='coder'`).Scan(&totalReqs); err != nil {
		t.Fatalf("agent rollup on upgraded DB: %v", err)
	}
	if totalReqs != 1 {
		t.Errorf("total_requests = %d, want 1", totalReqs)
	}
}
