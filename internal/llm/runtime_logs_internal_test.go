package llm

// Internal tests for the rotatingWriter / ProcessLogger / per-model fan-out.
// Lives in package llm (not llm_test) so it can reach unexported fields and
// the unexported RuntimeManager.logToEndpoint method.

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestProcessLogger_Rotation verifies that ProcessLogger rotates its shared
// file at the 10MB cap and keeps exactly one .1 backup. This satisfies spec
// section 3 "rotatingWriter rotates at 10MB and keeps exactly one .1 backup".
func TestProcessLogger_Rotation(t *testing.T) {
	// Redirect HOME so the log dir lands inside a temp directory. The
	// pathutil.ExpandPath implementation calls os.UserHomeDir which honors
	// $HOME on Unix.
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	pl, err := OpenProcessLogger("127.0.0.1", "9123")
	if err != nil {
		t.Fatalf("OpenProcessLogger: %v", err)
	}
	defer pl.Close()

	chunk := bytes.Repeat([]byte("a"), 1024*1024) // 1MB
	// Write 11MB total so rotation triggers at least once (cap is 10MB).
	for i := 0; i < 11; i++ {
		if _, err := pl.Stdout().Write(chunk); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	dir2 := filepath.Join(dir, ".meept", "logs", "runtimes")
	origPath := filepath.Join(dir2, "127.0.0.1-9123.process.log")
	backupPath := origPath + ".1"

	fi, err := os.Stat(origPath)
	if err != nil {
		t.Fatalf("stat orig: %v", err)
	}
	if fi.Size() >= 1024*1024 {
		t.Errorf("original file should be < 1MB after rotation, got %d bytes", fi.Size())
	}
	bfi, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("expected backup file at %s, got error: %v", backupPath, err)
	}
	if bfi.Size() < 1024*1024 {
		t.Errorf("expected backup to contain at least 1MB of data, got %d bytes", bfi.Size())
	}
}

// TestProcessLogger_StdoutStderrShareFile_AfterRotation is the regression test
// for the rotation bug: after rotation on the stdout writer, the stderr writer
// must still be usable because both writers share the same **os.File. Before
// the fix, the stderr writer held a stale *os.File pointing at a closed fd,
// causing silent EBADF write failures.
func TestProcessLogger_StdoutStderrShareFile_AfterRotation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	pl, err := OpenProcessLogger("127.0.0.1", "9124")
	if err != nil {
		t.Fatalf("OpenProcessLogger: %v", err)
	}
	defer pl.Close()

	// Sanity: both writers share the same **os.File and the same *int64.
	if pl.out.file != pl.err.file {
		t.Fatalf("stdout and stderr writers should share the same **os.File; got %p vs %p", pl.out.file, pl.err.file)
	}
	if pl.out.written != pl.err.written {
		t.Fatalf("stdout and stderr writers should share the same *int64 counter; got %p vs %p", pl.out.written, pl.err.written)
	}
	// And the shared counter is non-nil with a non-nil underlying file.
	if pl.out.file == nil || *pl.out.file == nil {
		t.Fatalf("shared file pointer is nil before any write")
	}
	// Capture identity of the original *os.File object. We compare pointer
	// identity (not fd) because the OS may reuse freed fds across
	// close/open. The fix is about ensuring both writers see the NEW
	// *os.File pointer, not just a new fd.
	oldFile := *pl.out.file

	// Write enough through stdout alone to force rotation.
	chunk := bytes.Repeat([]byte("b"), 1024*1024)
	for i := 0; i < 11; i++ {
		if _, err := pl.Stdout().Write(chunk); err != nil {
			t.Fatalf("stdout write %d: %v", i, err)
		}
	}

	// After rotation: file pointer must be non-nil, must be a DIFFERENT
	// *os.File object (proving rotation reopened the file), and both
	// writers must see the same new pointer.
	if pl.out.file == nil || *pl.out.file == nil {
		t.Fatalf("shared file pointer is nil after rotation")
	}
	if *pl.out.file == oldFile {
		t.Errorf("expected a new *os.File object after rotation; got the same pointer")
	}
	newFile := *pl.out.file
	if pl.err.file == nil || *pl.err.file == nil {
		t.Fatalf("stderr writer's file pointer is nil after stdout-triggered rotation")
	}
	if *pl.err.file != newFile {
		t.Errorf("stderr writer should share the same new *os.File as stdout after rotation")
	}

	// Now write through stderr: this would have failed pre-fix with EBADF
	// because the err writer held a stale pointer to the closed fd.
	marker := []byte("post-rotation-stderr-marker\n")
	if n, err := pl.Stderr().Write(marker); err != nil {
		t.Fatalf("stderr write after rotation failed (this is the Gap 1 bug): %v", err)
	} else if n != len(marker) {
		t.Errorf("short stderr write: got %d want %d", n, len(marker))
	}

	// And confirm a follow-up stdout write still works.
	if _, err := pl.Stdout().Write([]byte("post-rotation-stdout-marker\n")); err != nil {
		t.Errorf("stdout write after rotation failed: %v", err)
	}
}

