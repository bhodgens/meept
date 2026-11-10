package voice

// Branch 20261109-statemachine-remediation / leaf 05-00T: voice terminal-
// completeness gate tests. TESTS ONLY — these pin the audit findings
// (2026-11-09 voice) and MUST fail RED against the current implementation
// until leaves 01/03 land. Header comments cite the pinned finding id and
// the RED kind. The releaseOnce fan-out is verified-clean: these tests
// assert once-firing but assume no restructuring of it.

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
	_ "modernc.org/sqlite" // sqlite driver (house convention, callstore tests)
)

// =========================================================================
// Finding 2 (audit 2026-11-09 voice, C-V1): dial.go acquire-failure path
// (~326-343): after TrackOutbound succeeds, the registryGate-vs-Acquire
// race loss calls finishCall + HangupCall but NOT Manager.FailOutcome —
// m.outbound/m.done leak, the release hook never fires, WaitCall never
// closes. TestAcquireFailureFiresFailOutcome pins the target: a terminal
// outcome fires exactly once and the manager maps drain.
// RED kind: behavior (hook never fires today; IsOutbound leaks).
// =========================================================================

// gateAcquireFailRegistry passes the dialer's pre-gates (Live/Cap read
// the inner registry) but ALWAYS loses the Acquire race — the deterministic
// shape of the registryGate-vs-Acquire window at dial.go:326-343.
type gateAcquireFailRegistry struct {
	inner *CallRegistry
}

func (r *gateAcquireFailRegistry) Acquire(_, _, _ string) (Instance, error) {
	return Instance{}, errors.New("voice: gate test: simulated Acquire race loss")
}

func (r *gateAcquireFailRegistry) AcquireLeg(ccid, employeeID, target, leg string) (Instance, error) {
	return r.inner.AcquireLeg(ccid, employeeID, target, leg)
}
func (r *gateAcquireFailRegistry) Release(ccid string) { r.inner.Release(ccid) }
func (r *gateAcquireFailRegistry) Live() []Instance    { return r.inner.Live() }
func (r *gateAcquireFailRegistry) Cap() int            { return r.inner.Cap() }

// gateFixedCCIDController is a DialController with a FIXED ccid (the
// countingController's timestamp-derived ids are unreadable from outside)
// and counts the carrier hangup so the once-only teardown is observable.
type gateFixedCCIDController struct {
	mu      sync.Mutex
	ccid    string
	hangups int
}

func (c *gateFixedCCIDController) DialOut(_ context.Context, _, _, _ string, _ int) (string, error) {
	return c.ccid, nil
}
func (c *gateFixedCCIDController) AnswerCall(_ context.Context, _ string) error { return nil }
func (c *gateFixedCCIDController) StartStreaming(_ context.Context, _, _ string, _ telnyx.Codec) error {
	return nil
}
func (c *gateFixedCCIDController) HangupCall(_ context.Context, ccid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ccid == c.ccid {
		c.hangups++
	}
	return nil
}
func (c *gateFixedCCIDController) hangupCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hangups
}

