package voice

// bughunt 2026-11-07 voice F-1 (HIGH) pin: barge-in is per BARGE-IN, not
// per call. Before the fix `interrupt`/`cancelSent` were once-per-call
// latches: after the first barge-in the pacer idle-waited forever (the
// agent was muted for the rest of the call) and even the Telnyx clear
// frame stopped being sent (cancelSent early-return). The splice now
// re-arms when fresh child PCM arrives at the read pump (only a new
// supervisor-allowed say produces PCM — the child aborted the interrupted
// stream).

import (
	"bytes"
	"testing"
	"time"
)

func TestBargeInResume(t *testing.T) {
	wsA := &fakeWS{}
	// rawPairFor, not capturePairFor: the splice read pump must be the
	// sole .out reader. This test asserts media frames, not captured
	// child bytes. A second .out reader (a unix Read wakes only one
	// blocked reader) can steal every chunk, so the pacer writes zero
	// media frames.
	pA := rawPairFor(t, "cc-r")
	textA := &textSink{}

	cfg := spliceConfig()
	depsA := SpliceDeps{TelnyxWS: wsA, Audio: pA, Text: textA, CancelDraftFn: func(string) {}, Clock: time.Now}
	cancelA, doneA := startSplice(t, "cc-r", cfg, depsA)
	defer stopSplice(t, cancelA, doneA)

	// Pre-interrupt traffic: the pacer is actively writing.
	for i := 0; i < 12; i++ {
		_ = feedChild("cc-r")
	}
	deadline := time.Now().Add(2 * time.Second)
	for wsA.framesMediaCount() < 3 && time.Now().Before(deadline) {
		_ = feedChild("cc-r")
		time.Sleep(5 * time.Millisecond)
	}
	if wsA.framesMediaCount() < 3 {
		t.Fatalf("precondition: pacer never wrote media frames (got %d)", wsA.framesMediaCount())
	}

	// Barge-in #1 (rides the media WS, as TestBargeInPerCall does).
	_ = wsA.Write(append([]byte(`{"event":"interrupt","call_id":"cc-r"}`), '\n'))
	clearDeadline := time.Now().Add(2 * time.Second)
	for !wsA.framesHas(`"event":"clear"`) && time.Now().Before(clearDeadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !wsA.framesHas(`"event":"clear"`) {
		t.Fatalf("no clear frame after interrupt")
	}

	// Stopped window: no new child PCM => no new media frames (the latch
	// is doing its job; the resume must not be eager).
	beforeResume := wsA.framesMediaCount()
	time.Sleep(150 * time.Millisecond)
	if grew := wsA.framesMediaCount() - beforeResume; grew > 1 {
		t.Fatalf("pacer kept writing while stopped (grew by %d frames)", grew)
	}

	// The NEXT utterance: fresh child PCM resumes the latch.
	for i := 0; i < 12; i++ {
		_ = feedChild("cc-r")
		time.Sleep(5 * time.Millisecond)
	}
	again := time.Now().Add(2 * time.Second)
	for wsA.framesMediaCount() < beforeResume+3 && time.Now().Before(again) {
		_ = feedChild("cc-r")
		time.Sleep(5 * time.Millisecond)
	}
	if wsA.framesMediaCount() < beforeResume+3 {
		t.Fatalf("F-1 mute bug: pacer stayed stopped after the first barge-in (media frames %d, want >= %d)",
			wsA.framesMediaCount(), beforeResume+3)
	}

	// Barge-in #2 must land AGAIN: cancelSent re-armed, so the second
	// interrupt fires CancelDraftFn + writes a second clear frame.
	clearsBefore := wsA.framesCountOf(`"event":"clear"`)
	_ = wsA.Write(append([]byte(`{"event":"interrupt","call_id":"cc-r"}`), '\n'))
	two := time.Now().Add(2 * time.Second)
	for wsA.framesCountOf(`"event":"clear"`) <= clearsBefore && time.Now().Before(two) {
		time.Sleep(5 * time.Millisecond)
	}
	if wsA.framesCountOf(`"event":"clear"`) <= clearsBefore {
		t.Fatal("second barge-in never emitted a clear frame (cancelSent latch never re-armed)")
	}
}

// fakeWS counting helpers (the shared fake exposes framesCount/framesHas/
// framesSnapshot only).
func (w *fakeWS) framesMediaCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, f := range w.frames {
		if !contains(f, `"event":"clear"`) {
			n++
		}
	}
	return n
}

func (w *fakeWS) framesCountOf(sub string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, f := range w.frames {
		if contains(f, sub) {
			n++
		}
	}
	return n
}

func contains(haystack []byte, needle string) bool {
	return bytes.Contains(haystack, []byte(needle))
}
