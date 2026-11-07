package voice

// Gate tests for contract C9 (master.md) + leaf 11 shapes: typed refusals
// BEFORE any HTTP call, reserve-then-dial effect ordering, retry of the same
// request_id never rings twice, and no-answer timeout release.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// countingController is a CallController fake that counts DialOuts and can
// be pointed at an httptest server.
type countingController struct {
	mu     sync.Mutex
	dials  int
	failDV bool
}

func (c *countingController) DialOut(_ context.Context, _, _, _ string, _ int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failDV {
		return "", errors.New("dial failed")
	}
	c.dials++
	return "cc-dial-" + time.Now().Format("150405.000000000"), nil
}

func (c *countingController) AnswerCall(_ context.Context, _ string) error { return nil }
func (c *countingController) StartStreaming(_ context.Context, _, _ string, _ telnyx.Codec) error {
	return nil
}
func (c *countingController) HangupCall(_ context.Context, _ string) error { return nil }

func (c *countingController) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dials
}

// zeroRequestServer fails the test if ANY http request arrives. Refusals
// must fire BEFORE any HTTP call.
func zeroRequestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("HTTP request reached the server on a refusal path; refusals must fire BEFORE any call")
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

func dialConfig() DialConfig {
	return DialConfig{
		Enabled:         true,
		AllowedNumbers:  []string{"+15551230001"},
		PublicStreamURL: "wss://tunnel.example/media",
		ConnectionID:    "conn-1",
		FromNumber:      "+15551239999",
		TimeoutSecs:     30,
	}
}

// TestDialRefusals pins C9/leaf 11: every policy gate returns a typed error
// (ErrVoiceDisabled, ErrNumberNotAllowed, ErrNoPublicURL, ErrNoConnectionID)
// BEFORE any HTTP call - the httptest server asserts zero requests.
func TestDialRefusals(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*DialConfig)
		wantErr error
	}{
		{"voice disabled", func(c *DialConfig) { c.Enabled = false }, ErrVoiceDisabled},
		{"target not allowed", func(c *DialConfig) { c.AllowedNumbers = nil }, ErrNumberNotAllowed},
		{"empty public url", func(c *DialConfig) { c.PublicStreamURL = "" }, ErrNoPublicURL},
		{"empty connection id", func(c *DialConfig) { c.ConnectionID = "" }, ErrNoConnectionID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := zeroRequestServer(t)
			defer srv.Close()

			cfg := dialConfig()
			tc.mutate(&cfg)
			cfg.TestBaseURL = srv.URL

			reg := NewRegistry(4)
			d := NewDialer(cfg, &countingController{}, nil, reg, newTestLedger(t), nil, testLogger())
			_, err := d.Dial(context.Background(), DialRequest{
				EmployeeID: "emp-1", Target: "+15551230001", RequestID: "req-refusal-1",
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Dial error = %v, want %v (typed refusal)", err, tc.wantErr)
			}
		})
	}

	// Over-cap: registry cap 1 with one live call -> ErrBusy.
	// (bughunt voice F-3: cap 0 is the UNLIMITED sentinel, matching
	// Acquire - it must not refuse every dial.)
	srv := zeroRequestServer(t)
	defer srv.Close()
	cfg := dialConfig()
	cfg.TestBaseURL = srv.URL
	reg := NewRegistry(1)
	if _, err := reg.Acquire("cc-live-0", "emp-0", "+155****0000"); err != nil {
		t.Fatalf("seed acquire: %v", err)
	}
	d := NewDialer(cfg, &countingController{}, nil, reg, newTestLedger(t), nil, testLogger())
	_, err := d.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-1", Target: "+15551230001", RequestID: "req-refusal-2",
	})
	if !errors.Is(err, ErrBusy) {
		t.Errorf("over-cap dial error = %v, want ErrBusy", err)
	}

	// Unlimited sentinel: cap 0 dials succeed (registryGate and Acquire
	// agree; the old code refused every dial here while Acquire accepted it).
	srvU := zeroRequestServer(t)
	defer srvU.Close()
	cfgU := dialConfig()
	cfgU.TestBaseURL = srvU.URL
	dU := NewDialer(cfgU, &countingController{}, nil, NewRegistry(0), newTestLedger(t), nil, testLogger())
	if _, err := dU.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-u1", Target: "+15551230001", RequestID: "req-unlimited-1",
	}); err != nil {
		t.Errorf("cap-0 (unlimited) dial error = %v, want success", err)
	}

	// Duplicate live target: same target twice globally -> ErrDuplicateTarget.
	srv2 := zeroRequestServer(t)
	defer srv2.Close()
	reg2 := NewRegistry(4)
	if _, err := reg2.Acquire("cc-live-1", "emp-2", "+15551230009"); err != nil {
		t.Fatalf("seed acquire: %v", err)
	}
	cfg2 := dialConfig()
	cfg2.TestBaseURL = srv2.URL
	d2 := NewDialer(cfg2, &countingController{}, nil, reg2, newTestLedger(t), nil, testLogger())
	_, err = d2.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-3", Target: "+15551230009", RequestID: "req-refusal-3",
	})
	if !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("duplicate live target error = %v, want ErrDuplicateTarget (one live call per target globally)", err)
	}
}

