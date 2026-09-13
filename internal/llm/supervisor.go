package llm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Orphan self-termination: the runtime supervisor.
//
// A managed runtime is spawned as a child of the meept process that owns it,
// and the shutdown path (RuntimeProcess.Stop) signals that process's group.
// macOS has no parent-death signal, so when the daemon dies HARD (SIGKILL,
// panic, terminal teardown) nothing tells the runtime its owner is gone: it is
// re-parented to launchd and keeps running, holding a full model in RAM and the
// endpoint port until the next boot sweep (runtime_sweep.go) reaps it. The
// sweep closes the leak, but only at the NEXT boot; until then the port is
// taken and the next spawn of that endpoint is refused by the duplicate-spawn
// guard.
//
// The supervisor closes that window. RuntimeProcess.Start spawns the daemon
// binary in supervisor mode instead of the runtime:
//
//	meept-daemon --supervise-parent <daemon pid> --pid-report-fd <fd> -- <spawn argv>
//
// and the supervisor:
//
//  1. spawns the runtime with EXACTLY the original argv, in its own process
//     group, so the process table still shows the runtime's own command line --
//     the sweep's exact command-line match and the PID file's ownership token
//     keep identifying the runtime, not the wrapper;
//  2. reports the runtime's pid back to the spawning daemon over a pipe, so the
//     PID file names the RUNTIME process (see RuntimeProcess.Start);
//  3. checks every supervisorPollInterval whether its parent is still alive and,
//     once it is gone, terminates the runtime's process group: SIGTERM, then
//     SIGKILL after supervisorTermGrace;
//  4. exits as soon as the runtime exits, leaving no wrapper process behind.
//
// The supervisor never binds the endpoint port, never writes or removes the PID
// file, and never signals the runtime while the parent is alive. It also
// forwards SIGTERM/SIGINT (the signal the daemon's Stop sends) to the runtime
// group, so a graceful stop still terminates the runtime promptly.
//
// Only the daemon binary implements this mode (see supervisorBinary): a CLI or
// eval process that spawns a runtime must keep spawning it directly, because
// its own exit is not the runtime's death (`meept runtime start` asks for a
// runtime that outlives the CLI).

const (
	// supervisorParentFlag names the pid the supervisor watches.
	supervisorParentFlag = "--supervise-parent"
	// supervisorPIDReportFlag names the descriptor the supervisor reports the
	// runtime pid on. With no flag the report is skipped (descriptor 0): the
	// supervisor never writes to a descriptor it was not explicitly given.
	supervisorPIDReportFlag = "--pid-report-fd"
	// supervisorDeathFlag names the descriptor whose END-OF-FILE means the
	// spawning daemon is gone. It is the mechanism that makes parent-death
	// detection reliable on macOS (see supervisorParentAlive), so the daemon
	// always passes it even though supervisor mode works without it.
	supervisorDeathFlag = "--parent-death-fd"
	// supervisorReportFD is the descriptor the supervisor writes the runtime
	// pid to: the first entry of exec.Cmd.ExtraFiles.
	supervisorReportFD = 3
	// supervisorDeathFD is the descriptor the supervisor watches for parent
	// death: the second entry of exec.Cmd.ExtraFiles.
	supervisorDeathFD = 4
	// supervisorPollInterval is how often the supervisor checks whether the
	// process it was told to watch is still alive.
	supervisorPollInterval = 2 * time.Second
	// supervisorTermGrace bounds the SIGTERM grace period before the runtime is
	// SIGKILLed: long enough for llama-server / mlx_lm to exit cleanly, short
	// enough that a hard-killed daemon does not leave the port held for long.
	supervisorTermGrace = 10 * time.Second
	// supervisorReportTimeout bounds the spawn side's wait for the runtime pid.
	// Start blocks on it, so it must be finite.
	supervisorReportTimeout = 5 * time.Second
	// supervisorPollStep is the interval at which the grace period is checked
	// for the runtime's exit.
	supervisorPollStep = 100 * time.Millisecond
	// supervisorBinaryEnv overrides the resolved supervisor binary, for
	// installations whose daemon binary is renamed.
	supervisorBinaryEnv = "MEEPT_SUPERVISOR_BIN"
)

// supervisorExitUsage is the exit code for a malformed supervisor invocation.
const supervisorExitUsage = 2

