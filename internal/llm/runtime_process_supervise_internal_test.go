package llm

// Internal tests for the supervised spawn wiring in RuntimeProcess.Start.
//
// The test binary doubles as the supervisor binary (TestMain in
// supervisor_internal_test.go routes supervisor-mode re-executions into
// RunSupervisor), so these tests exercise the production spawn path end to end
// with a fake runtime instead of a real model.
//
// None of these tests call t.Parallel: they replace the package-level
// supervisorBinary seam, and Go runs sequential tests alone, never alongside a
// parallel test.

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// superviseFlag returns a pointer to v, the shape RuntimeConfig.Supervise uses
// so an absent value means "supervised".
func superviseFlag(v bool) *bool { return &v }

// testSupervisorBinary points the supervisor seam at this test binary, which
// implements supervisor mode through TestMain.
func testSupervisorBinary(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return self
}

// useTestSupervisor installs the test binary as the supervisor for the duration
// of one test.
func useTestSupervisor(t *testing.T) string {
	t.Helper()
	self := testSupervisorBinary(t)
	restore := supervisorBinary
	supervisorBinary = func() string { return self }
	t.Cleanup(func() { supervisorBinary = restore })
	return self
}

// runtimeProcessCmd returns the supervisor/direct child command of a process.
func runtimeProcessCmd(p *RuntimeProcess) *exec.Cmd {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd
}

// commandForPID returns the process-table command line for pid.
func commandForPID(t *testing.T, pid int) string {
	t.Helper()
	procs, err := ListRuntimeProcesses()
	if err != nil {
		t.Fatalf("ListRuntimeProcesses: %v", err)
	}
	for _, proc := range procs {
		if proc.PID == pid {
			return proc.Command
		}
	}
	return ""
}

// waitForProcessGone polls until pid disappears.
func waitForProcessGone(t *testing.T, pid int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pid %d is still alive %s later", pid, within)
}

// TestRuntimeProcess_SupervisedSpawnWrapsAndRecordsRuntime pins the two
// properties the whole design hangs on:
//
//   - the spawn command is wrapped: the direct child is the supervisor, which
//     carries the runtime's original argv after `--`;
//   - the PID file names the RUNTIME, not the wrapper, and the runtime's
//     process-table command line is the original spawn command -- so the boot
//     sweep's exact command-line match and StopAsOperator's identity check keep
//     working.
func TestRuntimeProcess_SupervisedSpawnWrapsAndRecordsRuntime(t *testing.T) {
	self := useTestSupervisor(t)

	pidFile := filepath.Join(t.TempDir(), "supervised.pid")
	spawn := []string{"sleep", "300"}
	cfg := &RuntimeConfig{
		EndpointKey:  "llama-cpp:127.0.0.1:8082",
		AutoStop:     true,
		PIDFile:      pidFile,
		SpawnCommand: spawn,
		Supervise:    superviseFlag(true),
	}
	p := NewRuntimeProcess(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("supervised start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if stopErr := p.Stop(stopCtx); stopErr != nil {
			t.Logf("cleanup stop: %v", stopErr)
		}
	})

	// 1. The wrapped spawn: the direct child is the supervisor, not the runtime.
	supCmd := runtimeProcessCmd(p)
	if supCmd == nil || supCmd.Process == nil {
		t.Fatal("no supervisor command recorded after a supervised start")
	}
	supervisorPID := supCmd.Process.Pid
	if supCmd.Args[0] != self {
		t.Errorf("supervised spawn ran %q, want the supervisor binary %q", supCmd.Args[0], self)
	}
	if len(supCmd.Args) < 3 || supCmd.Args[1] != supervisorParentFlag {
		t.Fatalf("supervisor argv = %v, want %s to lead it", supCmd.Args, supervisorParentFlag)
	}
	if got, want := supCmd.Args[2], strconv.Itoa(os.Getpid()); got != want {
		t.Errorf("%s = %q, want the spawning pid %q", supervisorParentFlag, got, want)
	}

	// 2. The runtime argv survives verbatim behind the `--` terminator.
	sep := -1
	for i, arg := range supCmd.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		t.Fatalf("supervisor argv %v has no -- terminator", supCmd.Args)
	}
	tail := supCmd.Args[sep+1:]
	if strings.Join(tail, "\x00") != strings.Join(spawn, "\x00") {
		t.Errorf("runtime argv under the supervisor = %v, want the original %v", tail, spawn)
	}

	// 3. The recorded process is the RUNTIME.
	runtimePID := p.PID()
	if runtimePID <= 0 {
		t.Fatal("no runtime pid after a supervised start")
	}
	if runtimePID == supervisorPID {
		t.Fatalf("pid %d is both the wrapper and the reported runtime; the wrapper must not be recorded", runtimePID)
	}
	entry, err := p.readPIDFile()
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if entry.PID != runtimePID || entry.Token != p.instanceToken {
		t.Errorf("pid file = %+v, want pid=%d token=%q", entry, runtimePID, p.instanceToken)
	}
	if entry.PID == supervisorPID {
		t.Error("the pid file must name the runtime, not the supervisor wrapper")
	}
	rec, err := ReadSpawnRecord(pidFile)
	if err != nil {
		t.Fatalf("read spawn record: %v", err)
	}
	if rec.PID != runtimePID {
		t.Errorf("spawn record pid = %d, want the runtime pid %d", rec.PID, runtimePID)
	}

	// 4. The runtime's own command line is the spawn command, and the sweep's
	//    matcher still accepts it; the wrapper's command line is visibly not it.
	runtimeCommand := commandForPID(t, runtimePID)
	if want := strings.Join(spawn, " "); runtimeCommand != want {
		t.Errorf("runtime process-table command = %q, want %q", runtimeCommand, want)
	}
	if !matchesSpawnCommand(runtimeCommand, spawn) {
		t.Error("the orphan sweep would no longer match the runtime's command line")
	}
	supervisorCommand := commandForPID(t, supervisorPID)
	if supervisorCommand == "" {
		t.Error("the supervisor process is missing from the process table")
	}
	if !strings.Contains(supervisorCommand, supervisorParentFlag) {
		t.Errorf("supervisor command = %q, want it to carry %s", supervisorCommand, supervisorParentFlag)
	}
	if supervisorCommand == runtimeCommand {
		t.Error("wrapper and runtime must have distinguishable command lines")
	}

	// 5. Stop still takes the runtime down and leaves no wrapper behind.
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("stop supervised runtime: %v", err)
	}
	if processAlive(runtimePID) {
		t.Errorf("runtime pid %d survived Stop", runtimePID)
	}
	waitForProcessGone(t, supervisorPID, 10*time.Second)
}

