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
	"strings"
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

// ---------------------------------------------------------------------------
// Leaf 01 (defect 2026-11-10, call local-376ee373022f7663): the exact set
// above only covers a fixed list; whisper emits DOZENS of paralinguistic
// bracket tokens — "(farting)", "(mouse clicking)", "(squeaking)",
// "[clears throat]", "(inhales deeply)" were all dispatched as user prompts
// and the companion answered each. The guard is now CLASS-based:
// isRealSpeech strips every bracketed-token run and drops finals whose
// residue is empty/punctuation-only. Contracts pinned below.
// ---------------------------------------------------------------------------

// TestIsRealSpeechClassTable: the SHAPE class, not the list. The five
// verbatim live-call tokens plus the whisper common set drop; real words
// outside any bracket keep the transcript dispatching.
func TestIsRealSpeechClassTable(t *testing.T) {
	dropped := []string{
		// the 5 verbatim tokens from live call local-376ee373022f7663:
		"(farting)", "(mouse clicking)", "(squeaking)", "[clears throat]", "(inhales deeply)",
		// >=10 more observed/live-shaped whisper paralinguistic tokens:
		"(laughing)", "(crosstalk)", "(upbeat music)", "(silence)", "(wind)",
		"(bell ringing)", "[music]", "(applause)", "(gasps)", "(clears throat)",
		"(static)",
		// multiple bracket tokens in one final:
		"(gasps) (applause)", "[clears throat] (inhales deeply)",
		// nested brackets: the run ends at the FIRST closer, so the whole
		// artifact (including the inner opener) strips:
		"[music (fade)]", "[music (fade)", "(())", "[ (in brackets) ]",
		// asterisk markers (voice-microfrag, live call
		// local-0627960006a7f3d9: whisper's *...* class — the bracket-
		// only strip dispatched "*unintelligible*" as seq=1 user prompt):
		"*unintelligible*", "*static*", "*music*", "*silence*",
		"*laughter* *applause*", // multiple asterisk runs in one final
		// punctuation/whitespace-only residue:
		" .", "()", "  ", "... ---", "?! ,",
	}
	for _, s := range dropped {
		if isRealSpeech(s) {
			t.Errorf("isRealSpeech(%q) = true, want false (bracket/punctuation-only final)", s)
		}
	}
	kept := []string{
		// mixed: bracket artifact + real words outside it -> the caller
		// spoke, dispatch.
		"hey (crosstalk) what's that",
		"ok (thanks) bye",
		"[BLANK_AUDIO] please", // residue "please" is speech
		// single voiced words are speech even when short/filler — only
		// bracketed and blank forms drop:
		"yeah", "mmm", "How's it going?", "Still here.", "Yeah, how are you doing today?",
		// unbalanced opener with NO closer anywhere: matches nothing,
		// stays as word residue (errs toward hearing the caller):
		"hello [music (fade",
		// asterisk shape keeps (voice-microfrag): mixed artifact + real
		// words outside the run dispatch; an unclosed run and a lone "*"
		// (no closer -> matches nothing) stay REAL — the caller might
		// have said something the shape rule cannot prove is an artifact:
		"hey *music* what's that",
		"stop * that noise",
		"*",
		"hello *",
		// parenthetical beyond the 60-char cap: shape says it carries
		// real words, keep:
		"(a very long parenthetical sentence that exceeds the sixty character token cap by a wide margin)",
	}
	for _, s := range kept {
		if !isRealSpeech(s) {
			t.Errorf("isRealSpeech(%q) = false, want true (real words outside bracket artifacts)", s)
		}
	}
}

// TestTranscriptPreviewBounded: the drop-log preview is capped at 40 runes
// (shorter text passes through unchanged).
func TestTranscriptPreviewBounded(t *testing.T) {
	long := "(a paralinguistic description well beyond forty characters for sure)"
	got := transcriptPreview(long)
	if len([]rune(got)) > 40 {
		t.Fatalf("preview %q is %d runes, want <= 40", got, len([]rune(got)))
	}
	if long[:10] != transcriptPreview(long[:10]) {
		t.Fatalf("short text must pass through unchanged: %q vs %q", long[:10], transcriptPreview(long[:10]))
	}
}

