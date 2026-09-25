package harness

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// maxConcurrentDaemons bounds concurrently-live scratch daemons. macOS
// exhausts its ephemeral port range when many localhost listeners/RPC
// sockets come up at once (AGENTS.md -p 2 rule); the harness enforces the
// same discipline in-process.
var daemonSlots = make(chan struct{}, 2)

// FreePort probes a free loopback port. The probe is TOCTOU by nature
// (same as the bash rig); callers use it immediately before spawn.
func FreePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("harness: probe free port: %v", err)
	}
	defer l.Close()
	addr := l.Addr().(*net.TCPAddr)
	if addr.Port == 18099 || addr.Port == 8081 {
		// Never collide with the user's live daemon defaults.
		return FreePort(t)
	}
	return addr.Port
}

// Stack wires one hermetic test world: fake LLM + scratch daemon + sandbox
// home. Create with NewStack (per-suite) or Start (per-test).
type Stack struct {
	Fake   *FakeLLM
	Daemon *Daemon

	// Work is the temp root: home/, state/, project/, bin/, daemon.log.
	Work       string
	Home       string // sandboxed $HOME
	MeeptHome  string // $MEEPT_HOME (Home/.meept)
	StateDir   string // daemon data dir
	ProjectDir string // registered project directory
	SocketPath string

	t testing.TB
}

// StartOption customizes one Start() sandbox world. Options run in order
// BEFORE the daemon boots: config overlays land in meept.json5, overlay
// hooks run just before the write.
type StartOption func(*startConfig)

// startConfig collects Start() options.
type startConfig struct {
	configOverlay func(cfg map[string]any) error
	beforeWrite   func(s *Stack) error
}

func newStartConfig() *startConfig { return &startConfig{} }

// applyLocked merges one dotted key path into cfg, creating intermediate
// maps. A nil value deletes the key.
func applyLocked(cfg map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	m := cfg
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	last := parts[len(parts)-1]
	if value == nil {
		delete(m, last)
		return
	}
	m[last] = value
}

// WithConfigOverlay merges dotted config keys into the sandbox
// meept.json5 before the daemon boots ("multiuser.enabled" ->
// {"multiuser":{"enabled":...}}). A nil value deletes a key. Applied in
// option order; later keys win. Values must marshal to JSON (maps, slices,
// scalars — no json5-specific syntax).
func WithConfigOverlay(keys map[string]any) StartOption {
	return func(sc *startConfig) {
		prev := sc.configOverlay
		sc.configOverlay = func(cfg map[string]any) error {
			if prev != nil {
				if err := prev(cfg); err != nil {
					return err
				}
				if cfg == nil {
					return nil
				}
			}
			for k, v := range keys {
				applyLocked(cfg, k, v)
			}
			return nil
		}
	}
}

// WithConfigHook registers a raw callback over the parsed meept.json5
// tree just before it is written (full structural control when dotted
// keys are not enough).
func WithConfigHook(fn func(cfg map[string]any)) StartOption {
	return func(sc *startConfig) {
		prev := sc.configOverlay
		sc.configOverlay = func(cfg map[string]any) error {
			if prev != nil {
				if err := prev(cfg); err != nil {
					return err
				}
				if cfg == nil {
					return nil
				}
			}
			fn(cfg)
			return nil
		}
	}
}

// WithPreBootHook runs a callback with the half-built Stack right before
// the config write + boot (seed extra files into MeeptHome, stage
// acp_agents.json5, etc). Fails the test on error.
func WithPreBootHook(fn func(s *Stack) error) StartOption {
	return func(sc *startConfig) { sc.beforeWrite = fn }
}

