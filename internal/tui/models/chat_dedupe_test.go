package models

// 04-clutter TEST-GATE T6 (F11b): TUI task-card dedupe pin.
//
// Frozen defect: notes-server 2.4 + F-ORCH-5 double-render. A completed
// task turn produces BOTH a task.completed relay (rendered as a styled
// "task completed" CARD via ChatTaskResultMsg / parseTaskResult) AND a
// turn.terminal event (rendered as the reply BUBBLE). The live TUI showed
// both for one turn (probes/tui-2plus2.json: the card AND the
// "## task completed: what is 2+2? / ### steps: / result: ..." block).
//
// Today's gate is SuppressTaskCompletedFor: conversation-scoped and
// PENDING-ONLY - it suppresses the relay card only while a turn is still
// awaiting its terminal. Once the terminal lands first (or no turn was
// submitted through this view), the card renders again next to the bubble.
//
// DEDUPE CONTRACT (pinned in BOTH client halves, master C1): a task reply
// renders ONCE keyed by (turn_id, task_id). The card and the terminal
// bubble are the SAME logical reply - whichever arrives first wins, the
// second is suppressed. A late terminal after a stalled notice still
// renders (dedupe must NEVER suppress a real terminal). Until
// 05-navigation/01 lands TaskID on turnTerminalMsg, key on turn_id alone
// (defensive); both shapes are pinned here.
//
// RED today: terminal-then-card and card-then-terminal both double-render.
// GREEN after 04-clutter/02 T2.

import (
	"reflect"
	"strings"
	"testing"
)

// dedupeReply is the logical task answer shared by the card and the
// terminal in every subtest - one reply, two delivery shapes.
const dedupeReply = "the answer is 4."

// registerDedupeTurn submits a turn through the real ack path so the turn
// router tracks it (the same wiring chat_async_test.go uses).
func registerDedupeTurn(t *testing.T, m *ChatModel, turnID string) {
	t.Helper()
	m.Update(submitTurnResultMsg{Ack: TurnSubmitAck{
		TurnID:         turnID,
		ConversationID: m.conversationID,
		SessionID:      m.sessionID,
		Accepted:       true,
	}})
}

// countReplyRenders counts transcript messages carrying the logical reply:
// assistant bubbles with the exact text, plus styled task cards (the card
// truncates its result summary, so match on the card marker plus a stable
// prefix of the reply rather than the full text).
func countReplyRenders(m *ChatModel, reply string) int {
	prefix := reply
	if len(prefix) > 20 {
		prefix = prefix[:20]
	}
	n := 0
	for _, msg := range m.messages {
		if msg.Role == RoleAssistant && msg.Content == reply {
			n++
		}
		if msg.Role == RoleSystem && strings.Contains(msg.Content, "task completed") && strings.Contains(msg.Content, prefix) {
			n++
		}
	}
	return n
}

// countTaskCards counts styled task-completed cards regardless of content.
func countTaskCards(m *ChatModel) int {
	n := 0
	for _, msg := range m.messages {
		if msg.Role == RoleSystem && strings.Contains(msg.Content, "task completed") {
			n++
		}
	}
	return n
}

