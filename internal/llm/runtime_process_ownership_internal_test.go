package llm

// Internal tests for runtime adoption ownership (docs/bugs-and-gaps.md
// "Runtime adoption ownership race"). Lives in package llm (not llm_test)
// so it can read the unexported instanceToken / spawnedByUs fields and the
// unexported parsePIDFile helper.
//
// Semantics under test:
//   - a pidfile carrying THIS instance's token → adopt as OWNED (Stop ok)
//   - a pidfile with a foreign token, or a legacy tokenless pidfile →
//     adopt as OBSERVED, NOT OWNED (Stop must not kill the process)
//   - a dead-pid pidfile → stale file removed, fresh spawn owned

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// startTestSleep spawns a real sleep subprocess in its own process group
// (mirroring production Start's Setpgid) so that any test that lets
// production code signal its group cannot take down the test binary.
func startTestSleep(t *testing.T, secs string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", secs)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait() // reap so the pid is fully gone after the test
	})
	return cmd
}

// testPidAlive reports whether a pid is alive by signal-0.
func testPidAlive(pid int) bool {
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}

// writeTestPidFile writes a pidfileEntry as the current JSON format.
func writeTestPidFile(t *testing.T, path string, entry pidfileEntry) {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal pidfile entry: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
}

func TestParsePIDFile_Formats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		data      string
		wantPID   int
		wantToken string
		wantErr   bool
	}{
		{name: "json with token", data: `{"pid":1234,"token":"abc123"}`, wantPID: 1234, wantToken: "abc123"},
		{name: "json without token", data: `{"pid":1234}`, wantPID: 1234, wantToken: ""},
		{name: "legacy bare int", data: "1234", wantPID: 1234, wantToken: ""},
		{name: "legacy bare int with whitespace", data: "  1234\n", wantPID: 1234, wantToken: ""},
		{name: "negative pid", data: "-5", wantErr: true},
		{name: "json negative pid", data: `{"pid":-5}`, wantErr: true},
		{name: "garbage", data: "not-a-pid", wantErr: true},
		{name: "empty", data: "", wantErr: true},
		{name: "malformed json", data: `{"pid":`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, err := parsePIDFile([]byte(tc.data))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got entry %+v", entry)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if entry.PID != tc.wantPID || entry.Token != tc.wantToken {
				t.Errorf("got %+v, want pid=%d token=%q", entry, tc.wantPID, tc.wantToken)
			}
		})
	}
}

// TestRuntimeProcess_AdoptForeignToken_ObservedNotOwned is the production-bug
// regression: a second manager instance sharing the run dir finds a live
// pidfile written by ANOTHER instance (foreign token). Start must adopt it
// as observed-not-owned and Stop must NOT kill the process.
func TestRuntimeProcess_AdoptForeignToken_ObservedNotOwned(t *testing.T) {
	victim := startTestSleep(t, "300")

	pidFile := filepath.Join(t.TempDir(), "foreign.pid")
	writeTestPidFile(t, pidFile, pidfileEntry{PID: victim.Process.Pid, Token: "0000foreign-instance-token"})

	p := NewRuntimeProcess(&RuntimeConfig{
		SpawnCommand: []string{"sleep", "300"},
		PIDFile:      pidFile,
	})
	if p.instanceToken == "0000foreign-instance-token" {
		t.Fatal("instance tokens must be random; foreign token impossibly matched")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Start (adopt foreign): %v", err)
	}

	if p.spawnedByUs {
		t.Error("foreign-token adoption must be observed-not-owned, got spawnedByUs=true")
	}
	if p.PID() != victim.Process.Pid {
		t.Errorf("expected adopted pid %d, got %d", victim.Process.Pid, p.PID())
	}
	if !p.IsRunning() {
		t.Error("adopted process should report running")
	}

	// Stop must refuse with the ownership sentinel: nothing is stopped, and the
	// process plus the foreign pidfile survive. The sentinel replaced a silent
	// nil return — a nil return let every stop surface (RPC, GUI, CLI) report
	// "stopped" while the runtime kept the model and the endpoint port.
	if err := p.Stop(ctx); err != ErrRuntimeNotOwned {
		t.Fatalf("Stop on observed runtime = %v, want ErrRuntimeNotOwned", err)
	}
	if !testPidAlive(victim.Process.Pid) {
		t.Fatal("foreign-token Stop killed the observed process — ownership race is back")
	}
	// The foreign pidfile must be left untouched (no cleanup side effects
	// on a runtime this instance does not own).
	if _, err := os.Stat(pidFile); err != nil {
		t.Errorf("foreign pidfile should survive an observed-adoption Stop: %v", err)
	}
}