// Start builds the binaries once per process (sync.OnceValues), creates a
// sandbox world under a fresh temp dir (NOT t.TempDir(): the daemon's Unix
// socket path must stay under the macOS 104-char sun_path limit, and a
// per-test TempDir prefix like TestChatTurnCreatesArtifact123/001 blows
// past it), writes minimal meept.json5 + models.json5 pointing every
// provider at a fresh FakeLLM, boots the daemon, and registers t.Cleanup
// teardown. Each call gets its own fake LLM and its own free HTTP port.
// Options (WithConfigOverlay, WithConfigHook, WithPreBootHook) customize
// the sandbox before boot.
func Start(t testing.TB, opts ...StartOption) *Stack {
	t.Helper()
	sc := newStartConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(sc)
		}
	}
	if sc.beforeWrite != nil {
		// Deferred to just after the dirs exist (below).
	}

	// MkdirTemp("", "meept-e2e-*") yields /var/folders/.../T/meept-e2e-NNN
	// (~70 chars); the socket at <root>/state/meept.sock stays ~85 chars.
	work, err := os.MkdirTemp("", "meept-e2e-*")
	if err != nil {
		t.Fatalf("harness: temp dir: %v", err)
	}
	if os.Getenv("MEEPT_E2E_KEEP") == "" {
		t.Cleanup(func() { os.RemoveAll(work) })
	} else {
		t.Logf("harness: MEEPT_E2E_KEEP=1 — keeping workdir %s", work)
	}
	// macOS TMPDIR resolves through /private/var; permission globs and
	// path checks must cover the resolved spelling (bash rig lesson).
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		work = resolved
	}

	s := &Stack{
		Work:       work,
		Home:       filepath.Join(work, "home"),
		StateDir:   filepath.Join(work, "state"),
		ProjectDir: filepath.Join(work, "project"),
		t:          t,
	}
	s.MeeptHome = filepath.Join(s.Home, ".meept")
	s.SocketPath = filepath.Join(s.StateDir, "meept.sock")

	for _, d := range []string{s.Home, s.MeeptHome, s.StateDir, s.ProjectDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("harness: mkdir %s: %v", d, err)
		}
	}

	s.seedRoster()

	if sc.beforeWrite != nil {
		if err := sc.beforeWrite(s); err != nil {
			t.Fatalf("harness: pre-boot hook: %v", err)
		}
	}

	s.Fake = NewFakeLLM()
	if os.Getenv("MEEPT_E2E_KEEP") != "" {
		s.Fake.SetDebugPath(filepath.Join(work, "fake-llm-requests.jsonl"))
	}
	t.Cleanup(s.Fake.Close)

	s.Daemon = newDaemon(t, s)
	s.writeConfigs(sc)
	s.Daemon.boot()

	// Defensive teardown even though Daemon.boot registers its own.
	t.Cleanup(func() { s.Daemon.stop() })
	return s
}

// writeConfigs pins the minimal meept.json5 + models.json5 into the
// sandboxed MEEPT_HOME. Every path lives inside Work; HTTP listens on a
// probed free port; models.json5 points ONE openai-compatible provider at
// the fake LLM and routes every alias through it (no lifecycle blocks —
// nothing spawns, so the classifier boot gate never trips).
// Start options (WithConfigOverlay / WithConfigHook) are applied to the
// parsed template right before it is re-serialized.
func (s *Stack) writeConfigs(sc *startConfig) {
	t := s.t
	httpPort := FreePort(t)
	s.Daemon.httpPort = httpPort
	s.Daemon.httpAddr = fmt.Sprintf("127.0.0.1:%d", httpPort)

	workGlob := filepath.ToSlash(filepath.Join(s.Work, "**"))
	cfgSrc := fmt.Sprintf(`{
  // e2e scratch daemon (go test harness): everything stays in the temp dir.
  "daemon": {
    "socket_path": %q,
    "pid_file": %q,
    "data_dir": %q,
    "log_level": "INFO",
  },
  "transport": {
    "rpc": {
      "enabled": true,
      "socket_path": %q,
    },
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
  "memory": {
    "data_dir": %q,
  },
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
  "multiagent": {
    "enabled": true,
  },
}`,
		s.SocketPath,
		filepath.Join(s.StateDir, "meept.pid"),
		s.StateDir,
		s.SocketPath,
		s.Daemon.httpAddr,
		filepath.Join(s.StateDir, "tls", "cert.pem"),
		filepath.Join(s.StateDir, "tls", "key.pem"),
		filepath.Join(s.StateDir, "memory"),
		filepath.Join(s.StateDir, "projects"),
		filepath.Join(s.StateDir, "audit.db"),
		workGlob,
	)

	cfg := []byte(cfgSrc)
	if sc.configOverlay != nil {
		// Strip // comments (invalid JSON) and trailing commas before
		// parsing, overlay the parsed tree, and re-serialize. The daemon
		// parses JSON5, so plain JSON output is accepted.
		stripped := json5Comment.ReplaceAllString(cfgSrc, "")
		stripped = json5TrailingCommas.ReplaceAllString(stripped, "$1")
		var parsed map[string]any
		if err := json.Unmarshal([]byte(stripped), &parsed); err != nil {
			t.Fatalf("harness: parse config template: %v", err)
		}
		if err := sc.configOverlay(parsed); err != nil {
			t.Fatalf("harness: config overlay: %v", err)
		}
		out, err := json.MarshalIndent(parsed, "", "  ")
		if err != nil {
			t.Fatalf("harness: serialize overlaid config: %v", err)
		}
		cfg = out
	}
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "meept.json5"), cfg, 0o600); err != nil {
		t.Fatalf("harness: write meept.json5: %v", err)
	}

	fakeV1 := s.Fake.URL() + "/v1"
	models := fmt.Sprintf(`{
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
      "options": {
        "baseURL": %q,
        "noAuth": true
      },
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
}`, fakeV1)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "models.json5"), []byte(models), 0o600); err != nil {
		t.Fatalf("harness: write models.json5: %v", err)
	}
}

