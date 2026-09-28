//go:build e2e

// Package continuityregressions is the hermetic e2e regression suite for
// the three continuity bug classes fixed on main in September 2026:
//
//  1. plan-write serialization (commit 9f503122): ApprovePlan runs the
//     Synthesize UpdatePlanStatus rewrite of plan.md CONCURRENTLY with the
//     evolver approval bridge's markPlanApplied. Both writers do
//     read-modify-write spans on the same file; the fixes made every write
//     atomic (tmp+rename, unique per-call scratch names) and serialized the
//     spans per path (plan.LockMarkdownWrite). The regression drives the
//     REAL daemon path — seeded usage stats → skills.evolve → pass C
//     archive proposal → plan → auto-approve → bridge + Synthesize — in a
//     loop, asserting after every iteration that the plan file ends with
//     BOTH the evolver origin stamp AND the applied marker (neither writer
//     clobbers the other) and that the actuator ran (the marker IS the
//     not-dropped proof).
//
//  2. recall-continuity branch selection (commits d045a450, bb6830a7,
//     14dd4145, ee0274be, 9c6f46f4): a follow-up QUESTION about prior work
//     ("did the change get made?") must be answered from the stored task
//     result via Dispatcher.RecallAnswer, and must never create a new task
//     (shouldCreateTask gate, run 44). Imperative messages referencing
//     prior work must NOT take the recall shortcut and must execute.
//
//  3. empty-completion alias failover (commit 317ba37b): a 200-OK
//     completion whose content is blank/whitespace-only is a provider
//     FLAKE — retried immediately within the short budget (same provider,
//     no plan sleep), and the bare ErrEmptyResponse sentinel on exhaustion
//     must rotate to the fallback provider instead of completing the turn
//     with garbage.
//
// Everything runs against a scratch daemon with a scripted fake LLM in a
// throwaway MEEPT_HOME (docs/workflows/e2e-testing.md fast tier).
package continuityregressions

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// sandbox is a self-contained scratch world. Unlike harness.Start (one
// provider, one boot), this suite needs custom models.json5 shapes (the
// two-provider failover topology) and evolver config, so it writes its own
// configs and boots the daemon itself — the daemon-lifecycle suite pattern.
type sandbox struct {
	t *testing.T

	Work       string
	Home       string
	MeeptHome  string
	StateDir   string
	ProjectDir string
	SocketPath string
	HTTPAddr   string

	Fake  *harness.FakeLLM // primary provider
	Fake2 *harness.FakeLLM // fallback provider (nil except the failover test)

	extraEnv []string

	cmd     *exec.Cmd
	logPath string
}

// sandboxOption customizes the config/model templates before boot.
type sandboxOption func(*sandboxConfig)

type sandboxConfig struct {
	meeptOverlay string   // extra raw JSON5 config text merged into the template
	aliases      string   // model_aliases JSON block
	providers    string   // providers JSON block
	defaultModel string   // top-level "model" (and small/classifier defaults)
	fake2        bool     // wire a second fake LLM as provider "fallback"
	extraEnv     []string // additional daemon/CLI env entries
}

func withMeeptOverlay(block string) sandboxOption {
	return func(sc *sandboxConfig) { sc.meeptOverlay += "\n" + block }
}

func withModels(aliases, providers, defaultModel string) sandboxOption {
	return func(sc *sandboxConfig) { sc.aliases, sc.providers, sc.defaultModel = aliases, providers, defaultModel }
}

func withSecondFake() sandboxOption {
	return func(sc *sandboxConfig) { sc.fake2 = true }
}

func withExtraEnv(kv ...string) sandboxOption {
	return func(sc *sandboxConfig) { sc.extraEnv = append(sc.extraEnv, kv...) }
}

