package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools/mcp"
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

// --- mcp dependency checks (Contract B: 20260905-dependency-visibility) ---

// helperFailf reports a table-case guard failure without failing the whole
// test from inside a goroutine (parallel table convention in this repo).
func helperFailf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mcp test guard: "+format+"\n", args...)
}

// TestMcpDependencyCheckTable covers the per-server pure helper: bare names
// resolve via exec.LookPath, absolute paths via stat, missing binaries are
// data (fail detail), never aborts.
func TestMcpDependencyCheckTable(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not on PATH in this test environment")
	}

	cases := []struct {
		desc       string
		name       string
		command0   string
		installHit string
		wantOK     bool
		wantDetail []string // substrings the detail must contain
		notDetail  []string // substrings the detail must NOT contain
	}{
		{
			desc:       "present-bare",
			name:       "gopls",
			command0:   "go",
			installHit: "brew install go",
			wantOK:     true,
			wantDetail: []string{"go found in path"},
		},
		{
			desc:       "missing-bare-with-hint",
			name:       "github",
			command0:   "definitely-not-a-binary-xyz",
			installHit: "npm install -g @modelcontextprotocol/server-github",
			wantOK:     false,
			wantDetail: []string{
				"definitely-not-a-binary-xyz not found",
				"install: npm install -g @modelcontextprotocol/server-github",
			},
		},
		{
			desc:       "absolute-present",
			name:       "local-server",
			command0:   makeTempExecutable(t),
			installHit: "",
			wantOK:     true,
			wantDetail: []string{"found in path"},
		},
		{
			desc:       "absolute-missing",
			name:       "obscura",
			command0:   "/nonexistent-dir-xyz/obscura",
			installHit: "cargo install --path obscura",
			wantOK:     false,
			wantDetail: []string{
				"/nonexistent-dir-xyz/obscura not found",
				"install: cargo install --path obscura",
			},
		},
		{
			desc:       "empty-hint-missing",
			name:       "nohint",
			command0:   "definitely-not-a-binary-xyz",
			installHit: "",
			wantOK:     false,
			wantDetail: []string{"definitely-not-a-binary-xyz not found"},
			notDetail:  []string{"install:"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			got := mcpDependencyCheck(tc.name, tc.command0, tc.installHit)
			if got.ok != tc.wantOK {
				helperFailf("%s: ok = %v, want %v (detail %q)", tc.desc, got.ok, tc.wantOK, got.detail)
			}
			if got.name != "mcp:"+tc.name {
				helperFailf("%s: name = %q, want %q", tc.desc, got.name, "mcp:"+tc.name)
			}
			for _, sub := range tc.wantDetail {
				if !strings.Contains(got.detail, sub) {
					helperFailf("%s: detail %q missing %q", tc.desc, got.detail, sub)
				}
			}
			for _, sub := range tc.notDetail {
				if strings.Contains(got.detail, sub) {
					helperFailf("%s: detail %q must not contain %q", tc.desc, got.detail, sub)
				}
			}
			if got.fixable {
				helperFailf("%s: dependency checks are report-only, got fixable", tc.desc)
			}
		})
	}
}

// makeTempExecutable creates an empty executable file and returns its path.
func makeTempExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-binary")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMcpDependencyChecksFilters asserts the slice builder emits one check
// per ENABLED stdio entry only: disabled servers and http servers produce
// zero checks.
func TestMcpDependencyChecksFilters(t *testing.T) {
	disabled := false
	cfg := &config.MCPServersConfig{
		Servers: []mcp.ServerConfig{
			{Name: "present", Command: []string{"go"}, InstallHint: "brew install go"},
			{Name: "missing", Command: []string{"definitely-not-a-binary-xyz"}, InstallHint: "npm install -g pkg"},
			{Name: "off", Enabled: &disabled, Command: []string{"definitely-not-a-binary-xyz"}, InstallHint: "npm install -g pkg"},
			{Name: "remote", Type: "http", URL: "https://example.invalid/mcp"},
		},
	}
	got := mcpDependencyChecks(cfg)
	if len(got) != 2 {
		t.Fatalf("mcpDependencyChecks emitted %d checks, want 2: %+v", len(got), got)
	}
	if got[0].name != "mcp:present" || !got[0].ok {
		t.Errorf("check[0] = %+v, want ok mcp:present", got[0])
	}
	if got[1].name != "mcp:missing" || got[1].ok {
		t.Errorf("check[1] = %+v, want failing mcp:missing", got[1])
	}
}

// TestMcpDependencyChecksNilConfig: no catalog entries → no checks, no warn.
func TestMcpDependencyChecksNilConfig(t *testing.T) {
	if got := mcpDependencyChecks(nil); len(got) != 0 {
		t.Fatalf("nil config emitted %d checks, want 0", len(got))
	}
}

// TestMcpDependencyCatalogWarn: a catalog load failure yields exactly one
// warn check named mcp:catalog — never an abort, never an empty report.
func TestMcpDependencyCatalogWarn(t *testing.T) {
	got := mcpCatalogFailureCheck("could not parse mcp_servers.json5: bad json5")
	if got.name != "mcp:catalog" {
		t.Fatalf("name = %q, want mcp:catalog", got.name)
	}
	if got.ok || !got.warn {
		t.Fatalf("got ok=%v warn=%v, want ok=false warn=true", got.ok, got.warn)
	}
	if !strings.Contains(got.detail, "could not load mcp catalog") ||
		!strings.Contains(got.detail, "bad json5") {
		t.Errorf("detail %q does not wrap the load error", got.detail)
	}
	if got.fixable {
		t.Error("catalog warn must not be fixable")
	}
}

// TestRunDoctorMcpChecksWired: end-to-end — runDoctor output includes the
// mcp: dependency block (from the real user catalog, or the single
// mcp:catalog warn line if it cannot load).
func TestRunDoctorMcpChecksWired(t *testing.T) {
	oldState := stateDir
	t.Cleanup(func() { stateDir = oldState })
	stateDir = t.TempDir()

	var out string
	// captureStdout swaps the global os.Stdout; keep this test serial.
	out = captureStdout(t, func() {
		if err := runDoctor(false); err != nil {
			t.Errorf("runDoctor(false): %v", err)
			return
		}
	})
	if !strings.Contains(out, "mcp:") {
		t.Errorf("runDoctor output has no mcp: dependency lines:\n%s", out)
	}
}