// TestProcessLogger_StdoutStderrShareFile_Concurrent exercises concurrent
// stdout/stderr writes under race to ensure the shared mutex and shared
// pointer don't deadlock or trigger -race failures.
func TestProcessLogger_StdoutStderrShareFile_Concurrent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	pl, err := OpenProcessLogger("127.0.0.1", "9125")
	if err != nil {
		t.Fatalf("OpenProcessLogger: %v", err)
	}
	defer pl.Close()

	const writers = 4
	const perWriter = 256 * 1024 // 256KB each; total 1MB across both streams x writers
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			chunk := bytes.Repeat([]byte("o"), perWriter)
			if _, err := pl.Stdout().Write(chunk); err != nil {
				t.Errorf("stdout write: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			chunk := bytes.Repeat([]byte("e"), perWriter)
			if _, err := pl.Stderr().Write(chunk); err != nil {
				t.Errorf("stderr write: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestPerModelFanOut verifies that logToEndpoint fans an event out to every
// per-model logger registered against an endpoint. This satisfies spec section
// 3 "Per-model event fan-out: simulated health transition on a shared process
// writes to both per-model logs."
func TestPerModelFanOut(t *testing.T) {
	// Redirect HOME so OpenModelLogger creates files inside a temp directory.
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	const endpointKey = "llama-cpp:127.0.0.1:7777"
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(&bytesBuffer{b: &bytes.Buffer{}}, nil)))

	// Register two providers on the same endpoint with distinct model keys.
	// We bypass RegisterConfig and populate the manager directly to avoid
	// subprocess / health-check side effects; we only need the modelLoggers
	// map wired up.
	mlAlpha, errA := OpenModelLogger("provider-a", "alpha")
	mlBeta, errB := OpenModelLogger("provider-b", "beta")
	if errA != nil || errB != nil {
		// If file creation fails (e.g. HOME-override quirks on non-Unix),
		// the test falls back to verifying the in-memory map plumbing.
		t.Logf("OpenModelLogger errors (acceptable fallback path): alpha=%v beta=%v", errA, errB)
	}
	if mlAlpha == nil || mlBeta == nil {
		t.Fatalf("OpenModelLogger returned nil loggers (alpha=%v beta=%v)", mlAlpha == nil, mlBeta == nil)
	}
	defer mlAlpha.Close()
	defer mlBeta.Close()

	loggerMap := map[string]*ModelLogger{
		"alpha": mlAlpha,
		"beta":  mlBeta,
	}
	mgr.mu.Lock()
	mgr.modelLoggers[endpointKey] = loggerMap
	mgr.mu.Unlock()

	// Emit an event that should land in BOTH per-model log files.
	mgr.logToEndpoint(endpointKey, "spawn_success", slog.Int("pid", 4242))

	// Verify both per-model log files exist and contain the event.
	logsDir := filepath.Join(dir, ".meept", "logs", "runtimes")
	for _, tc := range []struct {
		provider string
		model    string
	}{
		{"provider-a", "alpha"},
		{"provider-b", "beta"},
	} {
		path := filepath.Join(logsDir, tc.provider+"-"+tc.model+".log")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		// Each line is a JSON object; verify event + provider + model tags.
		var got struct {
			Msg      string `json:"msg"`
			Provider string `json:"provider"`
			Model    string `json:"model"`
			Event    string `json:"event"`
		}
		// The structured logger emits exactly one record per Log call; the
		// "register" event from OpenModelLogger is followed by our
		// "spawn_success" event. Find the spawn_success line.
		lines := strings.Split(string(data), "\n")
		found := false
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if err := json.Unmarshal([]byte(line), &got); err != nil {
				continue
			}
			if got.Event == "spawn_success" {
				found = true
				if got.Provider != tc.provider {
					t.Errorf("%s: expected provider=%s, got %s", path, tc.provider, got.Provider)
				}
				if got.Model != tc.model {
					t.Errorf("%s: expected model=%s, got %s", path, tc.model, got.Model)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected spawn_success event in %s; file contents:\n%s", path, string(data))
		}
	}
}

// bytesBuffer is a small io.Writer adapter for slog that writes to a bytes.Buffer.
type bytesBuffer struct {
	b *bytes.Buffer
}

func (w *bytesBuffer) Write(p []byte) (int, error) { return w.b.Write(p) }
