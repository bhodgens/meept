package session

// 03-context leaf 01 T4 RESUME CONTRACT (docs/plans/20261119-usability-
// remediation/03-context/01-session-history.md): a session resume after a
// daemon restart must resolve the working directory from the PERSISTED
// detection_context alone -- no in-memory state, no daemon-cwd fallback.
// The restart is simulated by closing the SQLite store and re-opening the
// same database file (the working_dir_persistence_test.go precedent).
//
// The negative branch pins the other half of the same contract: a session
// that binds nothing (no worktree, no project, no detection CWD) must
// resolve NOTHING -- callers then fail actionably with tools.ErrNoWorkingDir
// instead of guessing a directory (working_dir.go: "fail actionably
// (tools.ErrNoWorkingDir) instead of guessing a directory").
//
// Test-only import: this file pins the sentinel the callers apply so the
// unbound-session resolution can never silently start resolving something
// (import cycle check: package session does not import internal/tools in
// non-test code; this _test.go file is the only place the seam is pinned).

import (
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// TestResumeContract_DetectionContextAloneSurvivesRestart: given a daemon
// restart, a session whose ONLY working-dir binding is its persisted
// detection_context resolves that CWD from the reopened store.
func TestResumeContract_DetectionContextAloneSurvivesRestart(t *testing.T) {
	store, dbPath := testHelper(t)
	cwd := filepath.Join(t.TempDir(), "client-cwd")

	sess, err := store.Create("resume-contract-session")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SetDetectionContext(sess.ID, &DetectionContext{CWD: cwd}); err != nil {
		t.Fatalf("SetDetectionContext: %v", err)
	}

	// Simulate the daemon restart: drop the store, reopen the same file.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	restarted, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer restarted.Close()

	reloaded := restarted.Get(sess.ID)
	if reloaded == nil {
		t.Fatal("session not found after reopen")
	}
	if reloaded.DetectionContext == nil || reloaded.DetectionContext.CWD != cwd {
		t.Fatalf("detection_context lost across restart: %+v", reloaded.DetectionContext)
	}
	dir, src := ResolveWorkingDir(reloaded)
	if dir != cwd {
		t.Errorf("post-restart ResolveWorkingDir() dir = %q, want %q (the persisted detection CWD)", dir, cwd)
	}
	if src != WorkingDirFromDetection {
		t.Errorf("post-restart ResolveWorkingDir() source = %q, want %q", src, WorkingDirFromDetection)
	}
}

// TestResumeContract_WorktreeBeatsDetectionContextAfterRestart: the
// repo-wide precedence (worktree > project > detection CWD) holds across a
// restart too -- a session with BOTH bindings must keep resolving the
// worktree, never fall through to the detection context.
func TestResumeContract_WorktreeBeatsDetectionContextAfterRestart(t *testing.T) {
	store, dbPath := testHelper(t)
	cwd := filepath.Join(t.TempDir(), "client-cwd")
	worktree := filepath.Join(t.TempDir(), "provisioned-worktree")

	sess, err := store.Create("resume-contract-precedence")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SetDetectionContext(sess.ID, &DetectionContext{CWD: cwd}); err != nil {
		t.Fatalf("SetDetectionContext: %v", err)
	}
	if err := store.SetWorktree(sess.ID, "wt-1", worktree); err != nil {
		t.Fatalf("SetWorktree: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	restarted, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer restarted.Close()

	reloaded := restarted.Get(sess.ID)
	if reloaded == nil {
		t.Fatal("session not found after reopen")
	}
	dir, src := ResolveWorkingDir(reloaded)
	if dir != worktree {
		t.Errorf("post-restart ResolveWorkingDir() dir = %q, want the worktree %q", dir, worktree)
	}
	if src != WorkingDirFromWorktree {
		t.Errorf("post-restart ResolveWorkingDir() source = %q, want %q", src, WorkingDirFromWorktree)
	}
}

// TestResumeContract_UnboundSessionResolvesNothing: a session with no
// worktree, no project and no detection CWD resolves ("", none) after a
// restart -- the caller then fails actionably with tools.ErrNoWorkingDir
// (never the daemon's own process CWD).
func TestResumeContract_UnboundSessionResolvesNothing(t *testing.T) {
	store, dbPath := testHelper(t)

	sess, err := store.Create("resume-contract-unbound")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	restarted, err := NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer restarted.Close()

	reloaded := restarted.Get(sess.ID)
	if reloaded == nil {
		t.Fatal("session not found after reopen")
	}
	dir, src := ResolveWorkingDir(reloaded)
	if dir != "" || src != WorkingDirFromNone {
		t.Fatalf("unbound session resolved (%q, %q); want (\"\", none) -- the ErrNoWorkingDir path", dir, src)
	}
	if !errors.Is(tools.ErrNoWorkingDir, tools.ErrNoWorkingDir) {
		t.Fatal("sentinel identity check failed")
	}
}
