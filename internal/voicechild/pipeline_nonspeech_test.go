package voicechild

// Leaf 02 (defect 2026-11-09, call local-2d84c384fd93f447): whisper's
// no-speech markers arrived as FINAL transcripts and were dispatched as
// USER prompts ("[BLANK_AUDIO]" x3, "(sighs)" — the employee answered
// silence), and a repeated exact final RE-STAMPED the RMS barge-in
// evidence, re-authorizing trips that cancelled the very turn the prior
// final had already dispatched. Contracts pinned here:
//
//   - nonSpeechTranscript: case-insensitive exact-match set over the
//     TRIMMED text; consumeTranscripts drops matching finals entirely
//     (zero prompt frames, no evidence stamp, no dedupe state).
//   - exact-duplicate consecutive finals WITHIN the evidence window
//     collapse to one prompt (whisper repeats on re-voiced tails).
//   - the daemon talk loop needs no guard: the child is the single choke
//     point — a dropped duplicate never reaches a prompt frame at all.
//
// Fixture style: driveFinal (pipeline_final_c5_test.go) runs one final
// through consumeTranscripts; the fakeDaemon FIFO observes prompt frames.

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// TestNonSpeechTranscriptTable: the marker set matches on trimmed,
// case-insensitive text; real speech — including speech that merely
// contains a marker-shaped fragment — is never flagged.
func TestNonSpeechTranscriptTable(t *testing.T) {
	dropped := []string{
		"[BLANK_AUDIO]", "[blank_audio]", "<blank_audio>", "<BLANK AUDIO>",
		"  [BLANK_AUDIO]  ", "[blank audio]",
		"(sighs)", "(Sigh)", "(sigh)", "(LAUGHS)", "(laughing)", "(Laughs)",
		"(no speech)", "[no speech]", "[No Speech]",
		"(background noise)", "(background noises)", "[background noise]", "[BACKGROUND NOISES]",
		"   ", "\t \n",
	}
	for _, s := range dropped {
		if !nonSpeechTranscript(s) {
			t.Errorf("nonSpeechTranscript(%q) = false, want true", s)
		}
	}
	kept := []string{
		"Hello", "what time is it", "hey (laughs) that's funny",
		"[BLANK_AUDIO] please", "(sighs) loudly", "sighs", "blank audio",
		"the audio", "no speech detected by me",
	}
	for _, s := range kept {
		if nonSpeechTranscript(s) {
			t.Errorf("nonSpeechTranscript(%q) = true, want false (exact-match set only)", s)
		}
	}
}

// countQueuedPrompts counts prompt frames in the fake daemon FIFO
// without popping (total-across-scenario assertions, same style as
// countQueuedInterrupts).
func countQueuedPrompts(d *fakeDaemon) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, f := range d.queue {
		if f.Event == voiceframe.EventPrompt {
			n++
		}
	}
	return n
}

// finalEvidence reads (lastFinalAt zero?, lastFinalText) under playMu —
// the same regime consumeTranscripts stamps under.
func finalEvidence(p *pipeline) (zero bool, text string) {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return p.lastFinalAt.IsZero(), p.lastFinalText
}

// TestBlankAudioFinalDropped: a final that is only a whisper no-speech
// marker produces ZERO prompt frames, stamps NO RMS evidence, and records
// NO dedupe state; a genuine speech final still dispatches afterwards.
func TestBlankAudioFinalDropped(t *testing.T) {
	const ccid = "nonspeech1"
	d, _, p, _, _, _ := newRMSChild(t, ccid, true, rmsThresh)

	driveFinal(p, ccid, "[BLANK_AUDIO]")
	driveFinal(p, ccid, "  (sighs)  ")
	time.Sleep(100 * time.Millisecond) // absence window (fixture style)
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("non-speech finals emitted %d prompt frame(s), want 0 (must never dispatch a turn)", n)
	}
	if zero, text := finalEvidence(p); !zero || text != "" {
		t.Fatalf("non-speech finals stamped RMS evidence (zero=%v lastFinalText=%q); drops must not re-authorize barge-in trips", zero, text)
	}
	if p.interrupted {
		t.Fatal("non-speech final latched a barge-in; drops must not touch the latch")
	}

	// Real speech still dispatches normally after the drops.
	driveFinal(p, ccid, "actually, wait")
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "actually, wait" || f.Last == nil || !*f.Last {
		t.Fatalf("post-drop final = %q %q last=%v, want final prompt \"actually, wait\"", f.Event, f.Text, f.Last)
	}
}

// TestDuplicateFinalWithinWindowDropped: a final identical (after trim)
// to the last dispatched final within the evidence window is whisper's
// repeat of the SAME utterance — one prompt, no second turn, no second
// evidence stamp. A different final still dispatches; after the window
// expires the same text is treated as new speech again.
func TestDuplicateFinalWithinWindowDropped(t *testing.T) {
	const ccid = "nonspeech2"
	d, _, p, _, _, _ := newRMSChild(t, ccid, true, rmsThresh)

	driveFinal(p, ccid, "what time is it")
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	f := d.waitFrame(t) // pop the first prompt before asserting the next
	if f.Event != voiceframe.EventPrompt || f.Text != "what time is it" {
		t.Fatalf("first final = %q %q, want prompt \"what time is it\"", f.Event, f.Text)
	}
	_, first := finalEvidence(p)
	if first != "what time is it" {
		t.Fatalf("lastFinalText = %q, want %q", first, "what time is it")
	}

	// Exact repeat, and a whitespace-variant repeat: both collapsed
	// (first prompt popped above, so the FIFO must hold NOTHING new).
	driveFinal(p, ccid, "what time is it")
	driveFinal(p, ccid, "  what time is it  ")
	time.Sleep(100 * time.Millisecond)
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("duplicate finals produced %d extra prompt frame(s), want 0 (repeat of the same utterance must not dispatch a second turn)", n)
	}

	// A DIFFERENT final dispatches (dedupe is exact-match only).
	driveFinal(p, ccid, "and the weather")
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "and the weather" {
		t.Fatalf("distinct final after duplicate = %q %q, want prompt \"and the weather\"", f.Event, f.Text)
	}

	// Outside the evidence window the same text is new speech again.
	rmsGateEvidence(p, rmsSttEvidenceWindow+time.Second)
	driveFinal(p, ccid, "and the weather")
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "and the weather" {
		t.Fatalf("stale-window repeat = %q %q, want prompt dispatched (window expired)", f.Event, f.Text)
	}
}
