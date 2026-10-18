//go:build e2e

// Package sessionteardown covers the session-teardown e2e contract
// (session-affinity master Contract 6): the scratch llama-server stub
// endpoint receives EXACTLY ONE slot-erase per teardown moment.
//
// IDLE COVERAGE NOTE (leaf-07 decision, per the leaf's escape hatch): the
// idle path is NOT covered here. The daemon's IdleReaper ticker is
// hard-coded to 1 minute (internal/session/lifecycle.go
// defaultIdleCheckInterval) and there is no config knob for it, so a
// seconds-scale idle_release still waits up to a minute per tick —
// outside the hermetic budget and flaky by construction. The idle
// once-per-episode semantics are covered by the internal/session
// lifecycle unit tests, which construct the reaper directly. This suite
// proves the two daemon-driven paths end to end:
//
//   - archive (sessions.archive RPC) fires exactly one erase
//   - graceful daemon stop (ShutdownSweep, run before runtime stop)
//     fires exactly one erase per active session
//
// The stub ALSO mimics the llama-server --slot-save-path caveat: without
// it a real llama-server answers 501 NOT_SUPPORTED. The stub accepts the
// erase (200) so the suite tests meept's WIRE behavior; the 501 caveat is
// documented in docs/workflows/session-affinity.md, not exercised here.
package sessionteardown

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// llamaStub mimics a llama-server: GET /health answers ok, and
// POST /slots/0?action=erase (the VERIFIED route — pinned llama.cpp
// tools/server/server.cpp:286 + server-context.cpp:4796) is recorded and
// answered 200. Everything else is 404.
type llamaStub struct {
	mu     sync.Mutex
	erases []string // raw paths+query recorded, in order
	server *httptest.Server
}

func newLlamaStub(t *testing.T) *llamaStub {
	t.Helper()
	stub := &llamaStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); stream {
			// Mirror FakeLLM's SSE shape so the chat lane's streaming
			// terminator sees real content.
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher, _ := w.(http.Flusher)
			send := func(chunk string) {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
				if flusher != nil {
					flusher.Flush()
				}
			}
			send(`{"id":"stub","object":"chat.completion.chunk","model":"stub","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`)
			send(`{"id":"stub","object":"chat.completion.chunk","model":"stub","choices":[{"index":0,"delta":{"content":"llama stub ok"},"finish_reason":null}]}`)
			send(`{"id":"stub","object":"chat.completion.chunk","model":"stub","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"stub","object":"chat.completion","created":0,"model":"stub","choices":[{"index":0,"message":{"role":"assistant","content":"llama stub ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	})
	mux.HandleFunc("/slots/0", func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.erases = append(stub.erases, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		stub.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"n_discarded":0}`))
	})
	stub.server = httptest.NewServer(mux)
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *llamaStub) eraseCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.erases)
}

func (s *llamaStub) erasesSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.erases...)
}

// sandbox is the daemon-lifecycle scratch world with a llama-cpp runtime
// provider pointed at the stub. The lifecycle block registers the endpoint
// with the RuntimeManager (so TeardownBaseURL resolves it) but AutoStart
// stays false — nothing is spawned, the stub IS the server.
type sandbox struct {
	t          *testing.T
	Work       string
	Home       string
	MeeptHome  string
	StateDir   string
	ProjectDir string
	SocketPath string
	HTTPAddr   string

	Stub *llamaStub

	llamaChatLane bool

	cmd     *exec.Cmd
	logPath string
}

// sandboxOption customizes the sandbox before boot.
type sandboxOption func(*sandbox)

// withLlamaChatLane pins the chat agent's model to local-llama/stub so a
// chat turn actually SERVES on the llama endpoint (feeding the PM seam
// and the resolver snapshot the teardown coordinator reads).
var withLlamaChatLane sandboxOption = func(s *sandbox) { s.llamaChatLane = true }

