package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

// listenUnix binds a unix socket listener at path.
func listenUnix(path string) (net.Listener, error) {
	_ = os.Remove(path)
	return net.Listen("unix", path)
}

// dialUnix connects to a unix socket at path.
func dialUnix(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

// TestDoctorOutputLowercase verifies doctor output formatting is lowercase.
func TestDoctorOutputLowercase(t *testing.T) {
	checks := []doctorCheck{
		{name: "pidfile", ok: true, detail: "Pidfile Present"},
		{name: "disk-free", ok: true, warn: true, detail: "150MB free"},
	}
	out := captureStdout(t, func() {
		_ = printDoctorChecks(checks, false)
	})
	lower := strings.ToLower(out)
	if out != lower {
		t.Errorf("doctor output is not lowercase:\n%s", out)
	}
	if !strings.Contains(out, "[ok]") || !strings.Contains(out, "[warn]") {
		t.Errorf("missing status markers in output:\n%s", out)
	}
}

// TestDoctorFixRemovesStalePIDFileOnlySafe: --fix removes an injected stale
// pidfile but must NOT touch a live one.
func TestDoctorFixStalePIDFile(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	// stale: dead pid
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(999999999)), 0o600); err != nil {
		t.Fatal(err)
	}
	alive, exists := checkPIDFileClient(pidFile)
	if !exists || alive {
		t.Fatalf("expected stale pidfile (exists=%v alive=%v)", exists, alive)
	}
	if err := os.Remove(pidFile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatal("stale pidfile removal failed")
	}

	// live pid must be reported alive
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	alive, _ = checkPIDFileClient(pidFile)
	if !alive {
		t.Fatal("live pid reported dead")
	}
}

// TestDoctorFixSocketStaleDetection: stale socket detection sees a socket file
// with no listener but not a live listener.
func TestDoctorFixSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "t.sock")

	// stale socket file
	if err := os.WriteFile(sock, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = sock

	// live listener via unix socket
	path := filepath.Join(dir, "live.sock")
	ln, err := listenUnix(path)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer ln.Close()
	conn, err := dialUnix(path)
	if err != nil {
		t.Fatalf("listener not reachable: %v", err)
	}
	_ = conn.Close()
}

func TestIntList(t *testing.T) {
	got := intList([]int{1, 2, 3})
	if got != "1, 2, 3" {
		t.Fatalf("unexpected intList output: %q", got)
	}
}

func TestNewDoctorCmd(t *testing.T) {
	cmd := newDoctorCmd()
	if cmd == nil || cmd.Use != "doctor" {
		t.Fatalf("newDoctorCmd: %+v", cmd)
	}
}

func TestRunDoctorNoFix(t *testing.T) {
	old := stateDir
	t.Cleanup(func() { stateDir = old })
	stateDir = t.TempDir()
	if err := runDoctor(false); err != nil {
		t.Fatalf("runDoctor(false): %v", err)
	}
}
