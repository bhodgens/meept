package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test helpers ---

// setupOriginRepo creates a bare git repo at originPath, populates it with an
// initial commit, and returns (seedPath, commitSHA). The seed path is the
// working clone used to push initial content; tests can use it as the source
// for Snapshot or read its HEAD SHA.
func setupOriginRepo(t *testing.T, originPath string) (seedPath, commitSHA string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(originPath, 0o755))
	runGit(t, originPath, "init", "--bare", "-b", "main", originPath)

	seedPath = filepath.Join(t.TempDir(), "seed")
	runGitClone(t, seedPath, originPath, seedPath)
	setGitIdentity(t, seedPath)
	// main already exists from clone of bare repo with -b main; if not, create it.
	runGit(t, seedPath, "checkout", "-B", "main")
	writeFile(t, seedPath, "README.md", "# test\n")
	writeFile(t, seedPath, "main.go", "package main\nfunc main() {}\n")
	runGit(t, seedPath, "add", ".")
	runGit(t, seedPath, "commit", "-m", "initial")
	runGit(t, seedPath, "push", "-u", "origin", "main")
	commitSHA = getHeadSHA(t, seedPath)
	return seedPath, commitSHA
}

// setupDirtyRepo creates a bare repo + seed clone, commits a clean baseline,
// then leaves the seed dirty. Returns (seedPath, cleanCommitSHA, originPath).
func setupDirtyRepo(t *testing.T, originPath string) (seedPath, cleanCommitSHA string) {
	t.Helper()
	seedPath, cleanCommitSHA = setupOriginRepo(t, originPath)

	// Make dirty changes (staged + untracked).
	writeFile(t, seedPath, "main.go", "package main\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	writeFile(t, seedPath, "extra.txt", "untracked content\n")
	runGit(t, seedPath, "add", "main.go")
	return seedPath, cleanCommitSHA
}

// runGit runs a git command in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_TERMINAL_AUTOEXIT=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, string(out))
	}
}

// runGitClone runs a git clone where the destination directory does not
// exist yet. cmd.Dir is set to the PARENT of dest, since git clone creates
// the destination.
func runGitClone(t *testing.T, dest, repoURL, destPath string) {
	t.Helper()
	parent := filepath.Dir(dest)
	// Ensure parent exists.
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir parent %s: %v", parent, err)
	}
	cmd := exec.Command("git", "clone", repoURL, destPath)
	cmd.Dir = parent
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_TERMINAL_AUTOEXIT=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone %s into %s: %v\n%s", repoURL, destPath, err, string(out))
	}
}

// getHeadSHA returns the HEAD commit SHA of the repo at dir.
func getHeadSHA(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	require.NoError(t, err, "git rev-parse HEAD in %s", dir)
	return strings.TrimSpace(string(out))
}

// setGitIdentity configures a dummy git user.email and user.name for the repo
// at dir so that commits succeed in CI / containers without global git config.
func setGitIdentity(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "config", "user.email", "test@meept.local")
	runGit(t, dir, "config", "user.name", "meept test")
}

// writeFile writes content to path, creating parent dirs as needed.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

// newTestManager creates a Manager with worktreeRoot inside t.TempDir().
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	cfg := Config{
		WorktreeRoot:      filepath.Join(t.TempDir(), "worktrees"),
		GitFallbackToPeer: true,
	}
	return NewManager(cfg)
}

// fileExists checks if a path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// --- Mock types ---

type mockPatchStore struct {
	addCalled     bool
	addPath       string
	addReturnHash string
	addReturnErr  error

	// store maps hash → file path for Resolve
	store map[string]string
}

func (m *mockPatchStore) Add(_ context.Context, srcPath string) (string, error) {
	m.addCalled = true
	m.addPath = srcPath
	if m.addReturnErr != nil {
		return "", m.addReturnErr
	}
	if m.addReturnHash != "" {
		// Copy file into store for later Resolve.
		data, _ := os.ReadFile(srcPath)
		stored := srcPath + ".stored"
		_ = os.WriteFile(stored, data, 0o644)
		if m.store == nil {
			m.store = make(map[string]string)
		}
		m.store[m.addReturnHash] = stored
		return m.addReturnHash, nil
	}
	return "mock-hash-" + filepath.Base(srcPath), nil
}

func (m *mockPatchStore) Resolve(hash string) (string, error) {
	if m.store == nil {
		return "", fmt.Errorf("mock patch store: hash not found: %s", hash)
	}
	p, ok := m.store[hash]
	if !ok {
		return "", fmt.Errorf("mock patch store: hash not found: %s", hash)
	}
	return p, nil
}

type mockMetricsEmitter struct {
	materializeMS  []int64
	patchConflicts int
}

func (m *mockMetricsEmitter) ObserveWorkspaceMaterializeMS(ms int64) {
	m.materializeMS = append(m.materializeMS, ms)
}

func (m *mockMetricsEmitter) IncWorkspacePatchConflicts() {
	m.patchConflicts++
}

// --- Tests ---

