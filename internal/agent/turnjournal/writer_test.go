package turnjournal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// failingStore fails every Append; CloseRun fails too (so the close
// path's Warn branch is exercised directly).
type failingStore struct{ appends int }

func (f *failingStore) Append(ctx context.Context, rec Record) error {
	f.appends++
	return errors.New("boom")
}
func (f *failingStore) OpenRuns(ctx context.Context) ([]OpenRun, error) { return nil, nil }
func (f *failingStore) CompletedSteps(ctx context.Context, turnID string) (map[string][]byte, error) {
	return nil, errors.New("boom")
}
func (f *failingStore) CloseRun(ctx context.Context, turnID string) error { return errors.New("boom") }
func (f *failingStore) PruneClosed(ctx context.Context, olderThan time.Duration) error {
	return nil
}
func (f *failingStore) Close() error { return nil }

// recordingStore captures appended rows and closes for assertions.
type recordingStore struct {
	mu     sync.Mutex
	rows   []Record
	closed []string
}

func (r *recordingStore) Append(ctx context.Context, rec Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, rec)
	return nil
}
func (r *recordingStore) OpenRuns(ctx context.Context) ([]OpenRun, error) { return nil, nil }
func (r *recordingStore) CompletedSteps(ctx context.Context, turnID string) (map[string][]byte, error) {
	return nil, nil
}
func (r *recordingStore) CloseRun(ctx context.Context, turnID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = append(r.closed, turnID)
	r.rows = append(r.rows, Record{TurnID: turnID, Kind: KindRunClosed})
	return nil
}
func (r *recordingStore) PruneClosed(ctx context.Context, olderThan time.Duration) error {
	return nil
}
func (r *recordingStore) Close() error { return nil }

func (r *recordingStore) kinds() []RecordKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RecordKind, len(r.rows))
	for i, row := range r.rows {
		out[i] = row.Kind
	}
	return out
}

// T1: the four On* methods in order produce the expected rows.
func TestJournalWriterHappyPath(t *testing.T) {
	store := &recordingStore{}
	w := NewJournalWriter(store, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	w.OnRunStart(ctx, "turn-1", "sess-1", []byte(`{"class":"chat"}`))
	w.OnStepStart(ctx, "turn-1", "sess-1", "s0", 0)
	w.OnStepComplete(ctx, "turn-1", "sess-1", "s0", 0, []byte(`{"ok":true}`))
	w.OnStepStart(ctx, "turn-1", "sess-1", "s1", 1)
	w.OnStepComplete(ctx, "turn-1", "sess-1", "s1", 1, []byte(`{"ok":2}`))
	w.OnRunClose(ctx, "turn-1")

	got := store.kinds()
	want := []RecordKind{KindRunStarted, KindStepStarted, KindStepCompleted, KindStepStarted, KindStepCompleted, KindRunClosed}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row[%d] = %s, want %s", i, got[i], want[i])
		}
	}
	store.mu.Lock()
	if string(store.rows[0].ResultPayload) != `{"class":"chat"}` {
		t.Errorf("run_started payload = %q", store.rows[0].ResultPayload)
	}
	if string(store.rows[2].ResultPayload) != `{"ok":true}` {
		t.Errorf("step_completed payload = %q", store.rows[2].ResultPayload)
	}
	if len(store.closed) != 1 || store.closed[0] != "turn-1" {
		t.Errorf("closed = %v", store.closed)
	}
	store.mu.Unlock()
}

