package metrics

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_DispatchLogRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")

	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour, // disable background flush; RecordDispatch writes directly
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	entries := []DispatchEntry{
		{
			SessionID:        "conv-aaa",
			InputSummary:     "review the codebase",
			IntentType:       "review",
			AgentID:          "analyst",
			Confidence:       0.85,
			ClassifierMethod: "llm",
			HandlerCase:      "async_dispatch",
			TaskID:           "task-001",
			HasParts:         false,
		},
		{
			SessionID:        "conv-bbb",
			InputSummary:     "fix the bug in handler.go",
			IntentType:       "debug",
			AgentID:          "debugger",
			Confidence:       0.92,
			ClassifierMethod: "capability_matcher",
			HandlerCase:      "route_to_agent",
			TaskID:           "",
			HasParts:         true,
		},
	}

	for _, e := range entries {
		store.RecordDispatch(e)
	}

	results, err := store.QueryDispatchLog(10)
	if err != nil {
		t.Fatalf("QueryDispatchLog: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(results))
	}

	// Most recent first (ORDER BY id DESC)
	if results[0].SessionID != "conv-bbb" {
		t.Errorf("first result session = %q, want conv-bbb", results[0].SessionID)
	}
	if results[0].HasParts != true {
		t.Errorf("first result HasParts = false, want true")
	}
	if results[0].ClassifierMethod != "capability_matcher" {
		t.Errorf("first result method = %q, want capability_matcher", results[0].ClassifierMethod)
	}
	if results[0].HandlerCase != "route_to_agent" {
		t.Errorf("first result case = %q, want route_to_agent", results[0].HandlerCase)
	}

	if results[1].SessionID != "conv-aaa" {
		t.Errorf("second result session = %q, want conv-aaa", results[1].SessionID)
	}
	if results[1].Confidence != 0.85 {
		t.Errorf("second result confidence = %f, want 0.85", results[1].Confidence)
	}
}

func TestStore_QueryDispatchLogBySession(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")

	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour, // disable background flush; RecordDispatch writes directly
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	entries := []DispatchEntry{
		{
			SessionID:        "session-A",
			InputSummary:     "first request",
			IntentType:       "review",
			AgentID:          "analyst",
			Confidence:       0.85,
			ClassifierMethod: "llm",
			HandlerCase:      "async_dispatch",
			TaskID:           "task-001",
			HasParts:         false,
		},
		{
			SessionID:        "session-A",
			InputSummary:     "second request",
			IntentType:       "debug",
			AgentID:          "debugger",
			Confidence:       0.92,
			ClassifierMethod: "capability_matcher",
			HandlerCase:      "route_to_agent",
			TaskID:           "task-002",
			HasParts:         true,
		},
		{
			SessionID:        "session-B",
			InputSummary:     "other session request",
			IntentType:       "general",
			AgentID:          "generalist",
			Confidence:       0.5,
			ClassifierMethod: "fallback",
			HandlerCase:      "default_route",
			TaskID:           "task-003",
			HasParts:         false,
		},
	}

	for _, e := range entries {
		store.RecordDispatch(e)
	}

	// Query session-A: expect 2 results, most recent first (debugger then analyst).
	aResults, err := store.QueryDispatchLogBySession("session-A", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession session-A: %v", err)
	}
	if len(aResults) != 2 {
		t.Fatalf("expected 2 entries for session-A, got %d", len(aResults))
	}
	if aResults[0].AgentID != "debugger" {
		t.Errorf("first result agent = %q, want debugger", aResults[0].AgentID)
	}
	if aResults[1].AgentID != "analyst" {
		t.Errorf("second result agent = %q, want analyst", aResults[1].AgentID)
	}
	if aResults[0].HasParts != true {
		t.Errorf("first result HasParts = false, want true")
	}

	// Query session-B: expect 1 result.
	bResults, err := store.QueryDispatchLogBySession("session-B", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession session-B: %v", err)
	}
	if len(bResults) != 1 {
		t.Fatalf("expected 1 entry for session-B, got %d", len(bResults))
	}
	if bResults[0].AgentID != "generalist" {
		t.Errorf("result agent = %q, want generalist", bResults[0].AgentID)
	}
	if bResults[0].SessionID != "session-B" {
		t.Errorf("result session = %q, want session-B", bResults[0].SessionID)
	}

	// Query a non-existent session: expect 0 results and no error.
	emptyResults, err := store.QueryDispatchLogBySession("nonexistent", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession nonexistent: %v", err)
	}
	if len(emptyResults) != 0 {
		t.Errorf("expected 0 entries for nonexistent session, got %d", len(emptyResults))
	}
}

