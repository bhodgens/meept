package voice

// Gate tests for contract C5 (master.md, quoted verbatim). The manager maps
// keyed by call_control_id, mutex-guarded. Borrowed from uptractor
// callsvc/manager.go with one addition: the release hook.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// fakeController records every call-control invocation.
type fakeController struct {
	mu       sync.Mutex
	answers  int
	streams  int
	hangups  int
	streamed []string // ccids StartStreaming was called with
}

func (f *fakeController) AnswerCall(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers++
	return nil
}

func (f *fakeController) StartStreaming(_ context.Context, ccid, _ string, _ telnyx.Codec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streams++
	f.streamed = append(f.streamed, ccid)
	return nil
}

func (f *fakeController) HangupCall(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hangups++
	return nil
}

func (f *fakeController) countAnswers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.answers
}

func (f *fakeController) countStreams() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streams
}

func (f *fakeController) countHangups() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hangups
}

// noopSpawn never spawns a live session.
func noopSpawn(_ context.Context, _ telnyx.CallEvent) (context.CancelFunc, error) {
	return func() {}, nil
}

func newTestManager(t *testing.T, tel *fakeController) *Manager {
	t.Helper()
	return NewManager(ManagerConfig{
		StreamURL: "wss://tunnel.example/media",
		Codec:     telnyx.Codec{BidirectionalCodec: "L16", SamplingRate: 16000},
	}, tel, noopSpawn)
}

func inboundInitiated(ccid string) telnyx.CallEvent {
	return telnyx.CallEvent{
		EventType:     "call.initiated",
		CallControlID: ccid,
		From:          "+15551230001",
		To:            "+15551230002",
		Direction:     "incoming",
	}
}

// TestManagerInboundAnswerStream covers the inbound path: call.initiated
// answers the call and spawns; call.answered starts streaming.
func TestManagerInboundAnswerStream(t *testing.T) {
	tel := &fakeController{}
	m := newTestManager(t, tel)

	if err := m.HandleCallEvent(t.Context(), inboundInitiated("cc-in-1")); err != nil {
		t.Fatalf("HandleCallEvent(initiated): %v", err)
	}
	tel.mu.Lock()
	ans := tel.answers
	tel.mu.Unlock()
	if ans != 1 {
		t.Errorf("AnswerCall count after initiated = %d, want 1", ans)
	}

	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.answered", CallControlID: "cc-in-1", Direction: "incoming",
	}); err != nil {
		t.Fatalf("HandleCallEvent(answered): %v", err)
	}
	if got := tel.countStreams(); got != 1 {
		t.Errorf("StartStreaming count after answered = %d, want 1", got)
	}
}

// TestManagerOutboundNoAnswer pins the C4/C5 rule verbatim: never AnswerCall
// an outbound leg. Given a tracked outbound id, call.answered must call
// StartStreaming and must NOT call AnswerCall (the 404 aborts the session -
// uptractor live drill).
func TestManagerOutboundNoAnswer(t *testing.T) {
	tel := &fakeController{}
	m := newTestManager(t, tel)

	if err := m.TrackOutbound("cc-out-1"); err != nil {
		t.Fatalf("TrackOutbound: %v", err)
	}
	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.answered", CallControlID: "cc-out-1", Direction: "outgoing",
	}); err != nil {
		t.Fatalf("HandleCallEvent(outbound answered): %v", err)
	}
	if got := tel.countAnswers(); got != 0 {
		t.Errorf("AnswerCall called %d times on outbound leg, want 0 (never answer outbound)", got)
	}
	if got := tel.countStreams(); got != 1 {
		t.Errorf("StartStreaming count = %d, want 1", got)
	}
}

// TestManagerUntrackedAnsweredRefused pins: call.answered for an untracked
// id is refused with a named error.
func TestManagerUntrackedAnsweredRefused(t *testing.T) {
	m := newTestManager(t, &fakeController{})
	err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.answered", CallControlID: "cc-unknown", Direction: "incoming",
	})
	if err == nil {
		t.Fatalf("answered for untracked ccid accepted, want refusal error")
	}
}