// TestRuntimeProcess_AdoptForeignToken_ManagerReusePath exercises the same
// foreign-adoption path via a SECOND instance sharing the pidfile — the
// exact shape of the production churn (test binary / second daemon
// StopAll'ing the production runtimes).
func TestRuntimeProcess_AdoptForeignToken_ManagerReusePath(t *testing.T) {
	// Instance A spawns and writes its token into the pidfile.
	pidFile := filepath.Join(t.TempDir(), "shared.pid")
	cfgA := &RuntimeConfig{SpawnCommand: []string{"sleep", "300"}, PIDFile: pidFile}
	pA := NewRuntimeProcess(cfgA)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pA.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("instance A start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = pA.Stop(stopCtx) // instance A owns it; cleans up if the test fails early
	})
	spawnedPid := pA.PID()
	if !testPidAlive(spawnedPid) {
		t.Fatal("instance A process not alive after spawn")
	}

	// Instance B (foreign token — every construction randomizes) starts,
	// adopts as observed, then stops. The process must survive B's Stop.
	pB := NewRuntimeProcess(&RuntimeConfig{SpawnCommand: []string{"sleep", "300"}, PIDFile: pidFile})
	if pB.instanceToken == pA.instanceToken {
		t.Fatal("two constructions must not share an instance token")
	}
	if err := pB.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("instance B start (adopt): %v", err)
	}
	if pB.spawnedByUs {
		t.Error("instance B must adopt instance A's runtime as observed-not-owned")
	}
	if err := pB.Stop(ctx); err != ErrRuntimeNotOwned {
		t.Fatalf("instance B stop = %v, want ErrRuntimeNotOwned (B must not kill A's runtime)", err)
	}
	if !testPidAlive(spawnedPid) {
		t.Fatal("instance B's Stop killed instance A's runtime — the exact production bug")
	}
	if !pA.IsRunning() {
		t.Error("instance A should still consider its runtime running after B's Stop")
	}

	// And A can still stop its own.
	if err := pA.Stop(ctx); err != nil {
		t.Fatalf("instance A stop: %v", err)
	}
	if testPidAlive(spawnedPid) {
		t.Error("instance A's Stop should have terminated its own runtime")
	}
}

// TestRuntimeProcess_AdoptOwnToken_SameBootRestartOwned: a fresh
// RuntimeProcess constructed with the SAME instance token (the same-boot
// re-Start path — e.g. the manager's cached proc or a same-boot health
// restart) adopts the live pidfile process as OWNED and Stop kills it.
func TestRuntimeProcess_AdoptOwnToken_SameBootRestartOwned(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "own.pid")
	cfg := &RuntimeConfig{SpawnCommand: []string{"sleep", "300"}, PIDFile: pidFile}
	pA := NewRuntimeProcess(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pA.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("initial spawn: %v", err)
	}
	pid := pA.PID()

	// Same boot, fresh struct, same token.
	pB := &RuntimeProcess{config: cfg, pidFile: pidFile, instanceToken: pA.instanceToken}
	if err := pB.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("same-token adopt start: %v", err)
	}
	if !pB.spawnedByUs {
		t.Error("own-token adoption must be OWNED (spawnedByUs=true)")
	}
	if pB.PID() != pid {
		t.Errorf("expected adopted pid %d, got %d", pid, pB.PID())
	}

	// Owned adopt → Stop kills.
	if err := pB.Stop(ctx); err != nil {
		t.Fatalf("owned stop: %v", err)
	}
	if testPidAlive(pid) {
		t.Error("own-token Stop should have terminated the owned runtime")
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("pidfile should be removed after owned stop: %v", err)
	}
}

