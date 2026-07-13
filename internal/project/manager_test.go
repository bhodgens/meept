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
		BaseDir:         filepath.Join(dir, "projects"),
		DefaultBranch:   "main",
		WorktreePerPlan: "auto",
	}
	os.MkdirAll(cfg.BaseDir, 0o755)

	pm := NewProjectManager(store, nil, cfg, nil)
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

// TestCheckoutBranch_RejectsDashPrefixedNames verifies the option-injection
// guard refuses branch names beginning with '-'.
func TestCheckoutBranch_RejectsDashPrefixedNames(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// Create a fake git project so we get past the mode check.
	dir := t.TempDir()
	if err := exec.CommandContext(ctx, "git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := pm.store.CreateProject(ctx, &Project{
		ID:        "p1",
		Name:      "p1",
		Mode:      ModeGit,
		LocalPath: dir,
		Status:    "active",
	}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	for _, branch := range []string{"-b", "--orphan"} {
		if err := pm.CheckoutBranch(ctx, "p1", branch); err == nil {
			t.Errorf("CheckoutBranch(%q) succeeded; want error", branch)
		}
	}

	// Empty branch must also be rejected.
	if err := pm.CheckoutBranch(ctx, "p1", ""); err == nil {
		t.Error("CheckoutBranch(\"\") succeeded; want error")
	}
}

// TestRegisterGit_RejectsDashPrefixedURL verifies the URL guard refuses
// clone URLs beginning with '-'.
func TestRegisterGit_RejectsDashPrefixedURL(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()
	for _, url := range []string{"-x", "--upload-pack=evil"} {
		if _, err := pm.RegisterGit(ctx, "x"+url, "n", url); err == nil {
			t.Errorf("RegisterGit(%q) succeeded; want error", url)
		}
	}
}

func TestManager_WriteAndReadSidecar(t *testing.T) {
	pm, _ := newTestManager(t)
	dir := t.TempDir()

	// No sidecar yet.
	id, err := pm.ReadSidecarID(dir)
	if err != nil {
		t.Fatalf("ReadSidecarID on empty dir: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty ID, got %q", id)
	}

	// Write sidecar.
	if err := pm.WriteSidecarID(dir, "abc-123"); err != nil {
		t.Fatalf("WriteSidecarID: %v", err)
	}

	// Read it back.
	id, err = pm.ReadSidecarID(dir)
	if err != nil {
		t.Fatalf("ReadSidecarID: %v", err)
	}
	if id != "abc-123" {
		t.Errorf("expected %q, got %q", "abc-123", id)
	}
}

func TestManager_GetActive_None(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	p, err := pm.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil, got %+v", p)
	}
}

func TestManager_GetActive_Exists(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	p := &Project{
		ID: "active-1", Name: "alpha", Mode: ModeGit,
		LocalPath: "/tmp/alpha", Status: "active",
	}
	if err := store.CreateProject(ctx, p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := pm.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if got == nil || got.ID != "active-1" {
		t.Fatalf("expected active-1, got %+v", got)
	}
}

func TestManager_DeactivateActive(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	p := &Project{
		ID: "a1", Name: "alpha", Mode: ModeGit,
		LocalPath: "/tmp/alpha", Status: "active",
	}
	store.CreateProject(ctx, p)

	if err := pm.DeactivateActive(ctx); err != nil {
		t.Fatalf("DeactivateActive: %v", err)
	}

	got, _ := pm.GetActive(ctx)
	if got != nil {
		t.Errorf("expected nil after deactivate, got %+v", got)
	}
}

func TestManager_EnsureDefault_CreatesProject(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	p, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil project")
	}
	if p.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeGit)
	}
	if p.LocalPath == "" {
		t.Error("LocalPath should not be empty")
	}
	// Directory should exist.
	if _, err := os.Stat(p.LocalPath); os.IsNotExist(err) {
		t.Errorf("project dir not created: %s", p.LocalPath)
	}
	// .git should exist.
	if _, err := os.Stat(filepath.Join(p.LocalPath, ".git")); os.IsNotExist(err) {
		t.Errorf(".git not initialized in %s", p.LocalPath)
	}
	// Sidecar should exist.
	id, err := pm.ReadSidecarID(p.LocalPath)
	if err != nil {
		t.Fatalf("ReadSidecarID: %v", err)
	}
	if id != p.ID {
		t.Errorf("sidecar ID = %q, want %q", id, p.ID)
	}
	// Name should equal filepath.Base(LocalPath).
	if p.Name != filepath.Base(p.LocalPath) {
		t.Errorf("Name = %q, want %q", p.Name, filepath.Base(p.LocalPath))
	}
}

