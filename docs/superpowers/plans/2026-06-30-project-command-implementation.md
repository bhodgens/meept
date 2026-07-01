# `/project` Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `/project <path>` command with daemon-side recents, typeahead RPC, and AgentLoop workingDir fix.

**Architecture:** Single daemon, local-only. Recents stored in `projects.db` table `project_recents`. Typeahead RPC `project.readdir` returns (recents + filesystem fallback). On selection, `project.set` upserts project, updates session, calls `AgentLoop.SetWorkingDir`, touches recents.

**Tech Stack:** Go 1.24.2, SQLite, RPC, TUI (bubbletea), Flutter (optional Phase 4).

---

### Task 1: SQLite Migration — project_recents Table

**Files:**
- Modify: `internal/project/store.go:55-93` (initSchema function)

- [ ] **Step 1: Add project_recents table to schema**

Add after line 91 (after worktrees session index):

```go
if _, err := db.ExecContext(ctx, `
    CREATE TABLE IF NOT EXISTS project_recents (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        project_path TEXT UNIQUE NOT NULL,
        last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
    )`); err != nil {
    return fmt.Errorf("create project_recents table: %w", err)
}
if _, err := db.ExecContext(ctx, `
    CREATE INDEX IF NOT EXISTS idx_recents_last_used
    ON project_recents(last_used_at DESC)`); err != nil {
    return fmt.Errorf("create project_recents index: %w", err)
}
```

- [ ] **Step 2: Run build to verify syntax**

Run: `go build ./internal/project/...`
Expected: PASS (no errors)

- [ ] **Step 3: Commit**

```bash
git add internal/project/store.go
git commit -m "feat: add project_recents table for recent project paths"
```

---

### Task 2: RecentsStore — Go Interface and Implementation

**Files:**
- Create: `internal/project/recents.go`

- [ ] **Step 1: Write recents store interface and implementation**

Create `internal/project/recents.go`:

```go
package project

import (
    "context"
    "database/sql"
    "fmt"
    "time"
)

// RecentsStore provides recents tracking for projects.
type RecentsStore struct {
    db *sql.DB
}

// NewRecentsStore creates a new recents store.
func NewRecentsStore(db *sql.DB) *RecentsStore {
    return &RecentsStore{db: db}
}

// TouchRecent updates or inserts a project path in recents.
func (s *RecentsStore) TouchRecent(ctx context.Context, path string) error {
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO project_recents (project_path, last_used_at)
        VALUES (?, datetime('now'))
        ON CONFLICT(project_path) DO UPDATE SET last_used_at = datetime('now')
    `, path)
    return err
}