// json5TrailingCommas removes trailing commas before "}" or "]" so the
// template (written for the daemon's JSON5 parser) is valid JSON for the
// overlay round-trip.
var json5TrailingCommas = regexp.MustCompile(`(?s),(\s*[}\]])`)

// json5Comment strips // comments anywhere outside strings is overkill
// here: the template only ever comments on whole lines, so a conservative
// line-scoped pattern suffices for the overlay round-trip.
var json5Comment = regexp.MustCompile(`(?m)^\s*//[^\n]*(\n|$)`)

// Daemon manages one scratch meept-daemon process.
type Daemon struct {
	t        testing.TB
	stack    *Stack
	httpPort int
	httpAddr string

	cmd      *exec.Cmd
	logPath  string
	stopOnce sync.Once
}

func newDaemon(t testing.TB, s *Stack) *Daemon {
	return &Daemon{t: t, stack: s, logPath: filepath.Join(s.Work, "daemon.log")}
}

var (
	// repoRoot is resolved once, at the first harness use, from this
	// file's own location (e2e/harness/), so the build works regardless
	// of the test process's working directory.
	repoRoot = func() string {
		_, thisFile, _, _ := runtime.Caller(0) //nolint:dogsled // identity probe
		return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	}()

	buildBinaries = sync.OnceValues(func() (string, error) {
		dir, err := os.MkdirTemp("", "meept-e2e-bin")
		if err != nil {
			return "", err
		}
		for _, target := range []struct{ pkg, out string }{
			{"./cmd/meept-daemon", "meept-daemon"},
			{"./cmd/meept", "meept"},
		} {
			bin := filepath.Join(dir, target.out)
			cmd := exec.Command("go", "build", "-o", bin, target.pkg)
			cmd.Dir = repoRoot
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("go build %s: %v: %s", target.pkg, err, strings.TrimSpace(stderr.String()))
			}
		}
		return dir, nil
	})
)

// CLIPath returns the path of a freshly built meept binary (built once per
// test-process into a shared temp dir; never the repo's bin/).
func CLIPath(t testing.TB) string {
	t.Helper()
	dir, err := buildBinaries()
	if err != nil {
		t.Fatalf("harness: build binaries: %v", err)
	}
	return filepath.Join(dir, "meept")
}

// DaemonPath returns the freshly built meept-daemon binary path.
func DaemonPath(t testing.TB) string {
	t.Helper()
	dir, err := buildBinaries()
	if err != nil {
		t.Fatalf("harness: build binaries: %v", err)
	}
	return filepath.Join(dir, "meept-daemon")
}

