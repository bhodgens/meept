package models

// ChatModel handlers for the async turn flow (async-turn-migration
// leaf 04). Split out of chat.go to keep the giant Update switch readable;
// all of this is exercised by chat_async_test.go and models/chat_async_test.go.

import (
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/llm"
)

// handleSubmitTurnResult registers an accepted chat.submit ack and spawns
// the await goroutine. It is called from Update on submitTurnResultMsg.
func (m *ChatModel) handleSubmitTurnResult(msg submitTurnResultMsg) tea.Cmd {
	ack := msg.Ack

	// Conversation identity: the ack echoes the daemon-resolved
	// conversation; when absent fall back to the model's own.
	conversationID := ack.ConversationID
	if conversationID == "" {
		conversationID = m.conversationID
	}

	pt, ok := m.turns.register(ack.TurnID, conversationID, ack.SessionID)
	if !ok {
		// Duplicate ack (idempotent retry) — already tracked, never
		// double-register or double-await.
		return nil
	}
	m.pendingTurns = append(m.pendingTurns, pt)

	// F19: a terminal event may have arrived BEFORE this ack registered
	// the turn (WS event raced the submit RPC reply). Consume the
	// buffered event now — with a terminal status it resolves the turn
	// directly; parked keeps it pending.
	if early, buffered := m.earlyTerminals.take(ack.TurnID); buffered {
		if cmd := m.handleTurnTerminal(early); cmd != nil {
			return tea.Batch(
				func() tea.Msg { return turnSubmittedMsg{TurnID: ack.TurnID, ConversationID: conversationID} },
				cmd,
			)
		}
		return func() tea.Msg { return turnSubmittedMsg{TurnID: ack.TurnID, ConversationID: conversationID} }
	}

	return tea.Batch(
		func() tea.Msg { return turnSubmittedMsg{TurnID: ack.TurnID, ConversationID: conversationID} },
		awaitTurnCmd(m.turns, pt, m.livenessTimeout),
	)
}

// handleTurnTerminal renders the terminal result of a submitted turn:
// the reply bubble for completed turns, an error-styled bubble for
// failed/stalled/timeout/parked. Stalled turns get the honest "task may
// still complete" wording and their await state is kept so the real
// terminal result still lands when it arrives; every other status clears
// the turn. The pending progress line is removed for resolved turns.
//
// F21: delivery/persistence is keyed by the turn's OWN conversation id —
// never the currently selected session. A completed turn for another
// conversation is stored into THAT session's transcript and skipped from
// cross-session rendering (the current view must not show another
// session's reply).
func (m *ChatModel) handleTurnTerminal(msg turnTerminalMsg) tea.Cmd {
	pt, tracked := m.turns.get(msg.TurnID)
	if !tracked {
		// Untracked turn (submitted before this migration, or another
		// surface) — nothing to resolve; ignore.
		return nil
	}

	// The turn's own conversation identity wins over the model's
	// currently selected one (F21). The ack may have carried an empty
	// conversation id (fell back to m.conversationID at register time),
	// so re-derive it when the event carries the real value.
	turnConversation := pt.conversationID
	if msg.ConversationID != "" {
		turnConversation = msg.ConversationID
	}
	crossSession := turnConversation != "" && turnConversation != m.conversationID

	// Multiple concurrent turns are resolved independently: only this
	// turn's pending line is removed; other in-flight turns stay visible.
	// Cross-session turns never owned this view's pending line.
	if !crossSession {
		m.removePendingMessageForTurn(pt)
	}

	if msg.Status == turnStatusStalled {
		if crossSession {
			// Stalled notice for another conversation: no rendering
			// here; the turn stays pending and its await stays armed.
			return nil
		}
		// Honest stalled state: the task may still be running. Keep the
		// turn tracked so the real terminal event still renders when it
		// arrives; re-arm the await with the same liveness window.
		text := msg.Error
		if text == "" {
			text = fmt.Sprintf(stalledTurnText, int(m.livenessTimeout.Seconds()))
		}
		m.showPendingTurnLine(pt, text)
		if m.livenessTimeout > 0 {
			return awaitTurnCmd(m.turns, pt, m.livenessTimeout)
		}
		return nil
	}

	if msg.Status == turnStatusParked && msg.Error == "" {
		// Accepted-not-finished (async_dispatch ack): the work continues
		// elsewhere; the real result arrives in a later terminal event
		// with this turn's id. Keep the pending line and re-arm — do not
		// render the ack text as the result (async-turn-migration relay
		// fix, bench gate 2026-09-16). F6: parked never resolves the
		// awaiter (pendingTurn.notify ignores non-terminal statuses), so
		// this re-arm observes the real terminal when it lands.
		if crossSession {
			return nil
		}
		if m.livenessTimeout > 0 {
			return awaitTurnCmd(m.turns, pt, m.livenessTimeout)
		}
		return nil
	}

	m.dropPendingTurn(msg.TurnID)
	m.turns.remove(msg.TurnID)

	// F21: persistence is keyed by the turn's own session — the
	// transcript of the conversation the turn BELONGS to gets the
	// result, whatever view is active. Rendering into the visible
	// transcript happens only when it is the same conversation.
	persistSessionID := m.sessionIDForConversation(turnConversation, pt)

	if msg.Status == turnStatusCompleted && msg.Error == "" {
		if msg.Reply != "" {
			if crossSession {
				// Store into the owning session's transcript without
				// rendering into the currently visible one.
				m.appendToSessionTranscript(persistSessionID, RoleAssistant, msg.Reply)
			} else {
				m.addMessage(RoleAssistant, msg.Reply)
				m.trackDirtyMessage(RoleAssistant, msg.Reply)
			}
		}
	} else {
		// failed / timeout / parked: error-styled, honest bubble.
		detail := msg.Error
		if detail == "" {
			detail = fmt.Sprintf("turn ended with status %q", msg.Status)
		}
		if crossSession {
			m.appendToSessionTranscript(persistSessionID, RoleSystem, detail)
		} else {
			m.addMessage(RoleSystem, llm.UserMessage(fmt.Errorf("%s", detail)))
		}
	}

	if crossSession {
		// The visible view belongs to another conversation: loading and
		// progress state there are not driven by this turn.
		return nil
	}

	// Loading clears only when the last tracked turn resolves — with
	// concurrent turns the input stays in follow-up mode while any turn
	// is outstanding.
	m.loading = m.turns.hasPendingFor(m.conversationID)
	if !m.loading {
		m.progressState = nil
	}
	m.updateViewport()
	return nil
}

