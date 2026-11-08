package voice

// Test helpers for ChildProc (leaf 13): the fake child binaries (built once
// per test RUN by compiling tiny generated mains), the fake-daemon re-exec
// role used by the parent-death test, and the processAlive shim.
//
// Build determinism (flake fix): every ChildProc test used to run its own
// `go build` of the helper binaries inside the test process — buildFakeVoiceChild
// per test env, and a FULL cmd/meept-daemon build (buildSupervisorCapableBinary,
// ~7s) on every TestChildProcParentDeath iteration. Under -p 2 -race on a
// loaded host, one transient failure of those in-test builds (toolchain
// contention with concurrent sessions, a just-written binary briefly refused
// by macOS AMFI at exec time) surfaced as a test FAIL ~1 run in 20 — observed
// as TestChildProcParentDeath failing at 1.44s, i.e. inside the BUILD phase,
// far inside every supervision deadline (15s pid / 5s ready / 10s alive / 20s
// gone). Production logic was never involved in that failure class.
//
// Fix: each run-constant binary is built exactly once per test binary into a
// shared run-scoped dir, with bounded retries and a warm exec check proving
// the fresh binary actually runs. A real build failure still fails every
// dependent test fast, with the captured stderr. TestMain removes the dir.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// processAlive reports whether pid is a live process (signal 0). Distinct
// name from llm.processAlive to keep the packages decoupled.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// signalNotifyReal is wired by the TestMain file's init (os/signal lives
// there); the helper only calls through it.
var signalNotifyReal func(c chan<- os.Signal, sig ...os.Signal)

// hostOS is used by isDarwinOrLinux in childproc_test.go.
var hostOS = runtime.GOOS

// waitCmd reaps cmd's process, ignoring the exit state. Process.Wait is
// multi-return, so the discard cannot be a bare statement; the error is
// unactionable best-effort teardown in tests.
func waitCmd(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if _, err := cmd.Process.Wait(); err != nil {
		t.Logf("wait %s: %v", cmd.Path, err)
	}
}

// childProcBinDirMu guards the run-scoped shared binary dir.
var childProcBinDirMu sync.Mutex
var childProcBinDir string

// childProcSharedBinDir returns (creating once) the run-scoped directory the
// cached helper binaries are built into. cleanupSharedChildProcBins removes
// it when the test binary exits.
func childProcSharedBinDir() (string, error) {
	childProcBinDirMu.Lock()
	defer childProcBinDirMu.Unlock()
	if childProcBinDir == "" {
		dir, err := os.MkdirTemp("", "vsbin")
		if err != nil {
			return "", fmt.Errorf("shared test bin dir: %w", err)
		}
		childProcBinDir = dir
	}
	return childProcBinDir, nil
}

// cleanupSharedChildProcBins removes the run-scoped bin dir. Called from
// TestMain after m.Run(); safe when the dir was never created.
func cleanupSharedChildProcBins() {
	childProcBinDirMu.Lock()
	defer childProcBinDirMu.Unlock()
	if childProcBinDir != "" {
		_ = os.RemoveAll(childProcBinDir)
		childProcBinDir = ""
	}
}

// warmExec proves a freshly built binary execs. macOS can transiently kill a
// just-written binary at dyld/AMFI time under load; discovering that inside
// the supervised spawn chain costs the whole poll deadline, so it is checked
// at build time instead. args must make main exit on its own OR the binary
// must be safe to kill after warmGrace (the never-exit variants). A process
// that runs — exits with any code, or is still alive at the grace tick and
// gets reaped after our own Kill — proves exec + dyld are fine. A failure to
// even start it, or a signalled death that is NOT our own Kill, is transient:
// the caller retries the build+warm pair.
func warmExec(path string, warmGrace time.Duration, args ...string) error {
	cmd := exec.Command(path, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("warm exec start %s: %w", filepath.Base(path), err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	select {
	case err := <-waitCh:
		if err == nil {
			return nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// Ran and exited (any code): exec path is proven.
			return nil
		}
		return fmt.Errorf("warm exec wait %s: %w", filepath.Base(path), err)
	case <-time.After(warmGrace):
		// Still running: kill it and accept — exec + run are proven.
		_ = cmd.Process.Kill()
		select {
		case <-waitCh:
			return nil
		case <-time.After(2 * time.Second):
			return fmt.Errorf("warm exec %s: process unkillable", filepath.Base(path))
		}
	}
}

// runGoBuild runs one `go build -o out target` (target is either a
// generated .go file path or a module package path) with captured stderr,
// returned on failure for the retry report.
func runGoBuild(out, target string) error {
	cmd := exec.Command("go", "build", "-o", out, target)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// buildAndWarm compiles (srcPath = generated .go file, or pkgPath = module
// package) into <sharedDir>/<name> and warm-execs the result, retrying
// transient failures of either step together with backoff. Exactly one of
// src/pkg must be non-empty.
func buildAndWarm(name, src, pkg string, warmGrace time.Duration, warmArgs ...string) (string, error) {
	dir, err := childProcSharedBinDir()
	if err != nil {
		return "", err
	}
	target := src
	if pkg != "" {
		target = pkg
	}
	if src != "" {
		srcPath := filepath.Join(dir, name+".go")
		if wErr := os.WriteFile(srcPath, []byte(src), 0o600); wErr != nil {
			return "", fmt.Errorf("write helper source: %w", wErr)
		}
		target = srcPath
	}
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt-1) * 500 * time.Millisecond)
		}
		out := filepath.Join(dir, name)
		if bErr := runGoBuild(out, target); bErr != nil {
			last = fmt.Errorf("attempt %d build: %w", attempt, bErr)
			continue
		}
		if wErr := warmExec(out, warmGrace, warmArgs...); wErr != nil {
			_ = os.Remove(out)
			last = fmt.Errorf("attempt %d warm: %w", attempt, wErr)
			continue
		}
		return out, nil
	}
	return "", last
}

