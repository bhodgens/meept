package providers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
)

// writeFakeWhisper installs a whisper-cli stub: each invocation copies the
// -f WAV into captureDir (so tests can inspect what was sent) and prints
// "utt N" to stdout, N counting invocations.
func writeFakeWhisper(t *testing.T) (captureDir string) {
	t.Helper()
	binDir := t.TempDir()
	captureDir = t.TempDir()
	cnt := filepath.Join(binDir, "count")
	script := "#!/bin/sh\n" +
		"n=$(cat \"" + cnt + "\" 2>/dev/null || echo 0)\n" +
		"n=$((n+1)); echo $n > \"" + cnt + "\"\n" +
		"cp \"$4\" \"" + captureDir + "/utt$((n-1)).wav\" 2>/dev/null || echo missing-wav > \"" + captureDir + "/utt$((n-1)).wav\"\n" +
		"echo \"utt $n\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "whisper-cli"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake whisper-cli: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	return captureDir
}

func whisperCfg(sampleRate int) ProviderConfig {
	return ProviderConfig{Vendor: "whisper", Model: "/tmp/fake-model.bin", SampleRate: sampleRate}
}

// chunkOf returns ms milliseconds of s16le mono PCM at 8 kHz (8 samples/ms)
// with the given amplitude (0 = true silence).
func chunkOf(ms int, amplitude int16) []byte {
	n := ms * 8 // 8000 Hz, 8 samples per ms
	b := make([]byte, n*2)
	if amplitude == 0 {
		return b
	}
	for i := range n {
		b[2*i] = byte(amplitude)                //nolint:gosec // G115: test PCM encode of an int16 amplitude (LE low byte)
		b[2*i+1] = byte(uint16(amplitude) >> 8) //nolint:gosec // G115: test PCM encode of an int16 amplitude (LE high byte)
	}
	return b
}

const (
	whisperTestRate  = 8000
	loudAmplitude    = int16(2000) // RMS far above whisperSilenceRMS*32767
	silenceChunkMs   = 200
	minSilenceChunks = 4 // 4 x 200 ms = 800 ms > whisperSilenceMs
	speechChunkMs    = 400
	streamDrainWait  = 3 * time.Second
)

// streamOf starts one Stream and adapts its channel to the test's drain
// helper. The provider contract guarantees the channel closes on pcm
// close, so a drain always terminates.
func streamOf(t *testing.T, s voicechild.STTProvider, pcm <-chan []byte) <-chan voicechild.Transcript {
	t.Helper()
	ch, err := s.Stream(context.Background(), pcm)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	return ch
}

// textsOf maps transcripts to their text for failure messages.
func textsOf(trs []voicechild.Transcript) []string {
	out := make([]string, 0, len(trs))
	for _, tr := range trs {
		out = append(out, tr.Text)
	}
	return out
}

// drainTranscripts collects transcripts until the channel closes or the
// deadline passes.
func drainTranscripts(t *testing.T, ch <-chan voicechild.Transcript) []voicechild.Transcript {
	t.Helper()
	var out []voicechild.Transcript
	deadline := time.After(streamDrainWait)
	for {
		select {
		case tr, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, tr)
		case <-deadline:
			t.Fatalf("timed out draining transcripts (got %d)", len(out))
			return out
		}
	}
}

// T-D: one loud burst + trailing silence yields exactly ONE final
// Transcript and nothing before the silence closes the utterance.
func TestWhisperOneUtteranceOneFinal(t *testing.T) {
	captureDir := writeFakeWhisper(t)
	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	pcm := make(chan []byte, 16)
	go func() {
		pcm <- chunkOf(speechChunkMs, loudAmplitude)
		for range minSilenceChunks {
			pcm <- chunkOf(silenceChunkMs, 0)
		}
		close(pcm)
	}()
	trs := drainTranscripts(t, streamOf(t, s, pcm))
	if len(trs) != 1 {
		t.Fatalf("want exactly 1 final, got %d: %v", len(trs), textsOf(trs))
	}
	if !trs[0].Final || trs[0].Text != "utt 1" {
		t.Fatalf("want final utt 1, got %+v", trs[0])
	}
	if _, err := os.Stat(filepath.Join(captureDir, "utt0.wav")); err != nil {
		t.Fatalf("fake never saw a WAV: %v", err)
	}
}

