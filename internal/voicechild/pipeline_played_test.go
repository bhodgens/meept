package voicechild

// C1 played-edge tests (true end-of-turn drain marker): the pipeline
// sends exactly ONE played frame (last:true) on the text socket after the
// FINAL byte of a last:true say frame's stream is written to the .out
// socket, and NOTHING for a non-final (last:false) stream, a barge-in
// aborted stream, or a Speak error. The daemon arms the link floor's drain
// watch from this edge; the old blind arm at turn_final fired mid-reply on
// inter-sentence synth gaps (link-2fdc3f5fd8d81805: 10.94 s overlap).

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// PL-1: a two-sentence turn — first say last:false, second last:true —
// emits NO played for the first stream (mid-reply: more audio can still
// follow) and exactly ONE played frame with last:true after the final
// byte of the second stream reaches the socket. Wire bytes pinned.
func TestPlayedFrameEmittedAfterFinalStreamOnly(t *testing.T) {
	const ccid = "pl1"
	d, _, childConn, p, out := newPaceChild(t, ccid)
	tts := newPacedTTS(10*time.Millisecond, 3)
	swapTTS(p, tts)

	// Sentence 1: NOT the turn-final flush (last:false).
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "sentence 0", Last: new(false)})
	if ordinals := paceDrain(t, out, 3, 2*time.Millisecond); len(ordinals) != 3 {
		t.Fatalf("non-final stream did not pace out: %v", ordinals)
	}
	// The played edge must NOT exist for the non-final frame: give the
	// worker a beat to (not) emit, then sweep the queue.
	time.Sleep(300 * time.Millisecond)
	for _, f := range drainFrames(d) {
		if f.Event == voiceframe.EventPlayed {
			t.Fatalf("played emitted for a last:false stream: %+v", f)
		}
	}

	// Sentence 2: the turn-final flush.
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "sentence 1", Last: new(true)})
	if ordinals := paceDrain(t, out, 3, 2*time.Millisecond); len(ordinals) != 3 {
		t.Fatalf("final stream did not pace out: %v", ordinals)
	}

	// Exactly one played frame, last:true, call-scoped, byte-exact.
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPlayed {
		t.Fatalf("frame after final stream = %q, want %q", f.Event, voiceframe.EventPlayed)
	}
	if f.CallID != ccid {
		t.Fatalf("played call_id = %q, want %q", f.CallID, ccid)
	}
	if f.Last == nil || !*f.Last {
		t.Fatalf("played must carry last:true, got %v", f.Last)
	}
	raw, err := voiceframe.EncodeFrame(f)
	if err != nil {
		t.Fatalf("encode played frame: %v", err)
	}
	want := `{"call_id":"pl1","event":"played","last":true}` + "\n"
	if string(raw) != want {
		t.Fatalf("played wire bytes:\n got %s\nwant %s", raw, want)
	}
	time.Sleep(200 * time.Millisecond)
	if extra := drainFrames(d); len(extra) != 0 {
		t.Fatalf("played must fire exactly once per turn-final stream, extra frames: %+v", extra)
	}
}

// PL-2: a clear (barge-in) mid-last:true-stream aborts the writer — the
// stream never reaches natural EOF, so NO played frame is emitted: the
// floor for an interrupted turn rides the coordinator's idle release, and
// a phantom played edge would fake an end-of-turn on dead audio.
func TestNoPlayedOnBargeInAbort(t *testing.T) {
	const ccid = "pl2"
	d, _, childConn, p, out := newPaceChild(t, ccid)
	// 400 chunks x 640 B = 256 KB exceeds a Linux unix-socket send buffer
	// (~212 KB), so the clear lands mid-audio on every platform. The
	// 10 ms Speak delay keeps the one-shot provider shape unchanged.
	tts := newPacedTTS(10*time.Millisecond, 400)
	swapTTS(p, tts)

	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "sentence 0", Last: new(true)})
	paceDrain(t, out, 4, 2*time.Millisecond)
	clearLine(t, d, childConn, ccid)

	// Wait past any plausible late emit: the aborted stream's tail is
	// retired without socket writes, and retirement is NOT a drain.
	time.Sleep(500 * time.Millisecond)
	for _, f := range drainFrames(d) {
		if f.Event == voiceframe.EventPlayed {
			t.Fatalf("played emitted after a barge-in-aborted final stream: %+v", f)
		}
	}
}

// errTTS fails every Speak.
type errTTS struct{}

func (errTTS) Speak(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("test: speak failed")
}

// PL-3: a TTS Speak ERROR on the last:true frame (no audio ever) emits
// no played — the daemon-side denied/aborted-turn posture keeps riding
// the coordinator's fallback margin (late, never early).
func TestNoPlayedOnSpeakError(t *testing.T) {
	const ccid = "pl3"
	d, _, childConn, p, _ := newPaceChild(t, ccid)
	swapTTS(p, errTTS{})

	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "boom", Last: new(true)})
	time.Sleep(400 * time.Millisecond)
	for _, f := range drainFrames(d) {
		if f.Event == voiceframe.EventPlayed {
			t.Fatalf("played emitted for a failed synthesis: %+v", f)
		}
	}
}
