package llm

// Internal tests pinning audit finding F23: a supervised Stop must target the
// RUNTIME's own process group (the reported pid in the PID file), never the
// wrapper's, and must not clear the PID file / durable record while the
// runtime pid is verifiably alive or after a replacement runtime was
// installed.
//
// The seams used are the existing supervisor test patterns: the test binary
// itself implements supervisor mode (TestMain in supervisor_internal_test.go),
// so Start really spawns the wrapper, and clearPIDFileAndRecordIfDead /
// stopProcess are exercised against fake process-liveness where a real kill
// would violate the "no live process manipulation" test rule.

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestStopSupervised_TargetsRuntimeGroupNotWrapper is the live end-to-end pin
// (mirrors TestRuntimeProcess_SupervisedSpawnWrapsAndRecordsRuntime): after a
// supervised Start, Stop must take the runtime down (its OWN group) and leave
// no wrapper behind — with a short stop context so the ctx-cancellation
// escalation branch is the code under test.
func TestStopSupervised_TargetsRuntimeGroupNotWrapper(t *testing.T) {
	useTestSupervisor(t)

	pidFile := filepath.Join(t.TempDir(), "f23.pid")
	cfg := &RuntimeConfig{
		EndpointKey:  "llama-cpp:127.0.0.1:8082",
		AutoStop:     true,
		PIDFile:      pidFile,
		SpawnCommand: []string{"sleep", "300"},
		Supervise:    superviseFlag(true),
	}
	p := NewRuntimeProcess(cfg)

	startCtx, startCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startCancel()
	if err := p.Start(startCtx, io.Discard, io.Discard); err != nil {
		t.Fatalf("supervised start: %v", err)
	}

	runtimePID := p.PID()
	if runtimePID <= 0 {
		t.Fatal("no runtime pid after a supervised start")
	}
	supCmd := runtimeProcessCmd(p)
	if supCmd == nil || supCmd.Process == nil {
		t.Fatal("no supervisor command recorded after a supervised start")
	}
	wrapperPID := supCmd.Process.Pid
	if wrapperPID == runtimePID {
		t.Fatalf("wrapper and runtime are the same process (%d); test premise broken", wrapperPID)
	}

	// Stop with a context that is ALREADY expired: the escalation branch
	// (ctx.Done before waitDone) is exactly the F23 path.
	stopCtx, stopCancel := context.WithCancel(context.Background())
	stopCancel()
	if err := p.Stop(stopCtx); err != nil {
		t.Fatalf("stop with cancelled context: %v", err)
	}

	waitForProcessGone(t, runtimePID, 10*time.Second)
	waitForProcessGone(t, wrapperPID, 10*time.Second)

	// The runtime is dead, so the handles must be cleared (the verified
	// half of the fix): nothing stale may survive for a later recovery
	// branch to misread.
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("PID file must be removed once the runtime is verifiably dead, stat err = %v", err)
	}
	if _, err := ReadSpawnRecord(pidFile); err == nil {
		t.Error("durable spawn record must be removed once the runtime is verifiably dead")
	}
}

// TestClearPIDFileAndRecordIfDead pins the re-validation discipline directly,
// with a live stand-in process standing in for "the runtime survived":
//
//   - a live pid keeps its PID file and durable record (the old code deleted
//     them while the runtime kept serving, so every later stop surface
//     reported "not running");
//   - a dead pid with a PID file that still names it gets both cleared;
//   - a dead pid whose PID file was REWRITTEN for a replacement runtime
//     leaves the replacement's handles untouched.
func TestClearPIDFileAndRecordIfDead(t *testing.T) {
	t.Run("live runtime pid keeps its handles", func(t *testing.T) {
		dir := t.TempDir()
		pidFile := filepath.Join(dir, "live.pid")
		hold := exec.Command("sleep", "30")
		if err := hold.Start(); err != nil {
			t.Fatalf("spawn sleep: %v", err)
		}
		t.Cleanup(func() {
			_ = hold.Process.Kill()
			_, _ = hold.Process.Wait()
		})
		pid := hold.Process.Pid

		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"tok"}`, pid)), 0o600); err != nil {
			t.Fatalf("write pid file: %v", err)
		}
		if err := WriteSpawnRecord(SpawnRecord{PIDFile: pidFile, Argv: []string{"sleep", "30"}, PID: pid}); err != nil {
			t.Fatalf("write spawn record: %v", err)
		}

		p := NewRuntimeProcess(&RuntimeConfig{PIDFile: pidFile})
		p.clearPIDFileAndRecordIfDead(pid)

		if _, err := os.Stat(pidFile); err != nil {
			t.Errorf("PID file of a LIVE runtime must survive, stat err = %v", err)
		}
		if _, err := ReadSpawnRecord(pidFile); err != nil {
			t.Errorf("durable record of a LIVE runtime must survive: %v", err)
		}
	})

	t.Run("dead runtime pid clears its handles", func(t *testing.T) {
		dir := t.TempDir()
		pidFile := filepath.Join(dir, "dead.pid")
		pid := deadSpawnPID(t)

		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"tok"}`, pid)), 0o600); err != nil {
			t.Fatalf("write pid file: %v", err)
		}
		if err := WriteSpawnRecord(SpawnRecord{PIDFile: pidFile, Argv: []string{"sleep", "30"}, PID: pid}); err != nil {
			t.Fatalf("write spawn record: %v", err)
		}

		p := NewRuntimeProcess(&RuntimeConfig{PIDFile: pidFile})
		p.clearPIDFileAndRecordIfDead(pid)

		if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
			t.Errorf("PID file of a verifiably dead runtime must be removed, stat err = %v", err)
		}
		if _, err := ReadSpawnRecord(pidFile); err == nil {
			t.Error("durable record of a verifiably dead runtime must be removed")
		}
	})

	t.Run("replacement runtime handles are never touched", func(t *testing.T) {
		dir := t.TempDir()
		pidFile := filepath.Join(dir, "swapped.pid")
		deadPID := deadSpawnPID(t)

		// A health-driven restart has already rewritten the PID file and
		// the record for a FRESH runtime pid; the Stop's snapshot pid is
		// the dead one.
		hold := exec.Command("sleep", "30")
		if err := hold.Start(); err != nil {
			t.Fatalf("spawn sleep: %v", err)
		}
		t.Cleanup(func() {
			_ = hold.Process.Kill()
			_, _ = hold.Process.Wait()
		})
		newPID := hold.Process.Pid

		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"tok"}`, newPID)), 0o600); err != nil {
			t.Fatalf("write pid file: %v", err)
		}
		if err := WriteSpawnRecord(SpawnRecord{PIDFile: pidFile, Argv: []string{"sleep", "30"}, PID: newPID}); err != nil {
			t.Fatalf("write spawn record: %v", err)
		}

		p := NewRuntimeProcess(&RuntimeConfig{PIDFile: pidFile})
		p.clearPIDFileAndRecordIfDead(deadPID)

		if _, err := os.Stat(pidFile); err != nil {
			t.Errorf("replacement runtime's PID file must survive, stat err = %v", err)
		}
		rec, err := ReadSpawnRecord(pidFile)
		if err != nil {
			t.Fatalf("replacement runtime's record must survive: %v", err)
		}
		if rec.PID != newPID {
			t.Errorf("replacement record pid = %d, want %d (untouched)", rec.PID, newPID)
		}
	})
}
