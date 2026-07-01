# `/project` Command Design Specification

**Date**: 2026-06-30
**Status**: Approved
**Phase**: Phase 1 (local-only, single daemon)
**Cluster resource model**: Deferred to #0000-cluster-resource-model

---

## 1. Summary

The `/project <path>` command sets the workspace root for the current session. It provides a path-based typeahead showing the 5 most recent project paths, with filesystem fallback for new paths. On selection, the daemon auto-detects the project (walking up for `.git`), registers it if new, binds it to the session, and updates the agent loop's working directory.

**Key design decisions:**
- Single verb: `/project <path>` (no subcommands for the common path)
- Daemon-side recents table (`project_recents` in `projects.db`)
- Typeahead RPC: `project.readdir` returns (recents + filesystem fallback)
- Model 1: Registry is hidden machinery, auto-register on use
- Edge 1: Non-git paths registered as `mode="local"`, no git-init prompt (suppress by default)
- Edge 2: Typed path is project root; git root stored separately for `/git` ops (model 1-iii)
- Edge 3: All paths canonicalized (symlinks resolved, absolute)

---

## 2. Architecture

### 2.1 Data Flow

```
┌─────────────────────────────────────────────────────────────────┐
│  TUI / Flutter Client                                           │
│  ┌─────────────────┐     ┌─────────────────────────────────┐   │
│  │  Slash Input    │────▶│  Typeahead Component            │   │
│  │  /project <path>│     │  - Shows cached recents on open │   │
│  └─────────────────┘     │  - Calls project.readdir RPC    │   │
│                          │  - Filters recents + fs fallback│   │
│                          └───────────────┬─────────────────┘   │
│                                          │ RPC                  │
└──────────────────────────────────────────┼─────────────────────┘
                                           │
                                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  Daemon                                                         │
│  ┌─────────────────┐     ┌─────────────────────────────────┐   │
│  │  RPC Handler    │────▶│  ProjectManager                 │   │
│  │  project.readdir│     │  - DetectFromPath (git walk)    │   │
│  │  project.set    │     │  - Upsert project (git/local)   │   │
│  │                 │     │  - Set session.ProjectPath      │   │
│  │                 │     │  - Update AgentLoop.workingDir  │   │
│  └─────────────────┘     └───────────────┬─────────────────┘   │
│                                          │                     │
│  ┌─────────────────────────────────────────┴─────────────┐     │
│  │  SQLite (projects.db)                                 │     │
│  │  - projects: id, name, mode, local_path, git_root, …  │     │
│  │  - project_recents: path, last_used (TTL-pruned)      │     │
│  │  - sessions: id, project_id, project_path, …          │     │
│  └───────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 Component Responsibilities

| Component | Responsibility |
|-----------|----------------|
| **Client (TUI/Flutter)** | Render typeahead, cache recents from RPC, slash dispatch to `project.set` |
| **RPC Handler** (`internal/rpc/projects.go`) | New `project.readdir` method; existing `project.set` extended to update recents |
| **ProjectManager** (`internal/project/manager.go`) | `DetectFromPath` already exists; add `TouchRecent(path)` |
| **Session Store** (`internal/session/store_sqlite.go`) | No changes — already has `SetProject` |
| **AgentLoop** (`internal/agent/loop.go`) | Add `SetWorkingDir(string)` method + mutex guard |
| **SQLite schema** (`internal/project/store.go`) | New table `project_recents` |

### 2.3 Invariants

1. Every `/project <path>` → upserts `projects` row, touches `project_recents`, sets `session.ProjectPath`.
2. `AgentLoop.workingDir` is updated per-session (fixes the existing gap at `loop.go:471`).
3. Recents are daemon-side, cached client-side for instant open.

---

## 3. Components and Interfaces

### 3.1 Recents Table Schema

```sql
CREATE TABLE IF NOT EXISTS project_recents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_path TEXT UNIQUE NOT NULL,  -- canonical absolute path
    last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_recents_last_used
    ON project_recents(last_used_at DESC);
```

**TTL:** Prune entries older than 30 days via daily scheduler job.
**Cap:** Max 100 entries retained (prune oldest beyond 100).

**Upsert logic:**
```sql
INSERT INTO project_recents (project_path, last_used_at)
VALUES (?, datetime('now'))
ON CONFLICT(project_path) DO UPDATE SET last_used_at = datetime('now');
```

### 3.2 RPC Methods

#### `project.readdir` (new)

```go
// Request
type ReadDirRequest struct {
    Prefix string `json:"prefix"`  // e.g. "~/code/fo"
}

