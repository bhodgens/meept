# Feature: Project Context

> Status: **Draft**
> Last updated: 2026-05-27

## Problem Statement

Meept has no concept of "which project am I working on." The daemon has a single
`WorkingDir` set at startup from `os.Getwd()`. Sessions carry no project binding.
The agent executes tools against whatever path the daemon happens to be running in,
with no security boundary, no visual indicator, and no cross-machine synchronization.

This creates three classes of problem:

1. **Mental context**: The user cannot see which project the agent is operating on.
   Running `meept` from the wrong directory is a silent error.
2. **Security**: There is no filesystem sandboxing. An agent can write anywhere the
   daemon user has access.
3. **Cross-machine coordination**: When the daemon runs on a different host than the
   client, there is no mechanism to synchronize the working tree.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Project model | Git repository (primary) + local/detached fallback | Git is the natural synchronization mechanism for code projects |
| Storage | `~/.meept/projects/<name>/` with git clone + worktrees | Hybrid: daemon owns clones, clients can work on own copies |
| Session binding | One project per session, switchable mid-session | Project context scopes the agent's entire operating environment |
| Concurrency | One worktree per plan, shared by agents within that plan | Natural isolation boundary; avoids 8x repo duplication |
| Security | Path sandboxing to project root by default; `--nofence` opt-out | Defense in depth without blocking power users |
| Cross-machine | Git push/pull as sync; any meept-daemon can clone the same repo | Enables future clustering without bespoke file sync |

## Project Model

### Definition

A **project** is a directory tree that the agent operates within. It is one of:

- **git mode**: A git repository cloned into `~/.meept/projects/<name>/`. The
  daemon manages clone, branch, and worktree operations. Multiple daemon instances
  can clone the same remote and stay synchronized via git push/pull.
- **local mode**: A path on the local filesystem with no git tracking. Used for
  scratch work, one-off tasks, or non-code projects. The agent uses a scratch
  directory under `~/.meept/workspaces/` for isolation.

### Identity

```
Project {
    ID          string    // unique slug (e.g., "meept", "my-app")
    Name        string    // human-readable name
    Mode        string    // "git" | "local"

    // git mode fields
    GitURL      string    // clone URL (remote origin)
    Branch      string    // default branch (default: "main")

    // resolved at runtime per daemon instance
    LocalPath   string    // absolute path to project root on this machine

    // metadata
    Status      string    // "active" | "archived" | "error"
    LastSync    time.Time // last successful git pull (git mode)
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

For git projects, the identity is the GitURL. The ID is a local alias. Two daemon
instances with the same GitURL are working on the same project and can synchronize
via git.

### Auto-detection

When `meept chat` is run, the client auto-detects the project:

1. Walk up from CWD looking for `.git/`.
2. If found: use the git repo root as the project path. Extract project name from
   the directory name or git remote URL.
3. If not found: operate in "local" mode with CWD as the project path.
4. The auto-detected project is registered in the daemon's project registry on
   first use.

The user can override with `--project <name>` or `/project set <name>`.

## Session Binding

### Data Model Change

Add `ProjectID` and `ProjectPath` to the `Session` struct:

```go
type Session struct {
    ID              string
    Name            string
    Description     string
    ConversationID  string
    ProjectID       string    // FK to projects registry
    ProjectPath     string    // resolved local path (derived from project)
    CreatedAt       time.Time
    LastActivity    time.Time
    AttachedClients []string
    WorkerIDs       []string
    LeafMessageID   *int64
}
```

### Behavior

| Action | Behavior |
|--------|----------|
| `meept chat` | Auto-detect project from CWD. Create session bound to that project. |
| `meept chat --project meept` | Explicitly bind to named project. Error if not registered. |
| `meept chat --nofence` | Start with project context but no path sandboxing. |
| `/project` | Show current project name, mode, branch, and sync status. |
| `/project set <name>` | Switch project. Prompts to create a new session (with confirmation). |
| `/project list` | List all registered projects with mode and status indicators. |
| `/project add <path\|url>` | Register a new project. |
| `/project sync` | `git pull` on the project's worktree. |
| `/project status` | Show detailed git status: branch, dirty files, ahead/behind. |

### Switching Projects

Switching projects mid-session creates a new session by default (with user
confirmation). The old session is preserved. This is because:

1. Project context affects security boundaries, injected context (CLAUDE.md),
   and tool execution paths.
2. Switching without a new session would create a confusing audit trail.
3. The session list already provides navigation between sessions.

The confirmation prompt: "Switching to project 'foo' will start a new session.
Continue? [y/n]"

### Project Context Injection

When a project is bound to a session, the daemon injects context from the
project root:

1. **CLAUDE.md** / **AGENTS.md** / **.cursorrules**: Parsed and injected into
   the agent's system prompt (existing `internal/context` behavior, now scoped
   to the project path).
2. **README.md**: First N lines injected for project overview.
3. **.meept/skills/**: Project-local skills loaded from the project root.
4. **Git status**: Current branch, dirty state, recent commits injected as
   metadata.

This replaces the current behavior of scanning `daemon.Config.WorkingDir`.

## Worktree Architecture

### Why Worktrees

Git worktrees allow multiple checkouts of the same repository at different
branches/commits, sharing the `.git` object store. This enables:

1. Multiple sessions working on the same project simultaneously without conflicts.
2. Isolated agent execution without full repo duplication.
3. Clean merge workflows: agents work on feature branches, merge to main when done.

### Directory Layout

```
~/.meept/projects/
  meept/                        # project root = bare-ish repo with main worktree
    .git/
    .meept/                     # project-local meept config
    ...                         # main checkout (user's primary view)
    .git-worktrees/
      session-abc123/           # worktree for session abc123
        ...                     # checkout of feature/session-abc123 branch
      plan-fix-auth/            # worktree for isolated plan
        ...                     # checkout of plan/fix-auth branch
  my-app/                       # another project
    .git/
    ...
