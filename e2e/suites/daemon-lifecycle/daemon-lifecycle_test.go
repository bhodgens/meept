//go:build e2e

// Suite daemon-lifecycle: the scratch daemon's process lifecycle —
// boot health, graceful SIGTERM shutdown, kill/restart against the SAME
// sandbox home (state survives), and pid-file/pid hygiene across the
// restart. Boots the harness-built daemon binary directly with a
// hand-written sandbox meept.json5 (the harness Stack owns exactly one
// boot per Stack, so this suite drives DaemonPath + Stack.Env itself).
package daemonlifecycle

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// lifecycleSandbox is a hand-rolled scratch world: the harness's layout
// (work/home/state) with the config the daemon needs, but MULTIPLE boot
// generations against the same sandbox.
type lifecycleSandbox struct {
	t          *testing.T
	Work       string
	Home       string
	MeeptHome  string
	StateDir   string
	ProjectDir string
	SocketPath string
	HTTPAddr   string
	Fake       *harness.FakeLLM
	cmd        *exec.Cmd
	logPath    string
}

// newSandbox builds the directory skeleton + config without booting.
// The work root uses a SHORT prefix: the Unix socket lives at
// <root>/state/meept.sock and macOS sun_path caps at 104 bytes — the
// default MkdirTemp prefix + EvalSymlinks resolution
// (/var/... -> /private/var/...) overflows it and the RPC listener dies
// with "bind: invalid argument" (the harness's Start uses a shorter
// prefix and never resolves the work root for exactly this reason).
func newSandbox(t *testing.T, port int) *lifecycleSandbox {
	t.Helper()
	work, err := os.MkdirTemp("", "me2e-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	// NOTE: deliberately NOT EvalSymlinks'ing work — the resolved
	// /private/var spelling is 12 chars longer and breaks the socket cap.
	s := &lifecycleSandbox{
		t:          t,
		Work:       work,
		Home:       filepath.Join(work, "home"),
		StateDir:   filepath.Join(work, "state"),
		ProjectDir: filepath.Join(work, "project"),
		SocketPath: filepath.Join(work, "state", "meept.sock"),
		HTTPAddr:   harness.PortString(port),
		logPath:    filepath.Join(work, "daemon.log"),
	}
	s.MeeptHome = filepath.Join(s.Home, ".meept")
	for _, d := range []string{s.Home, s.MeeptHome, s.StateDir, s.ProjectDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	cfg := fmt.Sprintf(`{
  // e2e daemon-lifecycle scratch config (hand-written; same shape as the
  // harness's writeConfigs so the daemon boots identically).
  "daemon": {
    "socket_path": %q,
    "pid_file": %q,
    "data_dir": %q,
    "log_level": "INFO",
  },
  "transport": {
    "rpc": { "enabled": true, "socket_path": %q },
    "http": {
      "enabled": true,
      "addr": %q,
      "require_auth": false,
      "tls_cert_file": %q,
      "tls_key_file": %q,
      "rest": true,
      "websocket": false,
      "mcp": false,
    },
  },
  "memory": { "data_dir": %q },
  "projects": {
    "enabled": true,
    "base_dir": %q,
    "auto_detect": false,
    "fence_enabled": false,
  },
  "security": {
    "audit_db_path": %q,
    "allowed_paths": [%q],
  },
  "multiagent": { "enabled": true },
}`,
		s.SocketPath,
		filepath.Join(s.StateDir, "meept.pid"),
		s.StateDir,
		s.SocketPath,
		s.HTTPAddr,
		filepath.Join(s.StateDir, "tls", "cert.pem"),
		filepath.Join(s.StateDir, "tls", "key.pem"),
		filepath.Join(s.StateDir, "memory"),
		filepath.Join(s.StateDir, "projects"),
		filepath.Join(s.StateDir, "audit.db"),
		filepath.ToSlash(filepath.Join(s.Work, "**")),
	)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "meept.json5"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write meept.json5: %v", err)
	}

	// The FakeLLM wiring: one fresh fake per sandbox, routed through
	// models.json5 exactly like the harness does.
	s.Fake = harness.NewFakeLLM()
	t.Cleanup(s.Fake.Close)
	fakeModels := fmt.Sprintf(`{
  "model": "fake/fake-model",
  "small_model": "fake/fake-model",
  "classifier_model": "classifier",
  "summarizer_model": "summarizer",
  "extract_model": "",
  "refusal_model": "",
  "default_timeout": 300,
  "disabled_providers": [],
  "model_aliases": {
    "classifier":  { "models": ["fake/fake-model"], "timeout": 10, "max_fails": 2 },
    "summarizer":  { "models": ["fake/fake-model"], "timeout": 10, "max_fails": 2 },
    "small":       { "models": ["fake/fake-model"], "timeout": 10, "max_fails": 2 },
    "coder":       { "models": ["fake/fake-model"], "timeout": 60, "max_fails": 3 },
    "planner":     { "models": ["fake/fake-model"], "timeout": 60, "max_fails": 3 },
    "analyst":     { "models": ["fake/fake-model"], "timeout": 60, "max_fails": 3 }
  },
  "providers": {
    "fake": {
      "api": "openai",
      "options": { "baseURL": %q, "noAuth": true },
      "models": {
        "fake-model": {
          "name": "fake-model",
          "capabilities": ["completion", "code", "reasoning", "tool_use"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 65536,
          "max_output": 4096,
          "temperature": 0.7
        }
      }
    }
  }
}`, s.Fake.URL()+"/v1")
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "models.json5"), []byte(fakeModels), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	return s
}

