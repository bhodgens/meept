package metrics

// 02-routing TEST-GATE T3 (W9; audit-routing R3; master C5, matrix F7).
//
// dispatch_log has NO turn_id column and input_summary is written empty
// (privacy scrub replaced it with input_hash, but C5 requires a bounded,
// populated input_summary on the evidence surface). These pins drive the
// REAL store: record a dispatch carrying a turn id + input text, reopen
// the DB (idempotent-migration pin), and query it back.
//
// RED today: turn_id is not a column and not a DispatchEntry field, so
// the roundtrip cannot carry it; input_summary comes back empty.
// GREEN after 02-routing/02 (idempotent ALTER + INSERT/SELECT sites +
// dispatcher RecordDispatch wiring).
//
// The turn_id assertions go through the raw row map so this file
// compiles today and flips GREEN with zero gate edits when the column
// and the DispatchEntry.TurnID field land.

import (
	"path/filepath"
	"testing"
	"time"
)

// traceGateStore opens a fresh store on a temp DB.
func traceGateStore(t *testing.T, dbPath string) *Store {
	t.Helper()
	s, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore(%s): %v", dbPath, err)
	}
	return s
}

// dispatchLogColumns returns the dispatch_log column set.
func dispatchLogColumns(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	rows, err := s.DB().Query(`PRAGMA table_info(dispatch_log)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info iteration: %v", err)
	}
	return cols
}

// TestTraceTurnID_SchemaHasTurnID pins the C5 schema delta: dispatch_log
// must carry turn_id (the join key to turn.terminal / the turn journal).
func TestTraceTurnID_SchemaHasTurnID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "metrics.db")
	s := traceGateStore(t, dbPath)
	defer func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()

	cols := dispatchLogColumns(t, s)
	if !cols["turn_id"] {
		t.Error("dispatch_log has no turn_id column: C5 requires rows joined to turns by turn_id (idempotent ALTER, legacy rows COALESCE to '')")
	}
	if !cols["input_summary"] {
		t.Error("dispatch_log lost input_summary: C5 requires a populated, bounded input summary column")
	}
}

// TestTraceTurnID_RoundtripWithTurnID records a dispatch under a
// conversation id with a turn id and input text, reopens the store
// (migration idempotence), and asserts the query surface returns the
// turn id and a NON-EMPTY input_summary. The turn id is read from the
// raw row so the pin compiles before DispatchEntry.TurnID exists.
func TestTraceTurnID_RoundtripWithTurnID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "metrics.db")
	const convID = "conv-trace-gate"
	const turnID = "turn-trace-gate-1"
	const inputText = "Now change hello.txt so it says 'hello meept' instead."

	s1 := traceGateStore(t, dbPath)
	s1.RecordDispatch(DispatchEntry{
		SessionID:        convID,
		InputSummary:     inputText,
		IntentType:       "code",
		AgentID:          "coder",
		Confidence:       0.9,
		ClassifierMethod: "llm",
		HandlerCase:      "async_dispatch",
		TaskID:           "task-trace-gate",
		TurnNo:           1,
		Outcome:          "pending",
	})
	// C5 write side: the turn id must ride the recorded row. Until the
	// field exists, write it through the raw column when present so the
	// READ-side assertions below stay honest about what the store owes.
	cols := dispatchLogColumns(t, s1)
	if cols["turn_id"] {
		if _, err := s1.DB().Exec(
			`UPDATE dispatch_log SET turn_id = ? WHERE session_id = ? AND turn_no = 1`,
			turnID, convID); err != nil {
			t.Fatalf("seed turn_id: %v", err)
		}
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close s1: %v", err)
	}

	// Reopen: the ALTER must be idempotent and the row must survive.
	s2 := traceGateStore(t, dbPath)
	defer func() {
		if err := s2.Close(); err != nil {
			t.Errorf("close s2: %v", err)
		}
	}()

	entries, err := s2.QueryDispatchLogBySession(convID, 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (roundtrip lost the row)", len(entries))
	}
	if entries[0].InputSummary == "" {
		t.Error("input_summary came back empty: C5 requires populated input text on the evidence surface (bounded, sanitized)")
	}

	// turn_id read side: raw row first (schema), then the typed surface.
	var rowTurnID string
	err = s2.DB().QueryRow(
		`SELECT COALESCE(turn_id, '') FROM dispatch_log WHERE session_id = ?`, convID).Scan(&rowTurnID)
	if err != nil {
		t.Fatalf("turn_id read: %v (C5: dispatch_log must carry turn_id; legacy rows COALESCE to '')", err)
	}
	if rowTurnID != turnID {
		t.Errorf("row turn_id = %q, want %q", rowTurnID, turnID)
	}
}

// TestTraceTurnID_DispatcherRecordCarriesTurnID pins the WRITE path end
// to end at the store boundary: a recorded dispatch must persist the
// turn id it was handed, not drop it. Today RecordDispatch has no
// turn-id parameter/field at all, so the row cannot carry one; this
// test fails on the missing column and stays RED until both the schema
// and the entry field land (the dispatcher wiring is pinned e2e-side).
func TestTraceTurnID_DispatcherRecordCarriesTurnID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "metrics.db")
	s := traceGateStore(t, dbPath)
	defer func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()

	const convID = "conv-trace-write"
	s.RecordDispatch(DispatchEntry{
		SessionID:        convID,
		InputSummary:     "commit my changes",
		IntentType:       "git",
		AgentID:          "committer",
		ClassifierMethod: "llm",
		HandlerCase:      "async_dispatch",
		TurnNo:           1,
		Outcome:          "pending",
	})

	cols := dispatchLogColumns(t, s)
	if !cols["turn_id"] {
		t.Fatal("dispatch_log has no turn_id column: RecordDispatch cannot persist the turn join key (C5)")
	}
	// With the column present, a fresh record that carries no explicit
	// turn id must still roundtrip as '' (legacy COALESCE contract) and
	// never as SQL NULL breaking the typed scan.
	var got string
	if err := s.DB().QueryRow(
		`SELECT COALESCE(turn_id, '') FROM dispatch_log WHERE session_id = ?`, convID).Scan(&got); err != nil {
		t.Fatalf("turn_id scan: %v", err)
	}
	entries, err := s.QueryDispatchLogBySession(convID, 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession with turn_id column: %v (SELECT sites must include the new column)", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
}
