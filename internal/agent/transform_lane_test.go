package agent

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/pkg/security"
)

// Proof tests for wiring-gap remediation leaf 02 (transform lane
// activation): the hook registry's TransformContext registrations had ZERO
// production callers, so the daemon's registered secret-obfuscation pass
// never ran. reasoningCycle must fire RunTransformContext per iteration
// beside RunPrepareNextTurn, so that:
//
//   - a secret in the user input reaches the provider as a #XXXX#
//     placeholder (delete the wiring call and these tests FAIL: the raw
//     secret reaches the provider), and
//   - conversation history keeps the ORIGINALS (the clone-before-transform
//     rule) - a later turn's windowed messages still carry the raw secret,
//     never the placeholder.
//
// The chatter records every content the "provider" receives.

type transformLaneChatter struct {
	received []string
}

func (m *transformLaneChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	for _, msg := range messages {
		m.received = append(m.received, msg.Content)
	}
	return &llm.Response{
		Content: "ack.",
		Usage:   llm.TokenUsage{TotalTokens: 1},
	}, nil
}

func (m *transformLaneChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *transformLaneChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "transform-lane-chatter"}
}

func newTransformLaneLoop(t *testing.T, chatter llm.Chatter, obfuscator *intsecurity.SecretObfuscator) *AgentLoop {
	t.Helper()
	reg := NewHookRegistry(nil)
	if h := NewSecretObfuscationHook(obfuscator, slog.Default()); h != nil {
		reg.RegisterTransformContext("secret-obfuscation", HookPriorityCritical, h)
	}
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithHookRegistry(reg),
		WithAgentConfig(AgentConfig{
			MaxIterations: 5,
			Guards:        DefaultGuardConfig(),
		}),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	return loop
}

// TestTransformLane_ObfuscatedContentReachesProvider is the wiring proof:
// with the SecretObfuscationHook registered and a fake provider, the
// provider must receive #XXXX#-obfuscated user content - NOT the raw
// secret. Removing the RunTransformContext call in reasoningCycle makes
// this fail (raw secret leaks to the provider).
func TestTransformLane_ObfuscatedContentReachesProvider(t *testing.T) {
	// Unique secret planted in the env so the obfuscator (which loads env
	// secrets at construction) picks it up.
	const secret = "transformlane-secret-value-77"
	t.Setenv("TRANSFORM_LANE_TEST_SECRET", secret)

	chatter := &transformLaneChatter{}
	loop := newTransformLaneLoop(t, chatter, intsecurity.NewSecretObfuscator())

	const convID = "conv-transform-lane-provider"
	_, err := loop.RunOnce(context.Background(), "the credential is "+secret+", store it", convID)
	if err != nil {
		t.Fatalf("RunOnce = error %v, want a completed turn", err)
	}
	if len(chatter.received) == 0 {
		t.Fatal("provider received no messages")
	}
	placeholderRe := regexp.MustCompile(`#[0-9A-F]{12}#`)
	sawObfuscated := false
	for i, got := range chatter.received {
		if strings.Contains(got, secret) {
			t.Fatalf("provider call %d received the RAW secret: %q", i, got)
		}
		if strings.Contains(got, "the credential is") && placeholderRe.MatchString(got) {
			sawObfuscated = true
		}
	}
	if !sawObfuscated {
		t.Fatalf("provider never received the obfuscated user content; got %v", chatter.received)
	}
}

// TestTransformLane_HistoryKeepsOriginals pins history non-mutation: the
// transform lane must shape only what the model sees THIS iteration. After
// the turn, the conversation's windowed messages still carry the ORIGINAL
// secret, not the placeholder - and a SECOND turn's provider call sees a
// fresh obfuscation of the original again (proving the clone fed the
// transform, never the history).
func TestTransformLane_HistoryKeepsOriginals(t *testing.T) {
	const secret = "transformlane-history-secret-99"
	t.Setenv("TRANSFORM_LANE_TEST_SECRET", secret)

	chatter := &transformLaneChatter{}
	loop := newTransformLaneLoop(t, chatter, intsecurity.NewSecretObfuscator())

	const convID = "conv-transform-lane-history"
	if _, err := loop.RunOnce(context.Background(), "the credential is "+secret+", store it", convID); err != nil {
		t.Fatalf("first RunOnce = error %v", err)
	}

	// History: originals only.
	conv := loop.conversations.Get(convID)
	if conv == nil {
		t.Fatal("conversation missing after turn")
	}
	originalSeen := false
	for _, msg := range conv.GetMessages() {
		if strings.Contains(msg.Content, secret) {
			originalSeen = true
		}
		if regexp.MustCompile(`#[0-9A-F]{12}#`).MatchString(msg.Content) {
			t.Fatalf("history carries a placeholder instead of the original: %q", msg.Content)
		}
	}
	if !originalSeen {
		t.Fatal("history lost the original secret; the transform mutated history")
	}

	// Second turn: the provider again sees a placeholder (obfuscation ran
	// on the cloned window including the history original), never the raw
	// secret.
	chatter.received = nil
	if _, err := loop.RunOnce(context.Background(), "thanks, what did I send?", convID); err != nil {
		t.Fatalf("second RunOnce = error %v", err)
	}
	for i, got := range chatter.received {
		if strings.Contains(got, secret) {
			t.Fatalf("second-turn provider call %d received the RAW secret from history: %q", i, got)
		}
	}
}

// TestTransformLane_FixpointCap pins the fixpoint discipline: a hook that
// ALWAYS claims Modified must not spin the loop; the transform lane stops
// at the cap (5 passes).
type alwaysModifiedTransform struct {
	runs int
}

func (h *alwaysModifiedTransform) TransformContext(_ context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform {
	h.runs++
	return ContextTransform{Messages: messages, ToolDefs: toolDefs, Modified: true, Reason: "always"}
}

func TestTransformLane_FixpointCap(t *testing.T) {
	reg := NewHookRegistry(nil)
	hook := &alwaysModifiedTransform{}
	reg.RegisterTransformContext("always-modified", HookPriorityCritical, hook)

	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(&transformLaneChatter{}),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithHookRegistry(reg),
		WithAgentConfig(AgentConfig{MaxIterations: 2, Guards: DefaultGuardConfig()}),
	)
	loop.security = security.NewPermissionChecker(security.Config{})

	if _, err := loop.RunOnce(context.Background(), "hello", "conv-transform-cap"); err != nil {
		t.Fatalf("RunOnce = error %v", err)
	}
	if hook.runs == 0 {
		t.Fatal("transform hook never ran: the lane is not wired")
	}
	if hook.runs > maxTransformFixpointPasses {
		t.Fatalf("transform hook ran %d times in one iteration; cap is %d", hook.runs, maxTransformFixpointPasses)
	}
}