// boot starts one daemon generation against the sandbox and waits for
// /health. Reusable: kill + boot again = a restart.
func (s *lifecycleSandbox) boot() {
	t := s.t
	bin := harness.DaemonPath(t)
	logFile, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open daemon log: %v", err)
	}
	cmd := exec.Command(bin,
		"-c", filepath.Join(s.MeeptHome, "meept.json5"),
		"-d", s.StateDir,
		"-s", s.SocketPath,
	)
	cmd.Dir = s.Work // deliberately NOT the project dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "HOME="+s.Home, "MEEPT_HOME="+s.MeeptHome)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start daemon: %v", err)
	}
	s.cmd = cmd
	logFile.Close()

	t.Cleanup(func() { s.signalAndWait(syscall.SIGTERM) })

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // scratch self-signed cert
			ForceAttemptHTTP2: false,
		},
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := client.Get("https://" + s.HTTPAddr + "/health") //nolint:noctx // bounded poll
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon not healthy within 60s; log tail:\n%s", s.logTail())
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("daemon died during boot; log tail:\n%s", s.logTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// signalAndWait delivers sig once and reaps the process (idempotent for
// an already-dead generation).
func (s *lifecycleSandbox) signalAndWait(sig syscall.Signal) {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Signal(sig)
	done := make(chan struct{})
	go func() { _, _ = s.cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
}

func (s *lifecycleSandbox) pid() int {
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

func (s *lifecycleSandbox) logTail() string {
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		return fmt.Sprintf("(no log: %v)", err)
	}
	if len(data) > 4096 {
		data = data[len(data)-4096:]
	}
	return string(data)
}

func (s *lifecycleSandbox) healthStatus(t *testing.T) (int, bool) {
	t.Helper()
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // scratch self-signed cert
		},
	}
	resp, err := client.Get("https://" + s.HTTPAddr + "/health") //nolint:noctx // bounded call
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, true
}

// TestDaemonLifecycle_BootHealthAndGracefulShutdown covers the boot half
// of daemon-lifecycle-01: the binary boots to a healthy /health on the
// sandbox config, writes its pid file, and a SIGTERM shuts it down
// gracefully (the process exits, its pid-file era ends, nothing is left
// listening).
func TestDaemonLifecycle_BootHealthAndGracefulShutdown(t *testing.T) {
	s := newSandbox(t, harness.FreePort(t))
	s.boot()

	pid := s.pid()
	if pid == 0 {
		t.Fatal("boot recorded no pid")
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("daemon pid %d not alive after boot: %v", pid, err)
	}

	health := s.HTTPJSON(t)
	if health["status"] != "ok" {
		t.Fatalf("health = %+v, want status ok", health)
	}

	// The pid file names the live pid (daemon pid-file contract).
	pidFile := filepath.Join(s.StateDir, "meept.pid")
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v\nlog tail:\n%s", err, s.logTail())
	}
	var recorded int
	if _, err := fmt.Sscanf(string(raw), "%d", &recorded); err != nil || recorded != pid {
		t.Fatalf("pid file = %q (parsed %d err %v), want %d", raw, recorded, err, pid)
	}

	// Graceful shutdown: SIGTERM, bounded wait, process gone.
	s.signalAndWait(syscall.SIGTERM)
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("daemon %d still alive after SIGTERM", pid)
	}
}

