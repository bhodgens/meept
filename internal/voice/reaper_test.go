package voice

// Gate tests for contract C3 (master.md, quoted verbatim):
//
//	C3 ReaperConfig: type ReaperConfig struct { Status
//	CallStatusChecker; Interval, DeadAir, MaxDuration time.Duration } —
//	SEMANTICS per owner 2026-11-11: the reaper releases a call only
//	after DEAD AIR — no caller-side audio/response for `DeadAir`
//	(default 60s), NOT merely age-since-answer. The reaper's {ccid:
//	lastActivity} map is fed by the answered observer AND refreshed on
//	every caller-side audio/turn event; a probe fires only when
//	now-lastActivity > DeadAir. MaxDuration (default 30m, from call
//	start) remains the absolute cap. Interval default 30s.
//	func (m *Manager) RunReaper(ctx context.Context, cfg ReaperConfig)
//	— no-op + Error log when Status nil. CallStatusChecker is a
//	STANDALONE interface satisfied by *telnyx.Client. Reaper releases
//	via FailOutcome(ccid, outcome, reason) (manager.go:539) so the
//	frozen reason strings ride the release; no Manager state or
//	constructor change.
//
// Adapted from uptractor phone/core/internal/callsvc/reaper_test.go (fake
// CallStatusChecker pattern reused; MinAge cases replaced by the
// owner-ratified DEAD-AIR semantics). RED until leaf 02 (ReaperConfig /
// CallStatusChecker / RunReaper / Defaults undefined).

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// fakeStatus answers GetCallStatus from a per-call table and records
// which ids were probed (the uptractor fake, verbatim shape).
type fakeStatus struct {
	mu      sync.Mutex
	byID    map[string]telnyx.CallStatus
	errByID map[string]error
	asked   []string
}

func (f *fakeStatus) GetCallStatus(_ context.Context, id string) (telnyx.CallStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	if err := f.errByID[id]; err != nil {
		return telnyx.CallStatus{}, err
	}
	return f.byID[id], nil
}

func (f *fakeStatus) probeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

// captureFail records FailOutcome funnels so tests pin the release reason
// strings and exactly-once semantics.
type captureFail struct {
	mu      sync.Mutex
	calls   []string // ccid in FailOutcome call order
	outcome Outcome
	reasons []string
}

func (c *captureFail) add(ccid string, o Outcome, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, ccid)
	c.outcome = o
	c.reasons = append(c.reasons, reason)
}

// snapFields is the mutex-FREE snapshot of a captureFail's assertion
// fields (dated addendum 2026-11-11: returning captureFail by value
// copies the sync.Mutex — go vet 'return copies lock value'; returning
// only the guarded data keeps the locked-read pattern assertions
// unchanged).
type snapFields struct {
	outcome Outcome
	calls   []string
	reasons []string
}

// snapshot returns a locked copy for assertion reads (dated addendum
// 2026-11-11: -race flagged unlocked reads of the slices the terminal
// observer appends from the reaper goroutine; assertions unchanged).
func (c *captureFail) snapshot() snapFields {
	c.mu.Lock()
	defer c.mu.Unlock()
	return snapFields{
		outcome: c.outcome,
		calls:   append([]string(nil), c.calls...),
		reasons: append([]string(nil), c.reasons...),
	}
}

// callStatusCheckerIsStandalone pins the contract: a STANDALONE interface
// satisfied by *telnyx.Client — NOT an extension of telnyx.CallController
// (adding GetCallStatus there breaks every existing fake). Compile-time.
func TestCallStatusCheckerIsStandalone(t *testing.T) {
	var _ CallStatusChecker = (*telnyx.Client)(nil)
}

// TestReaperConfigDefaults pins the frozen defaults verbatim from
// phone/core/internal/callsvc/reaper.go: Interval 30s, DeadAir 60s
// (replaces the source MinAge 60s under DEAD-AIR semantics), MaxDuration
// 30m. The resolver symbol name is the implementer's choice; the VALUES
// are frozen, so the test resolves through ReaperConfig itself.
func TestReaperConfigDefaults(t *testing.T) {
	cfg := ReaperConfig{}
	if got := cfg.interval(); got != 30*time.Second {
		t.Errorf("interval default = %v, want 30s", got)
	}
	if got := cfg.deadAir(); got != 60*time.Second {
		t.Errorf("deadAir default = %v, want 60s", got)
	}
	if got := cfg.maxDuration(); got != 30*time.Minute {
		t.Errorf("maxDuration default = %v, want 30m", got)
	}
	// Zero values resolve to defaults; explicit values win.
	cfg2 := ReaperConfig{Interval: time.Second, DeadAir: 2 * time.Second, MaxDuration: 3 * time.Minute}
	if cfg2.interval() != time.Second || cfg2.deadAir() != 2*time.Second || cfg2.maxDuration() != 3*time.Minute {
		t.Errorf("explicit config overridden: %v/%v/%v", cfg2.interval(), cfg2.deadAir(), cfg2.maxDuration())
	}
}