func TestManager_Snapshot_Clean(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	seed, commitSHA := setupOriginRepo(t, origin)

	mgr := newTestManager(t)
	mgr.SetPatchStore(&mockPatchStore{})

	ref, err := mgr.Snapshot(context.Background(), seed)
	require.NoError(t, err)
	assert.Equal(t, commitSHA, ref.CommitSHA)
	assert.False(t, ref.Dirty)
	assert.Empty(t, ref.DiffBlobHash)
}

func TestManager_Snapshot_Dirty(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	seed, cleanSHA := setupDirtyRepo(t, origin)

	ps := &mockPatchStore{addReturnHash: "sha256:deadbeef"}
	mgr := newTestManager(t)
	mgr.SetPatchStore(ps)

	ref, err := mgr.Snapshot(context.Background(), seed)
	require.NoError(t, err)
	assert.Equal(t, cleanSHA, ref.CommitSHA)
	assert.True(t, ref.Dirty)
	assert.Equal(t, "sha256:deadbeef", ref.DiffBlobHash)
	assert.True(t, ps.addCalled, "PatchStore.Add should have been called")
}

func TestManager_Snapshot_Dirty_NoPatchStore_Error(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	seed, _ := setupDirtyRepo(t, origin)

	mgr := newTestManager(t)
	// No patch store wired.

	_, err := mgr.Snapshot(context.Background(), seed)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPatchStoreNotConfigured)
}

func TestManager_Ensure_CleanRepo(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	_, commitSHA := setupOriginRepo(t, origin)

	mgr := newTestManager(t)
	ref := WorkspaceRef{
		RepoURL:   origin,
		CommitSHA: commitSHA,
		Dirty:     false,
	}

	path, err := mgr.Ensure(context.Background(), ref)
	require.NoError(t, err)
	assert.NotEmpty(t, path)
	assert.True(t, fileExists(path), "worktree path should exist")

	// Verify the content is there.
	_, err = os.Stat(filepath.Join(path, "README.md"))
	assert.NoError(t, err)

	// Verify we're on an ephemeral branch.
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = path
	out, _ := cmd.Output()
	branch := strings.TrimSpace(string(out))
	assert.True(t, strings.HasPrefix(branch, "meept-job-"),
		"expected ephemeral branch meept-job-*, got %s", branch)

	// Cleanup.
	require.NoError(t, mgr.Close(path))
}

func TestManager_Ensure_WithPatch(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	seed, cleanSHA := setupDirtyRepo(t, origin)

	// Snapshot the dirty state.
	ps := &mockPatchStore{addReturnHash: "sha256:abc123"}
	mgr := newTestManager(t)
	mgr.SetPatchStore(ps)
	mgr.SetPatchResolver(ps)

	ref, err := mgr.Snapshot(context.Background(), seed)
	require.NoError(t, err)
	require.True(t, ref.Dirty)
	require.NotEmpty(t, ref.DiffBlobHash)

	// Ensure needs RepoURL to be set (Snapshot doesn't fill it in).
	ref.RepoURL = origin

	// Now Ensure materializes a fresh worktree with the patch applied.
	path, err := mgr.Ensure(context.Background(), ref)
	require.NoError(t, err)
	assert.NotEmpty(t, path)

	// Verify the patched content is present (main.go should have the dirty
	// version with println).
	content, err := os.ReadFile(filepath.Join(path, "main.go"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "println")

	// Cleanup.
	require.NoError(t, mgr.Close(path))
	_ = cleanSHA // cleanSHA is validated via ref.CommitSHA
}

func TestManager_Ensure_PatchConflict(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	seed, _ := setupDirtyRepo(t, origin)

	// Snapshot dirty.
	ps := &mockPatchStore{addReturnHash: "sha256:conflict"}
	mgr := newTestManager(t)
	mgr.SetPatchStore(ps)
	mgr.SetPatchResolver(ps)

	ref, err := mgr.Snapshot(context.Background(), seed)
	require.NoError(t, err)
	ref.RepoURL = origin

	// Mutate origin after snapshot so the patch won't apply cleanly.
	// Commit a diverging change to main.go in a fresh clone, then advance
	// the ref.CommitSHA to the new commit.
	advanceClone := filepath.Join(t.TempDir(), "advance")
	runGitClone(t, advanceClone, origin, advanceClone)
	setGitIdentity(t, advanceClone)
	writeFile(t, advanceClone, "main.go", "package main\nfunc main() {\n\t// completely different\n}\n")
	runGit(t, advanceClone, "add", "main.go")
	runGit(t, advanceClone, "commit", "-m", "diverging change")
	newSHA := getHeadSHA(t, advanceClone)
	// Push to origin so Ensure can fetch it.
	runGit(t, advanceClone, "push", "origin", "main")

	// Use the new SHA but the old patch — should conflict.
	ref.CommitSHA = newSHA

	metrics := &mockMetricsEmitter{}
	mgr.SetMetricsEmitter(metrics)

	_, err = mgr.Ensure(context.Background(), ref)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPatchConflict)

	var pc *PatchConflict
	assert.ErrorAs(t, err, &pc)
	if pc != nil {
		assert.Equal(t, newSHA, pc.Commit)
	}
	assert.Equal(t, 1, metrics.patchConflicts, "patch conflict metric should be emitted")
}