// drivePartial runs one Partial transcript through consumeTranscripts (the
// driveFinal seam with Final=false).
func drivePartial(p *pipeline, ccid, text string) {
	trans := make(chan Transcript, 1)
	trans <- Transcript{Text: text, Final: false}
	close(trans)
	done := make(chan struct{})
	go func() {
		p.consumeTranscripts(trans)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testPollDeadline):
		p.cancel()
		<-done
	}
}

// TestBracketTokenFinalDropped: a paralinguistic bracket final emits ZERO
// prompt frames, stamps NO RMS evidence, records NO dedupe state, logs
// exactly one INFO drop line per final (call_id + bounded preview), and a
// genuine speech final still dispatches afterwards.
func TestBracketTokenFinalDropped(t *testing.T) {
	const ccid = "deadair1"
	d, _, p, _, _, _ := newRMSChild(t, ccid, true, rmsThresh)
	logger := newCaptureLogger()
	p.logger = logger.logger()

	drops := []string{"(farting)", "(mouse clicking)", "[clears throat]", " .", "()", "[music (fade)]"}
	for _, s := range drops {
		driveFinal(p, ccid, s)
	}
	time.Sleep(100 * time.Millisecond) // absence window (fixture style)
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("bracket-only finals emitted %d prompt frame(s), want 0 (must never dispatch a turn)", n)
	}
	if zero, text := finalEvidence(p); !zero || text != "" {
		t.Fatalf("bracket finals stamped RMS evidence (zero=%v lastFinalText=%q); drops must not re-authorize barge-in trips or feed dedupe state", zero, text)
	}
	if p.interrupted {
		t.Fatal("bracket-only final latched a barge-in; drops must not touch the latch")
	}
	// One INFO line per dropped final, naming the reason + call_id.
	if n := strings.Count(logger.everything(), "voicechild: non-speech final dropped"); n != len(drops) {
		t.Fatalf("drop log lines = %d, want %d (one INFO per dropped final)", n, len(drops))
	}
	if !strings.Contains(logger.everything(), "call_id="+ccid) {
		t.Fatalf("drop log missing call_id; got:\n%s", logger.everything())
	}

	// Real speech still dispatches after the drops (dedupe state was
	// never touched by them).
	driveFinal(p, ccid, "Still here.")
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "Still here." || f.Last == nil || !*f.Last {
		t.Fatalf("post-drop final = %q %q last=%v, want final prompt \"Still here.\"", f.Event, f.Text, f.Last)
	}
}

// TestDropLogPreviewBounded: an over-long bracket final is logged with a
// preview capped at 40 runes — the full text must NOT hit the log. (The
// token body stays within the 60-char class cap so it still drops.)
func TestDropLogPreviewBounded(t *testing.T) {
	const ccid = "deadair-preview"
	long := "(mouse clicking on the keyboard repeatedly while scrolling)"
	d, _, p, _, _, _ := newRMSChild(t, ccid, true, rmsThresh)
	logger := newCaptureLogger()
	p.logger = logger.logger()

	driveFinal(p, ccid, long)
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("bracket final emitted %d prompt frame(s), want 0", n)
	}
	text := logger.everything()
	if !strings.Contains(text, "voicechild: non-speech final dropped") {
		t.Fatalf("missing drop log line; got:\n%s", text)
	}
	if strings.Contains(text, "while scrolling)") {
		t.Fatalf("drop log carries text past the 40-rune preview cap:\n%s", text)
	}
}

