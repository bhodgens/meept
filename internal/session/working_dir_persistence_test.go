package session

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/pkg/models"
)

// stubProjectResolver is a minimal in-package ProjectResolver for the RPC
// session.create path.
type stubProjectResolver struct {
	get *ProjectBinding
	err error
}

func (s stubProjectResolver) GetProject(context.Context, string) (*ProjectBinding, error) {
	return s.get, s.err
}

func (s stubProjectResolver) CreateOrResolveProject(context.Context, string) (*ProjectBinding, error) {
	return s.get, s.err
}

// TestSessionCreate_DetectionCWDNoProject_SurvivesStoreRoundTrip is the
// central regression for `meept session create --cwd DIR`: a session with a
// detection-context CWD and NO project must resolve that CWD at turn time —
// INCLUDING after a daemon restart, from the store alone. The restart is
// simulated by closing the store and re-opening the same database file.
func TestSessionCreate_DetectionCWDNoProject_SurvivesStoreRoundTrip(t *testing.T) {
	store, dbPath := testHelper(t)
	cwd := filepath.Join(t.TempDir(), "client-cwd")

	handler := NewHandler(store, nil, slog.Default())
	params, _ := json.Marshal(map[string]any{
		"name":              "cwd-session",
		"detection_context": map[string]string{"cwd": cwd},
	})
	result, err := handler.handleCreate(&models.BusMessage{Payload: params})
	if err != nil {
		t.Fatalf("handleCreate: %v", err)
	}
	sess := result.(*Session)
	if sess.ProjectPath != "" {
		t.Fatalf("expected no project binding, got %q", sess.ProjectPath)
	}
	if dir, src := ResolveWorkingDir(sess); dir != cwd || src != WorkingDirFromDetection {
		t.Fatalf("in-memory ResolveWorkingDir() = (%q, %q), want the detection CWD", dir, src)
	}

	// Simulate a daemon restart: drop the store and reopen the DB file.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	restarted, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer restarted.Close()

	reloaded := restarted.Get(sess.ID)
	if reloaded == nil {
		t.Fatal("session not found after reopen")
	}
	if reloaded.DetectionContext == nil {
		t.Fatal("detection context lost across the store round-trip")
	}
	if reloaded.DetectionContext.CWD != cwd {
		t.Errorf("detection CWD = %q, want %q", reloaded.DetectionContext.CWD, cwd)
	}
	dir, src := ResolveWorkingDir(reloaded)
	if dir != cwd {
		t.Errorf("post-restart ResolveWorkingDir() dir = %q, want %q", dir, cwd)
	}
	if src != WorkingDirFromDetection {
		t.Errorf("post-restart ResolveWorkingDir() source = %q, want %q", src, WorkingDirFromDetection)
	}
}

// TestSessionCreate_RPC_BindsDetectionCWDIntoProject: with a resolver wired,
// the RPC create path resolves the client CWD into a project and binds it to
// the session (matching the services path).
func TestSessionCreate_RPC_BindsDetectionCWDIntoProject(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())
	handler.SetProjectResolver(stubProjectResolver{get: &ProjectBinding{ID: "proj-1", Path: "/repos/resolved"}})

	params, _ := json.Marshal(map[string]any{
		"name":              "sess",
		"detection_context": map[string]string{"cwd": "/repos/resolved"},
	})
	result, err := handler.handleCreate(&models.BusMessage{Payload: params})
	if err != nil {
		t.Fatalf("handleCreate: %v", err)
	}
	sess := result.(*Session)
	if sess.ProjectID != "proj-1" || sess.ProjectPath != "/repos/resolved" {
		t.Fatalf("binding = (%q, %q), want (proj-1, /repos/resolved)", sess.ProjectID, sess.ProjectPath)
	}
	if dir, src := ResolveWorkingDir(sess); dir != "/repos/resolved" || src != WorkingDirFromProject {
		t.Errorf("ResolveWorkingDir() = (%q, %q), want the bound project", dir, src)
	}
}

// TestSessionCreate_RPC_BindsExplicitProject: an explicit project_id is
// resolved through the resolver so the session's project_path carries the
// directory.
func TestSessionCreate_RPC_BindsExplicitProject(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	handler := NewHandler(store, nil, slog.Default())
	handler.SetProjectResolver(stubProjectResolver{get: &ProjectBinding{ID: "proj-x", Path: "/repos/x"}})

	params, _ := json.Marshal(map[string]any{"name": "sess", "project_id": "proj-x"})
	result, err := handler.handleCreate(&models.BusMessage{Payload: params})
	if err != nil {
		t.Fatalf("handleCreate: %v", err)
	}
	sess := result.(*Session)
	if sess.ProjectID != "proj-x" || sess.ProjectPath != "/repos/x" {
		t.Fatalf("binding = (%q, %q), want (proj-x, /repos/x)", sess.ProjectID, sess.ProjectPath)
	}
}

// TestSessionCreate_RPC_UnboundNeverUsesActiveProject: with a resolver wired
// but no project_id and no client CWD, the session is UNBOUND — never the
// global active project, never EnsureDefault — and a WARN names why.
func TestSessionCreate_RPC_UnboundNeverUsesActiveProject(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store := NewMemoryStore(logger)
	handler := NewHandler(store, nil, logger)
	// The resolver would happily answer, but nothing in the request asks for
	// a project and there is no CWD, so it must not be consulted.
	handler.SetProjectResolver(stubProjectResolver{get: &ProjectBinding{ID: "active-ish", Path: "/repos/active"}})

	params, _ := json.Marshal(map[string]string{"name": "unbound"})
	result, err := handler.handleCreate(&models.BusMessage{Payload: params})
	if err != nil {
		t.Fatalf("handleCreate: %v", err)
	}
	sess := result.(*Session)
	if sess.ProjectID != "" || sess.ProjectPath != "" {
		t.Fatalf("expected an UNBOUND session, got (%q, %q)", sess.ProjectID, sess.ProjectPath)
	}
	if dir, src := ResolveWorkingDir(sess); dir != "" || src != WorkingDirFromNone {
		t.Errorf("ResolveWorkingDir() = (%q, %q), want nothing resolved", dir, src)
	}
	if reloaded := store.Get(sess.ID); reloaded == nil || reloaded.ProjectID != "" {
		t.Errorf("store reload = %+v, want no project bound", reloaded)
	}
	logged := buf.String()
	if !strings.Contains(logged, "level=WARN") || !strings.Contains(logged, "no working directory") {
		t.Errorf("expected a WARN naming the missing working directory, got:\n%s", logged)
	}
}

// TestResolveWorkingDir_PerSessionProjectIsolation: two sessions bound to
// different projects each resolve their own, and a third unbound session
// resolves nothing — no global active project exists to leak in.
func TestResolveWorkingDir_PerSessionProjectIsolation(t *testing.T) {
	a := &Session{ID: "a", ProjectID: "pa", ProjectPath: "/repos/a"}
	b := &Session{ID: "b", ProjectID: "pb", ProjectPath: "/repos/b"}
	unbound := &Session{ID: "u"}

	if dir, src := ResolveWorkingDir(a); dir != "/repos/a" || src != WorkingDirFromProject {
		t.Errorf("session a = (%q, %q)", dir, src)
	}
	if dir, src := ResolveWorkingDir(b); dir != "/repos/b" || src != WorkingDirFromProject {
		t.Errorf("session b = (%q, %q)", dir, src)
	}
	if dir, src := ResolveWorkingDir(unbound); dir != "" || src != WorkingDirFromNone {
		t.Errorf("unbound session = (%q, %q), want nothing", dir, src)
	}
}
