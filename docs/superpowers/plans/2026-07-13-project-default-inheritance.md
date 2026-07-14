# Project Default Inheritance & Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure every session has a working directory by default — inheriting the last active project, auto-creating one under `base_dir` when none exists, and adding `/project rename` plus sidecar-based adoption for renamed directories.

**Architecture:** The project manager gains `EnsureDefault`, `GetActive`, `DeactivateActive`, `CreateOrResolve`, and `Rename` methods. `SessionService.CreateSession` inherits the active project or triggers `EnsureDefault`. The RPC `project.set` handler resolves shorthand names under `base_dir` and deactivates the previous active project before binding. A `.meept/project_id` sidecar file inside each project directory enables adoption when directories are renamed externally. A new `project.rename` RPC + REST endpoint exposes renaming. TUI and Flutter GUI gain `/project rename <name>` support.

**Tech Stack:** Go (daemon, RPC, services), SQLite (project store), Flutter/Dart (GUI), Bubble Tea (TUI)

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/project/manager.go` | New: `EnsureDefault`, `GetActive`, `DeactivateActive`, `CreateOrResolve`, `Rename`, sidecar read/write helpers |
| `internal/project/store.go` | New: `DeactivateActive`, `GetActive`, `UpdateProjectPath`, `ListSessionsByProjectPath` methods |
| `internal/project/manager_test.go` | Tests for all new manager methods |
| `internal/project/store_test.go` | Tests for new store methods |
| `internal/project/types.go` | No changes (existing types suffice) |
| `internal/services/session_service.go` | `CreateSession` inherits active project or calls `EnsureDefault` |
| `internal/services/service.go` | Pass `ProjectManager` to `SessionService` constructor |
| `internal/services/session_service_test.go` | Tests for project inheritance on session create |
| `internal/rpc/projects.go` | `handleSet` resolves shorthand names; new `handleRename` handler; register `project.rename` |
| `internal/rpc/projects_test.go` | Tests for shorthand resolution and rename RPC |
| `internal/comm/http/api_handlers.go` | New `POST /api/v1/projects/{id}/rename` endpoint |
| `internal/comm/http/server.go` | Register rename route |
| `internal/tui/command_handler.go` | `/project rename <name>` subcommand; update help text |
| `ui/flutter_ui/lib/features/chat/chat_input.dart` | `/project rename <name>` parsing; route to daemon |
| `ui/flutter_ui/lib/services/sdk_client.dart` | `renameProject` method calling `project.rename` RPC |
| `docs/workflows/projects.md` | Update documentation for new resolution chain, rename, and sidecar |

---

## Task 1: Store — `GetActive`, `DeactivateActive`, `UpdateProjectPath`

**Files:**
- Modify: `internal/project/store.go`
- Test: `internal/project/store_test.go`

- [ ] **Step 1: Write failing tests for `GetActive` and `DeactivateActive`**

Add to `internal/project/store_test.go`:

```go
func TestStore_GetActive(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// No projects yet → no active project.
	p, err := store.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive on empty store: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil, got %+v", p)
	}

	// Create an active project.
	proj := &Project{
		ID: "p1", Name: "alpha", Mode: ModeGit,
		LocalPath: "/tmp/alpha", Status: "active",
	}
	if err := store.CreateProject(ctx, proj); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	p, err = store.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if p == nil || p.ID != "p1" {
		t.Fatalf("expected p1, got %+v", p)
	}

	// Create a second active project — GetActive should return one of them
	// (deterministically the most recently updated).
	proj2 := &Project{
		ID: "p2", Name: "beta", Mode: ModeGit,
		LocalPath: "/tmp/beta", Status: "active",
	}
	if err := store.CreateProject(ctx, proj2); err != nil {
		t.Fatalf("CreateProject p2: %v", err)
	}

	p, err = store.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive after p2: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil active project")
	}
}

func TestStore_DeactivateActive(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create two active projects.
	for _, p := range []*Project{
		{ID: "a1", Name: "a1", Mode: ModeGit, LocalPath: "/tmp/a1", Status: "active"},
		{ID: "a2", Name: "a2", Mode: ModeGit, LocalPath: "/tmp/a2", Status: "active"},
	} {
		if err := store.CreateProject(ctx, p); err != nil {
			t.Fatalf("CreateProject %s: %v", p.ID, err)
		}
	}

	// Deactivate all active.
	if err := store.DeactivateActive(ctx); err != nil {
		t.Fatalf("DeactivateActive: %v", err)
	}

	// No active project should remain.
	p, err := store.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive after deactivate: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil active after deactivate, got %+v", p)
	}
}