// TestRuntimeProcess_Roundtrip_OwnWriteThenStartOwned: spawn → pidfile
// written with our token → Start again on the same struct → adopted OWNED,
// same PID, no duplicate spawn.
func TestRuntimeProcess_Roundtrip_OwnWriteThenStartOwned(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "roundtrip.pid")
	p := NewRuntimeProcess(&RuntimeConfig{SpawnCommand: []string{"sleep", "300"}, PIDFile: pidFile})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("first start: %v", err)
	}
	pid := p.PID()

	// Our own write must carry our token.
	entry, err := p.readPIDFile()
	if err != nil {
		t.Fatalf("read back pidfile: %v", err)
	}
	if entry.PID != pid || entry.Token != p.instanceToken {
		t.Errorf("pidfile = %+v, want pid=%d token=%q", entry, pid, p.instanceToken)
	}

	// Same-struct re-Start (the manager's cached-proc path) adopts owned.
	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("second start (adopt): %v", err)
	}
	if !p.spawnedByUs {
		t.Error("roundtrip adoption must be OWNED")
	}
	if p.PID() != pid {
		t.Errorf("re-Start must not spawn a duplicate: pid changed %d → %d", pid, p.PID())
	}
	if !testPidAlive(pid) {
		t.Error("process should still be alive after re-Start adoption")
	}

	if err := p.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if testPidAlive(pid) {
		t.Error("roundtrip Stop should have terminated the owned runtime")
	}
}

// TestRuntimeProcess_LegacyPidfile_ObservedNotOwned: a pre-tokens bare-int
// pidfile naming a live process adopts as observed-not-owned.
func TestRuntimeProcess_LegacyPidfile_ObservedNotOwned(t *testing.T) {
	victim := startTestSleep(t, "300")

	pidFile := filepath.Join(t.TempDir(), "legacy.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(victim.Process.Pid)), 0o600); err != nil {
		t.Fatalf("write legacy pidfile: %v", err)
	}

	p := NewRuntimeProcess(&RuntimeConfig{SpawnCommand: []string{"sleep", "300"}, PIDFile: pidFile})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Start (adopt legacy): %v", err)
	}
	if p.spawnedByUs {
		t.Error("legacy pidfile adoption must be observed-not-owned")
	}
	if p.PID() != victim.Process.Pid {
		t.Errorf("expected adopted pid %d, got %d", victim.Process.Pid, p.PID())
	}

	if err := p.Stop(ctx); err != ErrRuntimeNotOwned {
		t.Fatalf("Stop on legacy-adopted runtime = %v, want ErrRuntimeNotOwned", err)
	}
	if !testPidAlive(victim.Process.Pid) {
		t.Fatal("legacy-adoption Stop killed the observed process")
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Errorf("legacy pidfile should survive an observed-adoption Stop: %v", err)
	}
}

// TestRuntimeProcess_StalePidFile_RemovedAndFreshSpawnOwned: a pidfile
// naming a DEAD process is removed and Start spawns a fresh, owned process.
func TestRuntimeProcess_StalePidFile_RemovedAndFreshSpawnOwned(t *testing.T) {
	// A definitely-dead pid: spawn true (exits immediately) and reap it.
	dead := exec.Command("true")
	if err := dead.Start(); err != nil {
		t.Fatalf("spawn true: %v", err)
	}
	deadPid := dead.Process.Pid
	_, _ = dead.Process.Wait()
	if testPidAlive(deadPid) {
		t.Skip("dead pid still reported alive; environment reaped oddly")
	}

	pidFile := filepath.Join(t.TempDir(), "stale.pid")
	writeTestPidFile(t, pidFile, pidfileEntry{PID: deadPid, Token: "0000stale-boot-token"})

	p := NewRuntimeProcess(&RuntimeConfig{SpawnCommand: []string{"sleep", "300"}, PIDFile: pidFile})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("Start after stale pidfile: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = p.Stop(stopCtx)
	}()

	if !p.spawnedByUs {
		t.Error("fresh spawn after stale pidfile must be OWNED")
	}
	if !p.IsRunning() {
		t.Error("freshly spawned process should be running")
	}
	// The stale file was replaced by our own tokenized write.
	entry, err := p.readPIDFile()
	if err != nil {
		t.Fatalf("pidfile missing after fresh spawn: %v", err)
	}
	if entry.PID != p.PID() || entry.Token != p.instanceToken {
		t.Errorf("pidfile after fresh spawn = %+v, want pid=%d our token", entry, p.PID())
	}
}

// --- audit findings F12 / F16 / F56 pins ---

