package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	if err := runDoctor(false, false); err != nil {
		t.Fatalf("runDoctor(false, false): %v", err)
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

	// captureStdout swaps the global os.Stdout; keep this test serial.
	out := captureStdout(t, func() {
		if err := runDoctor(false, false); err != nil {
			t.Errorf("runDoctor(false, false): %v", err)
			return
		}
	})
	if !strings.Contains(out, "mcp:") {
		t.Errorf("runDoctor output has no mcp: dependency lines:\n%s", out)
	}
}

// --- leaf 02/01: --install-missing executor (Contract E) ---

// lineFeeder hands out exactly one line per Read call. Each confirmInstall
// call builds its own bufio.Reader; a plain strings.Reader would let the
// first bufio fill its buffer with every pending answer, swallowing the
// next prompt's input. One-line-per-Read keeps multi-prompt flows honest.
type lineFeeder struct {
	lines []string
}

func (f *lineFeeder) Read(p []byte) (int, error) {
	if len(f.lines) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.lines[0])
	f.lines[0] = f.lines[0][n:]
	if len(f.lines[0]) == 0 {
		f.lines = f.lines[1:]
	}
	return n, nil
}

// newLineFeeder yields one newline-terminated line per answer. nil answers
// means immediate EOF.
func newLineFeeder(answers ...string) *lineFeeder {
	lines := make([]string, len(answers))
	for i, a := range answers {
		lines[i] = a + "\n"
	}
	return &lineFeeder{lines: lines}
}

func TestConfirmInstall(t *testing.T) {
	cases := []struct {
		desc    string
		answers []string
		want    bool
	}{
		{desc: "y", answers: []string{"y"}, want: true},
		{desc: "capital-Y", answers: []string{"Y"}, want: true},
		{desc: "yes", answers: []string{"yes"}, want: true},
		{desc: "capital-YES", answers: []string{"YES"}, want: true},
		{desc: "empty-line", answers: []string{""}, want: false},
		{desc: "n", answers: []string{"n"}, want: false},
		{desc: "no", answers: []string{"no"}, want: false},
		{desc: "garbage", answers: []string{"maybe"}, want: false},
		{desc: "eof-no-data-must-not-hang", answers: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			var w bytes.Buffer
			got := confirmInstall(newLineFeeder(tc.answers...), &w, "brew install thing")
			if got != tc.want {
				t.Fatalf("confirmInstall(answers=%q) = %v, want %v", tc.answers, got, tc.want)
			}
			if !strings.HasPrefix(w.String(), "run this command? [y/N] ") {
				t.Errorf("prompt prefix not written to w before read: %q", w.String())
			}
		})
	}
}

func TestRunInstallHint(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if err := runInstallHint(context.Background(), "true", &out, &errOut); err != nil {
			t.Fatalf("runInstallHint(true) = %v, want nil", err)
		}
	})
	t.Run("exit-status", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := runInstallHint(context.Background(), "exit 3", &out, &errOut)
		if err == nil || !strings.Contains(err.Error(), "exit status 3") {
			t.Fatalf("runInstallHint(exit 3) = %v, want error containing %q", err, "exit status 3")
		}
	})
	t.Run("streams-wired", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if err := runInstallHint(context.Background(), "echo hi; echo boo >&2", &out, &errOut); err != nil {
			t.Fatalf("runInstallHint(echo) = %v, want nil", err)
		}
		if out.String() != "hi\n" {
			t.Errorf("stdout = %q, want %q", out.String(), "hi\n")
		}
		if errOut.String() != "boo\n" {
			t.Errorf("stderr = %q, want %q", errOut.String(), "boo\n")
		}
	})
	t.Run("cancelled-context", func(t *testing.T) {
		var out, errOut bytes.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := runInstallHint(ctx, "sleep 5", &out, &errOut)
		if err == nil || !strings.Contains(err.Error(), "context") {
			t.Fatalf("runInstallHint(cancelled ctx) = %v, want error mentioning context", err)
		}
	})
}

// withInstallRunnerStub swaps the runInstallHintFn package-var seam for a
// recording stub. failAt returns the error to produce for the nth call
// (1-based); nil means every call succeeds.
func withInstallRunnerStub(t *testing.T, calls *[]string, failAt func(n int) error) {
	t.Helper()
	old := runInstallHintFn
	runInstallHintFn = func(ctx context.Context, hint string, stdout, stderr io.Writer) error {
		*calls = append(*calls, hint)
		if failAt != nil {
			return failAt(len(*calls))
		}
		return nil
	}
	t.Cleanup(func() { runInstallHintFn = old })
}

