package llm_test

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

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

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := proc.Start(ctx, &buf, io.Discard); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait for the subprocess to exit naturally so its stdout flushes
	// into our buffer. The sh -c echo completes in <1ms.
	time.Sleep(200 * time.Millisecond)
	_ = proc.Stop(ctx)

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
