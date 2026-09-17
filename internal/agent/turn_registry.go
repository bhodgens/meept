package agent

import (
	"sync"
	"time"
)

// SubmittedTurnRecord is one tracked async-submitted chat turn. Shape is
// stable for the reaper (async-turn-migration leaf 06), which consumes
// Stale() to reap abandoned turns as failed. Named ...TurnRecord (not
// TurnRecord) because package agent already has a snapshot TurnRecord in
// turn_compaction.go.
type SubmittedTurnRecord struct {
	TurnID         string
	ConversationID string
	TaskID         string
	SubmittedAt    time.Time
	LastProgressAt time.Time
}

// TurnRegistry tracks async chat.submit turns in memory. It backs the
// submit-ack idempotency dedupe (Register), progress liveness (Touch), and
// the reaper seam (Stale). Sufficient in-memory only: a daemon restart
// orphans tracked turns, which the reaper treats as failed.
type TurnRegistry struct {
	now func() time.Time

	mu    sync.Mutex
	turns map[string]*SubmittedTurnRecord
}

// NewTurnRegistry creates an empty registry. Nil logger-safe.
func NewTurnRegistry() *TurnRegistry {
	return &TurnRegistry{
		now:   time.Now,
		turns: make(map[string]*SubmittedTurnRecord),
	}
}

// Register tracks a newly submitted turn. Idempotent on turnID: when the
// turn is already registered it returns existing=true and the ORIGINAL
// record is preserved untouched (retry dedupe — never overwrite).
func (r *TurnRegistry) Register(turnID, conversationID string) bool {
	if r == nil {
		return false
	}
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.turns[turnID]; ok {
		return true
	}
	r.turns[turnID] = &SubmittedTurnRecord{
		TurnID:         turnID,
		ConversationID: conversationID,
		SubmittedAt:    now,
		LastProgressAt: now,
	}
	return false
}

// AttachTask records the orchestrator task created for a turn. No-op when
// the turn is unknown or taskID is empty.
func (r *TurnRegistry) AttachTask(turnID, taskID string) {
	if r == nil || taskID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.turns[turnID]; ok {
		rec.TaskID = taskID
	}
}

// Touch marks a turn as making progress (reaper clock reset). No-op when
// the turn is unknown.
func (r *TurnRegistry) Touch(turnID string) {
	if r == nil {
		return
	}
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.turns[turnID]; ok {
		rec.LastProgressAt = now
	}
}

// Complete removes a finished turn from tracking. No-op when the turn is
// unknown.
func (r *TurnRegistry) Complete(turnID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.turns, turnID)
}

// Stale returns records whose last progress is older than the given
// duration, ordered by LastProgressAt (oldest first). The returned slice is
// a copy; the registry keeps tracking these turns (the reaper owns removal
// via Complete).
func (r *TurnRegistry) Stale(olderThan time.Duration) []SubmittedTurnRecord {
	if r == nil {
		return nil
	}
	cutoff := r.now().UTC().Add(-olderThan)
	r.mu.Lock()
	defer r.mu.Unlock()
	stale := make([]SubmittedTurnRecord, 0, len(r.turns))
	for _, rec := range r.turns {
		if rec.LastProgressAt.Before(cutoff) {
			stale = append(stale, *rec)
		}
	}
	for i := 1; i < len(stale); i++ {
		for j := i; j > 0 && stale[j].LastProgressAt.Before(stale[j-1].LastProgressAt); j-- {
			stale[j], stale[j-1] = stale[j-1], stale[j]
		}
	}
	return stale
}

// TurnIDForTask returns the turn id that dispatched taskID, or "" when no
// tracked turn claims it (headless task, legacy turn, or already-completed
// turn). Used by the task-end relay so the real result is re-broadcast with
// the ORIGINATING turn id — clients filter turn.terminal by turn_id, and a
// fresh id would be invisible to them (bench gate 2026-09-16: relay events
// carried new ids, so async task results never reached the awaiting client).
func (r *TurnRegistry) TurnIDForTask(taskID string) string {
	if r == nil || taskID == "" {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.turns {
		if rec.TaskID == taskID {
			return rec.TurnID
		}
	}
	return ""
}