// fakeVoiceChildSrc is the fake meept-voice the tests spawn: it reads the
// start token line from stdin (the daemon pipes it in), records the spawn
// in <parent-of-audio>/spawns, dials the text socket, and holds until
// signalled or the connection dies.
const fakeVoiceChildSrc = `package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	textSocket := ""
	audioDir := ""
	args := os.Args[1:]
	for i := 0; i+1 < len(args); i += 2 {
		switch args[i] {
		case "--text-socket":
			textSocket = args[i+1]
		case "--audio-dir":
			audioDir = args[i+1]
		}
	}
	if textSocket == "" || audioDir == "" {
		fmt.Fprintln(os.Stderr, "fake child: --text-socket and --audio-dir required")
		os.Exit(2)
	}
	token, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	token = strings.TrimSpace(token)
	if token == "" {
		fmt.Fprintln(os.Stderr, "fake child: empty start token on stdin")
		os.Exit(3)
	}
	marker := filepath.Join(filepath.Dir(audioDir), "spawns")
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		if _, fwErr := fmt.Fprintf(f, "spawn\n"); fwErr != nil {
			fmt.Fprintln(os.Stderr, "fake child: spawn marker:", fwErr)
		}
		if fcErr := f.Close(); fcErr != nil {
			fmt.Fprintln(os.Stderr, "fake child: spawn marker close:", fcErr)
		}
	}
	conn, err := net.Dial("unix", textSocket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake child: dial: %v\n", err)
		os.Exit(4)
	}
	// Prove the start token as the FIRST line before any frame, exactly
	// like the real child (internal/voicechild/child.go): a daemon with
	// the token gate armed claims this conn; a legacy (disarmed) daemon
	// drops the line as an undecodable frame.
	if _, wErr := fmt.Fprintf(conn, "%s\n", token); wErr != nil {
		fmt.Fprintln(os.Stderr, "fake child: token write:", wErr)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := conn.Read(buf); err != nil {
				sigs <- syscall.SIGTERM
				return
			}
		}
	}()
	<-sigs
	if cErr := conn.Close(); cErr != nil {
		fmt.Fprintln(os.Stderr, "fake child: close conn:", cErr)
	}
}
`

// silentVoiceChildSrc is the never-connects variant used by the
// handshake-timeout test: sleeps until killed.
const silentVoiceChildSrc = `package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	for {
		select {
		case <-sigs:
			return
		case <-time.After(1 * time.Second):
		}
	}
}
`

// badTokenVoiceChildSrc dials and proves the WRONG start token — the
// impersonator the armed gate must refuse. It still reads (and discards)
// its stdin token line so the spawn's stdin contract holds.
const badTokenVoiceChildSrc = `package main

import (
	"bufio"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	args := os.Args[1:]
	textSocket := ""
	audioDir := ""
	for i := 0; i+1 < len(args); i += 2 {
		switch args[i] {
		case "--text-socket":
			textSocket = args[i+1]
		case "--audio-dir":
			audioDir = args[i+1]
		}
	}
	if textSocket == "" || audioDir == "" {
		os.Exit(2)
	}
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	conn, err := net.Dial("unix", textSocket)
	if err != nil {
		os.Exit(4)
	}
	if _, wErr := conn.Write([]byte("totally-wrong-token\n")); wErr != nil {
		os.Exit(5)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sigs:
	case <-time.After(60 * time.Second):
	}
	conn.Close()
}
`

