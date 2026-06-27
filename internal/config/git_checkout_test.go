package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func testGitLogger() *slog.Logger {
	return slog.Default()
}

func TestNewGitCheckout_Clone(t *testing.T) {
	repoDir, err := os.MkdirTemp("", "config-sync-test-repo")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(repoDir)

	bareRepoPath := filepath.Join(repoDir, "test.git")
	if _, err = git.PlainInit(bareRepoPath, true); err != nil {
		t.Fatal(err)
	}

	wtPath := filepath.Join(repoDir, "worktree")
	repo, err := git.PlainInit(wtPath, false)
	if err != nil {
		t.Fatal(err)
	}

	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	testFile := filepath.Join(wtPath, "config.json5")
	if err := os.WriteFile(testFile, []byte(`{"test": true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = w.Add("config.json5")
	if err != nil {
		t.Fatal(err)
	}

	_, err = w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	remote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{bareRepoPath},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = remote.Push(&git.PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/master"},
	})
	if err != nil {
		t.Fatal(err)
	}

	checkoutDir := filepath.Join(repoDir, "checkout")
	checkout, err := NewGitCheckout(bareRepoPath, checkoutDir, testGitLogger())
	if err != nil {
		t.Fatalf("NewGitCheckout failed: %v", err)
	}
	if checkout == nil {
		t.Fatal("expected non-nil checkout")
	}
	if checkout.Path() != checkoutDir {
		t.Errorf("expected path %s, got %s", checkoutDir, checkout.Path())
	}
}

func TestNewGitCheckout_OpenExisting(t *testing.T) {
	repoDir, err := os.MkdirTemp("", "config-sync-open-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(repoDir)

	bareRepoPath := filepath.Join(repoDir, "test.git")
	if _, err := git.PlainInit(bareRepoPath, true); err != nil {
		t.Fatal(err)
	}

	wtPath := filepath.Join(repoDir, "wt")
	repo, err := git.PlainInit(wtPath, false)
	if err != nil {
		t.Fatal(err)
	}

	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	f := filepath.Join(wtPath, "shared", "test.json5")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(`{"key":"val"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = w.Add(".")
	if err != nil {
		t.Fatal(err)
	}

	_, err = w.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	remote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{bareRepoPath},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = remote.Push(&git.PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/master:refs/heads/master"},
	})
	if err != nil {
		t.Fatal(err)
	}

	checkoutPath := filepath.Join(repoDir, "clone")
	checkout, err := NewGitCheckout(bareRepoPath, checkoutPath, testGitLogger())
	if err != nil {
		t.Fatalf("NewGitCheckout failed: %v", err)
	}
	_ = checkout
}

func TestIsDirty_Clean(t *testing.T) {
	repoDir, err := os.MkdirTemp("", "config-dirty-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(repoDir)

	// Init a repo (bare, won't have isDirty, so test with a worktree-based clone)
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatal(err)
	}

	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	if dirty, err := w.Status(); err == nil && dirty.IsClean() {
		// Clean by default
	} else if err != nil {
		t.Fatal(err)
	}

	// Create a GitCheckout for this repo
	checkout := &GitCheckout{
		repoURL:     "file://" + repoDir,
		checkoutDir: repoDir,
		repo:        repo,
		logger:      testGitLogger(),
	}

	dirty, err := checkout.IsDirty()
	if err != nil {
		t.Errorf("IsDirty on clean repo should work: %v", err)
	}
	if dirty {
		t.Error("IsDirty on clean repo should be false")
	}
}
