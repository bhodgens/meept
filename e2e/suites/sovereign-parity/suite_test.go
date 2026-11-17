//go:build e2e

// Package sovereignparity is the hermetic e2e gate suite for the
// sovereign-router-parity plan tree (docs/plans/20261115-sovereign-router-parity,
// gate leaf 00T). It pins contracts C1-C11 as failing tests BEFORE any
// implementation leaf lands: every scenario must be RED for a
// missing-implementation reason today and GREEN only after leaves 01-09
// land. Tests only — this suite never modifies production code.
//
// Topology (session-affinity / llm-serving-tiers precedent): one custom
// sandbox per test — a parityStub OpenAI-compatible endpoint per provider
// (per-model scripted replies + request counters), a scratch daemon built
// once per process by the shared harness (harness.DaemonPath, scratch bin
// under $TMPDIR/meept-e2e-bin*), and a temp MEEPT_HOME. No network, no
// real provider calls.
//
// Two layers:
//   - daemon-level scenarios (admission/posture/shadow/poolfailure/
//     budget/sticky/flagoff) drive the scratch daemon over the CLI, the
//     HTTP chat surface, and the bus, and read routing.db / metrics.db
//     read-only for decision-level assertions. These files stay free of
//     not-yet-existing symbols so they run (and fail) at runtime today.
//   - contract-level tests (quality/toolcheck/pricegroup/
//     derivedifficulty + contracts_test.go) reference the C1-C11 symbols
//     the implementation leaves must declare; the package therefore does
//     not compile until the leaves land — that compile failure IS the
//     documented RED gate.
package sovereignparity

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// TestMain is the suite scaffold (leaf 00T T1): standard runner; the
// harness wiring (scratch-daemon build, temp worlds) happens per test via
// newParitySandbox, which is the harness-boot seam the leaf names.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// parityStub: one OpenAI-compatible upstream, fully scripted.
// ---------------------------------------------------------------------------

// parityStub answers /v1/chat/completions for one provider. Replies are
// keyed by request shape, never arrival order: classifier-shaped prompts
// get the pinned classifier JSON (when set), planner-shaped prompts get a
// valid one-step plan, and everything else gets the per-model scripted
// mode ("ok" content, or an injected HTTP status for failure injection).
// Every request is counted, total and per model — the counters are the
// "fake upstream saw N requests" evidence the gate scenarios assert on.
type parityStub struct {
	mu            sync.Mutex
	hits          int
	byModel       map[string]int
	modes         map[string]string // model -> "ok" | "status:<code>"
	faultOnce     map[string]int    // model -> remaining one-shot status faults ("fault:<code>")
	classifierOut string            // pinned classifier JSON ("" = default chat reply)
	server        *httptest.Server
	v1            string // URL + "/v1" for models.json5 baseURL
}

func newParityStub(t *testing.T) *parityStub {
	t.Helper()
	st := &parityStub{
		byModel:   map[string]int{},
		modes:     map[string]string{},
		faultOnce: map[string]int{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", st.handleCompletions)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, `{"status":"ok"}`)
	})
	st.server = httptest.NewServer(mux)
	t.Cleanup(st.server.Close)
	st.v1 = st.server.URL + "/v1"
	return st
}

// setMode scripts a permanent per-model reply mode ("ok" or "status:401").
func (st *parityStub) setMode(model, mode string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.modes[model] = mode
}

// setFaultOnce scripts a one-shot status fault: the next request for the
// model gets the status, every later request gets "ok".
func (st *parityStub) setFaultOnce(model string, status int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.faultOnce[model] = status
}

// setClassifierOut pins the classifier JSON reply ("" restores default).
func (st *parityStub) setClassifierOut(out string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.classifierOut = out
}

// hitCount returns the total request count.
func (st *parityStub) hitCount() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.hits
}

// chatModelHits returns the request count for ONE model id — the
// chat-lane-only subset of hitCount (classifier/ambient/summarizer
// traffic hits other model names and must not pollute zero-dispatch
// assertions).
func (st *parityStub) chatModelHits(model string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.byModel[model]
}