// buildFakeVoiceChild returns the fake child binary, built once per test run
// (the dir argument is kept for call-site compatibility; the binary is
// run-constant and must NOT be rebuilt per test / per -count iteration —
// that rebuild loop was the flake surface).
func buildFakeVoiceChild(t *testing.T, dir string) string {
	t.Helper()
	_ = dir
	bin, err := fakeVoiceChildBin()
	if err != nil {
		t.Fatalf("build fake voice child: %v", err)
	}
	return bin
}

func buildSilentVoiceChild(t *testing.T, dir string) string {
	t.Helper()
	_ = dir
	bin, err := silentVoiceChildBin()
	if err != nil {
		t.Fatalf("build silent voice child: %v", err)
	}
	return bin
}

// buildBadTokenVoiceChild returns the wrong-token impersonator binary.
func buildBadTokenVoiceChild(t *testing.T, dir string) string {
	t.Helper()
	_ = dir
	bin, err := badTokenVoiceChildBin()
	if err != nil {
		t.Fatalf("build bad token voice child: %v", err)
	}
	return bin
}

// The run-scoped build caches. Each sync.OnceValues runs the build+warm
// sequence exactly once per test binary; a real (non-transient) build failure
// is cached and fails every dependent test — correct: it is a broken
// toolchain, not a flake.
var fakeVoiceChildBin = sync.OnceValues(func() (string, error) {
	// Warm exec with no args: main exits 2 immediately (missing flags).
	return buildAndWarm("fake-meept-voice", fakeVoiceChildSrc, "", 10*time.Second)
})

var silentVoiceChildBin = sync.OnceValues(func() (string, error) {
	// The silent child never exits on its own: warm exec starts it and
	// kills it at the grace tick.
	return buildAndWarm("silent-meept-voice", silentVoiceChildSrc, "", 1*time.Second)
})

var badTokenVoiceChildBin = sync.OnceValues(func() (string, error) {
	// Warm exec with no args: main exits 2 immediately (missing flags).
	return buildAndWarm("badtoken-meept-voice", badTokenVoiceChildSrc, "", 10*time.Second)
})

// buildSupervisorCapableBinary builds a meept-daemon-named binary that
// carries the supervisor entry point (llm's supervisor resolution accepts
// only executables whose basename starts with meept-daemon). It compiles
// the module's own cmd/meept-daemon, which already implements supervisor
// mode - no generated source, no internal-package violation. Built once per
// test run: this is the most expensive build in the suite (the whole daemon,
// ~7s) and per-count-iteration rebuilds multiplied both the wall time and
// the transient build-failure surface that flaked TestChildProcParentDeath.
func buildSupervisorCapableBinary(t *testing.T, dir string) string {
	t.Helper()
	_ = dir
	bin, err := supervisorCapableBin()
	if err != nil {
		t.Fatalf("go build meept-daemon supervisor helper: %v", err)
	}
	return bin
}

var supervisorCapableBin = sync.OnceValues(func() (string, error) {
	// Warm exec in supervisor mode with a malformed invocation:
	// runSupervisorMode reports the parse error and exits 2 BEFORE
	// spawning anything, proving the just-built binary execs with no
	// side effects.
	return buildAndWarm("meept-daemon-voice-sup", "", "github.com/caimlas/meept/cmd/meept-daemon",
		30*time.Second, "--supervise-parent", "0", "--", "/usr/bin/true")
})