// TestDialEffectsIdempotent pins C9 verbatim: the effect keyed
// (employee_id, target, request_id) is reserved BEFORE the POST; a retry of
// the same request_id does NOT dial twice (zero additional POSTs).
func TestDialEffectsIdempotent(t *testing.T) {
	tel := &countingController{}
	cfg := dialConfig()
	reg := NewRegistry(4)
	ledger := newTestLedger(t)
	d := NewDialer(cfg, tel, nil, reg, ledger, nil, testLogger())

	req := DialRequest{EmployeeID: "emp-1", Target: "+15551230001", RequestID: "req-idem-1"}

	res1, err := d.Dial(context.Background(), req)
	if err != nil {
		t.Fatalf("first Dial: %v", err)
	}
	if got := tel.count(); got != 1 {
		t.Fatalf("first dial count = %d, want 1", got)
	}

	res2, err := d.Dial(context.Background(), req)
	if err != nil {
		t.Fatalf("retry Dial: %v", err)
	}
	if got := tel.count(); got != 1 {
		t.Errorf("dial count after retry = %d, want 1 (retry never rings twice)", got)
	}
	if res2.CallControlID != res1.CallControlID {
		t.Errorf("retry returned ccid %q, want the reserved result %q", res2.CallControlID, res1.CallControlID)
	}
}

// TestDialOrdering pins C9 ordering: the audit row (direction outgoing,
// target) is written BEFORE DialOut, and a POST failure completes the
// effect with its failed outcome (no leaked reservation).
func TestDialOrdering(t *testing.T) {
	failing := &countingController{failDV: true}
	cfg := dialConfig()
	reg := NewRegistry(4)
	ledger := newCaptureLedger(t)
	audit := &captureAudit{}
	d := NewDialer(cfg, failing, nil, reg, ledger, audit, testLogger())

	_, err := d.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-1", Target: "+15551230001", RequestID: "req-order-1",
	})
	if err == nil {
		t.Fatalf("dial with failing controller succeeded, want error")
	}

	if audit.rowsCount() == 0 {
		t.Errorf("no audit row written (C9: audit row (direction outgoing, target) BEFORE DialOut)")
	}
	if leaked := ledger.pending(); leaked != 0 {
		t.Errorf("%d effect reservations leaked after dial failure, want 0 (failed dial completes its effect)", leaked)
	}
}

// TestOutboundTimeoutRelease pins the no-answer release: with the call never
// answered, the timer fires at timeout_secs and the registry slot is freed
// with outcome no_answer (master Test Gate: unanswered outbound releases
// slot + unparks goal).
func TestOutboundTimeoutRelease(t *testing.T) {
	tel := &countingController{}
	cfg := dialConfig()
	cfg.TimeoutSecs = 1 // test-scale
	reg := NewRegistry(4)
	d := NewDialer(cfg, tel, nil, reg, newTestLedger(t), &captureAudit{}, testLogger())

	res, err := d.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-1", Target: "+15551230001", RequestID: "req-timeout-1",
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	// While waiting: the target is occupied.
	if _, err := reg.Acquire("cc-probe", "emp-9", "+15551230001"); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("target not marked live during pending dial: %v, want ErrDuplicateTarget", err)
	}

	select {
	case <-res.Done:
		// terminal: no-answer path completed
	case <-time.After(10 * time.Second):
		t.Fatalf("dial never reached terminal state within timeout window")
	}

	// After no-answer: the slot is freed (acquire for the same target succeeds).
	if _, err := reg.Acquire("cc-after", "emp-9", "+15551230001"); err != nil {
		t.Errorf("target still occupied after no-answer timeout: %v, want slot released", err)
	}
	reg.Release("cc-after")
}