// SupervisorOptions is one supervisor invocation. ParseSupervisorArgs fills it
// from a command line; tests may shorten the timings.
type SupervisorOptions struct {
	// ParentPID is the pid whose disappearance the supervisor watches. It must
	// be the supervisor's own parent (RuntimeProcess.Start passes os.Getpid()
	// of the spawning daemon).
	ParentPID int
	// PIDReportFD is the descriptor the runtime pid is reported on, or 0 to
	// skip reporting.
	PIDReportFD int
	// ParentDeathFD is the descriptor whose EOF means the spawning parent is
	// gone, or 0 to watch the pid alone. This is the reliable signal: a
	// pid-based check cannot tell a live parent from a dead one that has not
	// been reaped yet (signal 0 succeeds against a zombie, and Getppid still
	// names it), and the kernel closes the pipe the moment the parent dies.
	ParentDeathFD int
	// Argv is the runtime command line, verbatim.
	Argv []string
	// Log receives the supervisor's diagnostics. nil means slog.Default().
	Log *slog.Logger
	// PollInterval overrides the parent check interval. 0 means
	// supervisorPollInterval.
	PollInterval time.Duration
	// TermGrace overrides the SIGTERM grace period. 0 means
	// supervisorTermGrace.
	TermGrace time.Duration
}

// SuperviseArgv returns the supervisor's argument list for a runtime spawn: the
// runtime argv follows the `--` terminator VERBATIM, so the supervisor passes
// it through unchanged and the process table still shows the runtime's own
// command line. reportFD and deathFD are the descriptors Start passes as
// ExtraFiles; 0 disables the corresponding channel.
func SuperviseArgv(parentPID, reportFD, deathFD int, spawn []string) []string {
	argv := make([]string, 0, 8+len(spawn))
	argv = append(argv,
		supervisorParentFlag, strconv.Itoa(parentPID),
		supervisorPIDReportFlag, strconv.Itoa(reportFD),
		supervisorDeathFlag, strconv.Itoa(deathFD),
		"--")
	return append(argv, spawn...)
}

// ParseSupervisorArgs parses a supervisor-mode command line (os.Args[1:]).
// requested is false for every other invocation, so the daemon's main can call
// it before cobra parses anything and leave a normal daemon start untouched;
// the runtime argv after `--` is never interpreted as daemon flags.
//
// requested is true with a non-nil error when the flag is present but malformed:
// the caller must report the error, not fall back to a daemon start.
func ParseSupervisorArgs(args []string) (opts SupervisorOptions, requested bool, err error) {
	seen := false
	i := 0
	for i < len(args) {
		token := args[i]
		if token == "--" {
			i++
			break
		}
		switch {
		case token == supervisorParentFlag, strings.HasPrefix(token, supervisorParentFlag+"="):
			value, next, perr := supervisorFlagValue(args, i, supervisorParentFlag)
			if perr != nil {
				return SupervisorOptions{}, true, perr
			}
			pid, aerr := strconv.Atoi(value)
			if aerr != nil {
				return SupervisorOptions{}, true, fmt.Errorf("%s: invalid pid %q: %w", supervisorParentFlag, value, aerr)
			}
			opts.ParentPID = pid
			i = next
			seen = true
		case token == supervisorPIDReportFlag, strings.HasPrefix(token, supervisorPIDReportFlag+"="):
			value, next, perr := supervisorFlagValue(args, i, supervisorPIDReportFlag)
			if perr != nil {
				return SupervisorOptions{}, true, perr
			}
			fd, aerr := strconv.Atoi(value)
			if aerr != nil {
				return SupervisorOptions{}, true, fmt.Errorf("%s: invalid descriptor %q: %w", supervisorPIDReportFlag, value, aerr)
			}
			opts.PIDReportFD = fd
			i = next
			seen = true
		case token == supervisorDeathFlag, strings.HasPrefix(token, supervisorDeathFlag+"="):
			value, next, perr := supervisorFlagValue(args, i, supervisorDeathFlag)
			if perr != nil {
				return SupervisorOptions{}, true, perr
			}
			fd, aerr := strconv.Atoi(value)
			if aerr != nil {
				return SupervisorOptions{}, true, fmt.Errorf("%s: invalid descriptor %q: %w", supervisorDeathFlag, value, aerr)
			}
			opts.ParentDeathFD = fd
			i = next
			seen = true
		default:
			if seen {
				return SupervisorOptions{}, true, fmt.Errorf("unexpected argument %q in supervisor mode", token)
			}
			// A normal daemon invocation: not supervisor mode.
			return SupervisorOptions{}, false, nil
		}
	}
	if !seen {
		return SupervisorOptions{}, false, nil
	}
	if opts.ParentPID <= 0 {
		return SupervisorOptions{}, true, fmt.Errorf("%s: parent pid must be positive, got %d", supervisorParentFlag, opts.ParentPID)
	}
	if opts.PIDReportFD < 0 {
		return SupervisorOptions{}, true, fmt.Errorf("%s: descriptor must not be negative, got %d", supervisorPIDReportFlag, opts.PIDReportFD)
	}
	opts.Argv = args[i:]
	if len(opts.Argv) == 0 {
		return SupervisorOptions{}, true, errors.New("supervisor mode needs the runtime command line after --")
	}
	return opts, true, nil
}

