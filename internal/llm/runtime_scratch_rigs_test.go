package llm

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// writeTestPIDFile writes a pid file in the daemon's on-disk format.
func writeTestPIDFile(t *testing.T, path string, pid int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"pid":%d,"token":"tok"}`, pid)), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
}

// TestScratchRigDirs_MapsRigLayout verifies the rig discovery maps temp-dir
// rig layouts to their run dirs (home/.meept/run), not the rig root.
func TestScratchRigDirs_MapsRigLayout(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	// Reset the cached roots: tempDirs reads TMPDIR on every call, so no
	// reset is needed, but the glob roots list must pick tmp up via
	// os.TempDir.
	rig := filepath.Join(tmp, "meept-e2e.testrig")
	runDir := filepath.Join(rig, "home", ".meept", "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bench := filepath.Join(tmp, "meept-bench-async123")
	benchRun := filepath.Join(bench, "home", ".meept", "run")
	if err := os.MkdirAll(benchRun, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := scratchRigDirs()
	got := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		got[d] = true
	}
	if !got[runDir] {
		t.Errorf("expected e2e rig run dir %s in %v", runDir, dirs)
	}
	if !got[benchRun] {
		t.Errorf("expected bench rig run dir %s in %v", benchRun, dirs)
	}
}

// TestCollectScratchRigSpawnRecords_MissingDirSkipped verifies a missing run
// dir is skipped, not an error — rigs that cleaned up properly have no dir.
func TestCollectScratchRigSpawnRecords_MissingDirSkipped(t *testing.T) {
	records, err := CollectScratchRigSpawnRecords([]string{filepath.Join(t.TempDir(), "does-not-exist")})
	if err != nil {
		t.Fatalf("missing dir must not error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected no records, got %d", len(records))
	}
}

// TestCollectScratchRigSpawnRecords_ReadsRecords verifies records are read
// from present dirs.
func TestCollectScratchRigSpawnRecords_ReadsRecords(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "mlx.pid")
	writeTestPIDFile(t, pidFile, 4242)
	WriteSpawnRecord(SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:58081",
		PIDFile:     pidFile,
		Argv:        []string{"mlx_lm", "server", "--port", "58081"},
		AutoStop:    true,
		PID:         4242,
	})

	records, err := CollectScratchRigSpawnRecords([]string{dir})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d: %+v", len(records), records)
	}
	if records[0].PID != 4242 || records[0].EndpointKey != "mlx:127.0.0.1:58081" {
		t.Errorf("unexpected record %+v", records[0])
	}
}

// TestSweepStaleSpawnRecords_ScratchAgeBound verifies the sweep reaps a
// scratch record at the scratch bound. The record's PID file ModTime is the
// age proxy, so an old file with a live, init-reparented, argv-identical
// process is reaped.
func TestSweepStaleSpawnRecords_ScratchAgeBound(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "mlx.pid")
	// PID 300 = the lister's live, init-reparented, argv-matching process.
	writeTestPIDFile(t, pidFile, 300)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(pidFile, old, old); err != nil {
		t.Fatal(err)
	}
	WriteSpawnRecord(SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:58081",
		PIDFile:     pidFile,
		Argv:        []string{"mlx_lm", "server", "--port", "58081"},
		AutoStop:    true,
		PID:         300,
	})

	table := &sweepTable{procs_: []RuntimeProcInfo{
		{PID: 300, PPID: 1, Command: "/usr/bin/python3 /opt/homebrew/bin/mlx_lm server --port 58081"},
	}}
	lister := func() ([]RuntimeProcInfo, error) {
		return table.procs(), nil
	}
	signalled := false
	signal := func(pid int, sig syscall.Signal) error {
		signalled = true
		table.drop(pid)
		return nil
	}
	confirmed := sweepStaleSpawnRecords(
		[]SpawnRecord{{
			EndpointKey: "mlx:127.0.0.1:58081",
			PIDFile:     pidFile,
			Argv:        []string{"mlx_lm", "server", "--port", "58081"},
			AutoStop:    true,
			PID:         300,
		}},
		ScratchRecordStaleAfter,
		time.Now,
		time.Millisecond,
		lister,
		signal,
		slog.Default(),
	)
	if !signalled {
		t.Error("expected the stale scratch runtime to be signalled")
	}
	if len(confirmed) != 1 || confirmed[0] != 300 {
		t.Errorf("expected pid 300 reaped at scratch bound, got %v", confirmed)
	}
}

// sweepTable is a mutable process table for sweep tests: the signal seam
// drops pids so waitForPIDGone can confirm the reap.
type sweepTable struct {
	mu     sync.Mutex
	procs_ []RuntimeProcInfo
}

func (t *sweepTable) procs() []RuntimeProcInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]RuntimeProcInfo(nil), t.procs_...)
}

func (t *sweepTable) drop(pid int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []RuntimeProcInfo
	for _, p := range t.procs_ {
		if p.PID != pid {
			out = append(out, p)
		}
	}
	t.procs_ = out
}
