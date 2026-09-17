package models

// Async turn migration (leaf 04) — chat-view wiring tests: submit action
// produces the submit cmd; turnSubmittedMsg tracks the turn; progress
// updates the pending line (latest wins); turnTerminalMsg renders the
// single result bubble; failed/stalled render error-styled bubbles with
// honest lowercase text; concurrent turns resolve independently; the
// task.completed dedupe suppresses relay bubbles while a turn is pending.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// newAsyncTestChatModel builds a chat model with an accepted-submit mock
// and a manual timer router for deterministic liveness tests.
func newAsyncTestChatModel(t *testing.T) (*ChatModel, *MockChatRPCClient) {
	t.Helper()
	mock := NewMockChatRPCClient()
	mock.SubmitAccepted = true
	mock.SubmitTurnID = "turn-1"
	mock.SubmitConversationID = ""
	mock.SubmitSessionID = "sess-1"
	userStyle := lipgloss.NewStyle()
	model := NewChatModelWithConfig(mock, userStyle, userStyle, userStyle, "once",
		InputBehaviorConfig{EnterBehavior: "shift_sends"}, ChatConfig{
			ScrollSpeed:     3,
			LivenessTimeout: 90 * time.Second,
		})
	model.SetSize(80, 24)
	model.sessionID = "sess-1"
	model.turns = newTurnRouter(NewManualTimerSource())
	return model, mock
}

// drainBatchMsgs executes cmd and returns every concrete message produced
// (flattening tea.BatchMsg one level, matching bubbletea runtime behavior).
func drainBatchMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			if c == nil {
				continue
			}
			if sub := c(); sub != nil {
				out = append(out, sub)
			}
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func hasMsg[T tea.Msg](msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(T); ok {
			return true
		}
	}
	return false
}

