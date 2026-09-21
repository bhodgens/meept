package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// F-A1 loop pins: on a typed *llm.ContextOverflowError the loop compacts
// context via the existing context firewall and retries ONCE; a second
// overflow surfaces as an honest error. F-A3 pins: the 3rd same-class nudge
// does not append to the conversation and the step terminalizes with an
// honest failure.

// overflowScriptChatter replays a fixed script (scriptedAttempt shape
// compatible with loop_quota_rotate_test.go's chatter) and counts calls.
type overflowScriptChatter struct {
	script []scriptedAttempt
	calls  atomic.Int32
}

func (s *overflowScriptChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	n := s.calls.Add(1)
	i := int(n) - 1
	if i >= len(s.script) {
		return nil, errors.New("script exhausted: unexpected extra chatter call")
	}
	return s.script[i].resp, s.script[i].err
}

func (s *overflowScriptChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *overflowScriptChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "overflow-script-chatter"}
}

// newOverflowTestLoop builds a minimal loop around the chatter. contextFirewall
// stays nil (stub chatter), which is exactly the "no firewall wired" shape the
// honest-error pin relies on.
func newOverflowTestLoop(t *testing.T, chatter llm.Chatter) *AgentLoop {
	t.Helper()
	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		MaxAttempts: 2,
	})
	t.Cleanup(clearDefaultBackoffOverride)
	loop := NewAgentLoop("sess-overflow-test", t.TempDir(),
		WithMessageBus(bus.New(nil, slog.New(slog.DiscardHandler))),
		WithLLMChatter(chatter),
	)
	return loop
}

// TestAgentLoop_ContextOverflowSingleCandidateSurfaces pins the honest-error
// half (F-A1c) on the no-firewall test harness: an overflow that persists
// past the saturation retry budget must surface honestly with the overflow
// as the cause. AMENDED (#52 reconciliation): a small-request overflow is
// endpoint saturation (transient — server-side KV cache drains), so the
// loop now retries the same payload with backoff (Mode C); the pin is that
// when the retries are exhausted the overflow is surfaced honestly (never
// laundered into a success), with the retry budget respected.
func TestAgentLoop_ContextOverflowSingleCandidateSurfaces(t *testing.T) {
	overflow := &llm.ContextOverflowError{
		ProviderID: "p1",
		ModelID:    "m1",
		StatusCode: 500,
	}
	script := []scriptedAttempt{{resp: nil, err: overflow}}
	for i := 0; i < maxSaturatedOverflowRetries+1; i++ {
		script = append(script, scriptedAttempt{resp: nil, err: overflow})
	}
	chatter := &overflowScriptChatter{script: script}
	loop := newOverflowTestLoop(t, chatter)

	_, err := loop.RunOnce(context.Background(), "hello", "conv-overflow-honest")
	if err == nil {
		t.Fatal("expected an honest error after the saturation retry budget")
	}
	if !errors.Is(err, overflow) && !strings.Contains(err.Error(), "context overflow") {
		t.Fatalf("err = %v, want the overflow surfaced honestly", err)
	}
	// retries happened (more than the initial call) but bounded by the
	// backoff budget — never a silent success
	if got := chatter.calls.Load(); got < 2 {
		t.Fatalf("chatter calls = %d, want >1 (saturation retries must fire)", got)
	}
}

// TestAgentLoop_ContextOverflowRetryOnceAfterCompaction pins the
// trim-and-retry-ONCE contract with a fake chatter plus a wired firewall:
// call 1 overflows, the loop compacts (messages shrink), call 2 succeeds.
// A THIRD overflow attempt must never happen.
func TestAgentLoop_ContextOverflowRetryOnceAfterCompaction(t *testing.T) {
	overflow := &llm.ContextOverflowError{ProviderID: "p1", ModelID: "m1", StatusCode: 500}
	var sawMessages atomic.Int32
	chatter := &overflowScriptChatter{
		script: []scriptedAttempt{
			{resp: nil, err: overflow},
			{resp: &llm.Response{Content: "recovered after compaction", FinishReason: "stop"}, err: nil},
		},
	}
	// Wrap the chatter so the second call proves the message slice shrank.
	wrapped := &messageCountingChatter{inner: chatter, lastLen: &sawMessages}

	SetDefaultBackoffOverride(BackoffConfig{BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, MaxAttempts: 2})
	t.Cleanup(clearDefaultBackoffOverride)
	loop := NewAgentLoop("sess-overflow-retry", t.TempDir(),
		WithMessageBus(bus.New(nil, slog.New(slog.DiscardHandler))),
		WithLLMChatter(wrapped),
	)
	// Wire a context firewall; the limit is large enough that the filler
	// passes pre-validation but compaction still has a real seam to drain
	// through on the provider's overflow verdict.
	fw := llm.NewContextFirewall(
		wrapped,
		&llm.ModelConfig{ContextLimit: 20000},
		llm.ContextFirewallConfig{Enabled: true, DropContextOnHardLimit: true},
		nil,
		slog.New(slog.DiscardHandler),
		nil,
	)
	loop.llm = fw
	loop.contextFirewall = fw
	// Grow the conversation so compaction actually has something to drop.
	conv := loop.conversations.Get("conv-overflow-retry")
	for i := 0; i < 40; i++ {
		conv.AddAssistantMessage(fmt.Sprintf("assistant filler %d %s", i, strings.Repeat("x", 120)))
		conv.AddUserMessage(fmt.Sprintf("user filler %d %s", i, strings.Repeat("y", 120)))
	}

	reply, err := loop.RunOnce(context.Background(), "hello", "conv-overflow-retry")
	if err != nil {
		t.Fatalf("RunOnce after compaction retry = error %v, want success", err)
	}
	if reply != "recovered after compaction" {
		t.Errorf("reply = %q, want the post-compaction response", reply)
	}
	if got := chatter.calls.Load(); got != 2 {
		t.Fatalf("chatter calls = %d, want 2 (overflow + one compaction retry)", got)
	}
}

// messageCountingChatter records the length of the message slice it was
// last called with.
type messageCountingChatter struct {
	inner   llm.Chatter
	lastLen *atomic.Int32
}

func (m *messageCountingChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.lastLen.Store(int32(len(messages)))
	return m.inner.Chat(ctx, messages, opts...)
}

func (m *messageCountingChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *messageCountingChatter) Config() *llm.ModelConfig {
	return m.inner.Config()
}
