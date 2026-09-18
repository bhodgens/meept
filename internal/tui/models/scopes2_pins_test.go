package models

// Regression pins for the scopes-2 audit findings F-B (early-terminal
// buffer unbounded growth) and F-C (ctrl+l / Reset orphan pending turns).

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestWeekEarlyTerminalBufferCapped pins F-B: buffering more turn ids
// than earlyTerminalBufferMax evicts the OLDEST entries, so ids that
// never register cannot grow the map unboundedly. Freshly buffered ids
// and the oldest survivor must still be consumable.
func TestWeekEarlyTerminalBufferCapped(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	model.earlyTerminals = newTurnRouterEarlyBuffer()

	total := earlyTerminalBufferMax + 16
	for i := range total {
		id := fmt.Sprintf("turn-%03d", i)
		model.earlyTerminals.buffer(id, turnTerminalMsg{
			TurnID: id,
			Reply:  "reply " + id,
			Status: turnStatusCompleted,
		})
	}

	// The oldest ids were evicted: the first earlyTerminalBufferMax ids
	// are gone.
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("turn-%03d", i)
		if _, still := model.earlyTerminals.take(id); still {
			t.Errorf("oldest buffered id %q should have been evicted at cap", id)
		}
	}
	// The newest earlyTerminalBufferMax ids survive, in order.
	for i := 16; i < total; i++ {
		id := fmt.Sprintf("turn-%03d", i)
		msg, ok := model.earlyTerminals.take(id)
		if !ok {
			t.Fatalf("recently buffered id %q must survive eviction", id)
		}
		if msg.Reply != "reply "+id {
			t.Errorf("id %q: reply = %q", id, msg.Reply)
		}
	}

	// A consumed buffer refills cleanly: buffer exactly the cap again and
	// every id — including the first — must survive.
	for i := total; i < total+earlyTerminalBufferMax; i++ {
		id := fmt.Sprintf("turn-%03d", i)
		model.earlyTerminals.buffer(id, turnTerminalMsg{TurnID: id})
	}
	if _, ok := model.earlyTerminals.take(fmt.Sprintf("turn-%03d", total)); !ok {
		t.Fatal("expected the first post-take buffered id to survive")
	}
}

// TestWeekCtrlLOrphansPendingTurns pins F-C (ctrl+l path): a pending
// turn followed by ctrl+l unregisters the turn; the late terminal event
// is a clean drop — no render, no panic, nothing left pending.
func TestWeekCtrlLOrphansPendingTurns(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	model.earlyTerminals = newTurnRouterEarlyBuffer()

	convA := model.conversationID
	model.textarea.SetValue("question before clear")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{
		TurnID:         "turn-clear",
		ConversationID: convA,
		SessionID:      "sess-1",
		Accepted:       true,
	}})
	if len(model.pendingTurns) != 1 {
		t.Fatalf("expected 1 pending turn, got %d", len(model.pendingTurns))
	}

	// ctrl+l clears the transcript and the conversation identity.
	model.Update(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	if len(model.pendingTurns) != 0 {
		t.Fatalf("ctrl+l must unregister pending turns, got %d", len(model.pendingTurns))
	}
	if model.conversationID == convA {
		t.Fatal("ctrl+l must rotate the conversation id")
	}

	// The late terminal event arrives after the clear: it belongs to the
	// OLD conversation — a clean drop, not a render into the fresh view
	// and not a panic.
	model.DeliverTurnTerminal("turn-clear", map[string]any{
		"turn_id":         "turn-clear",
		"conversation_id": convA,
		"reply":           "late answer",
		"status":          turnStatusCompleted,
	})
	if len(model.pendingTurns) != 0 {
		t.Fatalf("late terminal must not register a turn, got %d pending", len(model.pendingTurns))
	}
	for _, m := range model.messages {
		if m.Content == "late answer" {
			t.Error("late terminal for a cleared turn leaked into the fresh view")
		}
	}
}

// TestWeekResetOrphansPendingTurns pins F-C (Reset path): Reset
// unregisters pending turns so a later terminal is a clean drop.
func TestWeekResetOrphansPendingTurns(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	model.earlyTerminals = newTurnRouterEarlyBuffer()

	convA := model.conversationID
	model.textarea.SetValue("question before reset")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{
		TurnID:         "turn-reset",
		ConversationID: convA,
		SessionID:      "sess-1",
		Accepted:       true,
	}})
	if len(model.pendingTurns) != 1 {
		t.Fatalf("expected 1 pending turn, got %d", len(model.pendingTurns))
	}

	model.Reset()
	if len(model.pendingTurns) != 0 {
		t.Fatalf("Reset must unregister pending turns, got %d", len(model.pendingTurns))
	}

	// The late terminal is a clean drop: no panic, no render.
	model.DeliverTurnTerminal("turn-reset", map[string]any{
		"turn_id":         "turn-reset",
		"conversation_id": convA,
		"reply":           "late answer",
		"status":          turnStatusCompleted,
	})
	if len(model.pendingTurns) != 0 {
		t.Fatalf("late terminal must not register a turn, got %d pending", len(model.pendingTurns))
	}
	for _, m := range model.messages {
		if m.Content == "late answer" {
			t.Error("late terminal for a reset turn leaked into the fresh view")
		}
	}
}
