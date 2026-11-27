package turnjournal

// Bughunt 2026-11-25 A2-7 (issue #189): OnRunClose marked the turn in the
// closedRuns dedup set BEFORE the CloseRun store write, so a single failed
// close (SQLITE_BUSY, 5 s bound exceeded) for a turn that is closed again
// later in the same process — the quota-terminal path and the success path
// can both call OnRunClose — was a silent no-op until daemon restart. The
// Warn line "run stays open for rehydrate" was therefore false in-process:
// the run stayed open for the whole process lifetime and re-walked OpenRuns
// on every boot.
//
// Corrected posture: check the marker under the lock, RELEASE the mutex
// (no I/O under the mutex, root AGENTS.md), attempt CloseRun, and mark
// dedup only on success. A failed close leaves the turn unmarked so a
// later close retries it.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// flakyCloseStore fails the first n CloseRun calls, then succeeds. Append
// always succeeds so run_started bookkeeping stays clean.
type flakyCloseStore struct {
	mu        sync.Mutex
	failNext  int
	closeTrys int
	closedOK  []string
}

func (f *flakyCloseStore) Append(context.Context, Record) error { return nil }
func (f *flakyCloseStore) OpenRuns(context.Context) ([]OpenRun, error) {
	return nil, nil
}
func (f *flakyCloseStore) CompletedSteps(context.Context, string) (map[string][]byte, error) {
	return nil, nil
}
func (f *flakyCloseStore) CloseRun(_ context.Context, turnID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeTrys++
	if f.failNext > 0 {
		f.failNext--
		return errors.New("SQLITE_BUSY")
	}
	f.closedOK = append(f.closedOK, turnID)
	return nil
}
func (f *flakyCloseStore) PruneClosed(context.Context, time.Duration) error { return nil }
func (f *flakyCloseStore) Close() error                                     { return nil }

// TestJournalWriterFailedCloseRetriesInProcess: after one failed close, a
// second OnRunClose must re-attempt CloseRun and succeed once the store
// recovers — without a daemon restart. At the pre-fix posture the second
// call no-oped on the dedup marker and CloseRun was attempted exactly once.
func TestJournalWriterFailedCloseRetriesInProcess(t *testing.T) {
	store := &flakyCloseStore{failNext: 1}
	w := NewJournalWriter(store, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	w.OnRunStart(ctx, "turn-r", "s", []byte("p"))
	w.OnRunClose(ctx, "turn-r") // first attempt fails (store busy)

	store.mu.Lock()
	firstTries := store.closeTrys
	store.mu.Unlock()
	if firstTries != 1 {
		t.Fatalf("CloseRun tries = %d, want 1 after the first close", firstTries)
	}

	// Store has recovered. A later close of the SAME turn (the other
	// terminal path) must retry, not no-op.
	w.OnRunClose(ctx, "turn-r")

	store.mu.Lock()
	tries := store.closeTrys
	closed := append([]string(nil), store.closedOK...)
	store.mu.Unlock()
	if tries != 2 || len(closed) != 1 || closed[0] != "turn-r" {
		t.Errorf("retries=%d closedOK=%v, want 2 tries and turn-r finally closed (A2-7: a failed close must be retried in-process)", tries, closed)
	}

	// Third close is now a dedup no-op (successful-close marker persists).
	w.OnRunClose(ctx, "turn-r")
	store.mu.Lock()
	tries = store.closeTrys
	store.mu.Unlock()
	if tries != 2 {
		t.Errorf("CloseRun tries = %d after a successful close, want 2 (successful closes still dedup)", tries)
	}
}