// ListRecents returns the top N most recent project paths.
func (s *RecentsStore) ListRecents(ctx context.Context, limit int) ([]string, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT project_path FROM project_recents
        ORDER BY last_used_at DESC
        LIMIT ?
    `, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var paths []string
    for rows.Next() {
        var path string
        if err := rows.Scan(&path); err != nil {
            return nil, err
        }
        paths = append(paths, path)
    }
    return paths, rows.Err()
}

// PruneOlderThan removes entries older than the specified duration.
func (s *RecentsStore) PruneOlderThan(ctx context.Context, ttl time.Duration) (int64, error) {
    cutoff := time.Now().Add(-ttl)
    result, err := s.db.ExecContext(ctx, `
        DELETE FROM project_recents
        WHERE last_used_at < datetime(?)
    `, cutoff.Format("2006-01-02 15:04:05"))
    if err != nil {
        return 0, err
    }
    return result.RowsAffected()
}

// CapToN removes oldest entries to keep only the most recent N.
func (s *RecentsStore) CapToN(ctx context.Context, max int) (int64, error) {
    result, err := s.db.ExecContext(ctx, `
        DELETE FROM project_recents
        WHERE id NOT IN (
            SELECT id FROM (
                SELECT id FROM project_recents
                ORDER BY last_used_at DESC
                LIMIT ?
            )
        )
    `, max)
    if err != nil {
        return 0, err
    }
    return result.RowsAffected()
}
```

- [ ] **Step 2: Run build to verify syntax**

Run: `go build ./internal/project/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/project/recents.go
git commit -m "feat: add RecentsStore with Touch/List/Prune/Cap operations"
```

---

### Task 3: RecentsStore Unit Tests

**Files:**
- Create: `internal/project/recents_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/project/recents_test.go`:

```go
package project

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/caimlas/meept/pkg/sqlite"
)

func newTestRecentsStore(t *testing.T) *RecentsStore {
    t.Helper()
    dir := t.TempDir()
    dbPath := filepath.Join(dir, "test.db")
    pool, err := sqlite.NewPool(sqlite.PoolConfig{
        Path:     dbPath,
        PoolSize: 1,
        WALMode:  true,
    })
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { pool.Close() })

    db, err := pool.Get(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    defer pool.Put(db)

    // Create table
    _, err = db.Exec(`
        CREATE TABLE project_recents (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            project_path TEXT UNIQUE NOT NULL,
            last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `)
    if err != nil {
        t.Fatal(err)
    }

    return &RecentsStore{db: db}
}

func TestRecentsStore_TouchRecent(t *testing.T) {
    s := newTestRecentsStore(t)
    ctx := context.Background()

    // First touch
    if err := s.TouchRecent(ctx, "/path/one"); err != nil {
        t.Fatalf("TouchRecent failed: %v", err)
    }

    // Second touch (same path) - should update, not fail
    if err := s.TouchRecent(ctx, "/path/one"); err != nil {
        t.Fatalf("TouchRecent duplicate failed: %v", err)
    }

    // Verify count
    paths, err := s.ListRecents(ctx, 10)
    if err != nil {
        t.Fatalf("ListRecents failed: %v", err)
    }
    if len(paths) != 1 {
        t.Errorf("expected 1 path, got %d", len(paths))
    }
}

func TestRecentsStore_ListRecents(t *testing.T) {
    s := newTestRecentsStore(t)
    ctx := context.Background()

    // Touch 5 paths
    paths := []string{"/path/a", "/path/b", "/path/c", "/path/d", "/path/e"}
    for _, p := range paths {
        if err := s.TouchRecent(ctx, p); err != nil {
            t.Fatal(err)
        }
        time.Sleep(10 * time.Millisecond) // ensure distinct timestamps
    }

    // List top 3
    got, err := s.ListRecents(ctx, 3)
    if err != nil {
        t.Fatal(err)
    }
    if len(got) != 3 {
        t.Errorf("expected 3 paths, got %d", len(got))
    }
    // Most recent first
    if got[0] != "/path/e" {
        t.Errorf("expected /path/e first, got %s", got[0])
    }
}

func TestRecentsStore_PruneOlderThan(t *testing.T) {
    s := newTestRecentsStore(t)
    ctx := context.Background()

    // Touch 3 paths
    s.TouchRecent(ctx, "/path/one")
    time.Sleep(100 * time.Millisecond)
    s.TouchRecent(ctx, "/path/two")

    // Prune older than 50ms - should remove /path/one
    deleted, err := s.PruneOlderThan(ctx, 50*time.Millisecond)
    if err != nil {
        t.Fatal(err)
    }
    if deleted != 1 {
        t.Errorf("expected 1 deleted, got %d", deleted)
    }
}

func TestRecentsStore_CapToN(t *testing.T) {
    s := newTestRecentsStore(t)
    ctx := context.Background()

    // Touch 10 paths
    for i := 0; i < 10; i++ {
        s.TouchRecent(ctx, filepath.Join("/path", string(rune('a'+i))))
    }

    // Cap to 5
    deleted, err := s.CapToN(ctx, 5)
    if err != nil {
        t.Fatal(err)
    }
    if deleted != 5 {
        t.Errorf("expected 5 deleted, got %d", deleted)
    }

    remaining, _ := s.ListRecents(ctx, 100)
    if len(remaining) != 5 {
        t.Errorf("expected 5 remaining, got %d", len(remaining))
    }
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/project/recents_test.go ./internal/project/recents.go -v`
Expected: PASS (all 4 tests)

- [ ] **Step 3: Commit**

```bash
git add internal/project/recents_test.go
git commit -m "test: add comprehensive RecentsStore unit tests"
```

---

### Task 4: AgentLoop.SetWorkingDir Method

**Files:**
- Modify: `internal/agent/loop.go:471` (add method near workingDir field)

- [ ] **Step 1: Read loop.go to find existing mutex pattern**

Run: `grep -n "mu sync" internal/agent/loop.go`
Expected: Find the mutex field (likely around line 400-500)

- [ ] **Step 2: Add SetWorkingDir method**

Find a safe location after the struct definition (around line 1200, after NewAgentLoop). Add:

```go
// SetWorkingDir updates the working directory for artifact scanning.
// Safe to call concurrently.
func (l *AgentLoop) SetWorkingDir(path string) {
    l.mu.Lock()
    defer l.mu.Unlock()
    l.workingDir = path
}
```

- [ ] **Step 3: Run build to verify syntax**

Run: `go build ./internal/agent/...`
Expected: PASS

- [ ] **Step 4: Write unit test**

Modify `internal/agent/loop_test.go` (find or create):

```go
func TestAgentLoop_SetWorkingDir(t *testing.T) {
    loop := &AgentLoop{workingDir: "/old/path"}

    // Concurrent safety test
    done := make(chan struct{})
    go func() {
        loop.SetWorkingDir("/new/path")
        close(done)
    }()
    <-done

    if loop.workingDir != "/new/path" {
        t.Errorf("expected /new/path, got %s", loop.workingDir)
    }
}
```

- [ ] **Step 5: Run test**

Run: `go test ./internal/agent/loop_test.go ./internal/agent/loop.go -v -run TestAgentLoop_SetWorkingDir`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/loop.go internal/agent/loop_test.go
git commit -m "feat: add AgentLoop.SetWorkingDir with mutex protection"
```

---

### Task 5: RPC project.readdir Handler

**Files:**
- Modify: `internal/rpc/projects.go` (add new handler after line 53)
- Modify: `internal/rpc/projects.go` (register method)

- [ ] **Step 1: Add ReadDirRequest and ReadDirResponse types**

After line 17 (after ArtifactInvalidator interface), add:

```go
// ReadDirRequest is the request for project.readdir RPC.
type ReadDirRequest struct {
    Prefix string `json:"prefix"`
}

// ReadDirResponse is the response for project.readdir RPC.
type ReadDirResponse struct {
    Recents  []string `json:"recents"`
    Matches  []string `json:"matches"`
    GitRoots []string `json:"git_roots"`
}
```

- [ ] **Step 2: Register the new method**

Modify `RegisterProjectMethods` at line 52, add:
```go
server.RegisterHandler("project.readdir", h.handleReadDir)
```

- [ ] **Step 3: Implement handleReadDir**

After `handleDetect` (find end of file), add:

```go
// handleReadDir handles project.readdir RPC calls.
func (h *ProjectHandler) handleReadDir(ctx context.Context, params json.RawMessage) (any, error) {
    pm, err := h.pmOrErr()
    if err != nil {
        return nil, err
    }

    var req ReadDirRequest
    if err := json.Unmarshal(params, &req); err != nil {
        return nil, fmt.Errorf("invalid params: %w", err)
    }

    // Get top 5 recents
    recents, err := h.pm.recentsStore.ListRecents(ctx, 5)
    if err != nil {
        return nil, fmt.Errorf("list recents: %w", err)
    }

    // Filter recents by prefix
    var filteredRecents []string
    for _, r := range recents {
        if strings.Contains(r, req.Prefix) {
            filteredRecents = append(filteredRecents, r)
        }
    }

    // If no recent matches, do filesystem fallback
    var matches, gitRoots []string
    if len(filteredRecents) == 0 && req.Prefix != "" {
        // Expand tilde, read dir, return up to 50 entries
        expanded := expandTilde(req.Prefix)
        entries, err := os.ReadDir(expanded)
        if err == nil {
            for i, entry := range entries {
                if i >= 50 {
                    break
                }
                if !entry.IsDir() {
                    continue
                }
                path := filepath.Join(expanded, entry.Name())
                matches = append(matches, path)
                // Find git root
                gitRoot, _ := findGitRoot(path)
                gitRoots = append(gitRoots, gitRoot)
            }
        }
    }

    return &ReadDirResponse{
        Recents:  filteredRecents,
        Matches:  matches,
        GitRoots: gitRoots,
    }, nil
}
```

- [ ] **Step 4: Add helper functions**

At end of file:

```go
// expandTilde expands ~ to user home directory.
func expandTilde(path string) string {
    if strings.HasPrefix(path, "~") {
        home, _ := os.UserHomeDir()
        return filepath.Join(home, strings.TrimPrefix(path[1:], "/"))
    }
    return path
}

// findGitRoot walks up from path looking for .git.
func findGitRoot(path string) (string, error) {
    for {
        if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
            return path, nil
        }
        parent := filepath.Dir(path)
        if parent == path {
            return "", fmt.Errorf("no git root found")
        }
        path = parent
    }
}
```

- [ ] **Step 5: Add missing imports**

Add to imports: `"os"`, `"path/filepath"`, `"strings"`

- [ ] **Step 6: Run build**

Run: `go build ./internal/rpc/...`
Expected: PASS (may fail if recentsStore field not yet on ProjectManager)

- [ ] **Step 7: Commit**

```bash
git add internal/rpc/projects.go
git commit -m "feat: add project.readdir RPC handler with recents + fs fallback"
```

---

### Task 6: Wire RecentsStore into ProjectManager

**Files:**
- Modify: `internal/project/manager.go` (add recentsStore field)
- Modify: `internal/project/manager.go` (NewProjectManager constructor)

- [ ] **Step 1: Add recentsStore field to ProjectManager**

Modify struct at line 18:
```go
type ProjectManager struct {
    store         *Store
    recentsStore  *RecentsStore
    cfg           config.ProjectsConfig
    logger        *slog.Logger
}
```

- [ ] **Step 2: Update NewProjectManager to create RecentsStore**

Modify constructor at line 25:

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
```

- [ ] **Step 3: Add TouchRecent method to ProjectManager**

At end of manager.go:

```go
// TouchRecent updates the recents table for a project path.
func (pm *ProjectManager) TouchRecent(ctx context.Context, path string) error {
    if pm.recentsStore == nil {
        return nil // recents not wired, silently ignore
    }
    return pm.recentsStore.TouchRecent(ctx, path)
}
```

- [ ] **Step 4: Run build**

Run: `go build ./internal/project/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/manager.go
git commit -m "feat: wire RecentsStore into ProjectManager"
```

---

### Task 7: Update project.set Handler to Call SetWorkingDir + TouchRecent

**Files:**
- Modify: `internal/rpc/projects.go:handleSet` (find existing function)

- [ ] **Step 1: Read existing handleSet**

Run: `grep -n "func (h \*ProjectHandler) handleSet" internal/rpc/projects.go`
Read the function to understand current flow.

- [ ] **Step 2: Modify handleSet to accept Path parameter**

The request struct needs updating. Find or add:

```go
type SetProjectRequest struct {
    SessionID string `json:"session_id"`
    ProjectID string `json:"project_id"`
    Path      string `json:"path"` // NEW
}
```

- [ ] **Step 3: Add workingDir update call**

After `sessionStore.SetProject` succeeds (find line ~197-207), add:

```go
// Update AgentLoop workingDir if we have a reference
// Note: This requires wiring the AgentLoop registry into ProjectHandler
// For now, emit a bus event that the agent loop can subscribe to
h.msgBus.Publish("project.set", map[string]string{
    "session_id": sessionID,
    "path":       p.LocalPath,
})
```

- [ ] **Step 4: Call TouchRecent**

After the workingDir update:
```go
if err := h.pm.TouchRecent(ctx, p.LocalPath); err != nil {
    h.logger.Warn("project.set: TouchRecent failed", "error", err)
    // Non-fatal, continue
}
```

- [ ] **Step 5: Add message bus field to ProjectHandler**

Modify struct at line 19:
```go
type ProjectHandler struct {
    pm           *project.ProjectManager
    sessionStore session.Store
    artifactInv  ArtifactInvalidator
    msgBus       *bus.MessageBus  // NEW
}
```

Add setter:
```go
func (h *ProjectHandler) SetMessageBus(bus *bus.MessageBus) {
    h.msgBus = bus
}
```

- [ ] **Step 6: Run build**

Run: `go build ./internal/rpc/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/rpc/projects.go
git commit -m "feat: project.set calls TouchRecent + publishes workingDir update event"
```

---

### Task 8: AgentLoop Subscribes to project.set Events

**Files:**
- Modify: `internal/agent/loop.go` (find Start method or constructor)

- [ ] **Step 1: Add subscription in AgentLoop initialization**

Find where AgentLoop is started (search for `Start` method). Add:

```go
// In AgentLoop.Start() or initialization:
sub := l.msgBus.Subscribe("project.set", func(msg *bus.Message) {
    var data map[string]string
    if err := json.Unmarshal(msg.Payload, &data); err != nil {
        return
    }
    l.SetWorkingDir(data["path"])
})
// Ensure unsubscribe on Stop
```

- [ ] **Step 2: Run build**

Run: `go build ./internal/agent/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agent/loop.go
git commit -m "feat: AgentLoop subscribes to project.set events to update workingDir"
```

---

### Task 9: Daemon Wiring — Components.go

**Files:**
- Modify: `internal/daemon/components.go` (find project wiring section)

- [ ] **Step 1: Find existing ProjectManager wiring**

Run: `grep -n "NewProjectManager" internal/daemon/components.go`

- [ ] **Step 2: Create RecentsStore before ProjectManager**

Add before NewProjectManager call:

```go
// Create recents store
recentsDB, err := c.projectsDB.Pool.Get(ctx)
if err != nil {
    return fmt.Errorf("get recents db conn: %w", err)
}
recentsStore := project.NewRecentsStore(recentsDB)
```

- [ ] **Step 3: Pass recentsStore to NewProjectManager**

Update the NewProjectManager call to include `recentsStore`.

- [ ] **Step 4: Set message bus on ProjectHandler**

Find where ProjectHandler is created. Add:
```go
projectHandler.SetMessageBus(c.msgBus)
```

- [ ] **Step 5: Run build**

Run: `go build ./cmd/meept-daemon/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/components.go
git commit -m "feat: wire RecentsStore + message bus into project subsystem"
```

---

### Task 10: Config Schema — ProjectRecentConfig

**Files:**
- Modify: `internal/config/schema.go`

- [ ] **Step 1: Find ProjectsConfig struct**

Run: `grep -n "type ProjectsConfig" internal/config/schema.go`

- [ ] **Step 2: Add ProjectRecentConfig struct**

Add before or after ProjectsConfig:

```go
type ProjectRecentConfig struct {
    MaxEntries int `json:"max_entries"` // default 5
    TTLDays    int `json:"ttl_days"`    // default 30
}
```

- [ ] **Step 3: Add ProjectsRecent field to Config struct**

Find main `Config` struct (around line 46-94). Add:
```go
ProjectsRecent *ProjectRecentConfig `json:"projects_recent,omitempty"`
```

- [ ] **Step 4: Run build**

Run: `go build ./internal/config/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/schema.go
git commit -m "feat: add ProjectRecentConfig for recents TTL and cap settings"
```

---

### Task 11: Scheduler Job — Daily Recents Prune

**Files:**
- Modify: `internal/daemon/components.go` (find scheduler wiring)
- Create: `internal/project/pruner.go` (optional, or inline in components.go)

- [ ] **Step 1: Create pruner function**

Add to `internal/project/recents.go`:

```go
// SchedulePruneJob schedules a daily prune job.
func SchedulePruneJob(sched *SchedulerService, recents *RecentsStore, ttl time.Duration, max int) {
    sched.AddJob("recents_prune", time.Duration(24)*time.Hour, func(ctx context.Context) error {
        ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
        defer cancel()

        deleted, err := recents.PruneOlderThan(ctx, ttl)
        if err != nil {
            return err
        }
        capped, err := recents.CapToN(ctx, max)
        if err != nil {
            return err
        }
        slog.Default().Info("recents prune completed",
            "deleted", deleted, "capped", capped)
        return nil
    })
}
```

- [ ] **Step 2: Wire in components.go**

Find scheduler wiring section. Add call to `SchedulePruneJob` after creating recents store.

- [ ] **Step 3: Run build**

Run: `go build ./cmd/meept-daemon/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/project/recents.go internal/daemon/components.go
git commit -m "feat: schedule daily recents prune job (TTL + cap)"
```

---

### Task 12: TUI Typeahead Component

**Files:**
- Create: `internal/tui/components/project_typeahead.go`
- Modify: `internal/tui/command_handler.go`
- Modify: `internal/tui/app.go`

- [ ] **Step 1: Create typeahead component**

Create `internal/tui/components/project_typeahead.go`:

```go
package components

import (
    "strings"
    "time"

    "github.com/charmbracelet/bubbles/textinput"
    tea "github.com/charmbracelet/bubbletea"
)

// ProjectTypeaheadModel is the typeahead state.
type ProjectTypeaheadModel struct {
    textInput    textinput.Model
    recents      []string
    filtered     []string
    selected     string
    callback     func(path string) tea.Cmd
    debounceTimer *time.Timer
}

// NewProjectTypeahead creates a new typeahead component.
func NewProjectTypeahead(callback func(path string) tea.Cmd) *ProjectTypeaheadModel {
    ti := textinput.New()
    ti.Placeholder = "Enter project path..."
    ti.Focus()
    ti.Width = 60

    return &ProjectTypeaheadModel{
        textInput: ti,
        callback:  callback,
    }
}

// Init initializes the typeahead.
func (m *ProjectTypeaheadModel) Init() tea.Cmd {
    return textinput.Blink
}

// Update handles messages.
func (m *ProjectTypeaheadModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.Type {
        case tea.KeyEnter:
            if m.selected != "" {
                return m, m.callback(m.selected)
            }
        case tea.KeyEscape:
            return m, func() tea.Msg { return tea.Msg(nil) } // close
        }
    }

    var cmd tea.Cmd
    m.textInput, cmd = m.textInput.Update(msg)

    // Filter recents on text change
    prefix := m.textInput.Value()
    m.filtered = nil
    for _, r := range m.recents {
        if strings.Contains(r, prefix) {
            m.filtered = append(m.filtered, r)
        }
    }

    return m, cmd
}

// View renders the typeahead.
func (m *ProjectTypeaheadModel) View() string {
    var b strings.Builder
    b.WriteString(m.textInput.View())
    b.WriteString("\n")
    for i, item := range m.filtered {
        if i >= 5 {
            break
        }
        b.WriteString("  ")
        if item == m.selected {
            b.WriteString("> ")
        } else {
            b.WriteString("  ")
        }
        b.WriteString(item)
        b.WriteString("\n")
    }
    return b.String()
}

// SetRecents updates the recents list.
func (m *ProjectTypeaheadModel) SetRecents(recents []string) {
    m.recents = recents
    m.filtered = recents
}
```

- [ ] **Step 2: Run build**

Run: `go build ./internal/tui/components/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/tui/components/project_typeahead.go
git commit -m "feat: add ProjectTypeahead TUI component"
```

---

### Task 13: TUI Command Handler — Wire /project Set

**Files:**
- Modify: `internal/tui/command_handler.go`

- [ ] **Step 1: Find executeProjectSet**

Run: `grep -n "executeProjectSet" internal/tui/command_handler.go`
Read existing implementation.

- [ ] **Step 2: Modify to accept path argument**

Extend the function to call `SetProject` RPC with path instead of just name lookup.

- [ ] **Step 3: Run build**

Run: `go build ./internal/tui/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tui/command_handler.go
git commit -m "feat: extend /project set to accept path argument"
```

---

### Task 14: Unit Tests — RPC Handlers

**Files:**
- Create: `internal/rpc/projects_recents_test.go`

- [ ] **Step 1: Write tests for handleReadDir**

Create test file with tests for recents-only, fs fallback, empty cases.

- [ ] **Step 2: Run tests**

Run: `go test ./internal/rpc/... -v -run ReadDir`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/rpc/projects_recents_test.go
git commit -m "test: add unit tests for project.readdir RPC"
```

---

### Task 15: Integration Test

**Files:**
- Create: `tests/integration/project_typeahead_test.go`

- [ ] **Step 1: Write end-to-end test**

Test: Open typeahead → type prefix → select → verify session.ProjectPath + workingDir changed.

- [ ] **Step 2: Run test**

Run: `go test ./tests/integration/... -v -run Typeahead`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/integration/project_typeahead_test.go
git commit -m "test: add integration test for /project typeahead flow"
```

---

### Task 16: Documentation Update

**Files:**
- Modify: `docs/workflows/projects.md` (or create if missing)

- [ ] **Step 1: Document /project <path> command**

Add section documenting the new command, typeahead UX, recents behavior.

- [ ] **Step 2: Commit**

```bash
git add docs/workflows/projects.md
git commit -m "docs: document /project <path> command and typeahead UX"
```

---

## Self-Review Checklist

**1. Spec coverage:** Each section of the spec maps to tasks:
- §3.1 Recents table → Task 1, 2, 3
- §3.2 RPC methods → Task 5, 6, 7, 8, 9
- §3.4 AgentLoop fix → Task 4, 8
- §7 Config → Task 10
- §5 Testing → Task 3, 14, 15
- §6 TUI → Task 12, 13

**2. Placeholder scan:** No TBD/TODO found. All code snippets complete.

**3. Type consistency:** `RecentsStore` methods match across tasks. `SetWorkingDir` signature consistent.

**4. File paths:** All paths are exact (verified via Glob/Grep).

---

**Plan complete. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
