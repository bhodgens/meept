package agent

// Throttle parking tests (tree 03 leaf 02, DECISIONS.md D4/D8): the agent
// loop parks a turn on ThrottleBackoffError via the TurnParker, surfaces the
// D8 ThrottleGiveUpError when the wait exceeds MaxWait, and grows the resume
// schedule with the attempt count (BackoffPlan composition — no recompute).
//
// Scheduling is verified against an injected clock (fakeNowFunc from
// parked_turn_test.go); no test sleeps on the real clock for scheduling
// decisions. Real-clock polling in the resume test is bounded ≤2s.

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// throttleFailurePolicyForTests installs tiny failure-policy defaults so the
// BackoffPlan schedule is observable in milliseconds, and restores the
// previous override on cleanup.
func throttleFailurePolicyForTests(t *testing.T, base time.Duration) {
	t.Helper()
	SetFailurePolicyDefaults(llm.FailurePolicyConfig{
		Horizon:      time.Hour,
		BaseThrottle: base,
		PollFloor:    time.Minute,
	})
	t.Cleanup(clearFailurePolicyDefaults)
}

// throttleScriptChatter fails with errs[0..n] in order, then returns resp.
type throttleScriptChatter struct {
	mu    sync.Mutex
	errs  []error
	resp  *llm.Response
	calls int
}

func (m *throttleScriptChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	call := m.calls
	m.calls++
	if call < len(m.errs) {
		return nil, m.errs[call]
	}
	return m.resp, nil
}

func (m *throttleScriptChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *throttleScriptChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "throttle-script"}
}

func (m *throttleScriptChatter) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// newThrottleParkLoop builds a loop wired to a real TurnParker sharing the
// injected clock. The parker is NOT started: park-side tests never drain.
func newThrottleParkLoop(t *testing.T, chatter llm.Chatter, maxWait time.Duration) (*AgentLoop, *TurnParker, *fakeNowFunc) {
	t.Helper()
	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		MaxAttempts: 2,
	})
	t.Cleanup(clearDefaultBackoffOverride)
	loop := NewAgentLoop("sess-throttle-park", t.TempDir(),
		WithMessageBus(bus.New(nil, testLogger())),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	clock := &fakeNowFunc{now: time.Now()}
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, maxWait)
	parker.nowFunc = clock.nowFn
	loop.SetClock(clock.nowFn)
	loop.SetTurnParker(parker)
	return loop, parker, clock
}

// parkedThrottleRecord returns the single parked record (test-only, in-package
// access mirroring quota_resume.go's own field reads).
func parkedThrottleRecord(p *TurnParker) ParkedTurnRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.parked) == 0 {
		return ParkedTurnRecord{}
	}
	return p.parked[0]
}

func waitGoroutinesSettle(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Errorf("goroutine leak after parked turn: before=%d after=%d", before, after)
	}
}

