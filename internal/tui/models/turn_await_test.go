package models

// Async turn await-command contract tests (leaf 04), moved here from
// package tui in deadcode wave-3: they exercise ONLY the router + await
// command (no TUI view state), and keeping them in the models package lets
// the entire test-helper export block in turn.go (TestAwaitTurnCmd,
// DeliverTestTerminal/Progress, NewTestTurnRouter*, Register*, TurnRouter,
// TestTurnTerminalMsg) move into _test.go files so the production build
// ships none of it. The SubmitChat wire-format tests stay in package tui
// (they exercise the real RPCClient, which is package tui).

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// waitForArmedTimers polls until the router's manual timer source has
// armed want timers (or fails the test). Polling with a tiny sleep is
// test-harness synchronization, not liveness timing — the actual liveness
// window is injected and never slept on.
func waitForArmedTimers(t *testing.T, router *turnRouter, want int, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if router.armedTimers() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timers never armed: want %d, got %d", want, router.armedTimers())
}

// TestAwaitTurnCmdFiltersByTurnID runs the await command against a fake
// event source delivering an unrelated terminal event, a progress event,
// and the matching terminal: only the matching terminal must resolve.
func TestAwaitTurnCmdFiltersByTurnID(t *testing.T) {
	router := newTurnRouter(noopTimerSource{})
	pt, ok := router.register("turn-match", "conv-1", "")
	if !ok {
		t.Fatal("failed to register turn")
	}

	cmd := awaitTurnCmd(router, pt, 0) // liveness disabled

	// Unrelated turn terminal: must be ignored by the router (the
	// "miss" case — an untracked turn id never resolves anything).
	router.deliverTestTerminal("turn-other", "done", "completed")

	// Progress for the conversation refreshes liveness (no stall timer
	// armed here since liveness=0; just ensure no panic and no resolve).
	router.deliverTestProgress("conv-1", "thinking hard")

	// Matching terminal resolves the await.
	if !router.deliverTestTerminal("turn-match", "final reply", "completed") {
		t.Fatal("expected matching turn_id to hit")
	}

	msg := cmd()
	term, ok := msg.(turnTerminalMsg)
	if !ok {
		t.Fatalf("expected turnTerminalMsg, got %T", msg)
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
	router := newTurnRouter(NewManualTimerSource())
	pt, ok := router.register("turn-stall", "conv-1", "")
	if !ok {
		t.Fatal("failed to register turn")
	}

	liveness := 5 * time.Second
	cmd := awaitTurnCmd(router, pt, liveness)

	// Execute the command in a goroutine (as bubbletea would) and wait
	// for the liveness timer to be armed before firing it — no sleeps.
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	waitForArmedTimers(t, router, 1, time.Second)
	router.fireTimers()

	msg := <-done
	term, ok := msg.(turnTerminalMsg)
	if !ok {
		t.Fatalf("expected turnTerminalMsg, got %T", msg)
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
	router := newTurnRouter(NewManualTimerSource())
	pt, _ := router.register("turn-race", "conv-1", "")
	cmd := awaitTurnCmd(router, pt, 30*time.Second)

	router.deliverTestTerminal("turn-race", "got there", "completed")

	msg := cmd()
	term, ok := msg.(turnTerminalMsg)
	if !ok {
		t.Fatalf("expected turnTerminalMsg, got %T", msg)
	}
	if term.Status != "completed" {
		t.Errorf("expected completed (terminal beat timer), got %q", term.Status)
	}
}