func TestAcquireFailureFiresFailOutcome(t *testing.T) {
	const ccid = "cc-acquire-race"

	tel := &gateFixedCCIDController{ccid: ccid}
	cfg := dialConfig()
	cfg.TimeoutSecs = 3600 // the no-answer timer must never be the closer here
	reg := &gateAcquireFailRegistry{inner: NewRegistry(4)}

	mgr := NewManager(ManagerConfig{StreamURL: "wss://tunnel.example/media"}, tel, nil)
	var omu sync.Mutex
	var outcomes []Outcome
	var waitCh <-chan struct{}
	mgr.SetReleaseHook(func(got string, outcome Outcome) {
		omu.Lock()
		defer omu.Unlock()
		if got == ccid {
			outcomes = append(outcomes, outcome)
		}
	})

	d := NewDialer(cfg, tel, mgr, reg, newTestLedger(t), &captureAudit{}, testLogger())
	_, err := d.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-race", Target: cfg.AllowedNumbers[0], RequestID: "req-race-1",
	})
	if err == nil {
		t.Fatal("Dial must fail when the registry Acquire loses the race")
	}
	// Keep a handle on the manager's done channel BEFORE any teardown so
	// WaitCall-after-releaseOnce's fresh-channel semantics (audit M3)
	// cannot mask the leak.
	waitCh = mgr.WaitCall(ccid)

	// C-V1 target: the terminal outcome fires EXACTLY ONCE (FailOutcome —
	// the manager's releaseOnce makes a duplicate carrier hangup a no-op)
	// and the manager maps drain. Today nothing ever calls FailOutcome on
	// this path: the hook stays silent and IsOutbound leaks.
	omu.Lock()
	got := append([]Outcome(nil), outcomes...)
	omu.Unlock()
	if len(got) != 1 {
		t.Fatalf("acquire-failure terminal outcomes = %v, want exactly one FailOutcome "+
			"(finding 2: dial.go acquire-failure path never calls Manager.FailOutcome, "+
			"so m.outbound/m.done leak and the release fan-out never fires)", got)
	}
	if got[0] != OutcomeFailed {
		t.Errorf("acquire-failure outcome = %q, want %q", got[0], OutcomeFailed)
	}
	if mgr.IsOutbound(ccid) {
		t.Errorf("manager outbound map leaked ccid %q after the terminal "+
			"(finding 2: m.outbound never drained without FailOutcome)", ccid)
	}
	select {
	case <-waitCh:
	default:
		t.Errorf("WaitCall channel for ccid %q never closed — finding 2 leak: "+
			"m.done is stranded because no FailOutcome ran releaseOnce", ccid)
	}
	// The carrier hangup already happens today; assert it stayed once-only
	// (the fix must not add a second hangup beside the new FailOutcome).
	if n := tel.hangupCount(); n != 1 {
		t.Errorf("carrier hangups = %d, want 1", n)
	}
}

// =========================================================================
// Finding 9 (audit 2026-11-09 voice): dial.go:503-525 — the no-answer
// timer fire re-checks d.answered, then UNLOCKS, then calls
// FailOutcome(no_answer). A NotifyAnswered landing inside that unlocked
// window tears down a call that answered before teardown completed.
// Target (C-V1 leaf 01): check+fire atomic under the dialer lock, so an
// answer delivered BEFORE the teardown completed prevents the teardown.
// RED kind: behavior (deterministic via a stalled FailOutcome — the test
// parks the fire goroutine inside releaseOnce by holding the manager
// mutex, then lands the answer while teardown is still in flight).
// =========================================================================

