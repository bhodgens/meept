package llm

// Internal tests for the runtime supervisor (orphan self-termination).
//
// They cover three contracts:
//   - SuperviseArgv/ParseSupervisorArgs: the flag shape, and the guarantee that
//     the runtime argv survives verbatim (the orphan sweep matches it);
//   - RunSupervisor: the runtime really is spawned, its pid is reported, and the
//     supervisor exits with it (no wrapper left behind);
//   - parent death: a SIGKILLed parent leaves no runtime behind, both through
//     the parent-death pipe and through the 2s pid poll.
//
// The test binary plays both roles, so nothing here mocks the mechanism:
// TestMain routes supervisor-mode re-executions into RunSupervisor and
// --fake-daemon invocations into runFakeDaemon, which spawns the supervisor with
// a real parent-death pipe exactly as RuntimeProcess.Start does and then holds
// the write end until it is killed.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeDaemonFlag marks a test binary invocation that stands in for the daemon
// process (the supervisor's parent).
const fakeDaemonFlag = "--fake-daemon"

// fakeDaemonDeathPipe keeps the fake parent's parent-death write end alive for
// as long as the process runs: a package-level reference cannot be finalized, so
// the descriptor stays open until the process dies, which is exactly the signal
// production relies on.
var fakeDaemonDeathPipe *os.File

// TestMain hands the process to the supervisor (or to the fake parent) when the
// test binary is re-executed in one of those roles. It runs before m.Run(), so
// the flag package never parses their arguments.
func TestMain(m *testing.M) {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == fakeDaemonFlag {
		os.Exit(runFakeDaemon(args[1:]))
	}
	if opts, requested, err := ParseSupervisorArgs(args); requested {
		if err != nil {
			fmt.Fprintf(os.Stderr, "test supervisor: %v\n", err)
			os.Exit(2)
		}
		os.Exit(RunSupervisor(opts))
	}
	os.Exit(m.Run())
}

// runFakeDaemon is the test's stand-in for the daemon process. args is
// <death-pipe 1|0> <supervisor pid file> <supervisor exit file> <runtime argv...>.
//
// It wires the supervisor the way RuntimeProcess.Start does (report pipe as
// fd 3, parent-death pipe read end as fd 4 when requested), reaps it like the
// daemon's wait goroutine does, and then blocks holding the death pipe's write
// end: killing this process is what a hard-killed daemon looks like to the
// supervisor.
func runFakeDaemon(args []string) int {
	if len(args) < 4 {
		fmt.Fprintln(os.Stderr, "fake daemon: usage: --fake-daemon <death-pipe> <sup-pid-file> <sup-exit-file> <runtime argv...>")
		return 2
	}
	withDeathPipe := args[0] == "1"
	supPIDFile := args[1]
	supExitFile := args[2]
	runtimeArgv := args[3:]

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake daemon: %v\n", err)
		return 1
	}
	reportRead, reportWrite, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake daemon: report pipe: %v\n", err)
		return 1
	}
	deathRead, deathWrite, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake daemon: death pipe: %v\n", err)
		return 1
	}
	deathFD := 0
	extra := []*os.File{reportWrite}
	if withDeathPipe {
		deathFD = supervisorDeathFD
		extra = append(extra, deathRead)
	}
	cmd := exec.Command(self, SuperviseArgv(os.Getpid(), supervisorReportFD, deathFD, runtimeArgv)...)
	cmd.ExtraFiles = extra
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fake daemon: start supervisor: %v\n", err)
		return 1
	}
	closeFileQuietly(reportRead)
	closeFileQuietly(reportWrite)
	closeFileQuietly(deathRead)
	if withDeathPipe {
		fakeDaemonDeathPipe = deathWrite // held open until this process dies
	} else {
		closeFileQuietly(deathWrite)
	}
	if err := os.WriteFile(supPIDFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fake daemon: write supervisor pid file: %v\n", err)
		return 1
	}
	// Reap the supervisor when it exits, exactly as the daemon's wait goroutine
	// does, and record the exit status so the test can see that the wrapper is
	// gone (a live parent that never waits would leave it a zombie).
	go func() {
		werr := cmd.Wait()
		status := "0"
		if werr != nil {
			status = werr.Error()
		}
		if writeErr := os.WriteFile(supExitFile, []byte(status), 0o600); writeErr != nil {
			fmt.Fprintf(os.Stderr, "fake daemon: write supervisor exit file: %v\n", writeErr)
		}
	}()
	// Stay alive -- holding the death pipe write end -- until the test kills
	// this process. A signal channel (rather than an empty select) is a real
	// wakeup source, so the runtime does not report the process as deadlocked.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	<-sigs
	return 0
}