// Two bursts -> two finals, each with its own WAV.
func TestWhisperTwoUtterances(t *testing.T) {
	writeFakeWhisper(t)
	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	pcm := make(chan []byte, 16)
	go func() {
		for range 2 {
			pcm <- chunkOf(speechChunkMs, loudAmplitude)
			for range minSilenceChunks {
				pcm <- chunkOf(silenceChunkMs, 0)
			}
		}
		close(pcm)
	}()
	trs := drainTranscripts(t, streamOf(t, s, pcm))
	if len(trs) != 2 {
		t.Fatalf("want 2 finals, got %d: %v", len(trs), textsOf(trs))
	}
	for i, want := range []string{"utt 1", "utt 2"} {
		if trs[i].Text != want || !trs[i].Final {
			t.Fatalf("final %d: want %q, got %+v", i, want, trs[i])
		}
	}
}

// T-E: a never-silent source cannot grow the window past the 10 s cap -
// an 11 s burst yields the cap flush plus the tail flush, 2 finals, and
// both WAVs are <= ~10 s of audio (memory bound proof).
func TestWhisperNeverSilentCapHolds(t *testing.T) {
	captureDir := writeFakeWhisper(t)
	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	pcm := make(chan []byte, 16)
	go func() {
		// 11 s of continuous speech in 1 s chunks, never silent.
		for range 11 {
			pcm <- chunkOf(1000, loudAmplitude)
		}
		close(pcm)
	}()
	trs := drainTranscripts(t, streamOf(t, s, pcm))
	if len(trs) != 2 {
		t.Fatalf("want cap flush + tail flush = 2 finals, got %d: %v", len(trs), textsOf(trs))
	}
	// 10 s cap at 8 kHz s16le = 160000 B; allow WAV header (44 B) slack.
	entries, rerr := os.ReadDir(captureDir)
	if rerr != nil {
		t.Fatalf("read capture: %v", rerr)
	}
	for _, e := range entries {
		info, serr := e.Info()
		if serr != nil {
			t.Fatalf("stat %s: %v", e.Name(), serr)
		}
		if info.Size() > 160044 {
			t.Fatalf("%s carries %d bytes (> 10 s cap)", e.Name(), info.Size())
		}
	}
}

// T-F: odd-length chunks never drop the boundary sample: feeding 0xAB 0xCD
// as two single-byte chunks must land the sample 0xABCD in the WAV data.
func TestWhisperOddChunkBoundary(t *testing.T) {
	captureDir := writeFakeWhisper(t)
	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	pcm := make(chan []byte, 16)
	go func() {
		pcm <- []byte{0xAB} // low byte, dangling
		pcm <- []byte{0xCD} // completes the sample with the carry
		for range minSilenceChunks {
			pcm <- chunkOf(silenceChunkMs, 0)
		}
		close(pcm)
	}()
	trs := drainTranscripts(t, streamOf(t, s, pcm))
	if len(trs) != 1 {
		t.Fatalf("want 1 final from the boundary sample burst, got %d: %v", len(trs), textsOf(trs))
	}
	wav, rerr := os.ReadFile(filepath.Join(captureDir, "utt0.wav"))
	if rerr != nil {
		t.Fatalf("read captured wav: %v", rerr)
	}
	data := wav[44:]
	if len(data) != 2 || data[0] != 0xAB || data[1] != 0xCD {
		t.Fatalf("boundary sample mangled: wav data = %x, want abcd", data)
	}
}

// Empty pcm closes with zero transcripts and launches nothing.
func TestWhisperEmptyStream(t *testing.T) {
	writeFakeWhisper(t)
	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	pcm := make(chan []byte)
	close(pcm)
	trs := drainTranscripts(t, streamOf(t, s, pcm))
	if len(trs) != 0 {
		t.Fatalf("want 0 finals, got %d", len(trs))
	}
}