func TestStore_UpdateProjectPath(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	orig := &Project{
		ID: "u1", Name: "old-name", Mode: ModeGit,
		LocalPath: "/tmp/old-name", Status: "active",
	}
	if err := store.CreateProject(ctx, orig); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	if err := store.UpdateProjectPath(ctx, "u1", "/tmp/new-name", "new-name"); err != nil {
		t.Fatalf("UpdateProjectPath: %v", err)
	}

	got, err := store.GetProject(ctx, "u1")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.LocalPath != "/tmp/new-name" {
		t.Errorf("LocalPath = %q, want %q", got.LocalPath, "/tmp/new-name")
	}
	if got.Name != "new-name" {
		t.Errorf("Name = %q, want %q", got.Name, "new-name")
	}
}
```

Also add the `newTestStore` helper if it doesn't already exist in `store_test.go`:

```go
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/project/ -run 'TestStore_GetActive|TestStore_DeactivateActive|TestStore_UpdateProjectPath' -v`
Expected: FAIL — `store.GetActive undefined`, `store.DeactivateActive undefined`, `store.UpdateProjectPath undefined`

- [ ] **Step 3: Implement `GetActive` in `store.go`**

Add to `internal/project/store.go` after `GetProjectByPath`:

```go
// GetActive returns the single active project, or nil if none exist.
// When multiple projects have status="active", returns the most recently
// updated one (deterministic).
func (s *Store) GetActive(ctx context.Context) (*Project, error) {
	row, err := s.pool.QueryRow(ctx,
		`SELECT id, name, mode, git_url, branch, local_path, status, last_sync, created_at, updated_at
		 FROM projects WHERE status = 'active'
		 ORDER BY updated_at DESC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	p, err := scanProject(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return p, nil
}
```

- [ ] **Step 4: Implement `DeactivateActive` in `store.go`**

Add to `internal/project/store.go` after `GetActive`:

```go
// DeactivateActive sets all active projects to "inactive".
// Returns the count of deactivated projects.
func (s *Store) DeactivateActive(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE projects SET status = 'inactive', updated_at = ? WHERE status = 'active'`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("deactivate active projects: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Implement `UpdateProjectPath` in `store.go`**

Add to `internal/project/store.go` after `DeactivateActive`:

```go
// UpdateProjectPath updates a project's local_path and name (which must
// always equal filepath.Base(local_path)).
func (s *Store) UpdateProjectPath(ctx context.Context, id, localPath, name string) error {
	res, err := s.pool.Exec(ctx,
		`UPDATE projects SET local_path = ?, name = ?, updated_at = ? WHERE id = ?`,
		localPath, name, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update project path: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/project/ -run 'TestStore_GetActive|TestStore_DeactivateActive|TestStore_UpdateProjectPath' -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/project/store.go internal/project/store_test.go
git commit -m "feat(project): add GetActive, DeactivateActive, UpdateProjectPath store methods"
```

---

## Task 2: Store — `UpdateSessionProjectPath` for bulk session path updates

**Files:**
- Modify: `internal/project/store.go`
- Test: `internal/project/store_test.go`

When a project directory is renamed, all sessions pointing at the old `project_path` need updating. This requires a method on the session store, not the project store. We add it to the session package.

- [ ] **Step 1: Write failing test for `UpdateSessionsProjectPath`**

Add to `internal/session/session_project_integration_test.go`:

```go
func TestSQLiteStore_UpdateSessionsProjectPath(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// Create a session with a project path.
	sess, err := store.Create("test-sess")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SetProject(sess.ID, "proj-1", "/old/path"); err != nil {
		t.Fatalf("SetProject: %v", err)
	}

	// Update the project path for all sessions pointing at /old/path.
	if err := store.UpdateSessionsProjectPath(ctx, "/old/path", "/new/path"); err != nil {
		t.Fatalf("UpdateSessionsProjectPath: %v", err)
	}

	// Verify the session now has the new path.
	got := store.Get(sess.ID)
	if got == nil {
		t.Fatal("session not found")
	}
	if got.ProjectPath != "/new/path" {
		t.Errorf("ProjectPath = %q, want %q", got.ProjectPath, "/new/path")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestSQLiteStore_UpdateSessionsProjectPath -v`
Expected: FAIL — `store.UpdateSessionsProjectPath undefined`

- [ ] **Step 3: Add `UpdateSessionsProjectPath` to the `Store` interface and `SQLiteStore`**

In `internal/session/store.go`, add to the `Store` interface (after `SetProject`):

```go
	UpdateSessionsProjectPath(ctx context.Context, oldPath, newPath string) error
```

In `internal/session/store_sqlite.go`, add the implementation:

```go
// UpdateSessionsProjectPath bulk-updates all sessions whose project_path
// matches oldPath to newPath. Used when a project directory is renamed.
func (s *SQLiteStore) UpdateSessionsProjectPath(ctx context.Context, oldPath, newPath string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET project_path = ?, last_activity = ? WHERE project_path = ?`,
		newPath, time.Now().UTC().Format(time.RFC3339Nano), oldPath)
	if err != nil {
		return fmt.Errorf("update sessions project_path: %w", err)
	}
	return nil
}
```

Also add a no-op implementation to `MemoryStore` in `internal/session/session.go`:

```go
// UpdateSessionsProjectPath is a no-op for MemoryStore (sessions are in-memory).
func (s *MemoryStore) UpdateSessionsProjectPath(ctx context.Context, oldPath, newPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.ProjectPath == oldPath {
			sess.ProjectPath = newPath
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestSQLiteStore_UpdateSessionsProjectPath -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/session/store.go internal/session/store_sqlite.go internal/session/session.go internal/session/session_project_integration_test.go
git commit -m "feat(session): add UpdateSessionsProjectPath for bulk path updates on rename"
```

---

## Task 3: Manager — sidecar file read/write helpers

**Files:**
- Modify: `internal/project/manager.go`
- Test: `internal/project/manager_test.go`

A `.meept/project_id` file inside the project directory stores the UUID, enabling adoption when directories are renamed externally.

- [ ] **Step 1: Write failing tests for sidecar helpers**

Add to `internal/project/manager_test.go`:

```go
func TestManager_WriteAndReadSidecar(t *testing.T) {
	pm, _ := newTestManager(t)
	dir := t.TempDir()

	// No sidecar yet.
	id, err := pm.ReadSidecarID(dir)
	if err != nil {
		t.Fatalf("ReadSidecarID on empty dir: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty ID, got %q", id)
	}

	// Write sidecar.
	if err := pm.WriteSidecarID(dir, "abc-123"); err != nil {
		t.Fatalf("WriteSidecarID: %v", err)
	}

	// Read it back.
	id, err = pm.ReadSidecarID(dir)
	if err != nil {
		t.Fatalf("ReadSidecarID: %v", err)
	}
	if id != "abc-123" {
		t.Errorf("expected %q, got %q", "abc-123", id)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestManager_WriteAndReadSidecar -v`
Expected: FAIL — `pm.ReadSidecarID undefined`, `pm.WriteSidecarID undefined`

- [ ] **Step 3: Implement sidecar helpers in `manager.go`**

Add to `internal/project/manager.go`:

```go
// sidecarDir is the subdirectory inside a project for meept metadata.
const sidecarDir = ".meept"

// sidecarFile is the filename storing the project's UUID.
const sidecarFile = "project_id"

// sidecarPath returns the full path to the sidecar file for a project directory.
func sidecarPath(projectDir string) string {
	return filepath.Join(projectDir, sidecarDir, sidecarFile)
}

// WriteSidecarID writes the project's UUID into .meept/project_id inside
// the project directory. Creates .meept/ if it doesn't exist.
func (pm *ProjectManager) WriteSidecarID(projectDir, projectID string) error {
	meeptDir := filepath.Join(projectDir, sidecarDir)
	if err := os.MkdirAll(meeptDir, 0o755); err != nil {
		return fmt.Errorf("create sidecar dir: %w", err)
	}
	data := []byte(projectID)
	if err := os.WriteFile(sidecarPath(projectDir), data, 0o644); err != nil {
		return fmt.Errorf("write sidecar: %w", err)
	}
	return nil
}

// ReadSidecarID reads the project UUID from .meept/project_id. Returns
// empty string if the file does not exist (not an error).
func (pm *ProjectManager) ReadSidecarID(projectDir string) (string, error) {
	data, err := os.ReadFile(sidecarPath(projectDir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read sidecar: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project/ -run TestManager_WriteAndReadSidecar -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/manager.go internal/project/manager_test.go
git commit -m "feat(project): add .meept/project_id sidecar read/write helpers"
```

---

## Task 4: Manager — `GetActive`, `DeactivateActive`, `EnsureDefault`

**Files:**
- Modify: `internal/project/manager.go`
- Test: `internal/project/manager_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/project/manager_test.go`:

```go
func TestManager_GetActive_None(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	p, err := pm.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil, got %+v", p)
	}
}

func TestManager_GetActive_Exists(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	p := &Project{
		ID: "active-1", Name: "alpha", Mode: ModeGit,
		LocalPath: "/tmp/alpha", Status: "active",
	}
	if err := store.CreateProject(ctx, p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := pm.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if got == nil || got.ID != "active-1" {
		t.Fatalf("expected active-1, got %+v", got)
	}
}

func TestManager_DeactivateActive(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	p := &Project{
		ID: "a1", Name: "alpha", Mode: ModeGit,
		LocalPath: "/tmp/alpha", Status: "active",
	}
	store.CreateProject(ctx, p)

	if err := pm.DeactivateActive(ctx); err != nil {
		t.Fatalf("DeactivateActive: %v", err)
	}

	got, _ := pm.GetActive(ctx)
	if got != nil {
		t.Errorf("expected nil after deactivate, got %+v", got)
	}
}

func TestManager_EnsureDefault_CreatesProject(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	p, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil project")
	}
	if p.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeGit)
	}
	if p.LocalPath == "" {
		t.Error("LocalPath should not be empty")
	}
	// Directory should exist.
	if _, err := os.Stat(p.LocalPath); os.IsNotExist(err) {
		t.Errorf("project dir not created: %s", p.LocalPath)
	}
	// .git should exist.
	if _, err := os.Stat(filepath.Join(p.LocalPath, ".git")); os.IsNotExist(err) {
		t.Errorf(".git not initialized in %s", p.LocalPath)
	}
	// Sidecar should exist.
	id, err := pm.ReadSidecarID(p.LocalPath)
	if err != nil {
		t.Fatalf("ReadSidecarID: %v", err)
	}
	if id != p.ID {
		t.Errorf("sidecar ID = %q, want %q", id, p.ID)
	}
	// Name should equal filepath.Base(LocalPath).
	if p.Name != filepath.Base(p.LocalPath) {
		t.Errorf("Name = %q, want %q", p.Name, filepath.Base(p.LocalPath))
	}
}

func TestManager_EnsureDefault_DoesNotCreateWhenActiveExists(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	// Pre-create an active project.
	existing := &Project{
		ID: "existing", Name: "alpha", Mode: ModeGit,
		LocalPath: "/tmp/alpha", Status: "active",
	}
	store.CreateProject(ctx, existing)

	p, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if p.ID != "existing" {
		t.Errorf("expected existing, got %s", p.ID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/project/ -run 'TestManager_GetActive|TestManager_DeactivateActive|TestManager_EnsureDefault' -v`
Expected: FAIL — `pm.GetActive undefined`, `pm.EnsureDefault undefined`, `pm.DeactivateActive undefined`

- [ ] **Step 3: Implement `GetActive`, `DeactivateActive`, `EnsureDefault` in `manager.go`**

Add to `internal/project/manager.go`:

```go
// GetActive returns the currently active project, or nil if none.
func (pm *ProjectManager) GetActive(ctx context.Context) (*Project, error) {
	return pm.store.GetActive(ctx)
}

// DeactivateActive sets all active projects to inactive.
func (pm *ProjectManager) DeactivateActive(ctx context.Context) error {
	return pm.store.DeactivateActive(ctx)
}

// EnsureDefault ensures at least one active project exists. If none exists,
// creates a new project under base_dir/<short-uuid>, initializes git, writes
// the sidecar, and registers it as active. If an active project already
// exists, returns it without creating a new one.
func (pm *ProjectManager) EnsureDefault(ctx context.Context) (*Project, error) {
	// Check if an active project already exists.
	existing, err := pm.GetActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("check active project: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	// Generate a short hex ID (8 chars = 4 bytes, enough for uniqueness
	// within a single user's project directory).
	shortID := pm.generateShortID()
	localPath := filepath.Join(pm.cfg.BaseDir, shortID)

	// Create the directory.
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	// Initialize git.
	if err := pm.runGit(ctx, localPath, "init"); err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}

	// Set default branch.
	branch := pm.cfg.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	// Configure git to use the desired default branch.
	_ = pm.runGit(ctx, localPath, "symbolic-ref", "HEAD", "refs/heads/"+branch)

	// Configure user if not set (best-effort, ignore errors).
	_ = pm.runGit(ctx, localPath, "config", "user.email", "meept@local")
	_ = pm.runGit(ctx, localPath, "config", "user.name", "meept")

	// Write sidecar.
	if err := pm.WriteSidecarID(localPath, shortID); err != nil {
		return nil, fmt.Errorf("write sidecar: %w", err)
	}

	p := &Project{
		ID:        shortID,
		Name:      shortID,
		Mode:      ModeGit,
		LocalPath: localPath,
		Branch:    branch,
		Status:    "active",
	}

	if err := pm.store.CreateProject(ctx, p); err != nil {
		return nil, fmt.Errorf("create default project: %w", err)
	}

	pm.logger.Info("created default project",
		"id", shortID,
		"path", localPath,
	)

	return p, nil
}

// generateShortID returns an 8-character hex string using crypto/rand.
func (pm *ProjectManager) generateShortID() string {
	b := make([]byte, 4)
	if _, err := crypto_rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}
```

Add `crypto_rand "crypto/rand"` and `"encoding/hex"` to the import block in `manager.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/project/ -run 'TestManager_GetActive|TestManager_DeactivateActive|TestManager_EnsureDefault' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/manager.go internal/project/manager_test.go
git commit -m "feat(project): add GetActive, DeactivateActive, EnsureDefault manager methods"
```

---

## Task 5: Manager — `CreateOrResolve` for shorthand name resolution

**Files:**
- Modify: `internal/project/manager.go`
- Test: `internal/project/manager_test.go`

When a user types `/project myapp`, the daemon resolves `myapp` to `base_dir/myapp`, creates the directory, runs `git init`, writes the sidecar, and registers it.

- [ ] **Step 1: Write failing test**

Add to `internal/project/manager_test.go`:

```go
func TestManager_CreateOrResolve_ShorthandName(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	p, err := pm.CreateOrResolve(ctx, "myapp")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}
	if p.Name != "myapp" {
		t.Errorf("Name = %q, want %q", p.Name, "myapp")
	}
	expectedPath := filepath.Join(pm.cfg.BaseDir, "myapp")
	if p.LocalPath != expectedPath {
		t.Errorf("LocalPath = %q, want %q", p.LocalPath, expectedPath)
	}
	if p.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeGit)
	}
	// Directory should exist.
	if _, err := os.Stat(p.LocalPath); os.IsNotExist(err) {
		t.Errorf("project dir not created: %s", p.LocalPath)
	}
	// .git should exist.
	if _, err := os.Stat(filepath.Join(p.LocalPath, ".git")); os.IsNotExist(err) {
		t.Errorf(".git not initialized")
	}
	// Sidecar should exist.
	sid, err := pm.ReadSidecarID(p.LocalPath)
	if err != nil {
		t.Fatalf("ReadSidecarID: %v", err)
	}
	if sid != p.ID {
		t.Errorf("sidecar = %q, want %q", sid, p.ID)
	}
}

func TestManager_CreateOrResolve_ExistingShorthand(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// First call creates the project.
	p1, err := pm.CreateOrResolve(ctx, "myapp")
	if err != nil {
		t.Fatalf("CreateOrResolve first: %v", err)
	}

	// Second call should return the same project (idempotent).
	p2, err := pm.CreateOrResolve(ctx, "myapp")
	if err != nil {
		t.Fatalf("CreateOrResolve second: %v", err)
	}
	if p2.ID != p1.ID {
		t.Errorf("second call returned different ID: %q vs %q", p2.ID, p1.ID)
	}
}

func TestManager_CreateOrResolve_AbsolutePathWithGit(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// Create a real git repo in a temp dir.
	repoDir := filepath.Join(t.TempDir(), "myrepo")
	os.MkdirAll(repoDir, 0o755)
	initGitRepo(t, repoDir)

	p, err := pm.CreateOrResolve(ctx, repoDir)
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}
	if p.Mode != ModeGit {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeGit)
	}
	if p.LocalPath != repoDir {
		t.Errorf("LocalPath = %q, want %q", p.LocalPath, repoDir)
	}
}

func TestManager_CreateOrResolve_AbsolutePathNoGit(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// A directory with no .git.
	localDir := filepath.Join(t.TempDir(), "localproj")
	os.MkdirAll(localDir, 0o755)

	p, err := pm.CreateOrResolve(ctx, localDir)
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}
	if p.Mode != ModeLocal {
		t.Errorf("Mode = %q, want %q", p.Mode, ModeLocal)
	}
	if p.LocalPath != localDir {
		t.Errorf("LocalPath = %q, want %q", p.LocalPath, localDir)
	}
}

func TestManager_CreateOrResolve_AdoptsRenamedDir(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	// Create a project via shorthand.
	p, err := pm.CreateOrResolve(ctx, "original")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}

	// Simulate external rename: move the directory.
	renamedPath := filepath.Join(filepath.Dir(p.LocalPath), "renamed")
	if err := os.Rename(p.LocalPath, renamedPath); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Now resolve the renamed path — should adopt via sidecar.
	adopted, err := pm.CreateOrResolve(ctx, renamedPath)
	if err != nil {
		t.Fatalf("CreateOrResolve after rename: %v", err)
	}
	if adopted.ID != p.ID {
		t.Errorf("adopted ID = %q, want original %q", adopted.ID, p.ID)
	}
	if adopted.LocalPath != renamedPath {
		t.Errorf("adopted LocalPath = %q, want %q", adopted.LocalPath, renamedPath)
	}
	if adopted.Name != "renamed" {
		t.Errorf("adopted Name = %q, want %q", adopted.Name, "renamed")
	}

	// DB should reflect the new path.
	dbProj, err := store.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if dbProj.LocalPath != renamedPath {
		t.Errorf("DB LocalPath = %q, want %q", dbProj.LocalPath, renamedPath)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/project/ -run TestManager_CreateOrResolve -v`
Expected: FAIL — `pm.CreateOrResolve undefined`

- [ ] **Step 3: Implement `CreateOrResolve` in `manager.go`**

Add to `internal/project/manager.go`:

```go
// CreateOrResolve resolves a user-provided project argument.
//
// - Absolute path with .git → DetectFromPath (auto-register or return existing)
// - Absolute path without .git → check for sidecar (adopt if found),
//   otherwise RegisterLocal
// - Shorthand name (not absolute) → resolve to base_dir/<name>, mkdir -p,
//   git init if no .git, write sidecar, register as git project
//
// For all paths, if a sidecar file exists inside the directory, the project
// is adopted (DB local_path and name updated) rather than re-registered.
func (pm *ProjectManager) CreateOrResolve(ctx context.Context, arg string) (*Project, error) {
	// Check if arg is an absolute path.
	if filepath.IsAbs(arg) {
		absPath, err := filepath.Abs(arg)
		if err != nil {
			return nil, fmt.Errorf("resolve path: %w", err)
		}

		// Check for sidecar — enables adoption of renamed directories.
		sidecarID, err := pm.ReadSidecarID(absPath)
		if err != nil {
			return nil, fmt.Errorf("read sidecar: %w", err)
		}

		if sidecarID != "" {
			// Sidecar exists — look up in DB.
			existing, err := pm.store.GetProject(ctx, sidecarID)
			if err == nil && existing != nil {
				// Adopt: update path and name if they've changed.
				name := filepath.Base(absPath)
				if existing.LocalPath != absPath || existing.Name != name {
					if err := pm.store.UpdateProjectPath(ctx, sidecarID, absPath, name); err != nil {
						return nil, fmt.Errorf("adopt project path: %w", err)
					}
					existing.LocalPath = absPath
					existing.Name = name
				}
				return existing, nil
			}
			// Sidecar exists but project not in DB — re-register with the
			// sidecar's UUID.
			name := filepath.Base(absPath)
			p := &Project{
				ID:        sidecarID,
				Name:      name,
				Mode:      ModeGit,
				LocalPath: absPath,
				Branch:    pm.cfg.DefaultBranch,
				Status:    "active",
			}
			if p.Branch == "" {
				p.Branch = "main"
			}
			if err := pm.store.CreateProject(ctx, p); err != nil {
				return nil, fmt.Errorf("re-register from sidecar: %w", err)
			}
			return p, nil
		}

		// No sidecar — check for .git.
		if _, err := os.Stat(filepath.Join(absPath, ".git")); err == nil {
			// Git repo — use DetectFromPath (auto-registers or returns existing).
			return pm.DetectFromPath(ctx, absPath)
		}

		// No .git, no sidecar — register as local project.
		name := filepath.Base(absPath)
		return pm.RegisterLocal(ctx, "", name, absPath)
	}

	// Shorthand name: resolve to base_dir/<arg>.
	localPath := filepath.Join(pm.cfg.BaseDir, arg)

	// Check if directory already exists with a sidecar.
	sidecarID, _ := pm.ReadSidecarID(localPath)
	if sidecarID != "" {
		existing, err := pm.store.GetProject(ctx, sidecarID)
		if err == nil && existing != nil {
			return existing, nil
		}
	}

	// Check if already registered by path.
	existing, err := pm.store.GetProjectByPath(ctx, localPath)
	if err == nil && existing != nil {
		return existing, nil
	}

	// Create directory, git init, write sidecar, register.
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	// Initialize git if .git doesn't exist.
	if _, err := os.Stat(filepath.Join(localPath, ".git")); os.IsNotExist(err) {
		if err := pm.runGit(ctx, localPath, "init"); err != nil {
			return nil, fmt.Errorf("git init: %w", err)
		}
		branch := pm.cfg.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		_ = pm.runGit(ctx, localPath, "symbolic-ref", "HEAD", "refs/heads/"+branch)
		_ = pm.runGit(ctx, localPath, "config", "user.email", "meept@local")
		_ = pm.runGit(ctx, localPath, "config", "user.name", "meept")
	}

	// Determine branch.
	branch, _ := pm.gitOutput(ctx, localPath, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = pm.cfg.DefaultBranch
		if branch == "" {
			branch = "main"
		}
	}

	// Use the sidecar ID if it exists, otherwise generate a new one.
	id := sidecarID
	if id == "" {
		id = pm.generateShortID()
	}

	// Write sidecar.
	if err := pm.WriteSidecarID(localPath, id); err != nil {
		return nil, fmt.Errorf("write sidecar: %w", err)
	}

	p := &Project{
		ID:        id,
		Name:      arg,
		Mode:      ModeGit,
		LocalPath: localPath,
		Branch:    branch,
		Status:    "active",
	}

	if err := pm.store.CreateProject(ctx, p); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}

	pm.logger.Info("created project from shorthand",
		"id", id,
		"name", arg,
		"path", localPath,
	)

	return p, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/project/ -run TestManager_CreateOrResolve -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/manager.go internal/project/manager_test.go
git commit -m "feat(project): add CreateOrResolve for shorthand name + sidecar adoption"
```

---

## Task 6: Manager — `Rename` method

**Files:**
- Modify: `internal/project/manager.go`
- Test: `internal/project/manager_test.go`

Renames a project's directory (only for projects under `base_dir`), updates the DB, writes the sidecar, and updates all sessions.

- [ ] **Step 1: Write failing test**

Add to `internal/project/manager_test.go`:

```go
func TestManager_Rename_UnderBaseDir(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// Create a project via shorthand.
	p, err := pm.CreateOrResolve(ctx, "oldname")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}

	// Rename it.
	renamed, err := pm.Rename(ctx, p.ID, "newname")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.Name != "newname" {
		t.Errorf("Name = %q, want %q", renamed.Name, "newname")
	}
	expectedPath := filepath.Join(pm.cfg.BaseDir, "newname")
	if renamed.LocalPath != expectedPath {
		t.Errorf("LocalPath = %q, want %q", renamed.LocalPath, expectedPath)
	}

	// Old directory should not exist.
	if _, err := os.Stat(p.LocalPath); !os.IsNotExist(err) {
		t.Errorf("old dir should not exist: %s", p.LocalPath)
	}
	// New directory should exist.
	if _, err := os.Stat(renamed.LocalPath); os.IsNotExist(err) {
		t.Errorf("new dir should exist: %s", renamed.LocalPath)
	}
	// Sidecar should still have the same ID.
	sid, _ := pm.ReadSidecarID(renamed.LocalPath)
	if sid != p.ID {
		t.Errorf("sidecar = %q, want %q", sid, p.ID)
	}
}

func TestManager_Rename_ExternalProjectFails(t *testing.T) {
	pm, store := newTestManager(t)
	ctx := context.Background()

	// Create a project NOT under base_dir.
	p := &Project{
		ID: "ext1", Name: "ext", Mode: ModeGit,
		LocalPath: "/tmp/ext-project", Status: "active",
	}
	store.CreateProject(ctx, p)

	_, err := pm.Rename(ctx, "ext1", "newname")
	if err == nil {
		t.Fatal("expected error renaming external project")
	}
	if !strings.Contains(err.Error(), "under base_dir") {
		t.Errorf("expected 'under base_dir' error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestManager_Rename -v`
Expected: FAIL — `pm.Rename undefined`

- [ ] **Step 3: Implement `Rename` in `manager.go`**

Add to `internal/project/manager.go`:

```go
// Rename renames a project's directory and updates the DB.
// Only projects whose local_path is under base_dir can be renamed.
// The sidecar file travels with the directory. All sessions pointing
// at the old path are updated to the new path.
func (pm *ProjectManager) Rename(ctx context.Context, projectID, newName string) (*Project, error) {
	p, err := pm.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project for rename: %w", err)
	}

	// Only allow renaming projects under base_dir.
	baseDir, err := filepath.Abs(pm.cfg.BaseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve base_dir: %w", err)
	}
	projPath, err := filepath.Abs(p.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}
	if !strings.HasPrefix(projPath, baseDir+string(filepath.Separator)) {
		return nil, fmt.Errorf("cannot rename external project %q: only projects under base_dir can be renamed", p.LocalPath)
	}

	newPath := filepath.Join(baseDir, newName)

	// Move the directory.
	if err := os.Rename(p.LocalPath, newPath); err != nil {
		return nil, fmt.Errorf("rename directory: %w", err)
	}

	// Update DB.
	if err := pm.store.UpdateProjectPath(ctx, projectID, newPath, newName); err != nil {
		// Best-effort: try to move the directory back on DB failure.
		os.Rename(newPath, p.LocalPath)
		return nil, fmt.Errorf("update project path in DB: %w", err)
	}

	// Update sessions pointing at old path.
	if pm.sessionStore != nil {
		if err := pm.sessionStore.UpdateSessionsProjectPath(ctx, p.LocalPath, newPath); err != nil {
			pm.logger.Warn("failed to update sessions after rename",
				"old_path", p.LocalPath,
				"new_path", newPath,
				"error", err,
			)
		}
	}

	p.LocalPath = newPath
	p.Name = newName

	pm.logger.Info("renamed project",
		"id", projectID,
		"old_path", p.LocalPath,
		"new_path", newPath,
		"new_name", newName,
	)

	return p, nil
}
```

Add a `sessionStore session.Store` field to `ProjectManager` and update the constructor:

```go
type ProjectManager struct {
	store        *Store
	recentsStore *RecentsStore
	cfg          config.ProjectsConfig
	logger       *slog.Logger
	sessionStore SessionPathUpdater
}

// SessionPathUpdater is the narrow interface needed for bulk session path updates.
type SessionPathUpdater interface {
	UpdateSessionsProjectPath(ctx context.Context, oldPath, newPath string) error
}
```

Update `NewProjectManager`:

```go
func NewProjectManager(store *Store, recents *RecentsStore, cfg config.ProjectsConfig, logger *slog.Logger) *ProjectManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProjectManager{
		store:        store,
		recentsStore: recents,
		cfg:          cfg,
		logger:       logger,
	}
}

// SetSessionStore wires the session store for bulk path updates on rename.
func (pm *ProjectManager) SetSessionStore(s SessionPathUpdater) {
	pm.sessionStore = s
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/project/ -run TestManager_Rename -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/manager.go internal/project/manager_test.go
git commit -m "feat(project): add Rename method with base_dir restriction and session updates"
```

---

## Task 7: Wire session store into ProjectManager in daemon

**Files:**
- Modify: `internal/daemon/components.go`

- [ ] **Step 1: Add `SetSessionStore` call after project manager and session store are both created**

In `internal/daemon/components.go`, after the project manager creation block (around line 1498), add a call to wire the session store. The session store is created earlier in `wireSessionStore`. Find the spot after `c.ProjectManager = pm` and add:

```go
			// Wire session store into project manager for bulk path updates on rename.
			if c.SessionStore != nil {
				pm.SetSessionStore(c.SessionStore)
			}
```

This should go right after `c.ProjectManager = pm` (line 1499), before the orphaned worktrees cleanup.

- [ ] **Step 2: Build to verify compilation**

Run: `go build ./internal/daemon/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/components.go
git commit -m "feat(daemon): wire session store into project manager for rename support"
```

---

## Task 8: SessionService — inherit active project on create

**Files:**
- Modify: `internal/services/session_service.go`
- Modify: `internal/services/service.go`
- Test: `internal/services/session_service_test.go`

- [ ] **Step 1: Write failing test for project inheritance**

Add to `internal/services/session_service_test.go` (create if needed):

```go
package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
)

func newTestProjectManager(t *testing.T) *project.ProjectManager {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := project.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.ProjectsConfig{
		BaseDir:       filepath.Join(dir, "projects"),
		DefaultBranch: "main",
	}
	os.MkdirAll(cfg.BaseDir, 0o755)
	return project.NewProjectManager(store, nil, cfg, nil)
}

func TestCreateSession_InheritsActiveProject(t *testing.T) {
	// Create a session store + project manager with an active project.
	sessionStore := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t)
	ctx := context.Background()

	// Create an active project.
	activeProj, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}

	// Wire project manager into session service.
	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(pm)

	// Create a session — should inherit the active project.
	sess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectID != activeProj.ID {
		t.Errorf("ProjectID = %q, want %q", sess.ProjectID, activeProj.ID)
	}
	if sess.ProjectPath != activeProj.LocalPath {
		t.Errorf("ProjectPath = %q, want %q", sess.ProjectPath, activeProj.LocalPath)
	}
}

func TestCreateSession_CreatesDefaultWhenNoneActive(t *testing.T) {
	sessionStore := session.NewMemoryStore(nil)
	pm := newTestProjectManager(t)
	ctx := context.Background()

	// No active project yet.
	svc := NewSessionService(sessionStore)
	svc.SetProjectManager(pm)

	// Create a session — should auto-create a default project.
	sess, err := svc.CreateSession(ctx, CreateSessionRequest{Name: "test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ProjectID == "" {
		t.Error("ProjectID should not be empty")
	}
	if sess.ProjectPath == "" {
		t.Error("ProjectPath should not be empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run TestCreateSession_Inherits -v`
Expected: FAIL — `svc.SetProjectManager undefined`

- [ ] **Step 3: Add `ProjectManager` to `SessionService` and update `CreateSession`**

In `internal/services/session_service.go`, add a `pm` field and setter:

```go
type SessionService struct {
	store session.Store
	pm    ProjectResolver
}

// ProjectResolver is the narrow interface SessionService needs from the
// project manager: ensuring a default project exists and returning it.
type ProjectResolver interface {
	EnsureDefault(ctx context.Context) (*project.Project, error)
	GetActive(ctx context.Context) (*project.Project, error)
}

func NewSessionService(s session.Store) *SessionService {
	return &SessionService{store: s}
}

// SetProjectManager wires the project manager for default project resolution
// on session creation.
func (s *SessionService) SetProjectManager(pm ProjectResolver) {
	s.pm = pm
}
```

Add `"github.com/caimlas/meept/internal/project"` to imports.

Update `CreateSession`:

```go
func (s *SessionService) CreateSession(ctx context.Context, req CreateSessionRequest) (*session.Session, error) {
	if s.store == nil {
		return nil, wrapError("session", "CreateSession", ErrUnavailable)
	}
	name := req.Name
	if name == "" {
		name = "default"
	}
	sess, err := s.store.Create(name)
	if err != nil {
		return nil, wrapError("session", "CreateSession", err)
	}

	// Inherit or create the active project so every session has a working
	// directory.
	if s.pm != nil {
		p, err := s.pm.EnsureDefault(ctx)
		if err == nil && p != nil {
			s.store.SetProject(sess.ID, p.ID, p.LocalPath)
			sess.ProjectID = p.ID
			sess.ProjectPath = p.LocalPath
		}
	}

	return sess, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/ -run TestCreateSession_Inherits -v`
Expected: PASS

- [ ] **Step 5: Wire `ProjectManager` into `SessionService` in `NewRegistry`**

In `internal/services/service.go`, update the session service creation block (around line 125-128):

```go
	if cfg.SessionStore != nil {
		reg.Session = NewSessionService(cfg.SessionStore)
		reg.SessionStore = cfg.SessionStore
		reg.Thread = NewThreadService(cfg.SessionStore)
		if cfg.ProjectManager != nil {
			reg.Session.SetProjectManager(cfg.ProjectManager)
		}
	}
```

- [ ] **Step 6: Build to verify compilation**

Run: `go build ./internal/services/ ./internal/daemon/`
Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add internal/services/session_service.go internal/services/session_service_test.go internal/services/service.go
git commit -m "feat(session): inherit active project or create default on session creation"
```

---

## Task 9: RPC — update `handleSet` for shorthand resolution + single active project

**Files:**
- Modify: `internal/rpc/projects.go`
- Test: `internal/rpc/projects_test.go` (create if needed)

- [ ] **Step 1: Write failing test for shorthand resolution in `handleSet`**

Create `internal/rpc/projects_test.go`:

```go
package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
)

func newTestProjectHandler(t *testing.T) (*ProjectHandler, *project.ProjectManager) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := project.NewStore(dbPath, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := config.ProjectsConfig{
		BaseDir:       filepath.Join(dir, "projects"),
		DefaultBranch: "main",
	}
	os.MkdirAll(cfg.BaseDir, 0o755)

	pm := project.NewProjectManager(store, nil, cfg, nil)
	sessionStore := session.NewMemoryStore(nil)
	h := NewProjectHandler(pm, sessionStore)
	h.SetMessageBus(bus.NewMessageBus(nil))
	return h, pm
}

func TestHandleSet_ShorthandName_CreatesProject(t *testing.T) {
	h, pm := newTestProjectHandler(t)
	ctx := context.Background()

	// Create a session to bind to.
	sessionStore := session.NewMemoryStore(nil)
	sess, _ := sessionStore.Create("test")

	params, _ := json.Marshal(map[string]string{
		"session_id": sess.ID,
		"path":       "myapp",
	})

	result, err := h.handleSet(ctx, params)
	if err != nil {
		t.Fatalf("handleSet: %v", err)
	}

	m := result.(map[string]any)
	projectID, _ := m["project_id"].(string)
	if projectID == "" {
		t.Fatal("expected non-empty project_id")
	}

	// Project should exist under base_dir.
	p, err := pm.Get(ctx, projectID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	expectedPath := filepath.Join(pm.Config().BaseDir, "myapp")
	if p.LocalPath != expectedPath {
		t.Errorf("LocalPath = %q, want %q", p.LocalPath, expectedPath)
	}
}

func TestHandleSet_DeactivatesPreviousActive(t *testing.T) {
	h, pm := newTestProjectHandler(t)
	ctx := context.Background()

	sessionStore := session.NewMemoryStore(nil)
	sess, _ := sessionStore.Create("test")

	// Create a first project and set it active.
	p1, _ := pm.CreateOrResolve(ctx, "first")
	pm.store.CreateProject(ctx, &project.Project{
		ID: "manual-active", Name: "manual", Mode: project.ModeGit,
		LocalPath: "/tmp/manual", Status: "active",
	})

	// Set first project.
	params1, _ := json.Marshal(map[string]string{
		"session_id": sess.ID,
		"path":       "first",
	})
	h.handleSet(ctx, params1)

	// Set second project.
	params2, _ := json.Marshal(map[string]string{
		"session_id": sess.ID,
		"path":       "second",
	})
	h.handleSet(ctx, params2)

	// Only one active project should exist.
	active, err := pm.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if active == nil {
		t.Fatal("expected an active project")
	}
	_ = p1 // p1 was set, then overridden
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rpc/ -run 'TestHandleSet_ShorthandName|TestHandleSet_DeactivatesPrevious' -v`
Expected: FAIL — shorthand `myapp` won't resolve because `handleSet` passes it to `DetectFromPath` which requires a `.git` dir.

- [ ] **Step 3: Update `handleSet` in `rpc/projects.go`**

Replace the path-resolution block in `handleSet` (lines 202-216) with:

```go
	if req.Path != "" {
		// Use CreateOrResolve which handles both shorthand names and
		// absolute paths, including sidecar-based adoption.
		p, err = pm.CreateOrResolve(ctx, req.Path)
		if err != nil {
			return nil, fmt.Errorf("resolve project: %w", err)
		}
	} else if req.SessionID == "" || req.ProjectID == "" {
		return nil, fmt.Errorf("session_id and project_id, or path, are required")
	} else {
		// Verify project exists
		p, err = pm.Get(ctx, req.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("get project: %w", err)
		}
	}

	// Deactivate any previously active project before activating this one.
	if err := pm.DeactivateActive(ctx); err != nil {
		h.logger.Warn("failed to deactivate previous active project", "error", err)
	}

	// Set this project as active.
	p.Status = "active"
	if err := pm.SetStatus(ctx, p.ID, "active"); err != nil {
		h.logger.Warn("failed to set project active", "error", err)
	}
```

Add `SetStatus` to `ProjectManager` in `manager.go`:

```go
// SetStatus updates a project's status.
func (pm *ProjectManager) SetStatus(ctx context.Context, id, status string) error {
	p, err := pm.store.GetProject(ctx, id)
	if err != nil {
		return err
	}
	p.Status = status
	return pm.store.UpdateProject(ctx, p)
}
```

Also add a `Config()` accessor to `ProjectManager`:

```go
// Config returns the project manager's configuration.
func (pm *ProjectManager) Config() config.ProjectsConfig {
	return pm.cfg
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rpc/ -run 'TestHandleSet_ShorthandName|TestHandleSet_DeactivatesPrevious' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rpc/projects.go internal/rpc/projects_test.go internal/project/manager.go
git commit -m "feat(rpc): project.set resolves shorthand names and enforces single active project"
```

---

## Task 10: RPC — `handleRename` handler

**Files:**
- Modify: `internal/rpc/projects.go`
- Test: `internal/rpc/projects_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/rpc/projects_test.go`:

```go
func TestHandleRename(t *testing.T) {
	h, pm := newTestProjectHandler(t)
	ctx := context.Background()

	// Create a project via shorthand.
	p, err := pm.CreateOrResolve(ctx, "oldname")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}

	params, _ := json.Marshal(map[string]string{
		"id":      p.ID,
		"new_name": "newname",
	})

	result, err := h.handleRename(ctx, params)
	if err != nil {
		t.Fatalf("handleRename: %v", err)
	}

	m := result.(map[string]any)
	name, _ := m["name"].(string)
	if name != "newname" {
		t.Errorf("name = %q, want %q", name, "newname")
	}
	path, _ := m["local_path"].(string)
	expectedPath := filepath.Join(pm.Config().BaseDir, "newname")
	if path != expectedPath {
		t.Errorf("local_path = %q, want %q", path, expectedPath)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rpc/ -run TestHandleRename -v`
Expected: FAIL — `h.handleRename undefined`

- [ ] **Step 3: Implement `handleRename` and register it**

Add to `internal/rpc/projects.go`:

```go
// handleRename handles project.rename RPC calls.
func (h *ProjectHandler) handleRename(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		ID      string `json:"id"`
		NewName string `json:"new_name"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.ID == "" || req.NewName == "" {
		return nil, fmt.Errorf("id and new_name are required")
	}

	p, err := pm.Rename(ctx, req.ID, req.NewName)
	if err != nil {
		return nil, fmt.Errorf("rename project: %w", err)
	}

	return map[string]any{
		RPCKeyStatus:  "renamed",
		"id":          p.ID,
		"name":        p.Name,
		"local_path":  p.LocalPath,
	}, nil
}
```

Register it in `RegisterProjectMethods`:

```go
	server.RegisterHandler("project.rename", h.handleRename)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rpc/ -run TestHandleRename -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rpc/projects.go internal/rpc/projects_test.go
git commit -m "feat(rpc): add project.rename RPC handler"
```

---

## Task 11: HTTP — `POST /api/v1/projects/{id}/rename` endpoint

**Files:**
- Modify: `internal/comm/http/api_handlers.go`
- Modify: `internal/comm/http/server.go`

- [ ] **Step 1: Add route registration in `server.go`**

Find the projects routes section (search for `project` routes) and add:

```go
	mux.HandleFunc("POST /api/v1/projects/{id}/rename", s.handleProjectRename)
```

- [ ] **Step 2: Add handler in `api_handlers.go`**

```go
// handleProjectRename handles POST /api/v1/projects/{id}/rename.
func (s *Server) handleProjectRename(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "project id is required")
		return
	}

	var body struct {
		NewName string `json:"new_name"`
	}
	if !s.readJSON(w, r, &body) {
		return
	}
	if body.NewName == "" {
		s.writeError(w, http.StatusBadRequest, "new_name is required")
		return
	}

	result, err := s.rpcCall(r.Context(), "project.rename", map[string]string{
		"id":       id,
		"new_name": body.NewName,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 3: Build and verify**

Run: `go build ./internal/comm/http/`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/comm/http/api_handlers.go internal/comm/http/server.go
git commit -m "feat(http): add POST /api/v1/projects/{id}/rename endpoint"
```

---

## Task 12: TUI — `/project rename <name>` subcommand

**Files:**
- Modify: `internal/tui/command_handler.go`

- [ ] **Step 1: Add `rename` case to `executeProject` switch**

In `internal/tui/command_handler.go`, in the `executeProject` switch statement (around line 1655), add:

```go
	case "rename":
		return h.executeProjectRename(args[1:])
```

- [ ] **Step 2: Add a `getProjectID` accessor to `CommandHandler`**

The `CommandHandler` doesn't have direct access to the app's `currentProjectID`. Add a function-typed field similar to `getChatModel`:

In `internal/tui/command_handler.go`, add to the `CommandHandler` struct:

```go
type CommandHandler struct {
	rpc          *RPCClient
	getChatModel func() *models.ChatModel
	getProjectID func() string
	skillCommand *commands.SkillCommand
	notifier     NotificationToggler
}
```

Add the option:

```go
// WithProjectIDGetter sets a function that returns the current project ID.
func WithProjectIDGetter(fn func() string) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.getProjectID = fn
	}
}
```

Wire it in the app where the `CommandHandler` is constructed (find the `NewCommandHandler` call in `internal/tui/app.go` and add `WithProjectIDGetter(func() string { return a.currentProjectID })`).

- [ ] **Step 3: Implement `executeProjectRename`**

```go
// executeProjectRename renames the current project's directory.
func (h *CommandHandler) executeProjectRename(args []string) *CommandResult {
	if len(args) == 0 || args[0] == "" {
		return &CommandResult{
			Output:  "usage: /project rename <new-name>",
			IsError: true,
		}
	}

	newName := args[0]

	if h.getProjectID == nil {
		return &CommandResult{
			Output:  "project ID accessor not available",
			IsError: true,
		}
	}
	projectID := h.getProjectID()
	if projectID == "" {
		return &CommandResult{
			Output:  "no active project to rename",
			IsError: true,
		}
	}

	// Call project.rename RPC.
	result, err := h.rpc.Call("project.rename", map[string]string{
		"id":       projectID,
		"new_name": newName,
	})
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("rename failed: %v", err),
			IsError: true,
		}
	}

	return &CommandResult{
		Output: fmt.Sprintf("renamed project to '%s'", newName),
	}
}
```

- [ ] **Step 4: Update help text**

In the help text for `/project` (around line 396-415), add:

```
  /project rename <name>  rename current project directory
```

- [ ] **Step 5: Build to verify compilation**

Run: `go build ./internal/tui/`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/tui/command_handler.go
git commit -m "feat(tui): add /project rename <name> subcommand"
```

---

## Task 13: Flutter GUI — `/project rename <name>` support

**Files:**
- Modify: `ui/flutter_ui/lib/features/chat/chat_input.dart`
- Modify: `ui/flutter_ui/lib/services/sdk_client.dart`

- [ ] **Step 1: Add `renameProject` to `sdk_client.dart`**

Add to `ui/flutter_ui/lib/services/sdk_client.dart` after the `setProject` method:

```dart
  /// Calls `project.rename` via the bus/call RPC bridge.
  Future<Map<String, dynamic>> renameProject({
    required String projectId,
    required String newName,
  }) async {
    final envelope = await _post('/api/v1/bus/call', body: {
      'method': 'project.rename',
      'params': {
        'id': projectId,
        'new_name': newName,
      },
    });
    final inner = envelope['result'];
    if (inner is Map<String, dynamic>) return inner;
    if (envelope.containsKey('status') || envelope.containsKey('name')) {
      return envelope;
    }
    throw StateError('renameProject: unexpected envelope shape: $envelope');
  }
```

- [ ] **Step 2: Add `/project rename` parsing in `chat_input.dart`**

In `_tryHandleSlashCommand`, update the `/project` case to detect the `rename` subcommand. Before the existing `unawaited(_handleProjectSetCommand(arg))` line, add:

```dart
        // Check for "rename <new-name>" subcommand.
        if (arg.startsWith('rename ')) {
          final newName = arg.substring('rename '.length).trim();
          if (newName.isNotEmpty) {
            unawaited(_handleProjectRenameCommand(newName));
            return true;
          }
        }
```

- [ ] **Step 3: Implement `_handleProjectRenameCommand`**

```dart
  /// Handle `/project rename <new-name>`: renames the current project's
  /// directory via the daemon's project.rename RPC.
  Future<void> _handleProjectRenameCommand(String newName) async {
    final session = ref.read(activeSessionProvider);
    final sessionId = session?.id ?? widget.sessionId;
    if (sessionId.isEmpty) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('no active session'),
            backgroundColor: CyberpunkColors.redAlert,
          ),
        );
      }
      return;
    }

    final projectId = ref.read(currentProjectProvider).id;
    if (projectId.isEmpty) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('no active project to rename'),
            backgroundColor: CyberpunkColors.redAlert,
          ),
        );
      }
      return;
    }

    try {
      final sdk = ref.read(sdkClientProvider);
      await sdk.renameProject(projectId: projectId, newName: newName);
      await ref.read(currentProjectProvider.notifier).refresh();
      if (mounted) {
        showStatusMessage(ref, 'project renamed to: $newName');
        _resetInputState();
      }
    } catch (e) {
      debugPrint('[chat_input] /project rename failed: $e');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('project rename failed: $e'),
            backgroundColor: CyberpunkColors.redAlert,
          ),
        );
      }
    }
  }