// TestPartialNeverStampsEvidence: the RMS barge-in evidence stamp is
// FINALS-ONLY — a partial (bracket-shaped or real text) must never write
// lastFinalAt/lastFinalText (a loudness-only RMS trip stays suppressed no
// matter what previews flowed).
func TestPartialNeverStampsEvidence(t *testing.T) {
	const ccid = "deadair-partial"
	d, _, p, _, _, _ := newRMSChild(t, ccid, true, rmsThresh)

	drivePartial(p, ccid, "still here no")
	drivePartial(p, ccid, "(farting)")
	if zero, text := finalEvidence(p); !zero || text != "" {
		t.Fatalf("partial stamped RMS evidence (zero=%v lastFinalText=%q); stamping must stay finals-only", zero, text)
	}
	// Partials stay preview prompts — the drop guard is finals-only by
	// contract (the daemon never starts a turn on one). emitPrompt sets
	// Last ONLY for finals (keys.go), so a partial carries no last flag.
	for i := 0; i < 2; i++ {
		f := d.waitFrame(t)
		if f.Event != voiceframe.EventPrompt || f.Last != nil {
			t.Fatalf("partial %d = %q %q last=%v, want preview prompt (no last flag)", i, f.Event, f.Text, f.Last)
		}
	}
}

// TestLiveCallDeadAirFixture: replay the user finals of live call
// local-376ee373022f7663 (the 12 transcript rows = 6 user finals + the 6
// companion answers they wrongly triggered) through consumeTranscripts.
// Guarded, exactly the 2 real turns dispatch a prompt frame; the 4
// bracket-only finals drop silently.
func TestLiveCallDeadAirFixture(t *testing.T) {
	const ccid = "local-376ee373022f7663"
	d, _, p, _, _, _ := newRMSChild(t, ccid, true, rmsThresh)
	logger := newCaptureLogger()
	p.logger = logger.logger()

	// The 6 user rows verbatim, in call order.
	userRows := []string{
		"(farting)",
		"Still here.",
		"(mouse clicking)",
		"(squeaking)",
		"[clears throat]",
		"Yeah, how are you doing today?",
	}
	for _, row := range userRows {
		driveFinal(p, ccid, row)
	}
	// The two keepers arrive asynchronously through the daemon scanner;
	// the four bracket finals emit nothing at all. waitFor pins "at
	// least 2", the absence window pins "exactly 2" (fixture style).
	waitFor(t, func() bool { return countQueuedPrompts(d) == 2 })
	time.Sleep(100 * time.Millisecond)
	if n := countQueuedPrompts(d); n != 2 {
		t.Fatalf("fixture dispatched %d prompt frame(s), want exactly 2 (the dead-air loop answered 6 turns; the guard must leave only real speech)", n)
	}
	want := []string{"Still here.", "Yeah, how are you doing today?"}
	for i, text := range want {
		f := d.waitFrame(t)
		if f.Event != voiceframe.EventPrompt || f.Text != text || f.Last == nil || !*f.Last {
			t.Fatalf("fixture prompt %d = %q %q last=%v, want final prompt %q", i, f.Event, f.Text, f.Last, text)
		}
	}
	if n := strings.Count(logger.everything(), "voicechild: non-speech final dropped"); n != 4 {
		t.Fatalf("drop log lines = %d, want 4 (the four bracket rows)", n)
	}
}

// TestMixedBracketKeepsDispatch: the pins from the leaf contract — a
// transcript mixing a bracket artifact with real words dispatches with the
// ORIGINAL text (the guard drops, never rewrites), and a single voiced
// filler word ("yeah", "mmm") is speech. A punctuation-only final drops.
func TestMixedBracketKeepsDispatch(t *testing.T) {
	const ccid = "deadair-mixed"
	d, _, p, _, _, _ := newRMSChild(t, ccid, true, rmsThresh)

	keeps := []string{"hey (crosstalk) what's that", "yeah", "How's it going?", "mmm"}
	for _, text := range keeps {
		driveFinal(p, ccid, text)
		f := d.waitFrame(t)
		if f.Event != voiceframe.EventPrompt || f.Text != text || f.Last == nil || !*f.Last {
			t.Fatalf("mixed keep %q = %q %q last=%v, want final prompt with original text", text, f.Event, f.Text, f.Last)
		}
	}
	for _, text := range []string{" .", "()"} {
		driveFinal(p, ccid, text)
	}
	time.Sleep(100 * time.Millisecond) // absence window (fixture style)
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("punctuation-only finals emitted %d prompt frame(s), want 0", n)
	}
}
