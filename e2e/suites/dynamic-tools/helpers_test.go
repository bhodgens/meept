//go:build e2e

// Package dynamictools is the hermetic RED gate for the as-needed
// tool-exposure design (branch 03 leaf 00D, docs/research/dynamic-tools-
// design.md; gate leaf 00T). It boots a real scratch daemon against a
// three-model fixture (one entry tagged `capabilities: ["frontier"]`, one
// untagged, one unknown/unclassified) — all served by a single scripted
// FakeLLM — and drives the capability-gate cases 1-4 (orchestrator
// Integration Test Plan) through the EXISTING per-request model-override
// seam (chat.submit "model" -> agent.applyRequestModel -> loop.ApplyRequest
// Model; design 00D §2.2 "mid-session switch" vehicle).
//
// Every assertion is black-box against frozen contract surfaces only:
//   - config keys ([agent.tools] tool_exposure / dynamic_tool_exposure),
//   - the models.json5 `capabilities` array (the pinned "frontier" tag),
//   - the FakeLLM-recorded tool payloads + request model field,
//   - pinned daemon-log phrases (design §5.2 / §4.3).
//
// None of the implementation symbols (llm.CapFrontier, EffectiveExposureMode,
// the warn-once downgrade, the predicted-budget payload, tool_search /
// tool_view promotion) exist yet, so the suite COMPILES against current code
// and fails RED for the missing implementation. No implementation code is
// written under internal/ by this leaf.
//
// This is the hand-rolled-sandbox pattern of the continuity-regressions and
// session-affinity suites: harness.Start owns exactly one 1-model models.
// json5 shape, but this gate needs a custom 3-model capability topology + a
// per-test [agent.tools] overlay, so it writes its own configs and boots the
// harness-built daemon binary itself.
package dynamictools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
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

// Pinned frozen-contract markers (design 00D §5.2 / §4.3). None is emitted by
// current code — their ABSENCE (or, for the warn, its exactly-once naming)
// is the RED this gate asserts.
const (
	// Design §5.2.1: the gate warns ONCE per active model, naming the model
	// and the downgrade. The marker phrase is pinned verbatim; the model name
	// is carried on the same log line (slog text handler: `... not
	// frontier-class; search downgraded to predicted model=<ref>`), so the
	// assertion matches the phrase AND the model name INDEPENDENTLY per line.
	warnNotFrontierPhrase = "not frontier-class; search downgraded to predicted"

	// Design §4.3: a promotion or a model switch re-caches the prefix exactly
	// once; the loop emits this DEBUG line (internal/agent/loop.go:4516).
	// The cache-continuity case asserts its ABSENCE across same-episode turns.
	prefixCacheInvalidatedLine = "prefix cache invalidated"
)

// sandbox is the scratch world. Unlike harness.Start (one provider, one boot)
// this suite needs custom models.json5 shapes (the three-tier capability
// fixture) + a per-test [agent.tools] overlay, so it writes its own configs
// and boots the daemon itself — the daemon-lifecycle / continuity-regressions
// pattern.
type sandbox struct {
	t *testing.T

	Work       string
	Home       string
	MeeptHome  string
	StateDir   string
	ProjectDir string
	AgentTools string // raw JSON5 [agent.tools] block ("" = legacy default)
	DefaultMod string // default executor model ref (fake/<model-id>)

	Fake    *harness.FakeLLM
	cmd     *exec.Cmd
	logPath string
}

// sandboxOption customizes the config/model templates before boot.
type sandboxOption func(*sandboxConfig)

type sandboxConfig struct {
	agentTools string // raw [agent.tools] JSON5 block appended to meept.json5
	defaultMod string
}

// withAgentTools overlays an [agent.tools] block. Unknown keys are dropped by
// the non-strict JSON5 loader today, so the boot stays clean and the RED is
// behavioral (the gate / payload / warn never fire), not a config failure.
func withAgentTools(block string) sandboxOption {
	return func(sc *sandboxConfig) { sc.agentTools = block }
}

