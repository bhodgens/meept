package agent

// Tests for the class-agnostic TurnParker (tree 03 leaf 01, master Contract
// 1 / SHARED-CONVENTIONS §4.5). All scheduling behavior is verified against
// an injected clock: no test waits on the real wall clock for scheduling
// decisions (polling waits are bounded ≤100ms per the leaf checklist).
//
// The testClock's WaitUntil gates drainDue in lockstep with the injected
// now: the watcher poller blocks on it until the test advances the clock,
// so resumed counts are deterministic without real sleeps.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// fakeNowFunc returns a now func over a mutable time.Time.
type fakeNowFunc struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeNowFunc) nowFn() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeNowFunc) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// testClock couples a fake now to a condition the watcher's polling loop
// waits on, so tests drive drainDue deterministically by advancing the
// clock and signaling. The poller's ticker fires on the real clock (10ms
// cadence) but every scheduling decision inside drainDue reads the fake.
type testClock struct {
	*fakeNowFunc
	ch   chan struct{}
	mu   sync.Mutex
	wait func() bool // nil = always proceed
}

func newTestClock(start time.Time) *testClock {
	return &testClock{
		fakeNowFunc: &fakeNowFunc{now: start},
		ch:          make(chan struct{}, 64),
	}
}

// WaitUntil is the drain-gate installed via TurnParker's testDrainGate
// hook (package-private; quota_resume_test.go and this file are in the
// same package). It blocks until the test signals proceed or the 100ms
// poll timeout elapses.
func (c *testClock) WaitUntil() bool {
	c.mu.Lock()
	wait := c.wait
	c.mu.Unlock()
	if wait != nil && !wait() {
		return false
	}
	select {
	case <-c.ch:
		return true
	case <-time.After(100 * time.Millisecond):
		return true // time out open: never wedge the poller
	}
}

func (c *testClock) proceed() { c.ch <- struct{}{} }

func parkedTurnTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardTestWriter{}, nil))
}

