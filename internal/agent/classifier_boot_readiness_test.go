package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// llmTestClient builds a real *llm.Client pointed at an httptest server URL
// (same shape as newClassifierJSONServer in classifier_llm_gate_test.go).
func llmTestClient(t *testing.T, baseURL string) *llm.Client {
	t.Helper()
	return llm.NewClient(&llm.ModelConfig{BaseURL: baseURL, ModelID: "boot-window-pin"})
}

// Boot-window misroute pin tests (2026-10-22 live defect): the daemon's
// listeners accept chat.submit BEFORE the F-D8 classifier boot gate resolves,
// so a classification call inside the boot window used to dial a still-
// spawning runtime, fail, arm the 60s unavailability cooldown, and silently
// degrade the turn to keyword routing (heuristic_fallback @0.55).
// ClassifierBootReadiness makes such calls wait -- bounded, cancellable, and
// NEVER failing -- for the gate outcome.

// TestClassifierBootReadiness_SignalShapes pins the one-shot signal
// contract: Wait blocks until resolved; SignalReady -> nil; SignalFailed ->
// ErrClassifierBootGateFailed; first outcome wins; nil receiver is safe.
func TestClassifierBootReadiness_SignalShapes(t *testing.T) {
	t.Run("ready releases waiter with nil", func(t *testing.T) {
		b := NewClassifierBootReadiness()
		errCh := make(chan error, 1)
		go func() { errCh <- b.Wait(context.Background()) }()
		select {
		case err := <-errCh:
			t.Fatalf("Wait returned before signal: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		b.SignalReady()
		if err := <-errCh; err != nil {
			t.Fatalf("Wait after SignalReady = %v, want nil", err)
		}
		if !b.Done() || b.Failed() {
			t.Fatal("Done/Failed wrong after SignalReady")
		}
	})

	t.Run("failed releases waiter with ErrClassifierBootGateFailed", func(t *testing.T) {
		b := NewClassifierBootReadiness()
		b.SignalFailed()
		if err := b.Wait(context.Background()); !errors.Is(err, ErrClassifierBootGateFailed) {
			t.Fatalf("Wait after SignalFailed = %v, want ErrClassifierBootGateFailed", err)
		}
		if !b.Failed() {
			t.Fatal("Failed() must report the gate outcome")
		}
	})

	t.Run("first outcome wins", func(t *testing.T) {
		b := NewClassifierBootReadiness()
		b.SignalReady()
		b.SignalFailed() // must not overwrite
		if err := b.Wait(context.Background()); err != nil {
			t.Fatalf("Wait = %v, want nil (first outcome wins)", err)
		}
	})

	t.Run("context cancellation releases the wait", func(t *testing.T) {
		b := NewClassifierBootReadiness()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		if err := b.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Wait = %v, want context.DeadlineExceeded", err)
		}
	})

	t.Run("nil receiver never blocks", func(t *testing.T) {
		var b *ClassifierBootReadiness
		if !b.Done() {
			t.Fatal("nil Done() must be true (nothing to wait for)")
		}
		if err := b.Wait(context.Background()); err != nil {
			t.Fatalf("nil Wait = %v, want nil", err)
		}
		b.SignalReady() // must not panic
		b.SignalFailed()
	})
}

// TestLLMClassifier_BootWindowWaitsForGateThenClassifies is the DEFECT pin:
// a Classify call made while the gate is still open must NOT hit the wire
// immediately (which would fail against a spawning runtime and arm the
// cooldown); it must wait for SignalReady and then classify at full fidelity
// against the now-healthy endpoint.
func TestLLMClassifier_BootWindowWaitsForGateThenClassifies(t *testing.T) {
	var wireHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		wireHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		body := `{"choices":[{"message":{"role":"assistant","content":` +
			strconv.Quote(`{"intent":"code","confidence":0.9}`) +
			`}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	gate := NewClassifierBootReadiness()
	c := NewLLMClassifier(LLMClassifierConfig{
		Client:        llmTestClient(t, srv.URL),
		Model:         "test",
		Timeout:       2 * time.Second,
		BootReadiness: gate,
		BootWait:      5 * time.Second,
	}, nil)

	intentCh := make(chan *Intent, 1)
	errCh := make(chan error, 1)
	go func() {
		intent, err := c.Classify(context.Background(), "write a go function", nil)
		errCh <- err
		intentCh <- intent
	}()

	// While the gate is open the classifier must not touch the wire -- the
	// defect shape was an immediate dial against a spawning runtime.
	select {
	case err := <-errCh:
		t.Fatalf("Classify returned during boot window (err=%v); it must wait for the gate", err)
	case <-time.After(200 * time.Millisecond):
	}
	if n := wireHits.Load(); n != 0 {
		t.Fatalf("classifier hit the wire %d time(s) while the boot gate was open", n)
	}

	// Gate resolves -> the parked call proceeds and classifies normally.
	gate.SignalReady()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Classify after gate ready = error %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Classify did not resume after SignalReady")
	}
	intent := <-intentCh
	if intent == nil || intent.Type != string(IntentCode) {
		t.Fatalf("intent = %+v, want code lane", intent)
	}
	if n := wireHits.Load(); n != 1 {
		t.Fatalf("wire hits = %d, want exactly 1 (one classify after the gate)", n)
	}
	if c.unavailable.Load() {
		t.Fatal("unavailability cooldown must NOT be armed by a boot-window wait")
	}
}

// TestLLMClassifier_GateFailureDegradesWithoutHardFail pins the never-fail
// side of the contract: when the gate trips (daemon shutting down), the
// parked call is released immediately and proceeds through the ordinary
// attempt path -- a warmup/gate failure never hard-fails a turn.
func TestLLMClassifier_GateFailureDegradesWithoutHardFail(t *testing.T) {
	// Endpoint that refuses connections, like a runtime that never came up.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	srv.Close() // closed port: dial fails immediately

	gate := NewClassifierBootReadiness()
	c := NewLLMClassifier(LLMClassifierConfig{
		Client:        llmTestClient(t, srv.URL),
		Model:         "test",
		Timeout:       time.Second,
		BootReadiness: gate,
		BootWait:      10 * time.Second,
	}, nil)

	done := make(chan error, 1)
	go func() {
		_, err := c.Classify(context.Background(), "write a go function", nil)
		done <- err
	}()

	select {
	case <-done:
		t.Fatal("Classify returned while the gate was open")
	case <-time.After(100 * time.Millisecond):
	}

	gate.SignalFailed()
	select {
	case err := <-done:
		// The ordinary attempt path may fail (dead endpoint) -- that is the
		// pre-existing degrade-to-keyword behavior the dispatcher handles.
		// The pin is that the call was RELEASED promptly (no 10s wait) and
		// the error is the ordinary classification error, not a boot-gate
		// hard failure surfaced to the turn.
		if err != nil && errors.Is(err, ErrClassifierBootGateFailed) {
			t.Fatal("boot-gate failure must not surface as the classify error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SignalFailed did not release the parked classify call")
	}
}

// TestLLMClassifier_BootWaitBoundExpires pins the latency cap: when the gate
// never resolves, the bounded wait expires and the call proceeds through the
// ordinary path instead of stalling forever.
func TestLLMClassifier_BootWaitBoundExpires(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `{"choices":[{"message":{"role":"assistant","content":` +
			strconv.Quote(`{"intent":"chat","confidence":0.9}`) +
			`}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	gate := NewClassifierBootReadiness() // never signaled
	c := NewLLMClassifier(LLMClassifierConfig{
		Client:        llmTestClient(t, srv.URL),
		Model:         "test",
		Timeout:       2 * time.Second,
		BootReadiness: gate,
		BootWait:      150 * time.Millisecond,
	}, nil)

	start := time.Now()
	intent, err := c.Classify(context.Background(), "hello there", nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Classify after boot-wait expiry = error %v, want ordinary attempt", err)
	}
	if intent == nil {
		t.Fatal("intent nil after boot-wait expiry")
	}
	if elapsed < 100*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("elapsed = %v; want ~BootWait then a normal classify", elapsed)
	}
}

// TestIntentAnalyzer_BootWindowWaitsForGate pins the analyzer twin: the
// IntentAnalyzer dials the same classifier endpoint, so its analysis call
// must honor the same boot-gate wait.
func TestIntentAnalyzer_BootWindowWaitsForGate(t *testing.T) {
	var wireHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		wireHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		content := `{"goal":"g","ambiguity":0.1,"scope":"s","category":"c","confidence":0.9}`
		body := `{"choices":[{"message":{"role":"assistant","content":` +
			strconv.Quote(content) +
			`}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	gate := NewClassifierBootReadiness()
	ia := newIntentAnalyzer(llmTestClient(t, srv.URL), 0, nil, nil)
	ia.SetBootReadiness(gate)

	done := make(chan error, 1)
	go func() {
		_, err := ia.AnalyzeTrueIntent(context.Background(), "do the thing", nil)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("AnalyzeTrueIntent returned during boot window (err=%v)", err)
	case <-time.After(150 * time.Millisecond):
	}
	if n := wireHits.Load(); n != 0 {
		t.Fatalf("analyzer hit the wire %d time(s) while the boot gate was open", n)
	}

	gate.SignalReady()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AnalyzeTrueIntent after gate ready = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AnalyzeTrueIntent did not resume after SignalReady")
	}
}

// TestSetBootReadiness_NilGuards pins the setter nil-guard rule (repo
// convention: every Set* needs a nil guard; typed-nil must never be stored).
func TestSetBootReadiness_NilGuards(t *testing.T) {
	var c *LLMClassifier
	c.SetBootReadiness(nil) // must not panic
	c.SetBootReadiness(NewClassifierBootReadiness())

	var ia *IntentAnalyzer
	ia.SetBootReadiness(nil) // must not panic
	ia.SetBootReadiness(NewClassifierBootReadiness())

	c2 := &LLMClassifier{}
	c2.SetBootReadiness(nil) // nil signal must not arm a wait
	if c2.bootReadiness != nil {
		t.Fatal("nil signal stored on classifier")
	}
	ia2 := &IntentAnalyzer{}
	ia2.SetBootReadiness(nil)
	if ia2.bootReadiness != nil {
		t.Fatal("nil signal stored on analyzer")
	}
}

// TestDispatcher_SetClassifierBootReadiness_NilGuards pins the dispatcher
// setter: nil is a no-op and it forwards to both LLM consumers.
func TestDispatcher_SetClassifierBootReadiness_NilGuards(t *testing.T) {
	var d *Dispatcher
	d.SetClassifierBootReadiness(nil) // must not panic

	d2 := &Dispatcher{}
	d2.SetClassifierBootReadiness(nil)

	gate := NewClassifierBootReadiness()
	d3 := &Dispatcher{
		llmClassifier:  &LLMClassifier{},
		intentAnalyzer: &IntentAnalyzer{},
	}
	d3.SetClassifierBootReadiness(gate)
	if d3.llmClassifier.bootReadiness != gate {
		t.Fatal("dispatcher did not forward the gate to llmClassifier")
	}
	if d3.intentAnalyzer.bootReadiness != gate {
		t.Fatal("dispatcher did not forward the gate to intentAnalyzer")
	}
}
