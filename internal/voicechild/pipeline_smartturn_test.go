package voicechild

// Gate tests (foundation 00T, feature F1, contracts C1/C3 + 02-smart-turn.md):
// Smart Turn mode behavior at the child's local-leg analyzer seam.
//
// Locked decisions: SmartTurnMode int -- 0=default current behavior,
// 1=kill-switch (installs NO analyzer), 2=consume AnalyzeEndOfTurn verdict.
//
// RED style (compile-safe): the mode selector does not exist on the keys
// decode seam yet, and AnalyzeEndOfTurn has NO production caller today
// (pipeline.go:220-223). Both facts are probed through reflection on
// existing symbols + an injected recording TurnAnalyzer, so this package
// keeps compiling and the suite is RED for genuine missing-behavior
// reasons (not syntax errors). No t.Skip, no direct reference to a
// not-yet-existing field.
//
// Clause ids: C1 (master.md), F1 (findings index), 02-smart-turn.md T1/T2/T3.

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

// trackingTurn is a scripted TurnAnalyzer injected through SetTurnFactory.
// It records every seam call so the tests can assert whether the kill switch
// prevented construction/feeding and whether the verdict is consumed.
type trackingTurn struct {
	mu           sync.Mutex
	appendCalls  int
	analyzeCalls int
	clearCalls   int
	closed       bool
	complete     bool
	prob         float64
	verdictErr   error
}

func (a *trackingTurn) AppendAudio(_ []byte, _ bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.appendCalls++
}

func (a *trackingTurn) SetSampleRate(_ int)       {}
func (a *trackingTurn) SetVADStartSecs(_ float64) {}
func (a *trackingTurn) AnalyzeEndOfTurn() (bool, float64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.analyzeCalls++
	return a.complete, a.prob, a.verdictErr
}
func (a *trackingTurn) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clearCalls++
}
func (a *trackingTurn) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}
func (a *trackingTurn) counts() (appends, analyzes, clears int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.appendCalls, a.analyzeCalls, a.clearCalls
}

// requireModeSelector fails RED naming the missing C1 symbol before the
// behavior assertions run. Reuses the reflection gate from file 1 (same
// package) so the RED reason is "field does not exist", not a syntax error.
func requireModeSelector(t *testing.T) {
	t.Helper()
	requireIntContract(t, reflect.TypeFor[keysWire](), "keysWire", true)
	requireIntContract(t, reflect.TypeFor[KeysFrame](), "KeysFrame", false)
}

// smartTurnFixture drives a real local call through open + keys with the
// given mode selector on BOTH frames (the wiring seam is the impl leaf's
// choice; carrying it on either frame is enough). It injects tf as the turn
// factory and stt as the transcript source so finals can be emitted.
func smartTurnFixture(t *testing.T, ccid string, mode int, tf TurnFactory) (*fakeDaemon, *pipeline, *recordingSTT) {
	t.Helper()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	child := newTestChild(t, d, "tok-"+ccid)
	if tf != nil {
		child.SetTurnFactory(tf)
	}
	stt := newRecordingSTT()
	child.SetProviderFactories(
		func(ProviderConfig, string) (STTProvider, error) { return stt, nil },
		func(ProviderConfig, string) (TTSProvider, error) { return stubTTS{}, nil },
	)
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()

	// Raw open frame: Local=true is what arms analyzer init today; the
	// extra selector is ignored by the current decoder (RED) and consumed
	// once the contract lands (GREEN).
	sendRaw(t, childConn, fmt.Sprintf(
		`{"call_id":%q,"event":"open","codec":"L16","sample_rate":16000,"direction":"local","local":true,"smart_turn_mode":%d}`,
		ccid, mode))
	in, out := d.acceptAll(ccid)
	t.Cleanup(func() { _ = in.Close(); _ = out.Close() })

	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, ok := child.getPipeline(ccid)
	if !ok {
		t.Fatalf("pipeline %s missing after open", ccid)
	}

	// Keys frame carries the selector plus a short confirm window so the
	// verdict path fires quickly once implemented.
	sendRaw(t, childConn, fmt.Sprintf(
		`{"call_id":%q,"event":"keys","stt_key":"s","tts_key":"t","turn_confirm_ms":60,"smart_turn_mode":%d}`,
		ccid, mode))
	t.Cleanup(func() {
		if err := childConn.Close(); err != nil {
			t.Errorf("close child conn: %v", err)
		}
		if err := <-runDone; err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	})
	waitFor(t, func() bool { return stt.streamCalls() == 1 })
	return d, p, stt
}

