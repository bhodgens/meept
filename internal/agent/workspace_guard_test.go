package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// configTestGitIdentity sets a repo-local user identity so the real
// `git commit` calls in these tests succeed on clean CI runners that carry
// no global git config (fatal: empty ident name). Repo-local config only:
// no global config, no env leaking between sites.
func configTestGitIdentity(t *testing.T, repo string) {
	t.Helper()
	for _, kv := range [][2]string{
		{"user.name", "Meept Test"},
		{"user.email", "meept-test@example.com"},
	} {
		cmd := exec.Command("git", "config", kv[0], kv[1])
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config %s in %s failed: %v, output: %s", kv[0], repo, err, string(out))
		}
	}
}

func TestWorkspaceGuard_NormalWorkspace(t *testing.T) {
	// Create a temp base directory
	baseDir, err := os.MkdirTemp("", "meept-workspace-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	// Create a workspace manager
	wm := NewWorkspaceManager(WorkspaceConfig{BaseDir: baseDir, AutoCommit: false}, nil)

	// Create a workspace
	workspace, err := wm.Create(context.Background(), "test-normal", "Normal workspace test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify workspace exists
	if _, err := os.Stat(workspace); os.IsNotExist(err) {
		t.Fatal("Workspace directory was not created")
	}

	// Verify it's a git repo rooted at the workspace
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse failed: %v, output: %s", err, string(out))
	}
	toplevel := strings.TrimSpace(string(out))
	toplevelClean, err := filepath.EvalSymlinks(toplevel)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks failed for toplevel: %v", err)
	}
	workspaceClean, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks failed for workspace: %v", err)
	}
	if toplevelClean != workspaceClean {
		t.Fatalf("workspace is not its own repo root: got %q, want %q", toplevelClean, workspaceClean)
	}

	// Test commit (repo-local identity: clean CI runners have no global
	// git config, so the commit would fail with an empty ident)
	configTestGitIdentity(t, workspace)
	err = wm.Commit(context.Background(), "test-normal", "test commit", nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}

func TestWorkspaceGuard_ParentRepoToplevelRefused(t *testing.T) {
	// Create a temp base directory
	baseDir, err := os.MkdirTemp("", "meept-workspace-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	// Create a parent git repo INSIDE the baseDir
	parentRepo := filepath.Join(baseDir, "parent-repo")
	if err := os.MkdirAll(parentRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = parentRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init in parent failed: %v, output: %s", err, string(out))
	}
	// Make an initial commit so git log works
	if err := os.WriteFile(filepath.Join(parentRepo, "initial.txt"), []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "initial.txt")
	cmd.Dir = parentRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add in parent failed: %v, output: %s", err, string(out))
	}
	configTestGitIdentity(t, parentRepo)
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = parentRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit in parent failed: %v, output: %s", err, string(out))
	}

	// Create a workspace manager with baseDir INSIDE the parent repo
	// This way, the workspace will be created under parentRepo/baseDir
	wmBaseDir := parentRepo
	wm := NewWorkspaceManager(WorkspaceConfig{BaseDir: wmBaseDir, AutoCommit: false}, nil)

	// Create should FAIL because the workspace will be nested under parent-repo
	_, err = wm.Create(context.Background(), "test-parent-refused", "Should be refused")
	if err == nil {
		t.Fatal("Create should have failed for workspace inside parent repo")
	}

	// Verify parent repo's HEAD did not move
	cmd = exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = parentRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log in parent failed: %v, output: %s", err, string(out))
	}
	// The parent repo should have only its initial commit, not the "Initial workspace setup" commit
	if strings.Contains(string(out), "Initial workspace setup") {
		t.Fatal("Parent repo was incorrectly modified")
	}
}

func TestWorkspaceGuard_GITCeilingDirectoriesPreventsWalkUp(t *testing.T) {
	// Create a temp base directory
	baseDir, err := os.MkdirTemp("", "meept-workspace-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	// Create a parent git repo INSIDE the baseDir
	parentRepo := filepath.Join(baseDir, "parent-repo")
	if err := os.MkdirAll(parentRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = parentRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init in parent failed: %v, output: %s", err, string(out))
	}
	// Make an initial commit so git log works
	if err := os.WriteFile(filepath.Join(parentRepo, "initial.txt"), []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "initial.txt")
	cmd.Dir = parentRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add in parent failed: %v, output: %s", err, string(out))
	}
	configTestGitIdentity(t, parentRepo)
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = parentRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit in parent failed: %v, output: %s", err, string(out))
	}

	// Create a workspace manager with baseDir INSIDE the parent repo
	wmBaseDir := parentRepo
	wm := NewWorkspaceManager(WorkspaceConfig{BaseDir: wmBaseDir, AutoCommit: false}, nil)

	// Create should FAIL
	_, err = wm.Create(context.Background(), "test-ceiling", "Ceiling test")
	if err == nil {
		t.Fatal("Create should have failed due to GIT_CEILING_DIRECTORIES preventing walk-up")
	}

	// Verify parent repo's HEAD did not move
	cmd = exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = parentRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log in parent failed: %v, output: %s", err, string(out))
	}
	if strings.Contains(string(out), "Initial workspace setup") {
		t.Fatal("Parent repo was incorrectly modified by git add -A walk-up")
	}
}

func TestWorkspaceGuard_GitCmdWithCeiling(t *testing.T) {
	// Create a temp base directory
	baseDir, err := os.MkdirTemp("", "meept-workspace-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	// Create a parent git repo
	parentRepo := filepath.Join(baseDir, "parent-repo")
	if err := os.MkdirAll(parentRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = parentRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init in parent failed: %v, output: %s", err, string(out))
	}

	// Create a file in the parent repo to verify walk-up is blocked
	testFile := filepath.Join(parentRepo, "parent-file.txt")
	if err := os.WriteFile(testFile, []byte("parent file"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a workspace INSIDE the parent repo
	workspace := filepath.Join(parentRepo, "my-workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	wm := NewWorkspaceManager(WorkspaceConfig{BaseDir: baseDir, AutoCommit: false}, nil)

	// Register the workspace manually (since Create will fail)
	wm.mu.Lock()
	wm.workspaces["test-ceiling-gitcmd"] = workspace
	wm.mu.Unlock()

	// Try to run git add -A in the workspace - it should fail because
	// GIT_CEILING_DIRECTORIES prevents walking up to parent repo
	ok, output := wm.gitCmd(context.Background(), workspace, "add", "-A")
	if ok {
		t.Fatal("git add -A should have failed due to GIT_CEILING_DIRECTORIES")
	}
	// The output should contain an error about not being in a git repo or similar
	t.Logf("git add -A output: %s", output)
}