func newSandbox(t *testing.T, stub *llamaStub, opts ...sandboxOption) *sandbox {
	t.Helper()
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
		Stub:       stub,
		logPath:    filepath.Join(work, "daemon.log"),
	}
	s.MeeptHome = filepath.Join(s.Home, ".meept")
	for _, d := range []string{s.Home, s.MeeptHome, s.StateDir, s.ProjectDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	resolvedWork := work
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		resolvedWork = resolved
	}
	cfg := fmt.Sprintf(`{
  // session-teardown scratch config (daemon-lifecycle suite pattern).
  "daemon": {
    "socket_path": %q,
    "pid_file": %q,
    "data_dir": %q,
    "log_level": "DEBUG",
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
		filepath.ToSlash(filepath.Join(work, "**")),
		filepath.ToSlash(filepath.Join(resolvedWork, "**")),
	)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "meept.json5"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write meept.json5: %v", err)
	}

	// models.json5: the general lane rides an OpenAI-compatible FakeLLM
	// (cloud-shaped chat turns work hermetically); the llama provider
	// carries the lifecycle block that REGISTERS the stub endpoint with
	// the runtime manager (llama-cpp endpoint key) — AutoStart false,
	// nothing spawns. The lifecycle validates model_path, so the test
	// writes a touch file.
	fake := harness.NewFakeLLM()
	t.Cleanup(fake.Close)
	modelFile := filepath.Join(s.Work, "fake-model.gguf")
	if err := os.WriteFile(modelFile, []byte("gguf-placeholder"), 0o644); err != nil {
		t.Fatalf("write placeholder model: %v", err)
	}
	pidFile := filepath.Join(s.StateDir, "llama.pid")
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
    "chat-llama": { "models": ["local-llama/stub"], "timeout": 60, "max_fails": 3 },
    "classifier": { "models": ["fake/fake-model"], "timeout": 10, "max_fails": 2 },
    "summarizer": { "models": ["fake/fake-model"], "timeout": 10, "max_fails": 2 },
    "small":      { "models": ["fake/fake-model"], "timeout": 10, "max_fails": 2 },
    "coder":      { "models": ["fake/fake-model"], "timeout": 60, "max_fails": 3 },
    "planner":    { "models": ["fake/fake-model"], "timeout": 60, "max_fails": 3 },
    "analyst":    { "models": ["fake/fake-model"], "timeout": 60, "max_fails": 3 }
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
    },
    "local-llama": {
      "api": "openai",
      "options": { "baseURL": %q, "apiKey": "test-key-llama" },
      "lifecycle": {
        "runtime": "llama-cpp",
        "model_path": %q,
        "auto_start": false,
        "pid_file": %q,
        "spawn_command": ["sleep", "3600"],
        "health_check": { "endpoint": "/health", "interval_seconds": 3600, "timeout_seconds": 1, "unhealthy_threshold": 99 }
      },
      "models": {
        "stub": {
          "name": "stub",
          "capabilities": ["completion"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 8192,
          "max_output": 1024
        }
      }
    }
  }
}`,
		fake.URL()+"/v1",
		stub.server.URL+"/v1",
		modelFile,
		pidFile,
	)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "models.json5"), []byte(models), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	s.seedRoster()
	if s.llamaChatLane {
		// Pin the ALIAS, not the direct ref: the loop's rotation path only
		// engages modelRef values that name an alias (resolver.HasAlias
		// gate, loop.go reasoningCycle) — a direct provider/model ref is
		// silently dropped and the turn serves on the default provider
		// (observed live: llm_calls showed fake|fake-model with the pin
		// present). "chat-llama" is declared in models.json5 above.
		s.pinChatModel("chat-llama")
	}
	s.boot()
	return s
}

