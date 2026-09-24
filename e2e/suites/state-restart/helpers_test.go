//go:build e2e

// Shared helpers for the state-restart suite: raw RPC over the sandbox Unix
// socket (length-prefixed JSON-RPC framing) and a restart primitive that
// stops the harness daemon and re-boots a fresh process against the SAME
// sandbox home. The harness intentionally has no exported Stop/Restart API,
// so this suite spawns the built daemon binary itself (same flags/env as
// harness.Daemon.boot).
package staterestart

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

// stopDaemon sends SIGTERM to the process and waits for exit (SIGKILL after
// 10s). Safe to call on an already-exited process.
func stopDaemon(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _, _ = proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = proc.Kill()
		<-done
	}
	// Clean slate for the next boot even if shutdown skipped socket removal.
	_ = os.Remove(socketPathOf(t))
}

// socketPathOf resolves the sandbox socket path from the meept.json5 the
// harness wrote under the sandbox home.
func socketPathOf(t *testing.T) string {
	t.Helper()
	home := os.Getenv("MEEPT_E2E_RESTART_HOME")
	if home == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".meept", "meept.json5"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"socket_path"`) {
			var val string
			if _, err := fmt.Sscanf(line, `"socket_path": %q,`, &val); err == nil && val != "" {
				return val
			}
		}
	}
	return ""
}

// bootDaemon spawns a fresh scratch daemon against the stack's sandbox and
// waits for /health. Same flags and env as harness.Daemon.boot.
func bootDaemon(t *testing.T, s *harness.Stack) {
	t.Helper()

	bin := harness.DaemonPath(t)
	logPath := filepath.Join(s.Work, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open daemon log: %v", err)
	}
	defer logFile.Close()

	cmd := exec.Command(bin,
		"-c", filepath.Join(s.MeeptHome, "meept.json5"),
		"-d", s.StateDir,
		"-s", s.SocketPath,
	)
	cmd.Dir = s.Work
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = s.Env()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			ForceAttemptHTTP2: false,
		},
	}
	healthURL := s.HTTPBaseURL() + "/health"
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := client.Get(healthURL) //nolint:noctx // bounded poll in test harness
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				// Reap the exited boot process bookkeeping via a detached
				// wait goroutine when the test finishes; the process is
				// stopped by restartDaemon/deferred cleanup.
				return
			}
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("re-booted daemon not healthy within 60s; log tail:\n%s", s.Daemon.LogTail())
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("re-booted daemon died during boot; log tail:\n%s", s.Daemon.LogTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// restartDaemon stops the harness-booted daemon and re-boots a fresh
// daemon process against the SAME sandbox home. The returned func stops the
// re-booted daemon; the harness cleanup remains a safe no-op afterwards
// (SIGTERM on an exited pid, guarded by its own sync.Once).
func restartDaemon(t *testing.T, s *harness.Stack) func() {
	t.Helper()

	oldPID := s.Daemon.Pid()
	if oldPID == 0 {
		t.Fatal("harness daemon pid not recorded; nothing to restart")
	}
	// Strip the harness's own cleanup for this daemon? No — its stop() is
	// idempotent (SIGTERM on exited pid fails harmlessly) and releasing its
	// daemon slot at cleanup is exactly what we want.
	stopDaemon(t, oldPID)

	// Wait for the old process to be fully reaped before re-binding the
	// socket path.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(s.SocketPath); os.IsNotExist(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	bootDaemon(t, s)

	return func() {
		// The re-booted daemon has no harness-managed cmd; find and stop it
		// via the pidfile the daemon wrote at boot.
		data, err := os.ReadFile(filepath.Join(s.StateDir, "meept.pid"))
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil {
				stopDaemon(t, pid)
			}
		}
	}
}

// sentinelProbe drives one chat turn on the UNBOUND session after the
// restart, submitting with the session's PERSISTED conversation_id (the
// dual-lookup path), and returns the daemon log tail. On an unbound session
// the turn-start working-directory resolution must surface the actionable
// sentinel — the loud "chat turn has no working directory bound" warning
// with the bind-a-project hint — never a silent fallback to the daemon's
// own CWD.
func sentinelProbe(t *testing.T, s *harness.Stack, sessionID string) string {
	t.Helper()

	got := sessionGetRPC(t, s, sessionID)
	convID, _ := got["conversation_id"].(string)
	if convID == "" {
		t.Fatalf("session %s has no persisted conversation_id", sessionID)
	}

	payload, err := json.Marshal(map[string]any{
		"message":         "say hi",
		"session_id":      sessionID,
		"conversation_id": convID,
		"source_client":   "e2e-harness",
	})
	if err != nil {
		t.Fatalf("marshal submit: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
	}}
	resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/chat/submit", "application/json", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("sentinel probe submit: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sentinel probe submit status %d: %s", resp.StatusCode, body)
	}

	// Poll the daemon log for the turn's dispatch to settle, then return
	// the log tail for the caller's sentinel assertion.
	deadline := time.Now().Add(30 * time.Second)
	logPath := filepath.Join(s.Work, "daemon.log")
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), "Released per-task agent loops") {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return tailFile(logPath, 20000)
}

// tail returns the last n bytes of s.
func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// tailFile returns the last n bytes of the file at path.
func tailFile(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("(no log at %s: %v)", path, err)
	}
	return tail(string(data), n)
}

// rpcCall sends one JSON-RPC request over the sandbox Unix socket using the
// daemon's length-prefixed framing (<length>\n<payload>) and decodes the
// full response envelope ({jsonrpc, id, result?, error?}).
func rpcCall(t *testing.T, socketPath, method string, params any) map[string]any {
	t.Helper()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	defer conn.Close()

	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := fmt.Fprintf(conn, "%d\n", len(payload)); err != nil {
		t.Fatalf("write length: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	reader := bufio.NewReader(conn)
	lengthLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read length: %v", err)
	}
	var length int
	if _, err := fmt.Sscanf(strings.TrimSpace(lengthLine), "%d", &length); err != nil || length <= 0 {
		t.Fatalf("bad length line %q", lengthLine)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(reader, buf); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatalf("decode response %q: %v", buf, err)
	}
	return resp
}

// rpcResult asserts the call succeeded and returns the result object.
func rpcResult(t *testing.T, socketPath, method string, params any) map[string]any {
	t.Helper()
	resp := rpcCall(t, socketPath, method, params)
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("%s: RPC error: %v", method, e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: result is not an object: %v", method, resp["result"])
	}
	return result
}

// createSessionRPC creates a session with full control over the create
// params (no CLI --cwd defaulting), returning the session id.
func createSessionRPC(t *testing.T, s *harness.Stack, params map[string]any) string {
	t.Helper()
	result := rpcResult(t, s.SocketPath, "session.create", params)
	id, _ := result["id"].(string)
	if id == "" {
		t.Fatalf("session.create returned no id: %v", result)
	}
	return id
}

// sessionGetRPC fetches a session via session.get.
func sessionGetRPC(t *testing.T, s *harness.Stack, id string) map[string]any {
	t.Helper()
	return rpcResult(t, s.SocketPath, "session.get", map[string]any{"id": id})
}

// rawJSON renders v compactly for substring assertions.
func rawJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
