package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The daemon expands the ${VAR} provider credentials its config references
// (models.json5 `"apiKey": "${GALA_API_KEY}"`), so `meept daemon start` and
// `meept daemon restart` must hand the daemon the CALLER's environment. These
// tests use throwaway sentinel variables and assert on variable NAMES only:
// no test here reads, prints, or compares a secret value.

const (
	// envSpawnSentinel is a non-secret marker variable. Its value is written
	// into the child's view of the environment but is never asserted on.
	envSpawnSentinel = "MEEPT_DAEMON_SPAWN_SENTINEL"
	// envSpawnReport names the file the fake daemon binary writes the NAMES of
	// the environment variables it received to.
	envSpawnReport = "MEEPT_DAEMON_SPAWN_REPORT"
)

// envNames returns the variable names of an "KEY=value" environment slice.
// Values are dropped immediately: nothing downstream can leak one.
func envNames(env []string) map[string]bool {
	names := make(map[string]bool, len(env))
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok {
			names[name] = true
		}
	}
	return names
}

// TestDaemonSpawnCmdSetsEnvExplicitly guards the contract: the spawn command
// must carry an EXPLICIT environment. A nil Env would make the caller's
// provider keys survive only as a side effect of the call site, which is how a
// restart can end up with a daemon that has no keys at all (the daemon starts
// fine, then every cloud call fails `HTTP 401: Token not provided` while the
// log shows `has_api_key=false`).
func TestDaemonSpawnCmdSetsEnvExplicitly(t *testing.T) {
	t.Setenv(envSpawnSentinel, "sentinel-value")

	cmd := newDaemonSpawnCmd("/bin/true", nil)
	if cmd.Env == nil {
		t.Fatal("daemon spawn cmd has a nil Env: the daemon would depend on implicit environment inheritance; set Env explicitly to the caller's environment")
	}
	if !envNames(cmd.Env)[envSpawnSentinel] {
		t.Fatalf("daemon spawn cmd environment is missing the caller's %s variable", envSpawnSentinel)
	}
	if names := envNames(cmd.Env); !names["PATH"] {
		t.Fatal("daemon spawn cmd environment is missing PATH")
	}
}

// TestDaemonSpawnCmdPassesCallerEnvToChild runs the spawn command with a fake
// "meept-daemon" that records the NAMES of the variables it received, and
// asserts the caller's sentinel reached the child. This is the end-to-end half
// of the contract: the daemon process the CLI starts sees the caller's
// environment, which is what carries its API keys.
func TestDaemonSpawnCmdPassesCallerEnvToChild(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "names.txt")

	t.Setenv(envSpawnSentinel, "sentinel-value")
	t.Setenv(envSpawnReport, report)

	// A fake daemon binary: it writes the NAMES of its own environment to the
	// report file. Names only — a value never lands on disk.
	fakeDaemon := filepath.Join(dir, "fake-meept-daemon")
	script := "#!/bin/sh\n" +
		"printenv | sed -n 's/^\\([A-Za-z_][A-Za-z0-9_]*\\)=.*/\\1/p' > \"$" + envSpawnReport + "\"\n"
	if err := os.WriteFile(fakeDaemon, []byte(script), 0o600); err != nil {
		t.Fatalf("write fake daemon: %v", err)
	}
	if err := os.Chmod(fakeDaemon, 0o700); err != nil {
		t.Fatalf("chmod fake daemon: %v", err)
	}

	if out, err := newDaemonSpawnCmd(fakeDaemon, nil).CombinedOutput(); err != nil {
		t.Fatalf("run fake daemon: %v (output: %s)", err, out)
	}

	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read child environment report: %v", err)
	}
	childNames := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			childNames[line] = true
		}
	}
	if !childNames[envSpawnSentinel] {
		t.Fatalf("child environment is missing the caller's %s variable (child saw %d variables)", envSpawnSentinel, len(childNames))
	}
	if !childNames["PATH"] {
		t.Fatal("child environment is missing PATH")
	}
}