// Response
type ReadDirResponse struct {
    Recents    []string `json:"recents"`     // Top 5 from project_recents
    Matches    []string `json:"matches"`     // Filesystem fallback (if no recent matches prefix)
    GitRoots   []string `json:"git_roots"`   // Discovered git root per match (empty if none)
}
```

**Behavior:**
1. Query top-5 recents (always).
2. Filter recents by `Prefix` (substring match).
3. If zero recent matches → `readdir` on `ExpandTilde(Prefix)` up to 50 entries, sorted alphabetically.
4. For each fs entry, walk up to find `.git` (store as `git_root`, empty if none).

#### `project.set` (modified)

```go
// Request (extended)
type SetProjectRequest struct {
    SessionID string `json:"session_id"`
    ProjectID string `json:"project_id"`  // or look up by path
    Path      string `json:"path"`        // NEW: allows path-only invocation
}
```

**Behavior changes:**
1. If `Path` provided, call `DetectFromPath(Path)` → upsert project.
2. Set `session.ProjectPath` + `session.ProjectID`.
3. Call `AgentLoop.SetWorkingDir(Path)` — **fixes the gap**.
4. Call `TouchRecent(Path)`.

### 3.3 Client-Side Typeahead (TUI)

**File:** `internal/tui/components/project_typeahead.go` (new)

**Behavior:**
1. On open (`/project ` typed): call `project.readdir("")` → cache result, render recents.
2. On keystroke: filter cached recents by prefix; if zero matches, call `project.readdir(prefix)` for fs fallback.
3. On select: dispatch existing `/project set <path>` flow (which now calls the extended `project.set` RPC).

**Debouncing:** 150ms delay before firing fs fallback RPC (prevents spam on fast typists).

### 3.4 AgentLoop WorkingDir Fix

**File:** `internal/agent/loop.go`

**Add:**
```go
// SetWorkingDir updates the working directory for artifact scanning.
// Safe to call concurrently.
func (l *AgentLoop) SetWorkingDir(path string) {
    l.mu.Lock()
    defer l.mu.Unlock()
    l.workingDir = path
}
```

**Call site:** `internal/rpc/projects.go:handleSetProject` (after `sessionStore.SetProject` succeeds).

### 3.5 Edge-Case Handlers

| Edge case | Handler location | Behavior |
|-----------|------------------|----------|
| Non-git path | `ProjectManager.DetectFromPath` | Register as `mode="local"`, `git_root=""`. Client config `prompt_git_init` controls whether TUI shows a follow-up dialog (out of band). |
| Subpackage in monorepo | `DetectFromPath` | Walk up to find `.git`, store as `git_root`, but keep `local_path` = typed path. |
| Symlinks | Client before RPC | `filepath.EvalSymlinks` + `filepath.Abs` before sending to daemon. |
| Invalid path | `project.readdir` | Return error `"no such directory"`; client shows toast. |

---

## 4. Error Handling

### 4.1 Typeahead RPC Failures

| Failure | Client behavior |
|---------|-----------------|
| Daemon unreachable | Show cached recents (from last session); fs fallback unavailable. Toast: "daemon offline, using cached recents". |
| `project.readdir` timeout (>2s) | Render recents only; suppress fs fallback silently (non-fatal). |
| Permission denied on path | Return error `"permission denied: <path>"`; client shows single-line error in typeahead dropdown. |

### 4.2 Project Set Failures

| Failure | Client behavior |
|---------|-----------------|
| Path doesn't exist | RPC returns `"not_found"`; client shows error toast + keeps old project. |
| SQLite write failure | RPC returns `"db_readonly"`; client shows persistent error "project persistence failed". |
| AgentLoop.SetWorkingDir fails (path unmounted) | RPC returns `"workingdir_unreachable"`; client allows override ("set anyway, agent may not see files"). |

### 4.3 Recents Prune Failure (Daemon-Side)

Silent noop. Non-fatal; next daemon start retries prune via `components.go` startup job.

### 4.4 Distributed Daemon Edge Case

If daemon runs remotely (different filesystem namespace), `project.readdir` returns `"fs_namespace_mismatch"` error. Client disables typeahead permanently, shows notice: "remote daemon: filesystem completion unavailable". User must type full paths from recents only.

---

## 5. Testing

### 5.1 Unit Tests

| File | Functions to test |
|------|-------------------|
| `internal/project/recents_test.go` | `TouchRecent`, `ListRecents`, prune logic, cap logic |
| `internal/rpc/projects_test.go` | `handleReadDir` (recents + fs fallback), `handleSetProject` (upsert + workingDir update) |
| `internal/agent/loop_test.go` | `SetWorkingDir` (concurrent safety), `loadAgentsContext` (uses new workingDir) |
| `internal/tui/components/project_typeahead_test.go` | Filtering logic, debounce timing, select callback |

### 5.2 Integration Tests

| File | Scenario |
|------|----------|
| `tests/integration/project_typeahead_test.go` | End-to-end: open typeahead → type prefix → select → verify `session.ProjectPath` updated + workingDir changed |
| `tests/integration/project_detection_test.go` | Non-git path, monorepo subpackage, symlink resolution, invalid path errors |

### 5.3 CLI Smoke Tests

| Command | Expected |
|---------|----------|
| `meept chat --project ~/code/foo` | Daemon binds session to path, recents updated |
| `meept config get project` | Shows current session's bound project |

---

## 6. TUI and Flutter Parity

### 6.1 TUI Changes

| File | Change |
|------|--------|
| `internal/tui/command_handler.go` | Modify `executeProjectSet` to also accept a path (not just name/ID); dispatch new RPC |
| `internal/tui/components/project_typeahead.go` | New component (typeahead modal with recents + fs fallback) |
| `internal/tui/app.go` | Wire typeahead to slash input; handle `SetProjectResultMsg` |

### 6.2 Flutter Changes

| File | Change |
|------|--------|
| `ui/flutter_ui/lib/core/slash_commands.dart` | Add handler for `/project <path>` (currently null) |
| `ui/flutter_ui/lib/features/projects/project_picker.dart` | New dialog widget (recents + path input + fs browsing) |
| `ui/flutter_ui/lib/services/sdk_client.dart` | Add `setProject(path)` method (HTTP: `POST /api/v1/projects/set` with path body) |

---

## 7. Config and Migration

### 7.1 Config Schema Changes

**File:** `internal/config/schema.go`

**Add:**
```go
type ProjectRecentConfig struct {
    MaxEntries int  `json:"max_entries"`  // default 5
    TTLDays    int  `json:"ttl_days"`     // default 30
}