func newTestTurnParker(t *testing.T, maxWait time.Duration, resume func(context.Context, ParkedTurnRecord)) (*TurnParker, *testClock) {
	t.Helper()
	p := NewTurnParker(parkedTurnTestLogger(), resume, maxWait)
	p.SetPollInterval(10 * time.Millisecond)
	clock := newTestClock(time.Now())
	p.nowFunc = clock.nowFn
	p.testDrainGate = clock.WaitUntil
	return p, clock
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

// --- ParkedTurnRecord payload round-trip -----------------------------------

func TestParkedTurnRecord_TurnPayloadRoundTrip(t *testing.T) {
	type payload struct {
		Message        string            `json:"message"`
		Parts          []llm.ContentPart `json:"parts,omitempty"`
		ConversationID string            `json:"conversation_id"`
		ProviderID     string            `json:"provider_id"`
		CredentialKey  string            `json:"credential_key"`
	}
	want := payload{
		Message:        "hello",
		Parts:          []llm.ContentPart{{Type: "text", Text: "hi"}},
		ConversationID: "c1",
		ProviderID:     "p1",
		CredentialKey:  "cred",
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	rec := ParkedTurnRecord{
		ConversationID: "c1",
		SessionID:      "s1",
		AgentID:        "a1",
		Class:          llm.FailureQuota,
		ResumeAt:       time.Now().Add(time.Minute),
		Attempt:        1,
		MaxAttempts:    3,
		TurnPayload:    raw,
	}
	var got payload
	if err := json.Unmarshal(rec.TurnPayload, &got); err != nil {
		t.Fatalf("unmarshal TurnPayload: %v", err)
	}
	if got.Message != want.Message || got.ConversationID != want.ConversationID ||
		got.ProviderID != want.ProviderID || got.CredentialKey != want.CredentialKey ||
		len(got.Parts) != 1 || got.Parts[0].Text != want.Parts[0].Text {
		t.Fatalf("payload round-trip mismatch: got %+v want %+v", got, want)
	}
	if rec.Class != llm.FailureQuota || rec.Attempt != 1 || rec.MaxAttempts != 3 || rec.AgentID != "a1" {
		t.Fatalf("record fields not preserved: %+v", rec)
	}
}

// --- Task 1: park/resume scheduling ----------------------------------------

func TestTurnParker_ParkAndResumeAtResumeAt(t *testing.T) {
	var mu sync.Mutex
	var resumed []ParkedTurnRecord
	p, clock := newTestTurnParker(t, 24*time.Hour, func(ctx context.Context, turn ParkedTurnRecord) {
		mu.Lock()
		resumed = append(resumed, turn)
		mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	resumeAt := clock.nowFn().Add(20 * time.Millisecond)
	if !p.Park(ParkedTurnRecord{
		SessionID:   "s1",
		Class:       llm.FailureQuota,
		ResumeAt:    resumeAt,
		MaxAttempts: 3,
	}) {
		t.Fatal("expected park to succeed")
	}

	clock.advance(25 * time.Millisecond)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(resumed) == 1
	})
	mu.Lock()
	got := resumed[0]
	mu.Unlock()
	if got.SessionID != "s1" || got.Class != llm.FailureQuota {
		t.Fatalf("resumed record mismatch: %+v", got)
	}
}

func TestTurnParker_MaxWaitSoftStopSchedulesAtNowPlusMaxWait(t *testing.T) {
	var mu sync.Mutex
	var resumed []ParkedTurnRecord
	p, clock := newTestTurnParker(t, time.Hour, func(ctx context.Context, turn ParkedTurnRecord) {
		mu.Lock()
		resumed = append(resumed, turn)
		mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	// ResumeAt beyond maxWait: still parked, scheduled at now+maxWait.
	start := clock.nowFn()
	if !p.Park(ParkedTurnRecord{
		SessionID: "s1",
		Class:     llm.FailureQuota,
		ResumeAt:  start.Add(2 * time.Hour),
	}) {
		t.Fatal("park should schedule over-maxWait turns at now+maxWait")
	}
	clock.advance(time.Hour + 10*time.Millisecond)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(resumed) == 1
	})
}

func TestTurnParker_ParkFalseWhenImpossible(t *testing.T) {
	resumed := 0
	p, _ := newTestTurnParker(t, time.Hour, func(ctx context.Context, turn ParkedTurnRecord) {
		resumed++
	})

	// Zero ResumeAt: cannot schedule (mirrors quota's zero-unblock refusal).
	if p.Park(ParkedTurnRecord{SessionID: "s1", Class: llm.FailureQuota}) {
		t.Fatal("park should refuse zero ResumeAt")
	}
	// ResumeAt in the past: already due; caller should retry directly.
	if p.Park(ParkedTurnRecord{SessionID: "s2", Class: llm.FailureQuota, ResumeAt: time.Now().Add(-time.Second)}) {
		t.Fatal("park should refuse past ResumeAt")
	}
	// Without a resume func the parker is disabled entirely.
	disabled := NewTurnParker(parkedTurnTestLogger(), nil, time.Hour)
	if disabled.Park(ParkedTurnRecord{SessionID: "s3", Class: llm.FailureQuota, ResumeAt: time.Now().Add(time.Hour)}) {
		t.Fatal("park should be a no-op without resume func")
	}
	if resumed != 0 {
		t.Fatalf("unexpected resumes: %d", resumed)
	}
}

func TestTurnParker_PendingCounts(t *testing.T) {
	p, _ := newTestTurnParker(t, 24*time.Hour, func(ctx context.Context, turn ParkedTurnRecord) {})
	now := time.Now()
	if !p.Park(ParkedTurnRecord{SessionID: "s1", Class: llm.FailureQuota, ResumeAt: now.Add(time.Minute)}) {
		t.Fatal("park s1 refused")
	}
	if !p.Park(ParkedTurnRecord{SessionID: "s2", Class: llm.FailureThrottle, ResumeAt: now.Add(2 * time.Minute)}) {
		t.Fatal("park s2 refused")
	}
	if p.Pending() != 2 {
		t.Fatalf("pending = %d, want 2", p.Pending())
	}
	if p.Park(ParkedTurnRecord{SessionID: "s3", Class: llm.FailureQuota}) {
		t.Fatal("refused park should not change pending")
	}
	if p.Pending() != 2 {
		t.Fatalf("pending after refusal = %d, want 2", p.Pending())
	}
}

// Next(class) returns the earliest resume time for that class (leaf 04's
// per-class surface), not the global earliest.
func TestTurnParker_NextPerClass(t *testing.T) {
	p, _ := newTestTurnParker(t, 24*time.Hour, func(ctx context.Context, turn ParkedTurnRecord) {})
	now := time.Now()

	quotaEarly := now.Add(30 * time.Second)
	quotaLate := now.Add(2 * time.Minute)
	throttle := now.Add(time.Minute)

	if !p.Park(ParkedTurnRecord{SessionID: "q1", Class: llm.FailureQuota, ResumeAt: quotaLate}) {
		t.Fatal("park q1 refused")
	}
	if !p.Park(ParkedTurnRecord{SessionID: "q2", Class: llm.FailureQuota, ResumeAt: quotaEarly}) {
		t.Fatal("park q2 refused")
	}
	if !p.Park(ParkedTurnRecord{SessionID: "t1", Class: llm.FailureThrottle, ResumeAt: throttle}) {
		t.Fatal("park t1 refused")
	}

	got, ok := p.Next(llm.FailureQuota)
	if !ok || !got.Equal(quotaEarly) {
		t.Fatalf("Next(quota) = %v,%v want %v,true", got, ok, quotaEarly)
	}
	got, ok = p.Next(llm.FailureThrottle)
	if !ok || !got.Equal(throttle) {
		t.Fatalf("Next(throttle) = %v,%v want %v,true", got, ok, throttle)
	}
	if _, ok := p.Next(llm.FailureNone); ok {
		t.Fatal("Next(none) should find nothing parked")
	}

	// After both quota records drain past, Next(quota) reports none.
	p.mu.Lock()
	p.parked = p.parked[:0]
	p.mu.Unlock()
	if _, ok := p.Next(llm.FailureQuota); ok {
		t.Fatal("Next(quota) should be false with nothing parked")
	}
}

func TestTurnParker_StopHaltsDrain(t *testing.T) {
	var mu sync.Mutex
	resumed := 0
	p, clock := newTestTurnParker(t, 24*time.Hour, func(ctx context.Context, turn ParkedTurnRecord) {
		mu.Lock()
		resumed++
		mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	if !p.Park(ParkedTurnRecord{SessionID: "s1", Class: llm.FailureQuota, ResumeAt: clock.nowFn().Add(time.Millisecond)}) {
		t.Fatal("park refused")
	}
	clock.advance(time.Millisecond)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return resumed >= 1
	})
	p.Stop()
	if p.Pending() != 0 {
		t.Fatalf("pending after drain = %d, want 0", p.Pending())
	}

	// Parked-and-dropped path: park a new turn after Stop; it must never
	// resume (no poller) and remain counted until Stop's drop log.
	stopped := NewTurnParker(parkedTurnTestLogger(), func(ctx context.Context, turn ParkedTurnRecord) {
		mu.Lock()
		resumed++
		mu.Unlock()
	}, 24*time.Hour)
	stopped.SetPollInterval(10 * time.Millisecond)
	stopped.nowFunc = p.nowFunc
	stopped.testDrainGate = p.testDrainGate
	stoppedNow := newTestClock(time.Now())
	stopped.nowFunc = stoppedNow.nowFn
	stopped.testDrainGate = stoppedNow.WaitUntil
	stopped.Park(ParkedTurnRecord{SessionID: "s2", Class: llm.FailureQuota, ResumeAt: stoppedNow.nowFn().Add(time.Minute)})
	stopped.Stop() // logs dropped=1; no resume func may fire
	mu.Lock()
	defer mu.Unlock()
	if resumed != 1 {
		t.Fatalf("resumed = %d, want 1 (post-stop park must never resume)", resumed)
	}
}

func TestTurnParker_ResumeCallbackErrorDoesNotCrash(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	p, clock := newTestTurnParker(t, 24*time.Hour, func(ctx context.Context, turn ParkedTurnRecord) {
		mu.Lock()
		attempts++
		mu.Unlock()
		panic(errors.New("resume callback exploded"))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)
	defer p.Stop()

	// The panicking callback must not take down the watcher: drainDue
	// removes the turn from the queue before invoking resume (quota
	// behavior on error is drop), so the watcher keeps serving later turns.
	if !p.Park(ParkedTurnRecord{SessionID: "s1", Class: llm.FailureQuota, ResumeAt: clock.nowFn().Add(time.Millisecond)}) {
		t.Fatal("park s1 refused")
	}
	clock.advance(time.Millisecond)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return attempts == 1
	})
	if p.Pending() != 0 {
		t.Fatalf("pending after panicking resume = %d, want 0 (dropped)", p.Pending())
	}
	// A later park still resumes normally.
	if !p.Park(ParkedTurnRecord{SessionID: "s2", Class: llm.FailureQuota, ResumeAt: clock.nowFn().Add(10 * time.Millisecond)}) {
		t.Fatal("park s2 refused")
	}
	clock.advance(15 * time.Millisecond)
	clock.proceed()
	waitUntil(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return attempts == 2
	})
}