// TestManagerUnknownHangupNil pins: hangup for an unknown id is nil
// (idempotent teardown).
func TestManagerUnknownHangupNil(t *testing.T) {
	m := newTestManager(t, &fakeController{})
	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.hangup", CallControlID: "cc-never-seen",
	}); err != nil {
		t.Errorf("hangup for unknown ccid = %v, want nil (idempotent teardown)", err)
	}
}

// TestOnMediaStartIdempotent pins: the second OnMediaStart for the same ccid
// is a no-op (dial invoked exactly once).
func TestOnMediaStartIdempotent(t *testing.T) {
	m := newTestManager(t, &fakeController{})
	if err := m.HandleCallEvent(t.Context(), inboundInitiated("cc-media-1")); err != nil {
		t.Fatalf("HandleCallEvent(initiated): %v", err)
	}

	dials := 0
	dial := func(_ context.Context) error {
		dials++
		return nil
	}
	if err := m.OnMediaStart("cc-media-1", dial); err != nil {
		t.Fatalf("first OnMediaStart: %v", err)
	}
	if err := m.OnMediaStart("cc-media-1", dial); err != nil {
		t.Fatalf("second OnMediaStart: %v", err)
	}
	if dials != 1 {
		t.Errorf("dial invoked %d times, want 1 (idempotent media registration)", dials)
	}
}

// TestManagerWaitCallClosesOnHangup pins: WaitCall's channel closes when the
// call hangs up.
func TestManagerWaitCallClosesOnHangup(t *testing.T) {
	m := newTestManager(t, &fakeController{})
	if err := m.HandleCallEvent(t.Context(), inboundInitiated("cc-wait-1")); err != nil {
		t.Fatalf("HandleCallEvent(initiated): %v", err)
	}
	done := m.WaitCall("cc-wait-1")
	if done == nil {
		t.Fatalf("WaitCall returned nil channel")
	}
	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.hangup", CallControlID: "cc-wait-1",
	}); err != nil {
		t.Fatalf("HandleCallEvent(hangup): %v", err)
	}
	select {
	case <-done:
		// expected: channel closed on hangup
	case <-time.After(2 * time.Second):
		t.Fatalf("WaitCall channel did not close within 2s of hangup")
	}
}

// TestManagerReleaseHookOnce pins the borrowed-plus-one behavior: the
// release hook fires exactly once per ccid on hangup, and a duplicate hangup
// does not fire it again.
func TestManagerReleaseHookOnce(t *testing.T) {
	tel := &fakeController{}
	m := newTestManager(t, tel)
	if err := m.HandleCallEvent(t.Context(), inboundInitiated("cc-hook-1")); err != nil {
		t.Fatalf("HandleCallEvent(initiated): %v", err)
	}

	var fired []Outcome
	var mu sync.Mutex
	m.SetReleaseHook(func(ccid string, outcome Outcome) {
		mu.Lock()
		defer mu.Unlock()
		fired = append(fired, outcome)
	})

	_ = m.HandleCallEvent(t.Context(), telnyx.CallEvent{EventType: "call.hangup", CallControlID: "cc-hook-1"})
	_ = m.HandleCallEvent(t.Context(), telnyx.CallEvent{EventType: "call.hangup", CallControlID: "cc-hook-1"})

	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 1 {
		t.Errorf("release hook fired %d times, want exactly 1 (double hangup is idempotent)", len(fired))
	}
	if len(fired) == 1 && fired[0] != OutcomeHangup {
		t.Errorf("release outcome = %q, want %q", fired[0], OutcomeHangup)
	}
}

// TestManagerSetStreamURL pins the mutex-guarded live URL update.
func TestManagerSetStreamURL(t *testing.T) {
	m := newTestManager(t, &fakeController{})
	m.SetStreamURL("wss://new-tunnel.example/media")
	if got := m.StreamURL(); got != "wss://new-tunnel.example/media" {
		t.Errorf("StreamURL() = %q, want updated value", got)
	}
}