```

### Worktree Allocation Strategy

**Per-session worktree (primary)** + **per-plan worktree (for isolated plans)**.

```
Session A (project: meept)
  └─ worktree: .git-worktrees/session-a/  (branch: session/a)
       └─ all agents in this session share this worktree
       └─ agent: dispatcher
       └─ agent: coder
       └─ agent: debugger

Session B (project: meept)  [concurrent]
  └─ worktree: .git-worktrees/session-b/  (branch: session/b)
       └─ independent from session A

Session A, Plan "fix-auth" (isolated)
  └─ worktree: .git-worktrees/plan-fix-auth/  (branch: plan/fix-auth)
       └─ agents assigned to this plan work here
       └─ merged back to session/a when plan completes
```

**Decision point: when does a plan get its own worktree?**

- **Default**: Agents within a session share the session worktree. This is the
  common case for interactive work.
- **Isolated plan**: When the planner creates a plan that involves risky or
  exploratory changes, it can request an isolated worktree. The plan's agents
  work there until the plan completes or is abandoned.

**Recommended heuristic for isolation** (configurable):

| Condition | Isolated? |
|-----------|-----------|
| Plan touches > 5 files | Yes |
| Plan involves experimental/refactoring steps | Yes |
| Plan created by a background/scheduled job | Yes |
| Single-agent interactive task | No |
| Simple read-only analysis | No |

The isolation decision can be overridden per-plan via:
- Agent specifies `isolated: true` in the plan
- User sets `/plan isolate` before planning
- Config default: `projects.worktree_per_plan: "auto"` (use heuristic),
  `"always"`, or `"never"`

### Worktree Lifecycle

```
1. Session created, project bound
   → ProjectManager creates worktree (branch: session/<session-id>)
   → Worktree path stored in session metadata

2. Agent executes tool
   → Tool receives worktree path as CWD
   → All file/shell operations scoped to worktree

3. Plan created (isolated)
   → ProjectManager creates worktree (branch: plan/<plan-id>)
   → Plan's agents redirect to plan worktree

4. Plan completes
   → Changes committed to plan branch
   → Merge plan branch into session branch
   → Worktree cleaned up

5. Session ends
   → Session branch merged to project's default branch (with confirmation)
   → Worktree cleaned up
