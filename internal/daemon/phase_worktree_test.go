package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTempRepo creates a tiny git repo (initial commit on main) for
// worktree provisioning tests.
func initTempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

func TestPhaseWorktreeProvisioner_BranchesFromRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := initTempRepo(t)
	root := filepath.Join(t.TempDir(), "phase-worktrees")
	p := phaseWorktreeProvisioner(root, func(context.Context) string { return repo }, slog.Default())

	wt, err := p(context.Background(), "task-1", "ph-1", "Implementation")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if wt == "" {
		t.Fatal("provision returned empty path for a git repo")
	}
	if !strings.Contains(filepath.Base(wt), "task-1-implementation") {
		t.Errorf("worktree basename = %q, want task-1-implementation slug", filepath.Base(wt))
	}
	// Isolated checkout exists and shares the repo history.
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Errorf("worktree missing README: %v", err)
	}
	out, err := exec.Command("git", "-C", wt, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse in worktree: %v", err)
	}
	headInRepo, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse in repo: %v", err)
	}
	if strings.TrimSpace(string(out)) != strings.TrimSpace(string(headInRepo)) {
		t.Errorf("worktree HEAD %s != repo HEAD %s", out, headInRepo)
	}

	// Idempotent: second call for the same phase returns the same path.
	wt2, err := p(context.Background(), "task-1", "ph-1", "Implementation")
	if err != nil {
		t.Fatalf("re-provision: %v", err)
	}
	if wt2 != wt {
		t.Errorf("re-provision path = %q, want %q", wt2, wt)
	}
}

func TestPhaseWorktreeProvisioner_Degradations(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	// Non-git project: empty path, nil error (run shared).
	nonGit := t.TempDir()
	p := phaseWorktreeProvisioner(filepath.Join(t.TempDir(), "wt"), func(context.Context) string { return nonGit }, slog.Default())
	wt, err := p(context.Background(), "task-1", "ph-1", "Build")
	if err != nil {
		t.Fatalf("non-git repo should degrade to no worktree, got error: %v", err)
	}
	if wt != "" {
		t.Errorf("non-git repo worktree = %q, want empty", wt)
	}

	// No project: same.
	p2 := phaseWorktreeProvisioner(filepath.Join(t.TempDir(), "wt"), func(context.Context) string { return "" }, slog.Default())
	wt2, err := p2(context.Background(), "task-1", "ph-1", "Build")
	if err != nil || wt2 != "" {
		t.Errorf("no-project = (%q, %v), want (\"\", nil)", wt2, err)
	}
}