// boot starts the daemon with cwd deliberately different from any project
// dir (the bash rig's A4 rule) and waits for /health.
func (d *Daemon) boot() {
	t := d.t
	s := d.stack

	select {
	case daemonSlots <- struct{}{}:
	default:
		// Bound concurrent daemons; fail loudly rather than queueing
		// into port exhaustion.
		t.Fatal("harness: too many concurrent scratch daemons (slot cap 2); a prior test leaked its daemon")
	}

	bin := DaemonPath(t)
	logFile, err := os.OpenFile(d.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("harness: open daemon log: %v", err)
	}

	cmd := exec.Command(bin,
		"-c", filepath.Join(s.MeeptHome, "meept.json5"),
		"-d", s.StateDir,
		"-s", s.SocketPath,
	)
	cmd.Dir = s.Work // NOT the project dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		"HOME="+s.Home,
		"MEEPT_HOME="+s.MeeptHome,
	)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		<-daemonSlots
		t.Fatalf("harness: start daemon: %v", err)
	}
	d.cmd = cmd
	logFile.Close()

	t.Cleanup(func() { d.stop() })

	// Wait for HTTP /health (self-signed TLS). The daemon fails fast at
	// boot on config errors, so poll the process too.
	deadline := time.Now().Add(60 * time.Second)
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			ForceAttemptHTTP2: false,
		},
	}
	healthURL := "https://" + d.httpAddr + "/health"
	for {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Fatalf("harness: daemon exited during boot; log tail:\n%s", d.logTail())
		}
		resp, err := client.Get(healthURL) //nolint:noctx // bounded poll in test harness
		if err == nil {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("harness: daemon not healthy within 60s (%s); log tail:\n%s", healthURL, d.logTail())
		}
		// A dead daemon never becomes healthy.
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("harness: daemon process died during boot; log tail:\n%s", d.logTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// stop terminates the daemon (SIGTERM, then SIGKILL) exactly once.
func (d *Daemon) stop() {
	d.stopOnce.Do(func() {
		defer func() { <-daemonSlots }()
		if d.cmd == nil || d.cmd.Process == nil {
			return
		}
		_ = d.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _, _ = d.cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = d.cmd.Process.Kill()
			<-done
		}
	})
}

// Pid returns the daemon process id (0 before boot).
func (d *Daemon) Pid() int {
	if d.cmd == nil || d.cmd.Process == nil {
		return 0
	}
	return d.cmd.Process.Pid
}

// LogTail returns the last 4KB of the daemon log.
func (d *Daemon) LogTail() string { return d.logTail() }

func (d *Daemon) logTail() string {
	data, err := os.ReadFile(d.logPath)
	if err != nil {
		return fmt.Sprintf("(no daemon log at %s: %v)", d.logPath, err)
	}
	const tail = 4096
	if len(data) > tail {
		data = data[len(data)-tail:]
	}
	return string(data)
}

// HTTPBaseURL returns the daemon's https base URL.
func (s *Stack) HTTPBaseURL() string { return "https://" + s.Daemon.httpAddr }

// TasksDBPath returns the daemon's tasks.db path (data_dir/tasks.db).
func (s *Stack) TasksDBPath() string { return filepath.Join(s.StateDir, "tasks.db") }

// Env returns the sandboxed environment for CLI invocations.
func (s *Stack) Env() []string {
	return append(os.Environ(),
		"HOME="+s.Home,
		"MEEPT_HOME="+s.MeeptHome,
	)
}

// CLIArgv assembles the common CLI prefix: binary + socket + state dir.
func (s *Stack) CLIArgv(t testing.TB, args ...string) []string {
	t.Helper()
	return append([]string{
		CLIPath(t),
		"--socket", s.SocketPath,
		"-d", s.StateDir,
	}, args...)
}

