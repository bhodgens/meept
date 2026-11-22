//go:build e2e

// Package seatbelt proves the [runtime.seatbelt] macOS confinement policy
// end to end. A scratch daemon with the policy armed routes its shell tool
// through the local backend under /usr/bin/sandbox-exec, and filesystem
// markers read back from the (unsandboxed) test process show exactly which
// operations the profile denied:
//
//   - flag-off: the local backend runs with NO confinement and NO armed
//     log line — the legacy posture a seatbelt-less config keeps (runs on
//     every OS)
//   - deny_read / allow_write+deny_write / process rules: darwin-only,
//     each pinned by an on-disk marker that must or must not exist
//   - fail-closed: an invalid policy refuses every command (REFUSING
//     backend) instead of running unconfined — asserted on every OS
//
// darwin-only scenarios skip cleanly on Linux CI.
package seatbelt

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

func skipUnlessDarwin(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skipf("seatbelt confinement is macOS-only; %s cannot enforce it", runtime.GOOS)
	}
}

// seedShellAgent writes a minimal AGENT.md granting the shell tool on the
// code intent, so the pinned classifier routes the turn to an agent that
// definitely has shell (same pattern as the agent-groups suite).
func seedShellAgent(t *testing.T, st *harness.Stack, id string) {
	t.Helper()
	dir := filepath.Join(st.MeeptHome, "agents", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s agent dir: %v", id, err)
	}
	body := "---\n" +
		"id: " + id + "\n" +
		"name: E2E seatbelt shell\n" +
		"role: executor\n" +
		"description: e2e seatbelt gate agent\n" +
		"intents: [code]\n" +
		"enabled: true\ncan_delegate: false\n" +
		"additional_tools:\n  - shell_execute\n" +
		"capabilities:\n  - reasoning\nmax_iterations: 8\ntimeout_seconds: 120\n" +
		"---\n\nRun exactly the shell command the user gives you.\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s AGENT.md: %v", id, err)
	}
}

// realDir resolves symlinked harness paths (macOS /var is a symlink to
// /private/var) so the seatbelt subpath rules and the sandboxed commands
// speak the same absolute path.
func realDir(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks %s: %v", p, err)
	}
	return r
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// runShell drives one shell tool call through a full chat turn.
func runShell(t *testing.T, s *harness.Stack, sessionID, command string) {
	t.Helper()
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned code"}`)
	s.Fake.SetPostToolText("shell call issued")
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "shell",
		Arguments: fmt.Sprintf(`{"command":%q}`, command),
	})
	s.ChatTurn(t, sessionID, "Run this exact shell command: "+command, 150*time.Second)
}

// daemonLog returns the daemon's own meept.log contents (state-dir log,
// the one that carries post-logger INFO lines; same file the logrotate
// suite asserts on).
func daemonLog(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.log"))
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	return string(data)
}

// TestSeatbeltFlagOffLocalBackend pins the legacy posture: [runtime]
// enabled with an explicit local order and NO seatbelt block runs the
// local backend unconfined (write anywhere works) and never logs an armed
// seatbelt. Runs on every OS.
func TestSeatbeltFlagOffLocalBackend(t *testing.T) {
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedShellAgent(t, st, "seatbeltshell")
			return nil
		}),
		harness.WithConfigOverlay(map[string]any{
			"runtime.enabled":               true,
			"runtime.sandbox_backend_order": "local",
		}),
	)
	s.RegisterProject(t, "e2e-seatbelt")
	root := realDir(t, s.ProjectDir)
	out := filepath.Join(root, "unconfined.txt")
	sessionID := s.CreateSession(t, "seatbelt-off", s.ProjectDir)
	runShell(t, s, sessionID, "echo SEATBELT-UNCONFINED > "+out)

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("flag-off posture: unconfined write missing under local backend: %v", err)
	}
	if !strings.Contains(string(data), "SEATBELT-UNCONFINED") {
		t.Fatalf("marker content = %q, want SEATBELT-UNCONFINED", data)
	}
	if log := daemonLog(t, s); strings.Contains(log, "seatbelt confinement armed") {
		t.Fatalf("flag-off posture armed seatbelt anyway:\n%s", log)
	}
}