func TestManager_EnsureDefault_DoesNotCreateWhenActiveExists(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	// Pre-create an active project.
	existing := &Project{
		ID: "existing", Name: "alpha", Mode: ModeGit,
		LocalPath: "/tmp/alpha", Status: "active",
	}
	store.CreateProject(ctx, existing)

	p, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if p.ID != "existing" {
		t.Errorf("expected existing, got %s", p.ID)
	}
}

func TestManager_CreateOrResolve_ShorthandName(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	p, err := pm.CreateOrResolve(ctx, "myapp")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}
	if p.Name != "myapp" {
		t.Errorf("Name = %q, want %q", p.Name, "myapp")
	}
	expectedPath := filepath.Join(pm.cfg.BaseDir, "myapp")
	if p.LocalPath != expectedPath {
		t.Errorf("LocalPath = %q, want %q", p.LocalPath, expectedPath)
	}
	if p.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeGit)
	}
	// Directory should exist.
	if _, err := os.Stat(p.LocalPath); os.IsNotExist(err) {
		t.Errorf("project dir not created: %s", p.LocalPath)
	}
	// .git should exist.
	if _, err := os.Stat(filepath.Join(p.LocalPath, ".git")); os.IsNotExist(err) {
		t.Errorf(".git not initialized")
	}
	// Sidecar should exist.
	sid, err := pm.ReadSidecarID(p.LocalPath)
	if err != nil {
		t.Fatalf("ReadSidecarID: %v", err)
	}
	if sid != p.ID {
		t.Errorf("sidecar = %q, want %q", sid, p.ID)
	}
}

func TestManager_CreateOrResolve_ExistingShorthand(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// First call creates the project.
	p1, err := pm.CreateOrResolve(ctx, "myapp")
	if err != nil {
		t.Fatalf("CreateOrResolve first: %v", err)
	}

	// Second call should return the same project (idempotent).
	p2, err := pm.CreateOrResolve(ctx, "myapp")
	if err != nil {
		t.Fatalf("CreateOrResolve second: %v", err)
	}
	if p2.ID != p1.ID {
		t.Errorf("second call returned different ID: %q vs %q", p2.ID, p1.ID)
	}
}

func TestManager_CreateOrResolve_AbsolutePathWithGit(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// Create a real git repo in a temp dir.
	repoDir := filepath.Join(t.TempDir(), "myrepo")
	os.MkdirAll(repoDir, 0o755)
	initGitRepo(t, repoDir)

	p, err := pm.CreateOrResolve(ctx, repoDir)
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}
	if p.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeGit)
	}
	if p.LocalPath != repoDir {
		t.Errorf("LocalPath = %q, want %q", p.LocalPath, repoDir)
	}
}

func TestManager_CreateOrResolve_AbsolutePathNoGit(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// A directory with no .git.
	localDir := filepath.Join(t.TempDir(), "localproj")
	os.MkdirAll(localDir, 0o755)

	p, err := pm.CreateOrResolve(ctx, localDir)
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}
	if p.Mode != ModeLocal {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeLocal)
	}
	if p.LocalPath != localDir {
		t.Errorf("LocalPath = %q, want %q", p.LocalPath, localDir)
	}
}

func TestManager_CreateOrResolve_AdoptsRenamedDir(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	// Create a project via shorthand.
	p, err := pm.CreateOrResolve(ctx, "original")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}

	// Simulate external rename: move the directory.
	renamedPath := filepath.Join(filepath.Dir(p.LocalPath), "renamed")
	if err := os.Rename(p.LocalPath, renamedPath); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Now resolve the renamed path — should adopt via sidecar.
	adopted, err := pm.CreateOrResolve(ctx, renamedPath)
	if err != nil {
		t.Fatalf("CreateOrResolve after rename: %v", err)
	}
	if adopted.ID != p.ID {
		t.Errorf("adopted ID = %q, want original %q", adopted.ID, p.ID)
	}
	if adopted.LocalPath != renamedPath {
		t.Errorf("adopted LocalPath = %q, want %q", adopted.LocalPath, renamedPath)
	}
	if adopted.Name != "renamed" {
		t.Errorf("adopted Name = %q, want %q", adopted.Name, "renamed")
	}

	// DB should reflect the new path.
	dbProj, err := store.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if dbProj.LocalPath != renamedPath {
		t.Errorf("DB LocalPath = %q, want %q", dbProj.LocalPath, renamedPath)
	}
}

