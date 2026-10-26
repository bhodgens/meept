// Gate tests for the llama-hang-watchdog tree (docs/plans/llama-hang-watchdog),
// leaf 00T — written BEFORE the implementation exists. This file must fail to
// compile naming ONLY the new contract symbols (HealthChecker.RunTicks,
// RuntimeManager.watchdogTickOnce). Leaf 02 (self-watchdog) makes these rows
// GREEN.
//
// WHITEBOX SEAM (named for leaf 02 per Contract 4): the manager's
// single-sample watchdog logic must be exposed as the unexported method
//
//	func (m *RuntimeManager) watchdogTickOnce(now time.Time)
//
// — one watchdog-ticker firing: sample every live checker's RunTicks(), and
// on no-advance-across-2-consecutive-samples with a live process and a
// non-Stopped checker, log ERROR "health run stalled" and re-arm via
// hc.Start(context.Background()). This test calls that seam directly instead
// of waiting on the 60s production ticker.
package llm

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// wdogLogCapture is a minimal slog.Handler recording rendered records for the
// stalled-run assertion (the external-test capturingHandler in
// runtime_manager_test.go is unreachable from package llm; leaf 02 may reuse
// this fixture).
type wdogLogCapture struct {
	mu      sync.Mutex
	records []string
}

func (h *wdogLogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *wdogLogCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(r.Level.String())
	sb.WriteString(" ")
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(a.Value.String())
		return true
	})
	h.records = append(h.records, sb.String())
	return nil
}

func (h *wdogLogCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *wdogLogCapture) WithGroup(string) slog.Handler      { return h }

func (h *wdogLogCapture) contains(substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, rec := range h.records {
		if strings.Contains(rec, substr) {
			return true
		}
	}
	return false
}

func (h *wdogLogCapture) hasLevel(substr string, level slog.Level) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	prefix := level.String() + " "
	for _, rec := range h.records {
		if strings.HasPrefix(rec, prefix) && strings.Contains(rec, substr) {
			return true
		}
	}
	return false
}

// TestRunTicksAdvance — gate matrix F8: RunTicks() is the checker-liveness
// counter (Contract 4) — incremented once per checkOnce entry. A started
// checker against a live endpoint advances the counter monotonically; two
// snapshots straddling the run must strictly increase.
func TestRunTicksAdvance(t *testing.T) {
	srv := newCanaryFixture(t).srv // live /health + /v1 mux; canary stays disabled here

	cfg := &RuntimeConfig{
		HealthEndpoint:  "/health",
		HealthInterval:  5 * time.Millisecond,
		HealthTimeout:   100 * time.Millisecond,
		HealthThreshold: 3,
	}
	hc := NewHealthChecker(cfg, srv.URL)
	hc.Start(context.Background())
	defer hc.Stop()

	first := hc.RunTicks()
	if !gateWaitFor(500*time.Millisecond, func() bool { return hc.RunTicks() > first }) {
		t.Fatalf("RunTicks did not advance from %d within 500ms of a live run", first)
	}
	second := hc.RunTicks()
	if second <= first {
		t.Fatalf("RunTicks snapshot compare: first=%d second=%d, want strictly increasing", first, second)
	}
	if !gateWaitFor(500*time.Millisecond, func() bool { return hc.RunTicks() > second }) {
		t.Fatalf("RunTicks stalled at %d across a second snapshot window", second)
	}
}

// TestWatchdogRearmsStalledRun — gate matrix F8: the self-watchdog closes the
// silent-checker-death class. WHITEBOX (the one whitebox row the 00T brief
// explicitly documents): start a checker, then simulate a stalled run by
// Stop()ping it (running=false) while its process-alive probe still reports a
// live runtime; drive the manager's watchdogTickOnce seam directly and assert
// (a) an ERROR log containing "health run stalled", (b) the checker is running
// again, (c) RunTicks resumes advancing.
func TestWatchdogRearmsStalledRun(t *testing.T) {
	fx := newCanaryFixture(t)

	logs := &wdogLogCapture{}
	mgr := NewRuntimeManager(slog.New(logs))

	cfg := &RuntimeConfig{
		Type:            RuntimeLlamaCpp,
		EndpointKey:     "llama-cpp:127.0.0.1:18080",
		HealthEndpoint:  "/health",
		HealthInterval:  5 * time.Millisecond,
		HealthTimeout:   100 * time.Millisecond,
		HealthThreshold: 3,
	}
	// Register through the real seam so the manager owns the endpoint entry
	// the watchdog samples (RegisterConfig builds proc + hc and binds the
	// process-alive probe).
	if err := mgr.RegisterConfig("watchdog-test", cfg, fx.srv.URL); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}
	hc, ok := mgr.GetHealthChecker("watchdog-test")
	if !ok || hc == nil {
		t.Fatal("checker not registered with the manager")
	}
	// The endpoint's spawned process never started here (no spawn in tests),
	// so pin the probe live: the watchdog's "process alive" precondition must
	// hold for the stall to count.
	hc.SetProcessAliveProbe(func() bool { return true })

	hc.Start(context.Background())
	if !gateWaitFor(500*time.Millisecond, func() bool { return hc.RunTicks() > 0 }) {
		t.Fatalf("checker never ticked (RunTicks=%d)", hc.RunTicks())
	}

	// Simulate the silent death: the run goroutine is gone (running=false)
	// while the process it watched is still alive.
	hc.Stop()
	stalled := hc.RunTicks()

	// Contract 4: no advance across TWO consecutive samples triggers re-arm.
	// Drive the unexported seam directly (see file header).
	now := time.Now()
	mgr.watchdogTickOnce(now)
	mgr.watchdogTickOnce(now.Add(60 * time.Second))

	if !logs.hasLevel("health run stalled", slog.LevelError) {
		t.Errorf("no ERROR log containing \"health run stalled\"; captured: %v", logs.records)
	}

	hc.mu.RLock()
	running := hc.running
	hc.mu.RUnlock()
	if !running {
		t.Fatal("watchdog did not re-arm the stalled checker (running=false after watchdogTickOnce)")
	}
	defer hc.Stop()

	if !gateWaitFor(500*time.Millisecond, func() bool { return hc.RunTicks() > stalled }) {
		t.Fatalf("RunTicks did not resume advancing after re-arm (stuck at %d)", stalled)
	}
}
