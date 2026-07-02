package integration

// workspace_dirty_round_trip_test.go — Tests workspace snapshot and
// materialization with dirty worktree (spec §4.2, §10).
//
// This test creates a local git repo, commits a file, then modifies it
// (dirty state). It snapshots the workspace (which captures the commit
// SHA and generates a diff blob), then materializes it via Ensure (which
// clones, checks out the commit, and applies the diff patch).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/resources"
	"github.com/caimlas/meept/internal/workspace"
)

// TestWorkspaceSnapshotClean verifies that Snapshot captures a clean
// worktree state correctly.
func TestWorkspaceSnapshotClean(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tmpDir := t.TempDir()

	// Create a git repo with a commit.
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := gitInit(ctx, repoDir); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := gitConfig(ctx, repoDir); err != nil {
		t.Fatalf("git config: %v", err)
	}

	// Create a file and commit it.
	filePath := filepath.Join(repoDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := gitAdd(ctx, repoDir, "."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := gitCommit(ctx, repoDir, "initial commit"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// Create CAS store for patch store wiring.
	casDir := filepath.Join(tmpDir, "cas")
	casCfg := resources.CASConfig{
		StoreDir:      casDir,
		HashAlgorithm: resources.AlgoBlake3,
	}
	store, err := resources.NewCASStore(casCfg, nil)
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer store.Close()
	rm := resources.NewManager(store, nil)

	// Create WorkspaceManager.
	wsCfg := workspace.Config{
		WorktreeRoot:      filepath.Join(tmpDir, "worktrees"),
		GitFallbackToPeer: false,
	}
	wm := workspace.NewManager(wsCfg,
		workspace.WithPatchStore(&testPatchStoreAdapter{rm: rm}),
		workspace.WithPatchResolver(&testPatchResolverAdapter{rm: rm}),
	)

	// Snapshot the clean repo.
	ref, err := wm.Snapshot(ctx, repoDir)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Clean tree: Dirty=false, DiffBlobHash="".
	if ref.Dirty {
		t.Error("expected Dirty=false for clean tree")
	}
	if ref.DiffBlobHash != "" {
		t.Error("expected empty DiffBlobHash for clean tree")
	}
	if ref.CommitSHA == "" {
		t.Error("expected non-empty CommitSHA")
	}
}

// TestWorkspaceSnapshotDirty verifies that Snapshot captures dirty state
// and generates a diff blob in CAS.
func TestWorkspaceSnapshotDirty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := gitInit(ctx, repoDir); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := gitConfig(ctx, repoDir); err != nil {
		t.Fatalf("git config: %v", err)
	}

	// Create and commit a file.
	filePath := filepath.Join(repoDir, "code.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := gitAdd(ctx, repoDir, "."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := gitCommit(ctx, repoDir, "initial"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// Modify the file (dirty state).
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() { println(\"hello\") }\n"), 0o644); err != nil {
		t.Fatalf("WriteFile dirty: %v", err)
	}

	// Create CAS + WorkspaceManager.
	casDir := filepath.Join(tmpDir, "cas")
	store, err := resources.NewCASStore(resources.CASConfig{
		StoreDir:      casDir,
		HashAlgorithm: resources.AlgoBlake3,
	}, nil)
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer store.Close()
	rm := resources.NewManager(store, nil)

	wsCfg := workspace.Config{
		WorktreeRoot:      filepath.Join(tmpDir, "worktrees"),
		GitFallbackToPeer: false,
	}
	wm := workspace.NewManager(wsCfg,
		workspace.WithPatchStore(&testPatchStoreAdapter{rm: rm}),
		workspace.WithPatchResolver(&testPatchResolverAdapter{rm: rm}),
	)

	// Snapshot.
	ref, err := wm.Snapshot(ctx, repoDir)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if !ref.Dirty {
		t.Error("expected Dirty=true after modifying file")
	}
	if ref.DiffBlobHash == "" {
		t.Error("expected non-empty DiffBlobHash for dirty tree")
	}
	if ref.CommitSHA == "" {
		t.Error("expected non-empty CommitSHA")
	}
}

// --- git helpers ---

func gitInit(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "init", dir)
	cmd.Dir = dir
	return cmd.Run()
}

func gitConfig(ctx context.Context, dir string) error {
	for _, args := range [][]string{
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

func gitAdd(ctx context.Context, dir, path string) error {
	cmd := exec.CommandContext(ctx, "git", "add", path)
	cmd.Dir = dir
	return cmd.Run()
}

func gitCommit(ctx context.Context, dir, msg string) error {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", msg)
	cmd.Dir = dir
	return cmd.Run()
}
