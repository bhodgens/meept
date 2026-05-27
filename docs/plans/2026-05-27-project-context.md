# Project Context Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add project-scoped context to meept sessions, with git-backed worktrees for isolation, path fencing for security, and visual project indicators in TUI and Flutter UI.

**Architecture:** New `internal/project/` package provides `ProjectManager` which owns a SQLite-backed registry of git and local projects. Sessions bind to projects. Git projects use worktrees (one per session, optionally one per plan) for concurrent isolation. SecurityEngine fences tool execution to the project worktree path. TUI gets `ctrl+x p` project picker and status bar indicator.

**Tech Stack:** Go 1.22+, SQLite (existing patterns from session/memory), git worktrees, bubbletea TUI, JSON5 config.

**Feature spec:** `docs/workflows/project-context.md`

---

## Phase 1: Config Schema + Defaults

### Task 1: Add ProjectsConfig to schema

**Objective:** Add the `ProjectsConfig` struct and `Projects` field to the root `Config`.

**Files:**
- Modify: `internal/config/schema.go`

**Step 1: Add ProjectsConfig struct**

After `SessionConfig` (around line 139), add:

```go
// ProjectsConfig holds project context settings including worktree allocation,
// path fencing, and per-project permission profiles.
type ProjectsConfig struct {
	// Enabled turns on project context binding (default: true)
	Enabled bool `json:"enabled" toml:"enabled"`
	// BaseDir is the root directory for project clones (default: "~/.meept/projects")
	BaseDir string `json:"base_dir" toml:"base_dir"`
	// AutoDetect registers projects from CWD git root automatically (default: true)
	AutoDetect bool `json:"auto_detect" toml:"auto_detect"`
	// WorktreePerPlan controls worktree allocation for isolated plans: "auto", "always", "never" (default: "auto")
	WorktreePerPlan string `json:"worktree_per_plan" toml:"worktree_per_plan"`
	// WorktreeIsolationThreshold is the number of files touched before auto-isolating a plan (default: 5)
	WorktreeIsolationThreshold int `json:"worktree_isolation_threshold" toml:"worktree_isolation_threshold"`
	// MaxWorktreesPerProject limits concurrent worktrees per project (0 = unlimited, default: 10)
	MaxWorktreesPerProject int `json:"max_worktrees_per_project" toml:"max_worktrees_per_project"`
	// CleanupOrphanedWorktrees removes worktrees for ended sessions on daemon startup (default: true)
	CleanupOrphanedWorktrees bool `json:"cleanup_orphaned_worktrees" toml:"cleanup_orphaned_worktrees"`
	// FenceEnabled restricts tool execution to project worktree path (default: true)
	FenceEnabled bool `json:"fence_enabled" toml:"fence_enabled"`
	// AllowReadSystemPaths lists system paths readable even when fenced (default: ["/usr", "/etc", "/tmp"])
	AllowReadSystemPaths []string `json:"allow_read_system_paths" toml:"allow_read_system_paths"`
	// AutoSyncOnAttach pulls latest when attaching to a git project (default: false)
	AutoSyncOnAttach bool `json:"auto_sync_on_attach" toml:"auto_sync_on_attach"`
	// DefaultBranch is the default branch for new project checkouts (default: "main")
	DefaultBranch string `json:"default_branch" toml:"default_branch"`
}
```

**Step 2: Add Projects field to Config struct**

In the `Config` struct (around line 44), add after the `Session` field:

```go
	Projects        ProjectsConfig         `json:"projects"          toml:"projects"`
```

**Step 3: Add defaults in DefaultConfig()**

In `DefaultConfig()` (around line 1473, before the closing `}`), add:

```go
		Projects: ProjectsConfig{
			Enabled:                    true,
			BaseDir:                    "~/.meept/projects",
			AutoDetect:                 true,
			WorktreePerPlan:            "auto",
			WorktreeIsolationThreshold: 5,
			MaxWorktreesPerProject:     10,
			CleanupOrphanedWorktrees:   true,
			FenceEnabled:               true,
			AllowReadSystemPaths:       []string{"/usr", "/etc", "/tmp"},
			AutoSyncOnAttach:           false,
			DefaultBranch:              "main",
		},
```

