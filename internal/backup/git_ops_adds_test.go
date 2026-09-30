package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pins the L10 fix: when files were expected but EVERY Add failed, the
// clean-tree shortcut must return an error naming the failed files instead
// of reporting backup success with no commit.
func TestGitAddCommitPush_AllAddsFailed(t *testing.T) {
	repo, dir := helperCreateTestRepo(t, false)

	// Seed one commit so HEAD exists (a bare init has none).
	seed := filepath.Join(dir, "seed.txt")
	if err := os.WriteFile(seed, []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	if err := GitAddCommitPush(repo, []string{seed}, "seed"); err != nil {
		t.Fatalf("seed commit: %v", err)
	}

	// Every requested file is outside the repo: relToRepo passes them
	// through verbatim and Worktree.Add fails "entry not found" for each.
	// Pre-fix this silently returned success (clean tree, no commit).
	missing := []string{
		filepath.Join(t.TempDir(), "outside-a.txt"),
		filepath.Join(t.TempDir(), "outside-b.txt"),
	}
	for _, m := range missing {
		if err := os.WriteFile(m, []byte("data\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", m, err)
		}
	}

	err := GitAddCommitPush(repo, missing, "backup: should fail")
	if err == nil {
		t.Fatal("GitAddCommitPush with all adds failed: want error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to stage") {
		t.Errorf("error %q does not name the staging failure", err)
	}
	for _, m := range missing {
		if !strings.Contains(err.Error(), filepath.Base(m)) {
			t.Errorf("error %q does not list failed file %s", err, filepath.Base(m))
		}
	}
}

// Partial add failure (some files staged fine) keeps the legacy behavior:
// the commit proceeds with whatever staged.
func TestGitAddCommitPush_PartialAddFailureCommits(t *testing.T) {
	repo, dir := helperCreateTestRepo(t, false)

	seed := filepath.Join(dir, "seed.txt")
	if err := os.WriteFile(seed, []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	if err := GitAddCommitPush(repo, []string{seed}, "seed"); err != nil {
		t.Fatalf("seed commit: %v", err)
	}

	// One good (in-repo) file + one bad (outside repo) file.
	good := filepath.Join(dir, "backup.txt")
	if err := os.WriteFile(good, []byte("backup data\n"), 0o644); err != nil {
		t.Fatalf("write good: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(bad, []byte("data\n"), 0o644); err != nil {
		t.Fatalf("write bad: %v", err)
	}

	if err := GitAddCommitPush(repo, []string{good, bad}, "backup: partial"); err != nil {
		t.Fatalf("GitAddCommitPush with partial add failure: %v", err)
	}
}
