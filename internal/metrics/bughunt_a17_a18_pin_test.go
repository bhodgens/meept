// bughunt 2026-11-25 gaps A1-7 / A1-8 pin tests.
package metrics

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"
)

// A1-7: the sample threshold must be the EXACT uniform cut v < rate*2^64,
// not the old rate/4 math (threshold = rate*2^62). The old code kept only a
// quarter of the configured fraction; these boundary cases pin the fixed
// arithmetic to one bit: the trace just below the cut is kept, the trace at
// the cut is dropped, for several rates.
func TestSampleTraceThresholdIsExactUniformCut(t *testing.T) {
	cases := []struct {
		rate float64
	}{
		{0.25}, {0.5}, {0.125}, {0.01},
	}
	for _, tc := range cases {
		// Exact expected threshold under the fixed math: float64(MaxUint64)
		// ~ 2^64, so threshold == uint64(rate * 2^64).
		threshold := uint64(tc.rate * float64(math.MaxUint64))
		// Sanity: the OLD (broken) threshold was rate*2^62 = threshold/4.
		oldThreshold := uint64(tc.rate * float64(uint64(1)<<62))
		if oldThreshold >= threshold {
			t.Fatalf("rate %v: broken-math sanity check failed", tc.rate)
		}

		// A trace ID under the OLD cut must be kept (regression: old code
		// kept these too, this just anchors the range).
		below := Span{}
		binary.BigEndian.PutUint64(below.TraceID[:8], threshold/2)
		if !sampleTrace(below, tc.rate) {
			t.Errorf("rate %v: trace %x (mid-cut) must be kept", tc.rate, threshold/2)
		}

		// A trace ID between the OLD cut and the correct cut MUST be kept
		// under the fixed math but was dropped by the old rate/4 threshold
		// — this is the actual A1-7 regression boundary.
		mid := Span{}
		binary.BigEndian.PutUint64(mid.TraceID[:8], (oldThreshold+threshold)/2)
		if !sampleTrace(mid, tc.rate) {
			t.Errorf("rate %v: trace between old (%x) and correct (%x) cut must be kept under uniform [0,1) math",
				tc.rate, oldThreshold, threshold)
		}

		// Just under the exact cut: kept.
		justBelow := Span{}
		binary.BigEndian.PutUint64(justBelow.TraceID[:8], threshold-1)
		if !sampleTrace(justBelow, tc.rate) {
			t.Errorf("rate %v: trace threshold-1 must be kept", tc.rate)
		}
		// At the exact cut: dropped.
		at := Span{}
		binary.BigEndian.PutUint64(at.TraceID[:8], threshold)
		if sampleTrace(at, tc.rate) {
			t.Errorf("rate %v: trace at threshold must be dropped", tc.rate)
		}
		// Top of range: dropped.
		top := Span{}
		binary.BigEndian.PutUint64(top.TraceID[:8], math.MaxUint64)
		if sampleTrace(top, tc.rate) {
			t.Errorf("rate %v: max trace id must be dropped", tc.rate)
		}
	}
}

// A1-7 statistical sanity: uniform trace IDs under rate 0.25 keep ~25%
// (the broken math kept ~6.25%). Tolerance is generous; the point is the
// factor-of-four separation from the old threshold.
func TestSampleTraceRateKeepsConfiguredFraction(t *testing.T) {
	const rate = 0.25
	const n = 100000
	threshold := uint64(rate * float64(math.MaxUint64))
	kept := 0
	for i := 0; i < n; i++ {
		var sp Span
		// Spread probe IDs uniformly across the uint64 space.
		binary.BigEndian.PutUint64(sp.TraceID[:8], uint64(i)*(math.MaxUint64/n))
		if sampleTrace(sp, rate) {
			kept++
		}
	}
	got := float64(kept) / float64(n)
	if got < rate*0.9 || got > rate*1.1 {
		t.Fatalf("kept fraction = %.4f at rate %.2f, want within 10%% (old rate/4 math gives ~0.0625)", got, rate)
	}
	// Pin the identity: kept == ids below the exact threshold by construction.
	if want := len(keptBelowThreshold(n, threshold)); got > 0 && want != kept {
		t.Fatalf("kept %d != expected-below-threshold %d", kept, want)
	}
}

func keptBelowThreshold(n int, threshold uint64) (below []int) {
	for i := 0; i < n; i++ {
		v := uint64(i) * (math.MaxUint64 / uint64(n))
		if v < threshold {
			below = append(below, i)
		}
	}
	return below
}

