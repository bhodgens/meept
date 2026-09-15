package metrics

import (
	"fmt"
	"log/slog"
	"math/rand"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// burstTestDetector returns an enabled detector with calibrated defaults
// and a discarded logger.
func burstTestDetector() *BurstDetector {
	return NewBurstDetector(BurstDetectionConfig{Enabled: true}, slog.New(slog.DiscardHandler))
}

// burstKnownGoodStream draws a stream with no hard failures and a
// corrected mix of pc (deterministic per seed).
func burstKnownGoodStream(rng *rand.Rand, pc float64, turns int) []string {
	out := make([]string, turns)
	for i := range out {
		if rng.Float64() < pc {
			out[i] = "corrected"
		} else {
			out[i] = "ok"
		}
	}
	return out
}

// burstDegradedStream draws a stream whose hard-failure probability
// ramps linearly from p0 (pre-degradation background) at turn cut+1 to
// 1.0 by turn rampEnd, then stays 1.0. Non-failed turns during
// degradation carry a corrected background (pc) so the stream resembles
// a session that is partly re-routed while failing. mode "abrupt" jumps
// straight to 1.0 after turn cut instead (pc ignored — every
// post-cut turn fails).
func burstDegradedStream(rng *rand.Rand, mode string, turns, rampEnd, cut int, p0, pc float64) []string {
	out := make([]string, turns)
	for t := 1; t <= turns; t++ {
		var p float64
		switch mode {
		case "gradual":
			if t <= cut {
				p = p0
			} else {
				p = p0 + (1.0-p0)*float64(t-cut)/float64(rampEnd-cut)
			}
		case "abrupt":
			if t <= cut {
				p = p0
			} else {
				p = 1.0
			}
		}
		u := rng.Float64()
		switch {
		case u < p:
			out[t-1] = "failed_replan"
		case u < p+(1.0-p)*pc:
			out[t-1] = "corrected"
		default:
			out[t-1] = "ok"
		}
	}
	return out
}

// burstFirstCrossing replays a stream through a fresh session and
// returns the 1-based turn on which each detector first fired (0 = never
// fired within the stream).
func burstFirstCrossing(d *BurstDetector, sessionID string, stream []string) (ewmaTurn, ruleTurn int) {
	for t, outcome := range stream {
		burst, _ := d.ObserveOutcome(sessionID, outcome)
		if ewmaTurn == 0 && burst {
			ewmaTurn = t + 1
		}
		if ruleTurn == 0 && d.ThresholdRule(sessionID, 3) {
			ruleTurn = t + 1
		}
		if ewmaTurn != 0 && ruleTurn != 0 {
			break
		}
	}
	return ewmaTurn, ruleTurn
}

// TestBurstKnownGoodStreams: acceptance criterion 1 — fewer than 5% of
// known-good streams (ok + corrected mix, no failures) may flag. Runs
// 500 synthetic sessions across three corrected mixes; with the
// calibrated defaults the measured rate is 0.
func TestBurstKnownGoodStreams(t *testing.T) {
	mixes := []float64{0.0, 0.15, 0.30}
	const sessionsPerMix = 500
	const turns = 120
	seed := int64(43)

	flagged := 0
	total := 0
	for _, pc := range mixes {
		for range sessionsPerMix {
			seed++
			//nolint:gosec // deterministic synthetic outcome streams, not crypto
			rng := rand.New(rand.NewSource(seed))
			d := burstTestDetector()
			stream := burstKnownGoodStream(rng, pc, turns)
			fired := false
			for _, o := range stream {
				if b, _ := d.ObserveOutcome("s-good", o); b {
					fired = true
					break
				}
			}
			if fired {
				flagged++
			}
			total++
		}
	}
	rate := float64(flagged) / float64(total)
	if rate >= 0.05 {
		t.Errorf("known-good flag rate %.4f (%d/%d) >= 5%% acceptance bound", rate, flagged, total)
	}
	t.Logf("known-good flag rate: %d/%d = %.4f", flagged, total, rate)
}

// TestBurstLatencyVsThresholdRule: acceptance criterion 2 — on a
// gradual-degradation stream the EWMA burst detector fires EARLIER than
// the plain 3-consecutive-failures threshold rule; on an abrupt burst
// both latencies are reported honestly (the threshold rule wins there —
// a pure all-fail step is exactly the pattern it was designed for).
// Uses 50 seeded streams per mode and asserts the median gradual
// latency advantage.
func TestBurstLatencyVsThresholdRule(t *testing.T) {
	const streams = 200
	const turns = 200
	const rampEnd = 160 // gradual: hard-failure prob -> 1.0 by turn 160
	const cut = 40      // degradation starts here in both modes
	const corrBG = 0.10 // corrected background during gradual degradation

	type latencies struct {
		ewma, rule int // turn of first fire, 0 = never
	}
	run := func(mode string) []latencies {
		out := make([]latencies, 0, streams)
		for i := range streams {
			//nolint:gosec // deterministic seeded streams for reproducible latency stats, not crypto
			rng := rand.New(rand.NewSource(int64(i + 1)))
			stream := burstDegradedStream(rng, mode, turns, rampEnd, cut, 0.02, corrBG)
			d := burstTestDetector()
			e, r := burstFirstCrossing(d, "s-lat", stream)
			out = append(out, latencies{ewma: e, rule: r})
		}
		return out
	}

	// Gradual: EWMA must fire first on a clear majority of streams and
	// by a material median margin.
	grad := run("gradual")
	ewmaFirst := 0
	diffs := make([]int, 0, streams)
	for _, l := range grad {
		if l.ewma != 0 && l.rule != 0 {
			if l.ewma < l.rule {
				ewmaFirst++
			}
			diffs = append(diffs, l.rule-l.ewma)
		}
	}
	if len(diffs) == 0 {
		t.Fatal("no comparable gradual streams (both detectors silent)")
	}
	medianDiff := diffs[len(diffs)/2]
	p10 := diffs[len(diffs)/10]
	t.Logf("gradual: EWMA first on %d/%d comparable streams, median latency advantage %d turns, 10th-pct %d turns (positive = EWMA earlier)",
		ewmaFirst, len(diffs), medianDiff, p10)
	if ewmaFirst <= 19*len(diffs)/20 {
		t.Errorf("EWMA detector fired first on only %d/%d gradual streams (want >95%%)", ewmaFirst, len(diffs))
	}
	if medianDiff < 10 {
		t.Errorf("median gradual latency advantage %d turns (want >= 10)", medianDiff)
	}

	// Abrupt: report both medians honestly; no dominance asserted.
	ab := run("abrupt")
	abE, abR := 0, 0
	abCount := 0
	for _, l := range ab {
		if l.ewma != 0 {
			abE += l.ewma
			abCount++
		}
		if l.rule != 0 {
			abR += l.rule
		}
	}
	if abCount == 0 {
		t.Fatal("abrupt streams never fired the EWMA detector")
	}
	t.Logf("abrupt: mean first-fire turn over %d streams — burst detector %.1f, threshold rule %.1f (threshold rule expected faster here)",
		abCount, float64(abE)/float64(abCount), float64(abR)/float64(abCount))

	// Both detectors must eventually fire on every abrupt stream.
	for i, l := range ab {
		if l.ewma == 0 || l.rule == 0 {
			t.Errorf("abrupt stream %d: ewmaTurn=%d ruleTurn=%d (both must fire)", i, l.ewma, l.rule)
		}
	}
}

// TestBurstWarmupNoSignalBeforeWindowFill: no burst may fire before the
// window has filled, even for an all-failure stream (the worst case).
func TestBurstWarmupNoSignalBeforeWindowFill(t *testing.T) {
	d := burstTestDetector()
	for turn := range d.window - 1 {
		burst, _ := d.ObserveOutcome("s-warm", "failed_replan")
		if burst {
			t.Fatalf("burst fired at observation %d, before window fill (%d)", turn+1, d.window)
		}
	}
	if b, _ := d.ObserveOutcome("s-warm", "failed_replan"); !b {
		t.Error("all-failure stream did not fire at window fill")
	}
}

// TestBurstThresholdRuleBasics: the rule fires exactly when the last n
// window entries are all failed_replan; n<=0 defaults to 3.
func TestBurstThresholdRuleBasics(t *testing.T) {
	tests := []struct {
		name    string
		stream  []string
		n       int
		wantAny bool
	}{
		{name: "three consecutive failures fire", stream: []string{"ok", "ok", "failed_replan", "failed_replan", "failed_replan"}, n: 3, wantAny: true},
		{name: "two failures do not fire (n=3)", stream: []string{"failed_replan", "failed_replan"}, n: 3, wantAny: false},
		{name: "ok breaks the run", stream: []string{"failed_replan", "failed_replan", "ok", "failed_replan", "failed_replan", "failed_replan"}, n: 3, wantAny: true},
		{name: "corrected is not a failure", stream: []string{"corrected", "corrected", "corrected"}, n: 3, wantAny: false},
		{name: "n=2 fires on two", stream: []string{"failed_replan", "failed_replan"}, n: 2, wantAny: true},
		{name: "default n (0) behaves as 3", stream: []string{"failed_replan", "failed_replan"}, n: 0, wantAny: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := burstTestDetector()
			fired := false
			for _, o := range tc.stream {
				d.ObserveOutcome("s-rule", o)
				if d.ThresholdRule("s-rule", tc.n) {
					fired = true
				}
			}
			if fired != tc.wantAny {
				t.Errorf("ThresholdRule fired=%v, want %v", fired, tc.wantAny)
			}
		})
	}
}

