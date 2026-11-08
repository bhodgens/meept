package turnjournal

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestJournal(t *testing.T) *SQLiteJournal {
	t.Helper()
	j, err := NewSQLiteJournal(filepath.Join(t.TempDir(), "turnjournal.db"), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}

// T1: NewSQLiteJournal creates the turn_journal table with the frozen
// schema, and the CHECK rejects a step_completed row with a NULL payload.
func TestNewSQLiteJournal(t *testing.T) {
	j, err := NewSQLiteJournal(filepath.Join(t.TempDir(), "turnjournal.db"), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer j.Close()

	var name string
	err = j.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='turn_journal'",
	).Scan(&name)
	if err != nil || name != "turn_journal" {
		t.Fatalf("table missing: %v %q", err, name)
	}

	// step_completed with a NULL payload must be rejected by the CHECK.
	_, err = j.db.Exec(
		"INSERT INTO turn_journal (turn_id, session_id, step_id, step_index, kind, result_payload, created_at) VALUES ('t1','s1','step1',0,'step_completed',NULL,0)",
	)
	if err == nil {
		t.Fatal("expected CHECK rejection for step_completed with NULL payload, got nil error")
	}
	if !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("expected CHECK constraint error, got: %v", err)
	}
}

// T2: append-then-read roundtrip across all four record kinds.
func TestAppendReadRoundtrip(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()

	recs := []Record{
		{TurnID: "turn-a", SessionID: "sess-1", StepID: "", StepIndex: 0, Kind: KindRunStarted, ResultPayload: []byte(`{"class":"chat"}`), Timestamp: time.Now()},
		{TurnID: "turn-a", SessionID: "sess-1", StepID: "step-1", StepIndex: 0, Kind: KindStepStarted, Timestamp: time.Now()},
		{TurnID: "turn-a", SessionID: "sess-1", StepID: "step-1", StepIndex: 0, Kind: KindStepCompleted, ResultPayload: []byte(`{"ok":true}`), Timestamp: time.Now()},
	}
	for i, rec := range recs {
		if err := j.Append(ctx, rec); err != nil {
			t.Fatalf("append[%d]: %v", i, err)
		}
	}

	steps, err := j.CompletedSteps(ctx, "turn-a")
	if err != nil {
		t.Fatalf("completed steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("want 1 completed step, got %d: %v", len(steps), steps)
	}
	if got := string(steps["step-1"]); got != `{"ok":true}` {
		t.Fatalf("step-1 payload = %q, want %q", got, `{"ok":true}`)
	}

	runs, err := j.OpenRuns(ctx)
	if err != nil {
		t.Fatalf("open runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1 open run, got %d", len(runs))
	}
	if runs[0].TurnID != "turn-a" || runs[0].SessionID != "sess-1" {
		t.Fatalf("open run = %+v", runs[0])
	}
	if string(runs[0].TurnPayload) != `{"class":"chat"}` {
		t.Fatalf("turn payload = %q", runs[0].TurnPayload)
	}

	if err := j.Append(ctx, Record{TurnID: "turn-a", SessionID: "sess-1", Kind: KindRunClosed, Timestamp: time.Now()}); err != nil {
		t.Fatalf("append run_closed: %v", err)
	}
	runs, err = j.OpenRuns(ctx)
	if err != nil {
		t.Fatalf("open runs after close: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("want 0 open runs after close, got %d", len(runs))
	}
}

// T2: OpenRuns returns only runs with no run_closed row.
func TestOpenRunsOnlyDangling(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()

	seed := func(turnID string, closeIt bool) {
		t.Helper()
		if err := j.Append(ctx, Record{TurnID: turnID, SessionID: "s", Kind: KindRunStarted, Timestamp: time.Now()}); err != nil {
			t.Fatalf("seed %s: %v", turnID, err)
		}
		if closeIt {
			if err := j.CloseRun(ctx, turnID); err != nil {
				t.Fatalf("close %s: %v", turnID, err)
			}
		}
	}
	seed("open-1", false)
	seed("closed-1", true)

	runs, err := j.OpenRuns(ctx)
	if err != nil {
		t.Fatalf("open runs: %v", err)
	}
	if len(runs) != 1 || runs[0].TurnID != "open-1" {
		t.Fatalf("want only open-1, got %+v", runs)
	}
}

// T2: CompletedSteps maps only step_completed rows for the given turn.
func TestCompletedStepsFiltersKinds(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()

	now := time.Now()
	for _, rec := range []Record{
		{TurnID: "t", SessionID: "s", Kind: KindRunStarted, Timestamp: now},
		{TurnID: "t", SessionID: "s", StepID: "step-1", StepIndex: 0, Kind: KindStepStarted, Timestamp: now},
		{TurnID: "t", SessionID: "s", StepID: "step-1", StepIndex: 0, Kind: KindStepCompleted, ResultPayload: []byte("r1"), Timestamp: now},
		{TurnID: "t", SessionID: "s", StepID: "step-2", StepIndex: 1, Kind: KindStepStarted, Timestamp: now},
		{TurnID: "other", SessionID: "s", StepID: "step-9", StepIndex: 0, Kind: KindStepCompleted, ResultPayload: []byte("r9"), Timestamp: now},
	} {
		if err := j.Append(ctx, rec); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	steps, err := j.CompletedSteps(ctx, "t")
	if err != nil {
		t.Fatalf("completed steps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("want only step-1 for turn t, got %v", steps)
	}
	if string(steps["step-1"]) != "r1" {
		t.Fatalf("step-1 = %q", steps["step-1"])
	}
}

// T2: CloseRun writes the run_closed row and clears OpenRuns.
func TestCloseRun(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()

	if err := j.Append(ctx, Record{TurnID: "t", SessionID: "s", Kind: KindRunStarted, Timestamp: time.Now()}); err != nil {
		t.Fatalf("append: %v", err)
	}
	runs, err := j.OpenRuns(ctx)
	if err != nil || len(runs) != 1 {
		t.Fatalf("pre-close runs = %v (err %v)", runs, err)
	}
	if err := j.CloseRun(ctx, "t"); err != nil {
		t.Fatalf("close run: %v", err)
	}
	runs, err = j.OpenRuns(ctx)
	if err != nil {
		t.Fatalf("post-close query: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("want 0 open runs after CloseRun, got %+v", runs)
	}
	var n int
	if err := j.db.QueryRow("SELECT COUNT(*) FROM turn_journal WHERE turn_id='t' AND kind='run_closed'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("run_closed rows = %d (err %v)", n, err)
	}
}

// T2: a timed-out Append returns a wrapped error instead of blocking past
// the op timeout.
func TestAppendTimeout(t *testing.T) {
	j := newTestJournal(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := j.Append(ctx, Record{TurnID: "t", Kind: KindStepStarted, Timestamp: time.Now()})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error on cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped context.Canceled, got: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Append blocked %v, want < 5s", elapsed)
	}
}

// T3: *SQLiteJournal satisfies JournalStore, and a nil receiver returns the
// descriptive not-initialized error without panicking.
func TestJournalStoreInterfaceAndNilSafety(t *testing.T) {
	var store JournalStore = (*SQLiteJournal)(nil)

	ctx := context.Background()
	if err := store.Append(ctx, Record{Kind: KindRunStarted}); err == nil || !strings.Contains(err.Error(), "turnjournal: not initialized") {
		t.Fatalf("nil Append = %v", err)
	}
	if _, err := store.OpenRuns(ctx); err == nil || !strings.Contains(err.Error(), "turnjournal: not initialized") {
		t.Fatalf("nil OpenRuns = %v", err)
	}
	if _, err := store.CompletedSteps(ctx, "t"); err == nil || !strings.Contains(err.Error(), "turnjournal: not initialized") {
		t.Fatalf("nil CompletedSteps = %v", err)
	}
	if err := store.CloseRun(ctx, "t"); err == nil || !strings.Contains(err.Error(), "turnjournal: not initialized") {
		t.Fatalf("nil CloseRun = %v", err)
	}
	if err := store.PruneClosed(ctx, time.Hour); err == nil || !strings.Contains(err.Error(), "turnjournal: not initialized") {
		t.Fatalf("nil PruneClosed = %v", err)
	}
	// Close on a nil receiver is a safe no-op.
	if err := store.Close(); err != nil {
		t.Fatalf("nil Close = %v", err)
	}
}

// T4: PruneClosed deletes rows of runs closed before the cutoff and keeps
// everything else.
func TestPruneClosed(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()

	old := time.Now().Add(-8 * 24 * time.Hour)
	fresh := time.Now()

	for _, rec := range []Record{
		{TurnID: "old-run", SessionID: "s", Kind: KindRunStarted, Timestamp: old},
		{TurnID: "old-run", SessionID: "s", StepID: "step-1", Kind: KindStepCompleted, ResultPayload: []byte("r"), Timestamp: old},
		{TurnID: "old-run", SessionID: "s", Kind: KindRunClosed, Timestamp: old},
		{TurnID: "fresh-run", SessionID: "s", Kind: KindRunStarted, Timestamp: fresh},
		{TurnID: "fresh-run", SessionID: "s", StepID: "step-1", Kind: KindStepCompleted, ResultPayload: []byte("r"), Timestamp: fresh},
		{TurnID: "fresh-run", SessionID: "s", Kind: KindRunClosed, Timestamp: fresh},
	} {
		if err := j.Append(ctx, rec); err != nil {
			t.Fatalf("append %s/%s: %v", rec.TurnID, rec.Kind, err)
		}
	}

	if err := j.PruneClosed(ctx, 7*24*time.Hour); err != nil {
		t.Fatalf("prune: %v", err)
	}

	var oldRows, freshRows int
	if err := j.db.QueryRow("SELECT COUNT(*) FROM turn_journal WHERE turn_id='old-run'").Scan(&oldRows); err != nil {
		t.Fatalf("count old: %v", err)
	}
	if err := j.db.QueryRow("SELECT COUNT(*) FROM turn_journal WHERE turn_id='fresh-run'").Scan(&freshRows); err != nil {
		t.Fatalf("count fresh: %v", err)
	}
	if oldRows != 0 {
		t.Fatalf("old-run rows = %d, want 0", oldRows)
	}
	if freshRows != 3 {
		t.Fatalf("fresh-run rows = %d, want 3", freshRows)
	}

	runs, err := j.OpenRuns(ctx)
	if err != nil {
		t.Fatalf("open runs: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("OpenRuns after prune = %+v, want empty", runs)
	}
}

// bughunt 2026-11-07 dispatcher-queue F-4: OpenRuns' docblock promises the
// NEWEST payload on duplicate run_started rows. The old GROUP BY took MAX
// (session_id) and read result_payload from an arbitrary tie row — the bare-
// column rule only resolves to the row that satisfied the MAX aggregate, so
// with identical session_ids the payload was scan-order-dependent. The SQL
// now selects the row whose created_at equals the per-turn MAX explicitly.
func TestOpenRunsNewestPayloadWins(t *testing.T) {
	j := newTestJournal(t)
	ctx := context.Background()

	base := time.Now().Add(-time.Minute)
	if err := j.Append(ctx, Record{TurnID: "dup-1", SessionID: "s", Kind: KindRunStarted,
		ResultPayload: []byte(`{"payload":"older"}`), Timestamp: base}); err != nil {
		t.Fatalf("append older: %v", err)
	}
	if err := j.Append(ctx, Record{TurnID: "dup-1", SessionID: "s", Kind: KindRunStarted,
		ResultPayload: []byte(`{"payload":"newest"}`), Timestamp: base.Add(time.Second)}); err != nil {
		t.Fatalf("append newer: %v", err)
	}

	runs, err := j.OpenRuns(ctx)
	if err != nil {
		t.Fatalf("open runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1 open run, got %+v", runs)
	}
	if got := string(runs[0].TurnPayload); got != `{"payload":"newest"}` {
		t.Fatalf("OpenRuns payload = %s, want the NEWEST run_started row (F-4 tie-break)", got)
	}
}