// TestDialRegistryNormalizesTargets pins audit 2026-11-06 M7: the registry
// normalizes targets at the Acquire boundary, so an instance acquired with
// a raw-form number (the inbound spawn seam passes the webhook `from`)
// collides with the canonical E.164 twin at both Acquire and the dialer's
// duplicate-target gate. Without boundary normalization the two shapes
// could coexist live and ring one number twice.
func TestDialRegistryNormalizesTargets(t *testing.T) {
	reg := NewRegistry(4)
	// Acquire with a bare 10-digit number; the canonical twin must refuse.
	inst, err := reg.Acquire("cc-raw-1", "emp-1", "555 123 0001")
	if err != nil {
		t.Fatalf("Acquire(raw form): %v", err)
	}
	if inst.Target != "+15551230001" {
		t.Errorf("Acquire stored target %q, want canonical +15551230001 (M7: normalize at the boundary)", inst.Target)
	}
	if _, err := reg.Acquire("cc-can-1", "emp-2", "+15551230001"); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("Acquire(canonical twin) = %v, want ErrDuplicateTarget", err)
	}

	// The dialer's duplicate-target scan must also treat the live raw-form
	// entry as a duplicate of the canonical dial target (zero HTTP).
	srv := zeroRequestServer(t)
	defer srv.Close()
	cfg := dialConfig()
	cfg.TestBaseURL = srv.URL
	cfg.AllowedNumbers = []string{"+15551230001"}
	d := NewDialer(cfg, &countingController{}, nil, reg, newTestLedger(t), nil, testLogger())
	if _, err := d.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-3", Target: "+15551230001", RequestID: "req-m7-1",
	}); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("dial against live raw-form twin = %v, want ErrDuplicateTarget (M7)", err)
	}
}

// TestDialTimeoutSecsDrivesCarrierAndTimer pins audit 2026-11-06 M8: the
// configured TimeoutSecs drives the carrier-side timeout_secs (the fake
// records the value DialOut was invoked with) AND the local no-answer
// timer; the zero sentinel falls back to the shared default on both sides.
func TestDialTimeoutSecsDrivesCarrierAndTimer(t *testing.T) {
	// Configured value reaches DialOut verbatim.
	rec := &timeoutRecordingController{inner: &countingController{}}
	cfg := dialConfig()
	cfg.TimeoutSecs = 17
	d := NewDialer(cfg, rec, nil, NewRegistry(4), newTestLedger(t), &captureAudit{}, testLogger())
	allowedTarget := cfg.AllowedNumbers[0]
	if _, err := d.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-1", Target: allowedTarget, RequestID: "req-m8-1",
	}); err != nil {
		t.Fatalf("Dial(TimeoutSecs=17): %v", err)
	}
	if got := rec.lastTimeout(); got != 17 {
		t.Errorf("carrier timeout_secs = %d, want 17 (M8: config drives the carrier field, not a hardcoded 30)", got)
	}
	if got := d.NoAnswerSecs(); got != 17 {
		t.Errorf("NoAnswerSecs() = %d, want 17 (local timer must match the carrier limit)", got)
	}

	// Zero sentinel: both sides fall back to the shared default.
	cfg0 := dialConfig()
	cfg0.TimeoutSecs = 0
	d0 := NewDialer(cfg0, rec, nil, NewRegistry(4), newTestLedger(t), &captureAudit{}, testLogger())
	if got := d0.NoAnswerSecs(); got != DefaultNoAnswerSecs {
		t.Errorf("NoAnswerSecs() with unset config = %d, want default %d", got, DefaultNoAnswerSecs)
	}
	if _, err := d0.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-1", Target: allowedTarget, RequestID: "req-m8-2",
	}); err != nil {
		t.Fatalf("Dial(TimeoutSecs=0): %v", err)
	}
	if got := rec.lastTimeout(); got != DefaultNoAnswerSecs {
		t.Errorf("carrier timeout_secs with unset config = %d, want default %d", got, DefaultNoAnswerSecs)
	}
}