// TestSmartTurnModes is the gate named by the leaf's verify command.
// Subtests pin mode 0 (byte-identical), mode 1 (kill switch), mode 2
// (verdict consumed at confirm).
func TestSmartTurnModes(t *testing.T) {
	requireModeSelector(t)

	t.Run("mode0_installs_and_feeds_analyzer", func(t *testing.T) {
		tr := &trackingTurn{complete: true, prob: 0.95}
		built := 0
		var mu sync.Mutex
		tf := func(int) (TurnAnalyzer, error) {
			mu.Lock()
			built++
			mu.Unlock()
			return tr, nil
		}
		_, p, _ := smartTurnFixture(t, "st-mode0", 0, tf)
		waitFor(t, func() bool {
			p.turnMu.Lock()
			installed := p.turn != nil
			p.turnMu.Unlock()
			return installed
		})
		mu.Lock()
		n := built
		mu.Unlock()
		if n != 1 {
			t.Errorf("mode 0 built the turn analyzer %d times, want 1 (byte-identical current behavior)", n)
		}
	})

	t.Run("mode1_kill_switch_installs_no_analyzer", func(t *testing.T) {
		tr := &trackingTurn{complete: true, prob: 0.99}
		built := 0
		var mu sync.Mutex
		tf := func(int) (TurnAnalyzer, error) {
			mu.Lock()
			built++
			mu.Unlock()
			return tr, nil
		}
		_, p, _ := smartTurnFixture(t, "st-mode1", 1, tf)

		// Wait for the async analyzer-init barrier, then a short grace so a
		// late (wrong) install would be observed.
		p.analyzerInit.Wait()
		time.Sleep(80 * time.Millisecond)

		mu.Lock()
		n := built
		mu.Unlock()
		if n != 0 {
			t.Errorf("mode 1 kill switch built the turn analyzer %d time(s), want 0 (no analyzer installed)", n)
		}
		p.turnMu.Lock()
		installed := p.turn
		p.turnMu.Unlock()
		if installed != nil {
			t.Errorf("mode 1 kill switch left a turn analyzer installed (%T), want nil", installed)
		}
		if app, _, _ := tr.counts(); app != 0 {
			t.Errorf("mode 1 kill switch fed the analyzer %d chunk(s), want 0", app)
		}
	})

	t.Run("mode2_consumes_verdict_at_confirm", func(t *testing.T) {
		// complete=true: AnalyzeEndOfTurn is consulted at confirm-window
		// expiry and a prompt is dispatched (dead seam closed).
		tr := &trackingTurn{complete: true, prob: 0.8}
		tf := func(int) (TurnAnalyzer, error) { return tr, nil }
		d, p, stt := smartTurnFixture(t, "st-mode2", 2, tf)
		waitFor(t, func() bool {
			p.turnMu.Lock()
			installed := p.turn != nil
			p.turnMu.Unlock()
			return installed
		})
		stt.emit(Transcript{Text: "are you done yet", Final: true})

		// The decisive assertion: today AnalyzeEndOfTurn has no production
		// caller, so this wait times out -> RED. Once mode 2 consults the
		// verdict at confirm, it succeeds -> GREEN.
		waitFor(t, func() bool {
			_, analyzes, _ := tr.counts()
			return analyzes > 0
		})

		// complete=true permits dispatch; a prompt must reach the daemon.
		waitFor(t, func() bool { return countQueuedPrompts(d) > 0 })
	})
}

// TestSmartTurnVerdictDefersDispatch is the mode-2 half of the exposed
// contract: complete=false defers by one bounded re-arm (never an infinite
// hold) and the verdict is still consulted. Timing bounds are generous to
// avoid load flake; the point is that the verdict lane is live.
func TestSmartTurnVerdictDefersDispatch(t *testing.T) {
	requireModeSelector(t)

	tr := &trackingTurn{complete: false, prob: 0.2}
	tf := func(int) (TurnAnalyzer, error) { return tr, nil }
	d, p, stt := smartTurnFixture(t, "st-defer", 2, tf)
	waitFor(t, func() bool {
		p.turnMu.Lock()
		installed := p.turn != nil
		p.turnMu.Unlock()
		return installed
	})
	stt.emit(Transcript{Text: "still thinking about it", Final: true})

	waitFor(t, func() bool {
		_, analyzes, _ := tr.counts()
		return analyzes > 0
	})
	// Bounded deferral: the turn still dispatches within the generous window.
	waitFor(t, func() bool { return countQueuedPrompts(d) > 0 })

	_, analyzes, _ := tr.counts()
	if analyzes < 1 {
		t.Errorf("mode 2 did not consume AnalyzeEndOfTurn verdict (calls=%d)", analyzes)
	}
}
