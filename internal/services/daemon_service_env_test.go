package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDaemonStartCmdSetsEnvExplicitly guards the contract for the HTTP/daemon
// control start path: the spawn command must carry an EXPLICIT environment. A
// nil Env is what lets a rebuilt-environment spawn path start a key-less
// daemon without any error at start time.
func TestDaemonStartCmdSetsEnvExplicitly(t *testing.T) {
	const sentinel = "MEEPT_DAEMON_SERVICE_SENTINEL"
	t.Setenv(sentinel, "sentinel-value")

	cmd := newDaemonStartCmd("/bin/true", nil)
	if cmd.Env == nil {
		t.Fatal("daemon start cmd has a nil Env: the daemon would depend on implicit environment inheritance; set Env explicitly to the caller's environment")
	}
	names := map[string]bool{}
	for _, kv := range cmd.Env {
		if name, _, ok := strings.Cut(kv, "="); ok {
			names[name] = true
		}
	}
	if !names[sentinel] {
		t.Fatalf("daemon start cmd environment is missing the caller's %s variable", sentinel)
	}
}

// TestDaemonServiceStartPassesCallerEnv guards the same contract as the CLI
// spawn (cmd/meept/daemon_env_test.go) for the HTTP/daemon-control start path:
// the spawned daemon must receive THIS process's environment, because that is
// what carries the ${VAR} provider credentials its config references.
//
// The test uses a throwaway sentinel variable and a fake daemon binary that
// records the NAMES of the variables it received. No value is read or asserted
// on, and no secret is ever printed.
func TestDaemonServiceStartPassesCallerEnv(t *testing.T) {
	const (
		sentinel = "MEEPT_DAEMON_SERVICE_SENTINEL"
		reportV  = "MEEPT_DAEMON_SERVICE_REPORT"
	)
	dir := t.TempDir()
	report := filepath.Join(dir, "names.txt")

	t.Setenv(sentinel, "sentinel-value")
	t.Setenv(reportV, report)

	// Fake daemon: records the NAMES of its environment, then stays alive long
	// enough for Start's 200ms liveness check. Names only, never values.
	fakeDaemon := filepath.Join(dir, "fake-meept-daemon")
	script := "#!/bin/sh\n" +
		"printenv | sed -n 's/^\\([A-Za-z_][A-Za-z0-9_]*\\)=.*/\\1/p' > \"$" + reportV + "\"\n" +
		"sleep 1\n"
	if err := os.WriteFile(fakeDaemon, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake daemon: %v", err)
	}

	svc := NewDaemonService(filepath.Join(dir, "meept.pid"), dir, fakeDaemon, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("DaemonService.Start: %v", err)
	}

	// The fake daemon writes its report as soon as it runs; allow a moment on a
	// loaded machine.
	var data []byte
	var err error
	for range 40 {
		if data, err = os.ReadFile(report); err == nil && len(data) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || len(data) == 0 {
		t.Fatalf("fake daemon wrote no environment report: %v", err)
	}

	names := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			names[line] = true
		}
	}
	if !names[sentinel] {
		t.Fatalf("daemon environment is missing the caller's %s variable (child saw %d variables)", sentinel, len(names))
	}
	if !names["PATH"] {
		t.Fatal("daemon environment is missing PATH")
	}
}