// TestSuperviseArgv_PreservesRuntimeArgv pins the wrapper's shape: the runtime
// command line follows `--` byte-for-byte, because the process table must keep
// showing the runtime's own argv for the orphan sweep to match it.
func TestSuperviseArgv_PreservesRuntimeArgv(t *testing.T) {
	t.Parallel()
	spawn := []string{"llama-server", "-m", "/models/a.gguf", "--port", "8082", "--flag=a b"}
	argv := SuperviseArgv(4242, supervisorReportFD, supervisorDeathFD, spawn)

	wantHead := []string{
		"--supervise-parent", "4242",
		"--pid-report-fd", strconv.Itoa(supervisorReportFD),
		"--parent-death-fd", strconv.Itoa(supervisorDeathFD),
		"--",
	}
	if len(argv) != len(wantHead)+len(spawn) {
		t.Fatalf("argv length = %d, want %d: %v", len(argv), len(wantHead)+len(spawn), argv)
	}
	for i, want := range wantHead {
		if argv[i] != want {
			t.Errorf("argv[%d] = %q, want %q", i, argv[i], want)
		}
	}
	tail := argv[len(wantHead):]
	for i := range spawn {
		if tail[i] != spawn[i] {
			t.Errorf("runtime argv[%d] = %q, want %q (argv must pass through verbatim)", i, tail[i], spawn[i])
		}
	}
	// The original slice must not be aliased: a later mutation of spawn must
	// not rewrite the supervisor's command line.
	spawn[0] = "mutated"
	if tail[0] == "mutated" {
		t.Error("SuperviseArgv aliases the caller's argv slice")
	}
}

// TestParseSupervisorArgs pins the flag contract the daemon's main relies on:
// a normal daemon invocation is not supervisor mode, and a malformed supervisor
// invocation is an error rather than a silent daemon start.
func TestParseSupervisorArgs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		args          []string
		wantRequested bool
		wantErr       bool
		wantParent    int
		wantReportFD  int
		wantDeathFD   int
		wantArgv      []string
	}{
		{
			name:          "daemon start is untouched",
			args:          []string{"-f", "--debug"},
			wantRequested: false,
		},
		{
			name:          "no args",
			args:          nil,
			wantRequested: false,
		},
		{
			name:          "version subcommand",
			args:          []string{"version"},
			wantRequested: false,
		},
		{
			name:          "spaced flags",
			args:          []string{"--supervise-parent", "77", "--pid-report-fd", "3", "--parent-death-fd", "4", "--", "sleep", "300"},
			wantRequested: true,
			wantParent:    77,
			wantReportFD:  3,
			wantDeathFD:   4,
			wantArgv:      []string{"sleep", "300"},
		},
		{
			name:          "equals flags",
			args:          []string{"--supervise-parent=99", "--pid-report-fd=5", "--parent-death-fd=6", "--", "/bin/sh", "-c", "sleep 300"},
			wantRequested: true,
			wantParent:    99,
			wantReportFD:  5,
			wantDeathFD:   6,
			wantArgv:      []string{"/bin/sh", "-c", "sleep 300"},
		},
		{
			name:          "report and death fds default to off",
			args:          []string{"--supervise-parent", "5", "--", "sleep", "1"},
			wantRequested: true,
			wantParent:    5,
			wantReportFD:  0,
			wantDeathFD:   0,
			wantArgv:      []string{"sleep", "1"},
		},
		{
			name:          "missing parent pid",
			args:          []string{"--supervise-parent"},
			wantRequested: true,
			wantErr:       true,
		},
		{
			name:          "non-numeric parent pid",
			args:          []string{"--supervise-parent", "abc", "--", "sleep", "1"},
			wantRequested: true,
			wantErr:       true,
		},
		{
			name:          "zero parent pid",
			args:          []string{"--supervise-parent", "0", "--", "sleep", "1"},
			wantRequested: true,
			wantErr:       true,
		},
		{
			name:          "empty runtime argv",
			args:          []string{"--supervise-parent", "7", "--"},
			wantRequested: true,
			wantErr:       true,
		},
		{
			name:          "stray daemon flag after the supervisor flag",
			args:          []string{"--supervise-parent", "7", "--debug", "--", "sleep", "1"},
			wantRequested: true,
			wantErr:       true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, requested, err := ParseSupervisorArgs(tc.args)
			if requested != tc.wantRequested {
				t.Fatalf("requested = %v, want %v", requested, tc.wantRequested)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got opts %+v", opts)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !requested {
				return
			}
			if opts.ParentPID != tc.wantParent {
				t.Errorf("ParentPID = %d, want %d", opts.ParentPID, tc.wantParent)
			}
			if opts.PIDReportFD != tc.wantReportFD {
				t.Errorf("PIDReportFD = %d, want %d", opts.PIDReportFD, tc.wantReportFD)
			}
			if opts.ParentDeathFD != tc.wantDeathFD {
				t.Errorf("ParentDeathFD = %d, want %d", opts.ParentDeathFD, tc.wantDeathFD)
			}
			if strings.Join(opts.Argv, "\x00") != strings.Join(tc.wantArgv, "\x00") {
				t.Errorf("Argv = %v, want %v", opts.Argv, tc.wantArgv)
			}
		})
	}
}

