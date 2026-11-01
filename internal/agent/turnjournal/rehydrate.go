package turnjournal

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ClosedRunRetention is the boot-time retention window for closed runs
// (T4, research F6): at boot the daemon prunes every row of a run whose
// run_closed row is older than this. Open runs are never touched -- they
// are the resume surface. A const, not config: retention is a hygiene
// bound, not a tuning knob (flipping it is a product decision, same lane
// as the opt-in flag).
const ClosedRunRetention = 7 * 24 * time.Hour

// Rehydrate is the boot-time resume hook (master contract C3): list the
// journal's open runs -- turns that were in flight when the daemon last
// stopped -- and hand each one to requeue so the EXISTING queue path
// re-arms it. Rehydrate never executes turns itself, never creates a
// second park path, and never duplicates the queue's completion dedup
// (research F4/F5): the requeue closure owns the enqueue semantics and
// must keep the job's ORIGINAL ID so the queue's claim-token guard stays
// authoritative for completion races.
//
// Per-run failures are best-effort (the reArm boot-load pattern): a
// failing requeue logs Warn and the pass continues with the remaining
// runs -- the row stays open for the next boot. A store READ failure is
// different: without the open-run list nothing can be resumed, so the
// error is returned (resumed=0) and the caller logs it; rows also stay
// for the next boot. Boot itself never fails (the daemon wiring treats
// both outcomes as log-only).
//
// resumed counts runs whose requeue closure returned nil. A no-op
// requeue -- the job is already claimable, or the queue state machine
// already settled the turn -- counts exactly once per run: OpenRuns
// groups by turn_id, so duplicate run_started rows (leaf-02 observation:
// the newest payload wins) cannot double-count.
func Rehydrate(ctx context.Context, store JournalStore,
	requeue func(turnID, sessionID string, payload []byte) error,
	logger *slog.Logger,
) (resumed int, err error) {
	if store == nil {
		return 0, fmt.Errorf("turnjournal: rehydrate: nil store")
	}
	if requeue == nil {
		return 0, fmt.Errorf("turnjournal: rehydrate: nil requeue closure")
	}
	if logger == nil {
		logger = slog.Default()
	}

	runs, err := store.OpenRuns(ctx)
	if err != nil {
		return 0, fmt.Errorf("turnjournal: rehydrate: %w", err)
	}

	for _, run := range runs {
		if rerr := requeue(run.TurnID, run.SessionID, run.TurnPayload); rerr != nil {
			logger.Warn("turn journal: requeue failed for interrupted turn (stays open for the next boot)",
				"turn_id", run.TurnID,
				"session_id", run.SessionID,
				"error", rerr,
			)
			continue
		}
		resumed++
	}
	return resumed, nil
}

// PruneClosedRuns is the boot-time retention sweep (T4, research F6):
// delete the rows of runs closed before ClosedRunRetention. Best-effort
// by contract: a store failure logs Warn and boot continues -- retention
// must never be the thing that keeps a daemon from coming up. Runs only
// behind the flag: the daemon wiring skips the whole rehydrate block
// (this sweep included) when the journal is disabled or its store never
// opened.
func PruneClosedRuns(ctx context.Context, store JournalStore, logger *slog.Logger) {
	if store == nil || logger == nil {
		return
	}
	if err := store.PruneClosed(ctx, ClosedRunRetention); err != nil {
		logger.Warn("turn journal: closed-run retention prune failed (boot continues)",
			"error", err,
		)
	}
}
