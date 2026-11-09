package metrics

// Serving-tier attribution tests (llm-serving-tiers TIER-1 residual):
// the llm_calls tier columns exist on fresh AND pre-v10 databases
// (ColumnsMissingGuard migration, registry version 10), RecordLLMCall
// persists the tier triple verbatim, the disabled-path defaults are ""
// / false / -1 (never 0 depth — 0 means "tier-enabled primary call"),
// and the QueryLLMCallUsage read path surfaces the tier counts over
// attributed rows only.

import (
	"path/filepath"
	"testing"
	"time"
)

// TestTierColumns_FreshDB pins the fresh-DB half: the CREATE script
// includes the three llm_tier* columns with the disabled-path defaults,
// so the version-10 guard is a no-op.
func TestTierColumns_FreshDB(t *testing.T) {
	s := newTestStore(t)
	defer func() { _ = s.Close() }()

	var tierNotNull, degradedNotNull, depthNotNull int
	if err := s.db.Get(&tierNotNull,
		`SELECT COUNT(*) FROM pragma_table_info('llm_calls') WHERE name = 'llm_tier' AND "notnull" = 1`); err != nil {
		t.Fatalf("probe llm_tier column: %v", err)
	}
	if err := s.db.Get(&degradedNotNull,
		`SELECT COUNT(*) FROM pragma_table_info('llm_calls') WHERE name = 'llm_tier_degraded' AND "notnull" = 1`); err != nil {
		t.Fatalf("probe llm_tier_degraded column: %v", err)
	}
	if err := s.db.Get(&depthNotNull,
		`SELECT COUNT(*) FROM pragma_table_info('llm_calls') WHERE name = 'llm_tier_depth' AND "notnull" = 1`); err != nil {
		t.Fatalf("probe llm_tier_depth column: %v", err)
	}
	if tierNotNull != 1 || degradedNotNull != 1 || depthNotNull != 1 {
		t.Errorf("tier columns not all NOT NULL: tier=%d degraded=%d depth=%d (want 1/1/1)",
			tierNotNull, degradedNotNull, depthNotNull)
	}

	// Defaults: '' / 0 / -1 (the disabled path; depth 0 is reserved for
	// a tier-enabled primary call, so the backfill sentinel is -1).
	// dflt_value is the DDL literal verbatim ('' for a DEFAULT ''), so
	// the comparison happens in Go, mirroring the trace-columns test.
	var tierDefault, degradedDefault, depthDefault int
	var dfltValues []string
	if err := s.db.Select(&dfltValues,
		`SELECT COALESCE(dflt_value, '<null>') FROM pragma_table_info('llm_calls') WHERE name IN ('llm_tier','llm_tier_degraded','llm_tier_depth')`); err != nil {
		t.Fatalf("probe tier defaults: %v", err)
	}
	for _, d := range dfltValues {
		switch d {
		case "''": // sqlite reports the DEFAULT '' literal verbatim
			tierDefault++
		case "0":
			degradedDefault++
		case "-1":
			depthDefault++
		}
	}
	if tierDefault != 1 || degradedDefault != 1 || depthDefault != 1 {
		t.Errorf("tier column defaults wrong: tier=''=%d, degraded=0=%d, depth=-1=%d (want 1/1/1)",
			tierDefault, degradedDefault, depthDefault)
	}

	// Registry records version 10 (the guard no-ops on the inline columns).
	var v int
	if err := s.db.Get(&v, `PRAGMA user_version`); err != nil {
		t.Fatal(err)
	}
	if v < 10 {
		t.Errorf("PRAGMA user_version = %d, want >= 10", v)
	}
}