// newSandbox builds the world and boots the daemon.
func newSandbox(t *testing.T, opts ...sandboxOption) *sandbox {
	t.Helper()
	sc := &sandboxConfig{
		aliases: `{
    "classifier":  { "models": ["fake/fake-model"], "timeout": 10, "max_fails": 2 },
    "summarizer":  { "models": ["fake/fake-model"], "timeout": 10, "max_fails": 2 },
    "small":       { "models": ["fake/fake-model"], "timeout": 10, "max_fails": 2 },
    "coder":       { "models": ["fake/fake-model"], "timeout": 60, "max_fails": 3 },
    "planner":     { "models": ["fake/fake-model"], "timeout": 60, "max_fails": 3 },
    "analyst":     { "models": ["fake/fake-model"], "timeout": 60, "max_fails": 3 }
  }`,
		providers: `{
    "fake": {
      "api": "openai",
      "options": { "baseURL": "__FAKE1_V1__", "noAuth": true },
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
  }`,
		defaultModel: "fake/fake-model",
	}
	for _, opt := range opts {
		opt(sc)
	}

	// Short temp prefix: the Unix socket must stay under the macOS 104-byte
	// sun_path cap (harness + daemon-lifecycle lesson; do NOT EvalSymlinks).
	work, err := os.MkdirTemp("", "me2e-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	if os.Getenv("MEEPT_E2E_KEEP") == "" {
		t.Cleanup(func() { _ = os.RemoveAll(work) })
	}
	s := &sandbox{
		t:          t,
		Work:       work,
		Home:       filepath.Join(work, "home"),
		StateDir:   filepath.Join(work, "state"),
		ProjectDir: filepath.Join(work, "project"),
		SocketPath: filepath.Join(work, "state", "meept.sock"),
		HTTPAddr:   harness.PortString(harness.FreePort(t)),
		logPath:    filepath.Join(work, "daemon.log"),
		extraEnv:   sc.extraEnv,
	}
	s.MeeptHome = filepath.Join(s.Home, ".meept")
	for _, d := range []string{s.Home, s.MeeptHome, s.StateDir, s.ProjectDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	s.Fake = harness.NewFakeLLM()
	t.Cleanup(s.Fake.Close)
	fake1V1 := s.Fake.URL() + "/v1"
	fake2V1 := ""
	if sc.fake2 {
		s.Fake2 = harness.NewFakeLLM()
		t.Cleanup(s.Fake2.Close)
		fake2V1 = s.Fake2.URL() + "/v1"
	}
	providers := strings.ReplaceAll(sc.providers, "__FAKE1_V1__", fake1V1)
	providers = strings.ReplaceAll(providers, "__FAKE2_V1__", fake2V1)

	// The fence matches against RESOLVED paths (/private/var on macOS), so
	// allowed_paths must carry BOTH spellings of the work root (the
	// harness's Start EvalSymlinks's work for the same reason; this suite
	// keeps the unresolved root for the socket-path cap and allows both).
	resolvedWork := work
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		resolvedWork = resolved
	}

	cfg := fmt.Sprintf(`{
  // continuity-regressions scratch config (hand-written; same shape as the
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
    "allowed_paths": [%q, %q],
  },
  "multiagent": { "enabled": true },%s
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
		filepath.ToSlash(filepath.Join(resolvedWork, "**")),
		strings.ReplaceAll(sc.meeptOverlay, "__MEEPT_HOME__", s.MeeptHome),
	)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "meept.json5"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write meept.json5: %v", err)
	}

	models := fmt.Sprintf(`{
  "model": %q,
  "small_model": %q,
  "classifier_model": "classifier",
  "summarizer_model": "summarizer",
  "extract_model": "",
  "refusal_model": "",
  "default_timeout": 300,
  "disabled_providers": [],
  "model_aliases": %s,
  "providers": %s
}`, sc.defaultModel, sc.defaultModel, sc.aliases, providers)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "models.json5"), []byte(models), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}

	s.seedRoster()
	s.boot()
	return s
}

// seedRoster copies the repo's agent roster + prompt templates into the
// sandboxed MEEPT_HOME (same seeding the harness does so coder specialists
// register and step jobs run with tools). The repo root resolves from this
// file's own location (e2e/suites/continuity-regressions/), the same
// runtime.Caller identity probe the harness uses.
func (s *sandbox) seedRoster() {
	t := s.t
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("seedRoster: cannot resolve this file's path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	pairs := []struct{ src, dst string }{
		{filepath.Join(repoRoot, "config", "agents"), filepath.Join(s.MeeptHome, "agents")},
		{filepath.Join(repoRoot, "config", "prompts"), filepath.Join(s.MeeptHome, "prompts")},
	}
	for _, pair := range pairs {
		if _, err := os.Stat(pair.src); err != nil {
			continue
		}
		if err := copyTree(pair.src, pair.dst); err != nil {
			t.Fatalf("seed %s: %v", pair.dst, err)
		}
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// boot starts the daemon and waits for /health.
func (s *sandbox) boot() {
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
	cmd.Env = append(cmd.Env, s.extraEnv...)
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

func (s *sandbox) signalAndWait(sig syscall.Signal) {
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

func (s *sandbox) logTail() string {
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		return fmt.Sprintf("(no log: %v)", err)
	}
	if len(data) > 4096 {
		data = data[len(data)-4096:]
	}
	return string(data)
}

func (s *sandbox) env() []string {
	env := append(os.Environ(),
		"HOME="+s.Home,
		"MEEPT_HOME="+s.MeeptHome,
	)
	return append(env, s.extraEnv...)
}

// runCLI runs one meept CLI invocation against the sandbox. The socket and
// state dir are always pinned explicitly — without --socket the CLI dials a
// default under MEEPT_HOME that no sandbox daemon listens on.
func (s *sandbox) runCLI(timeout time.Duration, ignoreError bool, args ...string) (string, string) {
	t := s.t
	prefixed := append([]string{
		"--socket", s.SocketPath,
		"-d", s.StateDir,
	}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, harness.CLIPath(t), prefixed...)
	cmd.Dir = s.Work
	cmd.Env = s.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && !ignoreError {
		t.Fatalf("meept %s failed: %v\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

// createSession creates a named session bound to cwd and returns its id.
func (s *sandbox) createSession(name, cwd string) string {
	out, _ := s.runCLI(30*time.Second, false, "--cwd", cwd, "session", "create", name)
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Created session: "); ok {
			return strings.TrimSpace(after)
		}
	}
	s.t.Fatalf("session create returned no id; output:\n%s", out)
	return ""
}

// chatTurn submits one turn and awaits the terminal reply via the CLI.
func (s *sandbox) chatTurn(sessionID, message string, timeout time.Duration) string {
	t := s.t
	out, stderr := s.runCLI(timeout, true, "chat", "--session", sessionID, message)
	if strings.TrimSpace(out) == "" {
		t.Fatalf("chat turn returned empty reply\nstderr: %s\ndaemon log tail:\n%s", stderr, s.logTail())
	}
	return strings.TrimSpace(out)
}

// tasksDBPath is the daemon's tasks.db.
func (s *sandbox) tasksDBPath() string { return filepath.Join(s.StateDir, "tasks.db") }

// taskRows returns the current task rows (empty when tasks.db does not
// exist yet — the daemon creates it lazily on the first task).
func (s *sandbox) taskRows() []harness.TaskRow {
	t := s.t
	if _, err := os.Stat(s.tasksDBPath()); err != nil {
		return nil
	}
	return harness.Tasks(t, s.tasksDBPath())
}

// waitTaskTerminal polls tasks.db until the task reaches completed or failed.
func (s *sandbox) waitTaskTerminal(taskID string, timeout time.Duration) harness.TaskRow {
	t := s.t
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		for _, row := range s.taskRows() {
			if row.ID != taskID {
				continue
			}
			last = row.State
			if row.State == "completed" || row.State == "failed" {
				return row
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("task %s not terminal within %s (last state %q)", taskID, timeout, last)
	return harness.TaskRow{}
}

// rpc dials the daemon socket and performs one JSON-RPC call.
func (s *sandbox) rpc(method string, params any) map[string]any {
	client := harness.DialRPC(s.t, s.SocketPath)
	return client.CallResult(method, params)
}

// waitPlanApplied polls the plan file until it carries the applied marker.
func (s *sandbox) waitPlanApplied(planPath string, timeout time.Duration) string {
	t := s.t
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(planPath) //nolint:gosec // test-read of a sandbox plan path
		if err == nil && strings.Contains(string(data), "\n- applied: ") {
			return string(data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("applied marker never landed on %s within %s\ndaemon log tail:\n%s", planPath, timeout, s.logTail())
	return ""
}
