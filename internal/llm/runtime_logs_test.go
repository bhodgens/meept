package llm_test

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// syncedBuffer is a bytes.Buffer safe for concurrent Write+String, needed
// because RuntimeProcess.Start spawns io.Copy goroutines into the writer
// while the test polls it.
type syncedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestOpenModelLogger_CreatesFile(t *testing.T) {
	// We can't redirect HOME easily without env manipulation; rely on
	// OpenModelLogger's best-effort behavior. A failure to open falls back to
	// a stderr-backed logger (file is nil). Either way, the returned logger
	// must be non-nil and must accept Log calls without panicking.
	ml, err := llm.OpenModelLogger("testprov", "testmodel")
	if err != nil {
		t.Logf("OpenModelLogger returned error (acceptable in sandboxed env): %v", err)
	}
	if ml == nil {
		t.Fatal("expected non-nil ModelLogger")
	}
	ml.Log("register")
	_ = ml.Close()
}

func TestRuntimeProcess_Start_HonorsWriters(t *testing.T) {
	// Spawn a process that writes a marker to stdout and verify the passed
	// io.Writer receives the output (not os.Stdout).
	cfg := &llm.RuntimeConfig{
		SpawnCommand: []string{"sh", "-c", "echo marker-output"},
		PIDFile:      filepath.Join(t.TempDir(), "p.pid"),
	}
	proc := llm.NewRuntimeProcess(cfg)

	var buf syncedBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := proc.Start(ctx, &buf, io.Discard); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait for the child's stdout to land in buf — the assertion condition
	// itself, made eventual. (`sh -c echo` exits in <1ms; the previous
	// 200ms blind sleep both raced the write on slow CI and wasted 200ms
	// on fast machines. Do NOT Stop before the marker appears: Stop can
	// SIGTERM the child before it writes.)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := buf.String()
		if strings.Contains(got, "marker-output") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected stdout writer to capture 'marker-output' within 2s, got %q", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Hygiene only: the child has already exited, so this just reaps and
	// removes the PID file.
	if err := proc.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "marker-output") {
		t.Errorf("expected stdout writer to capture 'marker-output', got %q", got)
	}
}

func TestOpenProcessLogger_BestEffort(t *testing.T) {
	// Even with a nil/broken underlying file, the returned ProcessLogger
	// should return non-nil writers that accept writes without panicking.
	pl, err := llm.OpenProcessLogger("127.0.0.1", "9999")
	if err != nil {
		t.Logf("OpenProcessLogger error (acceptable): %v", err)
	}
	if pl == nil {
		t.Fatal("expected non-nil ProcessLogger")
	}
	n, err := pl.Stdout().Write([]byte("test\n"))
	if err != nil {
		t.Errorf("stdout write error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes written, got %d", n)
	}
	_ = pl.Close()
}
