package voicechild

// Defect 2026-11-14 pins: (a) the phrase/reply EXEMPT paths of the
// short-final gate consume the utterance-energy accumulator (the stale
// mixed-window defect: a loud dispatched utterance followed by an exempt
// "thanks" left the loud energy parked in the window, and the next quiet
// TV fragment was exempted as "energy"); (b) the RMS sustained window is
// sized off the FIXED codec-derived per-chunk sample constant, not the
// last chunk's byte length (short unix-socket reads must not shrink the
// bar); (c) the echo window ignores punctuation-only tokens in BOTH the
// recorded say text and the judged final — two dead tokens normalized to
// empty and scored 1.0 under fuzzy.Similarity, so agent say-text
// punctuation free-matched a whisper's ellipses and falsely cleared the
// provenance gate.

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// TestShortFinalExemptConsumesEnergyWindow: loud dispatched utterance ->
// exempt phrase ("thanks") -> quiet fragment. The quiet fragment must
// DROP (reason=energy): the exempt paths consumed the loud window, so its
// stale energy cannot rescue the next quiet final.
func TestShortFinalExemptConsumesEnergyWindow(t *testing.T) {
	const ccid = "gate-energy-consume"
	p := &pipeline{ccid: ccid, logger: newCaptureLogger().logger()}
	p.applyKeys(KeysFrame{MinTurnWords: 3,
		ExemptPhrases: []string{"thanks"},
		EnergyGate:    true, MinRMS: 3000,
		ReplyExemptS: 30})

	loud := constantChunk(loudSample)   // well over the 3000 floor
	quiet := constantChunk(quietSample) // well under it

	// 1. A loud multi-word utterance accumulates the window and PASSES
	//    (6 words >= base 3), resetting the streak. (The pass branch's
	//    own consume is the pre-existing posture.)
	for range 12 {
		p.noteUtteranceEnergy(loud)
	}
	if v, _, _ := p.evaluateShortFinal("please order the plumbing fixtures"); v != gatePass {
		t.Fatalf("loud 6-word final must pass, got %v", v)
	}

	// 2. Refill the window LOUD, then send an exempt phrase — the
	//    defect left this loud energy parked in the accumulator.
	for range 12 {
		p.noteUtteranceEnergy(loud)
	}
	if v, _, r := p.evaluateShortFinal("thanks."); v != gateExempt || r != "phrase" {
		t.Fatalf("exempt phrase: %v/%q, want exempt phrase", v, r)
	}

	// 3. The quiet fragment must now DROP as energy — under the fix the
	//    accumulator only holds the quiet chunk's samples (12 quiet
	//    chunks, far below the floor). Pre-fix, the parked loud window
	//    exempted it (reason=energy on the EXEMPTED line).
	for range 12 {
		p.noteUtteranceEnergy(quiet)
	}
	v, _, r := p.evaluateShortFinal("nice weather")
	if v != gateDrop || r != "energy" {
		t.Fatalf("quiet fragment after loud-then-exempt: %v/%q, want drop energy (stale loud window must be consumed)", v, r)
	}

	// 4. The reply-exempt path consumes too: refill loud, exempt as a
	//    reply (lastSayActivity is fresh), then expire the reply window
	//    so the next quiet final is judged on ENERGY again — it must
	//    drop.
	for range 12 {
		p.noteUtteranceEnergy(loud)
	}
	p.playMu.Lock()
	p.lastSayActivity = time.Now()
	p.playMu.Unlock()
	if v, _, r := p.evaluateShortFinal("ok"); v != gateExempt || r != "reply" {
		t.Fatalf("reply exempt: %v/%q, want exempt reply", v, r)
	}
	p.playMu.Lock()
	p.lastSayActivity = time.Now().Add(-time.Duration(p.replyExemptS) * time.Second * 2)
	p.playMu.Unlock()
	for range 12 {
		p.noteUtteranceEnergy(quiet)
	}
	if v, _, r := p.evaluateShortFinal("pretty much"); v != gateDrop || r != "energy" {
		t.Fatalf("quiet fragment after loud-then-reply-exempt: %v/%q, want drop energy", v, r)
	}
}

