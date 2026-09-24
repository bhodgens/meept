//go:build e2e

// Package staterestart covers the zero-unit-coverage restart flows: a real
// daemon process is stopped and re-booted against the SAME sandbox home
// (MEEPT_HOME + state dir), and every scenario asserts the persisted state
// (sessions, detection context, threads, project binding) is re-resolved
// from the store alone — never from daemon memory.
package staterestart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// state-restart-01: session + messages survive a daemon restart (same
// MEEPT_HOME). Created and saved via the daemon's own RPC surface so the
// assertion covers the persistence path itself.
func TestSessionAndMessagesSurviveRestart(t *testing.T) {
	s := harness.Start(t)

	sessionID := createSessionRPC(t, s, map[string]any{
		"name":        "restart-01",
		"description": "survives restart",
	})

	saveResult := rpcResult(t, s.SocketPath, "session.messages.save", map[string]any{
		"session_id": sessionID,
		"messages": []map[string]any{
			{"role": "user", "content": "hello before restart", "timestamp": time.Now().UTC().Format(time.RFC3339)},
			{"role": "assistant", "content": "hi before restart", "timestamp": time.Now().UTC().Format(time.RFC3339)},
		},
	})
	_ = saveResult

	stop := restartDaemon(t, s)
	defer stop()

	got := sessionGetRPC(t, s, sessionID)
	if got["id"] != sessionID {
		t.Fatalf("session id after restart = %v, want %s", got["id"], sessionID)
	}
	if got["name"] != "restart-01" {
		t.Fatalf("session name after restart = %v, want restart-01", got["name"])
	}

	msgResult := rpcResult(t, s.SocketPath, "session.messages.get", map[string]any{
		"session_id": sessionID, "offset": 0, "limit": 50,
	})
	raw := rawJSON(t, msgResult)
	if !strings.Contains(raw, "hello before restart") {
		t.Fatalf("user message lost across restart; result: %s", raw)
	}
	if !strings.Contains(raw, "hi before restart") {
		t.Fatalf("assistant message lost across restart; result: %s", raw)
	}
}

// state-restart-02: detection_context CWD re-resolves from the store after
// restart — the persisted column is the only source (the new daemon process
// has never held this session in memory).
func TestDetectionContextReresolvesAfterRestart(t *testing.T) {
	s := harness.Start(t)

	cwd := filepath.Join(s.Work, "detection-cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	sessionID := createSessionRPC(t, s, map[string]any{
		"name":              "restart-02",
		"detection_context": map[string]any{"cwd": cwd},
	})

	// Sanity before restart.
	got := sessionGetRPC(t, s, sessionID)
	if !strings.Contains(rawJSON(t, got), cwd) {
		t.Fatalf("detection context not visible before restart: %v", got)
	}

	stop := restartDaemon(t, s)
	defer stop()

	got = sessionGetRPC(t, s, sessionID)
	dc, _ := got["detection_context"].(map[string]any)
	if dc == nil {
		t.Fatalf("detection_context missing after restart: %s", rawJSON(t, got))
	}
	if dc["cwd"] != cwd {
		t.Fatalf("detection_context.cwd = %v, want %s (full session: %s)", dc["cwd"], cwd, rawJSON(t, got))
	}
}

// state-restart-03: threads survive restart with the active-thread pointer
// intact.
func TestThreadsSurviveRestartWithActivePointer(t *testing.T) {
	s := harness.Start(t)

	sessionID := createSessionRPC(t, s, map[string]any{"name": "restart-03"})

	createResult := rpcResult(t, s.SocketPath, "session.thread.create", map[string]any{
		"session_id":  sessionID,
		"topic_label": "review",
		"is_active":   true,
	})
	threadID, _ := createResult["id"].(string)
	if threadID == "" {
		t.Fatalf("thread create returned no id: %v", createResult)
	}

	stop := restartDaemon(t, s)
	defer stop()

	listResult := rpcResult(t, s.SocketPath, "session.thread.list", map[string]any{
		"session_id": sessionID,
	})
	raw := rawJSON(t, listResult)
	if !strings.Contains(raw, threadID) {
		t.Fatalf("thread %s lost across restart; list: %s", threadID, raw)
	}
	if !strings.Contains(raw, `"is_active":true`) {
		t.Fatalf("active-thread pointer lost across restart; list: %s", raw)
	}

	curResult := rpcResult(t, s.SocketPath, "session.thread.current", map[string]any{
		"session_id": sessionID,
	})
	thread, _ := curResult["thread"].(map[string]any)
	if thread == nil {
		t.Fatalf("no active thread after restart: %v", curResult)
	}
	if thread["id"] != threadID {
		t.Fatalf("active thread id = %v, want %s", thread["id"], threadID)
	}
}

