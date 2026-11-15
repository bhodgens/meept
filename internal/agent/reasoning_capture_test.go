package agent

// Reasoning-TOKEN RETENTION (voice path, part b) — agent-side pins.
//
//   - TestReasoningSink_DefaultPathDrops: the DEFAULT chat path (no sink
//     armed) keeps llm.Response.Reasoning dropped, exactly as before —
//     the reply text is thinking-stripped and history (the persisted
//     assistant message via AddAssistantMessage) carries no reasoning.
//   - TestReasoningSinkArmed_CapturesWithoutTouchingReplyOrHistory:
//     arming the opt-in sink captures the reasoning into the sink while
//     the returned reply AND the persisted assistant message stay
//     byte-identical to the no-sink case. This is the non-invasiveness
//     proof: the capture seam is add-only and cannot leak reasoning into
//     the reply (what becomes the text sent to TTS) or into the
//     conversation history.
//   - ReasoningSink unit checks: nil safety and last-write-wins.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

const testReasoning = "the user seems to be asking about the current time, so i will consult the clock context"

func newReasoningRetentionLoop(t *testing.T, resp *llm.Response) (*AgentLoop, *mockChatter) {
	t.Helper()
	chatter := newMockChatter(resp)
	loop := NewAgentLoop("sess-reasonkeep", t.TempDir(),
		WithLLMChatter(chatter),
		WithSecurityChecker(security.NewPermissionChecker(security.Config{})),
		WithLoopLogger(slog.New(slog.DiscardHandler)),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	)
	return loop, chatter
}

func reasonResponse(reply string) *llm.Response {
	return &llm.Response{
		Content:      reply,
		Reasoning:    testReasoning,
		FinishReason: "stop",
		Usage:        llm.TokenUsage{TotalTokens: 5},
	}
}

func lastAssistantContent(t *testing.T, loop *AgentLoop, convID string) string {
	t.Helper()
	conv := loop.conversations.Get(convID)
	if conv == nil {
		t.Fatalf("conversation %q not found", convID)
	}
	msgs := conv.GetMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleAssistant {
			return msgs[i].Content
		}
	}
	t.Fatalf("no assistant message in conversation %q", convID)
	return ""
}

// TestReasoningSink_DefaultPathDrops: with NO sink armed the reasoning
// channel is dropped as before — the returned reply is the clean text and
// the persisted assistant message holds no reasoning.
func TestReasoningSink_DefaultPathDrops(t *testing.T) {
	loop, _ := newReasoningRetentionLoop(t, reasonResponse("the current time is available"))

	reply, err := loop.RunOnce(context.Background(), "what time is it", "conv-rs-drop")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if reply != "the current time is available" {
		t.Errorf("reply = %q, want clean text", reply)
	}
	if strings.Contains(reply, testReasoning) || strings.Contains(reply, "thinking") {
		t.Errorf("reasoning leaked into the reply: %q", reply)
	}
	if got := lastAssistantContent(t, loop, "conv-rs-drop"); got != "the current time is available" {
		t.Errorf("persisted assistant message = %q, want clean text (no reasoning in history)", got)
	}
}

// TestReasoningSinkArmed_CapturesWithoutTouchingReplyOrHistory: arming the
// opt-in sink captures the reasoning, yet the reply text and the persisted
// assistant message are byte-identical to the no-sink path.
func TestReasoningSinkArmed_CapturesWithoutTouchingReplyOrHistory(t *testing.T) {
	loop, _ := newReasoningRetentionLoop(t, reasonResponse("the current time is available"))
	sink := &ReasoningSink{}
	ctx := WithReasoningSink(context.Background(), sink)

	reply, err := loop.RunOnce(ctx, "what time is it", "conv-rs-capture")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !sink.Has() {
		t.Fatal("sink did not capture non-empty reasoning")
	}
	if got := sink.Get(); got != testReasoning {
		t.Errorf("captured reasoning = %q, want %q", got, testReasoning)
	}
	// The reply (what would be spoken) and the history stay clean.
	if reply != "the current time is available" {
		t.Errorf("reply = %q, want clean text (byte-identical to no-sink)", reply)
	}
	if strings.Contains(reply, testReasoning) {
		t.Errorf("reasoning leaked into the reply despite capture: %q", reply)
	}
	if got := lastAssistantContent(t, loop, "conv-rs-capture"); got != "the current time is available" {
		t.Errorf("persisted assistant message = %q, want clean text (reasoning never persists)", got)
	}
}

func TestReasoningSink_Unit(t *testing.T) {
	// nil sink is a no-op on every method.
	var sink *ReasoningSink
	sink.Capture("x")
	if sink.Get() != "" || sink.Has() {
		t.Fatal("nil sink must be inert")
	}
	// Last write wins.
	s := &ReasoningSink{}
	if s.Has() {
		t.Fatal("fresh sink must not Has()")
	}
	s.Capture("")
	if s.Has() || s.Get() != "" {
		t.Fatal("empty capture must be ignored")
	}
	s.Capture("first")
	s.Capture("second")
	if s.Get() != "second" {
		t.Errorf("Get() = %q, want last write wins", s.Get())
	}
	// Context helper is nil-safe and round-trips.
	if reasoningSinkFromContext(nil) != nil {
		t.Fatal("reasoningSinkFromContext(nil) must be nil")
	}
	ctx := WithReasoningSink(context.Background(), s)
	if got := reasoningSinkFromContext(ctx); got == nil || got != s {
		t.Fatal("context does not carry the sink")
	}
	if reasoningSinkFromContext(context.Background()) != nil {
		t.Fatal("a bare context must carry no sink (chat lane byte-identical)")
	}
}
