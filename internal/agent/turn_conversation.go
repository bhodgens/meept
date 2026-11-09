package agent

// turn_conversation.go — the per-turn parent-conversation handle (DSH-1
// fork-seed). RunOnceWithParts arms the handle with the turn's live
// *Conversation next to the working-directory injection; executeToolCalls
// reads it into the tool context (tools.ContextWithConversation) so
// delegate_task's fork spawn can slice the REAL parent transcript at the
// spawn boundary. Turn-scoped like the reply-guard state (resetTurnGuards
// class): a stale handle must never outlive its turn.
//
// Concurrency: the field is written once per turn before any tool executes
// on that turn and read on the same turn's tool path, so a plain field
// would be race-free in practice — but tool execution can overlap the NEXT
// turn's arming on async turns, so the guard stays under l.mu like every
// other per-turn guard field (loop.go precedent).

import (
	"context"
)

// setTurnConversation arms the per-turn parent-conversation handle with the
// running turn's conversation. A nil conversation clears the handle
// (fail-safe: no carrier = no fork-seed source).
func (l *AgentLoop) setTurnConversation(conv *Conversation) {
	l.mu.Lock()
	l.turnConversation = conv
	l.mu.Unlock()
}

// turnConversationHandle reads the armed handle (nil when no turn is armed
// or the caller is a legacy/unwired path). Nil-safe on a nil loop.
func (l *AgentLoop) turnConversationHandle() *Conversation {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.turnConversation
}

// injectTurnConversation is called from RunOnceWithParts after the
// conversation is resolved: it arms the handle and registers its own
// clearing on turn exit via the returned func (deferred by the caller
// alongside the existing MarkInactive defer).
func (l *AgentLoop) injectTurnConversation(ctx context.Context, conv *Conversation) func() {
	l.setTurnConversation(conv)
	return func() { l.setTurnConversation(nil) }
}
