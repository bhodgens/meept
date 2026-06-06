package agent

import (
	"testing"
	"time"
)

func TestNewTurnManager(t *testing.T) {
	tm := NewTurnManager([]string{"agent-a", "agent-b"}, 10, 4096, 5*time.Minute)
	if tm.TokenHolder() != "agent-a" {
		t.Errorf("initial holder = %q, want agent-a", tm.TokenHolder())
	}
	if tm.CurrentTurn() != 1 {
		t.Errorf("current turn = %d, want 1", tm.CurrentTurn())
	}
	if tm.IsExhausted() {
		t.Error("should not be exhausted initially")
	}
}

func TestTurnManager_Yield(t *testing.T) {
	tm := NewTurnManager([]string{"agent-a", "agent-b"}, 10, 4096, 5*time.Minute)

	// Yield a -> b
	if err := tm.Yield(); err != nil {
		t.Fatalf("yield failed: %v", err)
	}
	if tm.TokenHolder() != "agent-b" {
		t.Errorf("holder = %q, want agent-b", tm.TokenHolder())
	}
	if tm.CurrentTurn() != 2 {
		t.Errorf("turn = %d, want 2", tm.CurrentTurn())
	}

	// Yield b -> a (round-robin)
	if err := tm.Yield(); err != nil {
		t.Fatalf("yield failed: %v", err)
	}
	if tm.TokenHolder() != "agent-a" {
		t.Errorf("holder = %q, want agent-a", tm.TokenHolder())
	}
}

func TestTurnManager_RequestToken(t *testing.T) {
	tm := NewTurnManager([]string{"agent-a", "agent-b", "agent-c"}, 10, 4096, 5*time.Minute)

	// agent-b requests token from agent-a
	passed, err := tm.RequestToken("agent-b")
	if err != nil {
		t.Fatalf("request token failed: %v", err)
	}
	if !passed {
		t.Error("expected token to pass")
	}
	if tm.TokenHolder() != "agent-b" {
		t.Errorf("holder = %q, want agent-b", tm.TokenHolder())
	}

	// non-participant requests token
	_, err = tm.RequestToken("agent-z")
	if err == nil {
		t.Error("expected error for non-participant")
	}
}

func TestTurnManager_MaxTurns(t *testing.T) {
	tm := NewTurnManager([]string{"agent-a", "agent-b"}, 3, 4096, 5*time.Minute)

	// Turn 1: a, Turn 2: b, Turn 3: a
	if err := tm.Yield(); err != nil {
		t.Fatalf("yield 1: %v", err)
	} // a -> b, turn 2
	if err := tm.Yield(); err != nil {
		t.Fatalf("yield 2: %v", err)
	} // b -> a, turn 3

	// This would be turn 4, exceeding max
	if err := tm.Yield(); err == nil {
		t.Error("expected error when exceeding max turns")
	}
	if !tm.IsExhausted() {
		t.Error("expected exhausted after max turns")
	}
}

func TestTurnManager_DefaultValues(t *testing.T) {
	tm := NewTurnManager([]string{"a"}, 0, 0, 0)
	if tm.MaxTurns() != 10 {
		t.Errorf("max turns = %d, want 10", tm.MaxTurns())
	}
}
