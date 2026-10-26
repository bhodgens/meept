// Gate tests for the llama-hang-watchdog tree (docs/plans/llama-hang-watchdog),
// leaf 00T — written BEFORE the implementation exists. This file must fail to
// compile naming ONLY the new contract symbols (Verdict / VerdictHealthy /
// VerdictUnhealthy / VerdictHung / VerdictNow / OnHealthVerdict /
// HealthVerdictCallback / RuntimeConfig.Canary* fields). Leaf 02 (HUNG
// verdict) makes these rows GREEN; leaf 01 supplies the canary lane they
// drive.
//
// The httptest fixture (canaryFixture), the tiny-cadence checker constructor
// (newCanaryChecker), and the bounded poll helper (gateWaitFor) live in
// health_checker_canary_internal_test.go (same package).
package llm

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestVerdictHungAfterThreshold — gate matrix F5: /health keeps answering
// 200 while completions HANG (blocked handler). After CanaryThreshold
// consecutive canary failures, VerdictNow() reports VerdictHung — the new
// tri-state that distinguishes "HTTP alive but inference dead" from the
// existing VerdictUnhealthy.
func TestVerdictHungAfterThreshold(t *testing.T) {
	fx := newCanaryFixture(t)
	release := make(chan struct{})
	defer close(release)
	fx.setCompletions(func(w http.ResponseWriter) {
		select {
		case <-release: // hang until the test ends
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	})
	hc := newCanaryChecker(t, fx, "ping", 2)

	hc.Start(context.Background())
	defer hc.Stop()

	if !gateWaitFor(2*time.Second, func() bool { return hc.IsHealthy() }) {
		t.Fatal("checker never became healthy with /health answering 200")
	}
	// The canary client times out (CanaryTimeout=100ms) on the hanging
	// handler, so failures accumulate; 2 threshold ticks => HUNG. The bound
	// covers arming + 2x(100ms timeout) + margin.
	if !gateWaitFor(2*time.Second, func() bool { return hc.VerdictNow() == VerdictHung }) {
		t.Fatalf("verdict = %v with healthy /health and hanging completions, want VerdictHung", hc.VerdictNow())
	}
	if !hc.IsHealthy() {
		// /health still answers 200: HUNG is its own state, not unhealthy.
		// (The bool callback fires false on the HUNG edge per F6, but the
		// verdict the manager samples here must be Hung, not Unhealthy.)
		if hc.VerdictNow() == VerdictUnhealthy {
			t.Fatal("a hanging canary with an answering /health reported VerdictUnhealthy, want VerdictHung")
		}
	}
}

// TestVerdictUnhealthyWhenHealthFails — gate matrix F5: unchanged /health
// semantics. A failing /health drives VerdictUnhealthy at the existing
// HealthThreshold (the canary lane must not alter the crash-restart path).
func TestVerdictUnhealthyWhenHealthFails(t *testing.T) {
	fx := newCanaryFixture(t)
	fx.setHealth(http.StatusInternalServerError)
	hc := newCanaryChecker(t, fx, "ping", 3)

	hc.Start(context.Background())
	defer hc.Stop()

	if !gateWaitFor(2*time.Second, func() bool { return hc.VerdictNow() == VerdictUnhealthy }) {
		t.Fatalf("verdict = %v with /health 500, want VerdictUnhealthy", hc.VerdictNow())
	}
	if hc.VerdictNow() == VerdictHung {
		t.Fatal("a failing /health reported VerdictHung; hung requires /health to ANSWER")
	}
}

// TestVerdictHealthyRecovery — gate matrix F5: from HUNG, when completions
// start answering 200+choices the next canary tick returns VerdictHealthy and
// resets the consecutive-failure counter (a later single failure must not
// instantly re-hang — recovery clears the streak).
func TestVerdictHealthyRecovery(t *testing.T) {
	fx := newCanaryFixture(t)
	hang := make(chan struct{})
	closed := make(chan struct{})
	var hangOnce, closedOnce sync.Once
	closeHang := func() { hangOnce.Do(func() { close(hang) }) }
	closeClosed := func() { closedOnce.Do(func() { close(closed) }) }
	releaseAll := func() { closeHang(); closeClosed() }
	defer releaseAll() // never leave a hanging completions handler behind on a failure path
	fx.setCompletions(func(w http.ResponseWriter) {
		select {
		case <-hang:
		case <-closed:
		}
		w.WriteHeader(http.StatusOK)
	})
	hc := newCanaryChecker(t, fx, "ping", 2)

	hc.Start(context.Background())
	defer hc.Stop()

	// Drive into HUNG with hanging completions.
	if !gateWaitFor(2*time.Second, func() bool { return hc.VerdictNow() == VerdictHung }) {
		t.Fatalf("setup failed: verdict = %v, want VerdictHung before recovery", hc.VerdictNow())
	}

	// Recover: completions answer the success shape again.
	fx.setCompletions(canaryRespondOK)
	closeHang()
	if !gateWaitFor(2*time.Second, func() bool { return hc.VerdictNow() == VerdictHealthy }) {
		t.Fatalf("verdict = %v after completions recovered, want VerdictHealthy", hc.VerdictNow())
	}
	closeClosed()

	// Failure-counter reset: flip completions to failing ONCE and assert the
	// verdict does not jump straight back to HUNG (threshold=2 requires two
	// consecutive failures after the reset). Poll just long enough for one
	// canary tick (interval 5ms + timeout 100ms) — the assertion is that HUNG
	// has NOT arrived within one tick, while the checker stays healthy.
	fx.setCompletions(func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	oneTick := time.After(150 * time.Millisecond)
	for {
		if hc.VerdictNow() == VerdictHung {
			t.Fatal("a single canary failure after recovery re-entered HUNG: the streak was not reset")
		}
		select {
		case <-oneTick:
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// verdictEvent records one callback firing.
type verdictEvent struct {
	healthy bool // bool-callback value
	verdict Verdict
	detail  string
	order   int
}

// dualRecorder captures bool-callback and verdict-callback firings in one
// ordered log (both callbacks fire from the checker's transition path, so a
// shared mutex + sequence number pins ordering).
type dualRecorder struct {
	mu     sync.Mutex
	seq    int
	bools  []bool
	events []verdictEvent
}

func (r *dualRecorder) onBool(healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	r.bools = append(r.bools, healthy)
	r.events = append(r.events, verdictEvent{healthy: healthy, order: r.seq})
}

func (r *dualRecorder) onVerdict(v Verdict, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	r.events = append(r.events, verdictEvent{verdict: v, detail: detail, order: r.seq})
}

func (r *dualRecorder) boolCalls() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]bool, len(r.bools))
	copy(out, r.bools)
	return out
}

func (r *dualRecorder) verdictEvents() []verdictEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]verdictEvent, len(r.events))
	copy(out, r.events)
	return out
}

