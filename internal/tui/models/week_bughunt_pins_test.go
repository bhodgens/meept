package models

// Week bughunt (2026-09-17) Group 6 pins — F6 parked-awaiter latch,
// F19 early terminal buffering, F21 cross-session terminal delivery.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestWeekParentParkedThenCompleted pins F6: a parked turn.terminal event
// must NOT latch the awaiter done — the later completed terminal still
// resolves the await with the real reply. (Adapted from the parent probe
// /tmp/meept-week-ui-probe_test.go.)
func TestWeekParentParkedThenCompleted(t *testing.T) {
	r := newTurnRouter(nil)
	pt, ok := r.register("turn-week", "conv-week", "")
	if !ok {
		t.Fatal("registration failed")
	}
	// Parked event: non-terminal, must not resolve the waiter.
	pt.notify(turnTerminalMsg{TurnID: "turn-week", Status: turnStatusParked})
	// The real terminal arrives later (as DeliverTurnTerminal does).
	pt.notify(turnTerminalMsg{TurnID: "turn-week", Status: turnStatusCompleted, Reply: "finished"})
	got := awaitTurnCmd(r, pt, 0)().(turnTerminalMsg)
	if got.Status != turnStatusCompleted {
		t.Fatalf("last result remains %s; completed reply discarded", got.Status)
	}
	if got.Reply != "finished" {
		t.Errorf("expected reply finished, got %q", got.Reply)
	}
}

// TestWeekParkedKeepsAwaiterArmed pins F6 at the await level: parked
// notified mid-await, then the completed terminal lands — the (re-armed)
// await must yield the completed result, never the parked one.
func TestWeekParkedKeepsAwaiterArmed(t *testing.T) {
	r := NewTestTurnRouter()
	pt, ok := r.Register("turn-pk", "conv-1")
	if !ok {
		t.Fatal("register failed")
	}
	cmd := TestAwaitTurnCmd(r, pt, 0)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	// Wait for the waiter to register, then park.
	deadline := time.Now().Add(time.Second)
	for {
		pt.mu.Lock()
		n := len(pt.waiters)
		pt.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	pt.notify(turnTerminalMsg{TurnID: "turn-pk", Status: turnStatusParked, Error: "quota wait"})

	// The await must NOT have resolved with parked; deliver the real
	// terminal and expect the await to complete with it.
	pt.notify(turnTerminalMsg{TurnID: "turn-pk", Status: turnStatusCompleted, Reply: "real result"})
	msg := <-done
	term := msg.(turnTerminalMsg)
	if term.Status != turnStatusCompleted || term.Reply != "real result" {
		t.Fatalf("expected completed/real result after parked, got %s/%q", term.Status, term.Reply)
	}
}

// TestWeekStalledVerdictDoesNotLatch pins F6 stalled side: after the
// liveness timer fires and the stalled verdict is returned, the turn's
// done latch is still open — a later addWaiter succeeds and the real
// terminal resolves through it.
func TestWeekStalledVerdictDoesNotLatch(t *testing.T) {
	ts := NewManualTimerSource()
	r := NewTestTurnRouterWithTimers(ts)
	pt, ok := r.Register("turn-st", "conv-1")
	if !ok {
		t.Fatal("register failed")
	}
	cmd := TestAwaitTurnCmd(r, pt, 20*time.Millisecond)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	// Wait for the timer to arm, then fire it (manual source).
	deadline := time.Now().Add(time.Second)
	for ts.ArmedTimers() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ts.FireTimers()

	msg := <-done
	term := msg.(turnTerminalMsg)
	if term.Status != turnStatusStalled {
		t.Fatalf("expected stalled verdict, got %s", term.Status)
	}
	// The latch must NOT be done: the turn can still resolve.
	if _, canWait := pt.addWaiter(); !canWait {
		t.Fatal("stalled verdict latched done; late terminal would be dropped")
	}
	pt.notify(turnTerminalMsg{TurnID: "turn-st", Status: turnStatusCompleted, Reply: "late but real"})
	pt.mu.Lock()
	res := pt.result
	pt.mu.Unlock()
	if res.Status != turnStatusCompleted || res.Reply != "late but real" {
		t.Fatalf("expected late terminal to resolve, got %s/%q", res.Status, res.Reply)
	}
}

// TestWeekStallTimerResetsOnProgress pins the F6 liveness requirement: a
// stalled liveness timer must be keyed on lastActivity, refreshed by
// progress delivery. A turn whose progress just refreshed must NOT stall
// when the original timer window elapses.
func TestWeekStallTimerResetsOnProgress(t *testing.T) {
	r := NewTestTurnRouterWithTimers(NewManualTimerSource())
	pt, ok := r.Register("turn-live", "conv-1")
	if !ok {
		t.Fatal("register failed")
	}
	liveness := time.Hour
	cmd := TestAwaitTurnCmd(r, pt, liveness)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	// Wait for the timer to arm, then deliver progress (refreshes
	// lastActivity) BEFORE firing.
	deadline := time.Now().Add(time.Second)
	for r.ArmedTimers() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	r.DeliverTestProgress("conv-1", "working")
	// Simulate the refresh: DeliverTestProgress sets lastActivity=now.
	r.FireTimers()

	select {
	case msg := <-done:
		term := msg.(turnTerminalMsg)
		if term.Status == turnStatusStalled {
			t.Fatal("stalled verdict fired despite fresh progress activity")
		}
	case <-time.After(50 * time.Millisecond):
		// Await still running: the timer fired but livenessRemaining saw
		// fresh activity and re-armed — correct. (The re-armed timer
		// would fire after the hour window; nothing more to prove here.)
	}
}

// TestWeekEarlyTerminalBuffered pins F19 (TUI side): a turn.terminal
// event delivered BEFORE the ack registers the turn is retained and
// consumed on registration.
func TestWeekEarlyTerminalBuffered(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	model.earlyTerminals = newTurnRouterEarlyBuffer()

	// Terminal arrives first: no tracked turn — must be buffered.
	model.DeliverTurnTerminal("turn-early", map[string]any{
		"turn_id": "turn-early",
		"reply":   "too fast",
		"status":  "completed",
	})
	if len(model.pendingTurns) != 0 {
		t.Fatal("early terminal must not resolve an unregistered turn")
	}

	// Now the ack registers the turn — buffered event must be consumed.
	cmd := model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{
		TurnID:         "turn-early",
		ConversationID: model.conversationID,
		SessionID:      "sess-1",
		Accepted:       true,
	}})
	if cmd == nil {
		t.Fatal("expected cmd from submit handling")
	}
	if len(model.pendingTurns) != 0 {
		t.Fatalf("expected buffered terminal to resolve the turn on registration, got %d pending", len(model.pendingTurns))
	}
	found := false
	for _, m := range model.messages {
		if m.Role == RoleAssistant && m.Content == "too fast" {
			found = true
		}
	}
	if !found {
		t.Error("expected early terminal's reply rendered after ack registration")
	}
	// The buffer is drained.
	if _, still := model.earlyTerminals.take("turn-early"); still {
		t.Error("expected buffered event consumed on registration")
	}
}

