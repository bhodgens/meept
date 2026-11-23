package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// turnTokensGateChatter emits a fixed number of tool-call responses (each
// carrying usage of `perCall` tokens) followed by a final stop response
// (also carrying `perCall` tokens). Every call is scripted so the loop
// exercises a real multi-call tool loop without touching any real provider.
type turnTokensGateChatter struct {
	calls     int
	toolCalls int
	perCall   int
}

func newTurnTokensGateChatter(toolCalls, perCall int) *turnTokensGateChatter {
	return &turnTokensGateChatter{toolCalls: toolCalls, perCall: perCall}
}

func (c *turnTokensGateChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	c.calls++
	usage := llm.TokenUsage{TotalTokens: c.perCall}
	if c.calls <= c.toolCalls {
		// Use the same SAFE file_read action with distinct args per call so
		// the tools actually execute (rather than being denied as unknown
		// actions) while distinct args keep the no-progress ladder quiet.
		return &llm.Response{
			Content:      "calling",
			FinishReason: "tool_calls",
			Usage:        usage,
			ToolCalls: []llm.ToolCall{{
				ID:   fmt.Sprintf("call-gate-%d", c.calls),
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "file_read",
					Arguments: fmt.Sprintf(`{"path":"/tmp/gate_probe_%d.txt"}`, c.calls),
				},
			}},
		}, nil
	}
	return &llm.Response{Content: "done", FinishReason: "stop", Usage: usage}, nil
}

func (c *turnTokensGateChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *turnTokensGateChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "turn-tokens-gate-chatter"}
}

// recordingPreExec is a PreExecChecker that snapshots the "turn_tokens"
// detail injected by the executor's turn-tokens provider at every tool
// call. It allows every action so the loop reaches all tool calls.
type recordingPreExec struct {
	mu     sync.Mutex
	values []string // observed "turn_tokens" values ("" when absent)
}

func (r *recordingPreExec) Check(action, toolName string, details map[string]string) security.PreExecDecision {
	r.mu.Lock()
	r.values = append(r.values, details["turn_tokens"])
	r.mu.Unlock()
	return security.PreExecDecision{Allowed: true}
}

func (r *recordingPreExec) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.values))
	copy(out, r.values)
	return out
}

// newTurnTokensGateLoop builds a loop whose executor carries the loop's
// LastTurnTokens as the gate provider (the production wiring at loop.go:3125)
// and whose permission checker records the injected value on each call.
func newTurnTokensGateLoop(t *testing.T, chatter llm.Chatter) (*AgentLoop, *Conversation, *recordingPreExec) {
	t.Helper()
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "gate probe", func(ctx context.Context, args map[string]any) (any, error) {
		return "ok", nil
	}))
	secChecker := security.NewPermissionChecker(security.Config{})
	recorder := &recordingPreExec{}
	secChecker.SetPreExecChecker("gate-agent", recorder)
	loop := NewAgentLoop("sess-turn-tokens-gate", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slog.New(slog.DiscardHandler))),
		WithAgentConfig(AgentConfig{MaxIterations: 8}),
	)
	// Mirror the production wiring: the executor asks the loop for the
	// accumulated turn total at each tool call.
	loop.executor = NewExecutor(registry, secChecker, WithExecutorAgentID("gate-agent"))
	loop.executor.SetTurnTokensProvider(loop.LastTurnTokens)
	conv := loop.conversations.Get("conv-turn-tokens-gate")
	conv.AddUserMessage("go")
	return loop, conv, recorder
}

