package voicechild

// Leaf 06 (barge-in trigger) leaf tests: deterministic on this host, unlike
// the byte-frozen gate TestBargeInViaPartial whose Scenario B/C depend on a
// stub-TTS playback window the host cannot observe (playback completes in
// sub-millisecond time). Here the TTS is GATED (gatedTTS, child_test.go):
// the stream hands out the first chunk, then blocks on the second read, so
// p.speaking stays true for as long as the test needs.
//
//   TestBargeInFrameOrder  (T4): partial while speaking -> the interrupt
//     frame precedes the partial's prompt frame in the emitted FIFO (C5).
//   TestBargeInLatchRearmLeaf (T2/T3): idle partial -> prompt only; first
//     partial while speaking -> exactly one interrupt; second partial while
//     still speaking -> latch blocks; clear + new say -> latch re-arms and
//     the next partial interrupts again.

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// newBargeInChild wires a pipeline with the recording STT and swaps in a
// gated TTS so playback stays active until the test releases it.
func newBargeInChild(t *testing.T, ccid string) (*fakeDaemon, *Child, *pipeline, *recordingSTT, *gatedTTS) {
	t.Helper()
	d, child, _, p, stt := newSTTWiredChild(t, ccid)
	waitFor(t, func() bool { return stt.streamCalls() == 1 })
	gate := newGatedTTS()
	swapTTS(p, gate)
	return d, child, p, stt, gate
}

// pendingInterrupts counts interrupt frames currently sitting in the fake
// daemon's FIFO without popping anything.
func pendingInterrupts(d *fakeDaemon) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, f := range d.queue {
		if f.Event == voiceframe.EventInterrupt {
			n++
		}
	}
	return n
}

// TestBargeInFrameOrder (leaf T4): with playback active, one partial emits
// interrupt BEFORE prompt for the same utterance (C5 order), read off the
// fake daemon's FIFO in arrival order.
func TestBargeInFrameOrder(t *testing.T) {
	const ccid = "bio1"
	d, child, p, stt, gate := newBargeInChild(t, ccid)

	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "hold on"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	defer close(gate.releaseSecond) // unblock playback so teardown drains

	stt.emit(Transcript{Text: "wait", Final: false})

	fInt := d.waitFrame(t)
	if fInt.Event != voiceframe.EventInterrupt {
		t.Fatalf("first frame for partial-while-speaking = %q, want interrupt (C5: interrupt first)", fInt.Event)
	}
	fPrompt := d.waitFrame(t)
	if fPrompt.Event != voiceframe.EventPrompt || fPrompt.Text != "wait" {
		t.Fatalf("second frame = %q %q, want prompt \"wait\"", fPrompt.Event, fPrompt.Text)
	}
	if n := pendingInterrupts(d); n != 0 {
		t.Fatalf("latch did not hold: %d extra interrupts queued after one barge-in", n)
	}
}

// TestBargeInLatchRearmLeaf (leaf T2/T3): full latch lifecycle, deterministic
// via the gated TTS - idle partial never interrupts; partial while speaking
// interrupts exactly once; the latch blocks a repeat while still speaking;
// clear + new say re-arms and the next partial interrupts again.
func TestBargeInLatchRearmLeaf(t *testing.T) {
	const ccid = "bil1"
	d, child, p, stt, gate1 := newBargeInChild(t, ccid)

	// T2: idle -> partial emits prompt only, no interrupt.
	stt.emit(Transcript{Text: "hello are there", Final: false})
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt {
		t.Fatalf("idle partial emitted %q, want prompt only", f.Event)
	}
	if n := pendingInterrupts(d); n != 0 {
		t.Fatalf("idle partial emitted %d interrupts, want 0", n)
	}

	// Speaking (gated stream: playback holds until released). enteredPlay
	// closes on the stream's first read, which happens AFTER playback's
	// locked section arms the latch - no isSpeaking() polling race.
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "sure, one moment"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	select {
	case <-gate1.enteredPlay:
	case <-time.After(5 * time.Second):
		t.Fatalf("utterance 1 playback never started")
	}

	// First partial while speaking -> exactly one interrupt (then prompt).
	stt.emit(Transcript{Text: "wait", Final: false})
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("partial while speaking emitted %q, want interrupt", f.Event)
	}
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "wait" {
		t.Fatalf("after interrupt got %q %q, want prompt \"wait\"", f.Event, f.Text)
	}

	// Second partial while STILL speaking: latch blocks the repeat; only
	// the prompt preview flows.
	stt.emit(Transcript{Text: "wait please", Final: false})
	time.Sleep(150 * time.Millisecond)
	if n := pendingInterrupts(d); n != 0 {
		t.Fatalf("second partial emitted %d extra interrupts, want exactly one per barge-in (latch)", n)
	}
	f = d.waitFrame(t) // drain the preview prompt
	if f.Event != voiceframe.EventPrompt || f.Text != "wait please" {
		t.Fatalf("second partial emitted %q %q, want prompt \"wait please\"", f.Event, f.Text)
	}

	// T3 re-arm: clear retires the stream; a new say restarts playback and
	// resets the latch; the next partial interrupts again.
	child.handleClear(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventClear})
	waitFor(t, p.isDropped)
	close(gate1.releaseSecond) // stale stream's next read: gen mismatch -> abort
	gate2 := newGatedTTS()
	swapTTS(p, gate2)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "where were we"}); err != nil {
		t.Fatalf("handleSay 2: %v", err)
	}
	select {
	case <-gate2.enteredPlay: // latch re-armed before this closed (see utterance 1)
	case <-time.After(5 * time.Second):
		t.Fatalf("utterance 2 playback never started")
	}
	defer close(gate2.releaseSecond)

	stt.emit(Transcript{Text: "go on", Final: false})
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("partial after re-arm emitted %q, want interrupt", f.Event)
	}
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "go on" {
		t.Fatalf("after re-arm interrupt got %q %q, want prompt \"go on\"", f.Event, f.Text)
	}
	// Total across the whole scenario: exactly 2 interrupts (one per
	// barge-in: 1 before re-arm + 1 after). Everything not popped is in
	// the queue, everything popped was asserted above.
	if n := pendingInterrupts(d); n != 0 {
		t.Fatalf("unexpected %d interrupts queued at end, want 2 total for the scenario", n)
	}
}
