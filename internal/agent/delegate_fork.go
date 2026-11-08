package agent

// delegate_fork.go implements the B2 history-seeded fork seam: a pure
// completedTurnPrefix that slices the parent conversation's messages through
// its last COMPLETED turn, wired into DelegateManager.SetForkSeed so
// Mode:"fork" spawns a child whose context begins with that snapshot.
//
// Contract: .hermes/contracts/01-delegation.md B2; leaf
// docs/plans/20261102-dsh-convergence/01-delegation/02-fork-seed.md.
//
// meept's conversation message list carries no explicit turn/end marker (the
// turn_end bus event lives separately in events.go); a completed turn is
// therefore identified STRUCTURALLY: a prefix is balanced when it ends on an
// assistant response with no unsettled tool call, i.e. every assistant
// ToolCall is matched by a following tool-role result. This is the one
// deliberate deviation from dsh's event-based turn/end
// (subagent-fork-in-process/index.ts:46-53): a future versioned-session-log
// (branch 03) can tighten the boundary. An in-flight (unbalanced) turn — a
// trailing assistant tool-call without its results, or a trailing user
// message with no assistant reply — is excluded so the seed is deterministic
// and replayable.

import "github.com/caimlas/meept/internal/llm"

// lastCompletedTurn returns the number of leading messages that belong to a
// completed (balanced) turn, i.e. the cut index one past the last assistant
// message under which every tool call issued in the prefix is settled by a
// following tool-role result. It returns -1 when no turn has completed.
//
// The scan walks forward tracking outstanding tool-call IDs; a position is a
// valid cut exactly when the message at it is an assistant response and no
// call is still outstanding. A tool-role result is never itself a cut (the
// turn is not completed until the assistant answers after the result), and a
// user message is never a cut (a turn starts with a user message, so a
// trailing user is an un-answered in-flight turn).
func lastCompletedTurn(msgs []llm.ChatMessage) int {
	outstanding := make(map[string]struct{}, 8)
	cut := -1
	for i, m := range msgs {
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				outstanding[tc.ID] = struct{}{}
			}
		}
		if m.Role == llm.RoleTool && m.ToolCallID != "" {
			if _, ok := outstanding[m.ToolCallID]; ok {
				delete(outstanding, m.ToolCallID)
			}
		}
		if m.Role == llm.RoleAssistant && len(outstanding) == 0 {
			cut = i + 1
		}
	}
	return cut
}

// completedTurnPrefix returns the parent's messages up to AND INCLUDING its
// last completed turn; the in-flight (unbalanced) turn is excluded, matching
// dsh's subagent-fork-in-process/index.ts:46-53. It returns nil when no turn
// has completed (the child starts fresh) or the conversation is nil.
//
// The returned slice is a COPY, so a later parent append can never mutate the
// child's seed. The seed is a stable snapshot captured at spawn (capture-once,
// B2): cold resume replays this prefix instead of re-forking the parent's
// newer history (leaf 06 persists it in the child descriptor).
func completedTurnPrefix(conv *Conversation) []llm.ChatMessage {
	if conv == nil {
		return nil
	}
	msgs := conv.GetMessages()
	n := lastCompletedTurn(msgs)
	if n <= 0 {
		return nil
	}
	if n > len(msgs) {
		n = len(msgs)
	}
	return append([]llm.ChatMessage(nil), msgs[:n]...)
}