// reaperRig: a Manager with an answered inbound call plus a terminal
// observer capturing the FailOutcome fan-out (the reaper's release lane).
type reaperRig struct {
	mgr *Manager
	tel *fakeController
}

func newReaperRig(t *testing.T) (*reaperRig, *captureFail) {
	t.Helper()
	tel := &fakeController{}
	mgr := newTestManager(t, tel)
	cap := &captureFail{}
	mgr.AddTerminalObserver(func(ccid string, outcome Outcome, reason string) {
		cap.add(ccid, outcome, reason)
	})
	return &reaperRig{mgr: mgr, tel: tel}, cap
}

// answeredCall runs ccid through call.initiated + call.answered so the
// manager tracks it (the reaper's answered-observer lane seeds
// lastActivity here).
func (r *reaperRig) answeredCall(t *testing.T, id string) {
	t.Helper()
	ctx := context.Background()
	for _, typ := range []string{"call.initiated", "call.answered"} {
		if err := r.mgr.HandleCallEvent(ctx, telnyx.CallEvent{
			EventType: typ, CallControlID: id, Direction: "incoming",
			From: "+155****0100", To: "+155****9106",
		}); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
}

// TestReaperReleasesSilentCallWithFrozenReason pins DEAD-AIR semantics:
// GIVEN a tracked call with NO caller-side activity beyond DeadAir (60s
// explicit config) whose carrier probe reports Alive=false; WHEN a reaper
// sweep runs; THEN the call is released through FailOutcome exactly once
// with CallEventErrored and the frozen reason "reaped:hangup-lost" on the
// terminal observer, and the teardown ran once.
func TestReaperReleasesSilentCallWithFrozenReason(t *testing.T) {
	rig, cap := newReaperRig(t)
	rig.answeredCall(t, "cc-lost")

	// Caller goes silent: no audio/turn events. DeadAir is set SHORT
	// (200ms) so silence past DeadAir actually elapses in-test; the call
	// is only seconds OLD, so an age-gated reaper (MinAge 60s default)
	// would never reap it — this is the DEAD-AIR discriminator.
	status := &fakeStatus{byID: map[string]telnyx.CallStatus{"cc-lost": {Known: true, Alive: false}}}
	cfg := ReaperConfig{Status: status, Interval: 100 * time.Millisecond, DeadAir: 200 * time.Millisecond, MaxDuration: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rig.mgr.RunReaper(ctx, cfg)

	// Within Interval + DeadAir the reaper must release the silent call
	// through the FailOutcome funnel (never HandleCallEvent — that
	// hard-codes reason "call.hangup").
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(cap.snapshot().calls) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := cap.snapshot()
	if len(snap.calls) != 1 {
		t.Fatalf("release fan-out fired %d times, want exactly 1", len(snap.calls))
	}
	if snap.reasons[0] != "reaped:hangup-lost" {
		t.Errorf("release reason = %q, want frozen %q", snap.reasons[0], "reaped:hangup-lost")
	}
	if rig.tel.countHangups() == 0 {
		t.Error("reaper never asked the carrier to hang up the dead call")
	}
}

// TestReaperFreshCallerActivityNeverReapedRegardlessOfAge pins the
// owner-ratified DEAD-AIR rule: a call with FRESH caller activity is NOT
// reaped regardless of age — age alone never reaps; lastActivity
// refreshes on every caller-side audio/turn event.
func TestReaperFreshCallerActivityNeverReapedRegardlessOfAge(t *testing.T) {
	rig, cap := newReaperRig(t)
	rig.answeredCall(t, "cc-talkative")

	// Carrier would call it dead, but the caller keeps talking.
	status := &fakeStatus{byID: map[string]telnyx.CallStatus{"cc-talkative": {Known: true, Alive: false}}}
	cfg := ReaperConfig{Status: status, Interval: 100 * time.Millisecond, DeadAir: time.Hour, MaxDuration: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rig.mgr.RunReaper(ctx, cfg)

	// Let several sweeps run while the reaper sees caller activity.
	time.Sleep(400 * time.Millisecond)
	snap := cap.snapshot()
	if len(snap.calls) != 0 {
		t.Fatalf("a call with fresh caller activity was reaped: %v (reasons %v)", snap.calls, snap.reasons)
	}
	if status.probeCount() == 0 {
		t.Error("reaper never probed; DEAD-AIR gating untestable")
	}
}

// TestReaperMaxDurationForceHangup pins the absolute cap: a call over
// MaxDuration (from call start) is hung up carrier-side and released with
// reason "reaped:max-duration" — WITHOUT asking Telnyx about aliveness.
func TestReaperMaxDurationForceHangup(t *testing.T) {
	rig, cap := newReaperRig(t)
	rig.answeredCall(t, "cc-long")

	// Still alive carrier-side — MaxDuration overrides. MaxDuration is
	// set SHORT (200ms) so the cap elapses in-test while the call is
	// only seconds old.
	status := &fakeStatus{byID: map[string]telnyx.CallStatus{"cc-long": {Known: true, Alive: true}}}
	cfg := ReaperConfig{Status: status, Interval: 100 * time.Millisecond, DeadAir: time.Hour, MaxDuration: 200 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rig.mgr.RunReaper(ctx, cfg)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(cap.snapshot().calls) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := cap.snapshot()
	if len(snap.calls) != 1 {
		t.Fatalf("release fan-out fired %d times, want exactly 1 for the over-max call", len(snap.calls))
	}
	if snap.reasons[0] != "reaped:max-duration" {
		t.Errorf("release reason = %q, want frozen %q", snap.reasons[0], "reaped:max-duration")
	}
	if rig.tel.countHangups() == 0 {
		t.Error("over-max call must be hung up carrier-side (best-effort)")
	}
}

// TestReaperWithoutCheckerDoesNothing pins the nil-Status contract: no-op
// + Error log; no release ever fires, no probe, no goroutine work.
func TestReaperWithoutCheckerDoesNothing(t *testing.T) {
	rig, cap := newReaperRig(t)
	rig.answeredCall(t, "cc-x")

	cfg := ReaperConfig{Status: nil, Interval: 50 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rig.mgr.RunReaper(ctx, cfg)

	time.Sleep(200 * time.Millisecond)
	snap := cap.snapshot()
	if len(snap.calls) != 0 {
		t.Errorf("reaper released calls with a nil status checker: %v", snap.calls)
	}
}

// TestReaperRaceWithRealHangupReleasesExactlyOnce pins exactly-once: a
// real call.hangup landing while the reaper releases the same ccid must
// yield exactly ONE terminal fan-out (releaseOnce dedupes — the reaper
// adds an input edge, never a second teardown path).
func TestReaperRaceWithRealHangupReleasesExactlyOnce(t *testing.T) {
	rig, cap := newReaperRig(t)
	rig.answeredCall(t, "cc-race")

	// Release both edges concurrently: the reaper's FailOutcome funnel
	// and a real hangup webhook through HandleCallEvent.
	status := &fakeStatus{byID: map[string]telnyx.CallStatus{"cc-race": {Known: false}}}
	cfg := ReaperConfig{Status: status, Interval: 50 * time.Millisecond, DeadAir: 100 * time.Millisecond, MaxDuration: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rig.mgr.RunReaper(ctx, cfg)
	if err := rig.mgr.HandleCallEvent(ctx, telnyx.CallEvent{
		EventType: "call.hangup", CallControlID: "cc-race",
	}); err != nil {
		t.Fatalf("real hangup: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(cap.snapshot().calls) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // let any duplicate land
	snap := cap.snapshot()
	if len(snap.calls) != 1 {
		t.Fatalf("terminal fan-out fired %d times under the reaper/hangup race, want exactly 1 (%v)", len(snap.calls), snap.reasons)
	}
}

// TestReaperCallEventErroredOnBus pins the observable-fault requirement:
// the reaper's release surfaces a CallEventErrored with the frozen reason
// on the phone leg's CallLeg Events bus (OutcomeFailed maps to Errored —
// callleg.go mapOutcomeEvent).
func TestReaperCallEventErroredOnBus(t *testing.T) {
	rig, cap := newReaperRig(t)
	rig.answeredCall(t, "cc-bus")

	status := &fakeStatus{byID: map[string]telnyx.CallStatus{"cc-bus": {Known: false}}}
	cfg := ReaperConfig{Status: status, Interval: 50 * time.Millisecond, DeadAir: 100 * time.Millisecond, MaxDuration: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rig.mgr.RunReaper(ctx, cfg)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(cap.snapshot().calls) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := cap.snapshot()
	if len(snap.calls) == 0 {
		t.Fatal("reaper never released cc-bus")
	}
	if snap.outcome != OutcomeFailed {
		t.Errorf("reaper outcome = %q, want OutcomeFailed (=> CallEventErrored on the leg bus)", snap.outcome)
	}
}