// A1-7 (step spans): RecordStepSpan must stamp a STABLE, derived TraceID
// (from the task id) so sampling applies per trace instead of seeing v=0
// and keeping 100% of step spans at any rate.
func TestRecordStepSpanDerivesStableTraceID(t *testing.T) {
	sink := &capturingSink{}
	exp := NewTraceExporter(TraceExporterConfig{}, sink)
	p := NewTraceProducer(exp, 1.0, nil) // no sampler: assert ID derivation itself

	p.RecordStepSpan(StepSpanSpec{TaskID: "task-aaa", StepID: "s1", State: "completed"})
	p.RecordStepSpan(StepSpanSpec{TaskID: "task-aaa", StepID: "s2", State: "completed"})
	p.RecordStepSpan(StepSpanSpec{TaskID: "task-bbb", StepID: "s1", State: "completed"})
	exp.Flush()

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.spans) != 3 {
		t.Fatalf("sink spans = %d, want 3 (rate 0.5 must keep BOTH steps of the task-aaa trace together and the task-bbb span — if it does not, adjust expectation via determinism, not by luck)", len(sink.spans))
	}
	a, b, c := sink.spans[0], sink.spans[1], sink.spans[2]
	if a.TraceID == ([16]byte{}) {
		t.Fatal("step span carries the ZERO TraceID — sampler sees v=0 and sampling is a no-op (A1-7 regression)")
	}
	if a.TraceID != b.TraceID {
		t.Fatal("two steps of one task got different TraceIDs — per-trace sampling broken")
	}
	if a.TraceID == c.TraceID {
		t.Fatal("different tasks share a derived TraceID — grouping broken")
	}
	// Determinism of the derivation itself.
	if deriveTraceID("task-aaa") != deriveTraceID("task-aaa") {
		t.Fatal("deriveTraceID not deterministic")
	}
	if deriveTraceID("") != ([16]byte{}) {
		t.Fatal("empty key must yield the zero ID (exporter fills from crypto/rand)")
	}
}

// A1-7 (step spans + sampler): with a real sampler installed, a rejected
// task's step spans are ALL dropped (per-trace decision), an accepted
// task's are all kept.
func TestRecordStepSpanSamplingAppliesPerTrace(t *testing.T) {
	sink := &capturingSink{}
	exp := NewTraceExporter(TraceExporterConfig{}, sink)
	p := NewTraceProducer(exp, 1.0, nil) // install sampler below deterministically

	// Find one trace id the 0.5 cut rejects and one it accepts, then route
	// the producer through a sampler using the real sampleTrace math.
	p.sampler = func(sp Span) bool { return sampleTrace(sp, 0.5) }

	// task ids are hashed; sweep for one kept and one dropped at 0.5.
	var keptTask, droppedTask string
	for i := 0; i < 500 && (keptTask == "" || droppedTask == ""); i++ {
		id := "sweep-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		sp := Span{TraceID: deriveTraceID(id)}
		if sampleTrace(sp, 0.5) && keptTask == "" {
			keptTask = id
		} else if !sampleTrace(sp, 0.5) && droppedTask == "" {
			droppedTask = id
		}
	}
	if keptTask == "" || droppedTask == "" {
		t.Fatal("sweep found no kept/dropped task ids")
	}

	p.RecordStepSpan(StepSpanSpec{TaskID: keptTask, StepID: "1"})
	p.RecordStepSpan(StepSpanSpec{TaskID: keptTask, StepID: "2"})
	p.RecordStepSpan(StepSpanSpec{TaskID: droppedTask, StepID: "1"})
	p.RecordStepSpan(StepSpanSpec{TaskID: droppedTask, StepID: "2"})
	exp.Flush()

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.spans) != 2 {
		t.Fatalf("sink spans = %d, want 2 (both kept-task steps, none of the dropped task)", len(sink.spans))
	}
	for _, sp := range sink.spans {
		if sp.TraceID != deriveTraceID(keptTask) {
			t.Fatal("dropped-task span leaked past the sampler")
		}
	}
}

// A1-8: after a cooldown window EXPIRES, the next failure (consecFails 4)
// must RE-ARM the circuit. The old code armed only at exactly 3, so a dead
// receiver degraded to a probe (full 10s timeout block) on EVERY batch.
func TestTraceSinkCircuitReArmsAfterCooldown(t *testing.T) {
	sink := NewOTLPTraceHTTPSink("http://127.0.0.1:1", nil, nil) // unroutable port

	// Drive exactly the threshold failures.
	for range traceCircuitThreshold {
		sink.noteFailure(errors.New("boom"))
	}
	if !sink.circuitBlocked() {
		t.Fatal("circuit must be open after threshold failures")
	}

	// Simulate the cooldown window elapsing.
	sink.mu.Lock()
	sink.circuitOpenUntil = time.Now().Add(-time.Second)
	sink.mu.Unlock()

	if sink.circuitBlocked() {
		t.Fatal("circuit must allow a probe after cooldown elapses")
	}

	// The probe fails (streak is now threshold+1). The fix re-arms on every
	// failure >= threshold; the old code never re-armed past 3.
	sink.noteFailure(errors.New("boom again"))
	if !sink.circuitBlocked() {
		t.Fatal("A1-8 regression: circuit not re-armed after a post-cooldown failure (probe-per-batch forever)")
	}

	// And the pattern repeats across several windows: never an open gap
	// where every batch probes.
	for range 5 {
		sink.mu.Lock()
		sink.circuitOpenUntil = time.Now().Add(-time.Second)
		sink.mu.Unlock()
		if sink.circuitBlocked() {
			t.Fatal("cooldown must always open a probe window")
		}
		sink.noteFailure(errors.New("still down"))
		if !sink.circuitBlocked() {
			t.Fatal("circuit must re-arm after each probe failure")
		}
	}

	// Recovery closes the loop.
	sink.noteSuccess()
	if sink.circuitBlocked() {
		t.Fatal("success must close the circuit")
	}
}
