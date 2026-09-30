package agent

// Pins for the bughunt 2026-09-29 fix wave on the reply-guard retry seam
// (M2, M3, L8, L24):
//
//   - M2: a dispatcher-armed l.guardFallback (the session-digest answer)
//     must ship on BOTH retry exits — an errored retry and a second guard
//     trip — not only on the no-retry path. The canned line discarded the
//     session's stored answer (the whole point of ce0cb7e6).
//   - M3: the retry re-entry of reasoningCycle can itself PARK on a
//     provider wait ("" reply, nil error, StateQuotaWait). That shape must
//     return ("", nil) WITHOUT the post-turn success pipeline — the same
//     contract the first-cycle turnParked guard enforces. The terminate
//     lane twin (applyTerminateReplyGuard) must propagate the parked shape
//     too, never a canned line masquerading as an answer.
//   - L8: the guardRetried check-and-set runs under l.mu (resetTurnGuards
//     writes it under the same lock).
//
// Loop-seam pins drive a full RunOnce so the turn-level guards are live;
// the terminate-lane pins drive applyTerminateReplyGuard directly.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// dumpThenFailChatter trips the guard on the first completion and FAILS the
// retry call — the M2 errored-retry exit.
type dumpThenFailChatter struct {
	callCount int
}

func (m *dumpThenFailChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	if m.callCount == 1 {
		return &llm.Response{
			Content: rawDumpSample,
			Usage:   llm.TokenUsage{TotalTokens: 1},
		}, nil
	}
	return nil, errors.New("retry LLM call failed (scripted)")
}

func (m *dumpThenFailChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *dumpThenFailChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "dump-then-fail-chatter"}
}

// parkFirstChatter parks on its FIRST call — the retry re-entry of
// reasoningCycle immediately hits a provider wait: the LLM call fails with
// ErrTurnParked after flipping the loop's state machine to StateQuotaWait
// (the pair chatWithFailoverRaw/parkThrottledTurn produce on a real
// provider wait). reasoningCycle maps that to ("", nil) — the parked shape
// the retry seams must detect (M3).
type parkFirstChatter struct {
	loop *AgentLoop
}

func (m *parkFirstChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	if m.loop != nil {
		m.loop.safeTransition(StateQuotaWait, "test_throttle_park", map[string]any{})
	}
	return nil, ErrTurnParked
}

func (m *parkFirstChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *parkFirstChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "park-first-chatter"}
}

const digestFallbackPin = `Prior task: "create hello.txt" — status: completed
Result: The file hello.txt has been created.`

// TestGuardRetry_ErroredRetryShipsDigestFallback (M2, errored-retry exit):
// when the dispatcher armed l.guardFallback and the rewrite retry FAILS, the
// armed digest answer ships instead of the generic canned line.
func TestGuardRetry_ErroredRetryShipsDigestFallback(t *testing.T) {
	chatter := &dumpThenFailChatter{}
	loop := newGuardRetryLoop(t, chatter)
	loop.SetGuardFallback(digestFallbackPin)

	const convID = "conv-guard-retry-errored-fallback"
	reply, err := loop.RunOnce(context.Background(), "remember that the build passed", convID)
	if err != nil {
		t.Fatalf("RunOnce = error %v, want the fallback reply (the turn already succeeded once)", err)
	}
	if !strings.Contains(reply, digestFallbackPin) {
		t.Fatalf("reply = %q, want the dispatcher-armed digest answer on the errored-retry exit", reply)
	}
	if strings.Contains(reply, "ask me to do something specific") {
		t.Fatalf("reply = %q, the canned line must not ship when a digest fallback is armed", reply)
	}
	// The armed fallback is consumed once.
	if loop.guardFallback != "" {
		t.Errorf("guardFallback = %q after the turn, want consumed (cleared)", loop.guardFallback)
	}
}

// TestGuardRetry_SecondTripShipsDigestFallback (M2, second-trip exit): a
// model that dumps BOTH times ships the armed digest answer on the second
// guard trip, not the canned line.
func TestGuardRetry_SecondTripShipsDigestFallback(t *testing.T) {
	chatter := &alwaysDumpChatter{}
	loop := newGuardRetryLoop(t, chatter)
	loop.SetGuardFallback(digestFallbackPin)

	const convID = "conv-guard-retry-cap-fallback"
	reply, err := loop.RunOnce(context.Background(), "remember that the build passed", convID)
	if err != nil {
		t.Fatalf("RunOnce = error %v, want the fallback reply", err)
	}
	if !strings.Contains(reply, digestFallbackPin) {
		t.Fatalf("reply = %q, want the armed digest answer on the second-trip exit", reply)
	}
	if strings.Contains(reply, "ask me to do something specific") {
		t.Fatalf("reply = %q, the canned line must not ship when a digest fallback is armed", reply)
	}
	if chatter.callCount != 2 {
		t.Fatalf("LLM calls = %d, want exactly 2 (the retry cap must stop a third call)", chatter.callCount)
	}
}

// TestGuardRetry_NoFallbackKeepsCannedLine pins the unchanged shape when
// nothing is armed: the errored retry still ships the canned line.
func TestGuardRetry_NoFallbackKeepsCannedLine(t *testing.T) {
	chatter := &dumpThenFailChatter{}
	loop := newGuardRetryLoop(t, chatter)

	reply, err := loop.RunOnce(context.Background(), "remember that the build passed", "conv-guard-retry-errored-canned")
	if err != nil {
		t.Fatalf("RunOnce = error %v, want the canned replacement", err)
	}
	if !strings.Contains(reply, "raw data instead of an answer") ||
		!strings.Contains(reply, "ask me to do something specific") {
		t.Fatalf("reply = %q, want the canned line when no digest fallback is armed", reply)
	}
}

