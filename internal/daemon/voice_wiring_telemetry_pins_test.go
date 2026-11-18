package daemon

// Telemetry contract pins (voice-events C-13/C-20, wiring level):
//   - C-13: the "heard" ring append rides a SUCCESSFUL frame enqueue -
//     a frame dropped by the full-queue default branch is never recorded
//     as heard (the telemetry must not lie about delivery); non-final
//     frames never record either.
//   - C-20(1): releaseCall probes HasRing, so an EVICTED ccid's release
//     does not resurrect a ring with a synthetic call_ended; a never-
//     tracked ccid still gets its terminal (the dial-failure posture).
import (
	"testing"

	"github.com/caimlas/meept/internal/voice"
)

// evTestEvents reads ccid's live-ring events (the ring is nil-safe).
func evTestEvents(t *testing.T, w *VoiceWiring, ccid string) []voice.TelemetryEvent {
	t.Helper()
	evs, _, _, _ := w.callEvents.Since(ccid, 0, 0)
	return evs
}

func TestRouteFrameHeardOnlyOnSuccessfulEnqueue(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.callEvents = voice.NewTelemetryRing(8, 8)
	last := true
	final := voice.Frame{CallID: "cc-heard", Event: voice.EventPrompt, Text: "hello there", Last: &last}

	// (a) queue has room: the frame is delivered AND heard is recorded.
	loop := voice.NewTalkLoop(voice.TalkDeps{Logger: w.logger}, voice.TalkLoopConfig{})
	w.loopsMu.Lock()
	w.loops["cc-heard"] = loop
	w.loopsMu.Unlock()
	w.routeFrame(final)
	if evs, _, _, found := w.callEvents.Since("cc-heard", 0, 0); !found || len(evs) != 1 || evs[0].Kind != voice.EventKindHeard {
		t.Fatalf("delivered final: ring = %+v found=%v, want one heard", evs, found)
	}

	// (b) queue full: the frame is DROPPED and heard is NOT recorded.
	// Fill the loop's 16-slot frames buffer (no goroutine drains it),
	// then route the final -> the default branch drops it silently.
	full := voice.NewTalkLoop(voice.TalkDeps{Logger: w.logger}, voice.TalkLoopConfig{})
	w.loopsMu.Lock()
	w.loops["cc-full"] = full
	w.loopsMu.Unlock()
	for range 16 {
		full.Frames() <- voice.Frame{CallID: "cc-full", Event: voice.EventPlayed}
	}
	w.routeFrame(voice.Frame{CallID: "cc-full", Event: voice.EventPrompt, Text: "dropped prompt", Last: &last})
	if evs, _, _, found := w.callEvents.Since("cc-full", 0, 0); found && len(evs) != 0 {
		t.Fatalf("dropped final recorded as heard: %+v", evs)
	}

	// (c) a partial (last != true) never records, even when delivered.
	partLoop := voice.NewTalkLoop(voice.TalkDeps{Logger: w.logger}, voice.TalkLoopConfig{})
	w.loopsMu.Lock()
	w.loops["cc-part"] = partLoop
	w.loopsMu.Unlock()
	w.routeFrame(voice.Frame{CallID: "cc-part", Event: voice.EventPrompt, Text: "partial preview"})
	if evs, _, _, found := w.callEvents.Since("cc-part", 0, 0); found && len(evs) != 0 {
		t.Fatalf("partial recorded as heard: %+v", evs)
	}
}

func TestReleaseCallDoesNotResurrectEvictedRing(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.callEvents = voice.NewTelemetryRing(4, 1) // maxRings=1
	// cc-1 records an event, then cc-2 evicts its ring.
	w.callEvents.Append("cc-1", voice.EventInput{Kind: voice.EventKindHeard})
	w.callEvents.Append("cc-2", voice.EventInput{Kind: voice.EventKindHeard})
	if _, _, _, found := w.callEvents.Since("cc-1", 0, 0); found {
		t.Fatal("precondition: cc-1 must be evicted (no live ring)")
	}
	// Releasing the dead evicted call must NOT recreate its ring (C-20):
	// the HasRing ever-recorded probe keeps the guard off, so no
	// synthetic call_ended lands and the ccid gains no live ring.
	before := len(evTestEvents(t, w, "cc-1"))
	w.releaseCall("cc-1", voice.OutcomeHangup)
	if evs := evTestEvents(t, w, "cc-1"); len(evs) != before {
		t.Fatalf("evicted ccid gained events on release: %+v", evs)
	}
	if _, _, _, found := w.callEvents.Since("cc-1", 0, 0); found {
		t.Fatal("releaseCall resurrected an evicted ring")
	}
	// A never-tracked ccid still gets the synthetic terminal (the
	// dial-failure posture: every ring-visible call has a named end).
	w.releaseCall("cc-never", voice.OutcomeFailed)
	if !w.callEvents.HasRing("cc-never") {
		t.Fatal("never-tracked ccid must get a synthetic call_ended on release")
	}
	if evs, _, _, _ := w.callEvents.Since("cc-never", 0, 0); len(evs) != 1 || evs[0].Kind != voice.EventKindCallEnded {
		t.Fatalf("synthetic terminal = %+v, want one call_ended", evs)
	}
}

// C-13 extension (heard-text fallback, local-53b347f028f0a43f follow-up
// of the sibling merge's raw-From swap): a FINAL prompt frame with an
// EMPTY From (legacy per-final mode / older or fake children that never
// fill raw) must record the heard event with the Text preview, never an
// empty one - a blank "heard:" line makes the operator feed lie. A frame
// that DOES carry From keeps the RAW (read before the swap moves it into
// Text).
func TestRouteFrameHeardFallsBackToTextWhenFromEmpty(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.callEvents = voice.NewTelemetryRing(8, 8)
	last := true

	loop := voice.NewTalkLoop(voice.TalkDeps{Logger: w.logger}, voice.TalkLoopConfig{})
	w.loopsMu.Lock()
	w.loops["cc-ntx"] = loop
	w.loopsMu.Unlock()
	w.routeFrame(voice.Frame{CallID: "cc-ntx", Event: voice.EventPrompt, Text: "local utterance one", Last: &last})
	evs, _, _, found := w.callEvents.Since("cc-ntx", 0, 0)
	if !found || len(evs) != 1 {
		t.Fatalf("no-From final: ring = %+v found=%v, want one heard", evs, found)
	}
	if evs[0].Kind != voice.EventKindHeard || evs[0].Text != "local utterance one" || evs[0].Words != 3 {
		t.Fatalf("no-From heard = %+v, want Text fallback with word count", evs[0])
	}

	// With From filled (aggregation-mode raw), the RAW is the heard text.
	loop2 := voice.NewTalkLoop(voice.TalkDeps{Logger: w.logger}, voice.TalkLoopConfig{})
	w.loopsMu.Lock()
	w.loops["cc-raw"] = loop2
	w.loopsMu.Unlock()
	w.routeFrame(voice.Frame{CallID: "cc-raw", Event: voice.EventPrompt, Text: "normalized prompt", From: "um raw transcript", Last: &last})
	evs, _, _, _ = w.callEvents.Since("cc-raw", 0, 0)
	if len(evs) != 1 || evs[0].Text != "um raw transcript" {
		t.Fatalf("raw-From heard = %+v, want the RAW text (read before the swap)", evs)
	}
}