// state-restart-04: an unbound session STAYS unbound across restart (no
// working dir materializes out of nowhere) and the tool surface keeps the
// actionable ErrNoWorkingDir sentinel (not the bare "no path specified").
func TestUnboundSessionStaysUnboundAcrossRestart(t *testing.T) {
	s := harness.Start(t)

	// No detection_context, no project: deliberately unbound. The raw RPC
	// create (not the CLI) avoids the CLI's implicit CWD defaulting.
	sessionID := createSessionRPC(t, s, map[string]any{"name": "restart-04"})

	got := sessionGetRPC(t, s, sessionID)
	if dc := got["detection_context"]; dc != nil {
		if m, ok := dc.(map[string]any); ok && len(m) > 0 {
			t.Fatalf("session unexpectedly has a detection context before restart: %v", dc)
		}
	}

	stop := restartDaemon(t, s)
	defer stop()

	got = sessionGetRPC(t, s, sessionID)
	if got["id"] != sessionID {
		t.Fatalf("session lost across restart: %v", got)
	}
	if dc := got["detection_context"]; dc != nil {
		if m, ok := dc.(map[string]any); ok && len(m) > 0 {
			t.Fatalf("unbound session gained a detection context across restart: %v", dc)
		}
	}
	if pp := got["project_path"]; pp != nil && pp != "" {
		t.Fatalf("unbound session gained a project binding across restart: %v", pp)
	}

	// The actionable sentinel: a turn on the unbound session must surface
	// the loud no-working-dir warning with its bind-a-project hint — the
	// daemon never falls back to its own CWD and never silently binds a
	// directory that the store does not carry.
	sentinel := sentinelProbe(t, s, sessionID)
	if !strings.Contains(sentinel, "chat turn has no working directory bound") {
		t.Fatalf("expected the actionable no-working-dir sentinel after restart, got:\n%s", sentinel)
	}
}

// state-restart-05 (L): a project-bound session re-binds via the persisted
// project_id after restart — the project store re-opens and the session's
// stored project_id still resolves to the registered project directory.
func TestProjectBoundSessionRebindsAfterRestart(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")

	// Fetch the project id from the registered record.
	listResult := rpcResult(t, s.SocketPath, "project.list", map[string]any{})
	projects, _ := listResult["projects"].([]any)
	var projectID string
	for _, p := range projects {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if name, _ := pm["name"].(string); name == "e2e-project" {
			projectID, _ = pm["id"].(string)
		}
	}
	if projectID == "" {
		t.Fatalf("registered project e2e-project not found: %v", listResult)
	}

	sessionID := createSessionRPC(t, s, map[string]any{
		"name":       "restart-05",
		"project_id": projectID,
	})

	got := sessionGetRPC(t, s, sessionID)
	if got["project_id"] != projectID {
		t.Fatalf("project binding not applied at create: %v", got)
	}

	stop := restartDaemon(t, s)
	defer stop()

	got = sessionGetRPC(t, s, sessionID)
	if got["project_id"] != projectID {
		t.Fatalf("project_id lost across restart: got %v, want %s", got["project_id"], projectID)
	}
	path, _ := got["project_path"].(string)
	if path == "" {
		t.Fatalf("project_path missing after restart (project store did not re-bind): %v", got)
	}
	if filepath.Base(path) != filepath.Base(s.ProjectDir) {
		t.Fatalf("project_path after restart = %q, want under %s", path, s.ProjectDir)
	}
}