// withDefaultModel sets the default executor model ref.
func withDefaultModel(ref string) sandboxOption {
	return func(sc *sandboxConfig) { sc.defaultMod = ref }
}

// newSandbox builds the three-model world and boots the daemon.
//
// Capability fixture (00D §5.1):
//   - "fake/frontier-flagship": capabilities carry the "frontier" tag
//   - "fake/local-mini":        untagged small model (completion only)
//   - "fake/mystery-lab":       unknown / unclassified (no capabilities)
func newSandbox(t *testing.T, opts ...sandboxOption) *sandbox {
	t.Helper()
	sc := &sandboxConfig{defaultMod: "fake/frontier-flagship"}
	for _, opt := range opts {
		opt(sc)
	}

	// Short temp prefix: the Unix socket must stay under the macOS 104-byte
	// sun_path cap (harness lesson; do NOT EvalSymlinks the root).
	work, err := os.MkdirTemp("", "mdt-*")
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
		AgentTools: sc.agentTools,
		DefaultMod: sc.defaultMod,
		logPath:    filepath.Join(work, "daemon.log"),
	}
	s.MeeptHome = filepath.Join(s.Home, ".meept")
	for _, d := range []string{s.Home, s.MeeptHome, s.StateDir, s.ProjectDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	s.Fake = harness.NewFakeLLM()
	t.Cleanup(s.Fake.Close)
	fakeV1 := s.Fake.URL() + "/v1"

	// The fence matches against RESOLVED paths (/private/var on macOS), so
	// allowed_paths carries both spellings of the work root.
	resolvedWork := work
	if resolved, err := filepath.EvalSymlinks(work); err == nil {
		resolvedWork = resolved
	}

	toolsBlock := ""
	if s.AgentTools != "" {
		toolsBlock = fmt.Sprintf(`  "agent": { "tools": %s },`, s.AgentTools)
	}

	cfg := fmt.Sprintf(`{
  // dynamic-tools scratch config (hand-written; same shape as the harness).
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
      "addr": "127.0.0.1:0",
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
%s
}`,
		s.socketPath(),
		filepath.Join(s.StateDir, "meept.pid"),
		s.StateDir,
		s.socketPath(),
		filepath.Join(s.StateDir, "tls", "cert.pem"),
		filepath.Join(s.StateDir, "tls", "key.pem"),
		filepath.Join(s.StateDir, "memory"),
		filepath.Join(s.StateDir, "projects"),
		filepath.Join(s.StateDir, "audit.db"),
		filepath.ToSlash(filepath.Join(work, "**")),
		filepath.ToSlash(filepath.Join(resolvedWork, "**")),
		toolsBlock,
	)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "meept.json5"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("write meept.json5: %v", err)
	}

	// Three-tier capability fixture. The "frontier" tag is an UNKNOWN
	// capability string today (internal/llm/providers.go modelConfigFrom just
	// maps it into Capabilities with no allow-list validation), so it boots
	// cleanly while remaining INERT — exactly the RED this gate pins: the tag
	// is present in the config but no gate reads it yet.
	aliasModels := []string{"fake/frontier-flagship", "fake/local-mini", "fake/mystery-lab"}
	aliasJSON := aliasArray(aliasModels)
	providers := fmt.Sprintf(`{
    "fake": {
      "api": "openai",
      "options": { "baseURL": %q, "noAuth": true },
      "models": {
        "frontier-flagship": {
          "name": "frontier-flagship",
          "capabilities": ["completion", "code", "reasoning", "tool_use", "frontier"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 200000,
          "max_output": 8192,
          "temperature": 0.7
        },
        "local-mini": {
          "name": "local-mini",
          "capabilities": ["completion"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 32000,
          "max_output": 4096,
          "temperature": 0.2
        },
        "mystery-lab": {
          "name": "mystery-lab",
          "capabilities": [],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 32000,
          "max_output": 4096,
          "temperature": 0.5
        }
      }
    }
  }`, fakeV1)

	models := fmt.Sprintf(`{
  "model": %q,
  "small_model": "fake/local-mini",
  "classifier_model": "classifier",
  "summarizer_model": "summarizer",
  "extract_model": "",
  "refusal_model": "",
  "default_timeout": 300,
  "disabled_providers": [],
  "model_aliases": {
    "classifier":  { "models": %s, "timeout": 10, "max_fails": 2 },
    "summarizer":  { "models": %s, "timeout": 10, "max_fails": 2 },
    "small":       { "models": %s, "timeout": 10, "max_fails": 2 },
    "coder":       { "models": %s, "timeout": 60, "max_fails": 3 },
    "planner":     { "models": %s, "timeout": 60, "max_fails": 3 },
    "analyst":     { "models": %s, "timeout": 60, "max_fails": 3 }
  },
  "providers": %s
}`, s.DefaultMod, aliasJSON, aliasJSON, aliasJSON, aliasJSON, aliasJSON, aliasJSON, providers)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "models.json5"), []byte(models), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}

	s.seedRoster()
	s.boot()
	return s
}