func TestNoAnswerVsAnsweredRace(t *testing.T) {
	const ccid = "cc-f9-race"
	cfg := dialConfig()
	cfg.TimeoutSecs = 1 // fire at +1s

	tel := &gateFixedCCIDController{ccid: ccid}
	reg := NewRegistry(4)
	if _, err := reg.Acquire(ccid, "emp-f9", cfg.AllowedNumbers[0]); err != nil {
		t.Fatalf("seed acquire: %v", err)
	}
	mgr := NewManager(ManagerConfig{StreamURL: "wss://tunnel.example/media"}, tel, nil)
	var omu sync.Mutex
	var fired []Outcome
	mgr.SetReleaseHook(func(got string, outcome Outcome) {
		omu.Lock()
		defer omu.Unlock()
		if got == ccid {
			fired = append(fired, outcome)
		}
	})
	// Track the leg so releaseOnce sees a live call (the TrackOutbound the
	// dial path would have done).
	if err := mgr.TrackOutbound(ccid); err != nil {
		t.Fatalf("TrackOutbound: %v", err)
	}
	d := NewDialer(cfg, tel, mgr, reg, newTestLedger(t), &captureAudit{}, testLogger())

	// Keep the done channel BEFORE teardown (manager deletes per-call
	// channels on release; a re-resolved WaitCall after teardown yields a
	// fresh never-closing channel — audit M3).
	waitCh := mgr.WaitCall(ccid)
	d.armNoAnswerTimer(ccid)
	// Let the fire goroutine get past its WaitCall read (it takes mgr.mu
	// there) before we park it: then the ONLY thing that can block on our
	// held mgr.mu is the fire branch's FailOutcome -> releaseOnce.
	time.Sleep(100 * time.Millisecond)

	// Park the eventual FailOutcome INSIDE releaseOnce: hold the manager
	// mutex. Same-package test-only lock inspection; no production surface
	// changes.
	mgr.mu.Lock()

	// Wait for the fire to reach FailOutcome: once mgr.mu is handed to the
	// fire goroutine, TryLock keeps failing while it sits in releaseOnce.
	deadline := time.Now().Add(5 * time.Second)
	reachedFire := false
	for time.Now().Before(deadline) {
		if !mgr.mu.TryLock() {
			reachedFire = true
			break
		}
		mgr.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if !reachedFire {
		mgr.mu.Unlock()
		t.Fatal("no-answer fire never reached FailOutcome (staging failure)")
	}

	// The answer webhook lands NOW — after the fire's answered re-check
	// (which ran and saw false, the timer tick having beaten it to the
	// lock) and before the teardown completes (the hook/Done close are
	// still parked behind our held manager mutex).
	ans := make(chan struct{})
	go func() {
		d.NotifyAnswered(ccid)
		close(ans)
	}()
	select {
	case <-ans:
		// Today's shape: the answer landed inside the unlocked window and
		// the parked teardown completed anyway — a no_answer outcome on an
		// answered call and its WaitCall channel closed. Both are the
		// finding-9 defect.
		mgr.mu.Unlock()

		omu.Lock()
		got := append([]Outcome(nil), fired...)
		omu.Unlock()
		for _, o := range got {
			if o == OutcomeNoAnswer {
				t.Errorf("no_answer outcome %q landed on a call that answered before its "+
					"teardown completed (finding 9: dial.go:503-525 re-checks d.answered, "+
					"unlocks, then fires FailOutcome — target: check+fire atomic under the "+
					"dialer lock)", o)
				break
			}
		}
		select {
		case <-waitCh:
			t.Errorf("WaitCall channel of an answered call was closed by the windowed " +
				"no-answer fire (finding 9: teardown of an answered call)")
		case <-time.After(2 * time.Second):
		}
	case <-time.After(2 * time.Second):
		// The fix shape (check+fire atomic under the dialer lock): the fire
		// holds d.mu across FailOutcome, so NotifyAnswered cannot enter the
		// window at all — it parks on the dialer lock. The window is closed;
		// let both complete and pass.
		mgr.mu.Unlock()
		<-ans
	}
}

// =========================================================================
// Finding 7 (audit 2026-11-09 voice, C-V3): calls.db boot sweep. A
// voice_calls row stranded at state='live' by a dead run must reach
// state='ended', outcome='failed' when the store opens, BEFORE listeners
// accept calls. No sweep symbol exists today; RED is pinned at the
// behavior level (the wiring-equivalent): reopen the store over an
// existing db with a stale live row and observe the reconciliation.
// RED kind: behavior via the wiring-level equivalent (documented choice
// per the leaf: no compile-RED named func, keeping the package buildable).
// =========================================================================

func gateCallRowState(t *testing.T, dbPath, callID string) (state, outcome string, found bool) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open calls.db: %v", err)
	}
	defer db.Close() //nolint:errcheck // read-only test handle
	err = db.QueryRow(`SELECT state, outcome FROM voice_calls WHERE call_id = ?`, callID).Scan(&state, &outcome)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false
	}
	if err != nil {
		t.Fatalf("query calls.db: %v", err)
	}
	return state, outcome, true
}

