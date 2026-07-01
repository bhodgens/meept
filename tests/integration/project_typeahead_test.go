package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/sqlite"
)

// TestProjectTypeaheadFlow verifies the end-to-end /project typeahead flow:
// 1. Touch recents via RecentsStore
// 2. Call project.readdir RPC to get filtered recents
// 3. Call project.set RPC to bind project to session
// 4. Verify session.ProjectPath is updated
// 5. Verify message bus event is published for AgentLoop synchronization
func TestProjectTypeaheadFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	requireGit(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "projects.db")

	// Create SQLite pool.
	pool, err := sqlite.NewPool(sqlite.PoolConfig{
		Path:     dbPath,
		PoolSize: 1,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	// Create project store.
	store, err := project.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	// Get a raw *sql.DB from the pool for the RecentsStore.
	conn, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("get pool conn: %v", err)
	}
	defer pool.Put(conn)

	recentsStore := project.NewRecentsStore(conn)

	cfg := config.ProjectsConfig{
		BaseDir: filepath.Join(dir, "projects"),
	}
	pm := project.NewProjectManager(store, recentsStore, cfg, nil)

	// Create message bus for event publishing.
	busCfg := &bus.Config{
		BufferSize: 100,
	}
	msgBus := bus.New(busCfg, nil)
	defer msgBus.Close()

	// Create project handler.
	sessionStore := session.NewMemoryStore(nil)
	// Create the test session first.
	testSess, err := sessionStore.Create("test-session")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	sessionID := testSess.ID
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	handler := rpc.NewProjectHandler(pm, sessionStore)
	handler.SetMessageBus(msgBus)

	ctx := context.Background()

	// Step 1: Touch 3 recent project paths.
	testPaths := []string{
		filepath.Join(dir, "project-alpha"),
		filepath.Join(dir, "project-beta"),
		filepath.Join(dir, "project-gamma"),
	}
	for _, p := range testPaths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		// Initialize as git repo for DetectFromPath.
		runGitInit(t, p)
	}

	// Touch recents in reverse order so project-gamma is most recent.
	for i := len(testPaths) - 1; i >= 0; i-- {
		if err := recentsStore.TouchRecent(ctx, testPaths[i]); err != nil {
			t.Fatalf("TouchRecent: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Step 2: Call project.readdir with prefix "gamma" to filter recents.
	params := json.RawMessage(`{"prefix":"gamma"}`)
	result, err := handler.HandleReadDirForTest(ctx, params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*rpc.ReadDirResponse)

	// Should have 1 recent matching "gamma".
	if len(resp.Recents) != 1 {
		t.Errorf("expected 1 recent matching 'gamma', got %d: %v", len(resp.Recents), resp.Recents)
	}
	if resp.Recents[0] != testPaths[2] {
		t.Errorf("expected %q, got %q", testPaths[2], resp.Recents[0])
	}

	// Step 3: Subscribe to message bus before calling project.set.
	sub := msgBus.Subscribe("test-sub", "project.set")
	defer msgBus.Unsubscribe(sub)

	// Step 4: Call project.set to bind the project to the session.
	setParams := json.RawMessage(`{"session_id":"` + sessionID + `","path":"` + testPaths[2] + `"}`)
	setResult, err := handler.HandleSetForTest(ctx, setParams)
	if err != nil {
		t.Fatalf("handleSet: %v", err)
	}

	// Step 5: Verify session.ProjectPath is updated.
	sess := sessionStore.Get(sessionID)
	if sess == nil {
		t.Fatal("session not found after project.set")
	}
	if sess.ProjectPath != testPaths[2] {
		t.Errorf("expected session.ProjectPath = %q, got %q", testPaths[2], sess.ProjectPath)
	}

	// Step 6: Verify message bus event was published.
	select {
	case <-sub.Channel:
		// Success - event received.
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for project.set message bus event")
	}

	// Verify setResult contains expected fields.
	resultMap, ok := setResult.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", setResult)
	}
	if status, ok := resultMap["status"].(string); !ok || status != "bound" {
		t.Errorf("expected status='bound', got %v", resultMap["status"])
	}
	// project_id must be non-empty (path-based detection auto-registers).
	if pid, ok := resultMap["project_id"].(string); !ok || pid == "" {
		t.Errorf("expected non-empty project_id, got %v", resultMap["project_id"])
	}
}

// TestProjectTypeaheadEmptyPrefix verifies that empty prefix returns all recents.
func TestProjectTypeaheadEmptyPrefix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

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
	defer pool.Close()

	store, err := project.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	conn, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("get pool conn: %v", err)
	}
	defer pool.Put(conn)

	recentsStore := project.NewRecentsStore(conn)
	cfg := config.ProjectsConfig{BaseDir: filepath.Join(dir, "projects")}
	pm := project.NewProjectManager(store, recentsStore, cfg, nil)

	handler := rpc.NewProjectHandler(pm, session.NewMemoryStore(nil))
	ctx := context.Background()

	// Touch 5 recents.
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, "project-"+string(rune('a'+i)))
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		runGitInit(t, path)
		recentsStore.TouchRecent(ctx, path)
		time.Sleep(10 * time.Millisecond)
	}

	// Call with empty prefix - should return all 5 recents.
	params := json.RawMessage(`{"prefix":""}`)
	result, err := handler.HandleReadDirForTest(ctx, params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*rpc.ReadDirResponse)

	if len(resp.Recents) != 5 {
		t.Errorf("expected 5 recents, got %d: %v", len(resp.Recents), resp.Recents)
	}
}

// TestProjectTypeaheadNoMatches verifies empty results when no matches.
func TestProjectTypeaheadNoMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

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
	defer pool.Close()

	store, err := project.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	handler := rpc.NewProjectHandler(project.NewProjectManager(store, nil, config.ProjectsConfig{BaseDir: dir}, nil), session.NewMemoryStore(nil))
	ctx := context.Background()

	// Call with non-matching prefix on empty store.
	params := json.RawMessage(`{"prefix":"nonexistent"}`)
	result, err := handler.HandleReadDirForTest(ctx, params)
	if err != nil {
		t.Fatalf("handleReadDir: %v", err)
	}
	resp := result.(*rpc.ReadDirResponse)

	if len(resp.Recents) != 0 {
		t.Errorf("expected 0 recents, got %d", len(resp.Recents))
	}
	if len(resp.Matches) != 0 {
		t.Errorf("expected 0 fs matches, got %d", len(resp.Matches))
	}
}


// runGitInit initializes a minimal git repo at the given path.
func runGitInit(t *testing.T, path string) {
	t.Helper()
	runGit(t, path, "init", path)
	runGit(t, path, "config", "user.name", "Test")
	runGit(t, path, "config", "user.email", "test@meept.local")
	// Create initial commit.
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, path, "add", "README.md")
	runGit(t, path, "commit", "-m", "Initial commit")
}