```

- [ ] **Step 4: Build to verify**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/features/chat/chat_input.dart ui/flutter_ui/lib/services/sdk_client.dart
git commit -m "feat(flutter): add /project rename <name> support"
```

---

## Task 14: Full integration test — session creation inherits project

**Files:**
- Test: `internal/project/manager_test.go`

- [ ] **Step 1: Write end-to-end test for the resolution chain**

```go
func TestManager_ResolutionChain_InheritThenSwitch(t *testing.T) {
	pm, _ := newTestManager(t)
	ctx := context.Background()

	// First call to EnsureDefault creates a uuid project.
	p1, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault first: %v", err)
	}

	// Second call returns the same project (already active).
	p2, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault second: %v", err)
	}
	if p2.ID != p1.ID {
		t.Errorf("second EnsureDefault returned different project: %q vs %q", p2.ID, p1.ID)
	}

	// CreateOrResolve with a shorthand name deactivates the old and creates new.
	p3, err := pm.CreateOrResolve(ctx, "myapp")
	if err != nil {
		t.Fatalf("CreateOrResolve: %v", err)
	}

	// Deactivate old, activate new.
	pm.DeactivateActive(ctx)
	pm.SetStatus(ctx, p3.ID, "active")

	// GetActive should now return the new project.
	active, _ := pm.GetActive(ctx)
	if active == nil || active.ID != p3.ID {
		t.Errorf("GetActive = %+v, want %s", active, p3.ID)
	}

	// EnsureDefault should return the new active project, not create another.
	p4, err := pm.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault after switch: %v", err)
	}
	if p4.ID != p3.ID {
		t.Errorf("EnsureDefault returned %q, want %q", p4.ID, p3.ID)
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/project/ -run TestManager_ResolutionChain -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/project/manager_test.go
git commit -m "test(project): add end-to-end resolution chain test"
```