func TestBootSweepEndsStaleLive(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "calls.db")

	// Run 1: a call starts and its process dies mid-call — the row stays
	// 'live' (End only runs through the release fan-out).
	s1, err := NewCallStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewCallStore(run 1): %v", err)
	}
	if err := s1.Start(context.Background(), CallRecord{
		CallID: "cc-stale-live", Number: "+155****0001", EmployeeID: "emp-sweep",
		Direction: DirectionOutgoing, SessionID: "sess-sweep",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close run 1: %v", err)
	}

	// Run 2 (boot): opening the store IS the wiring boot point — C-V3
	// pins that the sweep runs here, before any listener can accept a
	// call, marking stale rows ended/failed.
	s2, err := NewCallStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewCallStore(run 2): %v", err)
	}
	defer s2.Close()

	state, outcome, found := gateCallRowState(t, dbPath, "cc-stale-live")
	if !found {
		t.Fatal("stale row vanished across the reopen")
	}
	if state != "ended" || outcome != "failed" {
		t.Fatalf("boot sweep: stale live row state=%q outcome=%q, want state=\"ended\" "+
			"outcome=\"failed\" (finding 7: no boot sweep exists — voice_calls rows "+
			"stranded 'live' by a dead run are never reconciled)", state, outcome)
	}

	// Idempotence guard the review checklist pins: a sweep re-run over an
	// already-ended row must not resurrect or re-stamp it.
	s3, err := NewCallStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewCallStore(run 3): %v", err)
	}
	defer s3.Close()
	state2, _, _ := gateCallRowState(t, dbPath, "cc-stale-live")
	if state2 != "ended" {
		t.Errorf("second boot sweep changed the row state to %q, want 'ended' (sweep must be idempotent)", state2)
	}
}

// =========================================================================
// Finding 4 (audit 2026-11-09 voice): manager.go OnMediaStart idempotence
// is per-ccid "started once", so a FIRST splice that errored (a non-
// cancellation failure) permanently BLOCKS the reconnect splice — media
// reconnect never re-splices. Target: error-aware idempotence — after an
// errored splice a reconnect MUST re-splice; after a clean end it MUST
// NOT (the review checklist pins the error-aware-not-count-aware shape).
// RED kind: behavior (today's second start for the errored ccid no-ops).
// =========================================================================

func TestMediaReconnectResplices(t *testing.T) {
	mgr := NewManager(ManagerConfig{StreamURL: "wss://tunnel.example/media"}, nil, nil)

	// Case 1 — errored splice: reconnect must re-splice.
	errored := "cc-recon-error"
	firstCalls, secondCalls := 0, 0
	if err := mgr.OnMediaStart(errored, func(context.Context) error {
		firstCalls++
		return errors.New("gate test: splice died on a read error")
	}); err == nil {
		t.Fatal("first OnMediaStart must surface the splice error")
	}
	if firstCalls != 1 {
		t.Fatalf("first splice ran %d times, want 1", firstCalls)
	}
	err2 := mgr.OnMediaStart(errored, func(context.Context) error {
		secondCalls++
		return nil
	})
	if err2 != nil {
		t.Fatalf("reconnect splice errored: %v", err2)
	}
	if secondCalls != 1 {
		t.Errorf("media reconnect after an ERRORED splice did not re-splice (ran %d times, want 1) "+
			"— finding 4: OnMediaStart is 'started once' idempotent per ccid and blocks the "+
			"reconnect splice forever (target: error-aware idempotence)", secondCalls)
	}

	// Case 2 — clean end (successful, never errored): a legitimate late
	// duplicate media start must NOT splice a second bridge.
	clean := "cc-recon-clean"
	cleanCalls := 0
	if err := mgr.OnMediaStart(clean, func(context.Context) error {
		cleanCalls++
		return nil
	}); err != nil {
		t.Fatalf("clean first splice: %v", err)
	}
	if err := mgr.OnMediaStart(clean, func(context.Context) error {
		cleanCalls++
		return nil
	}); err != nil {
		t.Fatalf("clean second start must stay an idempotent nil: %v", err)
	}
	if cleanCalls != 1 {
		t.Errorf("duplicate media start after a CLEAN splice re-spliced (%d runs, want 1) — "+
			"error-aware idempotence must only re-arm after an ERROR", cleanCalls)
	}
}