// sessionIDForConversation maps a conversation id to the daemon session id
// whose transcript owns its turns (F21). Preference order: the session id
// recorded on the pending turn at submit time (the ack's session_id),
// then the model's own session when the conversation matches, then the
// conversation id itself (the daemon accepts it as the session key for
// relay persistence).
func (m *ChatModel) sessionIDForConversation(conversationID string, pt *pendingTurn) string {
	if pt != nil && pt.sessionID != "" {
		return pt.sessionID
	}
	if conversationID == "" || conversationID == m.conversationID {
		return m.sessionID
	}
	return conversationID
}

// appendToSessionTranscript records a rendered message into a NON-active
// session's stored transcript (F21): it lands in the per-session message
// store and the dirty-persistence buffer so switching to that session (or
// flushing) delivers it, without touching the currently visible view.
func (m *ChatModel) appendToSessionTranscript(sessionID, role, content string) {
	if sessionID == "" {
		return
	}
	msg := ChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}
	// Dirty buffer first: flushMessages persists per session id.
	if m.dirtyMessages == nil {
		m.dirtyMessages = make(map[string][]ChatMessage)
	}
	m.dirtyMessages[sessionID] = append(m.dirtyMessages[sessionID], msg)
	// Per-session transcript store so SetSession renders it on switch.
	if m.sessionMessages == nil {
		m.sessionMessages = make(map[string][]ChatMessage)
	}
	m.sessionMessages[sessionID] = append(m.sessionMessages[sessionID], msg)
}

// showPendingTurnLine ensures a pending transcript line exists for the
// turn and updates its text (the dimmed progress line).
func (m *ChatModel) showPendingTurnLine(pt *pendingTurn, text string) {
	if pt == nil {
		return
	}
	if m.pendingMsgIdx < 0 || m.pendingMsgIdx >= len(m.messages) ||
		m.messages[m.pendingMsgIdx].Role != StatePending {
		m.pendingMsgIdx = len(m.messages)
		m.messages = append(m.messages, ChatMessage{
			Role:      StatePending,
			Content:   text,
			Timestamp: time.Now(),
			State:     MessageNormal,
		})
	} else {
		m.messages[m.pendingMsgIdx].Content = text
	}
	m.updateViewport()
}