// TestTurnTokensGate_SeesAccumulatedTotalAcrossCalls is the M1 pin: a 3-call
// x 8000-token tool loop must expose the RUNNING turn total to the gate at
// each tool call — 8000, 16000, 24000 — not the per-call snapshot (8000
// every time). RED before the fix: recordTurnProviderUsage assigned `=` so
// the gate saw 8000 on all three calls and the cap never tripped.
func TestTurnTokensGate_SeesAccumulatedTotalAcrossCalls(t *testing.T) {
	loop, conv, recorder := newTurnTokensGateLoop(t, newTurnTokensGateChatter(3, 8000))
	if _, err := loop.reasoningCycle(context.Background(), conv, "conv-turn-tokens-gate"); err != nil {
		t.Fatalf("reasoningCycle: %v", err)
	}
	got := recorder.snapshot()
	want := []string{"8000", "16000", "24000"}
	if len(got) != len(want) {
		t.Fatalf("gate observations = %v, want %v (3 tool calls)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("gate observation %d = %q, want %q (full sequence %v)", i, got[i], want[i], got)
		}
	}
}

// TestTurnTokensGate_FinalTotalIsSumNoDoubleCount pins that the terminal
// report still equals the true sum (4 calls x 8000 = 32000) and that the
// no-op recordTurnTotal does not clobber or double-count the accumulated
// value.
func TestTurnTokensGate_FinalTotalIsSumNoDoubleCount(t *testing.T) {
	loop, conv, _ := newTurnTokensGateLoop(t, newTurnTokensGateChatter(3, 8000))
	if _, err := loop.reasoningCycle(context.Background(), conv, "conv-turn-tokens-gate"); err != nil {
		t.Fatalf("reasoningCycle: %v", err)
	}
	// 3 tool calls + 1 final stop call, each 8000 tokens.
	if got := loop.LastTurnTokens(); got != 32000 {
		t.Errorf("LastTurnTokens() = %d, want 32000 (4 calls x 8000)", got)
	}
}

// TestTurnTokensGate_ZeroUsageIgnoredAndReset pins two invariants: a zero
// usage report does not clobber an earlier count (absent usage is ignored),
// and resetLastTurnTokens clears the accumulated total at turn start.
func TestTurnTokensGate_ZeroUsageIgnoredAndReset(t *testing.T) {
	loop := NewAgentLoop("sess-turn-tokens-gate", "/tmp")
	loop.recordTurnProviderUsage(llm.TokenUsage{PromptTokens: 100, CompletionTokens: 100, TotalTokens: 200})
	if got := loop.LastTurnTokens(); got != 200 {
		t.Fatalf("LastTurnTokens() after first report = %d, want 200", got)
	}
	// A completely empty usage is absent and must not change the count.
	loop.recordTurnProviderUsage(llm.TokenUsage{})
	if got := loop.LastTurnTokens(); got != 200 {
		t.Errorf("LastTurnTokens() after empty usage = %d, want 200 (unchanged)", got)
	}
	// A non-zero usage with TotalTokens=0 adds nothing but is still "seen".
	loop.recordTurnProviderUsage(llm.TokenUsage{PromptTokens: 5})
	if got := loop.LastTurnTokens(); got != 200 {
		t.Errorf("LastTurnTokens() after zero-total usage = %d, want 200 (unchanged)", got)
	}
	loop.resetLastTurnTokens()
	if got := loop.LastTurnTokens(); got != 0 {
		t.Errorf("LastTurnTokens() after reset = %d, want 0", got)
	}
}

// TestTurnTokensGate_TerminalReportDoesNotClobber pins that recordTurnTotal
// no longer overwrites the accumulated sum, even if a caller passes a
// smaller value (the reply-guard re-entrant-cycle scenario).
func TestTurnTokensGate_TerminalReportDoesNotClobber(t *testing.T) {
	loop := NewAgentLoop("sess-turn-tokens-gate", "/tmp")
	loop.recordTurnProviderUsage(llm.TokenUsage{TotalTokens: 100})
	loop.recordTurnProviderUsage(llm.TokenUsage{TotalTokens: 150})
	loop.recordTurnTotal(150) // final-call snapshot: must NOT replace the sum
	if got := loop.LastTurnTokens(); got != 250 {
		t.Errorf("LastTurnTokens() after terminal report = %d, want 250 (accumulated sum preserved)", got)
	}
}

// Keep the tools import referenced even if the helper changes shape.
var _ tools.Tool = (*MockTool)(nil)