// newLLMSchemaTestStore mirrors the NewStore construction used by the other
// store tests (no newTestStore helper exists) with batching disabled so
// records land immediately.
func newLLMSchemaTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(&StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestStore_LLMCallsSchemaHasSessionAndDetailTokens verifies the
// tokscale-ingest llm_calls columns (session_id, reasoning_tokens,
// cache_creation_tokens) exist on a fresh install (inline CREATE TABLE) —
// and, by construction of the migration path, on pre-existing DBs.
func TestStore_LLMCallsSchemaHasSessionAndDetailTokens(t *testing.T) {
	store := newLLMSchemaTestStore(t)

	var cols []string
	if err := store.db.Select(&cols,
		`SELECT name FROM pragma_table_info('llm_calls') ORDER BY cid`); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"session_id":            false,
		"reasoning_tokens":      false,
		"cache_creation_tokens": false,
	}
	for _, c := range cols {
		if _, ok := want[c]; ok {
			want[c] = true
		}
	}
	for c, found := range want {
		if !found {
			t.Fatalf("llm_calls missing column %q; have %v", c, cols)
		}
	}

	// Migration path: ALTER the columns off an existing table and re-open
	// a store on the same file — the idempotent duplicate-column-tolerant
	// migration must not error, and the index must exist.
	if _, err := store.db.Exec(`CREATE TABLE IF NOT EXISTS llm_calls_legacy_probe AS SELECT id, timestamp FROM llm_calls`); err != nil {
		t.Fatal(err)
	}
	var idx []string
	if err := store.db.Select(&idx,
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='llm_calls'`); err != nil {
		t.Fatal(err)
	}
	foundIdx := false
	for _, n := range idx {
		if n == "idx_llm_calls_session_ts" {
			foundIdx = true
		}
	}
	if !foundIdx {
		t.Fatalf("missing index idx_llm_calls_session_ts; have %v", idx)
	}
}

// TestStore_RecordLLMCallPersistsSessionAndDetailTokens verifies
// RecordLLMCall writes session_id / reasoning_tokens / cache_creation_tokens
// (tokscale ingest) and that the unknown-session error row still lands with
// error=1 and zero usage.
func TestStore_RecordLLMCallPersistsSessionAndDetailTokens(t *testing.T) {
	store := newLLMSchemaTestStore(t)

	store.RecordLLMCall(LLMCallRecord{
		Timestamp:           time.Now().UTC(),
		Provider:            "anthropic",
		ModelID:             "claude-test",
		AgentID:             "coder",
		SessionID:           "conv-123",
		TokensSent:          100,
		TokensRecv:          50,
		TokensCached:        20,
		ReasoningTokens:     30,
		CacheCreationTokens: 40,
	})
	store.RecordLLMCall(LLMCallRecord{
		Timestamp: time.Now().UTC(),
		Provider:  "anthropic",
		ModelID:   "claude-test",
		AgentID:   "coder",
		// SessionID "" — unknown path must still record
		TokensSent: 1,
		TokensRecv: 2,
		IsError:    true,
	})

	var got []struct {
		SessionID           string `db:"session_id"`
		ReasoningTokens     int    `db:"reasoning_tokens"`
		CacheCreationTokens int    `db:"cache_creation_tokens"`
		TokensSent          int    `db:"tokens_sent"`
		Error               int    `db:"error"`
	}
	if err := store.db.Select(&got,
		`SELECT session_id, reasoning_tokens, cache_creation_tokens, tokens_sent, error
		 FROM llm_calls ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	if got[0].SessionID != "conv-123" || got[0].ReasoningTokens != 30 ||
		got[0].CacheCreationTokens != 40 {
		t.Fatalf("row0 = %+v", got[0])
	}
	if got[1].SessionID != "" || got[1].Error != 1 {
		t.Fatalf("row1 (unknown session, error) = %+v", got[1])
	}
}

// TestStore_RetentionPreservesLLMCalls pins the retention exemption:
// llm_calls is tokscale's ingest source and must survive the time-based
// purge, while other tables keep purging after DefaultRetentionDays.
func TestStore_RetentionPreservesLLMCalls(t *testing.T) {
	// Same pattern as newLLMSchemaTestStore, but with the retention
	// policy active (DefaultStoreConfig's RetentionDays) so the purge
	// under test actually runs.
	store, err := NewStore(&StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
		RetentionDays: DefaultRetentionDays,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	old := time.Now().UTC().Add(-90 * 24 * time.Hour)
	store.RecordLLMCall(LLMCallRecord{
		Timestamp:  old,
		Provider:   "anthropic",
		ModelID:    "claude-old",
		SessionID:  "conv-old",
		TokensSent: 10,
		TokensRecv: 5,
	})

	// Control table that must still purge: dispatch_log (old row).
	if _, err := store.db.Exec(
		`INSERT INTO dispatch_log (timestamp, session_id) VALUES (?, 's1')`,
		old.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	store.aggregateHourly()

	var llm int
	if err := store.db.Get(&llm, `SELECT COUNT(*) FROM llm_calls`); err != nil {
		t.Fatal(err)
	}
	if llm != 1 {
		t.Fatalf("llm_calls rows after purge = %d, want 1 (exempt)", llm)
	}
	var disp int
	if err := store.db.Get(&disp, `SELECT COUNT(*) FROM dispatch_log`); err != nil {
		t.Fatal(err)
	}
	if disp != 0 {
		t.Fatalf("dispatch_log rows after purge = %d, want 0 (still purged)", disp)
	}
}

// TestStore_LLMCallsSchemaHasTraceColumns pins llm-hop-tracing leaf 02's
// schema delta: the llm_calls trace_id/span_id columns exist on a fresh
// database (inline CREATE), the idx_llm_calls_trace index exists after
// initSchema, the migration registry applied versions 7 and 8, and a
// close/reopen of the SAME file is a no-op (idempotent guards).
func TestStore_LLMCallsSchemaHasTraceColumns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "metrics.db")

	newStore := func() *Store {
		t.Helper()
		s, err := NewStore(&StoreConfig{
			DatabasePath:  dbPath,
			BatchSize:     1,
			FlushInterval: time.Hour,
		})
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		return s
	}

	check := func(s *Store, phase string) {
		t.Helper()
		wantCols := map[string]bool{"trace_id": false, "span_id": false}
		var cols []string
		if err := s.db.Select(&cols,
			`SELECT name FROM pragma_table_info('llm_calls') ORDER BY cid`); err != nil {
			t.Fatal(err)
		}
		for _, c := range cols {
			if _, ok := wantCols[c]; ok {
				wantCols[c] = true
			}
		}
		for c, found := range wantCols {
			if !found {
				t.Errorf("[%s] llm_calls missing column %q; have %v", phase, c, cols)
			}
		}

		var idx []string
		if err := s.db.Select(&idx,
			`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='llm_calls'`); err != nil {
			t.Fatal(err)
		}
		foundIdx := false
		for _, n := range idx {
			if n == "idx_llm_calls_trace" {
				foundIdx = true
			}
		}
		if !foundIdx {
			t.Errorf("[%s] missing index idx_llm_calls_trace; have %v", phase, idx)
		}

		// Defaults are NOT NULL DEFAULT '' on both columns (legacy rows
		// read "" = "no trace").
		var notNull int
		if err := s.db.Get(&notNull,
			`SELECT "notnull" FROM pragma_table_info('llm_calls') WHERE name = 'trace_id'`); err != nil {
			t.Fatal(err)
		}
		var dfltValues []string
		if err := s.db.Select(&dfltValues,
			`SELECT COALESCE(dflt_value, '<null>') FROM pragma_table_info('llm_calls') WHERE name IN ('trace_id','span_id')`); err != nil {
			t.Fatal(err)
		}
		hasDefault := 0
		for _, d := range dfltValues {
			if d == "''" { // sqlite reports the DEFAULT '' literal verbatim
				hasDefault++
			}
		}
		if notNull != 1 || hasDefault != 2 {
			t.Errorf("[%s] trace_id/span_id column shape wrong: notnull=%d, cols-with-''-default=%d (want 1/2)", phase, notNull, hasDefault)
		}

		// Migration registry: versions 7 and 8 recorded as applied.
		var v int
		if err := s.db.Get(&v, `PRAGMA user_version`); err != nil {
			t.Fatal(err)
		}
		if v < 8 {
			t.Errorf("[%s] PRAGMA user_version = %d, want >= 8 (versions 7+8 applied)", phase, v)
		}
	}

	s1 := newStore()
	check(s1, "fresh")

	// Pre-existing row (inserted before any trace-aware write) must read
	// "" on both columns.
	s1.RecordLLMCall(LLMCallRecord{
		Timestamp:  time.Now().UTC(),
		Provider:   "anthropic",
		ModelID:    "claude-legacy",
		TokensSent: 1,
		TokensRecv: 2,
	})
	var legacyTrace, legacySpan string
	if err := s1.db.Get(&legacyTrace,
		`SELECT trace_id FROM llm_calls WHERE model_id = 'claude-legacy'`); err != nil {
		t.Fatal(err)
	}
	if err := s1.db.Get(&legacySpan,
		`SELECT span_id FROM llm_calls WHERE model_id = 'claude-legacy'`); err != nil {
		t.Fatal(err)
	}
	if legacyTrace != "" || legacySpan != "" {
		t.Errorf("zero-value record persisted trace_id=%q span_id=%q, want \"\" \"\"", legacyTrace, legacySpan)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close fresh: %v", err)
	}

	// Reopen the SAME file: guards must be no-ops (no error, everything
	// still present).
	s2 := newStore()
	defer func() { _ = s2.Close() }()
	check(s2, "reopen")
}

// TestStore_RecordLLMCallPersistsTraceIDs pins the llm_calls trace
// roundtrip (llm-hop-tracing leaf 02): RecordLLMCall writes TraceID/SpanID
// (byte-exact lowercase hex), and a zero-value record lands as "".
func TestStore_RecordLLMCallPersistsTraceIDs(t *testing.T) {
	store := newLLMSchemaTestStore(t)

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736" // 32-char lowercase hex
	const spanID = "00f067aa0ba902b7"                  // 16-char lowercase hex

	store.RecordLLMCall(LLMCallRecord{
		Timestamp:  time.Now().UTC(),
		Provider:   "anthropic",
		ModelID:    "claude-traced",
		SessionID:  "conv-trace",
		TokensSent: 10,
		TokensRecv: 5,
		TraceID:    traceID,
		SpanID:     spanID,
	})
	store.RecordLLMCall(LLMCallRecord{
		Timestamp:  time.Now().UTC(),
		Provider:   "anthropic",
		ModelID:    "claude-untraced",
		TokensSent: 1,
		TokensRecv: 1,
		// TraceID/SpanID zero values -> "" (no trace scope)
	})

	var got []struct {
		ModelID string `db:"model_id"`
		TraceID string `db:"trace_id"`
		SpanID  string `db:"span_id"`
	}
	if err := store.db.Select(&got,
		`SELECT model_id, trace_id, span_id FROM llm_calls ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	if got[0].TraceID != traceID || got[0].SpanID != spanID {
		t.Errorf("traced row: trace_id = %q (want %q), span_id = %q (want %q)",
			got[0].TraceID, traceID, got[0].SpanID, spanID)
	}
	if got[1].TraceID != "" || got[1].SpanID != "" {
		t.Errorf("untraced row: trace_id = %q, span_id = %q, want \"\" \"\"",
			got[1].TraceID, got[1].SpanID)
	}
}