// installFixture returns two missing-with-hint entries in catalog order.
func installFixture() []mcpDepEntry {
	return []mcpDepEntry{
		{name: "alpha", command: "definitely-not-a-binary-xyz", installHint: "brew install alpha"},
		{name: "beta", command: "definitely-not-a-binary-xyz", installHint: "npm install -g beta"},
	}
}

func TestInstallMissingFlowNoneMissing(t *testing.T) {
	var out, errOut bytes.Buffer
	var calls []string
	withInstallRunnerStub(t, &calls, nil)

	err := runInstallMissingFlow(context.Background(), nil, &out, &errOut, newLineFeeder("y"), true, runInstallHintFn)

	if err != nil {
		t.Fatalf("flow(none missing) = %v, want nil", err)
	}
	if len(calls) != 0 {
		t.Errorf("runner called %d time(s), want 0: %v", len(calls), calls)
	}
	if !strings.Contains(out.String(), "no missing mcp dependencies.") {
		t.Errorf("output missing none-missing line:\n%s", out.String())
	}
}

func TestInstallMissingFlowConsentYes(t *testing.T) {
	var out, errOut bytes.Buffer
	var calls []string
	withInstallRunnerStub(t, &calls, nil)

	err := runInstallMissingFlow(context.Background(), installFixture()[:1], &out, &errOut, newLineFeeder("y"), true, runInstallHintFn)

	if err != nil {
		t.Fatalf("flow(consent y) = %v, want nil", err)
	}
	if len(calls) != 1 || calls[0] != "brew install alpha" {
		t.Fatalf("runner calls = %v, want exactly [brew install alpha] (hint verbatim)", calls)
	}
	if !strings.Contains(out.String(), "install for alpha: brew install alpha") {
		t.Errorf("output missing install-for line:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "installed alpha: ok") {
		t.Errorf("output missing ok line:\n%s", out.String())
	}
}

func TestInstallMissingFlowConsentNo(t *testing.T) {
	var out, errOut bytes.Buffer
	var calls []string
	withInstallRunnerStub(t, &calls, nil)

	err := runInstallMissingFlow(context.Background(), installFixture()[:1], &out, &errOut, newLineFeeder("n"), true, runInstallHintFn)

	if err != nil {
		t.Fatalf("flow(consent n) = %v, want nil", err)
	}
	if len(calls) != 0 {
		t.Errorf("runner called %d time(s) after refusal, want 0: %v", len(calls), calls)
	}
	if !strings.Contains(out.String(), "install for alpha: brew install alpha") {
		t.Errorf("output missing install-for line:\n%s", out.String())
	}
	if strings.Contains(out.String(), "installed") {
		t.Errorf("refused install must not print a result line:\n%s", out.String())
	}
}

func TestInstallMissingFlowContinuesAfterError(t *testing.T) {
	var out, errOut bytes.Buffer
	var calls []string
	withInstallRunnerStub(t, &calls, func(n int) error {
		if n == 1 {
			return fmt.Errorf("exit status 3")
		}
		return nil
	})

	err := runInstallMissingFlow(context.Background(), installFixture(), &out, &errOut, newLineFeeder("y", "y"), true, runInstallHintFn)

	if err != nil {
		t.Fatalf("flow(runner error) = %v, want nil", err)
	}
	if len(calls) != 2 {
		t.Fatalf("runner calls = %v (%d), want 2 — loop must continue after failure", calls, len(calls))
	}
	if calls[0] != "brew install alpha" || calls[1] != "npm install -g beta" {
		t.Errorf("runner calls = %v, want [alpha hint, beta hint] in order", calls)
	}
	if !strings.Contains(out.String(), "installed alpha: failed (exit status 3)") {
		t.Errorf("output missing failed line:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "installed beta: ok") {
		t.Errorf("output missing second server's ok line:\n%s", out.String())
	}
}

func TestInstallMissingFlowTTYRefusal(t *testing.T) {
	var out, errOut bytes.Buffer
	var calls []string
	withInstallRunnerStub(t, &calls, nil)

	err := runInstallMissingFlow(context.Background(), installFixture()[:1], &out, &errOut, newLineFeeder("y"), false, runInstallHintFn)

	if err != nil {
		t.Fatalf("flow(tty=false) = %v, want nil", err)
	}
	if len(calls) != 0 {
		t.Errorf("runner called %d time(s) after refusal, want 0: %v", len(calls), calls)
	}
	if !strings.Contains(out.String(), "--install-missing requires an interactive terminal") {
		t.Errorf("output missing refusal line:\n%s", out.String())
	}
	if strings.Contains(out.String(), "install for") {
		t.Errorf("refusal must come before any prompt:\n%s", out.String())
	}
}

func TestValidateDoctorFlags(t *testing.T) {
	cases := []struct {
		desc           string
		fix            bool
		installMissing bool
		wantErr        string
	}{
		{desc: "install-missing-without-fix", fix: false, installMissing: true, wantErr: "--install-missing requires --fix"},
		{desc: "both", fix: true, installMissing: true},
		{desc: "neither", fix: false, installMissing: false},
		{desc: "fix-only", fix: true, installMissing: false},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			err := validateDoctorFlags(tc.fix, tc.installMissing)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateDoctorFlags(%v, %v) = %v, want nil", tc.fix, tc.installMissing, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateDoctorFlags(%v, %v) = %v, want error containing %q", tc.fix, tc.installMissing, err, tc.wantErr)
			}
		})
	}
}

