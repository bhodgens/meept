package llm

// Pins for the age-keyed stale-spawn-record sweep (issue #54): two orphaned
// 64K-ctx llama-servers (ppid=1) survived --keep runs whose daemons were
// SIGKILLed, and no existing sweep could touch them because the ownership
// guard adopts a leftover as observed-not-owned. A SpawnRecord older than
// spawnRecordStaleAfter whose pid is STILL alive, re-parented to init, and
// whose command line matches the record's argv is exactly such a leftover, so
// the stale sweep reaps it. Every guard below exists so the sweep cannot kill
// a live runtime somebody still owns.
//
// No real processes are signalled: the sweep's list and signal seams are
// faked, and "alive" is satisfied with the test process's own pid (the fake
// signaler intercepts everything).

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sweepFixedNow is the frozen clock the pins run under. Arbitrary; only
// differences to the PID-file mtime matter.
var sweepFixedNow = time.Unix(1_800_000_000, 0)

type signalCall struct {
	pid int
	sig syscall.Signal
}

// staleSweepFixture wires a fake process table and a fake signaler into the
// sweep. The map is mutated by the signaler so a SIGTERMed pid verifiably
// disappears from the next scan, exactly as ReapRuntimeProcesses requires.
type staleSweepFixture struct {
	procs   map[int]RuntimeProcInfo
	signals []signalCall
}

func newStaleSweepFixture() *staleSweepFixture {
	return &staleSweepFixture{procs: make(map[int]RuntimeProcInfo)}
}

func (f *staleSweepFixture) lister() RuntimeProcLister {
	return func() ([]RuntimeProcInfo, error) {
		out := make([]RuntimeProcInfo, 0, len(f.procs))
		for _, p := range f.procs {
			out = append(out, p)
		}
		return out, nil
	}
}

func (f *staleSweepFixture) signaler() runtimeSignaler {
	return func(pid int, sig syscall.Signal) error {
		f.signals = append(f.signals, signalCall{pid: pid, sig: sig})
		if sig == syscall.SIGTERM {
			delete(f.procs, pid) // dies on TERM: no SIGKILL escalation
		}
		return nil
	}
}

func (f *staleSweepFixture) run(records []SpawnRecord, maxAge time.Duration, age time.Duration) []int {
	return sweepStaleSpawnRecords(records, maxAge, func() time.Time { return sweepFixedNow },
		time.Millisecond, f.lister(), f.signaler(), slog.Default())
}

// staleRecordFixture writes a PID file (backdated by age) and its durable
// spawn record into a temp dir.
func staleRecordFixture(t *testing.T, pid int, age time.Duration, argv []string) SpawnRecord {
	t.Helper()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "runtime.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"tok"}`, pid)), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	old := sweepFixedNow.Add(-age)
	if err := os.Chtimes(pidFile, old, old); err != nil {
		t.Fatalf("backdate pid file: %v", err)
	}
	rec := SpawnRecord{
		EndpointKey: "llama:127.0.0.1:8099",
		PIDFile:     pidFile,
		Argv:        argv,
		AutoStop:    true,
		PID:         pid,
	}
	if err := WriteSpawnRecord(rec); err != nil {
		t.Fatalf("write spawn record: %v", err)
	}
	return rec
}

func assertUntouched(t *testing.T, rec SpawnRecord) {
	t.Helper()
	if _, err := os.Stat(rec.PIDFile); err != nil {
		t.Errorf("pid file must be untouched, stat err = %v", err)
	}
	if _, err := ReadSpawnRecord(rec.PIDFile); err != nil {
		t.Errorf("spawn record must be untouched, read err = %v", err)
	}
}

// Pin 1: stale record + alive ppid=1 command-line-matching process → reaped
// (SIGTERM to the process group, no SIGKILL needed, both handles removed).
func TestSweepStaleSpawnRecords_ReapsStaleAliveOrphan(t *testing.T) {
	selfPID := os.Getpid()
	argv := []string{"llama-server", "--port", "8099", "-c", "65536"}
	rec := staleRecordFixture(t, selfPID, 7*time.Hour, argv)

	fx := newStaleSweepFixture()
	fx.procs[selfPID] = RuntimeProcInfo{PID: selfPID, PPID: 1, Command: strings.Join(argv, " ")}

	reaped := fx.run([]SpawnRecord{rec}, spawnRecordStaleAfter, 7*time.Hour)

	if len(reaped) != 1 || reaped[0] != selfPID {
		t.Fatalf("reaped = %v, want [%d]", reaped, selfPID)
	}
	if len(fx.signals) != 1 || fx.signals[0] != (signalCall{selfPID, syscall.SIGTERM}) {
		t.Fatalf("signals = %v, want exactly one SIGTERM to pid %d", fx.signals, selfPID)
	}
	if _, err := os.Stat(rec.PIDFile); !os.IsNotExist(err) {
		t.Errorf("pid file must be removed after the reap, stat err = %v", err)
	}
	if _, err := os.Stat(SpawnRecordPath(rec.PIDFile)); !os.IsNotExist(err) {
		t.Errorf("spawn record must be removed after the reap, stat err = %v", err)
	}
}