// TestRunSupervisor_ReportsRuntimeAndExitsWithIt runs the supervisor as a real
// child process: it must spawn the runtime, report the runtime's pid on the
// report descriptor, exit with the runtime, and never report its own pid.
func TestRunSupervisor_ReportsRuntimeAndExitsWithIt(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	cmd := exec.Command(self,
		supervisorParentFlag, strconv.Itoa(os.Getpid()),
		supervisorPIDReportFlag, strconv.Itoa(supervisorReportFD),
		supervisorDeathFlag, "0",
		"--", "sleep", "1")
	cmd.ExtraFiles = []*os.File{writeEnd}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	closeFileQuietly(writeEnd) // the child holds its own copy
	t.Cleanup(func() {
		// Test cleanup: the supervisor is already expected to be gone, so a
		// kill or wait failure is logged, not asserted.
		if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Logf("cleanup: kill supervisor: %v", killErr)
		}
		if _, waitErr := cmd.Process.Wait(); waitErr != nil {
			t.Logf("cleanup: wait supervisor: %v", waitErr)
		}
	})

	pid, err := readSupervisorReport(readEnd)
	if err != nil {
		t.Fatalf("pid report: %v", err)
	}
	if pid == cmd.Process.Pid {
		t.Errorf("reported pid %d is the supervisor's own; it must be the runtime's", pid)
	}
	if !processAlive(pid) {
		t.Errorf("reported runtime pid %d is not alive", pid)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case werr := <-done:
		if werr != nil {
			t.Fatalf("supervisor exit: %v (want a clean exit once the runtime exits)", werr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("supervisor did not exit after its runtime exited")
	}
	if processAlive(pid) {
		t.Errorf("runtime pid %d must be gone once the supervisor exits", pid)
	}
}

// fakeParent is the test's handle on the --fake-daemon process.
type fakeParent struct {
	supervisorPIDFile string
	supervisorExit    string
	runtimePIDFile    string
	logFile           string
	cmd               *exec.Cmd
	reaped            bool
}

// startFakeParent launches the fake daemon with a fake runtime script that
// records its own pid and then runs runtimeBody.
func startFakeParent(t *testing.T, runtimeBody string, deathPipe bool) *fakeParent {
	t.Helper()
	dir := t.TempDir()
	runtimeScript := filepath.Join(dir, "fake-runtime.sh")
	runtimePIDFile := filepath.Join(dir, "runtime.pid")
	supervisorPIDFile := filepath.Join(dir, "supervisor.pid")
	supervisorExit := filepath.Join(dir, "supervisor.exit")
	logFile := filepath.Join(dir, "fake-parent.log")
	script := "#!/bin/sh\necho $$ > '" + runtimePIDFile + "'\n" + runtimeBody + "\n"
	if err := os.WriteFile(runtimeScript, []byte(script), 0o700); err != nil {
		t.Fatalf("write runtime script: %v", err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	deathFlag := "0"
	if deathPipe {
		deathFlag = "1"
	}
	log, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	cmd := exec.Command(self, fakeDaemonFlag, deathFlag, supervisorPIDFile, supervisorExit, runtimeScript)
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake parent: %v", err)
	}
	p := &fakeParent{
		supervisorPIDFile: supervisorPIDFile,
		supervisorExit:    supervisorExit,
		runtimePIDFile:    runtimePIDFile,
		logFile:           logFile,
		cmd:               cmd,
	}
	t.Cleanup(func() {
		p.killParent(t)
		if cerr := log.Close(); cerr != nil {
			t.Logf("log close: %v", cerr)
		}
	})
	return p
}

// killParent SIGKILLs the fake parent and reaps it, mirroring what a shell or
// launchd does for a dead daemon. Idempotent.
func (p *fakeParent) killParent(tb testing.TB) {
	if p.reaped {
		return
	}
	p.reaped = true
	// Cleanup: the fake parent may already be gone, so failures are logged,
	// not asserted.
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		tb.Logf("cleanup: kill fake parent: %v", err)
	}
	if _, err := p.cmd.Process.Wait(); err != nil {
		tb.Logf("cleanup: wait fake parent: %v", err)
	}
}

// pidFromFile waits for a pid a helper script wrote.
func (p *fakeParent) pidFromFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, perr := strconv.Atoi(strings.TrimSpace(string(data)))
			if perr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pid file %s was never written; fake parent log: %s", path, p.logTail())
	return 0
}