// TestAgentLoop_ThrottleBackoffParksTurn (leaf Task 2a): a
// ThrottleBackoffError whose wait fits MaxWait parks the turn — one
// ParkedTurnRecord (Class=throttle), no error surfaced to the loop's caller,
// StateQuotaWait transition with reason "throttle_wait", and no agent
// goroutine held while waiting.
func TestAgentLoop_ThrottleBackoffParksTurn(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	retryAt := time.Now().Add(50 * time.Millisecond)
	chatter := &throttleScriptChatter{
		errs: []error{&llm.ThrottleBackoffError{
			ProviderID: "p1",
			ModelID:    "m1",
			RetryAt:    retryAt,
			Attempt:    0,
			Cause:      &llm.APIError{StatusCode: http.StatusTooManyRequests, Detail: "slow down"},
		}},
		resp: &llm.Response{Content: "recovered", FinishReason: "stop"},
	}
	loop, parker, _ := newThrottleParkLoop(t, chatter, time.Hour)

	before := runtime.NumGoroutine()
	reply, err := loop.RunOnce(context.Background(), "hello", "conv-throttle-park")
	if err != nil {
		t.Fatalf("parked turn must not surface an error to the loop caller, got: %v", err)
	}
	if reply != "" {
		t.Errorf("reply = %q, want empty (turn parked, not answered)", reply)
	}

	if got := parker.Pending(); got != 1 {
		t.Fatalf("parker.Pending() = %d, want 1", got)
	}
	rec := parkedThrottleRecord(parker)
	if rec.Class != llm.FailureThrottle {
		t.Errorf("record class = %v, want FailureThrottle", rec.Class)
	}
	if rec.ResumeAt.IsZero() || rec.ResumeAt.Before(retryAt.Add(-time.Second)) {
		t.Errorf("ResumeAt = %v, want around the RetryAt/base step (>= %v)", rec.ResumeAt, retryAt)
	}
	if rec.Attempt != 0 {
		t.Errorf("record Attempt = %d, want 0", rec.Attempt)
	}
	// TurnPayload carries the frozen-key chat dispatch (Task 3 wires the
	// resume router; keys asserted in TestThrottleTurnPayload_FrozenKeys in
	// parked_turn_test.go's payload round-trip style).
	if rec.TurnPayload == nil {
		t.Error("TurnPayload = nil, want the frozen chat-dispatch encoding")
	}

	// State: parked, with the throttle_wait reason on the transition.
	if state := loop.GetState(); state != StateQuotaWait {
		t.Errorf("state = %v, want StateQuotaWait after parking", state)
	}
	history := loop.GetStateHistory()
	if len(history) == 0 || history[len(history)-1].Reason != "throttle_wait" {
		if len(history) == 0 {
			t.Fatal("no state transitions recorded")
		}
		t.Errorf("last transition reason = %q, want \"throttle_wait\"", history[len(history)-1].Reason)
	}

	// No model rotation: exactly one LLM call for the parked turn.
	if got := chatter.callCount(); got != 1 {
		t.Errorf("chatter calls = %d, want 1 (park, not retry)", got)
	}

	// The turn holds no goroutine while parked.
	waitGoroutinesSettle(t, before)
}

// TestAgentLoop_ThrottleGiveUpBeyondMaxWait (leaf Task 2b): a wait beyond
// MaxWait refuses to park and surfaces the D8 ThrottleGiveUpError; nothing
// is parked.
func TestAgentLoop_ThrottleGiveUpBeyondMaxWait(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	chatter := &throttleScriptChatter{
		errs: []error{&llm.ThrottleBackoffError{
			ProviderID: "p1",
			ModelID:    "m1",
			RetryAt:    time.Now().Add(time.Hour),
			Attempt:    0,
		}},
	}
	loop, parker, _ := newThrottleParkLoop(t, chatter, 10*time.Millisecond)

	_, err := loop.RunOnce(context.Background(), "hello", "conv-throttle-giveup")
	if err == nil {
		t.Fatal("expected the D8 give-up error to surface")
	}
	var giveUp *llm.ThrottleGiveUpError
	if !errors.As(err, &giveUp) {
		t.Fatalf("got %T (%v), want *llm.ThrottleGiveUpError", err, err)
	}
	if giveUp.ProviderID != "p1" || giveUp.ModelID != "m1" {
		t.Errorf("give-up error identity = %s/%s, want p1/m1", giveUp.ProviderID, giveUp.ModelID)
	}
	if giveUp.Waited <= 0 {
		t.Errorf("give-up Waited = %v, want the excess wait", giveUp.Waited)
	}
	if msg := giveUp.UserMessage(); !strings.Contains(msg, "turn abandoned") {
		t.Errorf("UserMessage() = %q, want the D8 wording", msg)
	}
	if parker.Pending() != 0 {
		t.Errorf("parker.Pending() = %d, want 0 (nothing parked on give-up)", parker.Pending())
	}
	// Give-up consumes the turn without a retry: one LLM call.
	if got := chatter.callCount(); got != 1 {
		t.Errorf("chatter calls = %d, want 1", got)
	}
}

// TestAgentLoop_ThrottleNoParkerPassthrough is the control: a loop without a
// wired parker keeps the tree-02 pass-through contract (error unchanged).
func TestAgentLoop_ThrottleNoParkerPassthrough(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	wantErr := &llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1", RetryAt: time.Now().Add(time.Hour), Attempt: 1}
	chatter := &throttleScriptChatter{errs: []error{wantErr}}
	SetDefaultBackoffOverride(BackoffConfig{BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, MaxAttempts: 2})
	t.Cleanup(clearDefaultBackoffOverride)
	loop := NewAgentLoop("sess-no-parker", t.TempDir(),
		WithMessageBus(bus.New(nil, testLogger())),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})

	_, err := loop.RunOnce(context.Background(), "hello", "conv-no-parker")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want the original ThrottleBackoffError (pass-through)", err)
	}
}