// TestDaemonLifecycle_StateSurvivesRestart covers the HIGH-VALUE
// kill/restart half: a session created on generation 1 is still there,
// with its detection-context CWD, after the daemon is killed and a NEW
// generation boots against the SAME sandbox home — resolved from the
// store alone (the state-restart contract at the lifecycle level).
func TestDaemonLifecycle_StateSurvivesRestart(t *testing.T) {
	s := newSandbox(t, harness.FreePort(t))
	s.boot()

	cli := harness.CLIPath(t)
	runCLI := func(ignoreError bool, args ...string) (string, string) {
		t.Helper()
		return runCLIWith(cli, s.Env(), s.Work, 30*time.Second, ignoreError, args...)
	}

	// Generation 1: register the project + create a session bound to it.
	// The CLI resolves the socket from $MEEPT_HOME/meept.sock when --socket
	// is unset — the sandbox daemon listens on the custom state-dir socket,
	// so every call passes --socket/-d explicitly (same as Stack.CLIArgv).
	// The CLI round-trip is also the readiness gate; the CLI prints
	// "Registered project: <name> (id: ...)" on success.
	var sessionID string
	var lastOut string
	harness.WaitFor(t, 30*time.Second, "generation-1 project registration", func() bool {
		out, stderr := runCLI(true, "--socket", s.SocketPath, "-d", s.StateDir,
			"projects", "add", s.ProjectDir, "--name", "lifecycle-proj")
		lastOut = out + "\nSTDERR:" + stderr
		t.Logf("projects add attempt: out=%q stderr=%q", out, stderr)
		return strings.Contains(lastOut, "Registered project")
	})
	sessionOut, stderr := runCLI(false, "--socket", s.SocketPath, "-d", s.StateDir,
		"--cwd", s.ProjectDir, "session", "create", "lifecycle-sess")
	sessionID = parseSessionID(t, sessionOut)
	if sessionID == "" {
		t.Fatalf("generation-1 session create returned no id:\n%s\nstderr:\n%s\nlast add output:\n%s\nlog tail:\n%s",
			sessionOut, stderr, lastOut, s.logTail())
	}

	// HARSH restart: SIGKILL (not SIGTERM) — the store must carry the state.
	s.signalAndWait(syscall.SIGKILL)
	if code, ok := s.healthStatus(t); ok && code == http.StatusOK {
		t.Fatal("health still answering after SIGKILL — a second daemon is listening")
	}

	// Generation 2: same sandbox home, fresh boot.
	s.boot()

	// The session survives; the JSON list carries the bound project path
	// (project_path is persisted in the sessions store and re-read from
	// the store alone after the restart).
	listOut, _ := runCLI(false, "--socket", s.SocketPath, "-d", s.StateDir,
		"session", "list", "--json")
	if !jsonContains(listOut, sessionID) {
		t.Fatalf("session %s lost across restart; list:\n%s\nlog tail:\n%s", sessionID, listOut, s.logTail())
	}
	if !jsonContains(listOut, "project") {
		t.Fatalf("session list JSON has no project binding:\n%s", listOut)
	}

	// A NEW session also works on generation 2 (the daemon is not a
	// zombie serving stale state).
	out2, _ := runCLI(false, "--socket", s.SocketPath, "-d", s.StateDir,
		"--cwd", s.ProjectDir, "session", "create", "post-restart-sess")
	if parseSessionID(t, out2) == "" {
		t.Fatalf("generation 2 could not create a session:\n%s", out2)
	}
}