// RunCLI runs one meept CLI command against the sandbox and returns
// (stdout, stderr). It fails the test when the command fails unless
// ignoreError is true.
func (s *Stack) RunCLI(t testing.TB, timeout time.Duration, ignoreError bool, args ...string) (string, string) {
	t.Helper()
	argv := s.CLIArgv(t, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = s.Work
	cmd.Env = s.Env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && !ignoreError {
		t.Fatalf("harness: meept %s failed: %v\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

// RegisterProject registers ProjectDir under the given name via the CLI.
func (s *Stack) RegisterProject(t testing.TB, name string) {
	t.Helper()
	s.RunCLI(t, 30*time.Second, false, "projects", "add", s.ProjectDir, "--name", name)
}

// CreateSession creates a named session bound to cwd and returns its id.
func (s *Stack) CreateSession(t testing.TB, name, cwd string) string {
	t.Helper()
	out, _ := s.RunCLI(t, 30*time.Second, false,
		"--cwd", cwd, "session", "create", name)
	// Output: "Created session: <id>"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Created session: "); ok {
			return strings.TrimSpace(after)
		}
	}
	t.Fatalf("harness: session create returned no id; output:\n%s", out)
	return ""
}

// ChatTurn submits one chat turn to the session and waits for the
// turn.terminal result (the CLI await path: chat.submit + bus.poll).
// Returns the terminal reply text. Fails the test on error/stall.
func (s *Stack) ChatTurn(t testing.TB, sessionID, message string, timeout time.Duration) string {
	t.Helper()
	out, stderr := s.RunCLI(t, timeout, true, "chat", "--session", sessionID, message)
	if strings.TrimSpace(out) == "" {
		t.Fatalf("harness: chat turn returned empty reply\nstderr: %s\ndaemon log tail:\n%s",
			stderr, s.Daemon.LogTail())
	}
	return strings.TrimSpace(out)
}

// SubmitChatHTTP posts to /api/v1/chat/submit and returns the ack as a map.
func (s *Stack) SubmitChatHTTP(t testing.TB, sessionID, message string) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"message":       message,
		"session_id":    sessionID,
		"source_client": "e2e-harness",
	})
	client := s.httpInsecureClient()
	resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/chat/submit", "application/json", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("harness: chat submit: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("harness: chat submit status %d: %s", resp.StatusCode, body)
	}
	var ack map[string]any
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("harness: chat submit ack decode: %v", err)
	}
	return ack
}

// GetTaskHTTP fetches /api/v1/tasks/{id} as a raw map.
func (s *Stack) GetTaskHTTP(t testing.TB, taskID string) map[string]any {
	t.Helper()
	client := s.httpInsecureClient()
	resp, err := client.Get(s.HTTPBaseURL() + "/api/v1/tasks/" + taskID) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("harness: get task: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("harness: get task status %d: %s", resp.StatusCode, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("harness: task decode: %v", err)
	}
	return out
}

// HealthJSON returns the /health payload (verify the HTTP surface).
func (s *Stack) HealthJSON(t testing.TB) map[string]string {
	t.Helper()
	client := s.httpInsecureClient()
	resp, err := client.Get(s.HTTPBaseURL() + "/health") //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("harness: health: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("harness: health decode: %v", err)
	}
	return out
}

func (s *Stack) httpInsecureClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
}

// WaitFor polls cond every 250ms until it passes or the timeout expires.
func WaitFor(t testing.TB, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("harness: timed out after %s waiting for %s", timeout, what)
}

// PortString formats a port number as a loopback addr (test convenience).
func PortString(port int) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) }

// seedRoster copies the repo's agent definitions (config/agents) and
// prompt templates (config/prompts) into the sandboxed MEEPT_HOME — the
// same seeding scripts/e2e-naive-user-chat.sh does. Without the roster
// the dispatcher's specialists never register ("agent spec not found:
// coder") and every step job falls back to a tool-less main loop.
func (s *Stack) seedRoster() {
	t := s.t
	pairs := []struct{ src, dst string }{
		{filepath.Join(repoRoot, "config", "agents"), filepath.Join(s.MeeptHome, "agents")},
		{filepath.Join(repoRoot, "config", "prompts"), filepath.Join(s.MeeptHome, "prompts")},
	}
	for _, pair := range pairs {
		if _, err := os.Stat(pair.src); err != nil {
			continue // repo without the roster: nothing to seed
		}
		if err := os.MkdirAll(pair.dst, 0o755); err != nil {
			t.Fatalf("harness: mkdir %s: %v", pair.dst, err)
		}
		if err := copyDir(pair.src, pair.dst); err != nil {
			t.Fatalf("harness: seed %s: %v", pair.dst, err)
		}
	}
}

// copyDir recursively copies src into dst (merge, files only).
func copyDir(src, dst string) error {
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