func extractMsg[T tea.Msg](msgs []tea.Msg) (T, bool) {
	for _, m := range msgs {
		if v, ok := m.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// pendingMessages returns the transcript's pending-role messages.
func pendingMessages(m *ChatModel) []ChatMessage {
	var out []ChatMessage
	for _, msg := range m.messages {
		if msg.Role == StatePending {
			out = append(out, msg)
		}
	}
	return out
}

// TestChatModel_SubmitProducesCmd verifies the submit action returns a cmd
// that yields submitTurnResultMsg (never a blocking ChatResponseMsg).
func TestChatModel_SubmitProducesCmd(t *testing.T) {
	model, mock := newAsyncTestChatModel(t)
	model.textarea.SetValue("hello async")

	cmd := model.doSendMessage()
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	msgs := drainBatchMsgs(cmd)
	if !hasMsg[submitTurnResultMsg](msgs) {
		t.Fatalf("expected submitTurnResultMsg in %T batch", msgs)
	}
	if hasMsg[ChatResponseMsg](msgs) {
		t.Error("expected NO ChatResponseMsg on the async path")
	}
	if len(mock.SubmitCalls) != 1 {
		t.Fatalf("expected 1 SubmitChat call, got %d", len(mock.SubmitCalls))
	}
	if mock.SubmitCalls[0] != "hello async" {
		t.Errorf("expected message forwarded, got %q", mock.SubmitCalls[0])
	}
	if mock.SubmitSessionIDSent != "sess-1" {
		t.Errorf("expected session_id sess-1, got %q", mock.SubmitSessionIDSent)
	}
}

// TestChatModel_SubmitRejected surfaces an accepted=false ack as an error
// bubble and clears the pending line.
func TestChatModel_SubmitRejected(t *testing.T) {
	model, mock := newAsyncTestChatModel(t)
	mock.SubmitAccepted = false
	mock.SubmitNote = "message is required"

	model.textarea.SetValue("should be rejected")
	cmd := model.doSendMessage()
	msgs := drainBatchMsgs(cmd)
	if !hasMsg[ChatSubmitErrorMsg](msgs) {
		t.Fatal("expected ChatSubmitErrorMsg for rejected submit")
	}

	// Feed the error through Update: pending clears, error bubble renders.
	model.Update(ChatSubmitErrorMsg{Err: errors.New("message is required")})
	if model.loading {
		t.Error("expected loading=false after rejected submit")
	}
	if len(pendingMessages(model)) != 0 {
		t.Errorf("expected pending line cleared, got %d", len(pendingMessages(model)))
	}
	found := false
	for _, m := range model.messages {
		if m.Role == RoleSystem && strings.Contains(m.Content, "message is required") {
			found = true
		}
	}
	if !found {
		t.Error("expected error bubble with the daemon's note")
	}
}

// TestChatModel_TurnSubmittedAndTracked verifies the ack registers the
// turn and the returned batch carries the await cmd.
func TestChatModel_TurnSubmittedAndTracked(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	model.textarea.SetValue("track me")

	cmd := model.doSendMessage()
	msgs := drainBatchMsgs(cmd)
	sub, ok := extractMsg[submitTurnResultMsg](msgs)
	if !ok {
		t.Fatal("expected submitTurnResultMsg")
	}

	awaitCmd := model.Update(sub)
	if awaitCmd == nil {
		t.Fatal("expected await cmd from Update(submitTurnResultMsg)")
	}
	if len(model.pendingTurns) != 1 {
		t.Fatalf("expected 1 tracked turn, got %d", len(model.pendingTurns))
	}
	if model.pendingTurns[0].turnID != "turn-1" {
		t.Errorf("expected turn-1 tracked, got %q", model.pendingTurns[0].turnID)
	}

	// The returned command must be a Batch carrying the turnSubmittedMsg
	// cmd AND the await goroutine cmd. The await cmd BLOCKS until the
	// terminal arrives — executing it here would hang the test, so only
	// its presence is asserted; the stalled/terminal await behavior is
	// covered by the router-level tests in internal/tui/chat_async_test.go.
	batch, ok := awaitCmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg from submit handling, got %T", awaitCmd())
	}
	if len(batch) != 2 {
		t.Fatalf("expected 2 cmds (turnSubmitted + await), got %d", len(batch))
	}
	msg := batch[0]()
	submitted, ok := msg.(turnSubmittedMsg)
	if !ok {
		t.Fatalf("expected turnSubmittedMsg as first batch member, got %T", msg)
	}
	if submitted.TurnID != "turn-1" {
		t.Errorf("expected turnSubmittedMsg.TurnID turn-1, got %q", submitted.TurnID)
	}
	if model.Update(submitted) != nil {
		t.Error("expected turnSubmittedMsg Update to be a no-op")
	}
}

// TestChatModel_ProgressLatestWins verifies turnProgressMsg updates the
// pending line's text, lowercase, with the latest text winning.
func TestChatModel_ProgressLatestWins(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)

	// Submit + ack to create the pending line.
	model.textarea.SetValue("progress please")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{TurnID: "turn-1", ConversationID: "conv-own", Accepted: true}})

	model.Update(turnProgressMsg{ConversationID: "conv-own", Text: "reading files"})
	pending := pendingMessages(model)
	if len(pending) == 0 {
		t.Fatal("expected a pending line")
	}
	if !strings.Contains(pending[len(pending)-1].Content, "reading files") {
		t.Errorf("expected pending line to carry progress text, got %q", pending[len(pending)-1].Content)
	}

	// Latest wins.
	model.Update(turnProgressMsg{ConversationID: "conv-own", Text: "running tests"})
	pending = pendingMessages(model)
	if !strings.Contains(pending[len(pending)-1].Content, "running tests") {
		t.Errorf("expected latest progress text, got %q", pending[len(pending)-1].Content)
	}
	if strings.Contains(pending[len(pending)-1].Content, "reading files") {
		t.Error("expected stale progress text replaced")
	}
	if strings.ToLower(pending[len(pending)-1].Content) != pending[len(pending)-1].Content {
		t.Errorf("pending line must be lowercase, got %q", pending[len(pending)-1].Content)
	}

	// Progress for another conversation does not clobber this line.
	model.Update(turnProgressMsg{ConversationID: "conv-other", Text: "elsewhere"})
	pending = pendingMessages(model)
	if strings.Contains(pending[len(pending)-1].Content, "elsewhere") {
		t.Error("expected other-conversation progress ignored")
	}
}

