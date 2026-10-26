//go:build e2e

// Suite runtime-hang: end-to-end proof of the llama-hang-watchdog tree
// (matrix row F10). A fake llama-server keeps /health answering 200 while
// /v1/chat/completions is wedged; the daemon's HealthChecker canary lane
// must reach VerdictHung within the configured threshold, publish
// runtime.health verdict=hung on the bus (Contract 2, via the daemon's
// SetHealthEventPublisher wiring in components.go), drive the existing
// auto-restart path (supervisor kill → respawn of the same
// spawn_command), and publish verdict=healthy once the respawned server
// answers completions again.
//
// Hermetic: no network, no real model. Fast cadence (1s health interval,
// 1s canary interval/timeout, threshold 2) keeps the whole suite well
// under 60s.
//
// Recovery control: the daemon's restart re-runs the SAME spawn_command,
// so the wedge is controlled by a FILE the fakeserver consults per request
// (--wedge-file), not by a per-process flag lifetime. The test deletes the
// wedge file after observing the hung event; the respawned generation then
// answers completions and the recovery event fires.
package runtimehang

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// Assertion budgets. Cadence: health tick 1s, canary every tick once
// armed, threshold 2, canary timeout 1s → HUNG lands ~4-6s after spawn;
// restart is immediate; recovery lands ~3-5s after the wedge is lifted.
// Budgets are generous multiples; the whole test still finishes < 60s.
const (
	hungWait     = 30 * time.Second
	restartWait  = 20 * time.Second
	recoveryWait = 25 * time.Second
	bootWait     = 60 * time.Second
)

// repoRoot resolves the module root from this test file's directory
// (e2e/suites/runtime-hang → root), so `go build` runs with the module.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve this file's path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// buildFakeServer compiles e2e/suites/runtime-hang/fakeserver into the
// sandbox and returns the binary path.
func buildFakeServer(t *testing.T, destDir string) string {
	t.Helper()
	bin := filepath.Join(destDir, "fakeserver")
	cmd := exec.Command("go", "build", "-o", bin, "./e2e/suites/runtime-hang/fakeserver")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeserver: %v\n%s", err, out)
	}
	return bin
}

// sandbox is the daemon-lifecycle scratch world with a lifecycle-managed
// llama-cpp provider whose spawn_command is the fake wedged server.
type sandbox struct {
	t          *testing.T
	Work       string
	Home       string
	MeeptHome  string
	StateDir   string
	SocketPath string
	HTTPAddr   string
	Port       int

	FakeBin     string
	WedgeFile   string // completions wedge while this exists
	FakePIDFile string // fakeserver rewrites its pid on every generation
	RuntimePID  string // lifecycle pid_file (manager-owned)

	expectedEndpointKey string

	cmd     *exec.Cmd
	logPath string
}

func newSandbox(t *testing.T, httpPort, llamaPort int) *sandbox {
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
		SocketPath: filepath.Join(work, "state", "meept.sock"),
		HTTPAddr:   harness.PortString(httpPort),
		Port:       llamaPort,

		WedgeFile:   filepath.Join(work, "wedge.flag"),
		FakePIDFile: filepath.Join(work, "fakeserver.pid"),
		RuntimePID:  filepath.Join(work, "state", "llama.pid"),
		logPath:     filepath.Join(work, "daemon.log"),

		expectedEndpointKey: fmt.Sprintf("llama-cpp:127.0.0.1:%d", llamaPort),
	}
	s.MeeptHome = filepath.Join(s.Home, ".meept")
	for _, d := range []string{s.Home, s.MeeptHome, s.StateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	s.FakeBin = buildFakeServer(t, work)

	// Stray-fakeserver sweep: registered BEFORE the daemon-stop cleanup
	// below and the one boot() registers, so it runs LAST (t.Cleanup is
	// LIFO) — after the daemon had its chance to stop its children.
	t.Cleanup(s.killStrayFakes)

	// Arm the wedge BEFORE anything spawns: generation 1 must serve
	// /health but block completions from the first request.
	if err := os.WriteFile(s.WedgeFile, []byte("wedge"), 0o644); err != nil {
		t.Fatalf("write wedge file: %v", err)
	}

	s.writeConfigs(t)
	return s
}

