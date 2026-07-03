# Session CLI Enhancement Design

**Date:** 2026-07-03
**Status:** Approved

## Overview

Enhance the `meept chat` CLI command with session targeting capabilities and establish a canonical session for one-shot queries.

## Problem Statement

Users need to:
1. Continue existing sessions from the CLI without entering TUI
2. Have a clear destination for one-shot queries that don't require session tracking
3. See canonical session IDs consistently across all interfaces (TUI, Flutter GUI)

## Design Decisions

### 1. CLI `--session` Flag

Add `--session` (alias: `-s`) flag to `meept chat` command for targeting specific sessions.

#### Behavior Matrix

| Invocation | Behavior |
|------------|----------|
| `meept chat --session session-abc123 "msg"` | Appends message to existing session, prints response, exits. **Errors if session not found.** |
| `meept chat --session session-abc123` (no message) | Opens TUI directly attached to that session |
| `meept chat "msg"` (no --session) | Routes to `oneshot_responses` session (auto-created if needed) |
| `meept chat` (no args, no --session) | Opens TUI to most recent session |

#### Implementation Notes

- Session ID validation: Must match existing session (no auto-create on typo)
- Single-message mode: Always exits after response, never enters TUI
- TUI launch mode: Opens directly to targeted session view

### 2. One-Shot Session

**Canonical name:** `oneshot_responses`

- Auto-created on first use if it doesn't exist
- All `meept chat "..."` commands without `--session` route here
- Visible in session list like any other session
- User can delete, archive, or reference it via `--session`

#### Why `oneshot_responses`?

- Explicit naming (not hidden with underscore prefix)
- Clear purpose from name alone
- Distinguishable from regular sessions in list views

### 3. Session ID Display

**Canonical format:** `session-XXXXXXXXXXXXXXXX` (16 hex chars)

Display requirements for both TUI and Flutter GUI:

- Show full canonical session ID in session detail view
- Position: Right-hand detail pane
- Label: `id:`
- No truncation in detail view (truncation acceptable in list/table views)

#### Current State

- **TUI:** Already displays in `internal/tui/models/sessions.go:renderSessionDetail()` (line 532-534)
- **Flutter GUI:** Verify/add session ID display in session detail panel

## API Changes

### New/Modified RPC Methods

| Method | Change |
|--------|--------|
| `session.get` | No change - already returns full session with ID |
| `session.create` | Support `name: "oneshot_responses"` explicitly |
| `session.list` | No change |

### CLI Command Changes

```bash
# New flag on existing chat command
meept chat [--session|-s <session-id>] [message]

# Examples
meept chat --session session-abc123 "continue implementing that feature"
meept chat -s session-abc123        # Open TUI to this session
meept chat "what is France?"        # Uses oneshot_responses
meept chat                          # Opens TUI to most recent
```

## Data Model

No schema changes required. Uses existing `sessions` table:

```sql
-- Existing schema, no changes needed
CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_activity DATETIME DEFAULT CURRENT_TIMESTAMP,
    archived BOOLEAN DEFAULT FALSE,
    ...
);
```

## UI Requirements

### TUI (Terminal User Interface)

- **Current:** Session ID displayed in right detail pane
- **Action:** Verify consistent formatting with Flutter GUI

### Flutter GUI

- **Current:** Session detail view exists
- **Action:** Ensure canonical session ID is displayed with `id:` label in detail view

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Session not found | `Error: session "session-xyz" not found` |
| Invalid session ID format | Treat as not-found (no special validation) |
| oneshot_responses creation fails | Log error, use ephemeral session (current `cli-` behavior) |

## Migration/Backward Compatibility

- **Breaking changes:** None
- **Existing behavior preserved:**
  - `meept chat` without args still opens TUI
  - `meept chat "msg"` without `--session` now uses `oneshot_responses` instead of ephemeral session
- **Migration needed:** No

## Testing Requirements

- [ ] `--session` flag parsing and shorthand `-s`
- [ ] Session not-found error handling
- [ ] `oneshot_responses` auto-creation
- [ ] TUI launch with `--session` but no message
- [ ] Session ID display in TUI detail pane
- [ ] Session ID display in Flutter GUI detail pane

## Out of Scope

- Session ID shortening/aliasing (keep full format everywhere)
- `meept session current` command (not needed for this enhancement)
- Ephemeral/non-persisted one-shot sessions (user may want to reference later)

## Success Criteria

1. User can run `meept chat --session <id> "continue..."` and get response appended to existing session
2. User can run `meept chat --session <id>` and open TUI to that session
3. One-shot queries accumulate in `oneshot_responses` session
4. Session ID visible in both TUI and Flutter GUI detail panes with consistent formatting