// TestRuntimeProcess_Stop_AutomaticRefusesForeignToken pins audit finding F12:
// the AUTOMATIC path (Stop/StopAll, health restart) must not signal a PID file
// another meept instance wrote. Simulated by a spawnedByUs process whose child
// is gone (cmd==nil) and whose PID file was rewritten with a FOREIGN token.
func TestRuntimeProcess_Stop_AutomaticRefusesForeignToken(t *testing.T) {
	victim := startTestSleep(t, "300")
	pidFile := filepath.Join(t.TempDir(), "foreign-token.pid")
	writeTestPidFile(t, pidFile, pidfileEntry{PID: victim.Process.Pid, Token: "0000another-instance"})

	p := &RuntimeProcess{
		config:        &RuntimeConfig{SpawnCommand: []string{"sleep", "300"}, PIDFile: pidFile},
		pidFile:       pidFile,
		instanceToken: "0000our-instance",
		spawnedByUs:   true, // we own the endpoint, but the file now names another instance's runtime
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Stop(ctx); err != ErrRuntimeNotOwned {
		t.Fatalf("automatic Stop on a foreign-token PID file = %v, want ErrRuntimeNotOwned", err)
	}
	if !testPidAlive(victim.Process.Pid) {
		t.Fatal("the foreign instance's runtime was signalled by an automatic Stop (F12)")
	}
}

// TestRuntimeProcess_Stop_AutomaticRefusesReusedPID pins the identity check on
// the automatic path (F12): our own token is in the PID file, but the pid now
// runs an unrelated command (ours died and the pid was reused). Stop must not
// signal it.
func TestRuntimeProcess_Stop_AutomaticRefusesReusedPID(t *testing.T) {
	victim := startTestSleep(t, "300") // runs "sleep 300", not the configured runtime
	pidFile := filepath.Join(t.TempDir(), "reused.pid")
	p := &RuntimeProcess{
		config:        &RuntimeConfig{SpawnCommand: []string{"mlx_lm", "server", "--port", "8081"}, PIDFile: pidFile},
		pidFile:       pidFile,
		instanceToken: "0000our-instance",
		spawnedByUs:   true,
	}
	writeTestPidFile(t, pidFile, pidfileEntry{PID: victim.Process.Pid, Token: p.instanceToken})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Stop(ctx); err == nil {
		t.Fatal("automatic Stop must refuse a pid that does not run this runtime (F12)")
	}
	if !testPidAlive(victim.Process.Pid) {
		t.Fatal("an unrelated reused pid was signalled by an automatic Stop (F12)")
	}
}

// TestRuntimeProcess_Stop_AutomaticAbsentPidClearsStaleFile pins the "not in the
// process table => nothing to signal, clear the stale file" rule (F12).
func TestRuntimeProcess_Stop_AutomaticAbsentPidClearsStaleFile(t *testing.T) {
	dead := exec.Command("true")
	if err := dead.Start(); err != nil {
		t.Fatalf("spawn true: %v", err)
	}
	deadPid := dead.Process.Pid
	_, _ = dead.Process.Wait()
	if testPidAlive(deadPid) {
		t.Skip("dead pid still reported alive; environment reaped oddly")
	}

	pidFile := filepath.Join(t.TempDir(), "stale.pid")
	p := &RuntimeProcess{
		config:        &RuntimeConfig{SpawnCommand: []string{"sleep", "300"}, PIDFile: pidFile},
		pidFile:       pidFile,
		instanceToken: "0000our-instance",
		spawnedByUs:   true,
	}
	writeTestPidFile(t, pidFile, pidfileEntry{PID: deadPid, Token: p.instanceToken})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop with an absent pid = %v, want nil", err)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("the stale pid file must be removed, stat err = %v", err)
	}
}

