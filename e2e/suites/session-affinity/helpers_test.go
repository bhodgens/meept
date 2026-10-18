//go:build e2e

// Package sessionaffinity covers the session-affinity e2e contract:
// every LLM request the scratch daemon fires carries the session_id
// affinity header with the TURN's session id, secondary scoped calls
// (classifier / reflection paths) carry it too, a session-less call
// (no WithTaskScope) carries none, and sticky endpoint rotation follows
// provider health. The suite uses the daemon-lifecycle /
// continuity-regressions sandbox pattern (harness.Start owns exactly one
// models.json5 shape; these tests need a custom two-provider topology,
// so they write their own configs and boot the harness-built binary).
package sessionaffinity

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

// headerStub is a minimal OpenAI-compatible server that records the
// session_id header of every /v1/chat/completions request and answers
// the response envelope the meept client parses (plain chat text — good
// enough for chat, classifier, and summarizer lanes).
type headerStub struct {
	mu       sync.Mutex
	sessions []string // header value per request, in arrival order
	fail503  bool
	baseURL  string // set once at construction; read via base()
}

func (h *headerStub) record(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions = append(h.sessions, sessionID)
}

func (h *headerStub) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.sessions...)
}

func (h *headerStub) setFail(fail bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fail503 = fail
}

func (h *headerStub) failed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fail503
}

// base returns the stub's models.json5 baseURL (URL + /v1).
func (h *headerStub) base() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.baseURL
}

// newHeaderStub starts the stub; base is its URL + /v1 for models.json5.
func newHeaderStub(t *testing.T) (*headerStub, string) {
	t.Helper()
	stub := &headerStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		stub.record(r.Header.Get("session_id"))
		if stub.failed() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"stub down"}}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		content := "stub ok"
		if stream, _ := body["stream"].(bool); stream {
			// Streaming path: mirror FakeLLM's SSE shape (role delta,
			// content delta, finish chunk with usage, [DONE]).
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher, _ := w.(http.Flusher)
			send := func(chunk string) {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
				if flusher != nil {
					flusher.Flush()
				}
			}
			send(`{"id":"stub","object":"chat.completion.chunk","model":"fake-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`)
			send(`{"id":"stub","object":"chat.completion.chunk","model":"fake-model","choices":[{"index":0,"delta":{"content":"stub ok"},"finish_reason":null}]}`)
			send(`{"id":"stub","object":"chat.completion.chunk","model":"fake-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		// Non-streaming path (classifier, summarizer, other secondary
		// lanes): plain JSON completion envelope.
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"stub","object":"chat.completion","created":0,"model":"fake-model","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, content)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	stub.baseURL = server.URL + "/v1"
	return stub, stub.baseURL
}

// sandbox is the daemon-lifecycle scratch world with a header-stub
// provider topology: provider "fake" at stubA (primary alias member) and
// provider "fakeb" at stubB. Distinct apiKeys make the two providers'
// EndpointKeys distinct (host + credential fingerprint); noAuth stays OFF
// so the client sends the Authorization header (the default affinity
// header path only needs a non-empty scope, but keeping the wire shape
// production-like avoids surprises). The general alias carries a
// seconds-scale timeout so a failed member's endpoint block expires
// between turns — with the production 300s the block would outlive the
// whole test and both members would sit blocked (the turn PARKS, the
// reply comes back empty).
type sandbox struct {
	t          *testing.T
	Work       string
	Home       string
	MeeptHome  string
	StateDir   string
	ProjectDir string
	SocketPath string
	HTTPAddr   string

	StubA *headerStub
	StubB *headerStub

	cmd     *exec.Cmd
	logPath string
}

// newSandbox builds the world and boots the daemon with the given
// alias-member list (["fake/fake-model"] single-provider, or
// ["fake/fake-model","fakeb/fake-model"] for the rotation test).
func newSandbox(t *testing.T, stubA, stubB *headerStub, aliasModels []string) *sandbox {
	t.Helper()

	// Short temp prefix: the Unix socket must stay under the macOS
	// 104-byte sun_path cap (do NOT EvalSymlinks the root).
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
		StubA:      stubA,
		StubB:      stubB,
		logPath:    filepath.Join(work, "daemon.log"),
	}
	s.MeeptHome = filepath.Join(s.Home, ".meept")
	for _, d := range []string{s.Home, s.MeeptHome, s.StateDir, s.ProjectDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// The fence matches RESOLVED paths (/private/var on macOS), so
	// allowed_paths carries both spellings of the work root.
	resolvedWork := work
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		resolvedWork = resolved
	}
	cfg := fmt.Sprintf(`{
  // session-affinity scratch config (daemon-lifecycle suite pattern).
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
  "llm": {
    "failure_policy": {
      "base_throttle": "1s",
      "pacing": { "enabled": false },
    },
  },
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

	// Affinity config: hermetic defaults only (enabled, session_id, 30m,
	// 5s) — the suite never shortens llm.session_affinity knobs.
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
    "general":    { "models": %s, "timeout": 5, "max_fails": 3 },
    "classifier": { "models": %s, "timeout": 10, "max_fails": 2 },
    "summarizer": { "models": %s, "timeout": 10, "max_fails": 2 },
    "small":      { "models": %s, "timeout": 10, "max_fails": 2 },
    "coder":      { "models": %s, "timeout": 60, "max_fails": 3 },
    "planner":    { "models": %s, "timeout": 60, "max_fails": 3 },
    "analyst":    { "models": %s, "timeout": 60, "max_fails": 3 }
  },
  "providers": {
    "fake": {
      "api": "openai",
      "options": { "baseURL": %q, "apiKey": "test-key-a" },
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
    }%s
  }
}`,
		aliasJSON(aliasModels), aliasJSON(aliasModels), aliasJSON(aliasModels),
		aliasJSON(aliasModels), aliasJSON(aliasModels), aliasJSON(aliasModels), aliasJSON(aliasModels),
		stubA.base(),
		providerBBlock(stubB),
	)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "models.json5"), []byte(models), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}

	s.seedRoster()
	s.pinChatModelAlias()
	s.boot()
	return s
}

// pinChatModelAlias points the chat agent's loop at the "general" alias so
// the CHAT lane goes through ResolveForAlias — the only path where sticky
// endpoint rotation (Contract 4) engages. Without it the chat loop holds
// the direct default model ref and a dead member just fails the turn.
func (s *sandbox) pinChatModelAlias() {
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
		t.Fatalf("chat AGENT.md has no id: chat line; cannot pin model alias")
	}
	text = strings.Replace(text, marker, marker+"model: general\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write chat AGENT.md: %v", err)
	}
}

// aliasJSON renders ["fake/fake-model", ...] as a JSON array literal.
func aliasJSON(models []string) string {
	parts := make([]string, len(models))
	for i, m := range models {
		parts[i] = fmt.Sprintf("%q", m)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// providerBBlock renders the second provider (empty when stubB is nil).
func providerBBlock(stubB *headerStub) string {
	if stubB == nil {
		return ""
	}
	return fmt.Sprintf(`,
    "fakeb": {
      "api": "openai",
      "options": { "baseURL": %q, "apiKey": "test-key-b" },
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
    }`, stubB.base())
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

// countBySession counts a stub's requests that carried sessionID's header.
func countBySession(values []string, sessionID string) int {
	n := 0
	for _, v := range values {
		if v == sessionID {
			n++
		}
	}
	return n
}
