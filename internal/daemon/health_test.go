package daemon

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCheckSocketListening(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	// No listener -> fail
	res := checkSocketListening(sock)
	if res.Status != "fail" {
		t.Fatalf("expected fail for missing listener, got %q (%s)", res.Status, res.Detail)
	}

	// Real listener -> pass
	ln := listenTemp(t, sock)
	defer ln.Close()
	res = checkSocketListening(sock)
	if res.Status != "pass" {
		t.Fatalf("expected pass for live listener, got %q (%s)", res.Status, res.Detail)
	}
}

func TestCheckPIDFileFresh(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "test.pid")

	// Missing -> fail
	if res := checkPIDFileFresh(pidPath); res.Status != "fail" {
		t.Fatalf("expected fail for missing pidfile, got %q", res.Status)
	}

	// Garbage -> fail
	if err := os.WriteFile(pidPath, []byte("not-a-pid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := checkPIDFileFresh(pidPath); res.Status != "fail" {
		t.Fatalf("expected fail for garbage pidfile, got %q", res.Status)
	}

	// Dead pid -> fail
	if err := os.WriteFile(pidPath, []byte("999999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := checkPIDFileFresh(pidPath); res.Status != "fail" {
		t.Fatalf("expected fail for dead pid, got %q", res.Status)
	}

	// Live pid (this test process) -> pass
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := checkPIDFileFresh(pidPath); res.Status != "pass" {
		t.Fatalf("expected pass for live pid, got %q (%s)", res.Status, res.Detail)
	}
}

func TestCheckSQLiteIntegrity(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.db")
	if _, err := os.Create(good); err != nil {
		t.Fatal(err)
	}
	res := checkSQLiteIntegrity(good)
	if res.Status != "pass" {
		t.Fatalf("expected pass for valid empty db, got %q (%s)", res.Status, res.Detail)
	}

	// Corrupt db -> fail
	bad := filepath.Join(dir, "bad.db")
	if err := os.WriteFile(bad, []byte("this is definitely not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	res = checkSQLiteIntegrity(bad)
	if res.Status == "pass" {
		t.Fatalf("expected non-pass for corrupt db, got %q", res.Status)
	}

	// Missing file -> fail
	res = checkSQLiteIntegrity(filepath.Join(dir, "nope.db"))
	if res.Status != "fail" {
		t.Fatalf("expected fail for missing db, got %q", res.Status)
	}
}

func TestCheckDataDirWritable(t *testing.T) {
	dir := t.TempDir()
	if res := checkDataDirWritable(dir); res.Status != "pass" {
		t.Fatalf("expected pass for writable dir, got %q", res.Status)
	}
	// Nonexistent dir -> fail
	if res := checkDataDirWritable(filepath.Join(dir, "missing")); res.Status != "fail" {
		t.Fatalf("expected fail for missing dir, got %q", res.Status)
	}
}

func TestCheckDiskFreeTriState(t *testing.T) {
	dir := t.TempDir()
	res := checkDiskFreeThreshold(dir, diskFreeWarnBytes)
	// Tri-state: result must be one of the three statuses
	switch res.Status {
	case "pass", "warn", "fail":
	default:
		t.Fatalf("invalid status %q", res.Status)
	}
}

func TestCheckRuntimeProcesses(t *testing.T) {
	// No runtime procs -> warn (report-only, not fatal)
	res := checkRuntimeProcesses(func() []string { return nil })
	if res.Status != "warn" {
		t.Fatalf("expected warn with no runtime processes, got %q", res.Status)
	}
	res = checkRuntimeProcesses(func() []string { return []string{"llama-server (pid 42)"} })
	if res.Status != "pass" {
		t.Fatalf("expected pass with runtime processes, got %q", res.Status)
	}
}

func TestRunHealthChecksOverall(t *testing.T) {
	dir := t.TempDir()
	opts := HealthOptions{
		SocketPath: filepath.Join(dir, "none.sock"),
		PIDFile:    filepath.Join(dir, "none.pid"),
		StateDir:   dir,
		DBPaths:    nil,
		StartTime:  time.Now(),
		Now:        func() time.Time { return time.Now() },
		RuntimeProcs: func() []string {
			return nil
		},
	}
	checks := RunHealthChecks(opts)
	if len(checks) == 0 {
		t.Fatal("expected checks to be returned")
	}
	names := map[string]bool{}
	for _, c := range checks {
		names[c.Name] = true
	}
	for _, want := range []string{"socket-listening", "pidfile-fresh", "data-dir-writable", "config-parse", "disk-free", "runtime-processes"} {
		if !names[want] {
			t.Errorf("missing check %q", want)
		}
	}
}

// listenTemp is a test helper that binds a unix socket at path.
func listenTemp(t *testing.T, path string) net.Listener {
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		t.Logf("stale socket removal failed: %v", rmErr)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen %s: %v", path, err)
	}
	return ln
}
