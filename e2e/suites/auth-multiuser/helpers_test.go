//go:build e2e

// Helpers for the auth-multiuser suite: sha256 hashing, a private daemon
// boot (the harness boots before the config rewrite, so multi-user tests
// boot one more daemon themselves), and signal helpers.
package authmultiuser

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// sha256Hex returns the hex sha256 digest of raw (the auth store's hash
// format).
func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// syscallTERM wraps syscall.SIGTERM (keeps the main file's imports lean).
func syscallTERM() os.Signal { return syscall.SIGTERM }

// waitForDaemonDown polls the sandbox health endpoint until the daemon
// stops answering (SIGTERM accepted). The harness's own cleanup later waits
// on the process itself; here we only need the ports/socket released.
func waitForDaemonDown(t *testing.T, s *harness.Stack) {
	t.Helper()
	client := &http.Client{
		Timeout: 1500 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig:   insecureTLSConf(),
			ForceAttemptHTTP2: false,
		},
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(s.HTTPBaseURL() + "/health") //nolint:noctx // bounded poll in test harness
		if err != nil {
			// Connection refused/EOF => the listener is down.
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("daemon still answering after SIGTERM within 45s")
}

// bootExtra boots a fresh daemon against the stack's sandbox (same flags
// and env as harness.Daemon.boot) and waits for /health.
func bootExtra(t *testing.T, s *harness.Stack) {
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
			TLSClientConfig: insecureTLSConf(), //nolint:gosec // set in helper
		},
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := client.Get(s.HTTPBaseURL() + "/health") //nolint:noctx // bounded poll in test harness
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				// The extra daemon writes its pidfile; the test's deferred
				// cleanup uses it to stop the process.
				return
			}
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("multi-user daemon not healthy within 60s; log tail:\n%s", s.Daemon.LogTail())
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("multi-user daemon died during boot; log tail:\n%s", s.Daemon.LogTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// insecureTLSConf returns the scratch-TLS transport config.
func insecureTLSConf() *tls.Config { return tlsConf }
