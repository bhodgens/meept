package metrics

// TTFT instrumentation tests (llm-serving-tiers leaf 04, tasks T1/T3):
// the llm_calls.ttft_ms column exists on fresh AND pre-registry databases
// (columnMissingGuard migration, registry version 9), RecordLLMCall
// persists the -1 "not measured" sentinel verbatim, and the metrics read
// paths (QueryLLMCallUsage / QueryModelUsage) surface ttft averages over
// measured rows only.

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestTTFTColumn_FreshDB pins the fresh-DB half of T1: the CREATE script
// includes ttft_ms (NOT NULL DEFAULT -1) so the version-9 guard is a no-op.
func TestTTFTColumn_FreshDB(t *testing.T) {
	s := newTestStore(t)
	defer func() { _ = s.Close() }()

	var notNull, hasDefault int
	if err := s.db.Get(&notNull,
		`SELECT COUNT(*) FROM pragma_table_info('llm_calls') WHERE name = 'ttft_ms' AND "notnull" = 1`); err != nil {
		t.Fatalf("probe ttft_ms column: %v", err)
	}
	if err := s.db.Get(&hasDefault,
		`SELECT COUNT(*) FROM pragma_table_info('llm_calls') WHERE name = 'ttft_ms' AND dflt_value = '-1'`); err != nil {
		t.Fatalf("probe ttft_ms default: %v", err)
	}
	if notNull != 1 || hasDefault != 1 {
		t.Errorf("ttft_ms column shape wrong: notnull=%d, default='-1'=%d (want 1/1)", notNull, hasDefault)
	}

	// Registry records version 9 (the guard no-ops on the inline column).
	var v int
	if err := s.db.Get(&v, `PRAGMA user_version`); err != nil {
		t.Fatal(err)
	}
	if v < 9 {
		t.Errorf("PRAGMA user_version = %d, want >= 9", v)
	}
}

// TestTTFTColumn_PreRegistryDB pins the migration half of T1: a database
// built from the PREVIOUS schema (no ttft_ms, user_version reset to 0,
// exactly the pre-registry posture) gains the column on open.
func TestTTFTColumn_PreRegistryDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "metrics.db")

	legacy, err := NewStore(&StoreConfig{DatabasePath: dbPath, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewStore(legacy): %v", err)
	}
	// Rebuild llm_calls at the pre-ttft shape (registry version 8 posture:
	// trace columns present, ttft_ms absent).
	if _, err := legacy.db.Exec(`DROP TABLE llm_calls`); err != nil {
		t.Fatalf("drop llm_calls: %v", err)
	}
	if _, err := legacy.db.Exec(`CREATE TABLE llm_calls (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
		provider        TEXT NOT NULL DEFAULT '',
		model_id        TEXT NOT NULL DEFAULT '',
		agent_id        TEXT NOT NULL DEFAULT '',
		session_id      TEXT NOT NULL DEFAULT '',
		tokens_sent     INTEGER NOT NULL DEFAULT 0,
		tokens_received INTEGER NOT NULL DEFAULT 0,
		tokens_cached   INTEGER NOT NULL DEFAULT 0,
		reasoning_tokens INTEGER NOT NULL DEFAULT 0,
		cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
		error           INTEGER NOT NULL DEFAULT 0,
		error_message   TEXT NOT NULL DEFAULT '',
		latency_ms      INTEGER NOT NULL DEFAULT 0,
		duration        REAL NOT NULL DEFAULT 0,
		trace_id        TEXT NOT NULL DEFAULT '',
		span_id         TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatalf("recreate legacy llm_calls: %v", err)
	}
	// Seed a legacy row so the ALTER's DEFAULT -1 backfill is observable.
	if _, err := legacy.db.Exec(`INSERT INTO llm_calls
		(provider, model_id, latency_ms) VALUES ('prov-a', 'legacy-model', 42)`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	// Pre-registry posture: schema is old but the version stamp is unknown.
	if _, err := legacy.db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatalf("reset user_version: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	upgraded, err := NewStore(&StoreConfig{DatabasePath: dbPath, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewStore(upgrade): %v", err)
	}
	defer func() { _ = upgraded.Close() }()

	// Column exists with the -1 sentinel backfilled onto legacy rows.
	var backfill int64
	if err := upgraded.db.Get(&backfill,
		`SELECT ttft_ms FROM llm_calls WHERE model_id = 'legacy-model'`); err != nil {
		t.Fatalf("legacy row ttft_ms: %v", err)
	}
	if backfill != -1 {
		t.Errorf("legacy row ttft_ms = %d, want -1 (DEFAULT backfill)", backfill)
	}
}

// TestTTFTRecordPersistAndReadPath pins T3's store half: RecordLLMCall
// persists TTFTMS verbatim (measured and sentinel rows), and BOTH read
// paths average measured rows only -- the -1 sentinel never drags an
// average toward zero, and an all-unmeasured window reports -1.
func TestTTFTRecordPersistAndReadPath(t *testing.T) {
	s := newTestStore(t)
	defer func() { _ = s.Close() }()

	now := time.Now()
	// One measured streaming call, one unmeasured (non-streaming/cache
	// hit/error) call, same provider+model so they land in ONE group.
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1",
		TokensSent: 10, TokensRecv: 5, LatencyMs: 900, TTFTMS: 120})
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1",
		TokensSent: 10, TokensRecv: 5, LatencyMs: 800, TTFTMS: -1})

	var storedTTFT int64
	if err := s.db.Get(&storedTTFT,
		`SELECT ttft_ms FROM llm_calls WHERE model_id = 'm1' AND ttft_ms = 120`); err != nil {
		t.Fatalf("measured row persisted: %v", err)
	}
	var sentinelRows int
	if err := s.db.Get(&sentinelRows,
		`SELECT COUNT(*) FROM llm_calls WHERE model_id = 'm1' AND ttft_ms = -1`); err != nil {
		t.Fatalf("sentinel row count: %v", err)
	}
	if sentinelRows != 1 {
		t.Errorf("sentinel rows = %d, want 1", sentinelRows)
	}

	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)

	// QueryLLMCallUsage: avg over MEASURED rows only (120), not (120-1)/2.
	rows, err := s.QueryLLMCallUsage(from, to, false)
	if err != nil {
		t.Fatalf("QueryLLMCallUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	if rows[0].AvgTTFTMs != 120 {
		t.Errorf("AvgTTFTMs = %v, want 120 (sentinel excluded from the average)", rows[0].AvgTTFTMs)
	}

	// ModelUsage (the live-metrics HTTP read path) surfaces it too.
	models, err := s.ModelUsageSince(context.Background(), from)
	if err != nil {
		t.Fatalf("ModelUsageSince: %v", err)
	}
	if len(models) != 1 || models[0].AvgTTFTMs != 120 {
		t.Fatalf("model usage = %+v, want one m1 row with AvgTTFTMs 120", models)
	}

	// A window with NO measured call reports -1 (the no-measurements
	// sentinel), never 0.
	s2 := newTestStore(t)
	defer func() { _ = s2.Close() }()
	s2.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-b", ModelID: "m2",
		TokensSent: 1, TokensRecv: 1, LatencyMs: 50, TTFTMS: -1})
	rows2, err := s2.QueryLLMCallUsage(from, to, false)
	if err != nil {
		t.Fatalf("QueryLLMCallUsage (unmeasured): %v", err)
	}
	if len(rows2) != 1 || rows2[0].AvgTTFTMs != -1 {
		t.Errorf("unmeasured AvgTTFTMs = %+v, want -1", rows2)
	}
}