// TestBoolCallbackUnchangedOnHung — gate matrix F6: callback compatibility
// (Contract 3). The legacy HealthChangeCallback func(bool) keeps driving
// attemptAutoRestart: on the transition INTO hung it receives false exactly
// once, and on recovery true exactly once. No duplicate or dropped bool edges.
func TestBoolCallbackUnchangedOnHung(t *testing.T) {
	fx := newCanaryFixture(t)
	hang := make(chan struct{})
	closed := make(chan struct{})
	var hangOnce, closedOnce sync.Once
	closeHang := func() { hangOnce.Do(func() { close(hang) }) }
	closeClosed := func() { closedOnce.Do(func() { close(closed) }) }
	releaseAll := func() { closeHang(); closeClosed() }
	defer releaseAll() // never leave a hanging completions handler behind on a failure path
	fx.setCompletions(func(w http.ResponseWriter) {
		select {
		case <-hang:
		case <-closed:
		}
		w.WriteHeader(http.StatusOK)
	})
	hc := newCanaryChecker(t, fx, "ping", 2)
	rec := &dualRecorder{}
	hc.OnHealthChange(rec.onBool)

	hc.Start(context.Background())
	defer hc.Stop()

	if !gateWaitFor(2*time.Second, func() bool { return hc.VerdictNow() == VerdictHung }) {
		t.Fatalf("setup failed: verdict = %v, want VerdictHung", hc.VerdictNow())
	}
	if !gateWaitFor(500*time.Millisecond, func() bool {
		bools := rec.boolCalls()
		falses := 0
		for _, b := range bools {
			if !b {
				falses++
			}
		}
		return falses == 1
	}) {
		t.Fatalf("bool callback saw %v entering HUNG, want exactly one false (restart path)", rec.boolCalls())
	}

	// Recovery: true exactly once.
	fx.setCompletions(canaryRespondOK)
	closeHang()
	if !gateWaitFor(2*time.Second, func() bool { return hc.VerdictNow() == VerdictHealthy }) {
		t.Fatalf("verdict = %v after recovery, want VerdictHealthy", hc.VerdictNow())
	}
	if !gateWaitFor(500*time.Millisecond, func() bool {
		bools := rec.boolCalls()
		trues := 0
		for _, b := range bools {
			if b {
				trues++
			}
		}
		return trues == 1
	}) {
		t.Fatalf("bool callback saw %v across hung+recovery, want [false ... true] once each", rec.boolCalls())
	}
	closeClosed()
}