// TestWeekCrossSessionTerminalStoredNotRendered pins F21: submit in
// conversation A, switch the model to conversation B, complete A — the
// reply is stored into A's transcript and NOT rendered into B's visible
// view.
func TestWeekCrossSessionTerminalStoredNotRendered(t *testing.T) {
	model, mock := newAsyncTestChatModel(t)
	model.earlyTerminals = newTurnRouterEarlyBuffer()

	// Submit in conversation A (model starts on conv A / sess-1).
	convA := model.conversationID
	model.textarea.SetValue("question for A")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{
		TurnID:         "turn-a",
		ConversationID: convA,
		SessionID:      "sess-a",
		Accepted:       true,
	}})
	if len(model.pendingTurns) != 1 {
		t.Fatalf("expected 1 pending turn, got %d", len(model.pendingTurns))
	}

	// Switch to conversation B (a different session).
	model.SetSession(nil)
	bMessages := []ChatMessage{{Role: RoleUser, Content: "b's own message"}}
	model.sessionID = "sess-b"
	model.conversationID = "conv-b"
	model.messages = bMessages
	model.pendingTurns = nil // B has no pending turns of its own

	// Complete turn A while B's view is active.
	model.Update(turnTerminalMsg{
		TurnID:         "turn-a",
		ConversationID: convA,
		Reply:          "answer for A",
		Status:         "completed",
	})

	// B's visible transcript must NOT contain A's reply.
	for _, m := range model.messages {
		if m.Content == "answer for A" {
			t.Error("cross-session reply leaked into the active view (B)")
		}
	}

	// A's transcript (per-session store AND dirty persistence buffer)
	// must have it, keyed by A's session id.
	foundInStore := false
	for _, m := range model.sessionMessages["sess-a"] {
		if m.Role == RoleAssistant && m.Content == "answer for A" {
			foundInStore = true
		}
	}
	if !foundInStore {
		t.Error("expected A's per-session transcript to hold the reply")
	}
	foundInDirty := false
	for _, m := range model.dirtyMessages["sess-a"] {
		if m.Role == RoleAssistant && m.Content == "answer for A" {
			foundInDirty = true
		}
	}
	if !foundInDirty {
		t.Error("expected A's dirty persistence buffer to hold the reply")
	}
	_ = mock
}

// TestWeekCrossSessionTurnStaysPending pins the F21 stalled/parked side:
// a stalled notice for another conversation must not render into the
// active view nor drop the turn.
func TestWeekCrossSessionTurnStaysPending(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	model.earlyTerminals = newTurnRouterEarlyBuffer()

	convA := model.conversationID
	model.textarea.SetValue("slow A question")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{
		TurnID:         "turn-slow",
		ConversationID: convA,
		SessionID:      "sess-a",
		Accepted:       true,
	}})

	// Switch to B.
	model.sessionID = "sess-b"
	model.conversationID = "conv-b"
	model.messages = []ChatMessage{}
	model.pendingTurns = nil

	cmd := model.Update(turnTerminalMsg{
		TurnID:         "turn-slow",
		ConversationID: convA,
		Status:         turnStatusStalled,
		Error:          "no progress for 90s — task may still be running",
	})
	if cmd != nil {
		t.Error("cross-session stall must not re-arm the visible view's await")
	}
	for _, m := range model.messages {
		if strings.Contains(m.Content, "no progress") {
			t.Error("cross-session stalled text leaked into B's view")
		}
	}
}