// TestChatModel_TerminalCompletedRendersReply verifies the completed
// terminal renders the reply bubble and clears the pending line.
func TestChatModel_TerminalCompletedRendersReply(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)

	model.textarea.SetValue("answer me")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{TurnID: "turn-1", ConversationID: model.conversationID, Accepted: true}})
	if !model.loading {
		t.Fatal("expected loading=true while turn in flight")
	}

	model.Update(turnTerminalMsg{
		TurnID:         "turn-1",
		ConversationID: model.conversationID,
		Reply:          "here is the answer",
		Status:         "completed",
		DurationMS:     1234,
	})

	if model.loading {
		t.Error("expected loading=false after terminal")
	}
	if len(model.pendingTurns) != 0 {
		t.Errorf("expected turn removed, got %d pending", len(model.pendingTurns))
	}
	if len(pendingMessages(model)) != 0 {
		t.Error("expected pending line cleared")
	}
	found := false
	for _, m := range model.messages {
		if m.Role == RoleAssistant && m.Content == "here is the answer" {
			found = true
		}
	}
	if !found {
		t.Error("expected assistant reply bubble")
	}
}

// TestChatModel_TerminalFailedRendersError verifies failed terminal
// renders an error-styled bubble with the daemon's error text, lowercase.
func TestChatModel_TerminalFailedRendersError(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)

	model.textarea.SetValue("fail me")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{TurnID: "turn-1", ConversationID: model.conversationID, Accepted: true}})

	model.Update(turnTerminalMsg{
		TurnID:         "turn-1",
		ConversationID: model.conversationID,
		Status:         "failed",
		Error:          "quota exhausted",
	})

	if len(pendingMessages(model)) != 0 {
		t.Error("expected pending line cleared on failure")
	}
	found := false
	for _, m := range model.messages {
		if m.Role == RoleSystem && strings.Contains(strings.ToLower(m.Content), "quota exhausted") {
			found = true
		}
	}
	if !found {
		t.Error("expected error bubble with the failure reason")
	}
}

// TestChatModel_StalledShowsHonestText verifies the stalled terminal
// renders the honest "may still be running" wording, lowercase, and keeps
// the turn tracked so the real terminal still lands later.
func TestChatModel_StalledShowsHonestText(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)

	model.textarea.SetValue("stall me")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{TurnID: "turn-1", ConversationID: model.conversationID, Accepted: true}})

	cmd := model.Update(turnTerminalMsg{
		TurnID:         "turn-1",
		ConversationID: model.conversationID,
		Status:         "stalled",
		Error:          renderTurnStalledText(model.livenessTimeout),
	})
	// cmd is a re-armed await cmd — never execute it here (it blocks
	// until a terminal arrives); presence is the assertion.
	if cmd == nil {
		t.Error("expected re-armed await cmd after stall")
	}
	if len(model.pendingTurns) != 1 {
		t.Errorf("expected stalled turn to stay tracked, got %d pending", len(model.pendingTurns))
	}
	pending := pendingMessages(model)
	if len(pending) == 0 {
		t.Fatal("expected pending line retained for stalled turn")
	}
	content := pending[len(pending)-1].Content
	if !strings.Contains(content, "no progress for 90s") || !strings.Contains(content, "may still be running") {
		t.Errorf("expected honest stalled text, got %q", content)
	}
	if content != strings.ToLower(content) {
		t.Errorf("stalled text must be lowercase, got %q", content)
	}

	// The real terminal later still resolves the stalled turn.
	router := model.turns
	router.DeliverTestTerminal("turn-1", "late reply", "completed")
	model.Update(turnTerminalMsg{
		TurnID:         "turn-1",
		ConversationID: model.conversationID,
		Reply:          "late reply",
		Status:         "completed",
	})
	if len(model.pendingTurns) != 0 {
		t.Error("expected stalled turn resolved by late terminal")
	}
	found := false
	for _, m := range model.messages {
		if m.Role == RoleAssistant && m.Content == "late reply" {
			found = true
		}
	}
	if !found {
		t.Error("expected late reply bubble")
	}
}

