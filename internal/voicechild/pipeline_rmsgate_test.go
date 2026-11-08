package voicechild

// Defect 2026-11-08 gate tests: the local leg's RMS VAD trip must be
// evidence-gated on STT — only a recent non-empty FINAL transcript lets a
// trip EMIT an interrupt. Echo/noise trips (no recent final, or a final
// older than rmsSttEvidenceWindow) are suppressed entirely: no interrupt
// frame, latch and playback state untouched, so an in-flight answer is
// never cancelled by speakers or room noise. A final within the window
// preserves the existing contract: trip -> interrupt (fixture style of
// bargein_rms_test.go: chunkedTTS parks playback, writeMic injects caller
// PCM on the .in socket, countQueuedInterrupts observes the FIFO).

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// rmsGateEvidence sets the pipeline's last-final timestamp as
// consumeTranscripts would, under the same lock regime (playMu).
func rmsGateEvidence(p *pipeline, age time.Duration) {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	if age < 0 {
		p.lastFinalAt = time.Time{} // zero: never saw a final
		return
	}
	p.lastFinalAt = time.Now().Add(-age)
}

// stampRecentFinal is the tests' shorthand for "whisper just finalized
// the caller's utterance" — the evidence the RMS gate requires before a
// trip may emit an interrupt.
func stampRecentFinal(p *pipeline) {
	rmsGateEvidence(p, 500*time.Millisecond)
}

// tripLocal speaks via a chunkedTTS, stamps the STT evidence window as if
// whisper had finalized `evidenceAge` ago (negative = never), then drives
// a full loud-mic window through the pump tap; returns the gate the
// caller must close to release playback for teardown.
func tripLocal(t *testing.T, ccid string, evidenceAge time.Duration) (*fakeDaemon, *Child, *pipeline, *chunkedTTS) {
	t.Helper()
	d, child, p, in, _, _ := newRMSChild(t, ccid, true, rmsThresh)
	gate := newChunkedTTS(quietSample, 12) // quiet agent: near-zero echo reference
	swapTTS(p, gate)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "the answer is"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	rmsGateEvidence(p, evidenceAge)
	writeMic(t, in, callerSample, 12) // loud caller mic: sustains the window
	return d, child, p, gate
}

// TestRMSTripNoSTTEvidence: RMS trip with NO recent final transcript -> no
// interrupt frame emitted, latch stays armed, playback keeps running.
func TestRMSTripNoSTTEvidence(t *testing.T) {
	const ccid = "rmsgate1"
	d, _, p, gate := tripLocal(t, ccid, -1) // whisper never heard anything

	assertNoInterruptWithin(t, d, 50*time.Millisecond,
		"RMS trip without any STT final emitted an interrupt (echo/noise must not cancel the answer)")
	// Playback survives the suppressed trip: latch untouched, still speaking.
	if !p.isSpeaking() {
		t.Fatalf("suppressed trip stopped playback (speaking=false); echo must not cancel the answer")
	}
	close(gate.park)
}

// TestRMSTripWithRecentSTTEvidence: RMS trip WITH a final within the
// window -> interrupt emitted (existing behavior preserved).
func TestRMSTripWithRecentSTTEvidence(t *testing.T) {
	const ccid = "rmsgate2"
	d, _, _, gate := tripLocal(t, ccid, 500*time.Millisecond) // whisper heard words moments ago

	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("RMS trip with recent STT final emitted %q, want interrupt", f.Event)
	}
	close(gate.park)
}

// TestRMSTripStaleSTTEvidence: final older than rmsSttEvidenceWindow -> no
// interrupt (the evidence expired; loudness alone is not speech).
func TestRMSTripStaleSTTEvidence(t *testing.T) {
	const ccid = "rmsgate3"
	d, _, p, gate := tripLocal(t, ccid, rmsSttEvidenceWindow+2*time.Second)

	assertNoInterruptWithin(t, d, 50*time.Millisecond,
		"RMS trip with a stale STT final emitted an interrupt (window must bound the evidence)")
	if !p.isSpeaking() {
		t.Fatalf("suppressed stale-evidence trip stopped playback")
	}
	close(gate.park)
}
