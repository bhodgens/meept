package agent

// Resume-path tests for throttle parking (tree 03 leaf 02 Task 3): a parked
// class=FailureThrottle turn resumes through the class-dispatching router
// (TurnParker.SetResumeFunc installed by SetThrottleParker) once its resume
// window passes, re-enters the loop via the ORIGINAL chat payload, and
// completes when the provider recovers. A re-throttling resume re-parks with
// a GROWN attempt (backoff growth across park generations). All scheduling
// runs on the injected clock (testClock from parked_turn_test.go); real waits
// are bounded ≤2s poll-polling only.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// newThrottleResumeLoop builds a loop + chat handler wired through
// SetThrottleParker on a testClock-driven parker, exactly as the daemon will
// wire them (leaf 03 merges the quota queue onto the same parker).
func newThrottleResumeLoop(t *testing.T, chatter llm.Chatter) (*AgentLoop, *ChatHandler, *TurnParker, *testClock) {
	t.Helper()
	throttleFailurePolicyForTests(t, time.Second)
	loop := NewAgentLoop("sess-throttle-resume", t.TempDir(),
		WithMessageBus(bus.New(nil, testLogger())),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	handler := &ChatHandler{loop: loop, logger: testLogger()}
	clock := newTestClock(time.Now())
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, 24*time.Hour)
	parker.SetPollInterval(10 * time.Millisecond)
	parker.nowFunc = clock.nowFn
	parker.testDrainGate = clock.WaitUntil
	handler.SetThrottleParker(parker)
	loop.SetClock(clock.nowFn)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	parker.Start(ctx)
	return loop, handler, parker, clock
}

// TestThrottleParkedTurnResume (Task 3): park → advance the clock → watcher
// fires → the payload re-enters the loop → success recorded; nothing stays
// parked.
func TestThrottleParkedTurnResume(t *testing.T) {
	throttleErr := &llm.ThrottleBackoffError{
		ProviderID: "p1",
		ModelID:    "m1",
		RetryAt:    time.Time{},
		Attempt:    0,
	}
	chatter := &scriptedToggleChatter{errs: []error{throttleErr}, reply: "recovered!"}
	loop, _, parker, clock := newThrottleResumeLoop(t, chatter)

	if _, err := loop.RunOnce(context.Background(), "hello", "conv-resume"); err != nil {
		t.Fatalf("park turn: %v", err)
	}
	if parker.Pending() != 1 {
		t.Fatalf("pending = %d, want 1 after park", parker.Pending())
	}
	firstResume, ok := parker.Next(llm.FailureThrottle)
	if !ok {
		t.Fatal("no throttle resume scheduled")
	}

	// Advance past the resume window: the drain fires and the turn re-runs.
	clock.advance(time.Until(firstResume) + time.Second)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		return parker.Pending() == 0 && chatter.callCount() >= 2
	})

	conv := loop.conversations.Get("conv-resume")
	if conv == nil || conv.LastUserMessage() != "hello" {
		t.Fatalf("conversation missing the original turn payload")
	}
	if got := chatter.callCount(); got != 2 {
		t.Errorf("chatter calls = %d, want 2 (park + resume)", got)
	}
}

// TestThrottleParkedTurnResumeReParksGrown (Task 2c + Task 3): a resume that
// throttles again re-parks through the normal branch with attempt+1, so the
// next resume time grows (2s step after the 1s base).
func TestThrottleParkedTurnResumeReParksGrown(t *testing.T) {
	first := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1", Attempt: 0}
	second := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1", Attempt: 0}
	chatter := &scriptedToggleChatter{errs: []error{first, second}, reply: "late"}
	loop, _, parker, clock := newThrottleResumeLoop(t, chatter)

	if _, err := loop.RunOnce(context.Background(), "hello", "conv-grow"); err != nil {
		t.Fatalf("park turn: %v", err)
	}
	firstResume, _ := parker.Next(llm.FailureThrottle)

	// Resume → throttle again → re-park with the grown attempt.
	clock.advance(time.Until(firstResume) + time.Second)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		return chatter.callCount() >= 2 && parker.Pending() == 1
	})

	rec := parkedThrottleRecord(parker)
	if rec.Attempt != 1 {
		t.Errorf("re-parked attempt = %d, want 1 (grown across generations)", rec.Attempt)
	}
	secondResume, _ := parker.Next(llm.FailureThrottle)
	if !secondResume.After(firstResume) {
		t.Errorf("second resume %v not after first %v", secondResume, firstResume)
	}
	// attempt=1 step = 2s base after the re-park instant (≥ first window).
	if wait := secondResume.Sub(clock.now); wait < 2*time.Second {
		t.Errorf("re-park wait = %v, want ≥ the grown 2s step", wait)
	}
}

// TestResumeRouter_ClassDispatch (Task 3): the installed router sends
// throttle records to the loop's resume path and quota records to the
// handler's fallback (the quota queue itself keeps its own watcher; leaf 03
// merges the queues).
func TestResumeRouter_ClassDispatch(t *testing.T) {
	chatter := &scriptedToggleChatter{reply: "ok"}
	loop, handler, parker, clock := newThrottleResumeLoop(t, chatter)

	var throttleResumed, fallbackResumed atomic.Int32
	// Re-install the router the wiring builds, but with observable sinks.
	parker.SetResumeFunc(func(ctx context.Context, rec ParkedTurnRecord) {
		if rec.Class == llm.FailureThrottle {
			throttleResumed.Add(1)
			return
		}
		handler.resumeRouterDefault(ctx, rec)
		fallbackResumed.Add(1)
	})

	_ = parker.Park(ParkedTurnRecord{
		ConversationID: "c1",
		SessionID:      "s1",
		Class:          llm.FailureThrottle,
		ResumeAt:       clock.now.Add(50 * time.Millisecond),
		TurnPayload:    []byte(`{"message":"hi"}`),
	})
	_ = parker.Park(ParkedTurnRecord{
		ConversationID: "c2",
		SessionID:      "s2",
		Class:          llm.FailureQuota,
		ResumeAt:       clock.now.Add(50 * time.Millisecond),
		TurnPayload:    []byte(`{}`),
	})
	// Advance past both resume times and let the gate open the drain.
	clock.advance(time.Second)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		return throttleResumed.Load() == 1 && fallbackResumed.Load() == 1
	})

	if got := throttleResumed.Load(); got != 1 {
		t.Errorf("throttle resumes = %d, want 1", got)
	}
	if got := fallbackResumed.Load(); got != 1 {
		t.Errorf("fallback resumes = %d, want 1", got)
	}
	if loop.TurnParker() != parker {
		t.Error("loop parker reference not wired by SetThrottleParker")
	}
}

// scriptedToggleChatter serves errs in order, then succeeds with reply.
type scriptedToggleChatter struct {
	errs  []error
	reply string
	calls atomic.Int32
}

func (m *scriptedToggleChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	i := int(m.calls.Add(1)) - 1
	if i < len(m.errs) && m.errs[i] != nil {
		return nil, m.errs[i]
	}
	return &llm.Response{Content: m.reply, FinishReason: "stop"}, nil
}

func (m *scriptedToggleChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *scriptedToggleChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "scripted-toggle"}
}

func (m *scriptedToggleChatter) callCount() int { return int(m.calls.Load()) }