func TestManager_CreateOrResolve_RejectsPathTraversal(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	for _, bad := range []string{"../evil", "foo/bar", "a/b", "..", "x/.."} {
		_, err := pm.CreateOrResolve(ctx, bad)
		if err == nil {
			t.Errorf("CreateOrResolve(%q) succeeded; want error", bad)
		}
	}
}

func TestManager_CreateOrResolve_RejectsEmptyName(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()
	_, err := pm.CreateOrResolve(ctx, "")
	if err == nil {
		t.Error("CreateOrResolve(\"\") succeeded; want error")
	}
}

func TestManager_Rename_UnderBaseDir(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// Create a project via shorthand.
	p, err := pm.CreateOrResolve(ctx, "oldname")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}

	// Rename it.
	renamed, err := pm.Rename(ctx, p.ID, "newname")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.Name != "newname" {
		t.Errorf("Name = %q, want %q", renamed.Name, "newname")
	}
	expectedPath := filepath.Join(pm.cfg.BaseDir, "newname")
	if renamed.LocalPath != expectedPath {
		t.Errorf("LocalPath = %q, want %q", renamed.LocalPath, expectedPath)
	}

	// Old directory should not exist.
	if _, err := os.Stat(p.LocalPath); !os.IsNotExist(err) {
		t.Errorf("old dir should not exist: %s", p.LocalPath)
	}
	// New directory should exist.
	if _, err := os.Stat(renamed.LocalPath); os.IsNotExist(err) {
		t.Errorf("new dir should exist: %s", renamed.LocalPath)
	}
	// Sidecar should still have the same ID.
	sid, _ := pm.ReadSidecarID(renamed.LocalPath)
	if sid != p.ID {
		t.Errorf("sidecar = %q, want %q", sid, p.ID)
	}
}

func TestManager_Rename_ExternalProjectFails(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	// Create a project NOT under base_dir.
	p := &Project{
		ID: "ext1", Name: "ext", Mode: ModeGit,
		LocalPath: "/tmp/ext-project", Status: "active",
	}
	store.CreateProject(ctx, p)

	_, err := pm.Rename(ctx, "ext1", "newname")
	if err == nil {
		t.Fatal("expected error renaming external project")
	}
	if !strings.Contains(err.Error(), "under base_dir") {
		t.Errorf("expected 'under base_dir' error, got: %v", err)
	}
}

func TestManager_Rename_RejectsPathTraversal(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// Create a project under base_dir.
	p, err := pm.CreateOrResolve(ctx, "validname")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}

	for _, bad := range []string{"../evil", "foo/bar", "..", "a/b"} {
		_, err := pm.Rename(ctx, p.ID, bad)
		if err == nil {
			t.Errorf("Rename(%q) succeeded; want error", bad)
		}
	}
}

func TestManager_ResolutionChain_InheritThenSwitch(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// First call to EnsureDefault creates a uuid project.
	p1, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault first: %v", err)
	}

	// Second call returns the same project (already active).
	p2, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault second: %v", err)
	}
	if p2.ID != p1.ID {
		t.Errorf("second EnsureDefault returned different project: %q vs %q", p2.ID, p1.ID)
	}

	// CreateOrResolve with a shorthand name deactivates the old and creates new.
	p3, err := pm.CreateOrResolve(ctx, "myapp")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}

	// Deactivate old, activate new.
	pm.DeactivateActive(ctx)
	pm.SetStatus(ctx, p3.ID, "active")

	// GetActive should now return the new project.
	active, _ := pm.GetActive(ctx)
	if active == nil || active.ID != p3.ID {
		t.Errorf("GetActive = %+v, want %s", active, p3.ID)
	}

	// EnsureDefault should return the new active project, not create another.
	p4, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault after switch: %v", err)
	}
	if p4.ID != p3.ID {
		t.Errorf("EnsureDefault returned %q, want %q", p4.ID, p3.ID)
	}
}