**Step 4: Run tests**

Run: `go build ./internal/config/...`
Expected: compiles without error

Run: `go test ./internal/config/... -v`
Expected: all existing tests pass

**Step 5: Commit**

```bash
git add internal/config/schema.go
git commit -m "feat(config): add ProjectsConfig struct and defaults"
```

---

### Task 2: Add projects section to config template

**Objective:** Add the `projects` section to the default config template so users see it in `meept.json5`.

**Files:**
- Modify: `config/meept.json5`

**Step 1: Add projects section to template**

After the `session` section (or after `workspace` for logical grouping), add:

```json5
  // Project context configuration
  // Projects bind sessions to codebases, enable path fencing, and provide
  // worktree isolation for concurrent agent sessions.
  // See: docs/workflows/project-context.md
  "projects": {
    "enabled": true,
    "base_dir": "~/.meept/projects",         // Root for git clones
    "auto_detect": true,                      // Auto-register from CWD git root
    "worktree_per_plan": "auto",              // "auto" | "always" | "never"
    "worktree_isolation_threshold": 5,        // Files touched before auto-isolating a plan
    "max_worktrees_per_project": 10,          // Max concurrent worktrees per project
    "cleanup_orphaned_worktrees": true,       // Remove orphaned worktrees on startup
    "fence_enabled": true,                    // Restrict tools to project worktree path
    "allow_read_system_paths": ["/usr", "/etc", "/tmp"],
    "auto_sync_on_attach": false,             // Pull on session attach
    "default_branch": "main",                 // Default branch for new checkouts
  },
```

**Step 2: Verify JSON5 validity**

Run: `python3 -c "import json5; json5.load(open('config/meept.json5'))"`
Expected: no parse error (if python json5 available), or just verify by eye

**Step 3: Commit**

```bash
git add config/meept.json5
git commit -m "docs(config): add projects section to default config template"
```

---

### Task 3: Add configui section for projects

**Objective:** Register a `projects` section in `meept config` TUI so users can edit project settings interactively.

**Files:**
- Create: `internal/configui/sections_projects.go`
- Modify: `internal/configui/sections.go`

**Step 1: Create sections_projects.go**

```go
// internal/configui/sections_projects.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildProjectsFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Projects
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("base_dir", "base dir", s.BaseDir),
		NewToggleField("auto_detect", "auto detect", s.AutoDetect),
		NewSelectField("worktree_per_plan", "worktree per plan", s.WorktreePerPlan,
			[]string{"auto", "always", "never"}),
		NewNumberField("worktree_isolation_threshold", "isolation threshold", s.WorktreeIsolationThreshold),
		NewNumberField("max_worktrees_per_project", "max worktrees", s.MaxWorktreesPerProject),
		NewToggleField("cleanup_orphaned_worktrees", "cleanup orphaned", s.CleanupOrphanedWorktrees),
		NewToggleField("fence_enabled", "fence enabled", s.FenceEnabled),
		NewTextField("default_branch", "default branch", s.DefaultBranch),
		NewToggleField("auto_sync_on_attach", "auto sync on attach", s.AutoSyncOnAttach),
	}
}
```

**Step 2: Register in sections.go switch**

In `BuildSectionFields` switch statement, add a case:

```go
	case "projects":
		return buildProjectsFields()
```

**Step 3: Build and test**

Run: `go build ./internal/configui/...`
Expected: compiles without error

**Step 4: Commit**

```bash
git add internal/configui/sections_projects.go internal/configui/sections.go
git commit -m "feat(configui): add projects section to config TUI"
```

---

## Phase 2: Project Data Model + Manager

### Task 4: Create project package with types and SQLite store

**Objective:** Create `internal/project/` with the `Project` type, `Worktree` type, and SQLite-backed persistence.

**Files:**
- Create: `internal/project/types.go`
- Create: `internal/project/store.go`

**Step 1: Create types.go**