// TestVerdictCallbackPayload — gate matrix F7: OnHealthVerdict (Contract 3)
// delivers (VerdictHung, non-empty detail) on the hang transition, and the
// verdict callback fires for the SAME transitions as the bool callback — no
// dropped edges (ordering is asserted via the shared recorder log).
func TestVerdictCallbackPayload(t *testing.T) {
	fx := newCanaryFixture(t)
	hang := make(chan struct{})
	closed := make(chan struct{})
	var hangOnce, closedOnce sync.Once
	closeHang := func() { hangOnce.Do(func() { close(hang) }) }
	closeClosed := func() { closedOnce.Do(func() { close(closed) }) }
	releaseAll := func() { closeHang(); closeClosed() }
	defer releaseAll() // never leave a hanging completions handler behind on a failure path
	fx.setCompletions(func(w http.ResponseWriter) {
		select {
		case <-hang:
		case <-closed:
		}
		w.WriteHeader(http.StatusOK)
	})
	hc := newCanaryChecker(t, fx, "ping", 2)
	rec := &dualRecorder{}
	hc.OnHealthChange(rec.onBool)
	hc.OnHealthVerdict(rec.onVerdict)

	hc.Start(context.Background())
	defer hc.Stop()

	if !gateWaitFor(2*time.Second, func() bool {
		for _, ev := range rec.verdictEvents() {
			if ev.detail != "" && ev.verdict == VerdictHung {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("no OnHealthVerdict(Hung, non-empty detail) arrived; events=%v", rec.verdictEvents())
	}
	closeHang()

	// Same-transition ordering: a bool false and a verdict Hung must BOTH
	// exist for the hang edge (no dropped transitions). Recover and assert
	// the healthy verdict edge fires too.
	fx.setCompletions(canaryRespondOK)
	if !gateWaitFor(2*time.Second, func() bool {
		sawHungVerdict, sawFalseBool, sawHealthyVerdict := false, false, false
		for _, ev := range rec.verdictEvents() {
			switch {
			case ev.detail != "" && ev.verdict == VerdictHung:
				sawHungVerdict = true
			case ev.detail == "" && !ev.healthy && ev.order > 0:
				sawFalseBool = true
			case ev.detail != "" && ev.verdict == VerdictHealthy:
				sawHealthyVerdict = true
			}
		}
		return sawHungVerdict && sawFalseBool && sawHealthyVerdict
	}) {
		t.Fatalf("transition edges incomplete; events=%v bools=%v", rec.verdictEvents(), rec.boolCalls())
	}
	closeClosed()

	// The verdict detail on the hung edge must be non-empty (operator-facing:
	// canary failure evidence).
	events := rec.verdictEvents()
	for _, ev := range events {
		if ev.verdict == VerdictHung && ev.detail == "" {
			t.Error("VerdictHung callback carried an empty detail")
		}
	}
}
