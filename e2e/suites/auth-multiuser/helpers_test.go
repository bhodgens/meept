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

// bootExtra boots a fresh daemon against the stack's sandbox (same flags
// and env as harness.Daemon.boot) and waits for /health.
//
// Boot-death detection waits on cmd.Wait() — NOT Signal(syscall.Signal(0)).
// The extra daemon is a direct child of the test binary; until something
// reaps it, a dead child still answers signal 0 as a zombie, so a signal
// probe turns every early boot failure into a silent 60s timeout. The
// exited channel fires the moment the process is reaped.
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
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

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
				// cleanup uses it to stop the process. The reaper
				// goroutine stays parked until the daemon eventually
				// exits (cleanup) and reaps it then.
				return
			}
		}
		select {
		case <-exited:
			t.Fatalf("multi-user daemon died during boot; log tail:\n%s", s.Daemon.LogTail())
		default:
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("multi-user daemon not healthy within 60s; log tail:\n%s", s.Daemon.LogTail())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// insecureTLSConf returns the scratch-TLS transport config.
func insecureTLSConf() *tls.Config { return tlsConf }