// T1: a failing store leaves every On* call returning silently - no
// panic, and the failure surfaces only as a Warn log (the Warn goes to
// the discard logger here; the contract under test is no-panic +
// proceed).
func TestJournalWriterFailingStoreNeverPanics(t *testing.T) {
	fs := &failingStore{}
	w := NewJournalWriter(fs, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	w.OnRunStart(ctx, "turn-1", "sess-1", nil)
	w.OnStepStart(ctx, "turn-1", "sess-1", "s0", 0)
	w.OnStepComplete(ctx, "turn-1", "sess-1", "s0", 0, []byte(`{}`))
	w.OnRunClose(ctx, "turn-1")

	if fs.appends != 3 {
		t.Fatalf("store saw %d appends, want 3 (all attempted)", fs.appends)
	}
}

// T1: double OnRunStart produces exactly one run_started row.
func TestJournalWriterRunStartIdempotent(t *testing.T) {
	store := &recordingStore{}
	w := NewJournalWriter(store, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	w.OnRunStart(ctx, "turn-1", "sess-1", []byte("p1"))
	w.OnRunStart(ctx, "turn-1", "sess-1", []byte("p2"))

	kinds := store.kinds()
	if len(kinds) != 1 || kinds[0] != KindRunStarted {
		t.Fatalf("rows = %v, want exactly one run_started", kinds)
	}
}

// T1: OnRunClose is idempotent - the second call does not append a
// second run_closed row.
func TestJournalWriterRunCloseIdempotent(t *testing.T) {
	store := &recordingStore{}
	w := NewJournalWriter(store, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	w.OnRunClose(ctx, "turn-1")
	w.OnRunClose(ctx, "turn-1")

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.closed) != 1 {
		t.Fatalf("closed = %v, want exactly one close", store.closed)
	}
}

// T1: a nil writer is a safe no-op for every method.
func TestJournalWriterNilReceiver(t *testing.T) {
	var w *JournalWriter
	ctx := context.Background()
	w.OnRunStart(ctx, "t", "s", nil)
	w.OnStepStart(ctx, "t", "s", "s0", 0)
	w.OnStepComplete(ctx, "t", "s", "s0", 0, []byte("{}"))
	w.OnRunClose(ctx, "t")
}

// T1/T5: a live writer over the real SQLite store round-trips through
// the JournalStore interface.
func TestJournalWriterOverSQLite(t *testing.T) {
	j, err := NewSQLiteJournal(filepath.Join(t.TempDir(), "turnjournal.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })

	w := NewJournalWriter(j, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	w.OnRunStart(ctx, "turn-sql", "sess-1", []byte(`{}`))
	w.OnStepComplete(ctx, "turn-sql", "sess-1", "s0", 0, []byte(`{"r":1}`))
	w.OnRunClose(ctx, "turn-sql")

	steps, err := j.CompletedSteps(ctx, "turn-sql")
	if err != nil {
		t.Fatalf("completed steps: %v", err)
	}
	if string(steps["s0"]) != `{"r":1}` {
		t.Fatalf("s0 = %q", steps["s0"])
	}
	runs, err := j.OpenRuns(ctx)
	if err != nil || len(runs) != 0 {
		t.Fatalf("runs after close = %v (err %v)", runs, err)
	}
}

// Keep strings referenced without infloning runtime cost in this file.
var _ = strings.Contains

// F-5 (bughunt agent-core): the dedup sets must stay BOUNDED across
// daemon-lifetime turn churn while the pinned interlock semantics hold:
// below the ring cap behavior is byte-identical to the unbounded legacy
// (retries coalesce, double closes no-op); above it the oldest turnIDs
// are forgotten FIFO — a forgotten turn re-appending one duplicate row
// is tolerated by OpenRuns (GROUP BY newest) and reclaimed by
// PruneClosed.

func TestJournalWriterDedupSetsStayBounded(t *testing.T) {
	store := &recordingStore{}
	w := NewJournalWriter(store, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	// Churn 3x the ring cap through open->close cycles.
	for i := 0; i < 3*dedupRingCap; i++ {
		id := fmt.Sprintf("t-%d", i)
		w.OnRunStart(ctx, id, "s", []byte(`{}`))
		w.OnRunClose(ctx, id)
	}

	w.runStartedMu.Lock()
	defer w.runStartedMu.Unlock()
	if len(w.runStartedRuns) != dedupRingCap || len(w.startedOrder) != dedupRingCap {
		t.Fatalf("runStartedRuns ring = map %d/order %d, want both %d",
			len(w.runStartedRuns), len(w.startedOrder), dedupRingCap)
	}
	if len(w.closedRuns) != dedupRingCap || len(w.closedOrder) != dedupRingCap {
		t.Fatalf("closedRuns ring = map %d/order %d, want both %d",
			len(w.closedRuns), len(w.closedOrder), dedupRingCap)
	}
	// FIFO: the ring forgot the OLDEST ids and kept the newest.
	if w.runStartedRuns["t-0"] || !w.runStartedRuns[fmt.Sprintf("t-%d", 3*dedupRingCap-1)] {
		t.Fatal("runStarted ring evicted the wrong end")
	}
	if w.closedRuns["t-0"] || !w.closedRuns[fmt.Sprintf("t-%d", 3*dedupRingCap-1)] {
		t.Fatal("closed ring evicted the wrong end")
	}
}

func TestJournalWriterRingKeepsLegacySemanticsBelowCap(t *testing.T) {
	store := &recordingStore{}
	w := NewJournalWriter(store, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	// Live-run retry coalesces (T1 shape).
	w.OnRunStart(ctx, "turn-x", "s", []byte("p1"))
	w.OnRunStart(ctx, "turn-x", "s", []byte("p2"))
	// Re-start after close is STILL deduped (legacy posture preserved:
	// rehydrate tolerates open runs via SQL, not writer re-append).
	w.OnRunClose(ctx, "turn-x")
	w.OnRunStart(ctx, "turn-x", "s", []byte("p3"))
	// Double close no-ops (T1 shape).
	w.OnRunClose(ctx, "turn-x")

	store.mu.Lock()
	defer store.mu.Unlock()
	var started, closed int
	for _, r := range store.rows {
		switch r.Kind {
		case KindRunStarted:
			started++
		case KindRunClosed:
			closed++
		}
	}
	if started != 1 || closed != 1 {
		t.Fatalf("rows: started=%d closed=%d, want 1/1 (legacy dedup semantics below cap)", started, closed)
	}
}

func TestJournalWriterFailedCloseKeepsDedupMarkers(t *testing.T) {
	store := &failingStore{}
	w := NewJournalWriter(store, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	w.OnRunStart(ctx, "turn-f", "s", []byte("p"))
	w.OnRunClose(ctx, "turn-f") // CloseRun fails -> keep markers

	w.runStartedMu.Lock()
	defer w.runStartedMu.Unlock()
	if !w.runStartedRuns["turn-f"] || !w.closedRuns["turn-f"] {
		t.Fatal("failed close must keep BOTH dedup markers (legacy posture; boot rehydrate settles the run)")
	}
	if len(w.closedOrder) != 1 {
		t.Fatalf("closedOrder should track the marked close: %v", w.closedOrder)
	}
}