func (st *parityStub) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	model, _ := body["model"].(string)

	st.mu.Lock()
	st.hits++
	st.byModel[model]++
	mode, modeOK := st.modes[model]
	if status, pending := st.faultOnce[model]; pending && status != 0 {
		mode = fmt.Sprintf("status:%d", status)
		st.faultOnce[model] = 0 // consumed: one shot only
		modeOK = true
	}
	clsOut := st.classifierOut
	st.mu.Unlock()

	stream, _ := body["stream"].(bool)
	sys := stubSystemText(body)
	all := stubMessageTexts(body)

	// Classifier-shaped prompts get the pinned classification JSON.
	if clsOut != "" && (strings.Contains(sys, "intent classifier") || strings.Contains(sys, "multi-intent detector")) {
		st.replyText(w, body, clsOut, stream)
		return
	}
	// Planner-shaped prompts get a valid one-step plan so a planning
	// dispatch (RequiresPlanning turns) completes end to end.
	if strings.Contains(all, "task planner") && strings.Contains(all, "Decompose") {
		st.replyText(w, body, `{"steps":[{"description":"do the scripted step","tool_hint":"code","depends_on":[]}]}`, stream)
		return
	}

	if modeOK && mode != "ok" {
		if status, convErr := parseStatusMode(mode); convErr == nil {
			w.WriteHeader(status)
			mustWriteString(w, fmt.Sprintf(`{"error":{"message":"parity stub injected status","status":%d}}`, status))
			return
		}
	}

	// Everything else (chat agents, executor turns, reviewers) gets a
	// plain completion with real content.
	st.replyText(w, body, "stub ok", stream)
}