// TestChatModel_DoubleSubmitIndependentResolution verifies two rapid
// submits are tracked by turn id and resolve independently.
func TestChatModel_DoubleSubmitIndependentResolution(t *testing.T) {
	model, mock := newAsyncTestChatModel(t)

	model.textarea.SetValue("first question")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{TurnID: "turn-1", ConversationID: model.conversationID, Accepted: true}})
	// Simulate the agent being active so the second send queues the same
	// async submit path for a second concurrent turn (direct registration
	// keeps the test focused on per-turn resolution).
	model.turns.Register("turn-2", model.conversationID)
	model.pendingTurns = append(model.pendingTurns, mustGet(t, model, "turn-2"))

	if len(model.pendingTurns) != 2 {
		t.Fatalf("expected 2 concurrent turns, got %d", len(model.pendingTurns))
	}
	if len(mock.SubmitCalls) != 1 {
		t.Errorf("expected exactly one real submit in this test, got %d", len(mock.SubmitCalls))
	}

	// Terminal for turn-2 only: turn-1 must remain tracked.
	model.Update(turnTerminalMsg{TurnID: "turn-2", ConversationID: model.conversationID, Reply: "second answer", Status: "completed"})
	if len(model.pendingTurns) != 1 {
		t.Fatalf("expected turn-1 still pending, got %d", len(model.pendingTurns))
	}
	if model.pendingTurns[0].turnID != "turn-1" {
		t.Errorf("expected turn-1 pending, got %q", model.pendingTurns[0].turnID)
	}
	if !model.loading {
		t.Error("expected loading=true while turn-1 still in flight")
	}

	// Terminal for turn-1 resolves everything.
	model.Update(turnTerminalMsg{TurnID: "turn-1", ConversationID: model.conversationID, Reply: "first answer", Status: "completed"})
	if len(model.pendingTurns) != 0 {
		t.Errorf("expected all turns resolved, got %d", len(model.pendingTurns))
	}
	if model.loading {
		t.Error("expected loading=false after last terminal")
	}

	// Each turn got its own reply bubble (order is delivery-order, not
	// submit-order, under concurrent turns).
	replies := map[string]bool{}
	for _, m := range model.messages {
		if m.Role == RoleAssistant {
			replies[m.Content] = true
		}
	}
	if len(replies) != 2 || !replies["first answer"] || !replies["second answer"] {
		t.Errorf("expected per-turn reply bubbles for both turns, got %v", replies)
	}
}

// mustGet returns a registered turn from the router or fails the test.
func mustGet(t *testing.T, m *ChatModel, turnID string) *pendingTurn {
	t.Helper()
	pt, ok := m.turns.get(turnID)
	if !ok {
		t.Fatalf("turn %q not tracked", turnID)
	}
	return pt
}

// TestChatModel_UntrackedTerminalIgnored verifies terminal events for
// untracked turn ids do not touch the transcript.
func TestChatModel_UntrackedTerminalIgnored(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	before := len(model.messages)
	model.Update(turnTerminalMsg{TurnID: "turn-unknown", ConversationID: model.conversationID, Reply: "ghost", Status: "completed"})
	if len(model.messages) != before {
		t.Error("expected untracked terminal ignored")
	}
}

