package agent

import (
	"testing"
	"time"
)

func TestSessionState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    SessionState
		expected bool
	}{
		{SessionCreated, false},
		{SessionActive, false},
		{SessionConverged, true},
		{SessionExhausted, true},
		{SessionFailed, true},
	}
	for _, tc := range tests {
		t.Run(string(tc.state), func(t *testing.T) {
			if got := tc.state.IsTerminal(); got != tc.expected {
				t.Errorf("IsTerminal() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestNewCollaborationSession(t *testing.T) {
	cfg := DefaultSessionConfig()
	sess := NewCollaborationSession("pair_programming", "task-42", []string{"coder", "planner"}, cfg)
	if sess.ID == "" {
		t.Error("session ID should not be empty")
	}
	if sess.Mode != "pair_programming" {
		t.Errorf("mode = %q, want pair_programming", sess.Mode)
	}
	if sess.State != SessionCreated {
		t.Errorf("state = %q, want created", sess.State)
	}
	if sess.TurnCount() != 0 {
		t.Errorf("turn count = %d, want 0", sess.TurnCount())
	}
	if sess.MaxTurns != 10 {
		t.Errorf("max turns = %d, want 10", sess.MaxTurns)
	}
}

func TestCollaborationSession_AddTurn(t *testing.T) {
	cfg := DefaultSessionConfig()
	sess := NewCollaborationSession("pair_programming", "task-42", []string{"coder"}, cfg)

	sess.AddTurn(TurnEntry{AgentID: "coder", Role: "driver", Content: "hello"})
	if sess.TurnCount() != 1 {
		t.Errorf("turn count = %d, want 1", sess.TurnCount())
	}
	if sess.TurnLog[0].TurnNumber != 1 {
		t.Errorf("turn number = %d, want 1", sess.TurnLog[0].TurnNumber)
	}

	sess.AddTurn(TurnEntry{AgentID: "planner", Role: "observer", Content: "looks good"})
	if sess.TurnCount() != 2 {
		t.Errorf("turn count = %d, want 2", sess.TurnCount())
	}
	if sess.TurnLog[1].TurnNumber != 2 {
		t.Errorf("turn number = %d, want 2", sess.TurnLog[1].TurnNumber)
	}
}

func TestCollaborationSession_StateTransitions(t *testing.T) {
	cfg := DefaultSessionConfig()
	sess := NewCollaborationSession("pair_programming", "task-42", []string{"coder"}, cfg)

	sess.MarkActive()
	if sess.State != SessionActive {
		t.Errorf("state = %q, want active", sess.State)
	}

	sess.MarkConverged()
	if sess.State != SessionConverged || !sess.State.IsTerminal() {
		t.Errorf("state = %q, want converged/terminal", sess.State)
	}
}

// TestDefaultSessionConfig_TokenBudgetUnlimited pins M7 (bughunt
// 2026-11-19): an unconfigured collaboration session must not carry a
// hidden 50k token ceiling — the same opt-in-cap class 419c9b77 removed
// from the chat loop. TokenBudget 0 = unlimited (the pair driver gates on
// >0); the other defaults stay unchanged.
func TestDefaultSessionConfig_TokenBudgetUnlimited(t *testing.T) {
	cfg := DefaultSessionConfig()
	if cfg.TokenBudget != 0 {
		t.Fatalf("DefaultSessionConfig TokenBudget = %d, want 0 (unlimited; no hidden ceiling)", cfg.TokenBudget)
	}
	if cfg.MaxTurns != 10 {
		t.Errorf("MaxTurns = %d, want 10 (unchanged)", cfg.MaxTurns)
	}
	if cfg.TurnTimeout != 5*time.Minute {
		t.Errorf("TurnTimeout = %v, want 5m (unchanged)", cfg.TurnTimeout)
	}
	if cfg.TimeBudget != 30*time.Minute {
		t.Errorf("TimeBudget = %v, want 30m (unchanged)", cfg.TimeBudget)
	}
	sess := NewCollaborationSession("pair_programming", "task-m7", []string{"coder", "planner"}, cfg)
	if sess.TokenBudget != 0 {
		t.Fatalf("session TokenBudget = %d, want 0 carried from the default config", sess.TokenBudget)
	}
}

// TestCollaborationSession_ExplicitTokenBudgetCarried pins that an
// explicit positive cap still flows through config -> session unchanged
// (M7 keeps explicit caps working; only the DEFAULT became unlimited).
func TestCollaborationSession_ExplicitTokenBudgetCarried(t *testing.T) {
	cfg := DefaultSessionConfig()
	cfg.TokenBudget = 1234
	sess := NewCollaborationSession("pair_programming", "task-m7-cap", []string{"coder", "planner"}, cfg)
	if sess.TokenBudget != 1234 {
		t.Fatalf("session TokenBudget = %d, want the explicit 1234", sess.TokenBudget)
	}
}