```

### Cluster Implication (Future)

For clustering, any meept-daemon instance can:

1. `git clone <GitURL>` into `~/.meept/projects/<name>/`
2. Create worktrees for its sessions
3. Push branches to the shared remote
4. Pull branches from other daemon instances

This requires no bespoke sync protocol -- git IS the sync protocol. Future work
will add:

- Automatic push-on-commit for shared branches
- Branch discovery between daemon instances
- Conflict detection and resolution workflows
- Health monitoring of remote connectivity

Tracked in future issue: `meept-clustered-worktree-sync`.

## Security: Path Fencing

### Default Behavior: Fenced Mode

When a project is bound, the agent's tool execution is **fenced** to the project's
worktree path. The `SecurityEngine` rejects any tool call that attempts to:

- Read files outside the project root (with exceptions for `/usr`, system paths)
- Write files outside the project root
- Execute shell commands with paths outside the project root
- Access `~/.meept/` itself (except through approved tools)

```
ProjectManager.GetFence(projectID) → FenceConfig {
    RootPath:    "/home/user/.meept/projects/meept/.git-worktrees/session-abc/",
    AllowRead:   []string{"/usr", "/etc", "/tmp"},
    AllowWrite:  []string{},  // only project root
    AllowExec:   []string{},  // only within project root
}
```

### Exception: `--nofence` Mode

When the client is launched with `meept chat --nofence`, the fencing is disabled
for that session. This is the power-user escape hatch for:

- System administration tasks
- Cross-project operations
- Debugging meept itself

The `--nofence` flag is visible in:

- TUI status bar: `[meept git:main* UNFENCED]` (red indicator)
- Session metadata
- Audit log entries
- Flutter UI: warning banner at top of chat

### Per-project Permission Profiles

Projects can define allowed tools and restrictions:

```json5
// ~/.meept/meept.json5
{
  projects: {
    "production-config": {
      allowed_tools: ["file_read", "shell_execute"],
      require_confirmation: true,
      fence: true,
      // cannot disable fence per-project without --nofence
    },
  },
}
```

## Project Registry

### Storage

SQLite table in the daemon's state database:

```sql
CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    mode        TEXT NOT NULL CHECK(mode IN ('git', 'local')),
    git_url     TEXT,           -- NULL for local mode
    branch      TEXT DEFAULT 'main',
    local_path  TEXT NOT NULL,
    status      TEXT DEFAULT 'active' CHECK(status IN ('active', 'archived', 'error')),
    last_sync   DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS project_worktrees (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    session_id  TEXT,           -- NULL if plan-scoped
    plan_id     TEXT,           -- NULL if session-scoped
    path        TEXT NOT NULL,  -- absolute path to worktree
    branch      TEXT NOT NULL,  -- branch name in worktree
    status      TEXT DEFAULT 'active' CHECK(status IN ('active', 'completed', 'cleaned')),
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### ProjectManager Component

New daemon component: `internal/project/manager.go`

```
ProjectManager {
    // Registration
    RegisterGit(id, name, gitURL) (*Project, error)
    RegisterLocal(id, name, path) (*Project, error)
    Unregister(id) error

    // Lifecycle
    Checkout(projectID) error            // ensure clone exists, pull latest
    Sync(projectID) error                // git pull
    Status(projectID) (*ProjectStatus, error)

    // Worktrees
    CreateWorktree(projectID, sessionID, planID) (*Worktree, error)
    ReleaseWorktree(worktreeID) error
    MergeWorktree(worktreeID, targetBranch) error

    // Queries
    Get(id) (*Project, error)
    List() ([]*Project, error)
    GetActiveWorktree(sessionID) (*Worktree, error)

    // Auto-detection
    DetectFromPath(path) (*Project, error)
}
```

### RPC Endpoints

New RPC methods (and corresponding HTTP API endpoints):

| Method | Description |
|--------|-------------|
| `project.register` | Register a new project (git or local) |
| `project.unregister` | Remove a project from registry |
| `project.list` | List all registered projects |
| `project.get` | Get project details + status |
| `project.set` | Bind project to current session |
| `project.sync` | Pull latest changes |
| `project.status` | Git status, branch, ahead/behind |
| `project.worktree.create` | Create a worktree for session/plan |
| `project.worktree.release` | Release and cleanup a worktree |

## TUI Changes

### Status Bar

The existing status bar (bottom of TUI) gains a project indicator:

```
Before:  ● connected │ mice:on │ ctrl+x 1 chat  2 tasks │ ~/git/meept
After:   ● connected │ mice:on │ ctrl+x 1 chat  2 tasks  p projects │ [meept main*]
```

The project indicator shows:

- `[meept main*]` -- git project, branch "main", dirty (asterisk)
- `[my-app feat/x]` -- git project, on feature branch
- `[local:/tmp/scratch]` -- local/detached mode
- `[meept main* UNFENCED]` -- `--nofence` mode, red styling

### `ctrl+x p` Projects Dialog

Analogous to the existing sessions dialog. Opens a fullscreen modal with:

```
┌─ projects ─────────────────────────────────────────────┐
│                                                         │
│  ● meept              git  main    3 sessions   clean   │
│  ○ my-app             git  feat/x  1 session    dirty   │
│  ○ production-config  git  main    0 sessions   clean   │
│  ○ /tmp/scratch       local  ---   1 session    ---     │
│                                                         │
│  [return] switch to project  [a] add  [d] remove        │
│  [s] sync  [i] project info                             │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

Key bindings:

| Key | Action |
|-----|--------|
| `return` | Switch to selected project (creates new session with confirmation) |
| `a` | Add new project (prompts for path or URL) |
| `d` | Remove/unregister project |
| `s` | Sync selected project (`git pull`) |
| `i` | Show project info (git URL, branches, recent commits) |
| `esc` | Close dialog |

### Slash Commands

| Command | Description |
|---------|-------------|
| `/project` | Show current project status |
| `/project set <name>` | Switch project (new session) |
| `/project list` | List all projects (inline, not modal) |
| `/project add <path\|url>` | Register new project |
| `/project remove <name>` | Unregister project |
| `/project sync` | Pull latest |
| `/project status` | Detailed git status |

### Vim Mode Extension

Add `:project <name>` as a vim command (analogous to existing `:session <name>`).

## Flutter UI (meept_ui) Changes

### Tab Bar: Project as Tab Identity

Instead of a generic "chat" tab, each open session tab is labeled with its
project name:

```
┌──────────────────────────────────────────────────┐
│  [meept*]  │  [my-app]  │  [+]  │               │
├──────────────────────────────────────────────────┤
│                                                   │
│  (active chat for meept project)                  │
│                                                   │
│  Status: main branch, 2 modified files            │
│                                                   │
└──────────────────────────────────────────────────┘
```

- Active tab shows project name with status indicator (asterisk = dirty).
- `+` button opens project picker (list of registered projects).
- Tab tooltip shows full project path and git URL.

### Project Indicator in Chat

Within the chat view, a subtle project status bar shows:

```
┌──────────────────────────────────────────────────┐
│  ◉ meept │ main │ ↑2 ↓0 │ 3 files changed        │
├──────────────────────────────────────────────────┤
```

Components: project name | branch | ahead/behind | dirty count.

### Project Picker

A top-level modal (not sidebar) for switching/adding projects, matching the
existing sessions modal UX.

## CLI Commands

New subcommands and flags:

```
meept projects                    # List registered projects
meept projects add <path|url>     # Register a project
meept projects remove <name>      # Unregister
meept projects sync <name>        # Pull latest
meept projects status <name>      # Show git status

meept chat --project <name>       # Start chat bound to specific project
meept chat --nofence              # Disable path fencing for this session
```

## Implementation Phases

### Phase 1: Core Data Model + Session Binding

**Scope**: Project registry, session-project binding, auto-detection, basic `/project` commands.

Changes:
1. Add `Project` type and `ProjectManager` to `internal/project/`
2. Add `projects` and `project_worktrees` SQLite tables
3. Add `ProjectID` and `ProjectPath` to `session.Session`
4. Auto-detect project from CWD git root on session create
5. Register auto-detected project in registry
6. Implement `/project` slash commands
7. Scope `internal/context` scanning to project path instead of daemon WorkingDir
8. Wire `ProjectManager` into daemon startup

**Files touched**:
- `internal/project/` (new package)
- `internal/session/session.go` (add fields)
- `internal/daemon/daemon.go` (wire ProjectManager)
- `internal/daemon/components.go` (create ProjectManager)
- `internal/agent/handler.go` (respect project path for tool execution)
- `internal/context/context_builder.go` (scope to project path)
- `internal/tui/command_handler.go` (add `/project` commands)
- `internal/tui/app.go` (status bar project indicator)
- `internal/rpc/proxy.go` (project RPC methods)

### Phase 2: Worktree Management

**Scope**: Git worktree creation, per-session worktrees, worktree cleanup.

Changes:
1. Extend `ProjectManager` with worktree CRUD
2. Auto-create worktree on session-project binding
3. Route tool execution CWD to session's worktree
4. Merge session branch to project default branch on session end
5. Cleanup orphaned worktrees on daemon startup

**Files touched**:
- `internal/project/manager.go` (worktree methods)
- `internal/project/worktree.go` (new file)
- `internal/agent/handler.go` (use worktree path)
- `internal/security/engine.go` (fence to worktree path)
- `internal/daemon/components.go` (startup cleanup)

### Phase 3: Path Fencing (Security)

**Scope**: `SecurityEngine` path fencing, `--nofence` flag, per-project permissions.

Changes:
1. Add `FenceConfig` to security engine
2. Validate all tool calls against project fence
3. Implement `--nofence` client flag (passed via RPC on session create)
4. Per-project permission profiles in config
5. Audit log entries include project context

**Files touched**:
- `internal/security/engine.go` (fence validation)
- `internal/security/fence.go` (new file)
- `cmd/meept/chat.go` (--nofence flag)
- `internal/config/schema.go` (project permissions)
- `internal/tui/app.go` (unfenced indicator)

### Phase 4: TUI + UI Panels

**Scope**: Projects dialog, tab identity, visual indicators.

Changes:
1. `ctrl+x p` projects modal in TUI
2. Session tab labeled with project name
3. Flutter UI project picker
4. Flutter UI tab identity
5. Project status indicators

**Files touched**:
- `internal/tui/modal.go` (projects modal)
- `internal/tui/app.go` (keybinding, status bar)
- `internal/tui/types/types.go` (project types)
- Flutter UI files (tab bar, project picker, status indicator)

### Phase 5: Cross-Machine Sync + Clustering Prep

**Scope**: Git-based sync, multi-daemon coordination.

Changes:
1. Auto-push on commit (configurable)
2. Branch discovery between daemon instances
3. Conflict detection
4. `meept project sync` for manual sync

Tracked in future issue: `meept-clustered-worktree-sync`.

## Open Questions (Resolved)

| # | Question | Resolution |
|---|----------|------------|
| 1 | Auto-clone unknown repos? | No. Require explicit `project add <url>` with user confirmation. Auto-detection only registers repos already present on disk. |
| 2 | WorkspaceManager interaction? | Git projects use worktrees. Local/detached projects use existing scratch dirs (`~/.meept/workspaces/`). WorkspaceManager is deprecated for git-mode projects. |
| 3 | Switch projects mid-session? | Creates a new session with confirmation prompt. Old session is preserved. |
| 4 | Monorepo support? | Project can be a subdirectory within a git repo. Git root is the sync boundary; project path is the fenced execution root. |
| 5 | Detached mode security? | Default: restricted to CWD project path. `--nofence` disables fencing for power users. Full filesystem access only with `--nofence`. |

## Future Work

- **Clustering**: Any meept-daemon instance clones the same repo, creates worktrees, syncs via git. Issue: `meept-clustered-worktree-sync`.
- **Plan-scoped worktrees**: Automatic isolation for complex plans based on heuristics.
- **Project templates**: Pre-configured project types (Go, Rust, Python) with default allowed tools and fence rules.
- **Project-level memory**: Episodic memory scoped per project, so memories from one project don't pollute another.

## Implementation Plan

See `docs/plans/2026-05-27-project-context.md` for the full task-by-task implementation plan (18 tasks across 9 phases).