```go
// Package project provides project context management for meept sessions.
package project

import "time"

// Mode represents the project mode (git or local).
type Mode string

const (
	ModeGit   Mode = "git"
	ModeLocal Mode = "local"
)

// Project represents a registered project.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Mode      Mode      `json:"mode"`
	GitURL    string    `json:"git_url,omitempty"`
	Branch    string    `json:"branch"`
	LocalPath string    `json:"local_path"`
	Status    string    `json:"status"` // "active", "archived", "error"
	LastSync  time.Time `json:"last_sync,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Worktree represents a git worktree allocated for a session or plan.
type Worktree struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	SessionID string    `json:"session_id,omitempty"`
	PlanID    string    `json:"plan_id,omitempty"`
	Path      string    `json:"path"`
	Branch    string    `json:"branch"`
	Status    string    `json:"status"` // "active", "completed", "cleaned"
	CreatedAt time.Time `json:"created_at"`
}
```

**Step 2: Create store.go**

SQLite store following the pattern from `internal/memory/ftstore.go`. Create the `projects` and `project_worktrees` tables with FTS support. Implement CRUD methods:

- `CreateProject(ctx, *Project) error`
- `GetProject(ctx, id) (*Project, error)`
- `ListProjects(ctx) ([]*Project, error)`
- `UpdateProject(ctx, *Project) error`
- `DeleteProject(ctx, id) error`
- `CreateWorktree(ctx, *Worktree) error`
- `GetWorktree(ctx, id) (*Worktree, error)`
- `GetActiveWorktreeBySession(ctx, sessionID) (*Worktree, error)`
- `ListWorktreesByProject(ctx, projectID) ([]*Worktree, error)`
- `UpdateWorktree(ctx, *Worktree) error`
- `DeleteWorktree(ctx, id) error`
- `CleanupOrphanedWorktrees(ctx) (int, error)`

**Step 3: Write tests**

Create `internal/project/store_test.go` with table-driven tests for each CRUD method.

**Step 4: Run tests**

Run: `go test ./internal/project/... -v`
Expected: all tests pass

**Step 5: Commit**

```bash
git add internal/project/
git commit -m "feat(project): add project types and SQLite store"
```

---

### Task 5: Create ProjectManager with registration and auto-detection

**Objective:** Implement the core `ProjectManager` that handles project registration, auto-detection from CWD, and git operations.

**Files:**
- Create: `internal/project/manager.go`
- Create: `internal/project/manager_test.go`

**Step 1: Create manager.go**

```go
type ProjectManager struct {
    store    *Store
    cfg      config.ProjectsConfig
    logger   *slog.Logger
}