// TestRuntimeProcess_SupervisorExitsWithRuntime pins the no-wrapper-left-behind
// rule: when the runtime exits on its own the supervisor follows it, so the
// process table does not accumulate wrappers.
func TestRuntimeProcess_SupervisorExitsWithRuntime(t *testing.T) {
	useTestSupervisor(t)

	pidFile := filepath.Join(t.TempDir(), "shortlived.pid")
	cfg := &RuntimeConfig{
		PIDFile:      pidFile,
		SpawnCommand: []string{"sleep", "1"},
		Supervise:    superviseFlag(true),
	}
	p := NewRuntimeProcess(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
		t.Fatalf("supervised start: %v", err)
	}
	supCmd := runtimeProcessCmd(p)
	if supCmd == nil || supCmd.Process == nil {
		t.Fatal("no supervisor command recorded after a supervised start")
	}
	supervisorPID := supCmd.Process.Pid
	runtimePID := p.PID()
	if !processAlive(runtimePID) && !processAlive(supervisorPID) {
		t.Skip("the fake runtime exited before it could be observed")
	}

	waitForProcessGone(t, supervisorPID, 10*time.Second)
	waitForProcessGone(t, runtimePID, 10*time.Second)

	// The RuntimeProcess notices the runtime is gone (the wait goroutine clears
	// the recorded pid), so a health restart can adopt/spawn cleanly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && p.IsRunning() {
		time.Sleep(25 * time.Millisecond)
	}
	if p.IsRunning() {
		t.Error("IsRunning must report false once the supervisor and runtime are gone")
	}
}

// TestRuntimeProcess_SuperviseOptOut pins the per-endpoint escape hatch
// (`supervise: false`) and the non-daemon executable rule: in both cases Start
// spawns the runtime directly, exactly as it did before the supervisor existed.
func TestRuntimeProcess_SuperviseOptOut(t *testing.T) {
	cases := []struct {
		name    string
		optOut  bool
		noSuper bool
	}{
		{name: "endpoint opts out", optOut: true},
		{name: "no supervisor binary available", noSuper: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useTestSupervisor(t)
			if tc.noSuper {
				supervisorBinary = func() string { return "" }
			}
			pidFile := filepath.Join(t.TempDir(), "direct.pid")
			cfg := &RuntimeConfig{
				PIDFile:      pidFile,
				SpawnCommand: []string{"sleep", "30"},
			}
			if tc.optOut {
				cfg.Supervise = superviseFlag(false)
			}
			p := NewRuntimeProcess(cfg)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := p.Start(ctx, io.Discard, io.Discard); err != nil {
				t.Fatalf("direct start: %v", err)
			}
			cmd := runtimeProcessCmd(p)
			if cmd == nil || cmd.Process == nil {
				t.Fatal("no command recorded after a direct start")
			}
			if cmd.Args[0] != cfg.SpawnCommand[0] {
				t.Errorf("direct spawn ran %q, want the runtime %q (no wrapper)", cmd.Args[0], cfg.SpawnCommand[0])
			}
			if p.PID() != cmd.Process.Pid {
				t.Errorf("recorded pid %d, want the spawned child %d", p.PID(), cmd.Process.Pid)
			}
			entry, err := p.readPIDFile()
			if err != nil {
				t.Fatalf("read pid file: %v", err)
			}
			if entry.PID != cmd.Process.Pid {
				t.Errorf("pid file names %d, want the spawned child %d", entry.PID, cmd.Process.Pid)
			}
			if err := p.Stop(ctx); err != nil {
				t.Fatalf("stop direct runtime: %v", err)
			}
		})
	}
}