// pinChatModel points the chat agent's loop at modelRef (a direct
// provider/model ref) so chat turns serve on that endpoint.
func (s *sandbox) pinChatModel(modelRef string) {
	t := s.t
	path := filepath.Join(s.MeeptHome, "agents", "chat", "AGENT.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read chat AGENT.md: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "\nmodel:") {
		return
	}
	marker := "id: chat\n"
	if !strings.Contains(text, marker) {
		t.Fatalf("chat AGENT.md has no id: chat line; cannot pin model")
	}
	text = strings.Replace(text, marker, marker+"model: "+modelRef+"\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write chat AGENT.md: %v", err)
	}
}

// seedRoster copies the repo's agent roster + prompt templates into the
// sandboxed MEEPT_HOME (same seeding the harness does).
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
	cmd.Dir = s.Work
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "HOME="+s.Home, "MEEPT_HOME="+s.MeeptHome)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start daemon: %v", err)
	}
	s.cmd = cmd
	logFile.Close()

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

// stopGraceful SIGTERMs the daemon and waits for exit — the path that runs
// the teardown ShutdownSweep before runtimes stop.
func (s *sandbox) stopGraceful() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _, _ = s.cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
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
	return append(os.Environ(),
		"HOME="+s.Home,
		"MEEPT_HOME="+s.MeeptHome,
	)
}

// runCLI runs one meept CLI invocation against the sandbox.
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
	var stdout, stderr strings.Builder
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

// archiveSession archives (archived=true) or unarchives a session via the
// sessions.archive RPC path (the same store.Archive hook the teardown
// coordinator subscribes to).
func (s *sandbox) archiveSession(sessionID string, archived bool) {
	// The RPC route is `sessions.archive`, but the CLI has NO `rpc`
	// passthrough subcommand (the TUI calls the RPC over its socket link;
	// `meept rpc ...` is not a command and errors with "accepts at most 1
	// arg(s)"). The archive REST endpoint is the same service path
	// (handleSessionArchive -> SessionService.ArchiveSession -> store.
	// Archive), so drive it over the sandbox's HTTPS endpoint.
	body, err := json.Marshal(map[string]any{"archived": archived})
	if err != nil {
		s.t.Fatalf("marshal archive body: %v", err)
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // scratch self-signed cert
			ForceAttemptHTTP2: false,
		},
	}
	req, err := http.NewRequest(http.MethodPatch,
		"https://"+s.HTTPAddr+"/api/v1/sessions/"+sessionID, bytes.NewReader(body))
	if err != nil {
		s.t.Fatalf("build archive request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		s.t.Fatalf("archive session %s: %v", sessionID, err)
	}
	defer resp.Body.Close() //nolint:errcheck // drained below
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		s.t.Fatalf("archive session %s: status %d", sessionID, resp.StatusCode)
	}
}

// waitForErases polls the stub until it has recorded n erases (or fails).
func waitForErases(t *testing.T, stub *llamaStub, n int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := stub.eraseCount(); got >= n {
			return stub.erasesSnapshot()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("stub recorded %d erase(s) within %s, want %d (recorded: %v)",
		stub.eraseCount(), timeout, n, stub.erasesSnapshot())
	return nil
}

// assertErasesExact fails unless every recorded erase is the VERIFIED
// route and the count is exactly n (allowing a settle window for stragglers
// — a second erase within the window still fails the exact-count assert).
func assertErasesExact(t *testing.T, stub *llamaStub, n int, settle time.Duration) []string {
	t.Helper()
	got := waitForErases(t, stub, n, 30*time.Second)
	time.Sleep(settle) // let any duplicate/dispatched straggler land
	got = stub.erasesSnapshot()
	if len(got) != n {
		t.Fatalf("erase count = %d, want EXACTLY %d (recorded: %v)", len(got), n, got)
	}
	for i, e := range got {
		if e != "POST /slots/0?action=erase" {
			t.Fatalf("erase %d = %q, want %q (VERIFIED route: POST /slots/0?action=erase)",
				i, e, "POST /slots/0?action=erase")
		}
	}
	return got
}