// Pin 2: fresh record (age < maxAge) → untouched, no signals.
func TestSweepStaleSpawnRecords_LeavesFreshRecord(t *testing.T) {
	selfPID := os.Getpid()
	argv := []string{"llama-server", "--port", "8099", "-c", "65536"}
	rec := staleRecordFixture(t, selfPID, 30*time.Minute, argv)

	fx := newStaleSweepFixture()
	fx.procs[selfPID] = RuntimeProcInfo{PID: selfPID, PPID: 1, Command: strings.Join(argv, " ")}

	reaped := fx.run([]SpawnRecord{rec}, spawnRecordStaleAfter, 30*time.Minute)

	if len(reaped) != 0 {
		t.Fatalf("reaped = %v, want none for a fresh record", reaped)
	}
	if len(fx.signals) != 0 {
		t.Fatalf("signals = %v, want none for a fresh record", fx.signals)
	}
	assertUntouched(t, rec)
}

// Pin 2b: age exactly == maxAge is NOT stale (age <= maxAge skips).
func TestSweepStaleSpawnRecords_BoundaryAgeNotStale(t *testing.T) {
	selfPID := os.Getpid()
	argv := []string{"llama-server", "--port", "8099"}
	rec := staleRecordFixture(t, selfPID, spawnRecordStaleAfter, argv)

	fx := newStaleSweepFixture()
	fx.procs[selfPID] = RuntimeProcInfo{PID: selfPID, PPID: 1, Command: strings.Join(argv, " ")}

	if reaped := fx.run([]SpawnRecord{rec}, spawnRecordStaleAfter, spawnRecordStaleAfter); len(reaped) != 0 {
		t.Fatalf("reaped = %v, want none at exactly maxAge", reaped)
	}
	assertUntouched(t, rec)
}

// Pin 3: stale record + dead pid → untouched here; the existing orphan sweep
// and record pruning own dead-pid cleanup, so the stale sweep must not
// double-process (a raced replacement spawn keeps its handles).
func TestSweepStaleSpawnRecords_SkipsDeadPID(t *testing.T) {
	deadPID := deadSpawnPID(t)
	argv := []string{"llama-server", "--port", "8099"}
	rec := staleRecordFixture(t, deadPID, 8*time.Hour, argv)

	fx := newStaleSweepFixture() // process table has no entry for deadPID

	if reaped := fx.run([]SpawnRecord{rec}, spawnRecordStaleAfter, 8*time.Hour); len(reaped) != 0 {
		t.Fatalf("reaped = %v, want none for a dead pid", reaped)
	}
	if len(fx.signals) != 0 {
		t.Fatalf("signals = %v, want none for a dead pid", fx.signals)
	}
	assertUntouched(t, rec)
}

// Pin 4: stale + alive but parent != 1 → untouched (a live meept daemon owns
// it; the live-owner veto posture).
func TestSweepStaleSpawnRecords_LeavesLiveParentOwner(t *testing.T) {
	selfPID := os.Getpid()
	argv := []string{"llama-server", "--port", "8099"}
	rec := staleRecordFixture(t, selfPID, 8*time.Hour, argv)

	fx := newStaleSweepFixture()
	fx.procs[selfPID] = RuntimeProcInfo{PID: selfPID, PPID: os.Getppid(), Command: strings.Join(argv, " ")}

	if reaped := fx.run([]SpawnRecord{rec}, spawnRecordStaleAfter, 8*time.Hour); len(reaped) != 0 {
		t.Fatalf("reaped = %v, want none while the parent is live", reaped)
	}
	if len(fx.signals) != 0 {
		t.Fatalf("signals = %v, want none while the parent is live", fx.signals)
	}
	assertUntouched(t, rec)
}

// Pin 5: command line mismatch → untouched, even at ppid=1 and stale.
func TestSweepStaleSpawnRecords_LeavesCommandLineMismatch(t *testing.T) {
	selfPID := os.Getpid()
	argv := []string{"llama-server", "--port", "8099", "-c", "65536"}
	rec := staleRecordFixture(t, selfPID, 8*time.Hour, argv)

	otherArgv := []string{"llama-server", "--port", "8100", "-c", "65536"}
	fx := newStaleSweepFixture()
	fx.procs[selfPID] = RuntimeProcInfo{PID: selfPID, PPID: 1, Command: strings.Join(otherArgv, " ")}

	if reaped := fx.run([]SpawnRecord{rec}, spawnRecordStaleAfter, 8*time.Hour); len(reaped) != 0 {
		t.Fatalf("reaped = %v, want none on command-line mismatch", reaped)
	}
	if len(fx.signals) != 0 {
		t.Fatalf("signals = %v, want none on command-line mismatch", fx.signals)
	}
	assertUntouched(t, rec)
}

// The exported wrapper must run the same sweep through the real ps/kill path
// without touching anything when every record is fresh — proved with a real
// (dead-pid) record so no process is ever signalled.
func TestSweepStaleSpawnRecords_ExportedWrapperSkipsFresh(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn true: %v", err)
	}
	pid := cmd.Process.Pid
	_, _ = cmd.Process.Wait()

	argv := []string{"llama-server", "--port", "8099"}
	rec := staleRecordFixture(t, pid, 30*time.Minute, argv)

	if reaped := SweepStaleSpawnRecords([]SpawnRecord{rec}, spawnRecordStaleAfter,
		func() time.Time { return sweepFixedNow }); len(reaped) != 0 {
		t.Fatalf("reaped = %v, want none for a fresh record", reaped)
	}
	assertUntouched(t, rec)
}