// TestSeatbeltDenyRead arms deny_read over one subtree: the confined shell
// reads nothing there (empty stdout + EPERM stderr), while a sibling
// subtree stays readable.
func TestSeatbeltDenyRead(t *testing.T) {
	skipUnlessDarwin(t)
	var lockedDir string
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedShellAgent(t, st, "seatbeltshell")
			lockedDir = realDir(t, st.ProjectDir) + "/locked"
			return os.MkdirAll(lockedDir, 0o755)
		}),
		harness.WithConfigHook(func(cfg map[string]any) {
			applyOverlay(cfg, "runtime.enabled", true)
			applyOverlay(cfg, "runtime.sandbox_backend_order", "local")
			applyOverlay(cfg, "runtime.seatbelt.enabled", true)
			applyOverlay(cfg, "runtime.seatbelt.deny_read", []string{lockedDir})
		}),
	)
	s.RegisterProject(t, "e2e-seatbelt")
	root := realDir(t, s.ProjectDir)
	secret := filepath.Join(root, "locked", "secret.txt")
	mustWrite(t, secret, "SEATBELT-SECRET\n")
	openDir := filepath.Join(root, "open")
	mustWrite(t, filepath.Join(openDir, "plain.txt"), "plain\n")

	sessionID := s.CreateSession(t, "seatbelt-dr", s.ProjectDir)
	runShell(t, s, sessionID,
		"cat "+secret+" > "+openDir+"/out.txt 2> "+openDir+"/err.txt; true")

	if data := readOrEmpty(t, openDir+"/out.txt"); strings.Contains(string(data), "SEATBELT-SECRET") {
		t.Fatalf("deny_read leaked: %q", data)
	}
	if data := readOrEmpty(t, openDir+"/err.txt"); !strings.Contains(string(data), "not permitted") {
		t.Fatalf("deny_read stderr missing EPERM text: %q", data)
	}
	// Sibling subtree stays readable (deny is scoped to the rule).
	runShell(t, s, sessionID, "cat "+filepath.Join(openDir, "plain.txt")+" > "+openDir+"/sibling.txt 2>&1; true")
	if data := readOrEmpty(t, openDir+"/sibling.txt"); !strings.Contains(string(data), "plain") {
		t.Fatalf("deny_read overreached: sibling subtree unreadable: %q", data)
	}
	if log := daemonLog(t, s); !strings.Contains(log, "seatbelt confinement armed") {
		t.Fatal("armed seatbelt never logged")
	}
}

// TestSeatbeltWriteConfined proves the write allow-list is a real
// boundary: a marker under the allowed root lands, one outside it does
// not, and one inside a deny_write hole inside the allowed root does not.
func TestSeatbeltWriteConfined(t *testing.T) {
	skipUnlessDarwin(t)
	var allowDir, holeDir string
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedShellAgent(t, st, "seatbeltshell")
			root := realDir(t, st.ProjectDir)
			allowDir = root + "/writable"
			holeDir = allowDir + "/holes"
			return os.MkdirAll(holeDir, 0o755)
		}),
		harness.WithConfigHook(func(cfg map[string]any) {
			applyOverlay(cfg, "runtime.sandbox_backend_order", "local")
			applyOverlay(cfg, "runtime.seatbelt.enabled", true)
			applyOverlay(cfg, "runtime.seatbelt.allow_write", []string{allowDir})
			applyOverlay(cfg, "runtime.seatbelt.deny_write", []string{holeDir})
		}),
	)
	s.RegisterProject(t, "e2e-seatbelt")
	root := realDir(t, s.ProjectDir)
	sessionID := s.CreateSession(t, "seatbelt-wc", s.ProjectDir)
	runShell(t, s, sessionID, fmt.Sprintf(
		"touch %s/f_inside 2>%s/err_out; touch %s/f_outside 2>>%s/err_out; touch %s/holes/f_hole 2>>%s/err_out; true",
		allowDir, allowDir, root, allowDir, allowDir, allowDir))

	if _, err := os.Stat(filepath.Join(allowDir, "f_inside")); err != nil {
		t.Fatalf("allowed write refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "f_outside")); err == nil {
		t.Fatal("write outside the allow-list succeeded (boundary leak)")
	}
	if _, err := os.Stat(filepath.Join(holeDir, "f_hole")); err == nil {
		t.Fatal("write into deny_write hole succeeded (last-match ordering broken)")
	}
	if data := readOrEmpty(t, filepath.Join(allowDir, "err_out")); !strings.Contains(string(data), "not permitted") {
		t.Fatalf("expected EPERM evidence for the refused writes: %q", data)
	}
}