// supervisorFlagValue returns the value of a `--flag value` / `--flag=value`
// pair and the index of the next argument.
func supervisorFlagValue(args []string, i int, flag string) (string, int, error) {
	if rest, ok := strings.CutPrefix(args[i], flag+"="); ok {
		if rest == "" {
			return "", 0, fmt.Errorf("%s needs a value", flag)
		}
		return rest, i + 1, nil
	}
	if i+1 >= len(args) {
		return "", 0, fmt.Errorf("%s needs a value", flag)
	}
	return args[i+1], i + 2, nil
}

// supervisorBinary resolves the executable that carries the supervisor entry
// point. Empty means supervision is unavailable and RuntimeProcess.Start spawns
// the runtime directly.
//
// Only the daemon binary has the entry point, so a process whose executable is
// not meept-daemon (the `meept` CLI, an eval harness, a test binary) must not
// wrap its spawns: a supervisor whose parent is a short-lived CLI would kill a
// runtime the user asked to outlive that CLI. MEEPT_SUPERVISOR_BIN overrides
// the resolution for a renamed daemon binary; tests replace the var.
var supervisorBinary = defaultSupervisorBinary

// defaultSupervisorBinary resolves the supervisor binary from the running
// executable: the daemon binary, and nothing else.
func defaultSupervisorBinary() string {
	if override := os.Getenv(supervisorBinaryEnv); override != "" {
		return override
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(filepath.Base(exe), "meept-daemon") {
		return ""
	}
	return exe
}

// RunSupervisor is the supervisor process: it spawns opts.Argv, reports the
// runtime pid, and exits when the runtime exits or when the watched parent
// disappears (killing the runtime first). The returned int is the process exit
// code: 0 for every normal end (including a parent-death termination, so a
// daemon's wait on the supervisor is not misread as a runtime failure), and
// non-zero only for a usage or spawn failure.
func RunSupervisor(opts SupervisorOptions) int {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	if len(opts.Argv) == 0 {
		fmt.Fprintln(os.Stderr, "meept-daemon: supervisor mode needs the runtime command line after --")
		return supervisorExitUsage
	}
	if opts.ParentPID <= 0 {
		fmt.Fprintln(os.Stderr, "meept-daemon: supervisor mode needs a positive --supervise-parent pid")
		return supervisorExitUsage
	}
	poll := opts.PollInterval
	if poll <= 0 {
		poll = supervisorPollInterval
	}
	grace := opts.TermGrace
	if grace <= 0 {
		grace = supervisorTermGrace
	}

	child := exec.Command(opts.Argv[0], opts.Argv[1:]...)
	child.Stdin = nil
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	// The runtime keeps its own process group, exactly as a direct spawn would
	// give it: the sweep's process-group kill, the "no grandchild outlives the
	// runtime" guarantee, and RuntimeProcess.Stop's group semantics are all
	// unchanged by the wrapper.
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Both descriptors are the supervisor's own channels, borrowed from the
	// spawning daemon: keep them out of the runtime so it cannot hold the report
	// pipe or the parent-death pipe open. CloseOnExec ignores an unusable
	// descriptor, so a hand-started supervisor cannot harm one it was not given.
	if opts.PIDReportFD > 0 {
		syscall.CloseOnExec(opts.PIDReportFD)
	}
	if opts.ParentDeathFD > 0 {
		syscall.CloseOnExec(opts.ParentDeathFD)
	}
	if err := child.Start(); err != nil {
		reportRuntimePID(opts.PIDReportFD, 0, err.Error(), log)
		fmt.Fprintf(os.Stderr, "meept-daemon: supervisor failed to spawn runtime: %v\n", err)
		return 1
	}
	runtimePID := child.Process.Pid
	reportRuntimePID(opts.PIDReportFD, runtimePID, "", log)
	parentGone := watchParentDeath(opts.ParentDeathFD)
	log.Debug("supervisor: runtime started", "runtime_pid", runtimePID, "supervised_parent", opts.ParentPID)

	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigs)

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case werr := <-exited:
			if werr != nil {
				log.Debug("supervisor: runtime exited with error", "runtime_pid", runtimePID, "error", werr)
			}
			// The runtime exited on its own: exit too, so no wrapper process
			// is left behind.
			return 0
		case <-parentGone:
			log.Warn("supervisor: parent-death pipe closed; terminating runtime",
				"supervised_parent", opts.ParentPID, "runtime_pid", runtimePID)
			terminateRuntimeGroup(runtimePID, grace, log)
			return 0
		case sig := <-sigs:
			log.Info("supervisor: signal received; terminating runtime",
				"signal", sig.String(), "runtime_pid", runtimePID)
			terminateRuntimeGroup(runtimePID, grace, log)
			return 0
		case <-ticker.C:
			if supervisorParentAlive(opts.ParentPID) {
				continue
			}
			log.Warn("supervisor: supervised parent is gone; terminating runtime",
				"supervised_parent", opts.ParentPID, "runtime_pid", runtimePID)
			terminateRuntimeGroup(runtimePID, grace, log)
			return 0
		}
	}
}

