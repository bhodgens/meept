# `/project` Command

> Status: **Implemented** (2026-06-30)
> Related: [Project Context](project-context.md)

## Overview

The `/project` command provides project management and switching capabilities within Meept sessions. It includes a typeahead UI for selecting from recent projects, with intelligent filtering and filesystem fallback.

## Command Reference

### `/project` (no arguments)

Display current project information for the active session.

### `/project list`

List all registered projects.

**Output:**
- Project ID, name, mode (git/local)
- Git URL (for git mode projects)
- Local path
- Status (active/archived)

### `/project set <path|name|id>`

Switch the current session to a different project.

**Arguments:**
- `path` - Absolute path to a project directory (auto-detects and registers if needed)
- `name` - Name of a registered project
- `id` - Project ID

**Behavior:**
1. Resolves the argument to a project (path auto-detection, name lookup, or ID)
2. Binds the session to the project
3. Updates the AgentLoop working directory for artifact scanning
4. Touches the recents table for typeahead prioritization
5. Publishes a bus event for cross-component synchronization

### `/project add <path|url>`

Register a new project.

**Arguments:**
- `path` - Local directory path to register
- `url` - Git repository URL to clone

**Examples:**
```bash
/project add /home/user/my-project
/project add https://github.com/user/repo.git
```

### `/project sync`

Synchronize the current project (git pull --ff-only).

**Requirements:**
- Only works for git mode projects
- Requires remote `origin` to be configured

### `/project status`

Display git status for the current project:
- Current branch
- Dirty state (modified files count)
- Ahead/behind remote

## Typeahead UX

The `/project set` command supports an interactive typeahead interface for quick project switching.

### Features

| Feature | Description |
|---------|-------------|
| Recent projects | Top 5 most recently used projects (by last_used_at) |
| Prefix filtering | Substring match on project paths |
| Keyboard navigation | Up/down arrows to select, Enter to confirm, Escape to cancel |
| Filesystem fallback | When no recents match, lists subdirectories of the typed path |
| Git root detection | Identifies git roots for filesystem matches |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate filtered results |
| `Enter` | Select highlighted project |
| `Escape` | Close typeahead without selection |

### Recents Behavior

**Storage:** SQLite table `project_recents` in `~/.meept/projects.db`

**Schema:**
```sql
CREATE TABLE project_recents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_path TEXT UNIQUE NOT NULL,
    last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_recents_last_used ON project_recents(last_used_at DESC);
```

**Operations:**
- `TouchRecent(path)` - Insert or update timestamp (called on `/project set`)
- `ListRecents(limit)` - Return top N paths by recency
- `PruneOlderThan(ttl)` - Remove entries older than TTL
- `CapToN(max)` - Keep only the most recent N entries

**Defaults:**
- `max_entries`: 50 (maximum recents to retain)
- `ttl_days`: 30 (entries older than 30 days are pruned)

**Maintenance:** A daily scheduled job (`project.recents_prune`) runs at 24-hour intervals to:
1. Prune entries older than `ttl_days`
2. Cap total entries to `max_entries`

## Configuration

Configure recents behavior in `~/.meept/meept.json5`:

```json5
{
  projects_recent: {
    max_entries: 50,   // Maximum number of recent projects to retain
    ttl_days: 30,      // Entries older than this are pruned
  }
}
```

**Schema:**
```go
type ProjectRecentConfig struct {
    MaxEntries int `json:"max_entries" toml:"max_entries"`
    TTLDays    int `json:"ttl_days" toml:"ttl_days"`
}
```

## Architecture

### Components

| Component | File | Responsibility |
|-----------|------|----------------|
| RecentsStore | `internal/project/recents.go` | SQLite-backed recents tracking |
| ProjectManager | `internal/project/manager.go` | Project registration and recents integration |
| project.readdir RPC | `internal/rpc/projects.go` | Typeahead data source (recents + filesystem fallback) |
| project.set RPC | `internal/rpc/projects.go` | Project binding, recents update, bus event publishing |
| ProjectTypeahead | `internal/tui/components/project_typeahead.go` | TUI typeahead component |
| AgentLoop.SetWorkingDir | `internal/agent/loop.go` | Working directory synchronization |

### Request Flow

```
User types "/project set "
       ↓
TUI opens ProjectTypeahead component
       ↓
Calls project.readdir RPC with prefix
       ↓
Handler returns: { recents: [...], matches: [...], git_roots: [...] }
       ↓
User selects project
       ↓
TUI calls project.set RPC
       ↓
Handler: 1) Binds session to project
         2) Touches recents
         3) Publishes bus event
       ↓
AgentLoop receives bus event → calls SetWorkingDir(path)
```

### Message Bus Event

**Topic:** `project.set`

**Payload:**
```json
{
  "session_id": "session-xxx",
  "path": "/absolute/path/to/project"
}
```

**Subscribers:**
- `AgentLoop` - Updates working directory for artifact scanning

## Testing

### Unit Tests

**File:** `internal/rpc/projects_recents_test.go`

Tests cover:
- Empty recents, empty prefix
- Recents-only queries
- Prefix filtering (substring match)
- Filesystem fallback
- Tilde expansion (`~` → home directory)
- Fs fallback cap (50 entries max)
- Recents trump filesystem fallback

### Integration Tests

**File:** `tests/integration/project_typeahead_test.go`

Tests cover:
- End-to-end typeahead flow
- Session project binding
- Message bus event publishing
- Empty prefix returns all recents
- No matching results handling

## Related Files

- `internal/project/store.go` - SQLite schema (project_recents table)
- `internal/project/recents.go` - RecentsStore implementation + SchedulePruneJob
- `internal/project/manager.go` - ProjectManager.TouchRecent, ListRecents
- `internal/rpc/projects.go` - RPC handlers (handleReadDir, handleSet)
- `internal/agent/loop.go` - SetWorkingDir, StartProjectSub (bus subscription)
- `internal/tui/components/project_typeahead.go` - TUI component
- `internal/tui/command_handler.go` - `/project` slash command dispatcher
- `internal/daemon/components.go` - Daemon wiring (RecentsStore, SchedulePruneJob)
- `internal/config/schema.go` - ProjectRecentConfig schema
- `config/meept.json5` - Default configuration

## Troubleshooting

**Typeahead shows no recents:**
- Check `~/.meept/projects.db` exists
- Verify `projects_recent` table: `sqlite3 ~/.meept/projects.db "SELECT * FROM project_recents;"`
- Recents may have been pruned (check config TTL)

**project.set fails with "session not found":**
- Session must be created before binding
- Check session store is wired correctly

**Bus event not received:**
- Verify message bus is initialized in daemon
- Check subscription topic matches exactly (`project.set`)
