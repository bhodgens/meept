package voicechild

// Dispatch-watermark gate tests (defect 2026-11-11, live call
// local-611f46b1c3fe5c5b): the RMS STT-evidence gate must additionally
// require the authorizing final to POST-DATE the current turn's dispatch
// (lastDispatchAt, stamped by consumeTranscripts right after a final
// prompt emits successfully). The hole: the authorizing final was
// usually the user's OWN prompt that already dispatched the in-flight
// turn, so any echo/noise RMS window in the first 10 s of a fast answer
// was auto-authorized and killed the reply mid-sentence (observed at
// 2.9 s and 1.2 s into replies). Fixture style per
// pipeline_rmsgate_test.go: chunkedTTS parks playback, writeMic injects
// caller PCM on the .in socket, the fake daemon FIFO observes frames.

import (
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// dispatchState reads (lastDispatchAt zero?, lastDispatchAt) under
// playMu — the same regime consumeTranscripts stamps under.
func dispatchState(p *pipeline) (zero bool, at time.Time) {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return p.lastDispatchAt.IsZero(), p.lastDispatchAt
}

// sttFinal drives a real FINAL transcript through the pipeline's own
// transcript consumer (the recordingSTT lane wired by newRMSChild) and
// waits for the resulting final prompt frame, so lastFinalAt AND the
// lastDispatchAt watermark are stamped exactly as production does —
// including the watermark's strict "after a successful emit" position.
// Caller must ensure playback is idle so the C5 speaking-final path
// emits no interrupt (clean FIFO).
func sttFinal(t *testing.T, d *fakeDaemon, stt *recordingSTT, text string) {
	t.Helper()
	stt.emit(Transcript{Text: text, Final: true})
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != text || f.Last == nil || !*f.Last {
		t.Fatalf("frame = %q %q last=%v, want final prompt %q", f.Event, f.Text, f.Last, text)
	}
}

// TestFinalDispatchStampsWatermark: consumeTranscripts stamps
// lastDispatchAt after a final prompt emits successfully, and the
// watermark does NOT precede that final's own evidence stamp — the exact
// property that makes the dispatching prompt unable to authorize its own
// turn's RMS trips. Partials never move the watermark (no turn starts on
// one).
func TestFinalDispatchStampsWatermark(t *testing.T) {
	const ccid = "rmsdisp0"
	d, _, p, _, _, stt := newRMSChild(t, ccid, true, rmsThresh)

	if zero, _ := dispatchState(p); !zero {
		t.Fatal("lastDispatchAt set before any final dispatched")
	}

	sttFinal(t, d, stt, "what time is it")
	zero, dispAt := dispatchState(p)
	if zero {
		t.Fatal("successful final dispatch did not stamp lastDispatchAt")
	}
	p.playMu.Lock()
	finalAt := p.lastFinalAt
	p.playMu.Unlock()
	if finalAt.After(dispAt) {
		t.Fatalf("evidence stamp %v post-dates its own dispatch watermark %v (fixture invariant: watermark follows the emit)", finalAt, dispAt)
	}

	// A partial emits a preview prompt but never moves the watermark.
	_, before := dispatchState(p)
	drivePartial(p, ccid, "and the weather no")
	if _, after := dispatchState(p); !after.Equal(before) {
		t.Fatalf("partial moved lastDispatchAt %v -> %v (watermark is finals-dispatch only)", before, after)
	}
}

// TestRMSTripEvidencePredatesDispatch: the live hole, end-to-end. The
// user's own prompt dispatched the turn (watermark stamped after that
// final); the agent starts answering; a sustained loud mic window
// arrives with NO new final. The only in-window evidence is the prompt
// that STARTED this turn: the trip must be SUPPRESSED — no interrupt
// frame, latch and playback untouched — and the suppression must NOT
// consume the one-shot evidence (the trip never fired).
func TestRMSTripEvidencePredatesDispatch(t *testing.T) {
	const ccid = "rmsdisp1"
	const chunks = 12
	d, child, p, in, _, stt := newRMSChild(t, ccid, true, rmsThresh)
	// Debug-level capture so the suppression reason is observable (the
	// captureLogger default is INFO; the watermark gate logs Debug).
	capLog := newCaptureLogger()
	p.logger = slog.New(slog.NewTextHandler(&captureWriter{c: capLog}, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Turn 1 dispatches while playback is idle: no C5 interrupt, FIFO
	// holds only the prompt (popped by sttFinal).
	sttFinal(t, d, stt, "what time is it")

	// The fast answer starts playing (quiet TTS: near-zero echo
	// reference, so the loud mic window also clears the echo guard —
	// only the watermark gate stands between it and a trip).
	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "the answer is"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })

	// Echo/noise inside the 10 s tail of the prompt's own final: the
	// window gate passes (recent final); the watermark gate must
	// suppress (evidence predates this turn's dispatch).
	writeMic(t, in, callerSample, chunks)
	assertNoInterruptWithin(t, d, 80*time.Millisecond,
		"RMS trip authorized by the prompt final that dispatched the live turn cancelled the reply (dispatch watermark: evidence must post-date the dispatch)")
	if !p.isSpeaking() {
		t.Fatal("suppressed stale-evidence trip stopped playback")
	}
	if zero, _ := finalEvidence(p); zero {
		t.Fatal("suppressed trip consumed STT evidence (lastFinalAt zeroed without firing)")
	}
	if !capLog.contains("evidence predates turn dispatch") {
		t.Fatalf("suppression Debug log missing the watermark reason; got:\n%s", capLog.everything())
	}
}

// TestRMSTripAfterNewPostDispatchFinalTrips: real barge-in still works.
// Same shape as the predates-dispatch hole, except a NEW whisper final
// lands AFTER the turn dispatched (the caller is genuinely talking over
// the answer): lastFinalAt now post-dates the watermark and the trip is
// re-earned. Deterministic fixture form: dispatchWatermark laid before
// the evidence stamp, exactly the relationship rmsGateEvidence models —
// then the sustained loud window emits the interrupt and consumes the
// evidence one-shot as before.
func TestRMSTripAfterNewPostDispatchFinalTrips(t *testing.T) {
	const ccid = "rmsdisp2"
	const chunks = 12
	d, child, p, in, _, _ := newRMSChild(t, ccid, true, rmsThresh)

	// The turn's prompt dispatched 4 s ago (watermark), and the caller's
	// new barge-in utterance finalized 500 ms ago — evidence STRICTLY
	// after the dispatch.
	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "the answer is"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	dispatchWatermark(p, 4*time.Second)
	rmsGateEvidence(p, 500*time.Millisecond) // re-stamps both: watermark 2.5 s ago, evidence 500 ms ago
	p.playMu.Lock()
	postDispatch := p.lastFinalAt.After(p.lastDispatchAt)
	p.playMu.Unlock()
	if !postDispatch {
		t.Fatal("fixture invariant broken: evidence does not post-date the dispatch")
	}

	writeMic(t, in, callerSample, chunks)
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("RMS trip with evidence post-dating the dispatch emitted %q, want interrupt (real barge-in must keep working)", f.Event)
	}
	// One-shot consumption is preserved alongside the watermark.
	if zero, _ := finalEvidence(p); !zero {
		t.Fatal("authorized trip did not consume the STT evidence (one-shot contract)")
	}
}