// timeoutRecordingController wraps a countingController and records the
// carrier timeout_secs the dialer passed to DialOut.
type timeoutRecordingController struct {
	inner *countingController

	mu   sync.Mutex
	last int
}

func (c *timeoutRecordingController) DialOut(ctx context.Context, to, from, connectionID string, timeoutSecs int) (string, error) {
	c.mu.Lock()
	c.last = timeoutSecs
	c.mu.Unlock()
	return c.inner.DialOut(ctx, to, from, connectionID, timeoutSecs)
}

func (c *timeoutRecordingController) AnswerCall(ctx context.Context, ccid string) error {
	return c.inner.AnswerCall(ctx, ccid)
}

func (c *timeoutRecordingController) StartStreaming(ctx context.Context, ccid, url string, codec telnyx.Codec) error {
	return c.inner.StartStreaming(ctx, ccid, url, codec)
}

func (c *timeoutRecordingController) HangupCall(ctx context.Context, ccid string) error {
	return c.inner.HangupCall(ctx, ccid)
}

func (c *timeoutRecordingController) lastTimeout() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// TestDialAnsweredCancelsNoAnswerTimer pins the H1 wiring end to end: a
// dialer constructed with a live manager has NotifyAnswered registered as
// the manager's call.answered hook, so an answered outbound call survives
// past the original timer deadline (no late OutcomeNoAnswer on a live call,
// registry slot retained) while an never-answered control call still times
// out and releases.
func TestDialAnsweredCancelsNoAnswerTimer(t *testing.T) {
	tel := &timeoutRecordingController{inner: &countingController{}}
	cfg := dialConfig()
	cfg.TimeoutSecs = 1 // test-scale
	reg := NewRegistry(4)
	mgr := NewManager(ManagerConfig{StreamURL: "wss://tunnel.example/media"}, tel, nil)
	d := NewDialer(cfg, tel, mgr, reg, newTestLedger(t), &captureAudit{}, testLogger())

	// Answered call: dial, then simulate the carrier pickup through the
	// manager's webhook path (TrackOutbound already ran inside Dial).
	res, err := d.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-1", Target: allowedNumber(t, cfg), RequestID: "req-h1-1",
	})
	if err != nil {
		t.Fatalf("Dial(answered call): %v", err)
	}
	if err := mgr.HandleCallEvent(context.Background(), telnyx.CallEvent{
		EventType: "call.answered", CallControlID: res.CallControlID, Direction: "outgoing",
	}); err != nil {
		t.Fatalf("HandleCallEvent(answered): %v", err)
	}

	// Never-answered control call on a different employee/target.
	cfg2 := dialConfig()
	cfg2.TimeoutSecs = 1
	target2 := allowedNumber(t, cfg2)
	reg2 := NewRegistry(4)
	mgr2 := NewManager(ManagerConfig{StreamURL: "wss://tunnel.example/media"}, tel, nil)
	d2 := NewDialer(cfg2, tel, mgr2, reg2, newTestLedger(t), &captureAudit{}, testLogger())
	res2, err := d2.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-2", Target: target2, RequestID: "req-h1-2",
	})
	if err != nil {
		t.Fatalf("Dial(control call): %v", err)
	}

	// The answered call must STILL be live after 2x the timer deadline.
	time.Sleep(2200 * time.Millisecond)
	live := 0
	for _, inst := range reg.Live() {
		if inst.CallControlID == res.CallControlID {
			live++
		}
	}
	if live != 1 {
		t.Errorf("answered call released past its timer deadline (H1: late no-answer landed on a live call)")
	}
	select {
	case <-res.Done:
		t.Errorf("answered call's Done channel closed, want still open (no-answer fired on answered call)")
	default:
	}

	// The control call must have timed out and released.
	select {
	case <-res2.Done:
		if _, err := reg2.Acquire("cc-probe-h1", "emp-9", target2); err != nil {
			t.Errorf("control call target not released after no-answer: %v", err)
		} else {
			reg2.Release("cc-probe-h1")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("control call never timed out")
	}
}