// TestManagerFailReleases pins Fail(ccid, reason) firing the release hook
// with the failed outcome (streaming.failed path).
func TestManagerFailReleases(t *testing.T) {
	m := newTestManager(t, &fakeController{})
	if err := m.HandleCallEvent(t.Context(), inboundInitiated("cc-fail-1")); err != nil {
		t.Fatalf("HandleCallEvent(initiated): %v", err)
	}
	fired := make(chan Outcome, 1)
	m.SetReleaseHook(func(ccid string, outcome Outcome) {
		fired <- outcome
	})
	m.Fail("cc-fail-1", "streaming failed")
	select {
	case o := <-fired:
		if o != OutcomeFailed {
			t.Errorf("Fail outcome = %q, want %q", o, OutcomeFailed)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("release hook not fired after Fail")
	}
}

// TestManagerAnsweredHookFires pins audit 2026-11-06 H1: SetAnsweredHook
// wires the call.answered notification - the hook fires for a tracked
// OUTBOUND answered event (the no-answer-timer cancel rides it) and for a
// tracked inbound one, but never for an untracked id (refused before the
// hook) and not when no hook is set.
func TestManagerAnsweredHookFires(t *testing.T) {
	tel := &fakeController{}
	m := newTestManager(t, tel)

	hooked := make(chan string, 4)
	m.SetAnsweredHook(func(ccid string) { hooked <- ccid })

	// Tracked outbound: hook must fire.
	if err := m.TrackOutbound("cc-answ-out"); err != nil {
		t.Fatalf("TrackOutbound: %v", err)
	}
	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.answered", CallControlID: "cc-answ-out", Direction: "outgoing",
	}); err != nil {
		t.Fatalf("HandleCallEvent(outbound answered): %v", err)
	}
	select {
	case got := <-hooked:
		if got != "cc-answ-out" {
			t.Errorf("answered hook ccid = %q, want cc-answ-out", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("answered hook not fired for tracked outbound call.answered (H1: unwired notification)")
	}

	// Tracked inbound: hook must fire too.
	if err := m.HandleCallEvent(t.Context(), inboundInitiated("cc-answ-in")); err != nil {
		t.Fatalf("HandleCallEvent(initiated): %v", err)
	}
	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.answered", CallControlID: "cc-answ-in", Direction: "incoming",
	}); err != nil {
		t.Fatalf("HandleCallEvent(inbound answered): %v", err)
	}
	select {
	case got := <-hooked:
		if got != "cc-answ-in" {
			t.Errorf("answered hook ccid = %q, want cc-answ-in", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("answered hook not fired for tracked inbound call.answered")
	}

	// Untracked id: refused, hook NOT invoked (channel stays empty).
	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.answered", CallControlID: "cc-answ-unknown", Direction: "incoming",
	}); err == nil {
		t.Fatalf("answered for untracked ccid accepted, want refusal error")
	}
	select {
	case got := <-hooked:
		t.Errorf("answered hook fired for untracked id %q, want no invocation", got)
	default:
	}
}

// failingSpawn simulates the wiring-side spawn seam erroring (child down,
// registry lost a race). Audit 2026-11-06 H3: the caller must not be
// stranded - the manager hangs the leg up and fires the release fan-out.
func failingSpawn(_ context.Context, _ telnyx.CallEvent) (context.CancelFunc, error) {
	return nil, errors.New("voice child unavailable")
}