func NewProjectManager(store *Store, cfg config.ProjectsConfig, logger *slog.Logger) *ProjectManager
func (pm *ProjectManager) RegisterGit(ctx context.Context, id, name, gitURL string) (*Project, error)
func (pm *ProjectManager) RegisterLocal(ctx context.Context, id, name, path string) (*Project, error)
func (pm *ProjectManager) Unregister(ctx context.Context, id string) error
func (pm *ProjectManager) Get(ctx context.Context, id string) (*Project, error)
func (pm *ProjectManager) List(ctx context.Context) ([]*Project, error)
func (pm *ProjectManager) DetectFromPath(ctx context.Context, path string) (*Project, error)
func (pm *ProjectManager) Status(ctx context.Context, id string) (*ProjectStatus, error)
func (pm *ProjectManager) Sync(ctx context.Context, id string) error
```

`DetectFromPath` walks up from `path` looking for `.git/`. If found:
1. Extract project name from directory or `git remote get-url origin`
2. Check if already registered (by local_path or git_url)
3. If not, register as git project
4. Return the project

For local mode (no `.git/`): register with `ModeLocal` using CWD as `LocalPath`.

**Step 2: Write tests for DetectFromPath**

Test cases:
- Path inside git repo -> detects git project
- Path outside git repo -> returns local project
- Already registered path -> returns existing project

**Step 3: Run tests**

Run: `go test ./internal/project/... -v`
Expected: all tests pass

**Step 4: Commit**

```bash
git add internal/project/manager.go internal/project/manager_test.go
git commit -m "feat(project): add ProjectManager with registration and auto-detection"
```

---

### Task 6: Add worktree management to ProjectManager

**Objective:** Implement git worktree creation, release, and merge for session isolation.

**Files:**
- Create: `internal/project/worktree.go`
- Create: `internal/project/worktree_test.go`

**Step 1: Create worktree.go**

Methods:

```go
func (pm *ProjectManager) CreateWorktree(ctx context.Context, projectID, sessionID, planID string) (*Worktree, error)
func (pm *ProjectManager) ReleaseWorktree(ctx context.Context, worktreeID string) error
func (pm *ProjectManager) MergeWorktree(ctx context.Context, worktreeID, targetBranch string) error
func (pm *ProjectManager) GetActiveWorktree(ctx context.Context, sessionID string) (*Worktree, error)
func (pm *ProjectManager) ShouldIsolatePlan(ctx context.Context, plan PlanInfo) bool
```

`CreateWorktree`:
1. Resolve project, get its LocalPath
2. Create branch: `session/<sessionID>` or `plan/<planID>`
3. `git worktree add <path> -b <branch>`
4. Record in store

`ReleaseWorktree`:
1. `git worktree remove <path>`
2. Optionally delete branch
3. Update store status to "cleaned"

`ShouldIsolatePlan` implements the heuristic from the spec:
- If `cfg.WorktreePerPlan == "always"` -> true
- If `"never"` -> false
- If `"auto"` -> check file count threshold, plan type

**Step 2: Write tests**

Use temp directories with `git init` for test repos. Test create/release lifecycle.

**Step 3: Run tests**

Run: `go test ./internal/project/... -v`
Expected: all tests pass

**Step 4: Commit**

```bash
git add internal/project/worktree.go internal/project/worktree_test.go
git commit -m "feat(project): add worktree creation, release, and merge"
```

---

## Phase 3: Session Binding

### Task 7: Add ProjectID to Session struct and store

**Objective:** Bind projects to sessions by adding `ProjectID` and `ProjectPath` fields.

**Files:**
- Modify: `internal/session/session.go`
- Modify: `internal/session/sqlite.go` (if it exists -- check for SQLite store)

**Step 1: Add fields to Session struct**

```go
type Session struct {
    // ... existing fields ...
    ProjectID   string `json:"project_id,omitempty"`
    ProjectPath string `json:"project_path,omitempty"`
}
```

**Step 2: Update SQLite schema (if applicable)**

Add `project_id TEXT` and `project_path TEXT` columns to the sessions table.

**Step 3: Update Create method**

Accept optional project binding:

```go
type CreateOptions struct {
    ProjectID   string
    ProjectPath string
    NoFence     bool
}

