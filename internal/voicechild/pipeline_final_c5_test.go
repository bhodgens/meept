package voicechild

// Pins the C5 contract for the local leg's whisper adapter: whisper emits
// FINALS only (no partials), and the RMS gate suppresses loudness-only
// trips. So a FINAL transcript arriving while playback is live must carry
// the interrupt itself - stop the old playback BEFORE the new prompt's
// reply can start - or the old say plays over the new turn's answer.
import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// driveFinal runs one Final transcript through consumeTranscripts on a
// live pipeline (direct channel feed - the same seam openTranscriptSession
// uses) and returns after the prompt frame is emitted.
func driveFinal(p *pipeline, ccid, text string) {
	trans := make(chan Transcript, 1)
	trans <- Transcript{Text: text, Final: true}
	close(trans)
	done := make(chan struct{})
	go func() {
		p.consumeTranscripts(trans)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		p.cancel()
		<-done
	}
}

// TestFinalWhileSpeakingEmitsInterrupt: a final transcript arriving during
// live playback emits the C5 interrupt BEFORE the prompt frame (old audio
// must stop before the new answer starts).
func TestFinalWhileSpeakingEmitsInterrupt(t *testing.T) {
	const ccid = "rmsgate-final-c5"
	d, child, p, _, _, _ := newRMSChild(t, ccid, true, rmsThresh)
	gate := newChunkedTTS(quietSample, 12)
	swapTTS(p, gate)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "old playback"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })

	driveFinal(p, ccid, "actually, wait")
	waitFor(t, func() bool {
		return countQueuedInterrupts(d) == 1
	})
	if !p.interrupted {
		t.Fatal("final during playback did not latch the barge-in (noteBargeIn)")
	}
}

// TestFinalWhenIdleNoInterrupt: a final transcript with NO live playback
// must not emit a spurious interrupt (nothing to stop; the prompt alone
// starts the turn).
func TestFinalWhenIdleNoInterrupt(t *testing.T) {
	const ccid = "rmsgate-final-idle"
	d, _, p, _, _, _ := newRMSChild(t, ccid, true, rmsThresh)
	if p.isSpeaking() {
		t.Fatal("pipeline speaking before any say")
	}
	driveFinal(p, ccid, "hello")
	if p.interrupted {
		t.Fatal("idle final latched the barge-in; want none when playback is idle")
	}
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("idle final emitted %d interrupts, want 0 (noteBargeIn must refuse when not speaking)", n)
	}
}
