//go:build e2e

// Package sessionbinding covers per-session project/working-dir binding:
// CWD detection persists a detection context, projectless sessions stay
// unbound with the actionable sentinel, and explicit project_id wins.
package sessionbinding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// sessionCreateRPC creates a session with full control over the params (no
// CLI --cwd defaulting) via the daemon's own RPC surface, returning (id,
// project_id, project_path).
func sessionCreateRPC(t *testing.T, s *harness.Stack, params map[string]any) (string, string, string) {
	t.Helper()
	resp := rawRPCCall(t, s.SocketPath, "session.create", params)
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("session.create error: %v", e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("session.create bad result: %v", resp)
	}
	id, _ := result["id"].(string)
	pid, _ := result["project_id"].(string)
	ppath, _ := result["project_path"].(string)
	return id, pid, ppath
}

// session-binding-01: session.create binds the client CWD into a project and
// persists the detection context — a session created with detection_context
// {cwd: DIR} must come back with a project whose LocalPath is DIR and the
// detection context retained.
func TestSessionCreateBindsCWDDetectionContext(t *testing.T) {
	s := harness.Start(t)

	cwd := filepath.Join(s.Work, "client-repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionID, projectID, projectPath := sessionCreateRPC(t, s, map[string]any{
		"name":              "bind-01",
		"detection_context": map[string]any{"cwd": cwd},
	})
	if sessionID == "" {
		t.Fatal("no session id")
	}
	if projectID == "" || projectPath == "" {
		t.Fatalf("session.create did not bind the client CWD into a project: id=%q path=%q", projectID, projectPath)
	}
	if filepath.Clean(projectPath) != filepath.Clean(cwd) {
		t.Fatalf("project_path = %q, want %q", projectPath, cwd)
	}

	// The detection context must persist (SetDetectionContext at create).
	get := rpcCall(t, s.SocketPath, "session.get", map[string]any{"id": sessionID})
	raw := rawJSON(t, get)
	if !strings.Contains(raw, "detection_context") || !strings.Contains(raw, cwd) {
		t.Fatalf("detection context not persisted on the session: %s", raw)
	}

	// Persistence means a store round-trip (Get re-read) keeps it; also
	// assert the session list view carries it (store List reads from db).
	list := rpcCall(t, s.SocketPath, "session.list", map[string]any{"limit": 50})
	listRaw := rawJSON(t, list)
	if !strings.Contains(listRaw, cwd) {
		t.Fatalf("detection context missing from the persisted list view: %s", listRaw)
	}
}

// session-binding-02: a session with no CWD/project stays unbound.
func TestSessionWithoutCWWDOrProjectStaysUnbound(t *testing.T) {
	s := harness.Start(t)

	sessionID, projectID, projectPath := sessionCreateRPC(t, s, map[string]any{"name": "bind-02"})
	if sessionID == "" {
		t.Fatal("no session id")
	}
	if projectID != "" || projectPath != "" {
		t.Fatalf("unbound session got project id=%q path=%q; no global fallback may bind it", projectID, projectPath)
	}

	get := rpcCall(t, s.SocketPath, "session.get", map[string]any{"id": sessionID})
	raw := rawJSON(t, get)
	if strings.Contains(raw, `"project_id":"`) && !strings.Contains(raw, `"project_id":""`) {
		t.Fatalf("unbound session carries a project id: %s", raw)
	}
	if strings.Contains(raw, "detection_context") {
		t.Fatalf("unbound session carries a detection context: %s", raw)
	}
}

// session-binding-03: an explicit project_id binding wins over CWD
// detection — both are provided, the session binds to the PROJECT, not to a
// CWD-resolved project.
func TestExplicitProjectIDWinsOverCWDDetection(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")

	list := rpcCall(t, s.SocketPath, "project.list", map[string]any{})
	projects, _ := list["projects"].([]any)
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
		t.Fatalf("project not registered: %v", list)
	}

	// A DIFFERENT directory as the detection CWD.
	otherCWD := filepath.Join(s.Work, "other-cwd")
	if err := os.MkdirAll(otherCWD, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessionID, gotProjectID, gotProjectPath := sessionCreateRPC(t, s, map[string]any{
		"name":              "bind-03",
		"project_id":        projectID,
		"detection_context": map[string]any{"cwd": otherCWD},
	})
	if sessionID == "" {
		t.Fatal("no session id")
	}
	if gotProjectID != projectID {
		t.Fatalf("explicit project_id not honored: got %q, want %q", gotProjectID, projectID)
	}
	if gotProjectPath == "" {
		t.Fatalf("explicit project binding produced no project_path: %v", gotProjectPath)
	}
	if filepath.Clean(gotProjectPath) == filepath.Clean(otherCWD) {
		t.Fatalf("CWD detection won over the explicit project_id: path=%q", gotProjectPath)
	}
	if filepath.Base(gotProjectPath) != filepath.Base(s.ProjectDir) {
		t.Fatalf("project_path = %q, want the registered project dir %s", gotProjectPath, s.ProjectDir)
	}

	// Working-directory precedence reads the PROJECT path for this session
	// (session.ResolveWorkingDir: worktree > project > detection CWD). The
	// persisted session record carries the binding; assert via session.get.
	get := rpcCall(t, s.SocketPath, "session.get", map[string]any{"id": sessionID})
	if pp, _ := get["project_path"].(string); filepath.Clean(pp) != filepath.Clean(s.ProjectDir) {
		t.Fatalf("persisted project_path = %q, want %q", pp, s.ProjectDir)
	}
}
