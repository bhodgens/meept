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

// speakLoopTestTool is a trivial read-only tool used to force a tool-call
// turn shape in the loop speak tests. When named reply_to_user it models
// the builtin: SetReplyFunc stores the loop-injected carrier and Execute
// delivers through it (agent.ReplyFuncSetter, satisfied structurally).
type speakLoopTestTool struct {
	name    string
	mu      sync.Mutex
	carrier func(text string) error
}

func (t *speakLoopTestTool) Name() string        { return t.name }
func (t *speakLoopTestTool) Description() string { return "speak loop test tool" }
func (t *speakLoopTestTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (t *speakLoopTestTool) SetReplyFunc(fn func(text string) error) {
	if fn == nil {
		return
	}
	t.mu.Lock()
	t.carrier = fn
	t.mu.Unlock()
}
func (t *speakLoopTestTool) Execute(_ context.Context, args map[string]any) (any, error) {
	if t.name == "reply_to_user" {
		t.mu.Lock()
		carrier := t.carrier
		t.mu.Unlock()
		if carrier == nil {
			return tools.NewErrorResult("reply_to_user tool is not available: no reply carrier configured"), nil
		}
		text, _ := args["text"].(string)
		if err := carrier(text); err != nil {
			return nil, err
		}
	}
	return &tools.ToolResult{Success: true, Result: "ok"}, nil
}
func (t *speakLoopTestTool) IsReadOnly(map[string]any) bool        { return true }
func (t *speakLoopTestTool) IsConcurrencySafe(map[string]any) bool { return true }

// speakingPublisher is a concurrency-safe fake SpeakPublisher.
type speakingPublisher struct {
	mu    sync.Mutex
	calls []speakCall
}

func (p *speakingPublisher) publish(kind SpeakKind, text, sessionID, conversationID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, speakCall{kind, text, sessionID, conversationID})
	return nil
}

func (p *speakingPublisher) recorded() []speakCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]speakCall, len(p.calls))
	copy(out, p.calls)
	return out
}

// newSpeakTestLoop builds a minimal loop wired with the given chatter and
// speak router, mirroring the guards-test harness.
func newSpeakTestLoop(t *testing.T, chatter llm.Chatter, router *SpeakRouter, attached, isolated bool) *AgentLoop {
	t.Helper()
	registry := NewPlaceholderToolRegistry()
	registry.Register(&speakLoopTestTool{name: "reply_to_user"})
	loop := NewAgentLoop("sess-speak", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(security.NewPermissionChecker(security.Config{})),
		WithMessageBus(bus.New(nil, slog.New(slog.DiscardHandler))),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
		WithLoopLogger(slog.New(slog.DiscardHandler)),
		WithSpeakRouter(router),
		WithSessionAttached(attached),
		WithIsolatedChild(isolated),
	)
	loop.executor = NewExecutor(registry, security.NewPermissionChecker(security.Config{}))
	return loop
}

// replyToUserResponse builds a chatter response that calls reply_to_user.
func replyToUserResponse(callID int, text string) *llm.Response {
	return &llm.Response{
		Content:      "",
		FinishReason: "tool_calls",
		Usage:        llm.TokenUsage{TotalTokens: 10},
		ToolCalls: []llm.ToolCall{{
			ID:   fmt.Sprintf("tc-%d", callID),
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "reply_to_user",
				Arguments: fmt.Sprintf(`{"text":%q}`, text),
			},
		}},
	}
}

// TestRunOnce_DetachedFinalTextNotifies verifies leaf 11's core seam: a
// DETACHED run (attached=false, isolated=false) whose final text is
// non-empty must deliver exactly one SpeakNotify, even though the model
// never called reply_to_user.
func TestRunOnce_DetachedFinalTextNotifies(t *testing.T) {
	chatter := newMockChatter(
		&llm.Response{Content: "goal round finished, all checks green", FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}},
	)
	pub := &speakingPublisher{}
	router := NewSpeakRouter(pub.publish)

	loop := newSpeakTestLoop(t, chatter, router, false, false)
	_, err := loop.RunOnce(context.Background(), "run the nightly goal", "conv-speak-detached")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	calls := pub.recorded()
	if len(calls) != 1 {
		t.Fatalf("notify deliveries = %d, want 1 (calls: %+v)", len(calls), calls)
	}
	if calls[0].kind != SpeakNotify {
		t.Errorf("kind = %s, want notify", calls[0].kind)
	}
	if calls[0].text != "goal round finished, all checks green" {
		t.Errorf("text = %q, want final response", calls[0].text)
	}
	if calls[0].sessionID != "sess-speak" || calls[0].conversationID != "conv-speak-detached" {
		t.Errorf("ids = %s/%s, want sess-speak/conv-speak-detached", calls[0].sessionID, calls[0].conversationID)
	}
}

