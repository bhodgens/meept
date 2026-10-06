package llm

// Internal tests for the rotatingWriter / ProcessLogger / per-model fan-out.
// Lives in package llm (not llm_test) so it can reach unexported fields and
// the unexported RuntimeManager.logToEndpoint method.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	for i := range 11 {
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
	// After rotation the primary file holds at most one chunk (1MB) plus the
	// "out: " line prefix applied to the first byte of the post-rotation write.
	// Allow a small slack for prefix overhead.
	if fi.Size() >= 1024*1024+16 {
		t.Errorf("original file should be ~1MB after rotation, got %d bytes", fi.Size())
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
	for i := range 11 {
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
	for range writers {
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

// TestRotatingWriter_CloseConcurrentWithWriteAndTruncate pins the Close data
// race (mutexio finding, fixed in 47ed44f4 / nolint-reasoned in a933a3ba):
// Close nil-ed the shared *os.File pointer WITHOUT w.mu while the auto-restart
// path's Truncate read it under the lock — a genuine -race hit on the
// StopAll/auto-restart overlap. Close now holds w.mu across the close
// sequence, is idempotent (second Close sees *w.file == nil and no-ops), and
// Truncate after Close is a correct no-op. It also fails functionally if Close
// double-closes (os.File.Close on an already-closed file errors) or panics on
// a nil deref.
//
// NOTE: the concurrency loop below only trips -race probabilistically (it was
// observed firing ~2 runs in 8), so it is NOT the pin for the invariant.
// TestRotatingWriter_LockOrderIsDeterministic below pins it deterministically.
func TestRotatingWriter_CloseConcurrentWithWriteAndTruncate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	pl, err := OpenProcessLogger("127.0.0.1", "9127")
	if err != nil {
		t.Fatalf("OpenProcessLogger: %v", err)
	}

	const writers = 4
	const perWriter = 64 * 1024
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range writers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			chunk := bytes.Repeat([]byte("o"), perWriter)
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := pl.Stdout().Write(chunk); err != nil {
					t.Errorf("stdout write during close: %v", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			chunk := bytes.Repeat([]byte("e"), perWriter)
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := pl.Stderr().Write(chunk); err != nil {
					t.Errorf("stderr write during close: %v", err)
					return
				}
			}
		}()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			pl.Truncate()
		}
	}()
	go func() {
		defer wg.Done()
		if err := pl.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		close(stop)
	}()
	wg.Wait()

	// Close is idempotent: a second call must not double-close the file
	// (which would error) nor panic.
	if err := pl.Close(); err != nil {
		t.Errorf("second Close must be a no-op, got: %v", err)
	}
	// Truncate after Close is a best-effort no-op, never a panic.
	pl.Truncate()
}

// TestRotatingWriter_CloseNilReceiver_NoPanic pins bughunt wave L6: Close's
// guard read `if w == nil || w.file == nil { w.mu.Lock() ... }`, which
// dereferences the NIL receiver inside the branch body and panics instead of
// returning. Latent (every production constructor sets a non-nil out writer)
// but wrong, and the guard that reads as defensive is the panicking one.
//
// Close on a nil *rotatingWriter must be a silent no-op — the io.Closer-ish
// contract the ProcessLogger.Close nil-guard above relies on.
func TestRotatingWriter_CloseNilReceiver_NoPanic(t *testing.T) {
	var w *rotatingWriter
	if err := w.Close(); err != nil {
		t.Errorf("Close on a nil writer returned %v, want nil", err)
	}
	// Also via the exported wrapper's nil-guard path.
	var p *ProcessLogger
	if err := p.Close(); err != nil {
		t.Errorf("ProcessLogger.Close on nil returned %v, want nil", err)
	}
	// And a writer that exists but never got a file pointer (the
	// OpenProcessLogger MkdirAll-failure shape).
	var nilFile *os.File
	w2 := newSharedRotatingWriter(&nilFile, new(int64), new(bool), filepath.Join(t.TempDir(), "x.log"), "out: ", &sync.Mutex{})
	if err := w2.Close(); err != nil {
		t.Errorf("Close on a writer with a nil file pointer returned %v, want nil", err)
	}
	// Same for a writer whose *os.File pointer itself is nil.
	w3 := newSharedRotatingWriter(nil, new(int64), new(bool), filepath.Join(t.TempDir(), "x.log"), "out: ", &sync.Mutex{})
	if err := w3.Close(); err != nil {
		t.Errorf("Close on a writer with a nil **os.File returned %v, want nil", err)
	}
}

