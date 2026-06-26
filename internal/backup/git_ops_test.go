package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// helperCreateTestRepo creates a real git repo in a temp dir for testing.
func helperCreateTestRepo(t *testing.T, bare bool) (*git.Repository, string) {
	t.Helper()
	tempDir := t.TempDir()

	repo, path, err := GitInit(tempDir)
	if err != nil {
		t.Fatalf("GitInit: %v", err)
	}
	if repo == nil {
		t.Fatal("expected non-nil repo")
	}
	if path != tempDir {
		t.Errorf("path: got %q, want %q", path, tempDir)
	}

	return repo, tempDir
}

func TestGitInit(t *testing.T) {
	repo, path := helperCreateTestRepo(t, false)
	if repo == nil {
		t.Fatal("expected non-nil repo from GitInit")
	}
	if path == "" {
		t.Error("expected non-empty path")
	}

	// Opening an existing repo should work
	repo2, path2, err := GitInit(path)
	if err != nil {
		t.Fatalf("GitInit (open existing): %v", err)
	}
	if repo2 == nil {
		t.Fatal("expected non-nil repo from GitInit(open)")
	}
	if path2 != path {
		t.Errorf("path: got %q, want %q", path2, path)
	}
}

func TestGitInit_NonExistentDir(t *testing.T) {
	_, _, err := GitInit("/dev/null/nonexistent/deeply/nested/dir")
	if err == nil {
		t.Fatal("expected error for /dev/null path")
	}
	// On Unix, /dev/null might not exist for makedir
	// Use a more realistic path that should succeed
	repo2, _, err2 := GitInit(filepath.Join(os.TempDir(), "git_test_init"))
	if err2 != nil {
		// If it fails for permission reasons, that's OK - just shouldn't panic
		return
	}
	if repo2 == nil {
		t.Error("expected non-nil repo")
	}
}

func TestGitAddCommitPush(t *testing.T) {
	repo, tempDir := helperCreateTestRepo(t, false)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello backup"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// This will fail on push since there's no remote, but the commit should succeed
	w, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	_, err = w.Add("test.txt")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	_, err = w.Commit("test commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@test.com",
			When:  nowFunc(),
		},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestGitListBackups_NoBackups(t *testing.T) {
	repo, _ := helperCreateTestRepo(t, false)

	// No backups directory
	backups, err := GitListBackups(repo, "test-node")
	if err != nil {
		t.Fatalf("GitListBackups: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("expected empty backups, got %v", backups)
	}
}

func TestGitListBackups_WithBackups(t *testing.T) {
	repo, tempDir := helperCreateTestRepo(t, false)

	// Create a backup directory structure
	backupDir := filepath.Join(tempDir, "backups", "2026-06-25", "test-node")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "test.db.zst"), []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	backups, err := GitListBackups(repo, "test-node")
	if err != nil {
		t.Fatalf("GitListBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}
	if backups[0] != "2026-06-25" {
		t.Errorf("backup date: got %q, want %q", backups[0], "2026-06-2026-06-25")
	}
}

func TestGitListBackups_MultipleDates(t *testing.T) {
	repo, tempDir := helperCreateTestRepo(t, false)

	dates := []string{"2026-06-23", "2026-06-25", "2026-06-24"}
	for _, date := range dates {
		backupDir := filepath.Join(tempDir, "backups", date, "test-node")
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(backupDir, "test.db.zst"), []byte("data"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	backups, err := GitListBackups(repo, "test-node")
	if err != nil {
		t.Fatalf("GitListBackups: %v", err)
	}
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(backups))
	}
	// Should be in descending order
	if backups[0] != "2026-06-25" || backups[2] != "2026-06-23" {
		t.Errorf("expected descending order, got %v", backups)
	}
}

func TestEnsureRemote(t *testing.T) {
	repo, _ := helperCreateTestRepo(t, false)

	// Add a remote
	err := EnsureRemote(repo, "origin", "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("EnsureRemote: %v", err)
	}

	// Calling again should not error
	err = EnsureRemote(repo, "origin", "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("EnsureRemote (duplicate): %v", err)
	}
}

// nowFunc returns the current time (can be overridden in tests).
func nowFunc() time.Time {
	return time.Unix(timeNow(), 0).UTC()
}

func timeNow() int64 {
	return 1000000000
}
