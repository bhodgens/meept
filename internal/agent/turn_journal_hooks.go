package agent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/caimlas/meept/internal/agent/turnjournal"
)

// TurnJournalHooks is the loop/daemon-facing journal seam (turn-journal
// tree leaf 02, master contracts C2 + C4). The daemon's step pipeline
// (AgentJobProcessor.Process) holds one when daemon.turn_journal_enabled
// is on and the store opened; the hooks own the best-effort
// turnjournal.JournalWriter and expose the four boundary events plus the
// PlanResume skip seam, so the daemon package touches turnjournal only
// through this file.
//
// All methods are BEST-EFFORT (master C2): a journal failure logs Warn
// and the turn proceeds. A nil *TurnJournalHooks is valid -- every method
// is a no-op -- so flag-off wiring stays byte-identical with zero
// call-site nil checks.
type TurnJournalHooks struct {
	store  turnjournal.JournalStore
	writer *turnjournal.JournalWriter
}

// NewTurnJournalHooks builds the seam over a live journal store.
// store must be non-nil (wired only when the flag is on and the store
// opened); a nil logger defaults to slog.Default().
func NewTurnJournalHooks(store turnjournal.JournalStore, logger *slog.Logger) *TurnJournalHooks {
	return &TurnJournalHooks{
		store:  store,
		writer: turnjournal.NewJournalWriter(store, logger),
	}
}

// OnRunStart journals the turn's run_started row. turnPayload is the
// queued job's request payload (the same role turn_payload plays in
// ParkedTurnRecord) so leaf 03's rehydrate can requeue without
// reconstructing history.
func (h *TurnJournalHooks) OnRunStart(ctx context.Context, turnID, sessionID string, turnPayload []byte) {
	if h == nil {
		return
	}
	h.writer.OnRunStart(ctx, turnID, sessionID, turnPayload)
}

// OnStepStart journals a step's beginning.
func (h *TurnJournalHooks) OnStepStart(ctx context.Context, turnID, sessionID, stepID string, stepIndex int) {
	if h == nil {
		return
	}
	h.writer.OnStepStart(ctx, turnID, sessionID, stepID, stepIndex)
}

// OnStepComplete journals a step's completion with its serialized
// result blob (research F3: restored on replay, never re-invoked).
func (h *TurnJournalHooks) OnStepComplete(ctx context.Context, turnID, sessionID, stepID string, stepIndex int, result []byte) {
	if h == nil {
		return
	}
	h.writer.OnStepComplete(ctx, turnID, sessionID, stepID, stepIndex, result)
}

// OnRunClose journals the terminal run_closed row of a completed job.
// NEVER called on the park path: a quota/throttle-parked turn leaves
// the run open so the resume re-enters at the same position (leaf T4).
func (h *TurnJournalHooks) OnRunClose(ctx context.Context, turnID string) {
	if h == nil {
		return
	}
	h.writer.OnRunClose(ctx, turnID)
}

// PlanResume consults the journal for turnID and returns which of the
// given steps already completed (with their recorded results) and the
// index of the first step to execute. A journal read failure is an
// error: the caller cannot decide the resume position without the
// journal, and guessing would re-execute recorded steps (research F4).
// A nil receiver returns an empty plan (execute everything).
func (h *TurnJournalHooks) PlanResume(ctx context.Context, turnID string, steps []turnjournal.StepRef) (map[string][]byte, int, error) {
	if h == nil {
		return nil, 0, nil
	}
	return turnjournal.PlanResume(ctx, h.store, turnID, steps)
}

// MarshalTurnResult serializes the step result envelope the journal
// stores. Falls back to an empty JSON object when the envelope is not
// marshalable (journaling is best-effort and never fails the turn).
func MarshalTurnResult(envelope any) []byte {
	b, err := json.Marshal(envelope)
	if err != nil {
		return []byte("{}")
	}
	return b
}
