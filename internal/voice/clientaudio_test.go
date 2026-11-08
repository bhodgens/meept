package voice

// clientaudio_test.go - file-mode reader + speaker-sink burst logic
// (local-leg leaf 08, T3). No microphone, no real player: the sink takes
// a Player seam and the capture is either a file reader or the ffmpeg
// pipe (pipe path only compile-checked; the tests drive file mode and
// the GOOS refusal).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakePlayer records Play calls and honors Clear.
type fakePlayer struct {
	bursts [][]byte
	playN  int
	closed bool
}

func (f *fakePlayer) Play(b []byte) error {
	f.playN++
	f.bursts = append(f.bursts, append([]byte(nil), b...))
	return nil
}
func (f *fakePlayer) Clear() { f.bursts = nil }
func (f *fakePlayer) Close() { f.closed = true }

func wavHeader(pcmLen int) []byte {
	return NewWAVHeader(16000, 1, pcmLen)
}

func TestWAVHeaderShape(t *testing.T) {
	pcm := bytes.Repeat([]byte{0x01, 0x02}, 160) // 320 bytes = 10ms @16k mono s16
	h := wavHeader(len(pcm))
	if len(h) != 44 {
		t.Fatalf("want 44-byte header, got %d", len(h))
	}
	if string(h[0:4]) != "RIFF" || string(h[8:12]) != "WAVE" {
		t.Fatalf("missing RIFF/WAVE magic: %v", h[0:12])
	}
	if string(h[36:40]) != "data" {
		t.Fatalf("missing data chunk id: %v", h[36:40])
	}
	// data size == pcmLen (little endian, last 4 bytes)
	want := uint32(len(pcm)) //nolint:gosec // G115: test WAV data-length; pcm is bounded by the fixture
	got := uint32(h[40]) | uint32(h[41])<<8 | uint32(h[42])<<16 | uint32(h[43])<<24
	if got != want {
		t.Fatalf("data size %d, want %d", got, want)
	}
}

func TestSpeakerSinkBurst(t *testing.T) {
	fp := &fakePlayer{}
	sink := NewSpeakerSink(fp, 50*time.Millisecond)
	// A burst: write PCM, flush, then silence past the gap -> one WAV handed to Play.
	if _, err := sink.Write([]byte("pcm-pcm")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if fp.playN != 1 {
		t.Fatalf("want 1 play after flush, got %d", fp.playN)
	}
	if got := fp.bursts[0]; !bytes.HasPrefix(got, []byte("RIFF")) || !bytes.HasSuffix(got, []byte("pcm-pcm")) {
		t.Fatalf("burst is not header+pcm: %d bytes", len(got))
	}
	// Second burst after the gap -> second Play.
	time.Sleep(60 * time.Millisecond)
	if _, err := sink.Write([]byte("more")); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush 2: %v", err)
	}
	if fp.playN != 2 {
		t.Fatalf("want 2 plays, got %d", fp.playN)
	}
	// Clear drops the pending burst.
	if _, err := sink.Write([]byte("pending")); err != nil {
		t.Fatalf("write 3: %v", err)
	}
	sink.Clear()
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush 3: %v", err)
	}
	if fp.playN != 2 {
		t.Fatalf("clear must drop pending burst: playN=%d", fp.playN)
	}
	sink.Close()
	if !fp.closed {
		t.Fatalf("close must close the player seam")
	}
}

func TestSpeakerSinkZeroPlayer(t *testing.T) {
	// nil player = a sink that discards (never panics).
	sink := NewSpeakerSink(nil, time.Second)
	if _, err := sink.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func TestRawMicCaptureFileMode(t *testing.T) {
	// A WAV file on disk: capture wraps it as a raw s16le stream (the 44-byte
	// header stripped), so io.Copy into the socket carries pure PCM.
	pcm := bytes.Repeat([]byte{0x07, 0x08}, 100)
	wav := append(wavHeader(len(pcm)), pcm...)
	path := t.TempDir() + "/in.wav"
	if err := os.WriteFile(path, wav, 0o600); err != nil {
		t.Fatalf("write wav: %v", err)
	}
	cap, err := NewRawMicCapture(path, "")
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	defer cap.Close()
	got, err := io.ReadAll(cap)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, pcm) {
		t.Fatalf("want raw pcm, got %d bytes with prefix %v", len(got), got[:min(4, len(got))])
	}
}

func TestRawMicCaptureMissingFile(t *testing.T) {
	if _, err := NewRawMicCapture("/nonexistent/x.wav", ""); err == nil {
		t.Fatalf("want error for missing file")
	}
}

func TestRawMicCaptureLiveUnsupportedOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only refusal check")
	}
	_, err := NewRawMicCapture("", "")
	if err == nil || !strings.Contains(err.Error(), "mic capture unsupported on linux") {
		t.Fatalf("want named GOOS refusal, got %v", err)
	}
	// The error must carry the --in hint.
	if !strings.Contains(err.Error(), "--in") {
		t.Fatalf("refusal must name the --in fallback: %v", err)
	}
}

func TestRawMicCaptureCtxCancelKillsFFmpeg(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: avfoundation pipe path")
	}
	// :0 audio-only input via the pipe; cancel must reap ffmpeg (no orphan).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	cap, err := newRawMicCaptureCtx(ctx, "")
	if err != nil {
		cancel()
		t.Skipf("no mic/ffmpeg on this host: %v", err)
	}
	cancel()
	// Close() cancels AND waits: until Wait reaps it, a killed process
	// is a zombie and Signal(0) still succeeds.
	if err := cap.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cap.mu.Lock()
		cmd := cap.cmd
		cap.mu.Unlock()
		if cmd == nil || cmd.Process == nil {
			break
		}
		// Signal 0 probes existence: err means reaped.
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cap.mu.Lock()
	cmd := cap.cmd
	cap.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
			t.Fatalf("ffmpeg survived ctx cancel (orphan)")
		}
	}
	_ = exec.Command // keep exec import for future arg assertions
	_ = errors.Is    // keep errors import referenced on all GOOSes
}