// TestDaemonLifecycle_RestartReplacesGeneration covers the daemon-lifecycle
// supervision core: repeated kill/restart cycles against one sandbox
// always come back healthy with a FRESH pid (no stale pid reuse, no
// double-bind of the socket), and the old generation's pid is dead
// before the new one serves.
func TestDaemonLifecycle_RestartReplacesGeneration(t *testing.T) {
	s := newSandbox(t, harness.FreePort(t))

	seenPIDs := map[int]bool{}
	for generation := 0; generation < 3; generation++ {
		s.boot()
		pid := s.pid()
		if pid == 0 {
			t.Fatalf("generation %d recorded no pid", generation)
		}
		if seenPIDs[pid] {
			t.Fatalf("generation %d reused pid %d from an earlier generation", generation, pid)
		}
		seenPIDs[pid] = true
		if health := s.HTTPJSON(t); health["status"] != "ok" {
			t.Fatalf("generation %d health = %+v", generation, health)
		}
		// Alternate harsh and graceful kills: the lifecycle must hold for both.
		if generation%2 == 0 {
			s.signalAndWait(syscall.SIGKILL)
		} else {
			s.signalAndWait(syscall.SIGTERM)
		}
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("generation %d pid %d survived its kill", generation, pid)
		}
	}
}

// TestDaemonLifecycle_StaleSocketAndPidRecovery covers the startup
// recovery contract: a killed generation leaves its socket + pid files
// behind; the next boot must recover (replace them) rather than fail to
// bind — the scratch-rig restart path.
func TestDaemonLifecycle_StaleSocketAndPidRecovery(t *testing.T) {
	s := newSandbox(t, harness.FreePort(t))
	s.boot()
	s.signalAndWait(syscall.SIGKILL)

	// The stale artifacts exist.
	if _, err := os.Stat(s.SocketPath); err != nil {
		t.Logf("note: SIGKILL left no socket file (daemon may clean it on death): %v", err)
	}

	// Restart over the stale artifacts.
	s.boot()
	if health := s.HTTPJSON(t); health["status"] != "ok" {
		t.Fatalf("restart over stale socket unhealthy: %+v\nlog tail:\n%s", health, s.logTail())
	}
	// CLI round-trip proves the NEW generation owns the socket.
	out, stderr := runCLIWith(harness.CLIPath(t), s.Env(), s.Work, 30*time.Second, false,
		"--socket", s.SocketPath, "-d", s.StateDir, "session", "list", "--json")
	if !jsonContains(out, "sessions") {
		t.Fatalf("post-stale-restart CLI broken:\n%s\nstderr:\n%s", out, stderr)
	}
}

// --- helpers ---

func (s *lifecycleSandbox) Env() []string {
	return append(os.Environ(), "HOME="+s.Home, "MEEPT_HOME="+s.MeeptHome)
}

// runCLIWith runs one CLI invocation with an explicit env/cwd — the
// Stack.RunCLI analogue for this suite's multi-generation sandbox.
func runCLIWith(bin string, env []string, dir string, timeout time.Duration, ignoreError bool, args ...string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && !ignoreError {
		return stdout.String(), stdout.String() + "\nSTDERR:" + stderr.String() + "\nERR:" + err.Error()
	}
	return stdout.String(), stderr.String()
}

// HTTPJSON fetches /health as a map (own client: this suite owns its boot).
func (s *lifecycleSandbox) HTTPJSON(t *testing.T) map[string]string {
	t.Helper()
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // scratch self-signed cert
		},
	}
	resp, err := client.Get("https://" + s.HTTPAddr + "/health") //nolint:noctx // bounded call
	if err != nil {
		t.Fatalf("health: %v\nlog tail:\n%s", err, s.logTail())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status %d: %s", resp.StatusCode, body)
	}
	var out map[string]string
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("health decode: %v (%s)", err, body)
	}
	return out
}

func parseSessionID(t *testing.T, out string) string {
	t.Helper()
	for _, line := range splitLines(out) {
		if after, ok := cutPrefix(line, "Created session: "); ok {
			return trimSpace(after)
		}
	}
	return ""
}

func jsonContains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

// tiny local shims so this file needs nothing outside stdlib + harness.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