// Constructor guards: model required, binary missing named, keyless OK.
func TestWhisperConstructorGuards(t *testing.T) {
	if _, err := NewSTT(ProviderConfig{Vendor: "whisper", SampleRate: 8000}, ""); !errors.Is(err, ErrLocalModelRequired) {
		t.Fatalf("empty model: want ErrLocalModelRequired, got %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := NewSTT(whisperCfg(8000), ""); !errors.Is(err, ErrWhisperBinMissing) {
		t.Fatalf("missing binary: want ErrWhisperBinMissing, got %v", err)
	}
	writeFakeWhisper(t)
	if _, err := NewSTT(whisperCfg(8000), ""); err != nil { // empty key must work
		t.Fatalf("local vendor with empty key: %v", err)
	}
}

// T-G (link no-dispatch regression, 2026-11-10): a paced producer that
// stops sending bytes WITHOUT closing the stream must still flush the
// open utterance. This is exactly the voice-link shape: the bridge pump
// (internal/voice/link.go pumpStream) forwards only real PCM — when the
// peer stops playing, ZERO chunks arrive — so the audio-ms silence rule
// never trips and every link run recorded 0/0 dispatched turns. The wall-
// idle timer is the fix; this test pins it deterministically with a
// shrunken whisperWallIdle (fake recognizer, no real whisper-cli).
func TestWhisperPacedStreamFlushesWithoutClose(t *testing.T) {
	captureDir := writeFakeWhisper(t)
	old := whisperWallIdle
	whisperWallIdle = 60 * time.Millisecond
	t.Cleanup(func() { whisperWallIdle = old })

	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	pcm := make(chan []byte)
	trs := streamOf(t, s, pcm)

	// Pace 10 x 20 ms chunks at ~real time (200 ms of audio total —
	// 2.5x LESS than the 500 ms audio-ms cutoff, so the old rule could
	// never have flushed this window), then go SILENT: no more chunks,
	// channel stays open.
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for range 10 {
			<-tick.C
			pcm <- chunkOf(20, loudAmplitude)
		}
	}()

	select {
	case tr := <-trs:
		if !tr.Final || tr.Text != "utt 1" {
			t.Fatalf("want final utt 1 from the idle flush, got %+v", tr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("paced stream went idle but no segment flushed without close")
	}
	if _, err := os.Stat(filepath.Join(captureDir, "utt0.wav")); err != nil {
		t.Fatalf("recognizer never received the WAV: %v", err)
	}

	// A second paced burst + close still flushes exactly one more.
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for range 10 {
			<-tick.C
			pcm <- chunkOf(20, loudAmplitude)
		}
		time.Sleep(150 * time.Millisecond) // let the idle flush land first
		close(pcm)
	}()
	got := drainTranscripts(t, trs)
	if len(got) != 1 || got[0].Text != "utt 2" {
		t.Fatalf("want exactly 1 more final (utt 2) after the idle flush, got %v", textsOf(got))
	}
}

// T-H: a CONTINUOUS producer (file-mode copyMicFile shape: bytes always
// in flight) must NOT flush mid-utterance on the wall-idle timer — only
// the audio-ms silence/EOF rules may close it.
func TestWhisperFastStreamNotSplitByIdleTimer(t *testing.T) {
	writeFakeWhisper(t)
	old := whisperWallIdle
	whisperWallIdle = 30 * time.Millisecond
	t.Cleanup(func() { whisperWallIdle = old })

	s, err := newWhisperSTT(whisperCfg(whisperTestRate))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	pcm := make(chan []byte)
	trs := streamOf(t, s, pcm)
	go func() {
		// 3 s of continuous audio pushed faster than real time (a chunk
		// every 2 ms) — well beyond the idle window, but the timer must
		// never fire while bytes keep arriving.
		for range 150 {
			pcm <- chunkOf(20, loudAmplitude)
			time.Sleep(2 * time.Millisecond)
		}
		for range minSilenceChunks {
			pcm <- chunkOf(silenceChunkMs, 0)
		}
		close(pcm)
	}()
	got := drainTranscripts(t, trs)
	if len(got) != 1 {
		t.Fatalf("want 1 final from the silence flush, got %d: %v", len(got), textsOf(got))
	}
}