// watchParentDeath returns a channel closed once the write end of the
// parent-death pipe is gone, i.e. once the supervised parent (and every
// process that inherited its end of the pipe) has exited. A nil channel (fd 0,
// an unusable descriptor) never fires, leaving the pid poll as the only check.
//
// This is the reliable half of parent-death detection. On macOS a pid check
// cannot tell a live parent from a dead one that has not been reaped yet:
// signal 0 succeeds against a zombie, and a zombie parent keeps Getppid
// pointing at it, so a daemon killed by a process that never waits would leave
// its runtime supervised forever. The kernel closes the pipe when the last
// holder of the write end dies, zombies included.
func watchParentDeath(fd int) <-chan struct{} {
	if fd <= 0 {
		return nil
	}
	file := os.NewFile(uintptr(fd), "parent-death")
	if file == nil {
		return nil
	}
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		buf := make([]byte, 1)
		for {
			if _, err := file.Read(buf); err != nil {
				return // EOF (parent gone) or an unusable descriptor
			}
		}
	}()
	return gone
}

// supervisorParentAlive reports whether the process the supervisor was told to
// watch is still alive. Two independent signals of death are checked:
//
//   - Getppid: the supervisor is always spawned as the watched process's direct
//     child, so a different parent means the watched process died (and the
//     supervisor was re-parented). Unlike signal 0 this cannot be fooled by a
//     reused pid, and it is the only check that catches a dead parent whose pid
//     has been reused by a live process.
//   - signal 0: catches a parent that is gone without the re-parenting having
//     been observed yet at the moment of the check.
//
// A ZOMBIE parent is the one case neither check sees (signal 0 succeeds against
// a zombie and Getppid still names it); the parent is reaped within moments by
// the shell or launchd in practice, and the boot-time sweep is the backstop.
func supervisorParentAlive(parentPID int) bool {
	if parentPID <= 0 {
		return true
	}
	if os.Getppid() != parentPID {
		return false
	}
	return processAlive(parentPID)
}

// terminateRuntimeGroup stops the runtime's process group the same way
// RuntimeProcess.Stop does: SIGTERM first, then SIGKILL once the grace period
// elapses without the runtime exiting.
func terminateRuntimeGroup(runtimePID int, grace time.Duration, log *slog.Logger) {
	if err := killProcessGroupPID(runtimePID, syscall.SIGTERM); err != nil {
		log.Debug("supervisor: SIGTERM failed (runtime already gone)",
			"runtime_pid", runtimePID, "error", err)
		return
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !processAlive(runtimePID) {
			return
		}
		time.Sleep(supervisorPollStep)
	}
	if err := killProcessGroupPID(runtimePID, syscall.SIGKILL); err != nil {
		log.Debug("supervisor: SIGKILL failed", "runtime_pid", runtimePID, "error", err)
		return
	}
	log.Warn("supervisor: runtime ignored SIGTERM; killed", "runtime_pid", runtimePID, "grace", grace.String())
}