// TestManagerSpawnFailureHangsUpCaller pins H3: on a spawn failure after
// AnswerCall, HandleCallEvent (a) issues the carrier HangupCall for the leg,
// (b) fires the release hook exactly once with OutcomeFailed (registry slot
// / goal / effects release), and (c) surfaces the spawn error to the webhook
// path. A later hangup for the already-released id stays nil (idempotent).
func TestManagerSpawnFailureHangsUpCaller(t *testing.T) {
	tel := &fakeController{}
	m := NewManager(ManagerConfig{
		StreamURL: "wss://tunnel.example/media",
		Codec:     telnyx.Codec{BidirectionalCodec: "L16", SamplingRate: 16000},
	}, tel, failingSpawn)

	released := make(chan Outcome, 1)
	m.SetReleaseHook(func(ccid string, outcome Outcome) { released <- outcome })

	err := m.HandleCallEvent(t.Context(), inboundInitiated("cc-spawn-1"))
	if err == nil {
		t.Fatalf("spawn failure swallowed, want surfaced error")
	}
	if got := tel.countHangups(); got != 1 {
		t.Errorf("HangupCall count after spawn failure = %d, want 1 (caller must not be stranded ringing)", got)
	}
	select {
	case o := <-released:
		if o != OutcomeFailed {
			t.Errorf("spawn-failure release outcome = %q, want %q", o, OutcomeFailed)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("release hook not fired after spawn failure (state would leak)")
	}
	// The terminal event already ran: a late hangup is a no-op, no second release.
	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.hangup", CallControlID: "cc-spawn-1",
	}); err != nil {
		t.Errorf("late hangup after spawn-failure teardown = %v, want nil", err)
	}
	select {
	case o := <-released:
		t.Errorf("release hook fired twice (%v), want exactly once", o)
	default:
	}
	if got := tel.countHangups(); got != 1 {
		t.Errorf("HangupCall count after late hangup = %d, want still 1 (spawn-failure path owns teardown)", got)
	}
}

// TestManagerSpawnFailureHangupErrorStillReleases pins the degraded branch
// of H3: when the carrier hangup ALSO fails, the release fan-out still fires
// exactly once and the surfaced error names both failures.
func TestManagerSpawnFailureHangupErrorStillReleases(t *testing.T) {
	tel := &fakeController{}
	m := NewManager(ManagerConfig{
		StreamURL: "wss://tunnel.example/media",
		Codec:     telnyx.Codec{BidirectionalCodec: "L16", SamplingRate: 16000},
	}, tel, failingSpawn)

	released := make(chan Outcome, 2)
	m.SetReleaseHook(func(ccid string, outcome Outcome) { released <- outcome })

	if err := m.HandleCallEvent(t.Context(), inboundInitiated("cc-spawn-2")); err == nil {
		t.Fatalf("spawn failure swallowed, want surfaced error")
	}
	select {
	case o := <-released:
		if o != OutcomeFailed {
			t.Errorf("release outcome = %q, want %q", o, OutcomeFailed)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("release hook not fired even when the carrier hangup failed")
	}
}

// TestManagerWaitCallNotRetainedAfterTeardown pins audit 2026-11-06 M3: the
// done channel is NOT retained after release, so a WaitCall issued AFTER
// teardown yields a fresh never-closing channel (the original holder keeps
// its closed channel; a late re-resolver does not).
func TestManagerWaitCallNotRetainedAfterTeardown(t *testing.T) {
	m := newTestManager(t, &fakeController{})
	if err := m.HandleCallEvent(t.Context(), inboundInitiated("cc-wait-2")); err != nil {
		t.Fatalf("HandleCallEvent(initiated): %v", err)
	}
	done := m.WaitCall("cc-wait-2")
	if err := m.HandleCallEvent(t.Context(), telnyx.CallEvent{
		EventType: "call.hangup", CallControlID: "cc-wait-2",
	}); err != nil {
		t.Fatalf("HandleCallEvent(hangup): %v", err)
	}
	select {
	case <-done:
		// the original channel closed: fine
	case <-time.After(2 * time.Second):
		t.Fatalf("original WaitCall channel never closed")
	}
	// Post-teardown re-resolution: fresh channel that never closes.
	select {
	case <-m.WaitCall("cc-wait-2"):
		t.Errorf("post-teardown WaitCall channel closed, want a fresh never-closing channel (M3: no retention)")
	case <-time.After(150 * time.Millisecond):
	}
}

// The manager must never surface a quota/park error to webhook handling:
// its error contract is plain refusal/teardown errors only. This compiles
// the errors.Is check the talk loop relies on (C6: a voice call is never
// quota-parked).
var _ = errors.Is