func TestTurnParker_NilSafe(t *testing.T) {
	var p *TurnParker
	p.Park(ParkedTurnRecord{})
	if p.Pending() != 0 {
		t.Fatal("nil parker Pending should be 0")
	}
	if _, ok := p.Next(llm.FailureQuota); ok {
		t.Fatal("nil parker Next should be false")
	}
	p.Start(context.Background())
	p.Stop()
}

// --- leaf 04 Task 1: WaitInfo (per-class surface query) ---------------------

// TestTurnParker_WaitInfoMixedClasses pins the surface contract: one query
// returns one ParkWaitInfo per class that has parked records, each carrying
// the earliest resume time and the pending count for that class. Classes
// with nothing parked are absent, and entries are sorted by next-resume so
// surfaces render deterministically.
func TestTurnParker_WaitInfoMixedClasses(t *testing.T) {
	now := time.Now()
	p := NewTurnParker(parkedTurnTestLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	p.nowFunc = func() time.Time { return now }

	if got := p.WaitInfo(); len(got) != 0 {
		t.Fatalf("WaitInfo on empty parker = %+v, want empty", got)
	}

	quotaAt := now.Add(30 * time.Minute)
	throttleAt := now.Add(15 * time.Minute)
	if !p.Park(ParkedTurnRecord{SessionID: "q1", Class: llm.FailureQuota, ResumeAt: quotaAt}) {
		t.Fatal("quota park refused")
	}
	if !p.Park(ParkedTurnRecord{SessionID: "q2", Class: llm.FailureQuota, ResumeAt: quotaAt.Add(time.Minute)}) {
		t.Fatal("second quota park refused")
	}
	if !p.Park(ParkedTurnRecord{SessionID: "t1", Class: llm.FailureThrottle, ResumeAt: throttleAt}) {
		t.Fatal("throttle park refused")
	}

	info := p.WaitInfo()
	if len(info) != 2 {
		t.Fatalf("WaitInfo len = %d, want 2 (one per parked class)", len(info))
	}
	// Sorted by next resume: throttle (15m) before quota (30m).
	if info[0].Class != llm.FailureThrottle {
		t.Errorf("info[0].Class = %v, want %v (earliest resume first)", info[0].Class, llm.FailureThrottle)
	}
	if !info[0].Next.Equal(throttleAt) {
		t.Errorf("info[0].Next = %v, want %v", info[0].Next, throttleAt)
	}
	if info[0].Pending != 1 {
		t.Errorf("info[0].Pending = %d, want 1", info[0].Pending)
	}
	if info[1].Class != llm.FailureQuota {
		t.Errorf("info[1].Class = %v, want %v", info[1].Class, llm.FailureQuota)
	}
	if !info[1].Next.Equal(quotaAt) {
		t.Errorf("info[1].Next = %v, want %v (earliest of the two quota records)", info[1].Next, quotaAt)
	}
	if info[1].Pending != 2 {
		t.Errorf("info[1].Pending = %d, want 2", info[1].Pending)
	}
}

// TestTurnParker_WaitInfoTracksResumeAndRefusals: WaitInfo reflects the live
// queue — a resumed record leaves its class, an unparkable record never
// enters it.
func TestTurnParker_WaitInfoTracksResumeAndRefusals(t *testing.T) {
	now := time.Now()
	p := NewTurnParker(parkedTurnTestLogger(), func(context.Context, ParkedTurnRecord) {}, time.Hour)
	p.nowFunc = func() time.Time { return now }

	// Zero resume time is refused (cannot schedule) and must not appear.
	if p.Park(ParkedTurnRecord{SessionID: "s0", Class: llm.FailureQuota}) {
		t.Fatal("zero-resume park should be refused")
	}
	if got := p.WaitInfo(); len(got) != 0 {
		t.Fatalf("WaitInfo after refused park = %+v, want empty", got)
	}

	if !p.Park(ParkedTurnRecord{SessionID: "s1", Class: llm.FailureThrottle, ResumeAt: now.Add(time.Minute)}) {
		t.Fatal("throttle park refused")
	}
	// Drain manually the way drainDue would (drainDue calls the resume
	// callback; here we only need the queue effect).
	p.mu.Lock()
	p.parked = nil
	p.mu.Unlock()
	if got := p.WaitInfo(); len(got) != 0 {
		t.Fatalf("WaitInfo after drain = %+v, want empty", got)
	}
}

// TestTurnParker_WaitInfoNilSafe mirrors the other nil-surface guards.
func TestTurnParker_WaitInfoNilSafe(t *testing.T) {
	var p *TurnParker
	if got := p.WaitInfo(); got != nil {
		t.Fatalf("nil parker WaitInfo = %+v, want nil", got)
	}
}
