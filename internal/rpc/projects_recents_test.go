package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/sqlite"
)

// newTestEnv creates a test env with a ProjectManager that has a working RecentsStore
// backed by an in-memory SQLite pool (t.TempDir + sqlite.Pool).
func newTestEnv(t *testing.T) (*ProjectHandler, *project.Store, *sqlite.Pool, *project.RecentsStore, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "projects.db")

	pool, err := sqlite.NewPool(sqlite.PoolConfig{
		Path:     dbPath,
		PoolSize: 1,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	store, err := project.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	// Get a raw *sql.DB from the pool for the RecentsStore.
	conn, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("get pool conn: %v", err)
	}
	recentsStore := project.NewRecentsStore(conn)

	cfg := config.ProjectsConfig{
		BaseDir: filepath.Join(dir, "projects"),
	}
	pm := project.NewProjectManager(store, recentsStore, cfg, nil)

	h := NewProjectHandler(pm, session.NewMemoryStore(nil))

	cleanup := func() {
		pool.Put(conn)
		pool.Close()
		store.Close()
	}
	return h, store, pool, recentsStore, cleanup
}

func TestHandleReadDir_EmptyRecentsNoPrefix(t *testing.T) {
	h, _, _, _, cleanup := newTestEnv(t)
	defer cleanup()

	params := json.RawMessage(`{"prefix":""}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*ReadDirResponse)
	if len(resp.Recents) != 0 {
		t.Errorf("expected 0 recents, got %d", len(resp.Recents))
	}
	if len(resp.Matches) != 0 {
		t.Errorf("expected 0 matches (empty prefix => no fs fallback), got %d", len(resp.Matches))
	}
	if len(resp.GitRoots) != 0 {
		t.Errorf("expected 0 git_roots, got %d", len(resp.GitRoots))
	}
}

func TestHandleReadDir_RecentsOnly(t *testing.T) {
	h, _, _, recentsStore, cleanup := newTestEnv(t)
	defer cleanup()

	// Touch 3 recents paths using the recentsStore directly.
	ctx := context.Background()
	recentsStore.TouchRecent(ctx, "/home/user/repos/foo")
	recentsStore.TouchRecent(ctx, "/home/user/repos/bar")
	recentsStore.TouchRecent(ctx, "/home/user/repos/baz")

	// Call with empty prefix to get all recents.
	params := json.RawMessage(`{"prefix":""}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*ReadDirResponse)
	if len(resp.Recents) != 3 {
		t.Fatalf("expected 3 recents, got %d: %v", len(resp.Recents), resp.Recents)
	}
	// Most recent first.
	if resp.Recents[0] != "/home/user/repos/baz" {
		t.Errorf("expected most recent first: got %q, want /home/user/repos/baz", resp.Recents[0])
	}
}

func TestHandleReadDir_RecentsFilteredByPrefix(t *testing.T) {
	h, _, _, recentsStore, cleanup := newTestEnv(t)
	defer cleanup()

	// Touch 3 paths using the recentsStore directly.
	ctx := context.Background()
	recentsStore.TouchRecent(ctx, "/home/user/repos/goo")
	recentsStore.TouchRecent(ctx, "/home/user/repos/fo")
	recentsStore.TouchRecent(ctx, "/home/user/repos/other")

	params := json.RawMessage(`{"prefix":"fo"}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*ReadDirResponse)

	// Should have 1 recent matching "fo" ("/home/user/repos/fo").
	if len(resp.Recents) != 1 {
		t.Errorf("expected 1 recent matching 'fo', got %d: %v", len(resp.Recents), resp.Recents)
	}
	if resp.Recents[0] != "/home/user/repos/fo" {
		t.Errorf("expected '/home/user/repos/fo', got %q", resp.Recents[0])
	}
	if len(resp.Matches) != 0 {
		t.Errorf("expected 0 fs matches (recents had matches), got %d", len(resp.Matches))
	}
}

func TestHandleReadDir_FsFallback(t *testing.T) {
	h, _, _, _, cleanup := newTestEnv(t)
	defer cleanup()

	// Create a temp directory with subdirectories.
	tmpDir := t.TempDir()
	for _, name := range []string{"alpha", "beta", "gamma", "notadir.txt"} {
		if name == "notadir.txt" {
			os.WriteFile(filepath.Join(tmpDir, name), nil, 0o644)
			continue
		}
		os.Mkdir(filepath.Join(tmpDir, name), 0o755)
	}

	// Use tmpDir as prefix - fs fallback will read tmpDir and return subdirs.
	params := json.RawMessage(`{"prefix":"` + tmpDir + `"}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*ReadDirResponse)

	// No recents in fresh store, so fs fallback kicks in and returns subdirs.
	if len(resp.Recents) != 0 {
		t.Errorf("expected 0 recents, got %d", len(resp.Recents))
	}
	// Should have 3 matches (alpha, beta, gamma - not notadir.txt).
	if len(resp.Matches) != 3 {
		t.Errorf("expected 3 fs matches (alpha, beta, gamma), got %d: %v", len(resp.Matches), resp.Matches)
	}
}