// TestSeatbeltProcessRules arms block_process_info and deny_exec over
// /bin/kill: ps must fail with EPERM and /bin/kill must be refused, while
// an ordinary pipeline keeps working. /bin/kill targets pid 99999 (no such
// process exists on this harness; even if it did, the deny proves itself
// via stderr), so nothing can be orphaned by this scenario.
func TestSeatbeltProcessRules(t *testing.T) {
	skipUnlessDarwin(t)
	var workDir string
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedShellAgent(t, st, "seatbeltshell")
			workDir = filepath.Join(realDir(t, st.ProjectDir), "proc")
			return os.MkdirAll(workDir, 0o755)
		}),
		harness.WithConfigHook(func(cfg map[string]any) {
			applyOverlay(cfg, "runtime.sandbox_backend_order", "local")
			applyOverlay(cfg, "runtime.seatbelt.enabled", true)
			applyOverlay(cfg, "runtime.seatbelt.block_process_info", true)
			applyOverlay(cfg, "runtime.seatbelt.deny_exec", []string{"/bin/kill"})
		}),
	)
	s.RegisterProject(t, "e2e-seatbelt")
	sessionID := s.CreateSession(t, "seatbelt-proc", s.ProjectDir)
	runShell(t, s, sessionID, fmt.Sprintf(
		"/bin/kill 99999 2> %s/kill_err.txt; ps > %s/ps_out.txt 2> %s/ps_err.txt; echo ok | /usr/bin/wc -l > %s/pipeline.txt; true",
		workDir, workDir, workDir, workDir))

	if data := readOrEmpty(t, filepath.Join(workDir, "ps_out.txt")); len(data) > 0 {
		t.Fatalf("block_process_info leaked ps output: %q", data[:min(len(data), 80)])
	}
	if data := readOrEmpty(t, filepath.Join(workDir, "ps_err.txt")); !strings.Contains(string(data), "not permitted") {
		t.Fatalf("ps was not denied with EPERM: %q", data)
	}
	if data := readOrEmpty(t, filepath.Join(workDir, "kill_err.txt")); !strings.Contains(string(data), "not permitted") {
		t.Fatalf("deny_exec(/bin/kill) did not refuse kill: %q", data)
	}
	if data := readOrEmpty(t, filepath.Join(workDir, "pipeline.txt")); !strings.Contains(string(data), "1") {
		t.Fatalf("confinement broke the ordinary pipeline: %q", data)
	}
}

// TestSeatbeltFailClosedBadPath arms seatbelt with a relative deny_read
// path. The policy is refused at wiring time and the shell tool lands on
// the REFUSING backend: no marker file may exist after the turn. Asserted
// on every OS (Linux refuses because seatbelt cannot be enforced there at
// all; darwin refuses on the invalid path).
func TestSeatbeltFailClosedBadPath(t *testing.T) {
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedShellAgent(t, st, "seatbeltshell")
			return nil
		}),
		harness.WithConfigOverlay(map[string]any{
			"runtime.enabled":               true,
			"runtime.sandbox_backend_order": "local",
			"runtime.seatbelt.enabled":      true,
			"runtime.seatbelt.deny_read":    []string{"not-an-absolute-path"},
		}),
	)
	s.RegisterProject(t, "e2e-seatbelt")
	root := realDir(t, s.ProjectDir)
	marker := filepath.Join(root, "should_not_exist.txt")
	sessionID := s.CreateSession(t, "seatbelt-fc", s.ProjectDir)
	runShell(t, s, sessionID, "echo LEAK > "+marker)

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("fail-closed posture executed a command despite an invalid seatbelt policy")
	}
	log := daemonLog(t, s)
	if !strings.Contains(log, "REFUSING backend") {
		t.Fatalf("no REFUSING backend log despite refused wiring:\n%s", tail(log, 2000))
	}
}

// helpers ---------------------------------------------------------------

func readOrEmpty(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return data
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// applyOverlay writes one dotted key into the parsed config tree, the same
// merge the harness's WithConfigOverlay performs (kept local so the hook
// can read stack-derived paths that only exist at hook time).
func applyOverlay(cfg map[string]any, dotted string, value any) {
	parts := strings.Split(dotted, ".")
	cur := cfg
	for i, k := range parts {
		if i == len(parts)-1 {
			cur[k] = value
			return
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
}
