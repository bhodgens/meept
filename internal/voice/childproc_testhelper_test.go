package voice

// Test helpers for ChildProc (leaf 13): the fake child binaries (built once
// per test run by compiling tiny generated mains), the fake-daemon re-exec
// role used by the parent-death test, and the processAlive shim.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

// goBuild compiles src into a standalone binary in dir.
func goBuild(t *testing.T, dir, binName, src string) string {
	t.Helper()
	out := filepath.Join(dir, binName)
	srcPath := filepath.Join(dir, binName+".go")
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", out, srcPath)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build %s: %v", binName, err)
	}
	t.Cleanup(func() {
		if rmErr := os.Remove(out); rmErr != nil {
			t.Logf("remove %s: %v", out, rmErr)
		}
	})
	return out
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

func buildFakeVoiceChild(t *testing.T, dir string) string {
	t.Helper()
	return goBuild(t, dir, "fake-meept-voice", fakeVoiceChildSrc)
}

func buildSilentVoiceChild(t *testing.T, dir string) string {
	t.Helper()
	return goBuild(t, dir, "silent-meept-voice", silentVoiceChildSrc)
}

// buildSupervisorCapableBinary builds a meept-daemon-named binary that
// carries the supervisor entry point (llm's supervisor resolution accepts
// only executables whose basename starts with meept-daemon). It compiles
// the module's own cmd/meept-daemon, which already implements supervisor
// mode - no generated source, no internal-package violation.
func buildSupervisorCapableBinary(t *testing.T, dir string) string {
	t.Helper()
	out := filepath.Join(dir, "meept-daemon-voice-sup")
	cmd := exec.Command("go", "build", "-o", out, "github.com/caimlas/meept/cmd/meept-daemon")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build meept-daemon supervisor helper: %v", err)
	}
	t.Cleanup(func() {
		if rmErr := os.Remove(out); rmErr != nil {
			t.Logf("remove %s: %v", out, rmErr)
		}
	})
	return out
}

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