func (s *sandbox) socketPath() string { return filepath.Join(s.StateDir, "meept.sock") }

func aliasArray(models []string) string {
	parts := make([]string, len(models))
	for i, m := range models {
		parts[i] = fmt.Sprintf("%q", m)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// seedRoster copies the repo's agent roster + prompt templates into the
// sandboxed MEEPT_HOME (same seeding the harness does, so coder specialists
// register and step jobs run with tools). The repo root resolves from this
// file's own location (e2e/suites/dynamic-tools/).
func (s *sandbox) seedRoster() {
	t := s.t
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("seedRoster: cannot resolve this file's path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	for _, pair := range []struct{ src, dst string }{
		{filepath.Join(repoRoot, "config", "agents"), filepath.Join(s.MeeptHome, "agents")},
		{filepath.Join(repoRoot, "config", "prompts"), filepath.Join(s.MeeptHome, "prompts")},
	} {
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

// boot starts the daemon and waits for the RPC socket to accept connections.
// Readiness is probed with a plain net.Dial (a not-yet-listening socket errors
// cleanly), NOT the harness RPC client whose Call* helpers t.Fatalf on a
// transport error while the daemon is still coming up.
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
		"-s", s.socketPath(),
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

	deadline := time.Now().Add(60 * time.Second)
	for {
		conn, err := net.Dial("unix", s.socketPath())
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon socket not reachable within 60s; log tail:\n%s", s.logTail())
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

// logLinesContaining returns the daemon-log lines containing phrase.
func (s *sandbox) logLinesContaining(phrase string) []string {
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		return nil
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.Contains(line, phrase) {
			out = append(out, line)
		}
	}
	return out
}

// logLineCount is the number of log lines containing phrase.
func (s *sandbox) logLineCount(phrase string) int {
	return len(s.logLinesContaining(phrase))
}

// warnNamesModel reports whether the gate's pinned downgrade marker line is
// present exactly once AND independently names the given model fragment
// (i.e. the warn named this model, not some other). Absent / re-logged /
// unnamed -> false (the RED / flap-regression signals).
func (s *sandbox) warnNamesModel(modelFragment string) bool {
	lines := s.logLinesContaining(warnNotFrontierPhrase)
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		if !strings.Contains(line, modelFragment) {
			return false // the marker fired but for a different model
		}
	}
	return true
}

// env is the CLI daemon-env for a sandboxed invocation.
func (s *sandbox) env() []string {
	return append(os.Environ(), "HOME="+s.Home, "MEEPT_HOME="+s.MeeptHome)
}

// createSession makes a named session bound to the project dir.
func (s *sandbox) createSession(name string) string {
	t := s.t
	prefixed := []string{"--socket", s.socketPath(), "-d", s.StateDir, "--cwd", s.ProjectDir, "session", "create", name}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, harness.CLIPath(t), prefixed...)
	cmd.Dir = s.Work
	cmd.Env = s.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("session create failed: %v\nstdout: %s\nstderr: %s\nlog tail:\n%s",
			err, stdout.String(), stderr.String(), s.logTail())
	}
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Created session: "); ok {
			return strings.TrimSpace(after)
		}
	}
	t.Fatalf("session create returned no id; output:\n%s", stdout.String())
	return ""
}

