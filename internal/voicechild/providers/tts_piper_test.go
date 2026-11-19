package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// writeFakeBin installs a POSIX-shell stub binary named name in a fresh
// temp dir and prepends that dir to PATH for the test, so the local
// adapters exercise real subprocess plumbing (pipes, exit codes, context
// kill) against a fake instead of the real tool (leaf 10 hermetic rule).
func writeFakeBin(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

// writePiperModel lays down model.onnx plus the piper sidecar config that
// declares audio.sample_rate, and returns the .onnx path.
func writePiperModel(t *testing.T, sampleRate int) string {
	t.Helper()
	dir := t.TempDir()
	onnx := filepath.Join(dir, "model.onnx")
	if err := os.WriteFile(onnx, []byte("fake-onnx"), 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	conf := `{"audio": {"sample_rate": ` + strconv.Itoa(sampleRate) + `}}`
	if err := os.WriteFile(filepath.Join(dir, "model.onnx.json"), []byte(conf), 0o600); err != nil {
		t.Fatalf("write model config: %v", err)
	}
	return onnx
}

func piperCfg(model string, sampleRate int) ProviderConfig {
	return ProviderConfig{Vendor: "piper", Model: model, SampleRate: sampleRate}
}

// T-A: constructor guards. Local vendor needs NO key; empty Model is the
// named ErrLocalModelRequired; a missing binary is ErrPiperBinMissing and
// is a CONSTRUCTION error (the child and other calls survive).
func TestPiperConstructorGuards(t *testing.T) {
	if _, err := NewTTS(ProviderConfig{Vendor: "piper"}, ""); !errors.Is(err, ErrLocalModelRequired) {
		t.Fatalf("empty model: want ErrLocalModelRequired, got %v", err)
	}
	t.Setenv("PATH", t.TempDir()) // empty PATH: no piper anywhere
	if _, err := NewTTS(piperCfg("/tmp/model.onnx", 16000), ""); !errors.Is(err, ErrPiperBinMissing) {
		t.Fatalf("missing binary: want ErrPiperBinMissing, got %v", err)
	}
	writeFakeBin(t, "piper", "exit 0\n")
	p, err := NewTTS(piperCfg("/tmp/model.onnx", 16000), "") // empty key must work
	if err != nil {
		t.Fatalf("local vendor with empty key: %v", err)
	}
	if p == nil {
		t.Fatalf("nil provider")
	}
}

// T-B: first audio bytes reach the caller while piper is still running
// (proves streaming, not whole-buffer), and the full text is streamed.
func TestPiperSpeakStreamsIncrementally(t *testing.T) {
	writeFakeBin(t, "piper", "printf 'AAAAAAAA'\nsleep 0.4\nprintf 'BBBBBBBB'\n")
	model := writePiperModel(t, 16000)
	p, err := newPiperTTS(piperCfg(model, 16000))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	rc, err := p.Speak(context.Background(), "hello there")
	if err != nil {
		t.Fatalf("speak: %v", err)
	}
	first := make([]byte, 8)
	deadline := time.Now().Add(250 * time.Millisecond) // fake sleeps 400 ms mid-stream
	var got []byte
	for len(got) < 8 {
		if time.Now().After(deadline) {
			t.Fatalf("no first bytes before the subprocess slept: got %q", got)
		}
		n, rerr := rc.Read(first[len(got):8])
		if rerr != nil {
			t.Fatalf("first read: %v", rerr)
		}
		got = append(got, first[:n]...)
	}
	if string(got) != "AAAAAAAA" {
		t.Fatalf("first bytes: want AAAAAAAA, got %q", got)
	}
	rest, rerr := io.ReadAll(rc)
	if rerr != nil {
		t.Fatalf("read rest: %v", rerr)
	}
	if string(rest) != "BBBBBBBB" {
		t.Fatalf("rest: want BBBBBBBB, got %q", rest)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close after clean exit: %v", err)
	}
}

// T-C: non-zero exit surfaces the named ErrPiperExit with a stderr tail.
func TestPiperNonZeroExitNamed(t *testing.T) {
	writeFakeBin(t, "piper", "echo boom >&2\nexit 3\n")
	model := writePiperModel(t, 16000)
	p, err := newPiperTTS(piperCfg(model, 16000))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	rc, err := p.Speak(context.Background(), "hello")
	if err != nil {
		t.Fatalf("speak: %v", err)
	}
	_, rerr := io.ReadAll(rc)
	if !errors.Is(rerr, ErrPiperExit) {
		t.Fatalf("want ErrPiperExit, got %v", rerr)
	}
	if !errors.Is(rc.Close(), ErrPiperExit) {
		t.Fatalf("Close after failed exit: want ErrPiperExit, got %v", rc.Close())
	}
}

// T-D: context cancel kills the subprocess (no orphan, no zombie).
func TestPiperCtxCancelKillsChild(t *testing.T) {
	dir := writeFakeBin(t, "piper", "echo started > \"$PJPID.started\"\necho $$ > \"$PJPID\"\nsleep 5\n")
	pidPath := filepath.Join(dir, "pid")
	t.Setenv("PJPID", pidPath)
	model := writePiperModel(t, 16000)
	p, err := newPiperTTS(piperCfg(model, 16000))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	rc, err := p.Speak(ctx, "hello")
	if err != nil {
		t.Fatalf("speak: %v", err)
	}
	// Wait for the fake to report itself instead of a fixed sleep, so a
	// slow machine cannot race the check (CI flake class).
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, serr := os.Stat(pidPath); serr == nil {
			break
		}
		if time.Now().After(deadline) {
			_, serr := os.Stat(pidPath + ".started")
			t.Fatalf("fake piper never recorded its pid (started=%v)", serr == nil)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if _, rerr := io.ReadAll(rc); !errors.Is(rerr, ErrPiperExit) {
		t.Fatalf("want ErrPiperExit after cancel, got %v", rerr)
	}
	data, rerr := os.ReadFile(pidPath)
	if rerr != nil {
		t.Fatalf("read fake pid: %v", rerr)
	}
	pid, rerr := strconv.Atoi(string(trimBytes(data)))
	if rerr != nil {
		t.Fatalf("parse pid %q: %v", data, rerr)
	}
	proc, ferr := os.FindProcess(pid)
	if ferr != nil {
		t.Fatalf("FindProcess(%d): %v", pid, ferr)
	}
	if serr := proc.Signal(syscall.Signal(0)); serr == nil {
		t.Fatalf("pid %d survived context cancel (orphan)", pid)
	}
}

// T-E: empty text returns an empty reader and starts no process.
func TestPiperEmptyTextNoProcess(t *testing.T) {
	writeFakeBin(t, "piper", "printf 'X'")
	model := writePiperModel(t, 16000)
	p, err := newPiperTTS(piperCfg(model, 16000))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	rc, err := p.Speak(context.Background(), "")
	if err != nil {
		t.Fatalf("speak empty: %v", err)
	}
	data, rerr := io.ReadAll(rc)
	if rerr != nil || len(data) != 0 {
		t.Fatalf("empty text: want empty stream, got %q err %v", data, rerr)
	}
}

// trimBytes strips trailing newline/whitespace from a byte slice.
func trimBytes(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == ' ' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// TestPiperModelRateSidecarNames pins the piper sidecar convention: the
// canonical path is the FULL model path + ".json" (voice.onnx ->
// voice.onnx.json, docs/workflows/tts.md:113/207). The legacy stripped
// ".onnx" name is accepted as a fallback for existing deployments.
// A wrong rate here plays local piper audio at the wrong speed/pitch.
func TestPiperModelRateSidecarNames(t *testing.T) {
	dir := t.TempDir()
	writeRate := func(name string, rate int) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(`{"audio":{"sample_rate":`+strconv.Itoa(rate)+`}}`), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	// Canonical: voice.onnx -> voice.onnx.json.
	writeRate("voice.onnx.json", 22050)
	if got := piperModelRate(filepath.Join(dir, "voice.onnx")); got != 22050 {
		t.Fatalf("canonical sidecar: got %d, want 22050", got)
	}
	// Legacy fallback: voice.json still works.
	writeRate("legacy.onnx", 16000)
	writeRate("legacy.json", 16000)
	if got := piperModelRate(filepath.Join(dir, "legacy.onnx")); got != 16000 {
		t.Fatalf("legacy sidecar: got %d, want 16000", got)
	}
	// Missing config -> 0 (passthrough).
	if got := piperModelRate(filepath.Join(dir, "missing.onnx")); got != 0 {
		t.Fatalf("missing sidecar: got %d, want 0", got)
	}
}

// TestPiperFinishPrefersLateFeedError (bughunt 2026-11-19 L18): finish()'s
// non-blocking werr drain BEFORE cmd.Wait can miss a feed error still in
// flight (a late EPIPE whose channel delivery races Wait). After Wait the
// feed result must be drained again and a non-nil feed error preferred
// over io.EOF — even when the child exits zero. Deterministic late-feed
// shape: the child stays ALIVE (sleep) so finish's pre-Wait drain sees an
// EMPTY channel (the miss); the feed error is buffered while cmd.Wait
// blocks, then the child is released to exit 0 — so only the post-Wait
// drain can observe the error. Without it, finish returns io.EOF.
func TestPiperFinishPrefersLateFeedError(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	script := "#!/bin/sh\nwhile [ ! -f \"" + release + "\" ]; do sleep 0.02; done\nexit 0\n"
	bin := filepath.Join(dir, "sleeper")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write sleeper: %v", err)
	}
	cmd := exec.Command(bin)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	s := &piperStream{cmd: cmd, werr: make(chan error, 1)}
	feedErr := fmt.Errorf("write: %w", syscall.EPIPE)
	go func() {
		// finish() runs its pre-Wait drain immediately (channel still
		// empty: the miss) and then blocks in cmd.Wait on the live
		// child. Buffer the late feed error, THEN release the child.
		time.Sleep(150 * time.Millisecond)
		s.werr <- feedErr
		if err := os.WriteFile(release, []byte("go"), 0o600); err != nil {
			t.Errorf("release sleeper: %v", err)
		}
	}()
	s.finish()
	if s.sticky == nil || errors.Is(s.sticky, io.EOF) {
		t.Fatalf("finish() returned %v, want the late feed error preferred over io.EOF (L18)", s.sticky)
	}
	if !errors.Is(s.sticky, ErrPiperExit) || !errors.Is(s.sticky, syscall.EPIPE) {
		t.Fatalf("sticky = %v, want ErrPiperExit wrapping the EPIPE feed error", s.sticky)
	}
	// Idempotence: a second finish must not re-Wait or change the sticky.
	s.finish()
	if !errors.Is(s.sticky, syscall.EPIPE) {
		t.Fatalf("second finish changed sticky: %v", s.sticky)
	}
}

// TestPiperLateEpipeSurfacesOnRealChild: an integration twin — a fake
// piper that exits WITHOUT reading stdin forces the text feed to end in
// EPIPE. Whatever the race outcome, a zero-exit child with a broken feed
// must never surface as a clean io.EOF: finish's post-Wait drain catches
// the feed error whenever it lands after the pre-Wait drain.
func TestPiperLateEpipeSurfacesOnRealChild(t *testing.T) {
	writeFakeBin(t, "piper", "exit 0\n") // never reads stdin
	model := writePiperModel(t, 16000)
	p, err := newPiperTTS(piperCfg(model, 16000))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	// A text far larger than the pipe buffer guarantees the feed write
	// is still in flight (or EPIPE'd) when the child exits.
	rc, err := p.Speak(context.Background(), strings.Repeat("x", 1<<20))
	if err != nil {
		t.Fatalf("speak: %v", err)
	}
	_, rerr := io.ReadAll(rc)
	cerr := rc.Close()
	// The child exited 0, so a failure can ONLY come from the feed lane.
	// If the feed error was observed at all, it must be named ErrPiperExit
	// (never a bare EOF swallow); a clean pass means the feed completed
	// before exit (no EPIPE happened), which is also legitimate.
	for _, e := range []error{rerr, cerr} {
		if e != nil && !errors.Is(e, ErrPiperExit) && !errors.Is(e, io.EOF) {
			t.Fatalf("unexpected error class: %v", e)
		}
	}
	if errors.Is(rerr, io.EOF) && errors.Is(cerr, ErrPiperExit) {
		t.Fatalf("Read reported clean EOF but Close found a feed error: finish must prefer the feed error on BOTH lanes (L18): read=%v close=%v", rerr, cerr)
	}
}