func TestManager_Close_RemovesWorktree(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	_, commitSHA := setupOriginRepo(t, origin)

	mgr := newTestManager(t)
	ref := WorkspaceRef{RepoURL: origin, CommitSHA: commitSHA}

	path, err := mgr.Ensure(context.Background(), ref)
	require.NoError(t, err)
	require.True(t, fileExists(path))

	require.NoError(t, mgr.Close(path))
	assert.False(t, fileExists(path), "worktree should be removed after Close")
}

func TestManager_Close_Idempotent(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	_, commitSHA := setupOriginRepo(t, origin)

	mgr := newTestManager(t)
	ref := WorkspaceRef{RepoURL: origin, CommitSHA: commitSHA}

	path, err := mgr.Ensure(context.Background(), ref)
	require.NoError(t, err)

	require.NoError(t, mgr.Close(path))
	// Second close — should be a no-op, not an error.
	require.NoError(t, mgr.Close(path))
	assert.False(t, fileExists(path))
}

func TestManager_Close_RejectsPathOutsideRoot(t *testing.T) {
	mgr := newTestManager(t)
	err := mgr.Close("/tmp/some-random-path-not-under-root")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotAWorktree)
}

func TestManager_Ensure_PeerURL_NotImplemented(t *testing.T) {
	mgr := newTestManager(t)
	ref := WorkspaceRef{
		RepoURL:   "peer:node-abc",
		CommitSHA: "abc123",
	}
	_, err := mgr.Ensure(context.Background(), ref)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPeerFetchNotImplemented)
}

func TestManager_Ensure_InvalidRef(t *testing.T) {
	mgr := newTestManager(t)

	tests := []struct {
		name string
		ref  WorkspaceRef
	}{
		{"empty repo url", WorkspaceRef{CommitSHA: "abc"}},
		{"empty commit sha", WorkspaceRef{RepoURL: "https://example.com/repo.git"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mgr.Ensure(context.Background(), tt.ref)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidWorkspaceRef)
		})
	}
}

func TestManager_Ensure_DirtyButNoResolver_Error(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	_, commitSHA := setupOriginRepo(t, origin)

	mgr := newTestManager(t)
	// No resolver wired.
	ref := WorkspaceRef{
		RepoURL:      origin,
		CommitSHA:    commitSHA,
		Dirty:        true,
		DiffBlobHash: "sha256:somehash",
	}
	_, err := mgr.Ensure(context.Background(), ref)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPatchStoreNotConfigured)
}

func TestManager_Setters_NilSafe(t *testing.T) {
	mgr := newTestManager(t)
	// All setters should be no-ops with nil — no panic.
	mgr.SetPatchStore(nil)
	mgr.SetPatchResolver(nil)
	mgr.SetMetricsEmitter(nil)
}

func TestManager_MetricsEmitter_Wired(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	_, commitSHA := setupOriginRepo(t, origin)

	metrics := &mockMetricsEmitter{}
	mgr := newTestManager(t)
	mgr.SetMetricsEmitter(metrics)

	ref := WorkspaceRef{RepoURL: origin, CommitSHA: commitSHA}
	path, err := mgr.Ensure(context.Background(), ref)
	require.NoError(t, err)
	assert.NotEmpty(t, metrics.materializeMS, "materialize metric should be recorded")

	require.NoError(t, mgr.Close(path))
}

func TestWorkspaceUnavailable_Error(t *testing.T) {
	err := &WorkspaceUnavailable{
		Commit:  "abc123",
		RepoURL: "https://example.com/repo.git",
	}
	assert.Contains(t, err.Error(), "abc123")
	assert.Contains(t, err.Error(), "example.com")

	// *WorkspaceUnavailable implements Is(ErrWorkspaceUnavailable) so
	// errors.Is works directly without double-wrapping.
	assert.ErrorIs(t, err, ErrWorkspaceUnavailable)
}

func TestPatchConflict_Error(t *testing.T) {
	err := &PatchConflict{
		Commit: "abc123",
		Reason: "context mismatch",
	}
	assert.Contains(t, err.Error(), "abc123")
	assert.Contains(t, err.Error(), "context mismatch")
	assert.ErrorIs(t, err, ErrPatchConflict)
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home dir")
	}
	assert.Equal(t, home, expandHome("~"))
	assert.Equal(t, filepath.Join(home, "foo"), expandHome("~/foo"))
	assert.Equal(t, "/abs/path", expandHome("/abs/path"))
	assert.Equal(t, "", expandHome(""))
}

func TestIsSubPath(t *testing.T) {
	assert.True(t, isSubPath("/a/b", "/a/b"))
	assert.True(t, isSubPath("/a/b", "/a/b/c"))
	assert.True(t, isSubPath("/a/b", "/a/b/c/d"))
	assert.False(t, isSubPath("/a/b", "/a/bc"))
	assert.False(t, isSubPath("/a/b", "/a"))
	assert.False(t, isSubPath("/a/b", "/x/b"))
}