// removePendingMessage removes the single legacy pending "sending..."
// message, if any (extracted from the old ChatResponseMsg case).
func (m *ChatModel) removePendingMessage() {
	if m.pendingMsgIdx >= 0 && m.pendingMsgIdx < len(m.messages) &&
		m.messages[m.pendingMsgIdx].Role == StatePending {
		m.messages = append(m.messages[:m.pendingMsgIdx], m.messages[m.pendingMsgIdx+1:]...)
	}
	m.pendingMsgIdx = -1
}

// removePendingMessageForTurn removes the pending line only if it belongs
// to the given turn (per-turn resolution under concurrent turns). The
// single pending slot is shared, so ownership is decided by which turn the
// slot was last claimed by — tracked via pendingTurnOwner.
func (m *ChatModel) removePendingMessageForTurn(pt *pendingTurn) {
	if pt == nil || m.pendingTurnOwner != pt.turnID {
		// The slot belongs to another in-flight turn: leave it alone.
		return
	}
	m.removePendingMessage()
	m.pendingTurnOwner = ""
}

// claimPendingLine records which turn owns the shared pending line slot.
func (m *ChatModel) claimPendingLine(turnID string) {
	m.pendingTurnOwner = turnID
}

// dropPendingTurn forgets a turn from the model's pending list.
func (m *ChatModel) dropPendingTurn(turnID string) {
	for i, pt := range m.pendingTurns {
		if pt.turnID == turnID {
			m.pendingTurns = append(m.pendingTurns[:i], m.pendingTurns[i+1:]...)
			return
		}
	}
}

// clearPendingTurns unregisters every pending turn (F-C). Called when the
// view discards its conversation identity (ctrl+l, Reset): the turns were
// submitted under the OLD conversation, so their terminal events would
// become cross-session deliveries that silently discard the real result.
// Turn ids are removed from the router and the pending list, and buffered
// early terminals are dropped — a terminal arriving after the reset is a
// clean no-op, not a misrouted render.
func (m *ChatModel) clearPendingTurns() {
	for _, pt := range m.pendingTurns {
		m.turns.remove(pt.turnID)
	}
	m.pendingTurns = nil
	m.earlyTerminals = newTurnRouterEarlyBuffer()
}

// earlyTerminalBufferMax is the cap on buffered early terminals. In
// normal operation the buffer holds at most a couple of in-flight races;
// the cap exists so turn ids that never register (e.g. task_completed_relay
// events for fresh ids submitted by another client) cannot grow it
// unboundedly. Oldest entries are evicted first.
const earlyTerminalBufferMax = 64

// earlyTerminalBuffer retains turn.terminal events that arrive BEFORE the
// ack registers the turn (F19): the submit RPC round-trip and the WS
// event race, so a fast daemon can deliver the terminal first. Buffered
// events are consumed on ack registration — never dropped. The map is
// capped (F-B): insert order is tracked and the OLDEST entry is evicted
// when the cap is hit, so ids that never register cannot grow it
// unboundedly.
type turnRouterEarlyBuffer struct {
	mu     sync.Mutex
	events map[string]turnTerminalMsg
	// order preserves insertion order for oldest-first eviction.
	order []string
}

// newTurnRouterEarlyBuffer creates the ephemeral early-event buffer.
func newTurnRouterEarlyBuffer() *turnRouterEarlyBuffer {
	return &turnRouterEarlyBuffer{events: make(map[string]turnTerminalMsg)}
}

// buffer records a terminal event for a turn id not (yet) tracked. When
// the buffer is at capacity the oldest entry is evicted (F-B).
func (b *turnRouterEarlyBuffer) buffer(turnID string, msg turnTerminalMsg) {
	if turnID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.events == nil {
		b.events = make(map[string]turnTerminalMsg)
	}
	if _, exists := b.events[turnID]; !exists {
		b.order = append(b.order, turnID)
		for len(b.order) > earlyTerminalBufferMax {
			oldest := b.order[0]
			b.order = b.order[1:]
			delete(b.events, oldest)
		}
	}
	b.events[turnID] = msg
}

// take removes and returns a buffered event for turnID, if any.
func (b *turnRouterEarlyBuffer) take(turnID string) (turnTerminalMsg, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	msg, ok := b.events[turnID]
	if ok {
		delete(b.events, turnID)
		for i, id := range b.order {
			if id == turnID {
				b.order = append(b.order[:i], b.order[i+1:]...)
				break
			}
		}
	}
	return msg, ok
}

// TurnActivityTimeout is the liveness window derived from the chat
// config: pending turns idle longer than this (with no progress events)
// are considered stalled by the App's stall sweep. Exported for the app
// wiring; the model itself derives it from livenessTimeout.
func (m *ChatModel) TurnActivityTimeout() time.Duration {
	return m.livenessTimeout
}