// allowedNumber returns the config's first allowlist entry (the tests dial
// their own allowed target; no hardcoded number is duplicated here).
func allowedNumber(t *testing.T, cfg DialConfig) string {
	t.Helper()
	if len(cfg.AllowedNumbers) == 0 {
		t.Fatalf("dial config has no allowed numbers")
	}
	return cfg.AllowedNumbers[0]
}

// bughunt 2026-11-07 voice F-2 pins: the no-answer fire must not tear down
// an ANSWERED call. Timer.Stop cannot un-arm a fire already in flight
// (Stop returns false once the value is buffered) — the answered map is the
// second cancellation channel, re-checked AT fire time. These exercise the
// guard directly (the map, not the Stop result).
func TestNoAnswerTimerRespectsAnsweredFlag(t *testing.T) {
	cfg := dialConfig()
	cfg.TimeoutSecs = 1 // fire at +1s

	// (a) UNANSWERED: the fire lands no-answer cleanup (finishCall
	// releases the registry slot). Legacy behavior, unchanged.
	reg := NewRegistry(1)
	d := NewDialer(cfg, &countingController{}, nil, reg, newTestLedger(t), &captureAudit{}, testLogger())
	res, err := d.Dial(context.Background(), DialRequest{
		EmployeeID: "emp-1", Target: cfg.AllowedNumbers[0], RequestID: "req-f2a",
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = res
	deadline := time.Now().Add(3 * time.Second)
	for len(reg.Live()) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if len(reg.Live()) != 0 {
		t.Fatalf("unanswered case: slot should release after the no-answer fire, live=%d", len(reg.Live()))
	}

	// (b) THE FIRE-IN-FLIGHT RACE, made deterministic: the fire wins the
	// select (Timer.Stop returned false / never ran), the goroutine blocks
	// on d.mu for the answered re-check, and the test lands the answer
	// marker WHILE holding that lock. The guard must then return WITHOUT
	// finishCall — the live call keeps its slot (the F-2 strand:
	// FailOutcome used to release a slot under an answered call while the
	// leg stayed up).
	cfgB := dialConfig()
	cfgB.TimeoutSecs = 1
	regB := NewRegistry(1)
	dB := NewDialer(cfgB, &countingController{}, nil, regB, newTestLedger(t), &captureAudit{}, testLogger())
	const ccid = "cc-f2b"
	if _, err := regB.Acquire(ccid, "emp-1", cfgB.AllowedNumbers[0]); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	dB.armNoAnswerTimer(ccid) // goroutine enters the select on the 1s timer

	dB.mu.Lock()
	dB.answered[ccid] = true            // the answer lands...
	time.Sleep(1400 * time.Millisecond) // ...the timer fires (value buffered), goroutine blocks on d.mu
	dB.mu.Unlock()                      // releases it onto the answered re-check

	// The guard path returns WITHOUT finishCall; a mutation (no guard)
	// releases the slot via finishCall inside the next ~100 ms.
	time.Sleep(300 * time.Millisecond)
	if len(regB.Live()) != 1 {
		t.Fatalf("answered call lost its slot to the no-answer fire (F-2): live=%d, want 1", len(regB.Live()))
	}
}

// (c) The answer webhook can beat DialOut's arm site: NotifyAnswered marks
// the ccid before armNoAnswerTimer runs, so arming is skipped entirely and
// no timer exists for the call.
func TestArmSkippedForAlreadyAnswered(t *testing.T) {
	cfg := dialConfig()
	cfg.TimeoutSecs = 1
	d := NewDialer(cfg, &countingController{}, nil, NewRegistry(4), newTestLedger(t), &captureAudit{}, testLogger())
	d.mu.Lock()
	d.answered["cc-early"] = true
	d.mu.Unlock()

	d.armNoAnswerTimer("cc-early")

	d.mu.Lock()
	timer := d.timers["cc-early"]
	d.mu.Unlock()
	if timer != nil {
		timer.Stop()
		t.Fatal("timer armed for an already-answered ccid (F-2: webhook can beat the arm site)")
	}
}
