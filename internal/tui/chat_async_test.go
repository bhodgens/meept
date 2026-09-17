package tui

// Async turn migration (leaf 04) — Task 1 tests: SubmitChat wire format
// and awaitTurnCmd turn_id filtering + injectable liveness. The chat-view
// model tests live in internal/tui/models/chat_async_test.go.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/llm"
	tuimodels "github.com/caimlas/meept/internal/tui/models"
)

// waitForArmedTimers polls until the router's manual timer source has
// armed want timers (or fails the test). Polling with a tiny sleep is
// test-harness synchronization, not liveness timing — the actual liveness
// window is injected and never slept on.
func waitForArmedTimers(t *testing.T, router *tuimodels.TurnRouter, want int, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if router.ArmedTimers() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timers never armed: want %d, got %d", want, router.ArmedTimers())
}

// TestSubmitChatParamsAndAck verifies SubmitChat calls "chat.submit" with
// message/session_id params and parses the ack (turn_id, accepted, note).
func TestSubmitChatParamsAndAck(t *testing.T) {
	server := newMockServer(t)
	var gotMethod string
	var gotParams map[string]any
	server.handler = func(method string, params json.RawMessage) (any, error) {
		gotMethod = method
		if err := json.Unmarshal(params, &gotParams); err != nil {
			t.Errorf("bad params: %v", err)
		}
		return map[string]any{
			"turn_id":         "turn-abc",
			"conversation_id": "conv-1",
			"session_id":      "sess-1",
			"accepted":        true,
		}, nil
	}
	defer server.Close()

	client := NewRPCClient(server.sockPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	ack, err := client.SubmitChat(context.Background(), "hello world", "sess-1", nil)
	if err != nil {
		t.Fatalf("SubmitChat failed: %v", err)
	}

	if gotMethod != "chat.submit" {
		t.Errorf("expected method chat.submit, got %q", gotMethod)
	}
	if gotParams["message"] != "hello world" {
		t.Errorf("expected message param hello world, got %v", gotParams["message"])
	}
	if gotParams["session_id"] != "sess-1" {
		t.Errorf("expected session_id param sess-1, got %v", gotParams["session_id"])
	}
	if _, hasParts := gotParams["parts"]; hasParts {
		t.Error("expected no parts key for empty parts slice")
	}

	if ack.TurnID != "turn-abc" {
		t.Errorf("expected turn_id turn-abc, got %q", ack.TurnID)
	}
	if !ack.Accepted {
		t.Error("expected accepted=true")
	}
	if ack.ConversationID != "conv-1" || ack.SessionID != "sess-1" {
		t.Errorf("unexpected ack ids: %+v", ack)
	}
}

// TestSubmitChatPartsPresent verifies parts are forwarded when provided.
func TestSubmitChatPartsPresent(t *testing.T) {
	server := newMockServer(t)
	var gotParams struct {
		Message string            `json:"message"`
		Parts   []json.RawMessage `json:"parts"`
	}
	server.handler = func(_ string, params json.RawMessage) (any, error) {
		if err := json.Unmarshal(params, &gotParams); err != nil {
			t.Errorf("bad params: %v", err)
		}
		return map[string]any{"turn_id": "t", "accepted": true}, nil
	}
	defer server.Close()

	client := NewRPCClient(server.sockPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	parts := []llm.ContentPart{{Type: "text", Text: "see attachment"}}
	if _, err := client.SubmitChat(context.Background(), "msg", "sess", parts); err != nil {
		t.Fatalf("SubmitChat: %v", err)
	}
	if len(gotParams.Parts) != 1 {
		t.Errorf("expected 1 part forwarded, got %d", len(gotParams.Parts))
	}
}

// TestSubmitChatRejectedAck verifies an accepted=false ack surfaces as an
// error carrying the daemon's note (the honest rejection reason).
func TestSubmitChatRejectedAck(t *testing.T) {
	server := newMockServer(t)
	server.handler = func(_ string, _ json.RawMessage) (any, error) {
		return map[string]any{
			"turn_id":         "",
			"accepted":        false,
			"note":            "message is required",
			"conversation_id": "conv-1",
		}, nil
	}
	defer server.Close()

	client := NewRPCClient(server.sockPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	ack, err := client.SubmitChat(context.Background(), "", "sess-1", nil)
	if err == nil {
		t.Fatal("expected error for accepted=false ack")
	}
	_ = ack
}

// TestAwaitTurnCmdFiltersByTurnID runs the await command against a fake
// event source delivering an unrelated terminal event, a progress event,
// and the matching terminal: only the matching terminal must resolve.
func TestAwaitTurnCmdFiltersByTurnID(t *testing.T) {
	router := tuimodels.NewTestTurnRouter()
	pt, ok := router.Register("turn-match", "conv-1")
	if !ok {
		t.Fatal("failed to register turn")
	}

	cmd := tuimodels.TestAwaitTurnCmd(router, pt, 0) // liveness disabled

	// Unrelated turn terminal: must be ignored by the router (the
	// "miss" case — an untracked turn id never resolves anything).
	router.DeliverTestTerminal("turn-other", "done", "completed")

	// Progress for the conversation refreshes liveness (no stall timer
	// armed here since liveness=0; just ensure no panic and no resolve).
	router.DeliverTestProgress("conv-1", "thinking hard")

	// Matching terminal resolves the await.
	if !router.DeliverTestTerminal("turn-match", "final reply", "completed") {
		t.Fatal("expected matching turn_id to hit")
	}

	msg := cmd()
	term, ok := msg.(tuimodels.TestTurnTerminalMsg)
	if !ok {
		t.Fatalf("expected TurnTerminalMsg, got %T", msg)
	}
	if term.TurnID != "turn-match" {
		t.Errorf("expected turn-match, got %q", term.TurnID)
	}
	if term.Reply != "final reply" {
		t.Errorf("expected reply 'final reply', got %q", term.Reply)
	}
	if term.Status != "completed" {
		t.Errorf("expected status completed, got %q", term.Status)
	}
}

// TestAwaitTurnCmdStalled verifies the stalled verdict fires via the
// INJECTABLE timer (no real sleeps) when nothing resolves the turn.
func TestAwaitTurnCmdStalled(t *testing.T) {
	router := tuimodels.NewTestTurnRouterWithTimers(tuimodels.NewManualTimerSource())
	pt, ok := router.Register("turn-stall", "conv-1")
	if !ok {
		t.Fatal("failed to register turn")
	}

	liveness := 5 * time.Second
	cmd := tuimodels.TestAwaitTurnCmd(router, pt, liveness)

	// Execute the command in a goroutine (as bubbletea would) and wait
	// for the liveness timer to be armed before firing it — no sleeps.
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	waitForArmedTimers(t, router, 1, time.Second)
	router.FireTimers()

	msg := <-done
	term, ok := msg.(tuimodels.TestTurnTerminalMsg)
	if !ok {
		t.Fatalf("expected TurnTerminalMsg, got %T", msg)
	}
	if term.Status != "stalled" {
		t.Errorf("expected stalled status, got %q", term.Status)
	}
	if term.TurnID != "turn-stall" {
		t.Errorf("expected turn-stall, got %q", term.TurnID)
	}
	want := "no progress for 5s"
	if got := term.Error; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("expected stalled text to start with %q, got %q", want, got)
	}
}

// TestAwaitTurnCmdTerminalBeatsTimer verifies the real terminal event wins
// when it arrives before the (manual) liveness timer fires.
func TestAwaitTurnCmdTerminalBeatsTimer(t *testing.T) {
	router := tuimodels.NewTestTurnRouterWithTimers(tuimodels.NewManualTimerSource())
	pt, _ := router.Register("turn-race", "conv-1")
	cmd := tuimodels.TestAwaitTurnCmd(router, pt, 30*time.Second)

	router.DeliverTestTerminal("turn-race", "got there", "completed")

	msg := cmd()
	term, ok := msg.(tuimodels.TestTurnTerminalMsg)
	if !ok {
		t.Fatalf("expected TurnTerminalMsg, got %T", msg)
	}
	if term.Status != "completed" {
		t.Errorf("expected completed (terminal beat timer), got %q", term.Status)
	}
}
