package voicechild

// BARGE-IN CONTRACT (2026-11-11) dispatch-side tests: agent-echo finals
// (whisper re-hearing the TTS through the speakers) must never dispatch a
// turn, stamp speech evidence, or fire the C5 interrupt — the self-answer
// loop the dispatch watermark (e0dbce12) unmasked. A HUMAN final that
// does not match the echo window keeps the full existing contract:
// interrupt on arrival (C5 speaking-final path) + prompt dispatch.
// Fixture style per pipeline_rmsdispatch_test.go (newRMSChild +
// chunkedTTS + recordingSTT).

import (
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// flushingSTT is a recording-style STT that also implements the
// sttFlusher seam, counting FlushNow calls (pipeline early-kick wiring).
type flushingSTT struct {
	recordingSTT
	flushes atomic.Int64
}

func (f *flushingSTT) FlushNow() { f.flushes.Add(1) }

// TestTripAttemptKicksWhisperFlush: a sustained-energy window that
// clears the echo guard reaches the FlushNow seam on a trip ATTEMPT even
// when the trip is ultimately SUPPRESSED (no evidence here) — the whole
// point is that the whisper final lands ~350 ms sooner on the C5 path
// regardless of whether the RMS lane itself fires (BARGE-IN CONTRACT
// (2026-11-11)).
func TestTripAttemptKicksWhisperFlush(t *testing.T) {
	const ccid = "echokick1"
	const chunks = 12
	d, child, p, in, _, _ := newRMSChild(t, ccid, true, rmsThresh)
	fstt := &flushingSTT{recordingSTT: *newRecordingSTT()}
	p.playMu.Lock()
	p.stt = fstt
	p.playMu.Unlock()

	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "the answer is"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })

	writeMic(t, in, callerSample, chunks) // loud window, no evidence -> suppressed trip
	assertNoInterruptWithin(t, d, 80*time.Millisecond,
		"loud window without evidence fired (kick test must not trip)")
	if n := fstt.flushes.Load(); n == 0 {
		t.Fatal("sustained-energy trip attempt never kicked the STT flush seam (FlushNow wiring dead)")
	}
}

// TestEchoFinalDropped: during live playback the agent's own sentence
// comes back through the mic as a whisper final -> NO prompt frame, NO
// interrupt, playback untouched, evidence NOT stamped, INFO drop log.
func TestEchoFinalDropped(t *testing.T) {
	const ccid = "echodrop1"
	const chunks = 12
	d, child, p, _, _, stt := newRMSChild(t, ccid, true, rmsThresh)
	capLog := newCaptureLogger()
	p.logger = slog.New(slog.NewTextHandler(&captureWriter{c: capLog}, nil))

	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	const agentLine = "the weather today is definitely sunny"
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: agentLine}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })

	// Whisper re-hears the agent's sentence (garbled tail — the fuzzy
	// lane exists precisely because echoes never land verbatim).
	stt.emit(Transcript{Text: "the weather today is definitely sunney", Final: true})
	time.Sleep(1200 * time.Millisecond) // past the confirm window: a leaked final would have dispatched by now

	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("echo final emitted %d prompt frame(s), want 0 (self-answer loop must not dispatch)", n)
	}
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("echo final emitted %d interrupt frame(s), want 0 (an echo must not cancel the live reply)", n)
	}
	if !p.isSpeaking() {
		t.Fatal("echo final stopped playback (latch must stay untouched)")
	}
	if zero, text := finalEvidence(p); !zero || text != "" {
		t.Fatalf("echo final stamped speech evidence (zero=%v text=%q); drops must be invisible to the RMS gate", zero, text)
	}
	if !capLog.contains("echo final dropped") {
		t.Fatalf("drop INFO log missing; got:\n%s", capLog.everything())
	}
	// The agent's own line is still recorded in the window (the drop
	// reads it, never mutates it).
	p.playMu.Lock()
	win := p.echoWindowTextLocked()
	p.playMu.Unlock()
	if len(win) != 1 || win[0] != agentLine {
		t.Fatalf("echo window = %v, want the one say entry", win)
	}
}

// TestHumanFinalDuringPlaybackStillDispatches: a caller utterance that
// does NOT match the echo window keeps the untouched C5 contract —
// interrupt on arrival, then the final prompt dispatches (the sibling
// daemon roster + min_turn_words layers carry any further policy).
func TestHumanFinalDuringPlaybackStillDispatches(t *testing.T) {
	const ccid = "echodrop2"
	const chunks = 12
	d, child, p, _, _, stt := newRMSChild(t, ccid, true, rmsThresh)

	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "the weather today is definitely sunny"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })

	stt.emit(Transcript{Text: "actually I need something else entirely", Final: true})
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("frame = %q, want interrupt (human barge-in final must keep the C5 path)", f.Event)
	}
	f = d.waitFrame(t)
	// The aggregation-mode prompt is NORMALIZED at the single choke
	// point (leaf 03 T3) with the OPEN-VOCABULARY preserve hook — every
	// content token survives, so the dispatch text equals the final
	// here; the point of this pin is that a non-echo human final keeps
	// the full C5 interrupt+dispatch path, not the token math.
	if f.Event != voiceframe.EventPrompt || f.Text != NormalizeUtterance("actually I need something else entirely", openVocabularyPreserve) {
		t.Fatalf("frame = %q %q, want human prompt (normalized) after the interrupt", f.Event, f.Text)
	}
}

// TestShortFinalBypassesEchoGate: a short tail ("call.") carries too
// little provenance to prove it is an echo (echoMinContentTokens floor —
// accepted policy: dedupe + the daemon-side gates carry the short-tail
// cases), so it keeps the full C5 path even though its one content word
// appears inside a recent agent sentence.
func TestShortFinalBypassesEchoGate(t *testing.T) {
	const ccid = "echodrop3"
	const chunks = 12
	d, child, p, _, _, stt := newRMSChild(t, ccid, true, rmsThresh)

	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "call me back tomorrow morning please"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })

	stt.emit(Transcript{Text: "call.", Final: true})
	f := d.waitFrame(t) // C5 interrupt for a speaking final
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("frame = %q, want interrupt", f.Event)
	}
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "call." {
		t.Fatalf("frame = %q %q, want short prompt dispatched through the provenance floor", f.Event, f.Text)
	}
}
