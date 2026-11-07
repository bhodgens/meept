package providers

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	if err := os.WriteFile(filepath.Join(dir, "model.json"), []byte(conf), 0o600); err != nil {
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
