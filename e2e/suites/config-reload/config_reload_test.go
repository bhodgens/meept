//go:build e2e

// Package configreload covers config-reload-01: SIGHUP reload applies
// config; a bad reload keeps the old config (the daemon stays healthy and
// keeps serving).
package configreload

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// readConfig returns the current meept.json5 content.
func readConfig(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.MeeptHome, "meept.json5"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(data)
}

// hup sends SIGHUP to the daemon and waits for the reload to settle.
func hup(t *testing.T, s *harness.Stack, pid int) {
	t.Helper()
	p, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find daemon process: %v", err)
	}
	if err := p.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("SIGHUP: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
}

// config-reload-01 (part 1): SIGHUP triggers a reload and the daemon keeps
// serving with an unchanged, valid config on disk.
func TestSIGHUPReloadKeepsDaemonHealthy(t *testing.T) {
	s := harness.Start(t)
	pid := s.Daemon.Pid()

	hup(t, s, pid)

	// Daemon still healthy and answering after the reload.
	health := s.HealthJSON(t)
	if health["status"] != "ok" {
		t.Fatalf("health after reload = %v, want ok", health["status"])
	}

	// The daemon log must show the reload path ran.
	tail := s.Daemon.LogTail()
	if !strings.Contains(tail, "reloading configuration") {
		t.Fatalf("daemon log does not show the SIGHUP reload; tail:\n%s", tail)
	}
}

// config-reload-01 (part 2): a BAD config on disk keeps the old config —
// the daemon does not crash, stays healthy, and keeps serving.
func TestBadReloadKeepsOldConfig(t *testing.T) {
	s := harness.Start(t)
	pid := s.Daemon.Pid()

	// Corrupt the on-disk config (syntactically broken JSON5).
	good := readConfig(t, s)
	badPath := filepath.Join(s.MeeptHome, "meept.json5")
	if err := os.WriteFile(badPath, []byte("{ this is not ] valid json5"), 0o600); err != nil {
		t.Fatalf("write bad config: %v", err)
	}
	t.Cleanup(func() { _ = os.WriteFile(badPath, []byte(good), 0o600) })

	hup(t, s, pid)

	// The daemon must still be alive and healthy (old config retained).
	health := s.HealthJSON(t)
	if health["status"] != "ok" {
		t.Fatalf("health after bad reload = %v; the daemon must keep the old config and stay healthy", health["status"])
	}

	// And the RPC surface still answers.
	sess := s.CreateSession(t, "reload-probe", s.Work)
	if sess == "" {
		t.Fatal("session.create failed after a bad reload; daemon not serving")
	}

	// Restore and reload: the daemon recovers with the good config.
	if err := os.WriteFile(badPath, []byte(good), 0o600); err != nil {
		t.Fatalf("restore config: %v", err)
	}
	hup(t, s, pid)
	health = s.HealthJSON(t)
	if health["status"] != "ok" {
		t.Fatalf("health after restored reload = %v, want ok", health["status"])
	}
}