// TestRMSTripRealPathNewFinalInterrupts: end-to-end barge-in on the real
// transcript lane — a final that lands DURING live playback interrupts
// the answer (the C5 speaking-final path, untouched by this fix, carries
// the interrupt itself) and its prompt dispatches after it. The RMS lane
// never self-cancels: the watermark follows every successful final
// dispatch, so a later echo/noise window off that same evidence stays
// suppressed and exactly one interrupt crosses the wire per barge-in.
func TestRMSTripRealPathNewFinalInterrupts(t *testing.T) {
	const ccid = "rmsdisp3"
	const chunks = 12
	d, child, p, in, _, stt := newRMSChild(t, ccid, true, rmsThresh)

	sttFinal(t, d, stt, "what time is it") // turn 1 dispatched (prompt popped)

	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "the answer is"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })

	// The caller barges in with a real utterance: whisper finalizes it
	// while playback is live.
	stt.emit(Transcript{Text: "actually wait", Final: true})
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("frame = %q, want interrupt (new speech over live playback must barge in)", f.Event)
	}
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "actually wait" {
		t.Fatalf("frame = %q %q, want prompt %q after the barge-in interrupt", f.Event, f.Text, "actually wait")
	}

	// A further loud window (same evidence, now behind its own dispatch
	// watermark + consumed one-shot) must NOT emit a second interrupt.
	writeMic(t, in, callerSample, chunks)
	assertNoInterruptWithin(t, d, 80*time.Millisecond,
		"second RMS window emitted an extra interrupt (exactly-once per barge-in, watermark re-armed by the new dispatch)")
}

// TestRMSTripZeroWatermarkSuppresses: a fresh evidence final with NO
// successful dispatch yet this call (zero watermark) must not authorize
// an RMS trip — no turn has dispatched, and a real final's own interrupt
// rides the C5 path, so the early-warning lane has nothing to prove.
// Latch and playback untouched (same suppression shape).
func TestRMSTripZeroWatermarkSuppresses(t *testing.T) {
	const ccid = "rmsdisp4"
	const chunks = 12
	d, child, p, in, _, _ := newRMSChild(t, ccid, true, rmsThresh)

	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "the answer is"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	stampRecentFinal(p)      // whisper heard words moments ago...
	dispatchWatermark(p, -1) // ...but no final has dispatched yet (zero watermark).
	writeMic(t, in, callerSample, chunks)
	assertNoInterruptWithin(t, d, 80*time.Millisecond,
		"RMS trip authorized with a zero dispatch watermark emitted an interrupt (RMS is the early-warning lane only)")
	if !p.isSpeaking() {
		t.Fatal("suppressed zero-watermark trip stopped playback")
	}
}
