package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func newTestManager(t *testing.T) (*ProjectManager, *Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.ProjectsConfig{
		BaseDir:        filepath.Join(dir, "projects"),
		DefaultBranch:  "main",
		WorktreePerPlan: "auto",
	}
	os.MkdirAll(cfg.BaseDir, 0o755)

	pm := NewProjectManager(store, cfg, nil)
	return pm, store
}

func initGitRepo(t *testing.T, dir string) {
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
	// Need an initial commit
	f, err := os.Create(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	run("add", ".")
	run("commit", "-m", "initial")
}

func TestRegisterLocal(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	p, err := pm.RegisterLocal(ctx, "local-1", "my-local", "/tmp/my-local")
	if err != nil {
		t.Fatalf("RegisterLocal: %v", err)
	}
	if p.Mode != ModeLocal {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeLocal)
	}
	if p.Name != "my-local" {
		t.Errorf("Name = %q, want %q", p.Name, "my-local")
	}

	got, err := pm.Get(ctx, "local-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "local-1" {
		t.Errorf("ID = %q, want %q", got.ID, "local-1")
	}
}

func TestRegisterGit(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// Create a "remote" repo to clone from
	remoteDir := filepath.Join(t.TempDir(), "remote")
	os.MkdirAll(remoteDir, 0o755)
	initGitRepo(t, remoteDir)

	p, err := pm.RegisterGit(ctx, "git-1", "my-git", remoteDir)
	if err != nil {
		t.Fatalf("RegisterGit: %v", err)
	}
	if p.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeGit)
	}
	if p.GitURL != remoteDir {
		t.Errorf("GitURL = %q, want %q", p.GitURL, remoteDir)
	}
	// Should have been cloned
	if _, err := os.Stat(p.LocalPath); os.IsNotExist(err) {
		t.Errorf("LocalPath %s does not exist", p.LocalPath)
	}
}

func TestUnregister(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	pm.RegisterLocal(ctx, "u1", "temp", "/tmp/temp")
	if err := pm.Unregister(ctx, "u1"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	_, err := pm.Get(ctx, "u1")
	if err != ErrNotFound {
		t.Errorf("after unregister: error = %v, want ErrNotFound", err)
	}
}

func TestList(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	pm.RegisterLocal(ctx, "l1", "p1", "/tmp/p1")
	pm.RegisterLocal(ctx, "l2", "p2", "/tmp/p2")

	projects, err := pm.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("len(projects) = %d, want 2", len(projects))
	}
}

func TestDetectFromPath(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// Create a git repo in a temp dir
	repoDir := filepath.Join(t.TempDir(), "my-repo")
	os.MkdirAll(repoDir, 0o755)
	initGitRepo(t, repoDir)

	// Create a subdirectory to detect from
	subDir := filepath.Join(repoDir, "src", "pkg")
	os.MkdirAll(subDir, 0o755)

	p, err := pm.DetectFromPath(ctx, subDir)
	if err != nil {
		t.Fatalf("DetectFromPath: %v", err)
	}
	if p.Name != "my-repo" {
		t.Errorf("Name = %q, want %q", p.Name, "my-repo")
	}
	if p.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeGit)
	}
	if p.LocalPath != repoDir {
		t.Errorf("LocalPath = %q, want %q", p.LocalPath, repoDir)
	}

	// Detecting again should return the same project (not create duplicate)
	p2, err := pm.DetectFromPath(ctx, subDir)
	if err != nil {
		t.Fatalf("DetectFromPath (2nd): %v", err)
	}
	if p2.ID != p.ID {
		t.Errorf("2nd detection ID = %q, want %q", p2.ID, p.ID)
	}
}

func TestDetectFromPathNoGit(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	noGitDir := t.TempDir()
	_, err := pm.DetectFromPath(ctx, noGitDir)
	if err == nil {
		t.Error("expected error when no git repo found")
	}
}

func TestStatus(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	repoDir := filepath.Join(t.TempDir(), "status-repo")
	os.MkdirAll(repoDir, 0o755)
	initGitRepo(t, repoDir)

	p, err := pm.RegisterLocal(ctx, "status-1", "status-proj", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	p.Mode = ModeGit
	pm.store.UpdateProject(ctx, p)

	status, err := pm.Status(ctx, "status-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Branch == "" {
		t.Error("expected non-empty branch")
	}
	if status.Dirty {
		t.Error("clean repo should not be dirty")
	}
}
