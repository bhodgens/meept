package signal

// Supervisor tests (leaf 10, T4): a stub `signal-cli` (Python script)
// speaks minimal json-rpc 2.0 over the configured unix socket; the
// supervisor must spawn it via the wrapped llm.RuntimeProcess, wait
// (bounded) for the socket, hand out a working Client, kill the process
// group on Stop, and restart a crashed child with backoff (capped).
//
// The stub is materialized per test into t.TempDir() — a TEST-ONLY
// artifact (leaf doc: never committed as a production artifact).

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubCLISource is the stub signal-cli: argv contains --socket <path>;
// it binds the socket and serves one-response-per-line json-rpc. With
// crashOnce true it hard-exits after its FIRST served request and then
// records a marker file beside the socket so the REPLACEMENT child
// (same argv, new pid) serves normally — the crash-once scenario the
// restart contract needs. The marker path rides MEEPT_STUB_MARKER.
func stubCLISource(crashOnce bool) string {
	crashLogic := ""
	if crashOnce {
		crashLogic = "" +
			"            marker = os.environ.get(\"MEEPT_STUB_MARKER\", \"\")\n" +
			"            if marker and not os.path.exists(marker):\n" +
			"                open(marker, \"w\").close()\n" +
			"                os._exit(70)\n"
	}
	return fmt.Sprintf(`#!/usr/bin/env python3
import json, os, socket, sys
path = sys.argv[sys.argv.index("--socket") + 1]
try:
    os.unlink(path)
except OSError:
    pass
srv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
srv.bind(path)
srv.listen(4)
while True:
    conn, _ = srv.accept()
    f = conn.makefile("rwb")
    try:
        for line in f:
            line = line.strip()
            if not line:
                continue
            req = json.loads(line)
            resp = {"jsonrpc": "2.0", "id": req["id"],
                    "result": json.dumps({"echo": req["method"]})}
            f.write((json.dumps(resp) + "\n").encode())
            f.flush()
%s            continue
    except Exception:
        pass
    finally:
        conn.close()
`, crashLogic)
}

func newSupervisorForTest(t *testing.T, crashOnce bool) (*Supervisor, string) {
	t.Helper()
	dir := t.TempDir()
	stubPath := filepath.Join(dir, "signal-cli-stub.py")
	if err := os.WriteFile(stubPath, []byte(stubCLISource(crashOnce)), 0o700); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	// Short dir keeps the unix socket path under the 104-byte limit.
	sockDir := filepath.Dir(stubPath)
	socketPath := filepath.Join(sockDir, "sig.sock")
	cfg := SupervisorConfig{
		StartTimeoutSeconds: 10,
		RestartBackoffMs:    100,
		RestartMaxBackoffMs: 400,
	}
	sup := NewSupervisor(stubPath, "+15559990000", socketPath, cfg, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if crashOnce {
		// The crash-once marker lives beside the socket; the stub's
		// replacement child sees it and serves normally.
		t.Setenv("MEEPT_STUB_MARKER", filepath.Join(sockDir, "crashed.marker"))
	}
	t.Cleanup(func() {
		_ = sup.Stop(context.Background())
	})
	return sup, socketPath
}

// awaitClient polls Client() until it returns a live client or the
// deadline expires. After a crash-restart the supervisor increments
// `restarts` as soon as spawnAndWait returns, but that wait only stats
// the socket path: the crashed child leaves its socket FILE behind, and
// the replacement unlinks/rebinds it a moment later. In that window the
// path exists but has no listener, so a single Client() dial gets
// ECONNREFUSED. Bounded polling removes the race without touching the
// production supervisor and keeps the crash-restart assertion strict.
func awaitClient(t *testing.T, sup *Supervisor, timeout time.Duration) *Client {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		c, err := sup.Client()
		if err == nil {
			return c
		}
		lastErr = err
		if time.Now().After(deadline) {
			t.Fatalf("Client did not become ready within %s: %v", timeout, lastErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestSupervisorStartClientStop covers the canonical T4 loop: Start
// waits for the socket, Client dials and completes one request, Stop
// kills the child (process group via RuntimeProcess) and the socket is
// cleaned up.
func TestSupervisorStartClientStop(t *testing.T) {
	sup, socketPath := newSupervisorForTest(t, false)
	ctx := context.Background()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket not created by child: %v", err)
	}

	c, err := sup.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if err := c.Typing("+15550001111", false); err != nil {
		t.Fatalf("Typing via supervised client (one round trip): %v", err)
	}

	if err := sup.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Errorf("socket file survived Stop: %v", err)
	}
	// Post-stop Client must be refused.
	if _, err := sup.Client(); err == nil {
		t.Errorf("Client after Stop succeeded, want refusal")
	}
}

// TestSupervisorRestartOnCrash pins the restart contract: the stub
// hard-exits after its first served request; the supervisor must
// restart it within the configured backoff and a fresh Client must
// recover.
func TestSupervisorRestartOnCrash(t *testing.T) {
	sup, _ := newSupervisorForTest(t, true)
	ctx := context.Background()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c, err := sup.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	// The stub echoes the method name, so Send's timestamp decode
	// fails — but the request reached the child, which is what the
	// crash scenario needs. Swallow the decode error on purpose.
	_, _ = c.Send("+15550001111", "kill me", nil) //nolint:errcheck // stub result shape differs; the request is what matters

	// Wait for the restart (bounded by backoff cap + slack).
	deadline := time.Now().Add(15 * time.Second)
	restarts := 0
	for time.Now().Before(deadline) {
		sup.stateMu.Lock()
		restarts = sup.restarts
		sup.stateMu.Unlock()
		if restarts >= 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if restarts < 1 {
		t.Fatalf("supervisor did not restart the crashed child within 15s")
	}

	// A fresh Client dials the NEW child's socket and completes a
	// request — the crash-recovery contract. Poll because the replacement
	// child may not have rebound the socket yet (see awaitClient).
	c2 := awaitClient(t, sup, 10*time.Second)
	if err := c2.Typing("+15550001111", true); err != nil {
		t.Fatalf("Typing after restart: %v", err)
	}
}

// TestSupervisorBackoffCapped pins the cap: a config whose max is below
// the doubled base resolves the restart wait to the max, never above.
func TestSupervisorBackoffCapped(t *testing.T) {
	sup := NewSupervisor("signal-cli", "", "", SupervisorConfig{
		RestartBackoffMs:    1000,
		RestartMaxBackoffMs: 1500,
	}, nil)
	got := sup.nextBackoff(time.Duration(sup.cfg.RestartBackoffMs) * time.Millisecond)
	if got > time.Duration(sup.cfg.RestartMaxBackoffMs)*time.Millisecond {
		t.Errorf("nextBackoff = %v, exceeds cap %v", got, sup.cfg.RestartMaxBackoffMs)
	}
	if got != 1500*time.Millisecond {
		t.Errorf("nextBackoff = %v, want capped 1500ms", got)
	}
}
