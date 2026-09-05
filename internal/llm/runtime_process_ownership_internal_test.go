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
	"io"
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

	// Stop must refuse: no error, and the process survives.
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop on observed runtime should be a silent no-op, got: %v", err)
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
	if err := pB.Stop(ctx); err != nil {
		t.Fatalf("instance B stop: %v", err)
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

	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop on legacy-adopted runtime should be a silent no-op, got: %v", err)
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
