package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// Pins for the reply-guard rewrite retry (agnes-reliability deferred
// observation): when the guard replaces a machine-shaped reply at the
// RunOnceWithParts response-assembly seam, the loop must nudge the model
// ONCE to rewrite the answer in plain language instead of shipping the
// canned apology — and the retry must be strictly bounded: a retry reply
// that trips the guard again ships the canned line (the pre-retry
// behavior). The handler.go choke point keeps single-shot semantics (its
// pins live in reply_guard_chokepoint_test.go / reply_guard_fallback_test.go
// and must stay green unchanged).

// rawDumpSample is a guard-tripping reply that ALSO passes the loop's
// final-text guards: no execution-action verbs (no unbacked-claims nudge),
// no announced-action shape. The 2026-09-13 dump form (observedToolResultDump
// in reply_guard_toolresult_test.go) fits: pure JSON with a memory_id key.
const rawDumpSample = observedToolResultDump

// dumpThenProseChatter scripts the failure shape: the FIRST completion is a
// raw tool-result dump (guard trips), the SECOND — after the rewrite nudge —
// is genuine prose (the recovery the retry exists to produce). The prose
// carries a per-call counter so the convergence detector can never see
// identical hashes.
type dumpThenProseChatter struct {
	callCount int
}

func (m *dumpThenProseChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	if m.callCount == 1 {
		return &llm.Response{
			Content: rawDumpSample,
			Usage:   llm.TokenUsage{TotalTokens: 1},
		}, nil
	}
	return &llm.Response{
		Content: "i stored your note and it came back as two memory entries rather than a readable answer.",
		Usage:   llm.TokenUsage{TotalTokens: 1},
	}, nil
}

func (m *dumpThenProseChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *dumpThenProseChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "dump-then-prose-chatter"}
}

// alwaysDumpChatter trips the guard on EVERY call — the bounded-cap shape:
// the retry must fire exactly once and the second dump must ship the canned
// replacement.
type alwaysDumpChatter struct {
	callCount int
}

func (m *alwaysDumpChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	// Pure-JSON dump (the guard's tool_result_json shape requires the
	// reply to START with '{'), varying per call so the convergence
	// detector stays quiet.
	return &llm.Response{
		Content: `{"memory_id":"` + strings.Repeat("a", m.callCount) + `","success":true}`,
		Usage:   llm.TokenUsage{TotalTokens: 1},
	}, nil
}

func (m *alwaysDumpChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *alwaysDumpChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "always-dump-chatter"}
}

// newGuardRetryLoop builds a loop wired like the guards integration tests.
// The dump replies carry no tool calls, so reasoningCycle's final-text path
// returns them directly to RunOnceWithParts where the guard seam fires.
func newGuardRetryLoop(t *testing.T, chatter llm.Chatter) *AgentLoop {
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

// TestGuardRetry_RecoversProseAnswer pins the happy path: dump on the first
// pass, prose after the rewrite nudge — the user gets the real answer, the
// canned line never ships, and the nudge is a user-role message (loop
// protocol preserved).
func TestGuardRetry_RecoversProseAnswer(t *testing.T) {
	chatter := &dumpThenProseChatter{}
	loop := newGuardRetryLoop(t, chatter)

	const convID = "conv-guard-retry-prose"
	reply, err := loop.RunOnce(context.Background(), "remember that the build passed", convID)
	if err != nil {
		t.Fatalf("RunOnce = error %v, want the recovered prose reply", err)
	}
	if !strings.Contains(reply, "stored your note") {
		t.Fatalf("reply = %q, want the model's rewritten prose answer", reply)
	}
	if strings.Contains(reply, "ask me to do something specific") {
		t.Fatalf("reply = %q, the canned apology must not ship when the retry recovers", reply)
	}
	// Exactly one extra reasoning cycle ran (the bounded retry — a full
	// fresh cycle, not a single model call).
	if chatter.callCount != 2 {
		t.Fatalf("LLM calls = %d, want exactly 2 (original + one bounded retry)", chatter.callCount)
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

// TestGuardRetry_CapShipsCannedOnSecondTrip is the bounded cap pin: a model
// that dumps BOTH times gets exactly one retry; the second dump ships the
// canned replacement — never a third model call, never an infinite loop.
func TestGuardRetry_CapShipsCannedOnSecondTrip(t *testing.T) {
	chatter := &alwaysDumpChatter{}
	loop := newGuardRetryLoop(t, chatter)

	const convID = "conv-guard-retry-cap"
	reply, err := loop.RunOnce(context.Background(), "remember that the build passed", convID)
	if err != nil {
		t.Fatalf("RunOnce = error %v, want the canned replacement, not a chain failure", err)
	}
	if !strings.Contains(reply, "raw data instead of an answer") {
		t.Fatalf("reply = %q, want the canned fallback after the retry also tripped the guard", reply)
	}
	if !strings.Contains(reply, "ask me to do something specific") {
		t.Fatalf("reply = %q, want the full canned line", reply)
	}
	if chatter.callCount != 2 {
		t.Fatalf("LLM calls = %d, want exactly 2 (the retry cap must stop a third call)", chatter.callCount)
	}
}

// TestGuardRetry_FlagResetsPerTurn pins the per-turn budget: after a turn
// spent the retry, a NEW turn gets a fresh one (mirror of the F-A3 nudge
// budget reset semantics).
func TestGuardRetry_FlagResetsPerTurn(t *testing.T) {
	loop := newGuardRetryLoop(t, &alwaysDumpChatter{})
	_ = loop
	loop.guardRetried = true
	loop.resetTurnGuards()
	if loop.guardRetried {
		t.Fatal("guardRetried must reset per turn (one retry per turn)")
	}
}

// TestGuardRetry_HandlersKeepSingleShot pins that the handler-facing seams
// are untouched: applyReplyGuardWithFallback and applyReplyGuardLogged
// replace once and never observe or set guardRetried — the turn-delivery
// path cannot continue a loop.
func TestGuardRetry_HandlersKeepSingleShot(t *testing.T) {
	loop := &AgentLoop{}
	got := applyReplyGuardWithFallback(rawDumpSample, nil, replyGuardContext{}, "")
	if !strings.Contains(got, "raw data instead of an answer") {
		t.Fatalf("handler seam changed: %q", got)
	}
	if loop.guardRetried {
		t.Fatal("the handler seam must not touch the loop retry flag")
	}
}
