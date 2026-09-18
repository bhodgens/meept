package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// F-A3 pins: per-turn per-class nudge budget. After
// maxNudgesPerClassPerTurn nudges of the SAME class, the 3rd+ nudge does not
// append to the conversation and the step terminalizes with an honest
// failure (ErrNudgeBudgetExhausted, "giving up this step").

// nudgeClaimChatter returns a final text claiming a created file (drives
// the unbackedSideEffectClaims nudge class). Successive responses VARY
// (unique file names) so the byte-level convergence detector stays quiet —
// the pin isolates the F-A3 nudge budget, not convergence.
type nudgeClaimChatter struct {
	callCount int
}

func (m *nudgeClaimChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	resp := &llm.Response{
		Content: fmt.Sprintf("claim %d: Created file done_%d.txt with the results", m.callCount, m.callCount),
		Usage:   llm.TokenUsage{TotalTokens: 1},
	}
	return resp, nil
}

func (m *nudgeClaimChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *nudgeClaimChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "nudge-claim-chatter"}
}

// newNudgeCapLoop builds a loop wired exactly like the guards integration
// tests (no executor: the claim responses carry no tool calls, so the
// final-text guards fire directly).
func newNudgeCapLoop(t *testing.T, chatter llm.Chatter) *AgentLoop {
	t.Helper()
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{
			MaxIterations: 20,
			Guards:        DefaultGuardConfig(),
		}),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	return loop
}

// countNudgeMessages counts user-role messages carrying the unbacked-claims
// nudge text.
func countNudgeMessages(t *testing.T, loop *AgentLoop, convID, marker string) int {
	t.Helper()
	conv := loop.conversations.Get(convID)
	if conv == nil {
		return 0
	}
	n := 0
	for _, msg := range conv.GetMessages() {
		if msg.Role == llm.RoleUser && strings.Contains(msg.Content, marker) {
			n++
		}
	}
	return n
}

// TestNudgeCap_UnbackedClaimsThirdNudgeDoesNotAppendAndStepTerminalizes is
// the F-A3 pin: a model that keeps returning unbacked file-claim text gets
// at most 2 nudges; the 3rd offending response must NOT append a 3rd nudge
// and the step must terminalize with the honest failure.
func TestNudgeCap_UnbackedClaimsThirdNudgeDoesNotAppendAndStepTerminalizes(t *testing.T) {
	const nudgeMarker = "those claims are unverified"
	chatter := &nudgeClaimChatter{}
	loop := newNudgeCapLoop(t, chatter)

	_, err := loop.RunOnce(context.Background(), "make the file", "conv-nudge-cap-claims")
	if err == nil {
		t.Fatal("expected the step to terminalize with the honest failure")
	}
	if !containsError(err, ErrNudgeBudgetExhausted) {
		t.Fatalf("err = %v, want ErrNudgeBudgetExhausted", err)
	}
	if !strings.Contains(err.Error(), "repeated unbacked claims; giving up this step") {
		t.Fatalf("err = %v, want the honest give-up wording", err)
	}
	got := countNudgeMessages(t, loop, "conv-nudge-cap-claims", nudgeMarker)
	if got != maxNudgesPerClassPerTurn {
		t.Fatalf("nudges appended = %d, want exactly %d (the 3rd must not append)", got, maxNudgesPerClassPerTurn)
	}
}

// TestNudgeCap_BudgetResetsPerTurn pins the per-turn semantics: after an
// exhausted turn, a NEW turn starts with a fresh budget (the 3rd turn's
// first nudge appends again).
func TestNudgeCap_BudgetResetsPerTurn(t *testing.T) {
	const nudgeMarker = "those claims are unverified"
	chatter := &nudgeClaimChatter{}
	loop := newNudgeCapLoop(t, chatter)

	// Turn 1: budget exhausts.
	_, err := loop.RunOnce(context.Background(), "make the file", "conv-nudge-reset")
	if !containsError(err, ErrNudgeBudgetExhausted) {
		t.Fatalf("turn 1 err = %v, want ErrNudgeBudgetExhausted", err)
	}
	// Turn 2 (same loop, same conversation id space but a NEW conv): fresh
	// budget — the first nudge must land again.
	if _, err := loop.RunOnce(context.Background(), "make the file", "conv-nudge-reset-2"); !containsError(err, ErrNudgeBudgetExhausted) {
		t.Fatalf("turn 2 err = %v, want ErrNudgeBudgetExhausted again", err)
	}
	// The second conversation must have its own fresh nudge trail.
	if got := countNudgeMessages(t, loop, "conv-nudge-reset-2", nudgeMarker); got != maxNudgesPerClassPerTurn {
		t.Fatalf("turn-2 nudges = %d, want %d (budget must reset per turn)", got, maxNudgesPerClassPerTurn)
	}
}

// TestNudgeCap_ConsumeNudgeBudgetUnit pins the budget primitive directly.
func TestNudgeCap_ConsumeNudgeBudgetUnit(t *testing.T) {
	l := &AgentLoop{nudgeClassCounts: make(map[string]int)}
	for i := 0; i < maxNudgesPerClassPerTurn; i++ {
		if !l.consumeNudgeBudget(nudgeClassUnbackedClaims) {
			t.Fatalf("consume %d: want true", i+1)
		}
	}
	if l.consumeNudgeBudget(nudgeClassUnbackedClaims) {
		t.Fatal("consume past cap: want false")
	}
	// A different class has its own budget.
	if !l.consumeNudgeBudget(nudgeClassNoProgress) {
		t.Fatal("a different class must have its own budget")
	}
	if !l.nudgeBudgetExhausted(nudgeClassUnbackedClaims) {
		t.Fatal("exhausted probe: want true")
	}
	if l.nudgeBudgetExhausted(nudgeClassNoProgress) {
		t.Fatal("unexhausted probe: want false")
	}
	// resetTurnGuards clears the budget.
	l.resetTurnGuards()
	if l.nudgeBudgetExhausted(nudgeClassUnbackedClaims) {
		t.Fatal("budget must reset per turn")
	}
}

// containsError is errors.Is without importing errors in every test above.
func containsError(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