// TestRuntimeProcess_Stop_FailsClosedWithoutIdentity pins audit finding F16's
// fail-closed rule: a recovered pid that cannot be identified (no durable
// record, no configured spawn command) must not be signalled.
func TestRuntimeProcess_Stop_FailsClosedWithoutIdentity(t *testing.T) {
	victim := startTestSleep(t, "300")
	pidFile := filepath.Join(t.TempDir(), "noid.pid")
	writeTestPidFile(t, pidFile, pidfileEntry{PID: victim.Process.Pid, Token: "0000our-instance"})

	p := &RuntimeProcess{
		config:        &RuntimeConfig{PIDFile: pidFile}, // no SpawnCommand, no record
		pidFile:       pidFile,
		instanceToken: "0000our-instance",
		spawnedByUs:   true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Stop(ctx); err == nil {
		t.Fatal("Stop must fail closed when identity cannot be established (F16)")
	}
	if !testPidAlive(victim.Process.Pid) {
		t.Fatal("a pid with no verifiable identity was signalled (F16)")
	}
}

// --- audit findings F79 (scan failure) / F78 (exit-path liveness) pins ---

// TestRuntimeProcess_Stop_ScanFailureFailsClosed pins audit finding F79: a FAILED
// process-table scan is an UNVERIFIABLE verdict that keeps the handles, never an
// "absent" verdict. Classifying it as absent returned success from Stop and
// deleted the PID file AND the durable record while the runtime was still live
// and serving, so every later stop surface read no file and reported "not
// running" for a runtime that still held the endpoint port.
func TestRuntimeProcess_Stop_ScanFailureFailsClosed(t *testing.T) {
	victim := startTestSleep(t, "300")
	pidFile := filepath.Join(t.TempDir(), "scan-failed.pid")
	writeTestPidFile(t, pidFile, pidfileEntry{PID: victim.Process.Pid, Token: "0000our-instance"})
	if err := WriteSpawnRecord(SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        []string{"sleep", "300"},
		AutoStop:    true,
		PID:         victim.Process.Pid,
	}); err != nil {
		t.Fatalf("write spawn record: %v", err)
	}

	scanErr := errors.New(`ps scan failed: exec: "ps": executable file not found in $PATH`)
	p := &RuntimeProcess{
		config:        &RuntimeConfig{SpawnCommand: []string{"sleep", "300"}, PIDFile: pidFile},
		pidFile:       pidFile,
		instanceToken: "0000our-instance",
		spawnedByUs:   true,
		listProcesses: func() ([]RuntimeProcInfo, error) { return nil, scanErr },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.StopAsOperator(ctx); err == nil {
		t.Fatal("a failed process scan must not report success (F79)")
	}
	// The AUTOMATIC path must fail closed too: it is the shutdown path, where a
	// false success is exactly what loses the runtime.
	if err := p.Stop(ctx); err == nil {
		t.Fatal("the automatic stop path must not report success on a failed scan (F79)")
	}
	if !testPidAlive(victim.Process.Pid) {
		t.Fatal("a failed scan must never signal the pid (F79)")
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Errorf("the PID file must survive a failed scan, stat err = %v (F79)", err)
	}
	if _, err := ReadSpawnRecord(pidFile); err != nil {
		t.Errorf("the durable record must survive a failed scan: %v (F79)", err)
	}

	// A scan that SUCCEEDED and does not list the pid is still the absent
	// verdict: the stale handles may go (the pre-existing F12 rule).
	p.listProcesses = func() ([]RuntimeProcInfo, error) { return nil, nil }
	if err := p.StopAsOperator(ctx); err != nil {
		t.Fatalf("a successful scan that omits the pid must stay a no-op success, got %v", err)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("a successful absent verdict must clear the stale PID file, stat err = %v", err)
	}
}

// writeExitPathHandle writes the PID file + durable record pair the exit path
// operates on, naming pid.
func writeExitPathHandle(t *testing.T, pidFile string, pid int) {
	t.Helper()
	writeTestPidFile(t, pidFile, pidfileEntry{PID: pid, Token: "0000our-instance"})
	if err := WriteSpawnRecord(SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        []string{"sleep", "300"},
		AutoStop:    true,
		PID:         pid,
	}); err != nil {
		t.Fatalf("write spawn record: %v", err)
	}
}

// assertExitPathHandles checks whether the pid file + record pair is present.
func assertExitPathHandles(t *testing.T, pidFile string, wantPresent bool, context string) {
	t.Helper()
	_, statErr := os.Stat(pidFile)
	if wantPresent && statErr != nil {
		t.Errorf("%s: the PID file must survive, stat err = %v", context, statErr)
	}
	if !wantPresent && !os.IsNotExist(statErr) {
		t.Errorf("%s: the PID file must be gone, stat err = %v", context, statErr)
	}
	_, recErr := ReadSpawnRecord(pidFile)
	if wantPresent && recErr != nil {
		t.Errorf("%s: the durable record must survive: %v", context, recErr)
	}
	if !wantPresent && recErr == nil {
		t.Errorf("%s: the durable record must be gone", context)
	}
}

// TestClearStalePIDFilesForPid_KeepsHandlesOfLivePID pins audit finding F78: the
// wait goroutine's exit path must not destroy the handles of a runtime that is
// still alive. Under the supervisor the process that goroutine waited on is the
// WRAPPER while the recorded pid is the runtime's, so a wrapper killed hard
// (SIGKILL) — or a runtime outliving a grace window — used to delete the only
// handle a later stop has: `meept runtime stop` then reported "not running" for
// a live runtime still holding the endpoint port.
func TestClearStalePIDFilesForPid_KeepsHandlesOfLivePID(t *testing.T) {
	victim := startTestSleep(t, "300")
	pidFile := filepath.Join(t.TempDir(), "exit-live.pid")
	writeExitPathHandle(t, pidFile, victim.Process.Pid)

	p := &RuntimeProcess{config: &RuntimeConfig{PIDFile: pidFile}, pidFile: pidFile, instanceToken: "0000our-instance"}
	p.clearStalePIDFilesForPid(victim.Process.Pid)
	assertExitPathHandles(t, pidFile, true, "a live pid must keep its handles (F78)")
}