// submitTurn drives ONE tool-bearing turn through the existing per-request
// model-override seam (the design §2.2 "mid-session switch" vehicle): RPC
// chat.submit with an optional "model" ref, with the intent pinned to the
// tooluse lane and a scripted file_write so an executor request carrying a
// tools array is guaranteed. It does NOT wait for the turn — the caller
// awaits the specific request via awaitNewToolRequest (pass the RequestCount
// snapshot taken just before this call so multi-turn sequences are unambiguous
// even when two turns ride the same model). modelRef is a models.json5 ref
// ("fake/local-mini") or "" for the default model. It returns the turn_id the
// ack minted.
func (s *sandbox) submitTurn(sessionID, message, modelRef string) string {
	t := s.t
	s.Fake.SetClassifierOutput(`{"intent":"tooluse","confidence":0.95,"reasoning":"pinned tooluse (dynamic-tools gate)"}`)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "file_write",
		Arguments: `{"path":"` + filepath.Join(s.ProjectDir, "dynamic-tools-gate.txt") + `","content":"gate probe","direct":true}`,
	})

	client := harness.DialRPC(t, s.socketPath()) // conn closed by DialRPC's t.Cleanup

	params := map[string]any{
		"message":    message,
		"session_id": sessionID,
	}
	if modelRef != "" {
		params["model"] = modelRef
	}
	ack := client.CallResult("chat.submit", params)
	if acc, _ := ack["accepted"].(bool); !acc {
		t.Fatalf("chat.submit not accepted: %v\nlog tail:\n%s", ack, s.logTail())
	}
	turnID, _ := ack["turn_id"].(string)
	return turnID
}

// awaitNewToolRequest polls the FakeLLM until a tool-bearing executor request
// at index >= beforeCount whose model field contains frag has been recorded,
// and returns it. beforeCount is the s.Fake.RequestCount() snapshot taken just
// before the matching submitTurn — that makes the await unambiguous across a
// multi-turn episode (two turns may ride the same model id).
func (s *sandbox) awaitNewToolRequest(frag string, beforeCount int, timeout time.Duration) map[string]any {
	t := s.t
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		reqs := s.Fake.Requests()
		if len(reqs) > beforeCount {
			for i := len(reqs) - 1; i >= beforeCount; i-- {
				body := reqs[i]
				if !hasTools(body) {
					continue
				}
				model, _ := body["model"].(string)
				if frag != "" && !strings.Contains(model, frag) {
					continue
				}
				return body
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("executor turn for model fragment %q never carried a tools payload (index>=%d) within %s\nlog tail:\n%s",
		frag, beforeCount, timeout, s.logTail())
	return nil
}

// hasTools reports whether a recorded OpenAI request body carries a non-empty
// tools array (an executor-lane request; mirrors the FakeLLM's own shape probe
// used for ToolCallCount).
func hasTools(body map[string]any) bool {
	raw, ok := body["tools"].([]any)
	if !ok || raw == nil {
		return false
	}
	return len(raw) > 0
}

// toolNamesFromBody extracts the offered tool names from one recorded request
// (the black-box observation point for the capability gate's payload shape).
func toolNamesFromBody(body map[string]any) []string {
	raw, _ := body["tools"].([]any)
	var out []string
	seen := map[string]bool{}
	for _, rt := range raw {
		tool, ok := rt.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// payloadEqual reports whether two recorded requests offer IDENTICAL tool
// payloads (name+description+parameters, canonical-serialized across the
// whole tools array). Used by the flag-off byte-identical case.
func payloadEqual(a, b map[string]any) bool {
	ra, _ := a["tools"].([]any)
	rb, _ := b["tools"].([]any)
	if len(ra) != len(rb) {
		return false
	}
	ja, _ := json.Marshal(ra)
	jb, _ := json.Marshal(rb)
	return string(ja) == string(jb)
}
