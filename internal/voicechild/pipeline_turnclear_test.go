package voicechild

// D3 regression (defect 2026-11-13): the Smart Turn analyzer was never
// reset between turns. grep proved the TurnAnalyzer interface's Clear()
// had ZERO non-test callers, while pumpAudioIn feeds AppendAudio
// continuously across the whole call — so the model's buffered turn
// audio and sticky speech state leaked from turn N into turn N+1's
// endpointing verdict. Contract pinned here: Clear runs (under turnMu)
// at EVERY final dispatch point — the legacy inline path, the
// aggregated dispatch — and at teardown before Close; partials never
// clear (they do not end a turn). Fixture shape mirrors
// pipeline_turnaggregate_test.go (newAggChild + recordingSTT lane);
// the fake analyzer mirrors the fakeVAD injection style.

import (
	"context"
	"sync"
	"testing"
)

// recordingTurn is a fake TurnAnalyzer that counts Clear/Close calls;
// injected directly into p.turn under turnMu (the regime every analyzer
// read/write uses — see the field comment in pipeline.go).
type recordingTurn struct {
	mu       sync.Mutex
	clears   int
	closes   int
	appended int
}

func (r *recordingTurn) AppendAudio(chunk []byte, isSpeech bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appended++
}

func (r *recordingTurn) SetSampleRate(sampleRate int) {}
func (r *recordingTurn) SetVADStartSecs(secs float64) {}
func (r *recordingTurn) AnalyzeEndOfTurn() (bool, float64, error) {
	return true, 0.9, nil
}

func (r *recordingTurn) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clears++
}

func (r *recordingTurn) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes++
	return nil
}

func (r *recordingTurn) counts() (clears, closes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.clears, r.closes
}

func installTurn(p *pipeline, fa *recordingTurn) {
	p.turnMu.Lock()
	p.turn = fa
	p.turnMu.Unlock()
}

func TestSmartTurnClearOnDispatchAndTeardown(t *testing.T) {
	const ccid = "turnclr1"
	zero := 0 // legacy per-final dispatch: inline path, byte-identical emit
	d, _, p, stt := newAggChild(t, ccid, &zero)

	fa := &recordingTurn{}
	installTurn(p, fa)

	// FINAL 1 -> inline dispatch -> exactly one Clear.
	stt.emit(Transcript{Text: "alpha beta gamma", Final: true})
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	if c, cl := fa.counts(); c != 1 || cl != 0 {
		t.Fatalf("after first final: clears=%d closes=%d, want 1/0 (dispatch must reset the turn buffer)", c, cl)
	}

	// PARTIAL -> no Clear: partials do not end a turn.
	stt.emit(Transcript{Text: "dangling half a", Final: false})
	waitFor(t, func() bool { return countQueuedPrompts(d) == 2 })
	if c, _ := fa.counts(); c != 1 {
		t.Fatalf("after partial: clears=%d, want 1 (partials must not reset the analyzer)", c)
	}

	// FINAL 2 -> second dispatch -> second Clear (turn N+1 starts clean).
	stt.emit(Transcript{Text: "delta epsilon zeta", Final: true})
	waitFor(t, func() bool { return countQueuedPrompts(d) == 3 })
	if c, _ := fa.counts(); c != 2 {
		t.Fatalf("after second final: clears=%d, want 2 (every dispatched final must reset the analyzer)", c)
	}

	// Teardown -> Clear BEFORE Close (one extra reset, then release).
	p.close(context.Background())
	c, cl := fa.counts()
	if cl != 1 {
		t.Fatalf("closes=%d, want 1 (ONNX session must be released on teardown)", cl)
	}
	if c != 3 {
		t.Fatalf("after teardown: clears=%d, want 3 (dispatch-path clears + teardown clear)", c)
	}

	// Post-teardown: the field was nulled — a second close must not
	// re-Clear or re-Close (exactly-once per call).
	p.close(context.Background())
	if c2, cl2 := fa.counts(); c2 != c || cl2 != cl {
		t.Fatalf("double teardown: clears=%d closes=%d, want %d/%d", c2, cl2, c, cl)
	}
}

func TestSmartTurnClearOnAggregatedDispatch(t *testing.T) {
	const ccid = "turnclg1"
	win := 40 // aggregation ON: two finals merge into ONE dispatch
	d, _, p, stt := newAggChild(t, ccid, &win)

	fa := &recordingTurn{}
	installTurn(p, fa)

	stt.emit(Transcript{Text: "yeah not sure", Final: true})
	stt.emit(Transcript{Text: "can you explain", Final: true})
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	popPrompts(d) // confirm the merged final prompt landed

	if c, _ := fa.counts(); c != 1 {
		t.Fatalf("after aggregated dispatch: clears=%d, want 1 (dispatchAggregated must reset the analyzer too)", c)
	}
	if _, parts, _ := pendingState(p); parts != 0 {
		t.Fatalf("pending buffer not cleared: parts=%d", parts)
	}

}