// TestTierColumns_PreV10DB pins the migration half: a database at the
// PRE-v10 shape (v9 posture: ttft_ms present, tier columns absent) gains
// the columns on open, and existing rows keep the disabled-path defaults.
func TestTierColumns_PreV10DB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "metrics.db")

	legacy, err := NewStore(&StoreConfig{DatabasePath: dbPath, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewStore(legacy): %v", err)
	}
	// Rebuild llm_calls at the v9 posture (ttft_ms present, tier columns
	// absent) and stamp user_version = 9 so ONLY the v10 step runs.
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
		ttft_ms         INTEGER NOT NULL DEFAULT -1,
		duration        REAL NOT NULL DEFAULT 0,
		trace_id        TEXT NOT NULL DEFAULT '',
		span_id         TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatalf("recreate v9 llm_calls: %v", err)
	}
	// Seed rows so the DEFAULT backfill is observable on real data.
	if _, err := legacy.db.Exec(`INSERT INTO llm_calls
		(provider, model_id, latency_ms) VALUES ('prov-a', 'legacy-model', 42)`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if _, err := legacy.db.Exec(`PRAGMA user_version = 9`); err != nil {
		t.Fatalf("stamp user_version 9: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	upgraded, err := NewStore(&StoreConfig{DatabasePath: dbPath, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewStore(upgrade): %v", err)
	}
	defer func() { _ = upgraded.Close() }()

	// The columns exist and the legacy row carries the disabled-path
	// defaults verbatim ('' / 0 / -1).
	var tier string
	var degraded bool
	var depth int
	if err := upgraded.db.QueryRow(
		`SELECT llm_tier, llm_tier_degraded, llm_tier_depth FROM llm_calls WHERE model_id = 'legacy-model'`).Scan(&tier, &degraded, &depth); err != nil {
		t.Fatalf("legacy row tier triple: %v", err)
	}
	if tier != "" || degraded || depth != -1 {
		t.Errorf("legacy row tier triple = (%q, %v, %d), want ('', false, -1)", tier, degraded, depth)
	}

	// Registry moved to 10 and a reopen is a no-op (idempotent guard).
	var v int
	if err := upgraded.db.Get(&v, `PRAGMA user_version`); err != nil {
		t.Fatal(err)
	}
	if v < 10 {
		t.Errorf("PRAGMA user_version = %d, want >= 10 after upgrade", v)
	}
}

// TestTierRecordPersistAndReadPath pins the record + read halves:
// RecordLLMCall persists the tier triple verbatim (attributed, degraded,
// and disabled-path rows), a zero-value record lands the disabled-path
// defaults ("" tier, -1 depth — never 0), and QueryLLMCallUsage counts
// tier_calls / tier_degraded_calls over ATTRIBUTED rows only.
func TestTierRecordPersistAndReadPath(t *testing.T) {
	s := newTestStore(t)
	defer func() { _ = s.Close() }()

	now := time.Now()
	// Attributed primary call (depth 0), attributed degraded call (depth 2),
	// and a disabled-path call (zero-value record — tier feature off).
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1",
		TokensSent: 10, TokensRecv: 5, LatencyMs: 100,
		LLMTier: "prov-a/m1", LLMTierDepth: 0})
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1",
		TokensSent: 10, TokensRecv: 5, LatencyMs: 110,
		LLMTier: "prov-a/fallback", LLMTierDegraded: true, LLMTierDepth: 2})
	s.RecordLLMCall(LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1",
		TokensSent: 10, TokensRecv: 5, LatencyMs: 120})

	// Attributed rows persist verbatim.
	var primaryDepth, degradedDepth int
	var degradedFlag bool
	if err := s.db.Get(&primaryDepth,
		`SELECT llm_tier_depth FROM llm_calls WHERE llm_tier = 'prov-a/m1'`); err != nil {
		t.Fatalf("primary row: %v", err)
	}
	if primaryDepth != 0 {
		t.Errorf("primary row llm_tier_depth = %d, want 0 (0 ≠ the -1 disabled sentinel)", primaryDepth)
	}
	if err := s.db.QueryRow(
		`SELECT llm_tier_degraded, llm_tier_depth FROM llm_calls WHERE llm_tier = 'prov-a/fallback'`).Scan(&degradedFlag, &degradedDepth); err != nil {
		t.Fatalf("degraded row: %v", err)
	}
	if !degradedFlag || degradedDepth != 2 {
		t.Errorf("degraded row = (%v, %d), want (true, 2)", degradedFlag, degradedDepth)
	}

	// The zero-value record keeps the disabled-path defaults: "" tier and
	// depth -1 — a metadata-less response never fabricates depth 0.
	var emptyTier string
	var emptyDepth int
	if err := s.db.QueryRow(
		`SELECT llm_tier, llm_tier_depth FROM llm_calls WHERE llm_tier = ''`).Scan(&emptyTier, &emptyDepth); err != nil {
		t.Fatalf("disabled-path row: %v", err)
	}
	if emptyTier != "" || emptyDepth != -1 {
		t.Errorf("disabled-path row = (%q, %d), want ('', -1)", emptyTier, emptyDepth)
	}

	// Read path: tier_calls counts attributed rows only (2 of 3),
	// tier_degraded_calls counts degraded rows only (1).
	rows, err := s.QueryLLMCallUsage(now.Add(-time.Hour), now.Add(time.Hour), false)
	if err != nil {
		t.Fatalf("QueryLLMCallUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	if rows[0].Calls != 3 {
		t.Errorf("Calls = %d, want 3", rows[0].Calls)
	}
	if rows[0].TierCalls != 2 {
		t.Errorf("TierCalls = %d, want 2 (disabled-path row excluded)", rows[0].TierCalls)
	}
	if rows[0].TierDegradedCalls != 1 {
		t.Errorf("TierDegradedCalls = %d, want 1", rows[0].TierDegradedCalls)
	}
}