// replyText emits the completion envelope for content, SSE when the
// request asked to stream (mirroring the shared FakeLLM chunk shape).
func (st *parityStub) replyText(w http.ResponseWriter, body map[string]any, content string, stream bool) {
	model, _ := body["model"].(string)
	usage := map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		send := func(chunk map[string]any) {
			raw, err := json.Marshal(chunk)
			if err != nil {
				return
			}
			mustWriteString(w, "data: "+string(raw)+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		send(map[string]any{"id": "parity", "object": "chat.completion.chunk", "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil}}})
		send(map[string]any{"id": "parity", "object": "chat.completion.chunk", "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}, "finish_reason": nil}}})
		send(map[string]any{"id": "parity", "object": "chat.completion.chunk", "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}, "usage": usage})
		mustWriteString(w, "data: [DONE]\n\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	msg := map[string]any{"role": "assistant", "content": content}
	env := map[string]any{
		"id": "parity", "object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}},
		"usage":   usage,
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(env); err != nil {
		http.Error(w, "encode: "+err.Error(), http.StatusInternalServerError)
	}
}

func parseStatusMode(mode string) (int, error) {
	code, ok := strings.CutPrefix(mode, "status:")
	if !ok {
		return 0, fmt.Errorf("not a status mode")
	}
	n, err := strconv.Atoi(code)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// mustWriteString writes s to w; a failing client write in a test stub is
// a harness bug, so it panics rather than silently dropping bytes.
func mustWriteString(w http.ResponseWriter, s string) {
	if _, err := io.WriteString(w, s); err != nil {
		panic("parityStub: client write failed: " + err.Error())
	}
}

func stubMessageTexts(body map[string]any) string {
	var sb strings.Builder
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := msg["content"].(string); ok {
			sb.WriteString(c)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func stubSystemText(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "system" {
			if c, _ := msg["content"].(string); c != "" {
				return c
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// paritySandbox: scratch daemon + scripted providers + temp MEEPT_HOME.
// ---------------------------------------------------------------------------

// parityModel is one models.json5 per-model entry. extra carries raw
// config keys the router does not know about yet (quality blocks,
// data_posture) — they round-trip through the JSON5 loader untouched,
// which is exactly the pre-landing state the gate tests exercise.
type parityModel struct {
	name       string
	caps       []string
	inCost     float64
	outCost    float64
	ctxLimit   int // 0 = omit the context_limit key (unknown-limit posture)
	maxOutput  int
	extra      map[string]any
	priceGroup string
}

// parityProvider is one provider block bound to one parityStub.
type parityProvider struct {
	id     string
	stub   *parityStub
	apiKey string
	models []parityModel
}

// parityAlias is one model_aliases entry.
type parityAlias struct {
	name       string
	members    []string // "provider/model-id" in priority order
	timeout    int
	maxFails   int
	balanced   bool   // balanced_sticky_requests
	priceGroup string // price_group (raw key; inert pre-landing)
}

// parityConfig describes one sandbox world.
type parityConfig struct {
	providers []parityProvider
	aliases   []parityAlias
	// secondaryRef names the "provider/model" every auxiliary alias
	// (classifier/summarizer/small/coder/planner/analyst) resolves to.
	// Empty: the first provider's first model.
	secondaryRef string
	// chatAlias pins the chat agent's AGENT.md model: to this alias so
	// chat turns ride ResolveForAlias (the routing surface under test).
	// Empty: no pin.
	chatAlias string
	// budget, when non-nil, is merged under the "llm" config block
	// (llm.budget.* keys).
	budget map[string]any
}

type paritySandbox struct {
	t          *testing.T
	Work       string
	Home       string
	MeeptHome  string
	StateDir   string
	ProjectDir string
	SocketPath string
	HTTPAddr   string

	stubs []*parityStub

	cmd     *exec.Cmd
	logPath string
}

// newParitySandbox builds the world and boots the scratch daemon.
func newParitySandbox(t *testing.T, cfg parityConfig) *paritySandbox {
	t.Helper()

	// Short temp prefix: the Unix socket must stay under the macOS
	// 104-byte sun_path cap (do NOT EvalSymlinks the root).
	work, err := os.MkdirTemp("", "me2e-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	if os.Getenv("MEEPT_E2E_KEEP") == "" {
		t.Cleanup(func() {
			if rmErr := os.RemoveAll(work); rmErr != nil {
				t.Logf("sandbox cleanup: %v", rmErr)
			}
		})
	}
	s := &paritySandbox{
		t:          t,
		Work:       work,
		Home:       filepath.Join(work, "home"),
		StateDir:   filepath.Join(work, "state"),
		ProjectDir: filepath.Join(work, "project"),
		SocketPath: filepath.Join(work, "state", "meept.sock"),
		HTTPAddr:   harness.PortString(harness.FreePort(t)),
		logPath:    filepath.Join(work, "daemon.log"),
	}
	s.MeeptHome = filepath.Join(s.Home, ".meept")
	for _, d := range []string{s.Home, s.MeeptHome, s.StateDir, s.ProjectDir} {
		if mkErr := os.MkdirAll(d, 0o755); mkErr != nil {
			t.Fatalf("mkdir %s: %v", d, mkErr)
		}
	}
	for i := range cfg.providers {
		s.stubs = append(s.stubs, cfg.providers[i].stub)
	}

	s.writeDaemonConfig(cfg)
	s.writeModelsConfig(cfg)
	s.seedRoster(t)
	if cfg.chatAlias != "" {
		s.pinChatModelAlias(cfg.chatAlias)
	}
	s.boot(t)
	return s
}

func (s *paritySandbox) writeDaemonConfig(cfg parityConfig) {
	t := s.t
	resolvedWork := s.Work
	if resolved, err := filepath.EvalSymlinks(s.Work); err == nil {
		resolvedWork = resolved
	}
	daemon := fmt.Sprintf(`{
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
		filepath.ToSlash(filepath.Join(s.Work, "**")),
		filepath.ToSlash(filepath.Join(resolvedWork, "**")),
	)

	merged := daemon
	if cfg.budget != nil {
		// Strip the trailing brace, splice the llm.budget keys into the
		// existing llm block, re-close. Plain JSON is valid JSON5.
		budgetRaw, err := json.Marshal(cfg.budget)
		if err != nil {
			t.Fatalf("marshal budget overlay: %v", err)
		}
		splice := fmt.Sprintf(`
    "budget": %s,`, string(budgetRaw))
		marker := `
    "failure_policy": {
      "base_throttle": "1s",
      "pacing": { "enabled": false },
    },`
		if !strings.Contains(merged, marker) {
			t.Fatal("daemon config template lost its failure_policy marker")
		}
		merged = strings.Replace(merged, marker, marker+splice, 1)
	}

	if err := os.WriteFile(filepath.Join(s.MeeptHome, "meept.json5"), []byte(merged), 0o600); err != nil {
		t.Fatalf("write meept.json5: %v", err)
	}
}

func (s *paritySandbox) writeModelsConfig(cfg parityConfig) {
	t := s.t
	secondary := cfg.secondaryRef
	if secondary == "" {
		if len(cfg.providers) == 0 || len(cfg.providers[0].models) == 0 {
			t.Fatal("parity config needs at least one provider model")
		}
		secondary = cfg.providers[0].id + "/" + cfg.providers[0].models[0].name
	}

	providers := map[string]any{}
	for _, p := range cfg.providers {
		models := map[string]any{}
		for _, m := range p.models {
			entry := map[string]any{
				"name":         m.name,
				"capabilities": m.caps,
				"input_cost":   m.inCost,
				"output_cost":  m.outCost,
				"max_output":   m.maxOutput,
				"temperature":  0.7,
			}
			if m.ctxLimit > 0 {
				entry["context_limit"] = m.ctxLimit
			}
			for k, v := range m.extra {
				entry[k] = v
			}
			models[m.name] = entry
		}
		providers[p.id] = map[string]any{
			"api":     "openai",
			"options": map[string]any{"baseURL": p.stub.v1, "apiKey": p.apiKey},
			"models":  models,
		}
	}

	aliases := map[string]any{}
	for _, a := range cfg.aliases {
		entry := map[string]any{
			"models":    a.members,
			"timeout":   a.timeout,
			"max_fails": a.maxFails,
		}
		if a.balanced {
			entry["balanced_sticky_requests"] = true
		}
		if a.priceGroup != "" {
			entry["price_group"] = a.priceGroup
		}
		aliases[a.name] = entry
	}
	// The auxiliary aliases ride the secondary model (fast, and keeps
	// scenario per-provider counters chat-lane-only unless a test says
	// otherwise).
	aux := map[string]any{"models": []string{secondary}, "timeout": 10, "max_fails": 2}
	aliases["classifier"] = aux
	aliases["summarizer"] = aux
	aliases["small"] = aux
	aliases["coder"] = aux
	aliases["planner"] = aux
	aliases["analyst"] = aux

	doc := map[string]any{
		"model":              secondary,
		"small_model":        secondary,
		"classifier_model":   "classifier",
		"summarizer_model":   "summarizer",
		"default_timeout":    300,
		"disabled_providers": []string{},
		"model_aliases":      aliases,
		"providers":          providers,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal models.json5: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "models.json5"), raw, 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
}

// pinChatModelAlias adds `model: <alias>` to the chat agent's frontmatter
// so chat turns resolve through ResolveForAlias (session-affinity
// precedent — without the pin the loop holds the default model client and
// never writes routing decisions).
func (s *paritySandbox) pinChatModelAlias(alias string) {
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
		t.Fatal("chat AGENT.md has no id: chat line; cannot pin model alias")
	}
	text = strings.Replace(text, marker, marker+"model: "+alias+"\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write chat AGENT.md: %v", err)
	}
}

// seedRoster copies the repo agent roster + prompt templates into the
// sandboxed MEEPT_HOME (same seeding the shared harness does).
func (s *paritySandbox) seedRoster(t *testing.T) {
	t.Helper()
	_, thisFile, _, ok := runtimeCallerFile()
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

// runtimeCallerFile resolves this file's location for repo-root derived
// paths (harness.DaemonPath needs no path; the roster seeding does).
func runtimeCallerFile() (string, string, int, bool) {
	_, file, line, ok := runtime.Caller(1)
	return "", file, line, ok
}

func (s *paritySandbox) boot(t *testing.T) {
	t.Helper()
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
	t.Cleanup(func() { s.stopDaemon(t) })

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // scratch self-signed cert
			ForceAttemptHTTP2: false,
		},
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		resp, err := client.Get("https://" + s.HTTPAddr + "/health") //nolint:noctx // bounded poll in test harness
		if err == nil {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck // drain-only
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon not healthy within 90s; log tail:\n%s", s.daemonLog(t))
		}
		if sigErr := cmd.Process.Signal(syscall.Signal(0)); sigErr != nil {
			t.Fatalf("daemon died during boot; log tail:\n%s", s.daemonLog(t))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (s *paritySandbox) stopDaemon(t *testing.T) {
	t.Helper()
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	if sigErr := s.cmd.Process.Signal(syscall.SIGTERM); sigErr != nil {
		t.Logf("daemon SIGTERM: %v", sigErr)
	}
	done := make(chan struct{})
	go func() { _, _ = s.cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		if killErr := s.cmd.Process.Kill(); killErr != nil {
			t.Logf("daemon SIGKILL: %v", killErr)
		}
		<-done
	}
}

// ---------------------------------------------------------------------------
// CLI + HTTP + bus helpers.
// ---------------------------------------------------------------------------

func (s *paritySandbox) runCLI(t *testing.T, timeout time.Duration, args ...string) (string, string) {
	t.Helper()
	argv := append([]string{harness.CLIPath(t), "--socket", s.SocketPath, "-d", s.StateDir}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = s.Work
	cmd.Env = append(os.Environ(), "HOME="+s.Home, "MEEPT_HOME="+s.MeeptHome)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Logf("meept %s: %v (stdout %q stderr %q)", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

// createSession creates a session bound to the project dir and returns id.
func (s *paritySandbox) createSession(t *testing.T, name string) string {
	t.Helper()
	out, _ := s.runCLI(t, 30*time.Second, "--cwd", s.ProjectDir, "session", "create", name)
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Created session: "); ok {
			return strings.TrimSpace(after)
		}
	}
	t.Fatalf("session create returned no id; output:\n%s", out)
	return ""
}

// chatTurn submits one chat turn via the CLI and waits for the terminal
// reply. Empty replies fail the test (the CLI await path's contract).
func (s *paritySandbox) chatTurn(t *testing.T, sessionID, message string, timeout time.Duration) string {
	t.Helper()
	out, stderr := s.runCLI(t, timeout, "chat", "--session", sessionID, message)
	if strings.TrimSpace(out) == "" {
		t.Fatalf("chat turn returned empty reply\nstderr: %s\ndaemon log tail:\n%s", stderr, s.daemonLog(t))
	}
	return strings.TrimSpace(out)
}

// submitChat posts to /api/v1/chat/submit and returns the ack map.
func (s *paritySandbox) submitChat(t *testing.T, sessionID, message string) map[string]any {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"message":       message,
		"session_id":    sessionID,
		"source_client": "e2e-sovereign-parity",
	})
	if err != nil {
		t.Fatalf("marshal submit: %v", err)
	}
	client := s.insecureClient(t)
	resp, err := client.Post("https://"+s.HTTPAddr+"/api/v1/chat/submit", "application/json", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("chat submit: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read submit ack: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("chat submit status %d: %s", resp.StatusCode, body)
	}
	var ack map[string]any
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("submit ack decode: %v (%s)", err, body)
	}
	return ack
}

// busCall dispatches an RPC method over the HTTP /api/v1/bus/call bridge
// and returns status + raw body.
func (s *paritySandbox) busCall(t *testing.T, method string, params map[string]any) (int, []byte) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"method": method, "params": params})
	if err != nil {
		t.Fatalf("marshal bus call: %v", err)
	}
	client := s.insecureClient(t)
	resp, err := client.Post("https://"+s.HTTPAddr+"/api/v1/bus/call", "application/json", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("bus call %s: %v", method, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read bus call reply: %v", err)
	}
	return resp.StatusCode, body
}

func (s *paritySandbox) insecureClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // scratch self-signed cert
		},
	}
}

func (s *paritySandbox) daemonLog(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		return fmt.Sprintf("(no daemon log at %s: %v)", s.logPath, err)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// read-only DB readers (routing.db, metrics.db).
// ---------------------------------------------------------------------------

// routingRow is one routing_decisions row, normalized for assertions.
type routingRow struct {
	Alias      string
	Provider   string
	Model      string
	Reason     string
	Candidates string
}

func openReadOnlyDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping %s: %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// routingRowsForAlias returns the alias's routing_decisions rows in
// insertion order (rowid).
func (s *paritySandbox) routingRowsForAlias(t *testing.T, alias string) []routingRow {
	t.Helper()
	db := openReadOnlyDB(t, filepath.Join(s.StateDir, "routing.db"))
	rows, err := db.Query(`SELECT alias, COALESCE(chosen_provider_id,''), COALESCE(chosen_model_id,''), COALESCE(reason,''), COALESCE(candidates_json,'') FROM routing_decisions WHERE alias = ? ORDER BY rowid`, alias)
	if err != nil {
		t.Fatalf("query routing_decisions: %v", err)
	}
	defer rows.Close()
	var out []routingRow
	for rows.Next() {
		var r routingRow
		if err := rows.Scan(&r.Alias, &r.Provider, &r.Model, &r.Reason, &r.Candidates); err != nil {
			t.Fatalf("scan routing row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate routing rows: %v", err)
	}
	return out
}

// routingRowCount returns the total routing_decisions row count.
func (s *paritySandbox) routingRowCount(t *testing.T) int {
	t.Helper()
	db := openReadOnlyDB(t, filepath.Join(s.StateDir, "routing.db"))
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM routing_decisions`).Scan(&n); err != nil {
		t.Fatalf("count routing_decisions: %v", err)
	}
	return n
}

// llmCallsCount returns the llm_calls row count (0 when metrics.db does
// not exist yet).
func (s *paritySandbox) llmCallsCount(t *testing.T) int {
	t.Helper()
	path := filepath.Join(s.StateDir, "metrics.db")
	if _, err := os.Stat(path); err != nil {
		return 0
	}
	db := openReadOnlyDB(t, path)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM llm_calls`).Scan(&n); err != nil {
		t.Fatalf("count llm_calls: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------------
// persistent framed JSON-RPC session (bus subscribe/poll), per the
// async-turn suite's client; subscriptions die with the connection, so a
// test's subscribe+poll calls share one session.
// ---------------------------------------------------------------------------

type busSession struct {
	conn   net.Conn
	reader *bufio.Reader
}

func openBusSession(t *testing.T, s *paritySandbox) *busSession {
	t.Helper()
	conn, err := net.DialTimeout("unix", s.SocketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial unix %s: %v", s.SocketPath, err)
	}
	return &busSession{conn: conn, reader: bufio.NewReader(conn)}
}

func (b *busSession) Close() { b.conn.Close() }

// call performs one framed JSON-RPC request ("<len>\n<payload>").
func (b *busSession) call(method string, params any) (json.RawMessage, error) {
	req := map[string]any{"jsonrpc": "2.0", "id": time.Now().UnixNano(), "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := b.conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(b.conn, "%d\n", len(payload)); err != nil {
		return nil, fmt.Errorf("write frame length: %w", err)
	}
	if _, err := b.conn.Write(payload); err != nil {
		return nil, fmt.Errorf("write frame payload: %w", err)
	}
	lengthLine, err := b.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read frame length: %w", err)
	}
	length, err := strconv.Atoi(strings.TrimSpace(lengthLine))
	if err != nil {
		return nil, fmt.Errorf("invalid frame length: %w", err)
	}
	respData := make([]byte, length)
	if _, err := io.ReadFull(b.reader, respData); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", method, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s: [%d] %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// terminalEvent mirrors the frozen turn.terminal payload fields the gate
// scenarios assert on.
type terminalEvent struct {
	ConversationID string `json:"conversation_id"`
	SessionID      string `json:"session_id"`
	TurnID         string `json:"turn_id"`
	Status         string `json:"status"`
	Reply          string `json:"reply"`
	Error          string `json:"error"`
}

// waitTerminal polls the subscription for the turn's first terminal
// event, INCLUDING parked ones (unlike the async-turn await, the gate
// scenarios must observe parked terminals, not skip them).
func waitTerminal(t *testing.T, sess *busSession, subID, turnID string, timeout time.Duration) terminalEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := sess.call("bus.poll", map[string]string{"subscription_id": subID})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					if ev.Topic != "turn.terminal" {
						continue
					}
					var payload terminalEvent
					if json.Unmarshal(ev.Payload, &payload) != nil || payload.TurnID != turnID {
						continue
					}
					// RULING (orchestrator, 2026-11-17): "parked" is the
					// async-ack's accepted-not-finished state — the CLI's
					// own submitAndAwait (cmd/meept/chat_async.go) skips it
					// and keeps waiting for the task_completed_relay, which
					// re-broadcasts under the SAME turn id. The first
					// parked event is not terminal; returning it here made
					// T15 read the ack as the verdict. Mirror the CLI.
					if payload.Status == "parked" {
						continue
					}
					return payload
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no turn.terminal for %s within %s", turnID, timeout)
	return terminalEvent{}
}

// subscribeTopic opens a bus subscription and returns (session, subID).
// Caller must keep the session open for the subscription's lifetime and
// Close it at test end.
func subscribeTopic(t *testing.T, s *paritySandbox, topic string) (*busSession, string) {
	t.Helper()
	sess := openBusSession(t, s)
	subRaw, err := sess.call("bus.subscribe", map[string]any{"topics": []string{topic}})
	if err != nil {
		sess.Close()
		t.Fatalf("bus.subscribe %s: %v", topic, err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(subRaw, &sub); err != nil || sub.SubscriptionID == "" {
		sess.Close()
		t.Fatalf("bus.subscribe ack unparseable: %s (%v)", subRaw, err)
	}
	return sess, sub.SubscriptionID
}

// waitForBusEvent polls a subscription until payload[key] == want (or the
// deadline passes). Returns the matched payload map.
func waitForBusEvent(t *testing.T, sess *busSession, subID, key, want string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := sess.call("bus.poll", map[string]string{"subscription_id": subID})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					var payload map[string]any
					if json.Unmarshal(ev.Payload, &payload) != nil {
						continue
					}
					if v, _ := payload[key].(string); v == want {
						return payload
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no bus event with %s=%q within %s", key, want, timeout)
	return nil
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