// TestAgentLoop_ThrottleParkScheduleGrowsWithAttempts (leaf Task 2c): the
// parked ResumeAt comes from plan.NextAttempt — exponential growth with the
// attempt count, server RetryAt honored when later.
func TestAgentLoop_ThrottleParkScheduleGrowsWithAttempts(t *testing.T) {
	base := 20 * time.Millisecond
	throttleFailurePolicyForTests(t, base)

	cases := []struct {
		name    string
		attempt int
		retryIn time.Duration // server RetryAt offset from clock start; 0 = none
		want    time.Duration
	}{
		{"attempt0_base", 0, 0, base},
		{"attempt1_double", 1, 0, 2 * base},
		{"attempt2_quadruple", 2, 0, 4 * base},
		{"attempt5_growth", 5, 0, 640 * time.Millisecond}, // 20ms*2^5 — below the PollFloor (floor at attempt ≥ 12)
		{"attempt12_floored", 12, 0, time.Minute},         // 20ms*2^12 ≥ 1m — pinned at PollFloor
		{"server_retryat_wins", 1, 300 * time.Millisecond, 300 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			clock := &fakeNowFunc{now: start}
			loop := NewAgentLoop("sess-sched", t.TempDir(),
				WithMessageBus(bus.New(nil, testLogger())),
				WithLLMChatter(&throttleScriptChatter{}),
			)
			loop.SetClock(clock.nowFn)
			parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
			parker.nowFunc = clock.nowFn
			loop.SetTurnParker(parker)

			var retryAt time.Time
			if tc.retryIn > 0 {
				retryAt = start.Add(tc.retryIn)
			}
			terr := &llm.ThrottleBackoffError{
				ProviderID: "p1",
				ModelID:    "m1",
				RetryAt:    retryAt,
				Attempt:    tc.attempt,
			}
			parked, giveUp := loop.parkThrottledTurn(context.Background(), terr)
			if giveUp != nil {
				t.Fatalf("parkThrottledTurn gave up: %v", giveUp)
			}
			if !parked {
				t.Fatal("parkThrottledTurn did not park")
			}
			rec := parkedThrottleRecord(parker)
			wantAt := start.Add(tc.want)
			if !rec.ResumeAt.Equal(wantAt) {
				t.Errorf("ResumeAt = %v, want %v (attempt %d, base %v)", rec.ResumeAt, wantAt, tc.attempt, base)
			}
		})
	}
}

// TestAgentLoop_ThrottleNoRotationOnParkedPath proves the parked turn never
// rotates or records alias failure (D4): alias health untouched, one call.
func TestAgentLoop_ThrottleNoRotationOnParkedPath(t *testing.T) {
	throttleFailurePolicyForTests(t, 20*time.Millisecond)
	chatter := &throttleScriptChatter{
		errs: []error{&llm.ThrottleBackoffError{ProviderID: "p1", ModelID: "m1", RetryAt: time.Now().Add(50 * time.Millisecond), Attempt: 0}},
	}
	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: "http://p1.invalid", ModelID: "m1", APIKey: "***", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: "http://p2.invalid", ModelID: "m2", APIKey: "***", ProviderID: "p2"},
	)
	SetDefaultBackoffOverride(BackoffConfig{BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, MaxAttempts: 2})
	t.Cleanup(clearDefaultBackoffOverride)
	loop := NewAgentLoop("sess-no-rotate", t.TempDir(),
		WithMessageBus(bus.New(nil, testLogger())),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	loop.security = security.NewPermissionChecker(security.Config{})
	clock := &fakeNowFunc{now: time.Now()}
	parker := NewTurnParker(testLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	parker.nowFunc = clock.nowFn
	loop.SetClock(clock.nowFn)
	loop.SetTurnParker(parker)

	if _, err := loop.RunOnce(context.Background(), "hello", "conv-no-rotate"); err != nil {
		t.Fatalf("parked turn surfaced an error: %v", err)
	}
	if _, fails, _, ok := resolver.GetAliasHealth(testClassifierAlias); ok && fails != 0 {
		t.Errorf("alias ConsecutiveFails = %d, want 0 (no RecordAliasFailure on throttle)", fails)
	}
	if got := chatter.callCount(); got != 1 {
		t.Errorf("chatter calls = %d, want 1 (no rotation)", got)
	}
}