// TestRMSWindowSizedOffFixedChunkSamples: short unix-socket reads must
// not shrink the sustained window. Ten tiny loud slivers (1 sample each
// after the odd-byte ignore — 10 samples total, 3% of the fixed 3200
// sample bar) must NOT trip, while the same count of FULL chunks does.
// Deterministic pre/post on one pipeline: the short-read sequence must
// leave the run under the fixed bar (the pre-fix bar was 10*last=10
// samples and tripped).
func TestRMSWindowSizedOffFixedChunkSamples(t *testing.T) {
	const ccid = "rms-shortread"
	p := &pipeline{ccid: ccid, logger: newCaptureLogger().logger()}
	p.local = true
	p.bargeInRms = rmsThresh
	p.speaking = true

	// Ten short 3-byte reads (one s16 sample each) of full-scale loud.
	sliver := []byte{0xff, 0x7f, 0x00}
	for range rmsWindowChunks {
		p.rmsBargeInCheck(sliver)
	}
	p.playMu.Lock()
	underBar := p.rmsRun < rmsWindowChunks*rmsChunkSamples
	runSamples := p.rmsRun
	p.playMu.Unlock()
	if !underBar {
		t.Fatalf("short reads must not reach the FIXED window bar: rmsRun=%d samples (bar %d) — a 10-sliver window is not 200 ms of loudness",
			runSamples, rmsWindowChunks*rmsChunkSamples)
	}

	// Same call, full chunks: the fixed bar is reachable and the echo
	// guard resets the run at the boundary (observed via the run
	// dropping below the bar again).
	full := make([]byte, 640)
	for i := 0; i+1 < len(full); i += 2 {
		full[i] = byte(callerSample & 0xff)
		full[i+1] = byte((callerSample >> 8) & 0xff)
	}
	for range rmsWindowChunks + 1 {
		p.rmsBargeInCheck(full)
	}
	p.playMu.Lock()
	defer p.playMu.Unlock()
	if p.rmsRun >= rmsWindowChunks*rmsChunkSamples {
		t.Fatalf("window boundary must reset the run: rmsRun=%d still over the bar", p.rmsRun)
	}
}

// TestRMSWindowShortReadsDelayTripLive drives the SHORT READS through the
// real .in socket pump with full evidence stamped: the pre-fix behavior
// tripped the interrupt on the sliver window (the bar shrank to the last
// read's sample count); the fix keeps the window at the codec-derived
// 200 ms, so the same slivers stay well under it and no interrupt is
// emitted. Full chunks under the identical evidence DO trip (the lane
// still works).
func TestRMSWindowShortReadsDelayTripLive(t *testing.T) {
	const ccid = "rms-shortread-live"
	const chunks = 12
	d, child, p, in, _, _ := newRMSChild(t, ccid, true, rmsThresh)
	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "the answer is"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	stampRecentFinal(p)
	setFinalText(p, "absolutely certain fresh caller words") // non-echo provenance

	// Phase 1: full-scale loud delivered as 3-byte short reads. The
	// pre-fix code tripped here (bar = 10 * 1 sample); the fixed bar
	// (3200 samples) is unreachable — no interrupt.
	sliver := []byte{0xff, 0x7f, 0x00}
	for range 40 {
		if _, err := in.Write(sliver); err != nil {
			t.Fatalf("write sliver: %v", err)
		}
	}
	assertNoInterruptWithin(t, d, 80*time.Millisecond,
		"short unix-socket reads shrank the RMS window into an early trip")

	// Phase 2: the same loudness as full chunks trips (control — the
	// sustained window still works off fixed sizing).
	writeMic(t, in, callerSample, chunks)
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("full loud window emitted %q, want interrupt", f.Event)
	}
	close(gate.park)
}

// TestEchoWindowIgnoresPunctuationSayText: the echo window's stored say
// text carries punctuation, but the provenance match runs over content
// tokens — a final whose only "overlap" with a stored say is
// punctuation ("..." vs ",") must NOT be called an echo (pre-fix,
// empty-normalizing tokens scored 1.0 against each other through
// fuzzy.Similarity and the containment ratio inflated).
func TestEchoWindowIgnoresPunctuationSayText(t *testing.T) {
	p := &pipeline{ccid: "echopunct"}
	p.playMu.Lock()
	p.echoWin.add("...(pauses)...")
	p.echoWin.add("Wow, -- really?!")
	p.playMu.Unlock()

	// A caller final sharing NOTHING with the stored says must not
	// match, even when wrapped in the same punctuation shapes.
	p.playMu.Lock()
	matched := p.echoWin.matches("... ... ...")
	matched2 := p.echoWin.matches("the grout sealer needs two coats tonight")
	p.playMu.Unlock()
	if matched || matched2 {
		t.Fatalf("punctuation/dissimilar finals must not echo-match: %v/%v", matched, matched2)
	}

	// A genuine echo of the say text still matches (control): the final
	// carries the say's content words plus 4+ letter filler to clear the
	// provenance floor.
	p.playMu.Lock()
	genuine := p.echoWin.matches("really definitely")
	p.playMu.Unlock()
	if !genuine {
		t.Fatal("genuine echo of stored say text must still match the window")
	}
}
