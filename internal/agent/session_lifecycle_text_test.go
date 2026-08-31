package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/bus"
)

// TestSessionLifecycleResultCarriesTurnText pins the harness-eval leaf 12
// contract: session-end hooks receive the turn's user + assistant text so
// post-session fact extraction can run on the dialogue.
func TestSessionLifecycleResultCarriesTurnText(t *testing.T) {
	mb := bus.New(nil, rewindTestLogger())
	defer mb.Close()

	loop := NewAgentLoop(
		"sess-ctx",
		t.TempDir(),
		WithMessageBus(mb),
		WithLoopLogger(rewindTestLogger()),
		WithLLMChatter(newMockChatter(llmResponse("the assistant says hi"))),
		WithHookRegistry(NewHookRegistry(rewindTestLogger())),
	)
	t.Cleanup(loop.stopRewake)

	var got SessionLifecycleResult
	hr := loop.HookRegistry()
	if hr == nil {
		t.Fatal("nil hook registry")
	}
	hr.RegisterSessionEndHook("probe", HookPriorityMonitor, endProbe{fn: func(r SessionLifecycleResult) { got = r }})

	if _, err := loop.RunOnce(t.Context(), "I prefer aisle seats", "conv-ctx"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got.UserMessage != "I prefer aisle seats" {
		t.Errorf("UserMessage = %q, want the turn's user input", got.UserMessage)
	}
	if got.AssistantMessage != "the assistant says hi" {
		t.Errorf("AssistantMessage = %q, want the turn's final text", got.AssistantMessage)
	}
}

type endProbe struct{ fn func(SessionLifecycleResult) }

func (p endProbe) OnSessionEnd(_ context.Context, _ SessionLifecycleState, result SessionLifecycleResult) error {
	p.fn(result)
	return nil
}