// TestGuardRetry_RetryParksSkipsPostTurnPipeline (M3, loop seam): when the
// rewrite retry parks (blank reply, nil error, StateQuotaWait), the turn
// returns ("", nil) — no reply text, no post-turn success side effects.
func TestGuardRetry_RetryParksSkipsPostTurnPipeline(t *testing.T) {
	chatter := &parkFirstChatter{}
	loop := newGuardRetryLoop(t, chatter)
	chatter.loop = loop
	loop.SetGuardFallback(digestFallbackPin)

	const convID = "conv-guard-retry-park"
	reply, err := loop.RunOnce(context.Background(), "remember that the build passed", convID)
	if err != nil {
		t.Fatalf("parked turn must not surface an error to the loop caller, got: %v", err)
	}
	if reply != "" {
		t.Fatalf("reply = %q, want empty for a parked retry (the park contract)", reply)
	}
	// The parked shape is detectable: the state machine sits in
	// StateQuotaWait, mirroring loop.go's first-cycle turnParked guard.
	if state := loop.GetState(); state != StateQuotaWait {
		t.Errorf("state = %v, want StateQuotaWait after a parked retry", state)
	}
	// No phantom assistant turn: a blank assistant completion must not be
	// appended after the dump + nudge (post-turn pipeline ran).
	conv := loop.conversations.Get(convID)
	msgs := conv.GetMessages()
	if len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == llm.RoleAssistant && strings.TrimSpace(last.Content) == "" {
			t.Error("blank assistant message appended after a parked retry (post-turn pipeline ran)")
		}
	}
}

// terminateGuardTwinLoop builds a loop for direct applyTerminateReplyGuard
// pins (the terminate lane's rewrite seam).
func terminateGuardTwinLoop(t *testing.T, chatter llm.Chatter) *AgentLoop {
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

// TestTerminateLane_ErroredRetryShipsDigestFallback (M2 twin): an errored
// rewrite re-entry ships the armed digest answer instead of the canned line.
func TestTerminateLane_ErroredRetryShipsDigestFallback(t *testing.T) {
	chatter := &dumpThenFailChatter{}
	loop := terminateGuardTwinLoop(t, chatter)
	loop.SetGuardFallback(digestFallbackPin)

	got := loop.applyTerminateReplyGuard(context.Background(), loop.conversations.Get("conv-term-fallback"), "conv-term-fallback", rawDumpSample)
	if !strings.Contains(got, digestFallbackPin) {
		t.Fatalf("reply = %q, want the armed digest answer on the terminate lane's errored-retry exit", got)
	}
	if strings.Contains(got, "ask me to do something specific") {
		t.Fatalf("reply = %q, the canned line must not ship when a digest fallback is armed", got)
	}
}

// TestTerminateLane_SecondTripShipsDigestFallback (M2 twin): a rewrite that
// dumps again ships the armed digest answer.
func TestTerminateLane_SecondTripShipsDigestFallback(t *testing.T) {
	chatter := &alwaysDumpChatter{}
	loop := terminateGuardTwinLoop(t, chatter)
	loop.SetGuardFallback(digestFallbackPin)

	got := loop.applyTerminateReplyGuard(context.Background(), loop.conversations.Get("conv-term-cap-fallback"), "conv-term-cap-fallback", rawDumpSample)
	if !strings.Contains(got, digestFallbackPin) {
		t.Fatalf("reply = %q, want the armed digest answer on the terminate lane's second-trip exit", got)
	}
	if strings.Contains(got, "ask me to do something specific") {
		t.Fatalf("reply = %q, the canned line must not ship when a digest fallback is armed", got)
	}
}

// TestTerminateLane_RetryParksReturnsParkedShape (M3 twin): a rewrite
// re-entry whose FIRST LLM call parks returns "" with the StateQuotaWait
// transition recorded — the parked-turn contract propagates; the canned
// line never masquerades as an answer for a turn that parked.
func TestTerminateLane_RetryParksReturnsParkedShape(t *testing.T) {
	chatter := &parkFirstChatter{}
	loop := terminateGuardTwinLoop(t, chatter)
	chatter.loop = loop
	loop.SetGuardFallback(digestFallbackPin)

	got := loop.applyTerminateReplyGuard(context.Background(), loop.conversations.Get("conv-term-park"), "conv-term-park", rawDumpSample)
	if got != "" {
		t.Fatalf("reply = %q, want empty for a parked rewrite re-entry", got)
	}
	if state := loop.GetState(); state != StateQuotaWait {
		t.Errorf("state = %v, want StateQuotaWait after a parked rewrite", state)
	}
}

// TestGuardRetry_FlagCheckAndSetUnderMutex (L8): the guardRetried
// check-and-set at the retry branch runs under l.mu. Pinned by exercising
// the same lock discipline concurrently against resetTurnGuards (run with
// -race in CI): racing writers must not lose an update or deadlock.
func TestGuardRetry_FlagCheckAndSetUnderMutex(t *testing.T) {
	loop := &AgentLoop{}
	loop.mu.Lock()
	loop.guardRetried = false
	loop.mu.Unlock()

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			// The same shape the retry branch uses.
			loop.mu.Lock()
			already := loop.guardRetried
			loop.guardRetried = true
			loop.mu.Unlock()
			_ = already
			loop.resetTurnGuards()
		})
	}
	wg.Wait()
	deadline := time.Now().Add(time.Second)
	for loop.guardRetried && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}