// In Config struct:
ProjectsRecent *ProjectRecentConfig `json:"projects_recent,omitempty"`
```

### 7.2 SQLite Migration

**File:** `internal/project/store.go`

**Migration SQL:**
```sql
CREATE TABLE IF NOT EXISTS project_recents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_path TEXT UNIQUE NOT NULL,
    last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_recents_last_used
    ON project_recents(last_used_at DESC);
```

Migration runs on daemon startup (existing migration pattern at `store.go:37-43`).

---

## 8. Rollout Plan

| Phase | Deliverable | Risk |
|-------|-------------|------|
| 1. Recents table + RPC | Backend only, no UI | Low |
| 2. `project.readdir` RPC | Backend + TUI typeahead | Medium (new RPC surface) |
| 3. `workingDir` fix | `AgentLoop.SetWorkingDir` call site | Medium (touches agent loop) |
| 4. Flutter parity | Dialog widget + SDK method | Low |
| 5. Config knob + pruning | Scheduler job, TTL | Low |

**Total estimated effort:** ~1 day (4-6 hours backend + 2-3 hours TUI + 2-3 hours Flutter).

---

## 9. Out of Scope (Deferred)

- **Cross-daemon project replication** — tracked in `.github/ISSUES/0000-cluster-resource-model.md`
- **Git-init prompt** — suppress by default; user runs `/git init` manually if desired
- **`/git` helper relocation** — `sync`/`status`/`add <url>` remain under `/project` for Phase 1

---

## 10. Acceptance Criteria

- [ ] `/project <path>` sets session project and updates agent `workingDir`
- [ ] Typeahead shows 5 recents on open, filesystem fallback on type (recents-first model D)
- [ ] Non-git paths registered as `mode="local"`
- [ ] Monorepo subpaths: typed path = project root, git root stored separately
- [ ] Recents pruned after 30 days, capped at 100 entries
- [ ] TUI and Flutter parity (typeahead modal + dialog)
- [ ] Unit tests >80% coverage on new code
- [ ] Integration tests pass for all edge cases

---

## 11. Open Questions

None resolved during design. All decisions documented in §1.

---

## 12. Related Issues

- `.github/ISSUES/0000-cluster-resource-model.md` — Cross-daemon task context gap (deferred)
