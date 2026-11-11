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
//
// Dispatch watermark (defect 2026-11-11): a real evidence final arrives
// AFTER the current turn's prompt dispatched, so the helper lays a
// watermark 2 s before the stamp as well — evidence that post-dates the
// dispatch is what the rmsBargeInCheck gate requires. Tests that want to
// pin the predates-dispatch suppression call dispatchWatermark after
// stamping (a stamp taken AFTER the helper resets the watermark to a
// later time than the evidence).
func rmsGateEvidence(p *pipeline, age time.Duration) {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	if age < 0 {
		p.lastFinalAt = time.Time{} // zero: never saw a final
		return
	}
	p.lastDispatchAt = time.Now().Add(-(age + 2*time.Second))
	p.lastFinalAt = time.Now().Add(-age)
}

// dispatchWatermark sets the pipeline's last-dispatch timestamp the way
// consumeTranscripts does after a final prompt emits successfully (playMu
// regime). Positive `age` = the dispatch happened that long ago; negative
// = zero watermark (no final has dispatched this call yet).
func dispatchWatermark(p *pipeline, age time.Duration) {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	if age < 0 {
		p.lastDispatchAt = time.Time{}
		return
	}
	p.lastDispatchAt = time.Now().Add(-age)
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
	d, _, p, gate := tripLocal(t, ccid, 500*time.Millisecond) // whisper heard words moments ago

	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("RMS trip with recent STT final emitted %q, want interrupt", f.Event)
	}
	// Leaf 02 (defect 2026-11-09): the trip CONSUMED the evidence —
	// lastFinalAt is zeroed so a second trip needs a NEW final.
	if zero, _ := finalEvidence(p); !zero {
		t.Fatal("authorized RMS trip left lastFinalAt set (evidence must be one-shot)")
	}
	close(gate.park)
}

// TestRMSTripEvidenceIsOneShot: the live defect (call
// local-2d84c384fd93f447) — five barge-ins cancelled the turn a PRIOR
// final had already dispatched, because the 10 s window stayed
// re-authorized by a stale lastFinalAt. Contract: an authorized trip
// clears the evidence, so a second sustained-loud window after the next
// say re-arms the latch is SUPPRESSED until a new final re-stamps it;
// then the trip fires again. Genuine new speech still interrupts.
func TestRMSTripEvidenceIsOneShot(t *testing.T) {
	const ccid = "rmsgate4"
	const chunks = 12
	d, child, p, in, out, _ := newRMSChild(t, ccid, true, rmsThresh)

	// Trip 1: recent final (stamp) + loud mic -> interrupt, evidence consumed.
	gate1 := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate1)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "first answer"}); err != nil {
		t.Fatalf("handleSay 1: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	stampRecentFinal(p) // the final that dispatched the in-flight turn
	writeMic(t, in, callerSample, chunks)
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("trip 1 emitted %q, want interrupt", f.Event)
	}
	if zero, _ := finalEvidence(p); !zero {
		t.Fatal("trip 1 did not consume the STT evidence (lastFinalAt still set)")
	}
	close(gate1.park)
	drainOut(t, out, chunks*640) // gate1's whole stream wrote out (buffer stays clean)

	// Trip 2 (the defect): a NEW say re-arms the latch; the SAME loud mic
	// audio keeps arriving, but NO new final has landed — the evidence
	// was consumed by trip 1, so this window must NOT cancel the new
	// in-flight answer. Sync on gate2's FIRST outbound chunk (the
	// TestRMSBargeIn barge-in 2 pattern: isSpeaking() alone can race the
	// retired stream's deferred clear, so a first-chunk observation is the
	// deterministic proof the re-arm is live).
	gate2 := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate2)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "second answer"}); err != nil {
		t.Fatalf("handleSay 2: %v", err)
	}
	drainOut(t, out, 640) // gate2's first chunk: the re-arm is live
	writeMic(t, in, callerSample, chunks)
	assertNoInterruptWithin(t, d, 80*time.Millisecond,
		"second RMS window re-tripped off consumed evidence (one-shot: a trip must clear lastFinalAt)")
	if !p.isSpeaking() {
		t.Fatal("suppressed second trip stopped playback")
	}
	close(gate2.park)
	drainOut(t, out, (chunks-1)*640) // drain gate2's remaining chunks

	// Trip 3: a NEW final re-authorizes — genuine new speech interrupts.
	stampRecentFinal(p)
	gate3 := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate3)
	defer close(gate3.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "third answer"}); err != nil {
		t.Fatalf("handleSay 3: %v", err)
	}
	drainOut(t, out, 640) // gate3's first chunk: re-armed
	writeMic(t, in, callerSample, chunks)
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("trip after a NEW final emitted %q, want interrupt (new speech must still barge in)", f.Event)
	}
	// Whole scenario: exactly 2 interrupts (trips 1 and 3, both popped;
	// trip 2's window stayed silent — nothing else in the FIFO).
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("scenario queued %d unexpected interrupts beyond the 2 popped, want 2 total", n)
	}
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
