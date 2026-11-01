package turnjournal

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// journalOpTimeout bounds one journal write from the turn path. Same
// posture as the store's opTimeout: journaling is best-effort and must
// never block the turn longer than this.
const journalOpTimeout = 5 * time.Second

// JournalWriter is the BEST-EFFORT turn-path seam (master contract C2):
// the step pipeline calls the four On* methods at the turn/step
// boundaries and the writer appends the matching journal rows. No On*
// method ever returns an error or fails the turn it records (master C2 /
// research F4): an append failure logs at Warn and the turn proceeds.
//
// A nil *JournalWriter is valid and every method is a no-op -- the
// flag-off / store-failed wiring keeps the legacy path byte-identical
// with zero call-site nil checks.
type JournalWriter struct {
	store  JournalStore
	logger *slog.Logger

	// runStartedMu guards the once-per-turn dedup sets. OnRunStart runs
	// before the first step of every turn attempt, and OnRunClose can
	// fire from both the success path and the quota-terminal path, so
	// without the guards a retried first step (or a double close) would
	// append a second run_started/run_closed row. The mutex only
	// serializes the map check-then-set; all store I/O happens after
	// Unlock (mutexio).
	runStartedMu   sync.Mutex
	runStartedRuns map[string]bool
	closedRuns     map[string]bool
}

// NewJournalWriter builds a writer over store. store must be non-nil; a
// nil logger defaults to slog.Default().
func NewJournalWriter(store JournalStore, logger *slog.Logger) *JournalWriter {
	if logger == nil {
		logger = slog.Default()
	}
	return &JournalWriter{
		store:          store,
		logger:         logger,
		runStartedRuns: make(map[string]bool),
		closedRuns:     make(map[string]bool),
	}
}

// record appends one row on a detached, op-bounded context, converting
// every failure into a Warn log. Detached deliberately: journal writes
// must survive the turn's request context being cancelled mid-flight
// (the whole point of the ledger is a hard kill), and best-effort
// means a cancelled turn never produces a spurious Warn.
func (w *JournalWriter) record(rec Record) {
	ctx, cancel := context.WithTimeout(context.Background(), journalOpTimeout)
	defer cancel()
	if err := w.store.Append(ctx, rec); err != nil {
		w.logger.Warn("turn journal: append failed (turn proceeds unchanged)",
			"turn_id", rec.TurnID,
			"step_id", rec.StepID,
			"kind", string(rec.Kind),
			"error", err,
		)
	}
}

// OnRunStart records the turn's run_started row with the class payload
// blob (the same role turn_payload plays in ParkedTurnRecord -- leaf
// 03's rehydrate requeues from it). Idempotent per writer instance: the
// second OnRunStart for the same turn is a no-op, so a retried first
// step cannot append a second run_started row. (Across restarts leaf
// 03's rehydrate consumes OpenRuns, which tolerates duplicates by
// yielding the newest payload.)
func (w *JournalWriter) OnRunStart(ctx context.Context, turnID, sessionID string, turnPayload []byte) {
	if w == nil || w.store == nil || turnID == "" {
		return
	}
	w.runStartedMu.Lock()
	if w.runStartedRuns[turnID] {
		w.runStartedMu.Unlock()
		return
	}
	w.runStartedRuns[turnID] = true
	w.runStartedMu.Unlock()

	w.record(Record{
		TurnID:        turnID,
		SessionID:     sessionID,
		Kind:          KindRunStarted,
		ResultPayload: turnPayload,
	})
}

// OnStepStart records a step's beginning.
func (w *JournalWriter) OnStepStart(ctx context.Context, turnID, sessionID, stepID string, stepIndex int) {
	if w == nil || w.store == nil || turnID == "" || stepID == "" {
		return
	}
	w.record(Record{
		TurnID:    turnID,
		SessionID: sessionID,
		StepID:    stepID,
		StepIndex: stepIndex,
		Kind:      KindStepStarted,
	})
}

// OnStepComplete records a step's completion with its serialized result
// blob (the resume path's memoized value -- research F3: a journaled
// result is returned on replay, never re-invoked).
func (w *JournalWriter) OnStepComplete(ctx context.Context, turnID, sessionID, stepID string, stepIndex int, result []byte) {
	if w == nil || w.store == nil || turnID == "" || stepID == "" {
		return
	}
	if result == nil {
		// The schema CHECK requires a non-nil payload on step_completed;
		// an empty JSON object keeps the row honest without inventing
		// content.
		result = []byte("{}")
	}
	w.record(Record{
		TurnID:        turnID,
		SessionID:     sessionID,
		StepID:        stepID,
		StepIndex:     stepIndex,
		Kind:          KindStepCompleted,
		ResultPayload: result,
	})
}

// OnRunClose records the terminal write of a completed job, removing
// the turn from OpenRuns. When the close write fails the run stays
// open -- leaf 03's rehydrate then treats it as interrupted, which is
// the safe direction (research F4: recorded steps are skipped, never
// re-executed). Best-effort like every On*: the error becomes a Warn,
// never a turn failure.
func (w *JournalWriter) OnRunClose(ctx context.Context, turnID string) {
	if w == nil || w.store == nil || turnID == "" {
		return
	}
	w.runStartedMu.Lock()
	if w.closedRuns[turnID] {
		w.runStartedMu.Unlock()
		return
	}
	w.closedRuns[turnID] = true
	w.runStartedMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), journalOpTimeout)
	defer cancel()
	if err := w.store.CloseRun(ctx, turnID); err != nil {
		w.logger.Warn("turn journal: close run failed (run stays open for rehydrate)",
			"turn_id", turnID,
			"error", err,
		)
	}
}