func TestHandleReadDir_FsFallbackTildePrefix(t *testing.T) {
	h, _, _, _, cleanup := newTestEnv(t)
	defer cleanup()

	// Call with tilde prefix - should expand to home dir.
	params := json.RawMessage(`{"prefix":"~"}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*ReadDirResponse)

	// FS fallback should work for ~ prefix.
	if len(resp.Matches) == 0 {
		t.Logf("no filesystem matches found for ~ (this can happen on some systems)")
	}
	// Matches should not contain error strings.
	for _, m := range resp.Matches {
		if m == "" {
			t.Error("matches should not contain empty entries")
		}
	}
}

func TestHandleReadDir_NoRecentsNoFsFallback(t *testing.T) {
	h, _, _, _, cleanup := newTestEnv(t)
	defer cleanup()

	// Non-existent path with prefix: no recents, fs fallback fails silently.
	params := json.RawMessage(`{"prefix":"/nonexistent/path/xyz"}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*ReadDirResponse)
	if len(resp.Recents) != 0 {
		t.Errorf("expected 0 recents, got %d", len(resp.Recents))
	}
	if len(resp.Matches) != 0 {
		t.Errorf("expected 0 matches (fs fallback should fail silently on non-existent path), got %d", len(resp.Matches))
	}
}

func TestHandleReadDir_RecentsTrumpsFsFallback(t *testing.T) {
	h, _, _, recentsStore, cleanup := newTestEnv(t)
	defer cleanup()

	// Touch one recent that matches the prefix exactly.
	recentsStore.TouchRecent(context.Background(), "/home/user/repos/test")

	// Also create a directory on disk that matches.
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "test"), 0o755)

	// The prefix matches both. Since recents have 1 match, fs fallback is
	// SKIPPED (the handler only does fs fallback when filtered recents is empty).
	params := json.RawMessage(`{"prefix":"/home/user/repos/test"}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*ReadDirResponse)

	if len(resp.Recents) != 1 {
		t.Errorf("expected 1 recent, got %d: %v", len(resp.Recents), resp.Recents)
	}
	// Even though the disk had a matching directory, fs fallback is skipped.
	if len(resp.Matches) != 0 {
		t.Errorf("expected 0 fs matches (recents trump fs fallback), got %d: %v", len(resp.Matches), resp.Matches)
	}
}

func TestHandleReadDir_MissingPM(t *testing.T) {
	h := NewProjectHandler(nil, session.NewMemoryStore(nil))
	params := json.RawMessage(`{"prefix":""}`)
	_, err := h.handleReadDir(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for nil project manager")
	}
	if got := err.Error(); got != "project manager not available" {
		t.Errorf("unexpected error: %q", got)
	}
}

func TestHandleReadDir_InvalidParams(t *testing.T) {
	h, _, _, _, cleanup := newTestEnv(t)
	defer cleanup()

	params := json.RawMessage(`{"notafield": true}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	// Invalid params should still succeed with empty prefix (defaults to empty).
	resp := result.(*ReadDirResponse)
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
}

func TestHandleReadDir_MaxEntriesLimit(t *testing.T) {
	h, _, _, _, cleanup := newTestEnv(t)
	defer cleanup()

	// Create 60 subdirectories to test that fs fallback caps at 50.
	tmpDir := t.TempDir()
	for i := 0; i < 60; i++ {
		os.Mkdir(filepath.Join(tmpDir, fmt.Sprintf("dir%03d", i)), 0o755)
	}

	params := json.RawMessage(`{"prefix":"` + tmpDir + `"}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*ReadDirResponse)

	if len(resp.Matches) > 50 {
		t.Errorf("expected <= 50 fs matches, got %d", len(resp.Matches))
	}
	if len(resp.Matches) == 0 {
		t.Error("expected some fs matches")
	}
}

func TestHandleReadDir_SubstringMatch(t *testing.T) {
	h, _, _, recentsStore, cleanup := newTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	// Touch recents with distinct names for substring testing.
	paths := []string{
		"/home/user/project-alpha",
		"/home/user/project-beta",
		"/home/user/other-gamma",
	}
	for _, p := range paths {
		recentsStore.TouchRecent(ctx, p)
	}

	// Test "alpha" substring match.
	params := json.RawMessage(`{"prefix":"alpha"}`)
	result, err := h.handleReadDir(context.Background(), params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*ReadDirResponse)
	if len(resp.Recents) != 1 {
		t.Errorf("expected 1 recent matching 'alpha', got %d: %v", len(resp.Recents), resp.Recents)
	}
	if !strings.Contains(resp.Recents[0], "alpha") {
		t.Errorf("expected recent to contain 'alpha', got %q", resp.Recents[0])
	}
}