// TestChatModel_SuppressTaskCompleted verifies the dedupe rule: while a
// turn is pending for a conversation, task.completed relays are suppressed;
// after the terminal lands they pass through again.
func TestChatModel_SuppressTaskCompleted(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)

	if model.SuppressTaskCompletedFor(model.conversationID) {
		t.Error("expected no suppression with no pending turns")
	}

	model.textarea.SetValue("dedupe me")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{TurnID: "turn-1", ConversationID: model.conversationID, Accepted: true}})

	if !model.SuppressTaskCompletedFor(model.conversationID) {
		t.Error("expected suppression while turn pending")
	}
	if model.SuppressTaskCompletedFor("conv-other") {
		t.Error("expected suppression to be conversation-scoped")
	}
	if model.SuppressTaskCompletedFor("") {
		t.Error("expected empty conversation to never suppress")
	}

	model.Update(turnTerminalMsg{TurnID: "turn-1", ConversationID: model.conversationID, Reply: "done", Status: "completed"})
	if model.SuppressTaskCompletedFor(model.conversationID) {
		t.Error("expected suppression lifted after terminal")
	}
}

// TestChatModel_DeliverTurnProgressRefreshesLiveness verifies the
// Deliver* entry points used by the App's EventStream dispatch.
func TestChatModel_DeliverTurnProgressRefreshesLiveness(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)

	model.textarea.SetValue("deliver to me")
	_ = drainBatchMsgs(model.doSendMessage())
	model.Update(submitTurnResultMsg{Ack: TurnSubmitAck{TurnID: "turn-1", ConversationID: model.conversationID, Accepted: true}})

	pt := mustGet(t, model, "turn-1")
	before := pt.lastActivity
	model.DeliverTurnProgress(model.conversationID, "still working")
	if !pt.lastActivity.After(before) {
		t.Error("expected liveness refreshed by progress delivery")
	}
	if pt.progressText != "still working" {
		t.Errorf("expected progress text stored, got %q", pt.progressText)
	}

	model.DeliverTurnTerminal("turn-1", map[string]any{
		"turn_id":     "turn-1",
		"reply":       "async reply",
		"status":      "completed",
		"duration_ms": float64(42),
	})
	_, stillOpen := pt.addWaiter()
	if stillOpen {
		t.Error("expected turn resolved after DeliverTurnTerminal")
	}
}

// TestChatModel_SubmitChatErrorPath verifies a transport error from
// SubmitChat clears the pending state honestly.
func TestChatModel_SubmitChatErrorPath(t *testing.T) {
	model, mock := newAsyncTestChatModel(t)
	mock.SubmitErr = errors.New("connection refused")

	model.textarea.SetValue("will fail")
	cmd := model.doSendMessage()
	msgs := drainBatchMsgs(cmd)
	sub, ok := extractMsg[ChatSubmitErrorMsg](msgs)
	if !ok {
		t.Fatal("expected ChatSubmitErrorMsg on transport error")
	}
	if sub.Err == nil || !strings.Contains(sub.Err.Error(), "connection refused") {
		t.Errorf("expected transport error preserved, got %v", sub.Err)
	}

	model.Update(sub)
	if model.loading {
		t.Error("expected loading=false after submit error")
	}
	if len(model.pendingTurns) != 0 {
		t.Error("expected no turns tracked after submit error")
	}
}

// TestChatModel_TurnActivityTimeout tests the config accessor.
func TestChatModel_TurnActivityTimeout(t *testing.T) {
	model, _ := newAsyncTestChatModel(t)
	if model.TurnActivityTimeout() != 90*time.Second {
		t.Errorf("expected 90s liveness, got %v", model.TurnActivityTimeout())
	}
}

// compile-time assertion the mock satisfies the interface in this file too.
var _ RPCClient = (*MockChatRPCClient)(nil)

// keep context import used if the test file evolves.
var _ = context.Background