// killStrayFakes is the belt-and-braces leak guard: any fakeserver from
// THIS sandbox's unique binary path still alive after teardown is killed.
func (s *sandbox) killStrayFakes() {
	out, err := exec.Command("pgrep", "-f", s.FakeBin).Output()
	if err != nil || len(out) == 0 {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		pid, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr != nil || pid <= 0 {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func (s *sandbox) writeConfigs(t *testing.T) {
	t.Helper()
	cfg := fmt.Sprintf(`{
  // runtime-hang scratch config (daemon-lifecycle suite pattern).
  "daemon": {
    "socket_path": %q,
    "pid_file": %q,
    "data_dir": %q,
    "log_level": "INFO"
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
      "mcp": false
    }
  },
  "memory": { "data_dir": %q },
  "projects": {
    "enabled": true,
    "base_dir": %q,
    "auto_detect": false,
    "fence_enabled": false
  },
  "security": {
    "audit_db_path": %q,
    "allowed_paths": [%q]
  },
  "multiagent": { "enabled": true }
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

	// models.json5: the general lane rides an OpenAI-compatible FakeLLM
	// (hermetic boot, classifier gate satisfied); the llama provider
	// carries the lifecycle block that spawns the fake wedged server with
	// the fast canary cadence. The chat-llama alias puts local-llama/stub
	// in the in-use set so StartAll does not skip the endpoint.
	fake := harness.NewFakeLLM()
	t.Cleanup(fake.Close)
	modelFile := filepath.Join(s.Work, "fake-model.gguf")
	if err := os.WriteFile(modelFile, []byte("gguf-placeholder"), 0o644); err != nil {
		t.Fatalf("write placeholder model: %v", err)
	}
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
        "auto_start": true,
        "pid_file": %q,
        "spawn_command": [%q, %q, "--wedge-completions", %q, %q],
        "spawn_timeout_seconds": 15,
        "health_check": {
          "endpoint": "/health",
          "interval_seconds": 1,
          "timeout_seconds": 1,
          "unhealthy_threshold": 30,
          "canary": {
            "enabled": true,
            "interval_seconds": 1,
            "timeout_seconds": 1,
            "threshold": 2
          }
        },
        "restart_policy": {
          "enabled": true,
          "max_attempts": 5,
          "cooldown_seconds": 1,
          "reset_after_seconds": 300
        }
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
		fmt.Sprintf("http://127.0.0.1:%d/v1", s.Port),
		modelFile,
		s.RuntimePID,
		s.FakeBin,
		"--port="+strconv.Itoa(s.Port),
		"--wedge-file="+s.WedgeFile,
		"--self-pid-file="+s.FakePIDFile,
	)
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "models.json5"), []byte(models), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}

	s.seedRoster()
}

// seedRoster copies the repo's agent roster + prompt templates into the
// sandboxed MEEPT_HOME (same seeding the harness does).
func (s *sandbox) seedRoster() {
	t := s.t
	root := repoRoot(t)
	pairs := []struct{ src, dst string }{
		{filepath.Join(root, "config", "agents"), filepath.Join(s.MeeptHome, "agents")},
		{filepath.Join(root, "config", "prompts"), filepath.Join(s.MeeptHome, "prompts")},
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

// start launches the daemon process WITHOUT waiting for health — the
// caller subscribes to the bus first (the RPC listener comes up in
// registry.StartAll, before both the HTTP listener and the runtime
// StartAll goroutine, so a socket-ready subscribe precedes any possible
// runtime.health event).
func (s *sandbox) start() {
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
	case <-time.After(30 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
}

// waitSocket blocks until the RPC unix socket accepts a connection.
func (s *sandbox) waitSocket() {
	s.t.Helper()
	deadline := time.Now().Add(bootWait)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", s.SocketPath)
		if err == nil {
			_ = conn.Close()
			return
		}
		if !s.alive() {
			s.t.Fatalf("daemon died before its RPC socket came up; log tail:\n%s", s.logTail())
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.t.Fatalf("RPC socket %s never came up; log tail:\n%s", s.SocketPath, s.logTail())
}

func (s *sandbox) waitHealthy() {
	s.t.Helper()
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // scratch self-signed cert
			ForceAttemptHTTP2: false,
		},
	}
	deadline := time.Now().Add(bootWait)
	for time.Now().Before(deadline) {
		resp, err := client.Get("https://" + s.HTTPAddr + "/health") //nolint:noctx // bounded poll
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if !s.alive() {
			s.t.Fatalf("daemon died during boot; log tail:\n%s", s.logTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
	s.t.Fatalf("daemon not healthy within %s; log tail:\n%s", bootWait, s.logTail())
}

func (s *sandbox) alive() bool {
	return s.cmd != nil && s.cmd.Process != nil && s.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (s *sandbox) logTail() string {
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		return fmt.Sprintf("(no log: %v)", err)
	}
	if len(data) > 8192 {
		data = data[len(data)-8192:]
	}
	return string(data)
}

func (s *sandbox) logContains(needle string) bool {
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), needle)
}

// fakePID reads the fakeserver's self-reported pid (0 when absent/unparsable).
func (s *sandbox) fakePID() int {
	data, err := os.ReadFile(s.FakePIDFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// healthEvent is one runtime.health bus payload (Contract 2 keys).
type healthEvent struct {
	EndpointKey string `json:"endpoint_key"`
	Verdict     string `json:"verdict"`
	Model       string `json:"model"`
	Detail      string `json:"detail"`
	Ts          int64  `json:"ts"`
}

// pollHealthEvents drains the bus subscription buffer and returns every
// runtime.health payload seen so far (the subscription keeps the last 100
// events server-side, so repeated polls re-see old ones — callers filter).
func pollHealthEvents(t *testing.T, rpc *harness.RPCClient, subID string) []healthEvent {
	t.Helper()
	res := rpc.CallResult("bus.poll", map[string]any{"subscription_id": subID})
	rawEvents, _ := res["events"].([]any)
	out := make([]healthEvent, 0, len(rawEvents))
	for _, e := range rawEvents {
		rec, ok := e.(map[string]any)
		if !ok || rec["topic"] != "runtime.health" {
			continue
		}
		payload, ok := rec["payload"].(map[string]any)
		if !ok {
			continue
		}
		blob, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		var ev healthEvent
		if err := json.Unmarshal(blob, &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// waitHealthEvent polls until an event with the given verdict appears.
func waitHealthEvent(t *testing.T, rpc *harness.RPCClient, subID, verdict string, budget time.Duration) healthEvent {
	t.Helper()
	deadline := time.Now().Add(budget)
	var last []healthEvent
	for time.Now().Before(deadline) {
		last = pollHealthEvents(t, rpc, subID)
		for _, ev := range last {
			if ev.Verdict == verdict {
				return ev
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no runtime.health verdict=%s event within %s; seen: %+v", verdict, budget, last)
	return healthEvent{}
}

// TestRuntimeHang_DetectRestartRecover is the F10 gate: wedged-completion
// fake server → verdict=hung event → runtime restarted (new fakeserver
// pid) → verdict=healthy recovery event.
func TestRuntimeHang_DetectRestartRecover(t *testing.T) {
	// TWO distinct free ports: the daemon's HTTP listener and the fake
	// llama-server endpoint. Sharing one made the daemon's own listener
	// trip the duplicate-spawn guard ("endpoint already has a listener").
	httpPort := harness.FreePort(t)
	llamaPort := harness.FreePort(t)
	for llamaPort == httpPort {
		llamaPort = harness.FreePort(t)
	}
	s := newSandbox(t, httpPort, llamaPort)
	s.start()

	// Subscribe to runtime.health BEFORE the daemon is fully up: the RPC
	// listener precedes the HTTP listener and the runtime StartAll
	// goroutine in daemon.Start, so no hung event can be published before
	// this subscription is live.
	s.waitSocket()
	rpc := harness.DialRPC(t, s.SocketPath)
	sub := rpc.CallResult("bus.subscribe", map[string]any{
		"topics": []string{"runtime.health"},
	})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}
	defer rpc.Call("bus.unsubscribe", map[string]any{"subscription_id": subID})

	s.waitHealthy()

	// Generation 1 must be up (spawned by StartAll, /health answering).
	deadline := time.Now().Add(restartWait)
	pid1 := 0
	for pid1 == 0 {
		pid1 = s.fakePID()
		if pid1 != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake server never spawned; log tail:\n%s", s.logTail())
		}
		time.Sleep(100 * time.Millisecond)
	}

	// (1) HUNG: canary threshold reached while /health keeps answering.
	hung := waitHealthEvent(t, rpc, subID, "hung", hungWait)
	if hung.EndpointKey != s.expectedEndpointKey {
		t.Fatalf("hung event endpoint_key = %q, want %q", hung.EndpointKey, s.expectedEndpointKey)
	}
	if hung.Detail == "" {
		t.Fatal("hung event carries an empty detail; operators need the canary failure reason")
	}
	if hung.Ts == 0 {
		t.Fatal("hung event ts is zero")
	}
	if !s.logContains("Runtime hung") {
		t.Fatalf("daemon log has no 'Runtime hung' ERROR line; log tail:\n%s", s.logTail())
	}

	// Lift the wedge so the RESPAWNED generation answers completions.
	// (The restart itself was already triggered by the bool callback on
	// the HUNG edge and does not depend on this.)
	if err := os.Remove(s.WedgeFile); err != nil {
		t.Fatalf("remove wedge file: %v", err)
	}

	// (2) RESTART: the supervisor killed generation 1 and respawned the
	// same spawn_command — proven by a new fakeserver pid.
	deadline = time.Now().Add(restartWait)
	pid2 := 0
	for {
		if pid := s.fakePID(); pid != 0 && pid != pid1 {
			pid2 = pid
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake server was not restarted within %s (pid still %d); log tail:\n%s",
				restartWait, pid1, s.logTail())
		}
		if !s.alive() {
			t.Fatalf("daemon died waiting for the restart; log tail:\n%s", s.logTail())
		}
		time.Sleep(200 * time.Millisecond)
	}
	// "Auto-restart succeeded" lands only after the respawn's health wait
	// completes (the pid change is observed mid-restart), so poll for it.
	logDeadline := time.Now().Add(restartWait)
	for !s.logContains("Auto-restart succeeded") {
		if time.Now().After(logDeadline) {
			t.Fatalf("daemon log never showed 'Auto-restart succeeded'; log tail:\n%s", s.logTail())
		}
		time.Sleep(200 * time.Millisecond)
	}

	// (3) RECOVERY: the respawned server answers completions, the canary
	// succeeds, and the manager publishes verdict=healthy for the endpoint
	// it had marked hung.
	healthy := waitHealthEvent(t, rpc, subID, "healthy", recoveryWait)
	if healthy.EndpointKey != s.expectedEndpointKey {
		t.Fatalf("healthy event endpoint_key = %q, want %q", healthy.EndpointKey, s.expectedEndpointKey)
	}
	if healthy.Ts < hung.Ts {
		t.Fatalf("recovery ts %d precedes hung ts %d", healthy.Ts, hung.Ts)
	}

	t.Logf("runtime-hang proved: hung(%+v) → restart pid %d→%d → healthy(%+v)", hung, pid1, pid2, healthy)
}