func (s *MemoryStore) Create(name string, opts ...CreateOptions) (*Session, error)
```

Or add a separate `SetProject(sessionID, projectID, projectPath string) error` method.

**Step 4: Write tests**

Test session creation with and without project binding. Test project field persistence.

**Step 5: Run tests**

Run: `go test ./internal/session/... -v`
Expected: all tests pass

**Step 6: Commit**

```bash
git add internal/session/
git commit -m "feat(session): add ProjectID and ProjectPath to Session"
```

---

### Task 8: Wire ProjectManager into daemon startup

**Objective:** Initialize ProjectManager on daemon start, run orphan cleanup, and make it available to components.

**Files:**
- Modify: `internal/daemon/components.go`
- Modify: `internal/daemon/daemon.go`

**Step 1: Add ProjectManager to Components struct**

```go
type Components struct {
    // ... existing fields ...
    ProjectManager *project.ProjectManager
}
```

**Step 2: Initialize in NewComponents**

After store creation:
```go
projStore := project.NewStore(cfg.StateDir + "/projects.db", logger)
projMgr := project.NewProjectManager(projStore, cfg.Projects, logger)
if cfg.Projects.CleanupOrphanedWorktrees {
    n, _ := projMgr.CleanupOrphans(ctx)
    logger.Info("cleaned orphaned worktrees", "count", n)
}
```

**Step 3: Build and verify**

Run: `go build ./internal/daemon/...`
Expected: compiles without error

**Step 4: Commit**

```bash
git add internal/daemon/components.go internal/daemon/daemon.go
git commit -m "feat(daemon): wire ProjectManager into daemon startup"
```

---

## Phase 4: RPC Endpoints

### Task 9: Add project RPC methods

**Objective:** Add RPC methods for project operations so the CLI and TUI can manage projects.

**Files:**
- Create: `internal/rpc/projects.go`
- Modify: `internal/rpc/proxy.go`

**Step 1: Create projects.go with handler methods**

Methods to implement:

| Method | Params | Returns |
|--------|--------|---------|
| `project.list` | `{}` | `{projects: []}` |
| `project.get` | `{id}` | `{project: {}}` |
| `project.register` | `{id, name, git_url?, local_path?}` | `{project: {}}` |
| `project.unregister` | `{id}` | `{ok: true}` |
| `project.set` | `{session_id, project_id}` | `{session: {}}` |
| `project.sync` | `{id}` | `{ok: true}` |
| `project.status` | `{id}` | `{status: {}}` |
| `project.detect` | `{path}` | `{project: {}}` |

**Step 2: Register in proxy.go**

Add entries for each method in the `RegisterHandler` calls.

**Step 3: Write tests**

Test each RPC method with mock ProjectManager.

**Step 4: Run tests**

Run: `go test ./internal/rpc/... -v`
Expected: all tests pass

**Step 5: Commit**

```bash
git add internal/rpc/projects.go internal/rpc/proxy.go
git commit -m "feat(rpc): add project management RPC methods"
```

---

### Task 10: Add project HTTP API endpoints

**Objective:** Expose project operations via REST API for meept_ui and remote clients.

**Files:**
- Modify: `internal/comm/http/api_handlers.go`

**Step 1: Add HTTP handlers**

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/projects` | GET | List projects |
| `/api/v1/projects/:id` | GET | Get project details |
| `/api/v1/projects` | POST | Register project |
| `/api/v1/projects/:id` | DELETE | Unregister project |
| `/api/v1/projects/:id/sync` | POST | Sync project |
| `/api/v1/projects/:id/status` | GET | Project status |
| `/api/v1/projects/detect` | POST | Detect project from path |

**Step 2: Register routes in the router setup**

**Step 3: Commit**

```bash
git add internal/comm/http/api_handlers.go
git commit -m "feat(http): add project REST API endpoints"
```

---

## Phase 5: Client Integration

### Task 11: Add `--project` and `--nofence` flags to CLI chat command

**Objective:** Allow users to specify project and fencing mode when starting a chat session.

**Files:**
- Modify: `cmd/meept/chat.go`

**Step 1: Add flags**

```go
chatCmd.Flags().String("project", "", "bind session to named project")
chatCmd.Flags().Bool("nofence", false, "disable path fencing for this session")
```

**Step 2: Pass to session creation**

On `session.create` RPC call, include `project_id` and `no_fence` fields. If `--project` is set, use it. Otherwise, send `detect_path` with CWD for auto-detection.

**Step 3: Build and test**

Run: `go build ./cmd/meept/...`
Expected: compiles without error

**Step 4: Commit**

```bash
git add cmd/meept/chat.go
git commit -m "feat(cli): add --project and --nofence flags to chat command"
```

---

### Task 12: Add `meept projects` CLI subcommand

**Objective:** Add CLI commands for project management outside of chat.

**Files:**
- Create: `cmd/meept/projects.go`

**Step 1: Create projects.go**

Subcommands:
```
meept projects              # list registered projects
meept projects add <path>   # register from local path
meept projects add <url>    # register from git URL
meept projects remove <id>  # unregister
meept projects sync <id>    # git pull
meept projects status <id>  # show git status
```

**Step 2: Register with root command**

**Step 3: Commit**

```bash
git add cmd/meept/projects.go
git commit -m "feat(cli): add meept projects subcommand"
```