// TestClearStalePIDFilesForPid_RemovesHandlesOfDeadPID is the other direction:
// the F97 guarantee still holds — a runtime that really exited leaves no stale
// handle behind.
func TestClearStalePIDFilesForPid_RemovesHandlesOfDeadPID(t *testing.T) {
	dead := exec.Command("true")
	if err := dead.Start(); err != nil {
		t.Fatalf("spawn true: %v", err)
	}
	deadPid := dead.Process.Pid
	_, _ = dead.Process.Wait()
	if processAlive(deadPid) {
		t.Skip("dead pid still reported alive; environment reaped oddly")
	}

	pidFile := filepath.Join(t.TempDir(), "exit-dead.pid")
	writeExitPathHandle(t, pidFile, deadPid)

	p := &RuntimeProcess{config: &RuntimeConfig{PIDFile: pidFile}, pidFile: pidFile, instanceToken: "0000our-instance"}
	p.clearStalePIDFilesForPid(deadPid)
	assertExitPathHandles(t, pidFile, false, "a dead pid must leave no stale handle (F97)")
}

// TestClearStalePIDFilesForPid_SkipsWhileTheStartLockIsHeld pins the second half
// of finding F78: compare + remove runs under the endpoint start lock, so a
// concurrent Start that just rewrote both files for a FRESH runtime cannot have
// them deleted by the exiting generation. With the lock held, the cleanup must
// leave the files alone; once released, it proceeds.
func TestClearStalePIDFilesForPid_SkipsWhileTheStartLockIsHeld(t *testing.T) {
	dead := exec.Command("true")
	if err := dead.Start(); err != nil {
		t.Fatalf("spawn true: %v", err)
	}
	deadPid := dead.Process.Pid
	_, _ = dead.Process.Wait()
	if processAlive(deadPid) {
		t.Skip("dead pid still reported alive; environment reaped oddly")
	}

	pidFile := filepath.Join(t.TempDir(), "exit-locked.pid")
	writeExitPathHandle(t, pidFile, deadPid)

	p := &RuntimeProcess{config: &RuntimeConfig{PIDFile: pidFile}, pidFile: pidFile, instanceToken: "0000our-instance"}

	unlock, err := acquireStartLock(pidFile)
	if err != nil {
		t.Fatalf("acquire start lock: %v", err)
	}
	p.clearStalePIDFilesForPid(deadPid)
	assertExitPathHandles(t, pidFile, true, "a start in flight owns the files; the cleanup must skip")
	unlock()

	p.clearStalePIDFilesForPid(deadPid)
	assertExitPathHandles(t, pidFile, false, "with the lock free the cleanup removes the dead handles")
}

// TestRuntimeProcess_RefusedSpawnDoesNotClaimOwnership pins audit finding F56:
// spawnedByUs is set only AFTER a real spawn + PID-file write, so a REFUSED
// spawn leaves the instance a non-owner and Stop refuses.
func TestRuntimeProcess_RefusedSpawnDoesNotClaimOwnership(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() {
		if cerr := ln.Close(); cerr != nil {
			t.Logf("listener close: %v", cerr)
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}

	pidFile := filepath.Join(t.TempDir(), "refused.pid")
	cfg := &RuntimeConfig{
		BaseURL:      "http://" + ln.Addr().String() + "/v1",
		SpawnCommand: []string{"mlx_lm", "server", "--model", "/m/x", "--port", port},
		PIDFile:      pidFile,
	}
	p := NewRuntimeProcess(cfg)
	if startErr := p.Start(context.Background(), io.Discard, io.Discard); startErr == nil {
		t.Fatal("expected the duplicate-spawn refusal")
	}
	if p.spawnedByUs {
		t.Error("a refused spawn must not set spawnedByUs (it would claim kill rights on the endpoint) (F56)")
	}
	if stopErr := p.Stop(context.Background()); stopErr != ErrRuntimeNotOwned {
		t.Errorf("Stop after a refused spawn = %v, want ErrRuntimeNotOwned (F56)", stopErr)
	}
}
