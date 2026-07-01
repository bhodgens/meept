package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func newWorktreeTestManager(t *testing.T) (*ProjectManager, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.ProjectsConfig{
		BaseDir:                    filepath.Join(dir, "projects"),
		DefaultBranch:              "main",
		WorktreePerPlan:            "auto",
		WorktreeIsolationThreshold: 5,
		MaxWorktreesPerProject:     10,
	}

	pm := NewProjectManager(store, nil, cfg, nil)

	// Create a git repo for testing
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0o755)
	initGitRepo(t, repoDir)

	// Register it as a git project
	_, err = pm.RegisterLocal(ctx(), "wt-proj", "test-proj", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	// Manually set mode to git since RegisterLocal sets local
	p, _ := store.GetProject(ctx(), "wt-proj")
	p.Mode = ModeGit
	p.GitURL = repoDir
	// Determine current branch from the repo
	branch, _ := pm.gitOutput(ctx(), repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	p.Branch = strings.TrimSpace(branch)
	if p.Branch == "" {
		p.Branch = "main"
	}
	store.UpdateProject(ctx(), p)

	return pm, repoDir
}

func ctx() context.Context {
	return context.Background()
}

func TestCreateWorktree(t *testing.T) {
	pm, _ := newWorktreeTestManager(t)

	w, err := pm.CreateWorktree(ctx(), "wt-proj", "sess-1", "")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if w.Branch != "session/sess-1" {
		t.Errorf("Branch = %q, want %q", w.Branch, "session/sess-1")
	}
	if w.Status != "active" {
		t.Errorf("Status = %q, want %q", w.Status, "active")
	}
	if _, err := os.Stat(w.Path); os.IsNotExist(err) {
		t.Errorf("worktree path %s does not exist", w.Path)
	}
}

func TestCreateWorktreeWithPlan(t *testing.T) {
	pm, _ := newWorktreeTestManager(t)

	w, err := pm.CreateWorktree(ctx(), "wt-proj", "", "plan-42")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if w.Branch != "plan/plan-42" {
		t.Errorf("Branch = %q, want %q", w.Branch, "plan/plan-42")
	}
}

func TestReleaseWorktree(t *testing.T) {
	pm, _ := newWorktreeTestManager(t)

	w, err := pm.CreateWorktree(ctx(), "wt-proj", "sess-release", "")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if err := pm.ReleaseWorktree(ctx(), w.ID); err != nil {
		t.Fatalf("ReleaseWorktree: %v", err)
	}

	got, err := pm.store.GetWorktree(ctx(), w.ID)
	if err != nil {
		t.Fatalf("GetWorktree: %v", err)
	}
	if got.Status != "cleaned" {
		t.Errorf("Status = %q, want %q", got.Status, "cleaned")
	}
}

func TestMergeWorktree(t *testing.T) {
	pm, _ := newWorktreeTestManager(t)

	// Create a worktree
	w, err := pm.CreateWorktree(ctx(), "wt-proj", "sess-merge", "")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Make a commit in the worktree
	f, err := os.Create(filepath.Join(w.Path, "new-file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("hello from worktree")
	f.Close()

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = w.Path
	cmd.CombinedOutput()
	cmd = exec.Command("git", "commit", "-m", "worktree change")
	cmd.Dir = w.Path
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_EMAIL=test@test.com", "GIT_AUTHOR_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com", "GIT_COMMITTER_NAME=Test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit in worktree: %s: %v", string(out), err)
	}

	// Get the branch we're on in the main repo (may be master or main)
	p, _ := pm.store.GetProject(ctx(), "wt-proj")

	// Merge the worktree branch into current branch
	if err := pm.MergeWorktree(ctx(), w.ID, p.Branch); err != nil {
		t.Fatalf("MergeWorktree: %v", err)
	}

	// Verify the file exists in main repo
	if _, err := os.Stat(filepath.Join(p.LocalPath, "new-file.txt")); os.IsNotExist(err) {
		t.Error("merged file should exist in main repo")
	}

	// Clean up
	pm.ReleaseWorktree(ctx(), w.ID)
}

func TestGetActiveWorktree(t *testing.T) {
	pm, _ := newWorktreeTestManager(t)

	// No worktree yet
	_, err := pm.GetActiveWorktree(ctx(), "sess-nope")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	w, err := pm.CreateWorktree(ctx(), "wt-proj", "sess-active", "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := pm.GetActiveWorktree(ctx(), "sess-active")
	if err != nil {
		t.Fatalf("GetActiveWorktree: %v", err)
	}
	if got.ID != w.ID {
		t.Errorf("ID = %q, want %q", got.ID, w.ID)
	}
}

func TestShouldIsolatePlan(t *testing.T) {
	pm, _ := newWorktreeTestManager(t)

	// Default config: auto, threshold 5
	tests := []struct {
		fileCount int
		want      bool
	}{
		{0, false},
		{4, false},
		{5, true},
		{10, true},
	}
	for _, tc := range tests {
		got := pm.ShouldIsolatePlan(tc.fileCount, "edit")
		if got != tc.want {
			t.Errorf("ShouldIsolatePlan(%d) = %v, want %v", tc.fileCount, got, tc.want)
		}
	}
}

func TestShouldIsolatePlanAlways(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, _ := NewStore(dbPath, nil)
	defer store.Close()

	cfg := config.ProjectsConfig{
		BaseDir:         filepath.Join(dir, "projects"),
		WorktreePerPlan: "always",
	}
	pm := NewProjectManager(store, nil, cfg, nil)

	if !pm.ShouldIsolatePlan(0, "edit") {
		t.Error("ShouldIsolatePlan(0) with 'always' = false, want true")
	}
}

func TestShouldIsolatePlanNever(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, _ := NewStore(dbPath, nil)
	defer store.Close()

	cfg := config.ProjectsConfig{
		BaseDir:         filepath.Join(dir, "projects"),
		WorktreePerPlan: "never",
	}
	pm := NewProjectManager(store, nil, cfg, nil)

	if pm.ShouldIsolatePlan(100, "edit") {
		t.Error("ShouldIsolatePlan(100) with 'never' = true, want false")
	}
}

func TestCountActiveWorktrees(t *testing.T) {
	pm, _ := newWorktreeTestManager(t)

	count, err := pm.CountActiveWorktrees(ctx(), "wt-proj")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	pm.CreateWorktree(ctx(), "wt-proj", "sess-1", "")
	pm.CreateWorktree(ctx(), "wt-proj", "sess-2", "")

	count, _ = pm.CountActiveWorktrees(ctx(), "wt-proj")
	if count != 2 {
		t.Errorf("count after 2 = %d, want 2", count)
	}
}

func TestCreateWorktreeNonGitProject(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, _ := NewStore(dbPath, nil)
	defer store.Close()

	cfg := config.ProjectsConfig{BaseDir: filepath.Join(dir, "projects")}
	pm := NewProjectManager(store, nil, cfg, nil)

	// Register a local (non-git) project
	pm.RegisterLocal(ctx(), "local-1", "local-proj", "/tmp/local")

	_, err := pm.CreateWorktree(ctx(), "local-1", "sess-1", "")
	if err == nil {
		t.Error("expected error creating worktree on non-git project")
	}
}