// reportRuntimePID writes the runtime pid (or "0 <reason>" on failure) to fd.
//
// The descriptor is borrowed from the spawning daemon (exec.Cmd.ExtraFiles) and
// is written with a raw write that never closes it: this process does not own
// it, and closing a descriptor it was not given would close an unrelated one --
// including a descriptor the supervising process or the Go runtime is using --
// when the flag is absent or wrong.
func reportRuntimePID(fd, pid int, reason string, log *slog.Logger) {
	if fd <= 0 {
		return
	}
	line := strconv.Itoa(pid)
	if reason != "" || pid <= 0 {
		line = "0 " + strings.ReplaceAll(reason, "\n", " ")
	}
	if _, err := syscall.Write(fd, []byte(line+"\n")); err != nil {
		log.Debug("supervisor: pid report write failed", "fd", fd, "error", err)
	}
}

// readSupervisorReport reads the runtime pid line the supervisor writes to the
// spawn pipe. The reported pid is the RUNTIME's, which is what the PID file must
// name: the wrapper is not the runtime.
func readSupervisorReport(r *os.File) (int, error) {
	defer func() {
		if cerr := r.Close(); cerr != nil {
			slog.Debug("supervisor: report pipe close failed", "error", cerr)
		}
	}()
	lines := make(chan string, 1)
	readErrs := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(r).ReadString('\n')
		if err != nil {
			readErrs <- err
			return
		}
		lines <- line
	}()
	select {
	case line := <-lines:
		return parseSupervisorReport(line)
	case err := <-readErrs:
		return 0, fmt.Errorf("supervisor report pipe: %w", err)
	case <-time.After(supervisorReportTimeout):
		// The deferred Close unblocks the reader goroutine.
		return 0, fmt.Errorf("no pid report within %s", supervisorReportTimeout)
	}
}

// parseSupervisorReport parses "<pid>\n" or "0 <reason>\n".
func parseSupervisorReport(line string) (int, error) {
	trimmed := strings.TrimSpace(line)
	fields := strings.SplitN(trimmed, " ", 2)
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, fmt.Errorf("invalid supervisor pid report %q: %w", trimmed, err)
	}
	if pid > 0 {
		return pid, nil
	}
	reason := "unknown reason"
	if len(fields) > 1 && strings.TrimSpace(fields[1]) != "" {
		reason = strings.TrimSpace(fields[1])
	}
	return 0, fmt.Errorf("supervisor could not start the runtime: %s", reason)
}

// startSupervised spawns the supervisor with this runtime's command line and
// returns the read end of the pid-report pipe together with the write end of the
// parent-death pipe. The caller keeps the death pipe open for as long as the
// runtime should live and closes it when the supervisor is done: closing it
// tells the supervisor the parent is gone, and a hard death of this process
// closes it in the kernel.
//
// Caller must hold p.mu (Start does): p.cmd is assigned here.
func (p *RuntimeProcess) startSupervised(ctx context.Context, stdout, stderr io.Writer, bin string) (*os.File, *os.File, error) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("supervisor pid report pipe: %w", err)
	}
	deathRead, deathWrite, err := os.Pipe()
	if err != nil {
		closeFileQuietly(readEnd)
		closeFileQuietly(writeEnd)
		return nil, nil, fmt.Errorf("supervisor parent-death pipe: %w", err)
	}
	argv := SuperviseArgv(os.Getpid(), supervisorReportFD, supervisorDeathFD, p.config.SpawnCommand)
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
	cmd.ExtraFiles = []*os.File{writeEnd, deathRead}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		closeFileQuietly(readEnd)
		closeFileQuietly(writeEnd)
		closeFileQuietly(deathRead)
		closeFileQuietly(deathWrite)
		return nil, nil, fmt.Errorf("failed to spawn runtime supervisor %s: %w", bin, err)
	}
	// The child holds its own copies of both descriptors (fd 3 report write
	// end, fd 4 parent-death read end); this process keeps only the report read
	// end and the parent-death WRITE end.
	closeFileQuietly(writeEnd)
	closeFileQuietly(deathRead)
	p.cmd = cmd
	return readEnd, deathWrite, nil
}

// closeFileQuietly closes a pipe end. A close error has no recovery and the
// caller's outcome is already decided.
func closeFileQuietly(f *os.File) {
	if f == nil {
		return
	}
	if err := f.Close(); err != nil {
		slog.Debug("runtime supervisor pipe close failed", "error", err)
	}
}
