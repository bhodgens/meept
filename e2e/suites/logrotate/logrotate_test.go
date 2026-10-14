//go:build e2e

// Package logrotate proves the daemon's in-process log cap
// (daemon.log_max_bytes; internal/logrotate) end to end: a scratch daemon
// booted with a small cap keeps its own meept.log bounded and rotates one
// retained generation, instead of growing without limit. A real chat turn
// (fake-LLM-driven, exactly like the smoke suite) generates enough INFO
// volume to force rotation deterministically. This is the e2e coverage the
// internal/logrotate package never had (bughunt 2026-10-13 H5), and the
// manifest entry that unblocks future edits to that package.
package logrotate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

func TestDaemonLogCapped(t *testing.T) {
	// Small enough that one chat turn's INFO logging forces several
	// rotations, so the .1 generation assertion is deterministic.
	const maxBytes = 1024
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"daemon.log_max_bytes": maxBytes,
	}))

	// Drive one full chat turn that executes a real tool: turn lifecycle,
	// classification, planner/executor, the LLM call and the tool execution
	// all log at INFO, giving guaranteed volume past the cap.
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "e2e-logrotate", s.ProjectDir)
	artifact := filepath.Join(s.ProjectDir, "capped.txt")
	s.Fake.SetPostToolText("Wrote capped.txt to " + artifact + ".")
	s.Fake.EnqueueFileWrite("call-1", artifact, "capped")
	_ = s.ChatTurn(t, sessionID,
		"Create a file named capped.txt containing the word capped",
		120*time.Second)

	// A second round-trip's worth of volume on top.
	_, _ = s.RunCLI(t, 30*time.Second, false, "session", "list", "--json")

	// The daemon resolves its own log to <state-dir>/meept.log (capped
	// writer), the -d dir the harness passes. The stdout/stderr capture the
	// harness keeps is separate (d.logPath) and only holds pre-logger output.
	logPath := filepath.Join(s.StateDir, "meept.log")

	fi, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("meept.log missing at %s: %v", logPath, err)
	}
	// The live file must stay bounded by the cap (generous slack for a write
	// in flight). With the cap NOT wired, a chat turn blows well past this.
	if fi.Size() > maxBytes*3 {
		t.Fatalf("meept.log is %d bytes, want <= %d: log cap not enforced", fi.Size(), maxBytes*3)
	}

	// A retained generation must exist: the chat turn logged far more than
	// 1024 bytes at INFO, so rotation ran. Bound the wait for a slow daemon.
	backup := logPath + ".1"
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(backup); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no retained generation %s after a chat turn: log cap did not rotate", backup)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
