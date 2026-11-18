package providers

// BARGE-IN CONTRACT (2026-11-11) — WhisperSTT.FlushNow early-kick tests.
// Fake-binary style per stt_whisper_test.go (writeFakeWhisper): the kick
// must force-recognize an OPEN voiced window well BEFORE the trailing-
// silence/wall-idle rules would close it, and must be a strict no-op
// when nothing voiced is buffered (never inject silence, never launch a
// subprocess).

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// T-FlushNow-1: an open voiced window force-flushes on the kick without
// waiting for the 500 ms silence rule — the recognizer's WAV lands and
// the final arrives early, and the SAME utterance is not re-recognized
// after the kick (the window was reset, so a following silence burst
// emits nothing more).
func TestWhisperFlushNowKicksOpenWindowEarly(t *testing.T) {
	captureDir := writeFakeWhisper(t)
	// Wall-idle far longer than the test's patience window: the ONLY
	// thing that can deliver the final inside 300 ms is the kick.
	old := whisperWallIdle
	whisperWallIdle = 10 * time.Second
	t.Cleanup(func() { whisperWallIdle = old })

	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	ws := s.(*WhisperSTT)
	pcm := make(chan []byte)
	trs := streamOf(t, s, pcm)

	pcm <- chunkOf(200, loudAmplitude) // 200 ms voiced: under the 500 ms audio rule

	start := time.Now()
	ws.FlushNow()
	select {
	case tr := <-trs:
		if !tr.Final || tr.Text != "utt 1" {
			t.Fatalf("want final utt 1 from the kick, got %+v", tr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("FlushNow did not force-recognize the open voiced window")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("kick flush took %v — looks like it waited for the silence rule, not the kick", elapsed)
	}
	if _, err := os.Stat(filepath.Join(captureDir, "utt0.wav")); err != nil {
		t.Fatalf("recognizer never received the kicked WAV: %v", err)
	}

	// The window reset with the flush: trailing silence now closes
	// nothing (voicedSamples back to 0) — no second final.
	for range minSilenceChunks {
		pcm <- chunkOf(silenceChunkMs, 0)
	}
	close(pcm)
	if got := drainTranscripts(t, trs); len(got) != 0 {
		t.Fatalf("post-kick silence re-recognized the flushed utterance: %v", textsOf(got))
	}
}

// T-FlushNow-2 (CONTRACT: FlushNow must NEVER fire on an empty window):
// a kick before ANY audio, and a kick after only un-voiced (silence)
// audio, launch zero recognizer invocations — the fake binary's capture
// dir stays empty and no transcript arrives.
func TestWhisperFlushNowNoOpOnEmptyWindow(t *testing.T) {
	captureDir := writeFakeWhisper(t)
	old := whisperWallIdle
	whisperWallIdle = 10 * time.Second
	t.Cleanup(func() { whisperWallIdle = old })

	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	ws := s.(*WhisperSTT)
	pcm := make(chan []byte)
	trs := streamOf(t, s, pcm)

	ws.FlushNow() // empty: nothing buffered at all
	pcm <- chunkOf(200, 0)
	ws.FlushNow() // silence-only: never voiced -> nothing to recognize
	pcm <- chunkOf(400, loudAmplitude)
	ws.FlushNow() // NOW it is voiced: this kick must land
	select {
	case tr := <-trs:
		if !tr.Final || tr.Text != "utt 1" {
			t.Fatalf("want exactly one final utt 1 from the voiced kick, got %+v", tr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("FlushNow never fired on the open voiced window (empty-kick no-ops must not disable real kicks)")
	}

	close(pcm)
	if got := drainTranscripts(t, trs); len(got) != 0 {
		t.Fatalf("extra transcripts after the kick: %v", textsOf(got))
	}
	// Exactly ONE recognizer invocation across all three kicks.
	entries, rerr := os.ReadDir(captureDir)
	if rerr != nil {
		t.Fatalf("read capture: %v", rerr)
	}
	if len(entries) != 1 {
		t.Fatalf("recognizer ran %d times, want 1 (empty/silence-only kicks must launch nothing)", len(entries))
	}
}

// T-FlushNow-3: the interface the pipeline's optional seam expects —
// WhisperSTT satisfies voicechild's sttFlusher shape (FlushNow()).
func TestWhisperImplementsFlusher(t *testing.T) {
	writeFakeWhisper(t)
	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if _, ok := s.(interface{ FlushNow() }); !ok {
		t.Fatal("WhisperSTT must expose FlushNow() for the pipeline's barge-in early-kick seam")
	}
	var _ = s
}
