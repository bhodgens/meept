package turnjournal

import (
	"context"
	"time"
)

// JournalStore is the consumer-facing interface over the durable step
// ledger (master contract C1). Leaf 02's JournalWriter and leaf 03's
// Rehydrate program against this interface, so tests can substitute a
// fake without a real database.
//
// Every method is bounded by the store's op timeout (5s) and returns
// wrapped errors prefixed "turnjournal:".
type JournalStore interface {
	// Append records one turn/step boundary event.
	Append(ctx context.Context, rec Record) error
	// OpenRuns lists turns with no run_closed row (interrupted runs).
	OpenRuns(ctx context.Context) ([]OpenRun, error)
	// CompletedSteps maps step_id -> result payload for a turn's
	// step_completed rows.
	CompletedSteps(ctx context.Context, turnID string) (map[string][]byte, error)
	// CloseRun writes the run_closed row for a completed job.
	CloseRun(ctx context.Context, turnID string) error
	// PruneClosed deletes rows of runs closed before the cutoff
	// (finding F6 retention).
	PruneClosed(ctx context.Context, olderThan time.Duration) error
	// Close releases the underlying handle.
	Close() error
}

// Compile-time assertion: the SQLite store satisfies the interface.
var _ JournalStore = (*SQLiteJournal)(nil)