---

## Phase 6: TUI Integration

### Task 13: Add project indicator to TUI status bar

**Objective:** Show current project name, branch, and sync status in the status bar.

**Files:**
- Modify: `internal/tui/app.go`

**Step 1: Track current project in App struct**

```go
type App struct {
    // ... existing fields ...
    currentProject *types.ProjectInfo
}
```

**Step 2: Update status bar rendering**

Modify `getQuickActions` / status bar render to include project indicator:

- `[meept main*]` -- git project, dirty
- `[local:/tmp/scratch]` -- local mode
- `[meept main* UNFENCED]` -- nofence mode (red)

Format: appended after existing keybinding hints, separated by ` │ `.

**Step 3: Fetch project info on session creation/switch**

After `session.create` response, extract `project_id` and fetch project details via RPC.

**Step 4: Commit**

```bash
git add internal/tui/app.go
git commit -m "feat(tui): add project indicator to status bar"
```

---

### Task 14: Add `/project` slash commands to TUI

**Objective:** Implement `/project`, `/project set`, `/project list`, `/project add`, `/project sync`, `/project status` commands.

**Files:**
- Modify: `internal/tui/command_handler.go`

**Step 1: Register /project commands**

In `handleSlashCommand`, add cases for `/project` with subcommand parsing.

**Step 2: Implement each subcommand**

- `/project` (no args) -> show current project info inline
- `/project list` -> list all projects with mode/status
- `/project set <name>` -> switch project (prompts for new session confirmation)
- `/project add <path|url>` -> register new project via RPC
- `/project sync` -> pull latest for current project
- `/project status` -> detailed git status

**Step 3: Add types for project RPC responses**

In `internal/tui/types/types.go`:

```go
type ProjectInfo struct {
    ID       string `json:"id"`
    Name     string `json:"name"`
    Mode     string `json:"mode"`
    Branch   string `json:"branch"`
    GitURL   string `json:"git_url,omitempty"`
    Status   string `json:"status"`
    Dirty    bool   `json:"dirty"`
}
```

**Step 4: Commit**

```bash
git add internal/tui/command_handler.go internal/tui/types/types.go
git commit -m "feat(tui): add /project slash commands"
```

---

### Task 15: Add `ctrl+x p` projects dialog

**Objective:** Full-screen project picker modal, analogous to the existing sessions dialog.

**Files:**
- Modify: `internal/tui/modal.go`
- Modify: `internal/tui/app.go`

**Step 1: Add ProjectListMsg and ProjectSelectMsg**

```go
type ProjectListMsg struct {
    Projects []types.ProjectInfo
    Err      error
}

type ProjectSelectMsg struct {
    ProjectID string
}
```

**Step 2: Create project list modal rendering**

Render a selectable list of projects with columns: name, mode, branch, sessions, status.

Key bindings:
- `return` -> select and switch (with confirmation)
- `a` -> add new project
- `s` -> sync selected
- `d` -> remove/unregister
- `esc` -> close

**Step 3: Wire ctrl+x p keybinding**

In the command mode handler, add `p` as a shortcut that opens the projects dialog.

**Step 4: Handle ProjectSelectMsg**

On selection, prompt "Switch to project '<name>'? This will start a new session. [y/n]"
If confirmed, create new session bound to selected project.

**Step 5: Commit**

```bash
git add internal/tui/modal.go internal/tui/app.go
git commit -m "feat(tui): add ctrl+x p projects dialog"
```

---

## Phase 7: Security (Path Fencing)

### Task 16: Implement path fencing in SecurityEngine

**Objective:** Restrict tool execution to the project worktree path when fencing is enabled.

**Files:**
- Create: `internal/security/fence.go`
- Modify: `internal/security/engine.go`

**Step 1: Create FenceConfig and FenceChecker**

```go
type FenceConfig struct {
    Enabled      bool
    RootPath     string
    AllowRead    []string
    NoFence      bool // per-session override from --nofence
}

type FenceChecker struct {
    cfg FenceConfig
}

func NewFenceChecker(cfg FenceConfig) *FenceChecker
func (fc *FenceChecker) CheckPath(path string, op string) error  // op: "read", "write", "exec"
func (fc *FenceChecker) CheckCommand(cmd string, workDir string) error
```