// TestBurstConcurrentObserveOutcome mirrors embed_health_test.go's
// TestConcurrentCheckSafe: ObserveOutcome + ThresholdRule from 10
// goroutines under -race must be clean.
func TestBurstConcurrentObserveOutcome(t *testing.T) {
	d := burstTestDetector()
	outcomes := []string{"ok", "corrected", "failed_replan"}

	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for g := range 10 {
		wg.Go(func() {
			//nolint:gosec // per-goroutine seeded draws for -race coverage, not crypto
			rng := rand.New(rand.NewSource(int64(g)))
			for range 200 {
				o := outcomes[rng.Intn(len(outcomes))]
				d.ObserveOutcome(fmt.Sprintf("s-%d", g), o)
				d.ThresholdRule(fmt.Sprintf("s-%d", g), 3)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestBurstSessionCapEvictionAndReset: hitting the session cap evicts
// the least-recently-touched session; ResetSession drops one session's
// state without touching others.
func TestBurstSessionCapEvictionAndReset(t *testing.T) {
	d := burstTestDetector()
	d.maxSessions = 3

	// Fill sessions a, b, c.
	for _, id := range []string{"a", "b", "c"} {
		d.ObserveOutcome(id, "ok")
	}
	// Touch a so b becomes least-recently-touched.
	d.ObserveOutcome("a", "failed_replan")
	// Add d -> must evict b, not a.
	d.ObserveOutcome("d", "ok")
	if _, ok := d.sessions["b"]; ok {
		t.Error("session b should have been evicted (least-recently-touched)")
	}
	for _, id := range []string{"a", "d"} {
		if _, ok := d.sessions[id]; !ok {
			t.Errorf("session %s should still be tracked", id)
		}
	}

	// ResetSession drops exactly one session.
	d.ResetSession("b")
	if _, ok := d.sessions["b"]; ok {
		t.Error("session b should be gone after ResetSession")
	}
	if _, ok := d.sessions["a"]; !ok {
		t.Error("ResetSession must not touch session a")
	}
	// Reset of an unknown session is a no-op (no panic).
	d.ResetSession("nope")
}

// TestBurstInertWhenDisabled: cfg.Enabled=false leaves every entry
// point inert.
func TestBurstInertWhenDisabled(t *testing.T) {
	d := NewBurstDetector(BurstDetectionConfig{Enabled: false}, slog.New(slog.DiscardHandler))
	if d.Enabled() {
		t.Fatal("detector reports enabled with cfg.Enabled=false")
	}
	for i := range 25 {
		if b, score := d.ObserveOutcome("s-off", "failed_replan"); b || score != 0 {
			t.Fatalf("disabled detector signaled at iteration %d: burst=%v score=%v", i+1, b, score)
		}
	}
	if d.ThresholdRule("s-off", 3) {
		t.Error("disabled ThresholdRule fired")
	}
}

// TestBurstWindowFloorAndThresholdOverride: WindowSize floors at 5;
// Threshold override is honored.
func TestBurstWindowFloorAndThresholdOverride(t *testing.T) {
	d := NewBurstDetector(BurstDetectionConfig{Enabled: true, WindowSize: 2, Threshold: 0.9}, slog.New(slog.DiscardHandler))
	if d.window != burstMinWindow {
		t.Errorf("window = %d, want floor %d", d.window, burstMinWindow)
	}
	if d.thr != 0.9 {
		t.Errorf("threshold = %v, want override 0.9", d.thr)
	}
}

// TestLoadSessionOutcomes reconstructs the per-session outcome sequence
// from a real store: RecordDispatch + ResolvePendingOutcome +
// MarkTaskFailedReplan, then assert order and content.
func TestLoadSessionOutcomes(t *testing.T) {
	store, err := NewStore(&StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour, // disable background flush; RecordDispatch writes directly
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Turn 1: analyst dispatch, later corrected to reviewer.
	// Turn 2: debugger dispatch, resolves ok.
	// Turn 3: coder dispatch on task-9, marked failed_replan.
	// A second session's rows must not leak in.
	store.RecordDispatch(DispatchEntry{SessionID: "sess-load", TurnNo: 1, AgentID: "analyst", ClassifierMethod: "llm", Outcome: "pending"})
	store.RecordDispatch(DispatchEntry{SessionID: "other", TurnNo: 1, AgentID: "debugger", ClassifierMethod: "llm", Outcome: "pending"})
	store.RecordDispatch(DispatchEntry{SessionID: "sess-load", TurnNo: 2, AgentID: "debugger", ClassifierMethod: "llm", Outcome: "pending"})
	store.RecordDispatch(DispatchEntry{SessionID: "sess-load", TurnNo: 3, AgentID: "coder", ClassifierMethod: "llm", TaskID: "task-9", Outcome: "pending"})

	if _, err := store.ResolvePendingOutcome("sess-load", 2, "reviewer", 5); err != nil {
		t.Fatalf("ResolvePendingOutcome turn 2: %v", err)
	}
	if _, err := store.ResolvePendingOutcome("sess-load", 3, "debugger", 5); err != nil {
		t.Fatalf("ResolvePendingOutcome turn 3: %v", err)
	}
	if err := store.MarkTaskFailedReplan("task-9"); err != nil {
		t.Fatalf("MarkTaskFailedReplan: %v", err)
	}

	got, err := store.LoadSessionOutcomes("sess-load", 10)
	if err != nil {
		t.Fatalf("LoadSessionOutcomes: %v", err)
	}
	want := []ResolvedOutcome{
		{TurnNo: 1, Outcome: "corrected", CorrectedAgent: "reviewer"},
		{TurnNo: 2, Outcome: "ok"},
		{TurnNo: 3, Outcome: "failed_replan"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d outcomes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}

	// limit applies: most recent 2 rows, ascending order preserved.
	got, err = store.LoadSessionOutcomes("sess-load", 2)
	if err != nil {
		t.Fatalf("LoadSessionOutcomes limit 2: %v", err)
	}
	if len(got) != 2 || got[0].TurnNo != 2 || got[1].TurnNo != 3 {
		t.Errorf("limit 2: got %+v, want turns [2 3] ascending", got)
	}

	// Unknown session: empty slice, no error.
	got, err = store.LoadSessionOutcomes("missing", 5)
	if err != nil {
		t.Fatalf("LoadSessionOutcomes unknown session: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("unknown session: got %+v, want empty", got)
	}
}

// TestBurstReplayFromStore: end-to-end — LoadSessionOutcomes output
// replays through ObserveOutcome and the failure burst fires.
func TestBurstReplayFromStore(t *testing.T) {
	store, err := NewStore(&StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err := err; err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// 5 clean turns, then enough failed turns to fill the window and
	// fire (each failed turn its own task so the failed_replan marks
	// stay independent).
	const clean = 5
	const total = 25 // clean 5 + 20 failed: window is 20, so the burst fires inside the failed run
	for turn := 1; turn <= total; turn++ {
		taskID := fmt.Sprintf("task-%s-%d", map[bool]string{true: "ok", false: "fail"}[turn <= clean], turn)
		store.RecordDispatch(DispatchEntry{SessionID: "sess-replay", TurnNo: turn, AgentID: "coder", ClassifierMethod: "llm", TaskID: taskID, Outcome: "pending"})
		if turn <= clean {
			if _, err := store.ResolvePendingOutcome("sess-replay", turn+1, "coder", 5); err != nil {
				t.Fatalf("resolve turn %d: %v", turn, err)
			}
		} else if err := store.MarkTaskFailedReplan(taskID); err != nil {
			t.Fatalf("mark failed turn %d: %v", turn, err)
		}
	}

	// Full-history load (limit 25): clean turns resolve 'ok' (same
	// agent re-dispatch), the failed run starts at turn clean+1.
	full, err := store.LoadSessionOutcomes("sess-replay", total)
	if err != nil {
		t.Fatalf("LoadSessionOutcomes full: %v", err)
	}
	if len(full) != total || full[0].Outcome != "ok" || full[0].TurnNo != 1 ||
		full[clean-1].TurnNo != clean || full[clean].Outcome != "failed_replan" {
		t.Fatalf("unexpected full reconstruction: head %+v, turn %d %+v, turn %d %+v",
			full[0], clean, full[clean-1], clean+1, full[clean])
	}

	// Windowed load: most recent 20 rows (turns 6..25, all failed_replan).
	outcomes, err := store.LoadSessionOutcomes("sess-replay", 20)
	if err != nil {
		t.Fatalf("LoadSessionOutcomes: %v", err)
	}
	if len(outcomes) != 20 {
		t.Fatalf("got %d outcomes, want 20", len(outcomes))
	}
	if outcomes[0].TurnNo != clean+1 || outcomes[19].TurnNo != total {
		t.Fatalf("windowed reconstruction bounds: first %+v last %+v, want turns %d..%d",
			outcomes[0], outcomes[19], clean+1, total)
	}

	d := burstTestDetector()
	firedAt := 0
	for i, o := range outcomes {
		if b, _ := d.ObserveOutcome("sess-replay", o.Outcome); b && firedAt == 0 {
			firedAt = i + 1
		}
	}
	if firedAt == 0 {
		t.Error("replayed failure burst never fired")
	} else {
		t.Logf("replayed burst fired at replayed turn %d", firedAt)
	}
}
