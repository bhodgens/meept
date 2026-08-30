package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/skills"
)

// stateHookSentinel is the loop-path response content. State-mode dispatch
// must return the state protocol's answer instead; the normal path must
// return exactly this.
const stateHookSentinel = "NORMAL-PATH-SENTINEL"

// stateAnswerAction is a minimal well-formed state protocol response.
const stateAnswerAction = `{"action":{"answer":"state-answer"}}`

// newStateHookLoop builds a loop with both a loop-path chatter (returning the
// sentinel) and a wired real SkillStateRuntime driven by the given scripted
// state responses. The runtime's tool runner is a no-op (non-nil, as Run
// requires), never invoked by answer-only scripts.
func newStateHookLoop(t *testing.T, sessionID string, stateResponses ...string) (*AgentLoop, *stateChatterStub) {
	t.Helper()
	loop := NewAgentLoop(sessionID, t.TempDir(),
		WithLLMChatter(newMockChatter(&llm.Response{Content: stateHookSentinel, FinishReason: "stop"})))
	ch := &stateChatterStub{responses: stateResponses}
	rt := NewSkillStateRuntime(loop, SkillStateConfig{}, nil)
	rt.chatter = ch
	rt.WithToolRunner(func(_ context.Context, _ []llm.ToolCall) []*ExecutionResult { return nil })
	WithSkillStateRuntime(rt)(loop)
	return loop, ch
}

// TestRunWithSkill_StateModeDispatch verifies the SKILL.state dispatch hook
// (05-config-wiring.md Task 3): a skill declaring state:true routes to the
// wired runtime before any conversation mutation; a plain skill never reaches
// the runtime; an unwired runtime (skills.state disabled) degrades to the
// normal path. The sentinels make the executed path observable: state mode
// answers through the runtime's scripted chatter, the normal path through the
// loop's.
func TestRunWithSkill_StateModeDispatch(t *testing.T) {
	t.Run("state_true_routes_to_runtime", func(t *testing.T) {
		loop, ch := newStateHookLoop(t, "hook-state-true", stateAnswerAction)

		skill := &skills.Skill{Name: "state-skill", Body: "STATE BODY", State: true}
		out, err := loop.RunWithSkill(context.Background(), skill, "do the thing", "conv-state-1")
		if err != nil {
			t.Fatalf("RunWithSkill: %v", err)
		}
		if out != "state-answer" {
			t.Fatalf("output: got %q want state protocol answer (sentinel would mean dispatch missed)", out)
		}
		if ch.calls != 1 {
			t.Fatalf("state chatter calls: got %d want 1", ch.calls)
		}
		// The conversation must be untouched: the hook fires before the
		// skill body is installed as system prompt.
		conv := loop.conversations.Get("conv-state-1")
		if conv == nil {
			t.Fatal("conversation missing (normal path created it?)")
		}
		if got := conv.GetSystemPrompt(); got != "" {
			t.Fatalf("state mode must not touch conversation prompt, got %q", got)
		}
	})

	t.Run("state_false_never_calls_runtime", func(t *testing.T) {
		loop, ch := newStateHookLoop(t, "hook-state-false", stateAnswerAction)

		skill := &skills.Skill{Name: "plain-skill", Body: "PLAIN BODY"}
		out, err := loop.RunWithSkill(context.Background(), skill, "plain input", "conv-plain-1")
		if err != nil {
			t.Fatalf("RunWithSkill normal path: %v", err)
		}
		if ch.calls != 0 {
			t.Fatalf("state runtime must not run for State=false, got %d chatter calls", ch.calls)
		}
		if out != stateHookSentinel {
			t.Fatalf("normal path output: got %q want %q", out, stateHookSentinel)
		}
		conv := loop.conversations.Get("conv-plain-1")
		if conv == nil {
			t.Fatal("conversation missing on normal path")
		}
		if got := conv.GetSystemPrompt(); got != "PLAIN BODY" {
			t.Fatalf("normal path must install skill body as prompt, got %q", got)
		}
	})

	t.Run("state_true_nil_runtime_falls_back", func(t *testing.T) {
		// No runtime wired (skills.state disabled): State=true must degrade
		// to the normal conversation path, not error.
		loop := NewAgentLoop("hook-state-nil", t.TempDir(),
			WithLLMChatter(newMockChatter(&llm.Response{Content: stateHookSentinel, FinishReason: "stop"})))
		skill := &skills.Skill{Name: "orphan-state-skill", Body: "STATE BODY", State: true}
		out, err := loop.RunWithSkill(context.Background(), skill, "input", "conv-orphan-1")
		if err != nil {
			t.Fatalf("RunWithSkill with nil runtime: %v", err)
		}
		if out != stateHookSentinel {
			t.Fatalf("fallback output: got %q want %q", out, stateHookSentinel)
		}
		conv := loop.conversations.Get("conv-orphan-1")
		if conv == nil {
			t.Fatal("conversation missing on fallback path")
		}
		if got := conv.GetSystemPrompt(); got != "STATE BODY" {
			t.Fatalf("fallback must install skill body as prompt, got %q", got)
		}
	})

	t.Run("state_true_runtime_error_propagates", func(t *testing.T) {
		loop := NewAgentLoop("hook-state-err", t.TempDir(),
			WithLLMChatter(newMockChatter(&llm.Response{Content: stateHookSentinel, FinishReason: "stop"})))
		ch := &stateChatterStub{responses: []string{stateAnswerAction}, chatErr: errors.New("state boom")}
		rt := NewSkillStateRuntime(loop, SkillStateConfig{}, nil)
		rt.chatter = ch
		rt.WithToolRunner(func(_ context.Context, _ []llm.ToolCall) []*ExecutionResult { return nil })
		WithSkillStateRuntime(rt)(loop)

		skill := &skills.Skill{Name: "state-skill-err", Body: "STATE BODY", State: true}
		_, err := loop.RunWithSkill(context.Background(), skill, "input", "conv-err-1")
		if err == nil || !strings.Contains(err.Error(), "state boom") {
			t.Fatalf("runtime error must propagate, got %v", err)
		}
		// The failed state run must not leave the skill body installed as a
		// conversation system prompt (dispatch happened before SetSystemPrompt).
		if conv := loop.conversations.Get("conv-err-1"); conv != nil && conv.GetSystemPrompt() != "" {
			t.Fatalf("failed state run must not mutate conversation prompt, got %q", conv.GetSystemPrompt())
		}
	})
}

// TestWithSkillStateRuntime_NilGuard verifies the option's nil guards: an
// untyped nil and a typed-nil *SkillStateRuntime must both leave the field
// unset, and a real runtime must be retrievable.
func TestWithSkillStateRuntime_NilGuard(t *testing.T) {
	loop := NewAgentLoop("hook-nilguard", t.TempDir())
	WithSkillStateRuntime(nil)(loop)
	if got := loop.SkillStateRuntime(); got != nil {
		t.Fatalf("untyped nil must not wire a runtime, got %v", got)
	}
	var typedNil *SkillStateRuntime
	WithSkillStateRuntime(typedNil)(loop)
	if got := loop.SkillStateRuntime(); got != nil {
		t.Fatalf("typed nil must not wire a runtime, got %v", got)
	}
	real := NewSkillStateRuntime(loop, SkillStateConfig{}, nil)
	WithSkillStateRuntime(real)(loop)
	if got := loop.SkillStateRuntime(); got != real {
		t.Fatalf("real runtime must be stored, got %v want %v", got, real)
	}
}