// HasPendingTurns reports whether any submitted turn is still awaiting its
// terminal event. Exposed for the App wiring (ctrl+c stop-work checks).
func (m *ChatModel) HasPendingTurns() bool {
	return len(m.pendingTurns) > 0
}

// DeliverTurnTerminal routes a turn.terminal bus event (delivered by the
// App's EventStream dispatch) into the await goroutine for the tracked
// turn. Events whose turn id is NOT (yet) tracked are retained in the
// early-event buffer (F19): the submit RPC reply can race the WS event,
// and the buffered terminal is consumed when the ack registers the turn.
func (m *ChatModel) DeliverTurnTerminal(turnID string, payload map[string]any) {
	if turnID == "" {
		return
	}
	pt, ok := m.turns.get(turnID)
	if !ok {
		// F19: buffer the early event — the ack may still be in flight.
		m.earlyTerminals.buffer(turnID, turnTerminalMsg{
			TurnID:         turnID,
			ConversationID: stringFromPayload(payload, "conversation_id"),
			Reply:          stringFromPayload(payload, "reply"),
			Status:         stringFromPayload(payload, "status"),
			Error:          stringFromPayload(payload, "error"),
			DurationMS:     int64FromPayload(payload, "duration_ms"),
		})
		return
	}
	if payload != nil {
		if v, ok := payload["conversation_id"].(string); ok && v != "" {
			pt.mu.Lock()
			// The event carries the authoritative conversation id (F21).
			if pt.conversationID == "" {
				pt.conversationID = v
			}
			pt.mu.Unlock()
		}
	}
	pt.mu.Lock()
	pt.lastActivity = time.Now()
	if reply := stringFromPayload(payload, "reply"); reply != "" {
		pt.progressText = reply
	}
	pt.mu.Unlock()
	pt.notify(turnTerminalMsg{
		TurnID:         turnID,
		ConversationID: pt.conversationID,
		Reply:          stringFromPayload(payload, "reply"),
		Status:         stringFromPayload(payload, "status"),
		Error:          stringFromPayload(payload, "error"),
		DurationMS:     int64FromPayload(payload, "duration_ms"),
	})
}

// DeliverTurnProgress routes a progress text for a conversation into the
// tracked turns of that conversation: it refreshes liveness (so the
// stalled timer does not fire while the daemon is visibly making
// progress — F6: the refresh resets the timer window, not just the
// timestamp) and stores the latest text for the pending line.
func (m *ChatModel) DeliverTurnProgress(conversationID, text string) {
	for _, pt := range m.pendingTurns {
		if pt.conversationID != conversationID && conversationID != "" {
			continue
		}
		pt.bumpActivity()
		if text != "" {
			pt.mu.Lock()
			pt.progressText = text
			pt.mu.Unlock()
		}
	}
}

// SuppressTaskCompletedFor reports whether a task.completed relay chat
// message for conversationID should be IGNORED because a submitted turn is
// still awaiting its terminal event.
//
// DOUBLE-RENDER DEDUPE RULE (binding leaf decision): a completed task turn
// produces BOTH a task.completed relay chat message AND a turn.terminal
// event. The turn.terminal ack carries no task_id, so the correct rule is
// conversation-scoped: once a turn is submitted via SubmitChat for a
// conversation, that conversation's task.completed chat relays are
// suppressed until turnTerminalMsg arrives. The sidebar's task.completed
// handling (task LIST view) is unrelated and never suppressed.
func (m *ChatModel) SuppressTaskCompletedFor(conversationID string) bool {
	if conversationID == "" {
		return false
	}
	return m.turns.hasPendingFor(conversationID)
}

// stringFromPayload extracts a string payload key (bus payloads arrive as
// map[string]any from JSON).
func stringFromPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

// int64FromPayload extracts a numeric payload key as int64.
func int64FromPayload(payload map[string]any, key string) int64 {
	if payload == nil {
		return 0
	}
	if v, ok := payload[key].(float64); ok {
		return int64(v)
	}
	if v, ok := payload[key].(int64); ok {
		return v
	}
	if v, ok := payload[key].(int); ok {
		return int64(v)
	}
	return 0
}

// renderTurnStalledText returns the honest lowercase stalled wording for
// a liveness window (used by tests and the pending line).
func renderTurnStalledText(liveness time.Duration) string {
	return fmt.Sprintf(stalledTurnText, int(liveness.Seconds()))
}
