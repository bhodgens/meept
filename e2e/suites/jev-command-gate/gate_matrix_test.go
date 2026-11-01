//go:build e2e

// jev-gate-01: the fail-closed matrix (leaf 01 Contract C3) + serialization
// byte-stability (Contract C2). Stub scorers produce only shapes the real
// client can produce (band-label answers, argmax-only confidence).
package jevgatecmd

import (
	"context"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/jevgate"
	"github.com/caimlas/meept/internal/llm"
)

// matrixScorer is a scripted ScoreClient double.
type matrixScorer struct {
	result *llm.CommandScoreResult
	err    error
	calls  int
}

func (s *matrixScorer) ScoreCommand(_ context.Context, _ string) (*llm.CommandScoreResult, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func shellCall(t *testing.T, argsJSON string) llm.ToolCall {
	t.Helper()
	return llm.ToolCall{Function: llm.ToolCallFunction{Name: "shell", Arguments: argsJSON}}
}

func gateCfg() jevgate.ScoreGateConfig {
	return jevgate.ScoreGateConfig{Enabled: true, ScoreThreshold: 0.90, ConfidenceFloor: 0.10}
}

// TestGate01_FailClosedMatrix walks every branch of the C3 table: allow
// ONLY the clean-allow shape; escalate everything else.
func TestGate01_FailClosedMatrix(t *testing.T) {
	args := `{"command":"ls -la"}`
	cases := []struct {
		name    string
		scorer  *matrixScorer
		allowed bool
	}{
		{"clean allow (parsed band, not degraded, above threshold, above floor)",
			&matrixScorer{result: nonDegraded(0.95, 0.9)}, true},
		{"below threshold", &matrixScorer{result: nonDegraded(0.5, 0.9)}, false},
		{"below confidence floor (veto)", &matrixScorer{result: nonDegraded(0.95, 0.05)}, false},
		{"degraded answer", &matrixScorer{result: bandResult("0.0")}, false},
		{"scorer error", &matrixScorer{err: errors.New("transport down")}, false},
		{"nil result", &matrixScorer{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := jevgate.NewScoreGateHook(tc.scorer, gateCfg(), nil, nil)
			block := hook.BeforeToolCall(context.Background(), shellCall(t, args))
			if block.Block == tc.allowed {
				t.Fatalf("allowed=%t but Block=%t (reason %q)", tc.allowed, block.Block, block.Reason)
			}
			if !tc.allowed && block.HardError {
				// escalation must be a SOFT deny (human decides), never a
				// HardError security refusal.
				t.Errorf("escalation must be soft deny; got HardError=%t", block.HardError)
			}
		})
	}
}

// TestGate01_ScorerUnavailableIsAsk pins fail-closed on a nil scorer.
func TestGate01_ScorerUnavailableIsAsk(t *testing.T) {
	hook := jevgate.NewScoreGateHook(nil, gateCfg(), nil, nil)
	block := hook.BeforeToolCall(context.Background(), shellCall(t, `{"command":"ls"}`))
	if !block.Block || block.HardError {
		t.Fatalf("nil scorer must soft-escalate; got %+v", block)
	}
}

// TestGate01_DisabledPassthrough pins the opt-in: disabled = zero behavior.
func TestGate01_DisabledPassthrough(t *testing.T) {
	scorer := &matrixScorer{result: nonDegraded(0.0, 0.0)}
	hook := jevgate.NewScoreGateHook(scorer, gateCfg(), nil, nil)
	hook.SetEnabled(false)
	block := hook.BeforeToolCall(context.Background(), shellCall(t, `{"command":"rm -rf /"}`))
	if block.Block {
		t.Fatal("disabled gate must pass through")
	}
	if scorer.calls != 0 {
		t.Fatalf("disabled gate must not call the scorer; calls=%d", scorer.calls)
	}
}

// TestGate01_SerializationByteStable pins C2: identical inputs produce
// identical bytes regardless of map iteration order.
func TestGate01_SerializationByteStable(t *testing.T) {
	facts := map[string]string{"b": "2", "a": "1", "c": "3"}
	first := jevgate.SerializeCommandContext("echo hi", "/w", facts)
	for i := 0; i < 50; i++ {
		if got := jevgate.SerializeCommandContext("echo hi", "/w", facts); got != first {
			t.Fatalf("iteration %d changed the bytes:\n%q\n%q", i, got, first)
		}
	}
	// Newlines inside values are flattened (line-anchored spoof resistance).
	withNewline := jevgate.SerializeCommandContext("echo a\ncwd: /evil", "/w", nil)
	if !containsExactlyOneCwdField(withNewline) {
		t.Fatalf("embedded newline forged an extra field: %q", withNewline)
	}
}

func containsExactlyOneCwdField(s string) bool {
	count := 0
	idx := 0
	for {
		i := indexOf(s[idx:], "\ncwd: ")
		if i < 0 {
			break
		}
		count++
		idx += i + 1
	}
	return count == 1
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// compile-time: the hook satisfies the registry interface.
var _ agent.BeforeToolCallHook = (*jevgate.ScoreGateHook)(nil)