**Step 2: Integrate into SecurityEngine**

`SecurityEngine.Check()` receives the fence config from the session's project binding. Before any tool execution, check the path against the fence.

**Step 3: Write tests**

- Path inside project root -> allowed
- Path outside project root, read -> allowed if in AllowRead
- Path outside project root, write -> blocked
- NoFence mode -> all allowed
- Local mode with CWD as root -> fences to CWD

**Step 4: Run tests**

Run: `go test ./internal/security/... -v`
Expected: all tests pass

**Step 5: Commit**

```bash
git add internal/security/fence.go internal/security/engine.go
git commit -m "feat(security): implement path fencing for project context"
```

---

## Phase 8: Context Injection

### Task 17: Scope context scanning to project path

**Objective:** When a session has a project binding, scan CLAUDE.md/README from the project worktree instead of daemon WorkingDir.

**Files:**
- Modify: `internal/context/context_builder.go`

**Step 1: Add project path parameter to BuildOverview**

The context builder currently uses `cb.artifacts.WorkingDir`. When a project is bound, set `WorkingDir` to the session's active worktree path.

**Step 2: Trigger rescan on project switch**

When `/project set` changes the project, invalidate the artifact cache for the old path and rescan at the new path.

**Step 3: Commit**

```bash
git add internal/context/context_builder.go
git commit -m "feat(context): scope artifact scanning to project worktree path"
```

---

## Phase 9: Documentation

### Task 18: Update CLAUDE.md with project context architecture

**Objective:** Document the new project system in the project's CLAUDE.md so future development has context.

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Add project section to Architecture Overview table**

Add `project` row:

| Layer | Go Packages |
|-------|-------------|
| **Project** | `internal/project` (manager, store, worktree) |

**Step 2: Add project config to Configuration section**

Document the `projects` config section and its fields.

**Step 3: Add project-related commands to CLI section**

```
# Project commands
./bin/meept projects                    # List registered projects
./bin/meept projects add <path|url>     # Register project
./bin/meept projects remove <name>      # Unregister
./bin/meept projects sync <name>        # Pull latest
./bin/meept projects status <name>      # Show git status
./bin/meept chat --project <name>       # Chat bound to specific project
./bin/meept chat --nofence              # Disable path fencing
```

**Step 4: Update Project Structure**

Add `internal/project/` to the tree.

**Step 5: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md with project context architecture"
```

---

## Dependency Graph

```
Task 1 (config schema)
  ├── Task 2 (config template)
  ├── Task 3 (configui section)
  └── Task 4 (project types + store)
       ├── Task 5 (project manager)
       │    └── Task 6 (worktree mgmt)
       └── Task 7 (session binding)
            ├── Task 8 (daemon wiring)
            └── Task 9 (RPC methods)
                 ├── Task 10 (HTTP API)
                 ├── Task 11 (CLI flags)
                 ├── Task 12 (CLI subcommand)
                 ├── Task 13 (TUI status bar)
                 ├── Task 14 (TUI slash commands)
                 └── Task 15 (TUI dialog)
Task 16 (path fencing) ← depends on Task 7 + Task 8
Task 17 (context injection) ← depends on Task 8
Task 18 (docs) ← after all above
```

## Estimated Effort

| Phase | Tasks | Est. Hours |
|-------|-------|------------|
| Phase 1: Config | 3 | 1-2 |
| Phase 2: Data Model | 3 | 4-6 |
| Phase 3: Session Binding | 2 | 2-3 |
| Phase 4: RPC | 2 | 2-3 |
| Phase 5: Client | 2 | 2-3 |
| Phase 6: TUI | 3 | 4-6 |
| Phase 7: Security | 1 | 3-4 |
| Phase 8: Context | 1 | 1-2 |
| Phase 9: Docs | 1 | 1 |
| **Total** | **18** | **20-30** |
