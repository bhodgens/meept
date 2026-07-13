package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
)

func newTestProjectHandler(t *testing.T) (*ProjectHandler, *project.ProjectManager) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := project.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.ProjectsConfig{
		BaseDir:       filepath.Join(dir, "projects"),
		DefaultBranch: "main",
	}
	os.MkdirAll(cfg.BaseDir, 0o755)

	pm := project.NewProjectManager(store, nil, cfg, nil)
	sessionStore := session.NewMemoryStore(nil)
	h := NewProjectHandler(pm, sessionStore)
	h.SetMessageBus(bus.New(nil, nil))
	return h, pm
}

func TestHandleSet_ShorthandName_CreatesProject(t *testing.T) {
	h, pm := newTestProjectHandler(t)
	ctx := context.Background()

	// Create a session to bind to using the handler's session store.
	sess, _ := h.sessionStore.Create("test")

	params, _ := json.Marshal(map[string]string{
		"session_id": sess.ID,
		"path":       "myapp",
	})

	result, err := h.handleSet(ctx, params)
	if err != nil {
		t.Fatalf("handleSet: %v", err)
	}

	m := result.(map[string]any)
	projectID, _ := m["project_id"].(string)
	if projectID == "" {
		t.Fatal("expected non-empty project_id")
	}

	// Project should exist under base_dir.
	p, err := pm.Get(ctx, projectID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	expectedPath := filepath.Join(pm.Config().BaseDir, "myapp")
	if p.LocalPath != expectedPath {
		t.Errorf("LocalPath = %q, want %q", p.LocalPath, expectedPath)
	}
}

func TestHandleSet_DeactivatesPreviousActive(t *testing.T) {
	h, pm := newTestProjectHandler(t)
	ctx := context.Background()

	sess, _ := h.sessionStore.Create("test")

	// Create a first project and set it active.
	p1, _ := pm.CreateOrResolve(ctx, "first")
	_ = p1

	// Set first project.
	params1, _ := json.Marshal(map[string]string{
		"session_id": sess.ID,
		"path":       "first",
	})
	h.handleSet(ctx, params1)

	// Set second project.
	params2, _ := json.Marshal(map[string]string{
		"session_id": sess.ID,
		"path":       "second",
	})
	h.handleSet(ctx, params2)

	// Only one active project should exist.
	active, err := pm.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if active == nil {
		t.Fatal("expected an active project")
	}
}