// TestRotatingWriter_CloseIsIdempotent pins the other half of bf6150c2's
// invariant: Close is idempotent. The SECOND Close must be a no-op that
// reports nil — double-closing an *os.File returns os.ErrClosed, and the
// ProcessLogger.Close path can be reached twice (StopAll plus the deferred
// cleanup in the spawn path).
func TestRotatingWriter_CloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	pl, err := OpenProcessLogger("127.0.0.1", "9191")
	if err != nil {
		t.Fatalf("OpenProcessLogger: %v", err)
	}
	if _, err := pl.Stdout().Write([]byte("idempotence probe\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// First Close closes the shared file and nils the shared pointer.
	if err := pl.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if pl.out.file == nil || *pl.out.file != nil {
		t.Fatal("Close did not nil the shared *os.File pointer")
	}
	// Second and third Close: no-ops, nil error, no panic.
	for i := range 2 {
		if err := pl.Close(); err != nil {
			t.Errorf("Close call %d after close returned %v, want nil (Close must be idempotent)", i+2, err)
		}
	}
	// The partner writer (stderr) shares the pointer and must observe the
	// close, and closing it too must also be a no-op.
	if err := pl.err.Close(); err != nil {
		t.Errorf("partner (stderr) Close after close returned %v, want nil", err)
	}
	// Truncate after Close stays a correct no-op.
	pl.Truncate()
}

// TestRotatingWriter_LockOrderIsDeterministic is the DETERMINISTIC pin for
// the invariant the concurrent test above only catches ~2 runs in 8.
//
// bf6150c2's contract is that every path touching the shared *os.File pointer
// takes w.mu BEFORE it reads or writes that pointer: no pre-lock fast path
// anywhere, which is what makes the Close-vs-Truncate race impossible rather
// than merely unlikely. The old concurrency test proves that only by winning a
// scheduling lottery.
//
// This test proves it by construction instead: hold w.mu from the test, fire
// each method on another goroutine, and assert it CANNOT complete while the
// lock is held. A method with an early return guarded by a pre-lock read of
// w.file (the regression shape) returns immediately and fails here on every
// run; a method that takes the lock first blocks, deterministically, forever
// until the test releases it.
//
// It also pins the nil-receiver asymmetry: a nil *rotatingWriter must return
// WITHOUT trying to lock (it has no mutex), which is the same property L6
// fixed — so the nil case is checked here too rather than only above.
func TestRotatingWriter_LockOrderIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	pl, err := OpenProcessLogger("127.0.0.1", "9192")
	if err != nil {
		t.Fatalf("OpenProcessLogger: %v", err)
	}
	defer pl.Close()

	for _, tc := range []struct {
		name string
		call func(w *rotatingWriter)
	}{
		{"Close", func(w *rotatingWriter) { _ = w.Close() }},
		{"Truncate", func(w *rotatingWriter) { w.Truncate() }},
		{"Write", func(w *rotatingWriter) { _, _ = w.Write([]byte("probe\n")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Hold the shared lock: the writers share one mutex, so this
			// blocks both the out and err paths.
			pl.out.mu.Lock()
			done := make(chan struct{})
			go func() {
				defer close(done)
				tc.call(pl.out)
			}()

			select {
			case <-done:
				pl.out.mu.Unlock()
				t.Fatalf("%s completed while w.mu was held: it has a pre-lock fast path, so its read of the shared *os.File is outside the lock (Close/Truncate race)", tc.name)
			case <-time.After(250 * time.Millisecond):
				// Correct: the method is blocked on the lock it must take
				// before touching the shared pointer.
			}
			pl.out.mu.Unlock()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("%s never completed after the lock was released (deadlock)", tc.name)
			}
		})
	}

	// A nil writer has no mutex to lock, so its Close must return without
	// one — this is the L6 fix, asserted under the same lock-order lens.
	var nilWriter *rotatingWriter
	nilDone := make(chan struct{})
	go func() {
		defer close(nilDone)
		_ = nilWriter.Close()
	}()
	select {
	case <-nilDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Close on a nil writer blocked: it tried to lock a mutex it does not have")
	}
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

// TestProcessLogger_LinePrefix verifies that the "out: "/"err: " line prefix
// is actually written to the file (the S3-Bug1 regression). Each newline-
// delimited line emitted via Stdout/Stderr must be prefixed.
func TestProcessLogger_LinePrefix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	pl, err := OpenProcessLogger("127.0.0.1", "9126")
	if err != nil {
		t.Fatalf("OpenProcessLogger: %v", err)
	}
	defer pl.Close()

	if _, err := pl.Stdout().Write([]byte("hello world\nsecond line\n")); err != nil {
		t.Fatalf("stdout write: %v", err)
	}
	if _, err := pl.Stderr().Write([]byte("warn msg\n")); err != nil {
		t.Fatalf("stderr write: %v", err)
	}

	path := filepath.Join(dir, ".meept", "logs", "runtimes", "127.0.0.1-9126.process.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	want := "out: hello world\nout: second line\nerr: warn msg\n"
	if string(data) != want {
		t.Errorf("unexpected log contents:\n got: %q\nwant: %q", string(data), want)
	}
}

// TestRuntimeManager_LegacyModelPath_InUseGate verifies that a provider
// configured with the legacy singular `model_path` (which ValidateAndNormalize
// mirrors under the synthetic "default" ModelPaths key) still passes the
// in-use gate when cfg.ModelKeys is populated from the provider's real models
// map. This is the S4-Bug1 regression: without ModelKeys, the gate would look
// for "<provider>/default" and never match the real "<provider>/<model-id>"
// references in the in-use set, causing the spawn to be skipped silently.
//
// We verify the gate decision directly via endpointHasInUseLocked (via the
// public Status WouldStart field) rather than driving a full StartAll, to
// avoid depending on a real HTTP health endpoint.
func TestRuntimeManager_LegacyModelPath_InUseGate(t *testing.T) {
	mgr := NewRuntimeManager(slog.Default())
	pidFile := filepath.Join(t.TempDir(), "legacy.pid")
	cfg := &RuntimeConfig{
		Type:            RuntimeLlamaCpp,
		ModelPath:       tempModelFileInternal(t),
		ModelPaths:      map[string]string{"default": tempModelFileInternal(t)},
		ModelKeys:       []string{"lfm-code"}, // real model id from provider's models map
		EndpointKey:     "llama-cpp:127.0.0.1:8770",
		PIDFile:         pidFile,
		AutoStart:       true,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "10"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  1 * time.Second,
		HealthTimeout:   2 * time.Second,
		HealthThreshold: 1,
	}
	if err := mgr.RegisterConfig("local", cfg, "http://127.0.0.1:8770"); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}

	// In-use set references the REAL model id, not "default".
	mgr.SetModelsInUse(map[string]struct{}{"local/lfm-code": {}})

	// The gate must pass: WouldStart=true and InUseModels includes "lfm-code".
	status, ok := mgr.StatusForProvider("local")
	if !ok {
		t.Fatal("provider not found")
	}
	if !status.WouldStart {
		t.Error("expected WouldStart=true for legacy config with ModelKeys populated; the gate did not match the real model id")
	}
	found := false
	for _, m := range status.InUseModels {
		if m == "lfm-code" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected InUseModels to contain 'lfm-code'; got %v", status.InUseModels)
	}

	// Negative control: with the in-use set pointing at a different model,
	// the gate must fail.
	mgr.SetModelsInUse(map[string]struct{}{"local/other": {}})
	status2, _ := mgr.StatusForProvider("local")
	if status2.WouldStart {
		t.Error("expected WouldStart=false when in-use set does not include the provider's model")
	}
}

