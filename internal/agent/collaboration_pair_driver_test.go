package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestPairProgrammingDriver_Name(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	if d.Name() != "pair_programming" {
		t.Errorf("Name() = %q, want pair_programming", d.Name())
	}
}

func TestPairProgrammingDriver_CanInitiate(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	if !d.CanInitiate("coder", "test reason") {
		t.Error("CanInitiate should return true")
	}
}

func TestPairProgrammingDriver_getOtherParticipant(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	tests := []struct {
		parts    []string
		current  string
		expected string
	}{
		{[]string{"a", "b"}, "a", "b"},
		{[]string{"a", "b"}, "b", "a"},
		{[]string{"a", "b", "c"}, "a", "b"},
		{[]string{"a"}, "a", ""},
	}
	for _, tc := range tests {
		got := d.getOtherParticipant(tc.parts, tc.current)
		if got != tc.expected {
			t.Errorf("getOtherParticipant(%v, %q) = %q, want %q", tc.parts, tc.current, got, tc.expected)
		}
	}
}

func TestPairProgrammingDriver_parseObserverResponse(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	tests := []struct {
		name       string
		input      string
		wantAction string
	}{
		// Structured JSON responses
		{"structured approve", `{"action": "approve", "feedback": "code is clean"}`, "approve"},
		{"structured request_changes", `{"action": "request_changes", "feedback": "fix nil check"}`, "request_changes"},
		{"structured request_token", `{"action": "request_token", "feedback": "let me drive"}`, "request_token"},
		{"structured in text", "Review complete. {\"action\": \"approve\", \"feedback\": \"LGTM\"}", "approve"},

		// Fallback keyword matching
		{"keyword approve", "This looks good to me. Approve.", "approve"},
		{"keyword lgtm", "LGTM", "approve"},
		{"keyword request_token", "I want to take over as driver. request_token", "request_token"},
		{"keyword default", "There's a bug in line 42. Fix the off-by-one.", "request_changes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, _ := d.parseObserverResponse(tc.input)
			if action != tc.wantAction {
				t.Errorf("parse(%q) action = %q, want %q", tc.input, action, tc.wantAction)
			}
		})
	}
}

func TestNewPairProgrammingDriver_Defaults(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{})
	if d.logger == nil {
		t.Error("logger should not be nil")
	}
	if d.conversations == nil {
		t.Error("conversations map should be initialized")
	}
}

// pairApproveChatter is a scripted llm.Chatter for the pair-driver budget
// pins: every turn returns a plain-text reply (the observer's fallback
// parse yields request_changes, so the session runs to turn exhaustion
// instead of converging on turn 1).
type pairApproveChatter struct{}

func (pairApproveChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: "work continues", FinishReason: "stop"}, nil
}

func (c pairApproveChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (pairApproveChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "pair-budget-fixture"}
}

// newPairBudgetDriver builds a pair driver whose registry runs scripted
// loops (no network) and whose workspace lives under a temp dir.
func newPairBudgetDriver(t *testing.T) *PairProgrammingDriver {
	t.Helper()
	reg := NewAgentRegistry(RegistryConfig{
		Logger:        leafDiscardLogger(),
		SharedChatter: pairApproveChatter{},
	})
	for _, id := range []string{"coder", "planner"} {
		if err := reg.RegisterSpec(&AgentSpec{ID: id, Name: id, Role: RoleExecutor, Enabled: true}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	ws := NewWorkspaceManager(WorkspaceConfig{BaseDir: t.TempDir(), AutoCommit: false}, leafDiscardLogger())
	return NewPairProgrammingDriver(PairProgrammingDriverDeps{
		Registry:  reg,
		Workspace: ws,
		Logger:    leafDiscardLogger(),
	})
}

// TestPairProgrammingDriver_TokenBudgetGate pins M7's enforcement
// semantics at the driver gate (collaboration_pair_driver.go runTurnLoop):
// TokenBudget 0 = unlimited (the gate is skipped entirely — an
// unconfigured session never hard-stops on tokens), while an explicit
// positive cap still fires ErrBudgetExceeded once usage reaches it.
func TestPairProgrammingDriver_TokenBudgetGate(t *testing.T) {
	t.Run("zero budget skips the gate", func(t *testing.T) {
		d := newPairBudgetDriver(t)
		cfg := DefaultSessionConfig()
		cfg.MaxTurns = 2
		sess := NewCollaborationSession("pair_programming", "task-m7-zero", []string{"coder", "planner"}, cfg)
		// Usage far beyond the OLD hidden 50k default: with TokenBudget 0
		// the gate must not fire.
		sess.AddTurn(TurnEntry{AgentID: "coder", Role: "driver", Content: "prior work", TokensUsed: 60000})
		res, err := d.Run(context.Background(), sess)
		if err != nil {
			t.Fatalf("zero-budget session errored (hidden ceiling?): %v", err)
		}
		if res.State != SessionExhausted {
			t.Fatalf("state = %q, want exhausted (turn bound, not token bound)", res.State)
		}
	})

	t.Run("explicit positive cap still enforces", func(t *testing.T) {
		d := newPairBudgetDriver(t)
		cfg := DefaultSessionConfig()
		cfg.TokenBudget = 1000
		sess := NewCollaborationSession("pair_programming", "task-m7-cap", []string{"coder", "planner"}, cfg)
		sess.AddTurn(TurnEntry{AgentID: "coder", Role: "driver", Content: "prior work", TokensUsed: 1000})
		_, err := d.Run(context.Background(), sess)
		if !errors.Is(err, ErrBudgetExceeded) {
			t.Fatalf("err = %v, want ErrBudgetExceeded for an explicit positive cap", err)
		}
		// runTurnLoop marks exhausted at the gate; Run's error path then
		// marks the session failed — either terminal state proves the cap
		// fired (the pre-fix default would have fired at 50000 instead).
		if state := sess.GetState(); state != SessionExhausted && state != SessionFailed {
			t.Fatalf("state = %q, want a terminal state on budget trip", state)
		}
	})
}
