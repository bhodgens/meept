//go:build e2e

// Package cisurface covers the CLI surface contract (cli-surface-01): the
// session/thread/effects commands reflect the daemon's persisted state, and
// against a dead daemon they fail fast with the connect error.
package cisurface

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// cli-surface-01: CLI session/thread/effects commands reflect the daemon's
// persisted state; a dead daemon fails fast.
func TestCLIReflectsPersistedState(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")

	// Create persisted state through the daemon (CLI path).
	sessionID := s.CreateSession(t, "cli-surface", s.ProjectDir)

	// session list --json must contain the session.
	out, _ := s.RunCLI(t, 30*time.Second, false, "session", "list", "--json")
	if !strings.Contains(out, sessionID) {
		t.Fatalf("session list --json missing session %s:\n%s", sessionID, out)
	}

	// session get must print the id.
	out, _ = s.RunCLI(t, 30*time.Second, false, "session", "get", sessionID)
	if !strings.Contains(out, sessionID) {
		t.Fatalf("session get output missing id:\n%s", out)
	}

	// Create a thread via the CLI and see it in thread list.
	out, _ = s.RunCLI(t, 30*time.Second, false,
		"thread", "new", "review", "--session", sessionID)
	if !strings.Contains(out, "created new thread") {
		t.Fatalf("thread new output unexpected:\n%s", out)
	}
	out, _ = s.RunCLI(t, 30*time.Second, false,
		"thread", "list", "--session", sessionID, "--json")
	if !strings.Contains(out, "review") {
		t.Fatalf("thread list --json missing the review thread:\n%s", out)
	}
	var threadList struct {
		Threads []struct {
			ID       string `json:"id"`
			IsActive bool   `json:"is_active"`
		} `json:"threads"`
	}
	if err := json.Unmarshal([]byte(out), &threadList); err != nil {
		t.Fatalf("thread list --json decode: %v\n%s", err, out)
	}
	if len(threadList.Threads) == 0 {
		t.Fatalf("thread list --json returned no threads:\n%s", out)
	}

	// effects list against the live daemon answers (empty ledger → empty
	// list, never an error).
	out, _ = s.RunCLI(t, 30*time.Second, false, "effects", "list")
	if strings.Contains(out, "effects ledger unavailable") {
		t.Fatalf("effects list unavailable on a live daemon:\n%s", out)
	}
}

// TestCLIFailsFastOnDeadDaemon: with no daemon listening on the socket, CLI
// commands fail fast with the connect error instead of hanging.
func TestCLIFailsFastOnDeadDaemon(t *testing.T) {
	s := harness.Start(t)
	pid := s.Daemon.Pid()
	if pid == 0 {
		t.Fatal("daemon pid not recorded")
	}
	// SIGKILL the daemon to simulate a crash (no graceful socket cleanup).
	if p, err := findProcess(pid); err == nil {
		_ = killProcess(p)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := findProcess(pid); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	start := time.Now()
	_, stderr := s.RunCLI(t, 30*time.Second, true, "session", "list")
	elapsed := time.Since(start)

	combined := strings.ToLower(stderr)
	if !strings.Contains(combined, "connect") && !strings.Contains(combined, "connection") && !strings.Contains(combined, "dial") {
		t.Fatalf("expected a connect-flavored error against a dead daemon, stderr:\n%s", stderr)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("CLI took %s to fail against a dead daemon; must fail fast", elapsed)
	}
}
