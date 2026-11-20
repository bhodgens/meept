package bus

// C6 critical topic set (20261119-usability-remediation / 01-safety/03).
//
// These are the per-task/plan-once TERMINAL topics: losing one to a full
// subscriber buffer breaks every awaiting client's state machine (the
// serial bus-drop terminal loss, F-NEW-3). Producers MUST route them
// through PublishBlocking / PublishBlockingT (5s bounded wait), and the
// rpc poll buffer keeps them in a separate ring exempt from the
// drop-oldest progress trim. Progress chatter (agent.progress,
// task.progress, step.started, llm.call, ...) stays non-blocking and
// drop-oldest.
//
// The set is EXACT (master.md C6): turn.terminal, task.completed,
// task.failed, task.rejected, plan.completed, plan.rejected, plan.failed.
// plan.failed is created by 01-safety/05; it is declared critical here so
// its publish is blocking from the moment it exists.

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// criticalTopicSet is the declared C6 contract. Kept as a map for O(1)
// membership checks on the hot publish/collector paths.
var criticalTopicSet = map[string]struct{}{
	"turn.terminal":  {},
	"task.completed": {},
	"task.failed":    {},
	"task.rejected":  {},
	"plan.completed": {},
	"plan.rejected":  {},
	"plan.failed":    {},
}

// IsCriticalTopic reports whether topic is in the C6 critical set:
// a per-task/plan-once terminal event that must use the blocking publish
// path and is exempt from the poll buffer's drop-oldest trim.
func IsCriticalTopic(topic string) bool {
	_, ok := criticalTopicSet[topic]
	return ok
}

// ErrLoopClosed is the closed-runtime sentinel for turn submission: a
// turn handed to a torn-down loop must terminalize failed with this
// error instead of running on dead state or vanishing silently. The
// closed-loop guard itself (RunOnceWithParts check + stale-loop rebuild)
// is 01-safety/05 T4; this leaf declares the shared sentinel so the turn
// outbox and the guard agree on the error identity.
var ErrLoopClosed = errors.New("agent: loop closed")

// TurnOutboxEntry is one retained turn outcome. Payload is the terminal
// event payload as published (turn.terminal shape); Seq is the outbox
// assignment order (1-based, monotonic) so a recovering client can page
// deterministically.
type TurnOutboxEntry struct {
	TurnID    string
	SessionID string
	Topic     string
	Payload   []byte
	Seq       int64
	At        time.Time
}

// TurnOutbox retains per-turn-once terminal outcomes so a client that
// missed the live event (full buffer timeout, daemon restart, reconnect)
// can always recover the turn outcome (C6 / W25 recovery seam). It is
// bounded drop-oldest with explicit overflow accounting: retention loss
// is counted, never silent. Safe for concurrent use.
//
// The turn.result query RPC that exposes this store is 01-safety/05 T5;
// this leaf owns the store and the publish-side capture.
type TurnOutbox struct {
	mu       sync.Mutex
	capacity int
	entries  []TurnOutboxEntry
	nextSeq  int64
	trimmed  int64
}

// NewTurnOutbox creates an outbox retaining at most capacity terminal
// outcomes. capacity <= 0 falls back to DefaultTurnOutboxCapacity.
func NewTurnOutbox(capacity int) *TurnOutbox {
	if capacity <= 0 {
		capacity = DefaultTurnOutboxCapacity
	}
	return &TurnOutbox{capacity: capacity, entries: make([]TurnOutboxEntry, 0, capacity)}
}

// DefaultTurnOutboxCapacity bounds retained terminal outcomes. Matches
// the C6 critical ring size so a reconnecting client can recover every
// terminal the poll ring would still hold.
const DefaultTurnOutboxCapacity = 256

// Record retains a terminal outcome for turnID. Only C6 critical topics
// are retained; anything else is ignored (returns false) so progress
// chatter can never evict terminals. The payload is copied: callers may
// reuse their buffer after Record returns.
func (o *TurnOutbox) Record(turnID, sessionID, topic string, payload []byte) bool {
	if o == nil || !IsCriticalTopic(topic) {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nextSeq++
	entry := TurnOutboxEntry{
		TurnID:    turnID,
		SessionID: sessionID,
		Topic:     topic,
		Payload:   append([]byte(nil), payload...),
		Seq:       o.nextSeq,
		At:        time.Now().UTC(),
	}
	o.entries = append(o.entries, entry)
	if over := len(o.entries) - o.capacity; over > 0 {
		o.entries = o.entries[over:]
		o.trimmed += int64(over)
	}
	return true
}

// Since returns retained entries with Seq > sinceSeq in assignment order
// (a fresh slice; callers own it). sinceSeq == 0 returns everything
// retained.
func (o *TurnOutbox) Since(sinceSeq int64) []TurnOutboxEntry {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]TurnOutboxEntry, 0, len(o.entries))
	for _, e := range o.entries {
		if e.Seq > sinceSeq {
			out = append(out, e)
		}
	}
	return out
}

// Lookup returns the most recent retained terminal for turnID, or false
// when the outbox holds none (never retained or trimmed away).
func (o *TurnOutbox) Lookup(turnID string) (TurnOutboxEntry, bool) {
	if o == nil {
		return TurnOutboxEntry{}, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := len(o.entries) - 1; i >= 0; i-- {
		if o.entries[i].TurnID == turnID {
			return o.entries[i], true
		}
	}
	return TurnOutboxEntry{}, false
}

// Stats reports retention accounting: retained entry count, the next
// assignment seq (cursor head), and entries lost to the bounded trim.
func (o *TurnOutbox) Stats() (retained int, nextSeq int64, trimmed int64) {
	if o == nil {
		return 0, 0, 0
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.entries), o.nextSeq, o.trimmed
}

// TurnOutbox returns the bus's terminal-outbox store, creating it on
// first use (capacity DefaultTurnOutboxCapacity). Nil-safe. The store is
// the C6 recovery seam: critical terminals are captured at publish time
// even when live delivery drops, and the turn.result query RPC
// (01-safety/05 T5) reads it for disconnected-client recovery.
func (b *MessageBus) TurnOutbox() *TurnOutbox {
	if b == nil {
		return nil
	}
	b.outboxMu.Lock()
	defer b.outboxMu.Unlock()
	if b.turnOutbox == nil {
		b.turnOutbox = NewTurnOutbox(DefaultTurnOutboxCapacity)
	}
	return b.turnOutbox
}

// captureTerminalOutbox records a published critical terminal into the
// bus outbox (publish-side capture, C6). Best-effort by design: capture
// must never block or fail a publish; retention loss is counted in
// TurnOutbox.Stats. Turn/session ids are read from the payload when
// present (turn_id / session_id keys, the turn.terminal contract).
func (b *MessageBus) captureTerminalOutbox(topic string, msg *models.BusMessage) {
	if !IsCriticalTopic(topic) || msg == nil {
		return
	}
	var ids struct {
		TurnID    string `json:"turn_id"`
		SessionID string `json:"session_id"`
		TaskID    string `json:"task_id"`
		PlanID    string `json:"plan_id"`
	}
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &ids); err != nil {
			b.logger.Debug("bus: terminal outbox capture: undecodable payload",
				"topic", topic, "error", err)
		}
	}
	key := ids.TurnID
	if key == "" {
		key = ids.TaskID
	}
	if key == "" {
		key = ids.PlanID
	}
	if key == "" {
		key = msg.ID
	}
	b.TurnOutbox().Record(key, ids.SessionID, topic, msg.Payload)
}
