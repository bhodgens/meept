package rpc

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestHandleRename(t *testing.T) {
	h, pm := newTestProjectHandler(t)
	ctx := context.Background()

	// Create a project via shorthand.
	p, err := pm.CreateOrResolve(ctx, "oldname")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}

	params, _ := json.Marshal(map[string]string{
		"id":       p.ID,
		"new_name": "newname",
	})

	result, err := h.handleRename(ctx, params)
	if err != nil {
		t.Fatalf("handleRename: %v", err)
	}

	m := result.(map[string]any)
	name, _ := m["name"].(string)
	if name != "newname" {
		t.Errorf("name = %q, want %q", name, "newname")
	}
	path, _ := m["local_path"].(string)
	expectedPath := filepath.Join(pm.Config().BaseDir, "newname")
	if path != expectedPath {
		t.Errorf("local_path = %q, want %q", path, expectedPath)
	}
}

// initGitRepoForRPC creates a minimal git repo with an initial commit.
func initGitRepoForRPC(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, string(out), err)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	f, err := os.Create(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	run("add", ".")
	run("commit", "-m", "initial")
}

// setupGitProject creates a git-backed project suitable for worktree creation.
// Returns the project ID.
func setupGitProject(t *testing.T, h *ProjectHandler) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0o755)
	initGitRepoForRPC(t, repoDir)

	pm := h.pm
	// RegisterGit clones the source repo and sets mode=git, which is required
	// for worktree creation.
	p, err := pm.RegisterGit(ctx, "wt-proj", "test-proj", repoDir)
	if err != nil {
		t.Fatalf("RegisterGit: %v", err)
	}
	return p.ID
}

// TestWorktreeCreate_HappyPath tests successful worktree creation.
func TestWorktreeCreate_HappyPath(t *testing.T) {
	h, _ := newTestProjectHandler(t)
	ctx := context.Background()

	projectID := setupGitProject(t, h)

	sess, _ := h.sessionStore.Create("test")

	params, _ := json.Marshal(map[string]string{
		"session_id": sess.ID,
		"project_id": projectID,
	})

	result, err := h.handleWorktreeCreate(ctx, params)
	if err != nil {
		t.Fatalf("handleWorktreeCreate: %v", err)
	}

	m := result.(map[string]any)
	worktreeID, _ := m["worktree_id"].(string)
	if worktreeID == "" {
		t.Fatal("expected non-empty worktree_id")
	}
	path, _ := m["path"].(string)
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	branch, _ := m["branch"].(string)
	if branch == "" {
		t.Fatal("expected non-empty branch")
	}
	if !strings.HasPrefix(branch, "session/") {
		t.Errorf("branch = %q, want prefix %q", branch, "session/")
	}

	// Verify worktree path exists on disk.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("worktree path %s does not exist", path)
	}

	// Verify worktree was bound to session.
	updated := h.sessionStore.Get(sess.ID)
	if updated == nil {
		t.Fatal("session not found after worktree create")
	}
	if updated.WorktreeID != worktreeID {
		t.Errorf("session worktree_id = %q, want %q", updated.WorktreeID, worktreeID)
	}
}

// TestWorktreeCreate_MissingSessionID tests that an empty session_id returns an error.
func TestWorktreeCreate_MissingSessionID(t *testing.T) {
	h, _ := newTestProjectHandler(t)
	ctx := context.Background()

	params, _ := json.Marshal(map[string]string{
		"project_id": "some-project",
	})

	_, err := h.handleWorktreeCreate(ctx, params)
	if err == nil {
		t.Fatal("expected error for missing session_id, got nil")
	}
	if !strings.Contains(err.Error(), "session_id is required") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "session_id is required")
	}
}

// TestWorktreeCreate_SessionNotFound tests that a non-existent session returns an error.
func TestWorktreeCreate_SessionNotFound(t *testing.T) {
	h, _ := newTestProjectHandler(t)
	ctx := context.Background()

	params, _ := json.Marshal(map[string]string{
		"session_id": "nonexistent-session",
	})

	_, err := h.handleWorktreeCreate(ctx, params)
	if err == nil {
		t.Fatal("expected error for non-existent session, got nil")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "session not found")
	}
}

// TestWorktreeRemove_HappyPath tests successful worktree removal.
func TestWorktreeRemove_HappyPath(t *testing.T) {
	h, _ := newTestProjectHandler(t)
	ctx := context.Background()

	projectID := setupGitProject(t, h)
	sess, _ := h.sessionStore.Create("test")

	// Create a worktree first.
	createParams, _ := json.Marshal(map[string]string{
		"session_id": sess.ID,
		"project_id": projectID,
	})
	createResult, err := h.handleWorktreeCreate(ctx, createParams)
	if err != nil {
		t.Fatalf("handleWorktreeCreate: %v", err)
	}
	cm := createResult.(map[string]any)
	worktreePath, _ := cm["path"].(string)

	// Remove the worktree.
	removeParams, _ := json.Marshal(map[string]string{
		"session_id": sess.ID,
	})
	result, err := h.handleWorktreeRemove(ctx, removeParams)
	if err != nil {
		t.Fatalf("handleWorktreeRemove: %v", err)
	}

	m := result.(map[string]string)
	status := m[RPCKeyStatus]
	if status != "removed" {
		t.Errorf("status = %q, want %q", status, "removed")
	}

	// Verify session's worktree was cleared.
	updated := h.sessionStore.Get(sess.ID)
	if updated == nil {
		t.Fatal("session not found after worktree remove")
	}
	if updated.WorktreeID != "" {
		t.Errorf("session worktree_id = %q, want empty", updated.WorktreeID)
	}

	// Verify worktree path was cleaned from disk.
	if worktreePath != "" {
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Errorf("worktree path %s should have been removed", worktreePath)
		}
	}
}

// TestWorktreeRemove_MissingSessionID tests that an empty session_id returns an error.
func TestWorktreeRemove_MissingSessionID(t *testing.T) {
	h, _ := newTestProjectHandler(t)
	ctx := context.Background()

	params, _ := json.Marshal(map[string]string{})

	_, err := h.handleWorktreeRemove(ctx, params)
	if err == nil {
		t.Fatal("expected error for missing session_id, got nil")
	}
	if !strings.Contains(err.Error(), "session_id is required") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "session_id is required")
	}
}

// TestWorktreeRemove_SessionNotFound tests that a non-existent session returns an error.
func TestWorktreeRemove_SessionNotFound(t *testing.T) {
	h, _ := newTestProjectHandler(t)
	ctx := context.Background()

	params, _ := json.Marshal(map[string]string{
		"session_id": "nonexistent-session",
	})

	_, err := h.handleWorktreeRemove(ctx, params)
	if err == nil {
		t.Fatal("expected error for non-existent session, got nil")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "session not found")
	}
}