// TestDoctorCobraInstallMissingRequiresFix exercises the full cobra path:
// --install-missing without --fix reaches RunE and fails the explicit
// flag validation there (MarkFlagsRequiredTogether is deliberately NOT
// used — it would also reject `doctor --fix` alone).
func TestDoctorCobraInstallMissingRequiresFix(t *testing.T) {
	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--install-missing"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("cobra Execute(--install-missing without --fix) = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--install-missing requires --fix") {
		t.Errorf("cobra error %q does not carry the validation message", err)
	}
}

func TestMissingInstallCandidates(t *testing.T) {
	entries := []mcpDepEntry{
		{name: "have", command: "sh"},
		{name: "miss", command: "definitely-not-a-binary-xyz", installHint: "brew install miss"},
		{name: "nohint", command: "definitely-not-a-binary-xyz"},
	}
	checks := []doctorCheck{
		{name: "mcp:have", ok: true},
		{name: "mcp:miss", ok: false},
		{name: "mcp:nohint", ok: false},
	}
	got := missingInstallCandidates(checks, entries)
	if len(got) != 1 {
		t.Fatalf("missingInstallCandidates = %+v, want exactly the miss entry", got)
	}
	if got[0].name != "miss" || got[0].installHint != "brew install miss" {
		t.Errorf("candidate = %+v, want name/hint preserved from the catalog entry", got[0])
	}
}

// TestMcpDependencyEntriesMirror: the (name, command, hint) triple slice
// keeps the same filtering and ordering mcpDependencyChecks has always had.
func TestMcpDependencyEntriesMirror(t *testing.T) {
	disabled := false
	cfg := &config.MCPServersConfig{
		Servers: []mcp.ServerConfig{
			{Name: "present", Command: []string{"sh"}, InstallHint: "brew install sh"},
			{Name: "missing", Command: []string{"definitely-not-a-binary-xyz"}, InstallHint: "npm install -g pkg"},
			{Name: "off", Enabled: &disabled, Command: []string{"definitely-not-a-binary-xyz"}, InstallHint: "npm install -g pkg"},
			{Name: "remote", Type: "http", URL: "https://example.invalid/mcp"},
		},
	}
	got := mcpDependencyEntries(cfg)
	if len(got) != 2 {
		t.Fatalf("mcpDependencyEntries = %+v, want 2 entries", got)
	}
	if got[0].name != "present" || got[0].command != "sh" || got[0].installHint != "brew install sh" {
		t.Errorf("entry[0] = %+v, want present/sh/brew hint", got[0])
	}
	if got[1].name != "missing" || got[1].installHint != "npm install -g pkg" {
		t.Errorf("entry[1] = %+v, want missing entry with npm hint", got[1])
	}
	if entries := mcpDependencyEntries(nil); len(entries) != 0 {
		t.Errorf("nil config gave %d entries, want 0", len(entries))
	}
}