// TestRunOnce_DetachedToolNotifyThenFinalText_Dedup verifies Task 4: when
// the model already called reply_to_user mid-turn AND produces final text,
// the user receives exactly ONE notify (the tool's), not two.
func TestRunOnce_DetachedToolNotifyThenFinalText_Dedup(t *testing.T) {
	chatter := newMockChatter(
		replyToUserResponse(1, "started the migration"),
		&llm.Response{Content: "migration finished cleanly", FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}},
	)
	pub := &speakingPublisher{}
	router := NewSpeakRouter(pub.publish)

	loop := newSpeakTestLoop(t, chatter, router, false, false)
	_, err := loop.RunOnce(context.Background(), "migrate the db", "conv-speak-dedup")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	calls := pub.recorded()
	if len(calls) != 1 {
		t.Fatalf("notify deliveries = %d, want 1 (tool notify + final text must dedup); calls: %+v", len(calls), calls)
	}
	if calls[0].kind != SpeakNotify || calls[0].text != "started the migration" {
		t.Errorf("call = %+v, want the tool's notify", calls[0])
	}
}

// TestRunOnce_IsolatedChild_NeverNotifies verifies C4 end-to-end through the
// loop: an isolated child's final text classifies to SpeakParent and the
// notify surface is never touched.
func TestRunOnce_IsolatedChild_NeverNotifies(t *testing.T) {
	chatter := newMockChatter(
		&llm.Response{Content: "child report: parser rewritten", FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}},
	)
	pub := &speakingPublisher{}
	router := NewSpeakRouter(pub.publish)

	loop := newSpeakTestLoop(t, chatter, router, true, true) // attached+isolated (C4 fail-closed)
	_, err := loop.RunOnce(context.Background(), "do isolated work", "conv-speak-isolated")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	calls := pub.recorded()
	if len(calls) != 1 {
		t.Fatalf("deliveries = %d, want 1 (parent report only); calls: %+v", len(calls), calls)
	}
	if calls[0].kind != SpeakParent {
		t.Errorf("kind = %s, want parent (C4)", calls[0].kind)
	}
}

// TestRunOnce_AttachedRouterNil_BubbleOnly verifies the legacy path: with no
// router configured, RunOnce behaves exactly as before (no routed delivery
// attempted, no panic).
func TestRunOnce_AttachedRouterNil_BubbleOnly(t *testing.T) {
	chatter := newMockChatter(
		&llm.Response{Content: "plain chat answer", FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}},
	)
	loop := newSpeakTestLoop(t, chatter, nil, true, false)
	resp, err := loop.RunOnce(context.Background(), "hello", "conv-speak-legacy")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if resp != "plain chat answer" {
		t.Errorf("response = %q, want unchanged bubble text", resp)
	}
}

// TestRunOnce_DetachedEmptyFinalText_NoNotify verifies the C3 empty-text
// rule end-to-end: a detached run that ends with empty final text must not
// notify.
func TestRunOnce_DetachedEmptyFinalText_NoNotify(t *testing.T) {
	chatter := newMockChatter(
		&llm.Response{Content: "   ", FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}},
	)
	pub := &speakingPublisher{}
	router := NewSpeakRouter(pub.publish)

	loop := newSpeakTestLoop(t, chatter, router, false, false)
	if _, err := loop.RunOnce(context.Background(), "produce nothing", "conv-speak-empty"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if calls := pub.recorded(); len(calls) != 0 {
		t.Errorf("deliveries = %d, want 0 (empty final text)", len(calls))
	}
}