---

## Task 15: Documentation update

**Files:**
- Modify: `docs/workflows/projects.md`

- [ ] **Step 1: Update the project workflow documentation**

Add a section on project resolution and the new `/project rename` command. Key content:

```markdown
## Project Resolution

Every session gets a working directory automatically:

1. **Inherit**: If an active project exists, new sessions inherit its path.
2. **Auto-create**: If no active project exists, one is created under
   `projects.base_dir/<short-uuid>` with `git init`.
3. **Override**: `/project <name>` switches to a different project at any time.

### `/project` subcommands

| Command | Behavior |
|---|---|
| `/project` | Show current project info |
| `/project <name>` | Resolve to `base_dir/<name>`, mkdir, git init, switch |
| `/project /abs/path` | Detect git or register as local, switch |
| `/project rename <new-name>` | Rename current project's directory (base_dir projects only) |
| `/project list` | List all registered projects |
| `/project sync` | Git pull current project |
| `/project status` | Show git status of current project |

### Sidecar files

Each project directory contains `.meept/project_id` storing the project's
UUID. This enables automatic adoption when a directory is renamed outside
meept — the next `/project /new/path` detects the sidecar and updates the
database rather than creating a duplicate.

### Single active project

Only one project is `active` at a time. Switching projects via `/project`
deactivates the previous one.
```

- [ ] **Step 2: Commit**

```bash
git add docs/workflows/projects.md
git commit -m "docs: update project workflow docs for resolution chain and rename"
```

---

## Task 16: Full build and test sweep

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 2: Run all project + session + rpc + services tests**

Run: `go test ./internal/project/ ./internal/session/ ./internal/rpc/ ./internal/services/ -v`
Expected: all PASS

- [ ] **Step 3: Run race detector**

Run: `go test -race ./internal/project/ ./internal/session/ ./internal/services/`
Expected: all PASS

- [ ] **Step 4: Flutter analyze**

Run: `cd ui/flutter_ui && flutter analyze`
Expected: no new errors

- [ ] **Step 5: Final commit if any fixups needed**

```bash
git add -A
git commit -m "fix: address build/test sweep issues"
```