// TestTUITaskCardDedupe pins the (turn_id, task_id) dedupe contract in the
// TUI. RED today: the pending-only gate lets the second delivery render.
func TestTUITaskCardDedupe(t *testing.T) {
	t.Run("terminal_then_card_renders_once", func(t *testing.T) {
		m, _ := newAsyncTestChatModel(t)
		registerDedupeTurn(t, m, "turn-dedupe-1")

		// The terminal bubble lands first (the normal async order).
		m.Update(turnTerminalMsg{
			TurnID:         "turn-dedupe-1",
			ConversationID: m.conversationID,
			Reply:          dedupeReply,
			Status:         turnStatusCompleted,
		})
		// The task.completed relay card arrives AFTER the terminal - the
		// pending-only gate is already lifted, so today it renders too.
		m.Update(ChatTaskResultMsg{
			TaskID:         "task-dedupe-1",
			TaskName:       "what is 2+2?",
			State:          StateCompleted,
			CompletedSteps: 1,
			TotalSteps:     1,
			ResultSummary:  dedupeReply,
		})

		if got := countReplyRenders(m, dedupeReply); got != 1 {
			t.Fatalf("double render: the logical reply rendered %d times (terminal bubble + task card), want exactly 1 "+
				"(dedupe key (turn_id, task_id); second delivery suppressed)", got)
		}
	})

	t.Run("card_then_terminal_renders_once", func(t *testing.T) {
		m, _ := newAsyncTestChatModel(t)
		registerDedupeTurn(t, m, "turn-dedupe-2")

		// The relay card lands first (raced ahead of the terminal).
		m.Update(ChatTaskResultMsg{
			TaskID:         "task-dedupe-2",
			TaskName:       "what is 2+2?",
			State:          StateCompleted,
			CompletedSteps: 1,
			TotalSteps:     1,
			ResultSummary:  dedupeReply,
		})
		// The terminal arrives second: its bubble is the SAME logical
		// reply and must be suppressed (whichever arrives first wins).
		m.Update(turnTerminalMsg{
			TurnID:         "turn-dedupe-2",
			ConversationID: m.conversationID,
			Reply:          dedupeReply,
			Status:         turnStatusCompleted,
		})

		if got := countReplyRenders(m, dedupeReply); got != 1 {
			t.Fatalf("double render: card-first then terminal rendered the reply %d times, want exactly 1", got)
		}
	})

	t.Run("relay_json_card_after_terminal_renders_once", func(t *testing.T) {
		// The other card delivery shape: a task.completed JSON relay via
		// ChatResponseMsg (parseTaskResult). Same dedupe contract.
		m, _ := newAsyncTestChatModel(t)
		registerDedupeTurn(t, m, "turn-dedupe-3")

		m.Update(turnTerminalMsg{
			TurnID:         "turn-dedupe-3",
			ConversationID: m.conversationID,
			Reply:          dedupeReply,
			Status:         turnStatusCompleted,
		})
		m.Update(ChatResponseMsg{Reply: `{"task_completed":true,"task_id":"task-dedupe-3","name":"what is 2+2?","completed":1,"total":1,"result":"` + dedupeReply + `"}`})

		if got := countReplyRenders(m, dedupeReply); got != 1 {
			t.Fatalf("double render: terminal bubble + JSON relay card rendered the reply %d times, want exactly 1", got)
		}
		if got := countTaskCards(m); got > 1 {
			t.Fatalf("more than one task card rendered: %d", got)
		}
	})

	t.Run("late_terminal_after_stalled_still_renders", func(t *testing.T) {
		// REGRESSION PIN (must never break): a stalled notice is NOT a
		// terminal - the real terminal that lands later must still render
		// even though dedupe state exists for the turn. Dedupe suppresses
		// DUPLICATES, never a late real terminal.
		m, _ := newAsyncTestChatModel(t)
		registerDedupeTurn(t, m, "turn-dedupe-late")

		// The liveness window fires: honest stalled notice, turn stays
		// tracked (handleTurnTerminal keeps the await armed).
		m.Update(turnTerminalMsg{
			TurnID:         "turn-dedupe-late",
			ConversationID: m.conversationID,
			Status:         turnStatusStalled,
		})
		// The real terminal lands late.
		m.Update(turnTerminalMsg{
			TurnID:         "turn-dedupe-late",
			ConversationID: m.conversationID,
			Reply:          dedupeReply,
			Status:         turnStatusCompleted,
		})

		if got := countReplyRenders(m, dedupeReply); got != 1 {
			t.Fatalf("late terminal after stalled rendered %d times, want exactly 1 - dedupe must never suppress a late real terminal", got)
		}
	})

	t.Run("distinct_turns_both_render", func(t *testing.T) {
		// Dedupe is per (turn_id, task_id): two DIFFERENT turns carrying
		// the same answer text must both render (no content-based dedupe).
		m, _ := newAsyncTestChatModel(t)
		registerDedupeTurn(t, m, "turn-dedupe-a")
		registerDedupeTurn(t, m, "turn-dedupe-b")

		m.Update(turnTerminalMsg{TurnID: "turn-dedupe-a", ConversationID: m.conversationID, Reply: dedupeReply, Status: turnStatusCompleted})
		m.Update(turnTerminalMsg{TurnID: "turn-dedupe-b", ConversationID: m.conversationID, Reply: dedupeReply, Status: turnStatusCompleted})

		if got := countReplyRenders(m, dedupeReply); got != 2 {
			t.Fatalf("two distinct turns rendered %d times, want 2 - dedupe keys on turn/task id, not content", got)
		}
	})

	t.Run("dedupe_key_carries_task_id_when_present", func(t *testing.T) {
		// C1 contract: the dedupe key is (turn_id, task_id). Until
		// 05-navigation/01 lands TaskID on turnTerminalMsg the TUI keys on
		// turn_id alone (defensive). Pin the field's existence through
		// reflection (compiles before it lands): RED until the terminal
		// carries task_id so the full key can be formed. The defensive
		// turn_id-only dedupe is covered by terminal_then_card above and
		// must keep working both before and after the field lands.
		field := reflect.ValueOf(turnTerminalMsg{}).FieldByName("TaskID")
		if !field.IsValid() || field.Kind() != reflect.String {
			t.Fatal("turnTerminalMsg has no string TaskID field (05-navigation/01 adds it via C1); " +
				"the full (turn_id, task_id) dedupe key cannot be formed - defensive turn_id-only is interim")
		}

		// Once the field exists, a same-turn terminal+card pair carrying
		// the SAME task_id must still render once (the full key agrees
		// with the defensive key on the common case).
		m, _ := newAsyncTestChatModel(t)
		registerDedupeTurn(t, m, "turn-dedupe-key-1")
		term := turnTerminalMsg{
			TurnID:         "turn-dedupe-key-1",
			ConversationID: m.conversationID,
			Reply:          dedupeReply,
			Status:         turnStatusCompleted,
		}
		reflect.ValueOf(&term).Elem().FieldByName("TaskID").SetString("task-dedupe-key-X")
		m.Update(term)
		m.Update(ChatTaskResultMsg{
			TaskID:         "task-dedupe-key-X",
			TaskName:       "what is 2+2?",
			State:          StateCompleted,
			CompletedSteps: 1,
			TotalSteps:     1,
			ResultSummary:  dedupeReply,
		})
		if got := countReplyRenders(m, dedupeReply); got != 1 {
			t.Fatalf("same-turn full-key (turn_id, task_id) pair rendered %d times, want 1", got)
		}
	})
}