// tempModelFileInternal is the in-package variant of createTempModelFile
// (which lives in package llm_test and is therefore not visible here).
func tempModelFileInternal(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(path, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("create temp model file: %v", err)
	}
	return path
}

// bytesBuffer is a small io.Writer adapter for slog that writes to a bytes.Buffer.
type bytesBuffer struct {
	b *bytes.Buffer
}

func (w *bytesBuffer) Write(p []byte) (int, error) { return w.b.Write(p) }

// TestRuntimeManager_SharedSpawn_PerModelFanOut verifies spec §3 + §4: when
// two providers share an endpoint and StartAll spawns the shared subprocess,
// BOTH per-model log files must contain the spawn_success event (fan-out).
// This is the integration counterpart to TestPerModelFanOut (which only
// exercises logToEndpoint directly).
func TestRuntimeManager_SharedSpawn_PerModelFanOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	server := newHealthTestServer(t)
	mgr := NewRuntimeManager(slog.New(slog.NewTextHandler(&bytesBuffer{b: &bytes.Buffer{}}, nil)))

	pidFile := filepath.Join(home, "shared-fanout.pid")
	cfg1 := &RuntimeConfig{
		Type:        RuntimeLlamaCpp,
		ModelPath:   tempModelFileInternal(t),
		ModelPaths:  map[string]string{"alpha": tempModelFileInternal(t)},
		ModelKeys:   []string{"alpha"},
		EndpointKey: "llama-cpp:127.0.0.1:7790",
		PIDFile:     pidFile,
		AutoStart:   true,
		AutoStop:    true,
		// Must outlive the test: the health check now requires the spawned
		// process to be alive, so a "sleep 0.1" fake reads unhealthy and
		// StartAll fails before the log fan-out under test can happen.
		SpawnCommand:    []string{"sleep", "30"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}
	cfg2 := &RuntimeConfig{
		Type:            RuntimeLlamaCpp,
		ModelPath:       tempModelFileInternal(t),
		ModelPaths:      map[string]string{"beta": tempModelFileInternal(t)},
		ModelKeys:       []string{"beta"},
		EndpointKey:     "llama-cpp:127.0.0.1:7790", // Same endpoint.
		PIDFile:         pidFile,
		AutoStart:       true,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "30"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}
	if err := mgr.RegisterConfig("provider-a", cfg1, server.URL); err != nil {
		t.Fatalf("RegisterConfig a: %v", err)
	}
	if err := mgr.RegisterConfig("provider-b", cfg2, server.URL); err != nil {
		t.Fatalf("RegisterConfig b: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = mgr.StopAll(stopCtx)

	// Both per-model log files must exist and contain a spawn_success event.
	logsDir := filepath.Join(home, ".meept", "logs", "runtimes")
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
			t.Errorf("read per-model log %s: %v", path, err)
			continue
		}
		if !strings.Contains(string(data), `"event":"spawn_success"`) &&
			!strings.Contains(string(data), `"event":"spawn_success`) {
			t.Errorf("expected spawn_success event in %s; contents:\n%s", path, string(data))
		}
	}
}

// newHealthTestServer returns an httptest.Server that responds 200 OK to any
// request (sufficient for the health checker to mark the endpoint healthy).
func newHealthTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}
