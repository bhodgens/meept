package models

// Task-reply render dedupe (04-clutter/02, gate T6; DEDUPE CONTRACT C1).
//
// A completed task turn arrives at the chat view as TWO deliveries of the
// SAME logical reply: the task.completed relay (styled CARD via
// ChatTaskResultMsg / parseTaskResult) and the turn.terminal event (reply
// BUBBLE). The frozen defect (notes-server 2.4 + F-ORCH-5): both rendered
// for one turn. The pre-existing gate (SuppressTaskCompletedFor) is
// pending-only -- once the terminal lands first, the card renders again.
//
// Rule: first delivery wins, the second is suppressed, keyed by
// (turn_id, task_id). The terminal carries no task id until C1 lands the
// field on the wire (turnTerminalMsg.TaskID is read defensively -- the
// ledger records it whenever non-empty), so the interim association
// rules are:
//   - terminal-first: a completed terminal records the reply text for the
//     active conversation; a card whose ResultSummary equals it (or whose
//     task id was already rendered) is suppressed;
//   - card-first: a rendered card with a tracked pending turn binds the
//     summary to that turn id; the matching terminal bubble is suppressed
//     (the turn still RESOLVES -- pending line removed, loading cleared);
//   - full key: task ids recorded on either side suppress each other.
//
// Never-suppressed: a late real terminal after a stalled notice (a
// stalled verdict renders no reply and records nothing), and a second
// DISTINCT turn carrying the same answer text (content checks are scoped
// to card-vs-terminal pairing of ONE conversation/turn, never global).

// noteTaskCardRendered records a card that just rendered: its task id
// goes in the rendered ledger, and its summary binds to any tracked
// pending turn of this conversation so the terminal that follows is
// recognized as the same logical reply (card-first order).
func (m *ChatModel) noteTaskCardRendered(taskID, summary string) {
	if m.renderedTaskIDs == nil {
		m.renderedTaskIDs = make(map[string]bool)
	}
	if taskID != "" {
		m.renderedTaskIDs[taskID] = true
	}
	if summary == "" {
		return
	}
	if m.turnCardReplies == nil {
		m.turnCardReplies = make(map[string]string)
	}
	for _, pt := range m.pendingTurns {
		if pt == nil {
			continue
		}
		pt.mu.Lock()
		conv := pt.conversationID
		pt.mu.Unlock()
		if conv == "" || conv == m.conversationID {
			m.turnCardReplies[pt.turnID] = summary
		}
	}
}

// duplicateTaskCard reports whether a task.completed card is the second
// delivery of an already-rendered reply: its task id rendered before, or
// (terminal-first order) its summary equals the last reply the terminal
// path rendered into this conversation.
func (m *ChatModel) duplicateTaskCard(taskID, summary string) bool {
	if taskID != "" && m.renderedTaskIDs[taskID] {
		return true
	}
	if summary != "" && m.lastTerminalReply != "" && summary == m.lastTerminalReply {
		return true
	}
	return false
}

// noteTerminalReplyRendered records that the terminal path rendered this
// reply bubble (either delivery order), so a later card for the same task
// id (full C1 key) or the same summary (interim, terminal-first order) is
// recognized as a duplicate.
func (m *ChatModel) noteTerminalReplyRendered(reply, taskID string) {
	if m.renderedTaskIDs == nil {
		m.renderedTaskIDs = make(map[string]bool)
	}
	if taskID != "" {
		m.renderedTaskIDs[taskID] = true
	}
	m.lastTerminalReply = reply
}

// duplicateTerminalReply reports whether the terminal bubble is the
// second delivery of a reply the card already rendered (card-first
// order): the full (turn_id, task_id) key matches a rendered task id, or
// this turn was bound to the card's summary when it rendered. A stalled
// notice records no binding, so the real terminal that lands late is
// NEVER suppressed by this predicate.
func (m *ChatModel) duplicateTerminalReply(turnID, reply, taskID string) bool {
	if reply == "" {
		return false
	}
	if taskID != "" && m.renderedTaskIDs[taskID] {
		return true
	}
	if m.turnCardReplies != nil {
		if bound, ok := m.turnCardReplies[turnID]; ok && bound == reply {
			delete(m.turnCardReplies, turnID)
			return true
		}
	}
	return false
}
