package metrics

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TestDispatchLogMigration_IdempotentReopen verifies the S1 schema delta on
// a fresh DB and on reopen of the same file (the tolerate-duplicate-column
// ALTERs must be no-ops the second time).
func TestDispatchLogMigration_IdempotentReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "metrics.db")

	s1, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore(fresh): %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close fresh: %v", err)
	}

	// Reopen the SAME file: the ALTERs must tolerate the existing columns
	// and the new indexes must already exist (IF NOT EXISTS).
	s2, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore(reopen): %v", err)
	}
	defer func() { _ = s2.Close() }()

	wantCols := map[string]bool{
		"input_hash": false, "model": false, "margin": false,
		"turn_no": false, "outcome": false, "corrected_agent": false,
	}
	rows, err := s2.db.Query(`PRAGMA table_info(dispatch_log)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if _, ok := wantCols[name]; ok {
			wantCols[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	for col, found := range wantCols {
		if !found {
			t.Errorf("dispatch_log missing column %q after migration", col)
		}
	}

	wantIdx := map[string]bool{
		"idx_dispatch_log_hash": false, "idx_dispatch_log_outcome": false,
	}
	idxRows, err := s2.db.Query(`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='dispatch_log'`)
	if err != nil {
		t.Fatalf("sqlite_master: %v", err)
	}
	defer idxRows.Close()
	for idxRows.Next() {
		var name string
		if err := idxRows.Scan(&name); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		if _, ok := wantIdx[name]; ok {
			wantIdx[name] = true
		}
	}
	for idx, found := range wantIdx {
		if !found {
			t.Errorf("dispatch_log missing index %q after migration", idx)
		}
	}
}

// TestRecordDispatch_RoundTripWithNewColumns writes an entry with every new
// field (margin set) and one without (margin nil), then reads back via
// QueryDispatchLog and checks the round trip. Margin must round-trip as a
// float when set and NULL when absent.
func TestRecordDispatch_RoundTripWithNewColumns(t *testing.T) {
	s := newTestStore(t)

	margin := 0.42
	s.RecordDispatch(DispatchEntry{
		SessionID:        "conv-rt",
		InputSummary:     "should not be persisted",
		IntentType:       "code",
		AgentID:          "coder",
		Confidence:       0.9,
		ClassifierMethod: "llm",
		HandlerCase:      "route_to_agent",
		InputHash:        "abc123def4567890",
		Model:            "provider/model-1",
		Margin:           &margin,
		TurnNo:           1,
		Outcome:          "pending",
	})
	s.RecordDispatch(DispatchEntry{
		SessionID:  "conv-rt",
		IntentType: "general",
		AgentID:    "generalist",
		TurnNo:     2,
	})

	results, err := s.QueryDispatchLog(10)
	if err != nil {
		t.Fatalf("QueryDispatchLog: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d rows, want 2", len(results))
	}
	// ORDER BY id DESC: row 2 first.
	door1 := results[1]
	if door1.InputHash != "abc123def4567890" || door1.Model != "provider/model-1" || door1.TurnNo != 1 {
		t.Errorf("door-1 row round-trip mismatch: %+v", door1)
	}
	if door1.Margin == nil || *door1.Margin != margin {
		t.Errorf("door-1 margin = %v, want %v", door1.Margin, margin)
	}
	if door1.Outcome != "pending" {
		t.Errorf("door-1 outcome = %q, want pending", door1.Outcome)
	}
	if door1.CorrectedAgent != "" {
		t.Errorf("door-1 corrected_agent = %q, want empty", door1.CorrectedAgent)
	}

	nonDoor1 := results[0]
	if nonDoor1.Margin != nil {
		t.Errorf("non-door-1 margin = %v, want nil (NULL)", nonDoor1.Margin)
	}
	if nonDoor1.InputHash != "" || nonDoor1.Model != "" {
		t.Errorf("non-door-1 row has unexpected hash/model: %q/%q", nonDoor1.InputHash, nonDoor1.Model)
	}

	// Raw check: margin is genuinely NULL in the row, not 0.
	var nullMargin sql.NullFloat64
	if err := s.db.Get(&nullMargin,
		`SELECT margin FROM dispatch_log WHERE input_hash = ''`); err != nil {
		t.Fatalf("null margin query: %v", err)
	}
	if nullMargin.Valid {
		t.Errorf("margin stored as %v, want NULL", nullMargin.Float64)
	}
}

// TestCountDispatchRows verifies the per-session turn counter source used
// by the dispatcher (turn_no = COUNT(*) + 1).
func TestCountDispatchRows(t *testing.T) {
	s := newTestStore(t)

	if got := s.CountDispatchRows("conv-count"); got != 0 {
		t.Fatalf("fresh session count = %d, want 0", got)
	}
	for i := 0; i < 3; i++ {
		s.RecordDispatch(DispatchEntry{SessionID: "conv-count", TurnNo: i + 1})
	}
	if got := s.CountDispatchRows("conv-count"); got != 3 {
		t.Fatalf("session count = %d, want 3", got)
	}
	if got := s.CountDispatchRows("conv-other"); got != 0 {
		t.Fatalf("other session count = %d, want 0", got)
	}
}