// runVoiceFakeDaemon is the parent-death test's re-exec role: it stands in
// for the daemon. args: <sup-bin> <pid-file> <ready-file> <runtime argv...>.
// It spawns the supervisor the way llm.RuntimeProcess.startSupervised does
// (report pipe fd 3, death pipe fd 4), writes the reported child pid to
// pidFile, touches ready-file once the death pipe is held, reaps the
// supervisor like Start's wait goroutine does, and blocks holding the death
// pipe write end until SIGKILLed - which is what a hard-killed daemon looks
// like to the supervisor.
//
// runVoiceFakeDaemon runs in a re-exec'd test binary with no *testing.T, so
// best-effort errors report to stderr instead of failing a test.
func runVoiceFakeDaemon(args []string) int {
	if len(args) < 4 {
		fmt.Fprintln(os.Stderr, "fake daemon: usage: --voice-fake-daemon <sup-bin> <pid-file> <ready-file> <argv...>")
		return 2
	}
	supBin := args[0]
	pidFile := args[1]
	readyFile := args[2]
	runtimeArgv := args[3:]

	reportRead, reportWrite, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: report pipe:", err)
		return 1
	}
	deathRead, deathWrite, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: death pipe:", err)
		return 1
	}
	cmd := exec.Command(supBin, llmSuperviseArgvShim(os.Getpid(), 3, 4, runtimeArgv)...)
	cmd.ExtraFiles = []*os.File{reportWrite, deathRead}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// The supervisor forwards its own stdin to the runtime (RunSupervisor
	// defaults opts.Stdin to os.Stdin and assigns child.Stdin = opts.Stdin),
	// and the voice child reads its one-time start token line from stdin
	// before it connects. exec.Cmd nils Stdin to /dev/null, so without this
	// the child exits(3) at spawn - empty start token - and the
	// parent-death teardown has nothing left to terminate. Feed the token
	// line through a pipe: it fits the pipe buffer, so write-then-close
	// never blocks and the child sees EOF after the line.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: stdin pipe:", err)
		return 1
	}
	if _, wErr := stdinW.WriteString("fake-daemon-start-token\n"); wErr != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: stdin token write:", wErr)
	}
	if cErr := stdinW.Close(); cErr != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: stdin token close:", cErr)
	}
	cmd.Stdin = stdinR
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: start supervisor:", err)
		return 1
	}
	if cwErr := reportWrite.Close(); cwErr != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: close report write end:", cwErr)
	}
	if cdErr := deathRead.Close(); cdErr != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: close death read end:", cdErr)
	}
	// Hold the death pipe for this process's whole life (the parent-death
	// mechanism under test), via the package-level reference.
	voiceFakeDaemonDeathPipe = deathWrite
	if rfErr := os.WriteFile(readyFile, []byte("ready"), 0o600); rfErr != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: ready file:", rfErr)
	}

	// Read the supervisor's pid report line ("<pid>\n" or "0 <reason>\n").
	lineCh := make(chan string, 1)
	go func() {
		line, _ := bufioReadLine(reportRead)
		lineCh <- line
	}()
	var line string
	select {
	case line = <-lineCh:
	case <-time.After(15 * time.Second):
		fmt.Fprintln(os.Stderr, "fake daemon: no supervisor pid report within 15s")
		return 1
	}
	if rcErr := reportRead.Close(); rcErr != nil {
		fmt.Fprintln(os.Stderr, "fake daemon: close report read end:", rcErr)
	}
	pid := 0
	if line != "" {
		var aErr error
		pid, aErr = strconv.Atoi(strings.SplitN(line, " ", 2)[0])
		if aErr != nil {
			fmt.Fprintln(os.Stderr, "fake daemon: bad pid report:", aErr)
		}
	}
	if pid > 0 {
		if wErr := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600); wErr != nil { //nolint:gosec // test temp path
			fmt.Fprintln(os.Stderr, "fake daemon: pid file:", wErr)
		}
	} else {
		fmt.Fprintf(os.Stderr, "fake daemon: supervisor reported failure: %q\n", line)
		return 1
	}

	// Reap the supervisor when it exits (mirrors Start's wait goroutine).
	supDone := make(chan struct{})
	go func() {
		if _, wErr := cmd.Process.Wait(); wErr != nil {
			fmt.Fprintln(os.Stderr, "fake daemon: reap supervisor:", wErr)
		}
		close(supDone)
	}()

	// Stay alive until killed.
	sigs := make(chan os.Signal, 1)
	signalNotifyReal(sigs, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sigs:
	case <-supDone:
	}
	return 0
}

// bufioReadLine reads one line (without the newline) from f.
func bufioReadLine(f *os.File) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := f.Read(buf)
		if n == 0 || err != nil {
			return sb.String(), err
		}
		if buf[0] == '\n' {
			return sb.String(), nil
		}
		sb.WriteByte(buf[0])
	}
}

// llmSuperviseArgvShim mirrors llm.SuperviseArgv's flag shape (the llm
// package does not export SuperviseArgv; the shape is pinned by its own
// tests and the daemon consumes it only internally).
func llmSuperviseArgvShim(parentPID, reportFD, deathFD int, spawn []string) []string {
	argv := make([]string, 0, 8+len(spawn))
	argv = append(argv,
		"--supervise-parent", strconv.Itoa(parentPID),
		"--pid-report-fd", strconv.Itoa(reportFD),
		"--parent-death-fd", strconv.Itoa(deathFD),
		"--")
	return append(argv, spawn...)
}

// fakeChildSpawnCount reads the helper's spawn-count marker file.
func fakeChildSpawnCount(t *testing.T, dir string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "spawns"))
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