// waitSupervisorExit waits for the fake parent to finish reaping the
// supervisor, returning the recorded exit status.
func (p *fakeParent) waitSupervisorExit(t *testing.T, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(p.supervisorExit)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("the supervisor had not exited %s after the runtime did; fake parent log: %s",
		within, p.logTail())
	return ""
}

// waitGone polls pid until it disappears, failing after the deadline.
func (p *fakeParent) waitGone(t *testing.T, pid int, within time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	for time.Since(start) < within {
		if !processAlive(pid) {
			return time.Since(start)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pid %d is still alive %s after the wait began; fake parent log: %s",
		pid, within, p.logTail())
	return 0
}

// logTail returns the tail of the captured helper output for failure reports.
func (p *fakeParent) logTail() string {
	data, err := os.ReadFile(p.logFile)
	if err != nil {
		return fmt.Sprintf("<unreadable: %v>", err)
	}
	const limit = 2000
	if len(data) > limit {
		data = data[len(data)-limit:]
	}
	return strings.TrimSpace(string(data))
}

// TestSupervisor_TerminatesRuntimeWhenParentIsKilled is the orphan
// self-termination contract: the daemon (the fake parent) is SIGKILLed, no
// shutdown path runs, and the runtime must die by itself within seconds.
//
// The parent is deliberately left UNREAPED while the assertion runs, so it is a
// zombie for its whole duration. That makes this an airtight test of the
// parent-death pipe: a pid-based check cannot tell a zombie from a live parent
// (signal 0 succeeds and Getppid still names it), so the 2s poll can never
// explain the runtime's death here.
func TestSupervisor_TerminatesRuntimeWhenParentIsKilled(t *testing.T) {
	p := startFakeParent(t, "exec sleep 300", true)
	supervisorPID := p.pidFromFile(t, p.supervisorPIDFile)
	runtimePID := p.pidFromFile(t, p.runtimePIDFile)
	if supervisorPID == runtimePID {
		t.Fatalf("supervisor and runtime must be distinct processes (both %d)", supervisorPID)
	}

	// Hard kill: the fake daemon dies without signalling anything.
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill fake parent: %v", err)
	}
	took := p.waitGone(t, runtimePID, 5*time.Second)
	t.Logf("runtime %d died %s after its parent was SIGKILLed (parent not reaped)", runtimePID, took)
	p.waitGone(t, supervisorPID, 5*time.Second)
}

// TestSupervisor_PollDetectsParentDeath covers the fallback: without a
// parent-death pipe the supervisor must notice the dead parent through its 2s
// pid poll and take the runtime down.
func TestSupervisor_PollDetectsParentDeath(t *testing.T) {
	p := startFakeParent(t, "exec sleep 300", false)
	supervisorPID := p.pidFromFile(t, p.supervisorPIDFile)
	runtimePID := p.pidFromFile(t, p.runtimePIDFile)
	if supervisorPID == runtimePID {
		t.Fatalf("supervisor and runtime must be distinct processes (both %d)", supervisorPID)
	}

	// SIGKILL and reap, as a shell or launchd parent does.
	p.killParent(t)
	took := p.waitGone(t, runtimePID, 5*time.Second)
	t.Logf("runtime %d died %s after the pid poll noticed the dead parent", runtimePID, took)
}

// TestSupervisor_ExitsWhenRuntimeExits pins the no-wrapper-left-behind rule: a
// runtime that exits on its own takes the supervisor with it, while the parent
// is still alive. The fake parent reaps the supervisor (as the daemon does), so
// the recorded exit status proves the wrapper really terminated.
func TestSupervisor_ExitsWhenRuntimeExits(t *testing.T) {
	p := startFakeParent(t, "exit 0", true)

	status := p.waitSupervisorExit(t, 5*time.Second)
	if status != "0" {
		t.Errorf("supervisor exit status = %q, want a clean exit", status)
	}
	if !processAlive(p.cmd.Process.Pid) {
		t.Fatal("the parent must still be alive; the supervisor must have exited because the runtime did")
	}
}
