package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// Pins for the TERMINATING-TOOL lane of the reply guard (loop seam twin of
// reply_guard_retry_test.go): a terminating tool's buildTerminateResponse
// product short-circuits the reasoning cycle, so the loop seam's guard in
// RunOnceWithParts never sees it. applyTerminateReplyGuard must classify the
// dump, run ONE bounded rewrite-nudge re-entry of reasoningCycle, ship the
// rewritten prose when it clears, and ship the canned line when the rewrite
// also dumps (or the retry errors).

// dumpThenProseTerminatingTool mirrors mockTerminatingLoopTool but returns a
// raw memory_store-style JSON dump — the exact shape the reply guard's
// tool_result_json rule replaces.
type dumpTerminatingTool struct {
	name string
	dump string
}

func (t *dumpTerminatingTool) Name() string        { return t.name }
func (t *dumpTerminatingTool) Description() string { return "terminating dump test tool" }
func (t *dumpTerminatingTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (t *dumpTerminatingTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return &tools.ToolResult{
		Success:   true,
		Result:    t.dump,
		Terminate: true,
	}, nil
}
func (t *dumpTerminatingTool) IsReadOnly(map[string]any) bool        { return false }
func (t *dumpTerminatingTool) IsConcurrencySafe(map[string]any) bool { return false }

func newTerminateGuardLoop(t *testing.T, chatter llm.Chatter) *AgentLoop {
	t.Helper()
	registry := NewPlaceholderToolRegistry()
	registry.Register(&dumpTerminatingTool{
		name: "memory_store",
		dump: `{"memory_id":"term-guard-0001","success":true,"type":"task"}`,
	})
	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{
			MaxIterations: 10,
			Guards:        DefaultGuardConfig(),
		}),
	)
	loop.executor = NewExecutor(registry, secChecker)
	return loop
}

// TestTerminateLane_GuardTripRewritesToProse pins the happy path: the
// terminating tool's dump trips the guard, ONE rewrite re-entry runs, and the
// rewritten prose ships instead of the canned line.
func TestTerminateLane_GuardTripRewritesToProse(t *testing.T) {
	chatter := &dumpThenProseChatter{}
	loop := newTerminateGuardLoop(t, chatter)

	// First completion: a tool call to the terminating tool. The retry
	// completion (served by dumpThenProseChatter.callCount==2) is prose.
	chatter.callCount = 0

	const convID = "conv-term-guard-prose"
	reply, err := loop.RunOnce(context.Background(), "remember that the build passed", convID)
	if err != nil {
		t.Fatalf("RunOnce = error %v, want the recovered prose reply", err)
	}
	if !strings.Contains(reply, "stored your note") {
		t.Fatalf("reply = %q, want the model's rewritten prose answer", reply)
	}
	if strings.Contains(reply, "memory_id") || strings.Contains(reply, "raw data instead of an answer") {
		t.Fatalf("reply = %q, neither the raw dump nor the canned line may ship when the retry recovers", reply)
	}
	// Exactly two model calls: the original tool-call turn + one bounded
	// rewrite re-entry.
	if chatter.callCount != 2 {
		t.Fatalf("LLM calls = %d, want exactly 2 (original + one bounded rewrite)", chatter.callCount)
	}
	// The rewrite nudge landed as a user-role message.
	conv := loop.conversations.Get(convID)
	sawNudge := false
	for _, msg := range conv.GetMessages() {
		if msg.Role == llm.RoleUser && strings.Contains(msg.Content, replyGuardRewriteNudge[len("[system: "):]) {
			sawNudge = true
		}
	}
	if !sawNudge {
		t.Fatal("the rewrite nudge never landed in the conversation as a user message")
	}
}

// TestTerminateLane_SecondTripShipsCanned pins the bounded cap: a model that
// dumps on the rewrite too gets the canned replacement — exactly one extra
// call, never a loop.
func TestTerminateLane_SecondTripShipsCanned(t *testing.T) {
	chatter := &alwaysDumpChatter{}
	loop := newTerminateGuardLoop(t, chatter)

	const convID = "conv-term-guard-cap"
	reply, err := loop.RunOnce(context.Background(), "remember that the build passed", convID)
	if err != nil {
		t.Fatalf("RunOnce = error %v, want the canned replacement, not a chain failure", err)
	}
	if !strings.Contains(reply, "raw data instead of an answer") ||
		!strings.Contains(reply, "ask me to do something specific") {
		t.Fatalf("reply = %q, want the canned fallback after the rewrite also tripped the guard", reply)
	}
	if chatter.callCount != 2 {
		t.Fatalf("LLM calls = %d, want exactly 2 (the rewrite cap must stop a third call)", chatter.callCount)
	}
}

// TestTerminateLane_ProsePassesByteIdentical pins the no-trip path: a
// terminating tool whose result is genuine prose (e.g. a well-written ack)
// passes through byte-identical with no extra model call.
func TestTerminateLane_ProsePassesByteIdentical(t *testing.T) {
	chatter := newMockChatter(
		&llm.Response{
			Content:      "checking the memory",
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "tc-1", Type: "function", Function: llm.ToolCallFunction{Name: "memory_store", Arguments: "{}"}},
			},
		},
	)
	registry := NewPlaceholderToolRegistry()
	registry.Register(&dumpTerminatingTool{name: "memory_store", dump: "your note is stored and searchable."})
	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{MaxIterations: 10, Guards: DefaultGuardConfig()}),
	)
	loop.executor = NewExecutor(registry, secChecker)

	reply, err := loop.RunOnce(context.Background(), "remember the build passed", "conv-term-prose")
	if err != nil {
		t.Fatalf("RunOnce = error %v", err)
	}
	if reply != "your note is stored and searchable." {
		t.Fatalf("reply = %q, want byte-identical prose passthrough", reply)
	}
	if chatter.callCount != 1 {
		t.Fatalf("LLM calls = %d, want 1 (no rewrite for prose)", chatter.callCount)
	}
}

// TestTerminateLane_FlagResetsPerTurn pins the per-turn budget reset for the
// terminate lane's rewrite flag.
func TestTerminateLane_FlagResetsPerTurn(t *testing.T) {
	loop := &AgentLoop{}
	loop.terminateGuardRetried = true
	loop.resetTurnGuards()
	if loop.terminateGuardRetried {
		t.Fatal("terminateGuardRetried must reset per turn (one rewrite per turn)")
	}
}
