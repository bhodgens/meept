package models

// ChatModel handlers for the async turn flow (async-turn-migration
// leaf 04). Split out of chat.go to keep the giant Update switch readable;
// all of this is exercised by chat_async_test.go and models/chat_async_test.go.

import (
	"fmt"
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

	pt, ok := m.turns.register(ack.TurnID, conversationID)
	if !ok {
		// Duplicate ack (idempotent retry) — already tracked, never
		// double-register or double-await.
		return nil
	}
	m.pendingTurns = append(m.pendingTurns, pt)

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
func (m *ChatModel) handleTurnTerminal(msg turnTerminalMsg) tea.Cmd {
	pt, tracked := m.turns.get(msg.TurnID)
	if !tracked {
		// Untracked turn (submitted before this migration, or another
		// surface) — nothing to resolve; ignore.
		return nil
	}

	// Multiple concurrent turns are resolved independently: only this
	// turn's pending line is removed; other in-flight turns stay visible.
	m.removePendingMessageForTurn(pt)

	if msg.Status == turnStatusStalled {
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
		// fix, bench gate 2026-09-16).
		if m.livenessTimeout > 0 {
			return awaitTurnCmd(m.turns, pt, m.livenessTimeout)
		}
		return nil
	}

	m.dropPendingTurn(msg.TurnID)
	m.turns.remove(msg.TurnID)

	if msg.Status == turnStatusCompleted && msg.Error == "" {
		if msg.Reply != "" {
			m.addMessage(RoleAssistant, msg.Reply)
			m.trackDirtyMessage(RoleAssistant, msg.Reply)
		}
	} else {
		// failed / timeout / parked: error-styled, honest bubble.
		detail := msg.Error
		if detail == "" {
			detail = fmt.Sprintf("turn ended with status %q", msg.Status)
		}
		m.addMessage(RoleSystem, llm.UserMessage(fmt.Errorf("%s", detail)))
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
// turn. It is safe to call for events that match no tracked turn — those
// are ignored (turn_id filtering).
func (m *ChatModel) DeliverTurnTerminal(turnID string, payload map[string]any) {
	if turnID == "" {
		return
	}
	pt, ok := m.turns.get(turnID)
	if !ok {
		return
	}
	pt.progressText = stringFromPayload(payload, "reply")
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
// progress) and stores the latest text for the pending line.
func (m *ChatModel) DeliverTurnProgress(conversationID, text string) {
	now := time.Now()
	for _, pt := range m.pendingTurns {
		if pt.conversationID != conversationID && conversationID != "" {
			continue
		}
		pt.mu.Lock()
		pt.lastActivity = now
		if text != "" {
			pt.progressText = text
		}
		pt.mu.Unlock()
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
