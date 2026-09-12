# TUI

## Overview

Terminal UI built with bubbletea v2 (`internal/tui/`). Provides chat, sessions, tasks, plans, agents, and search views.

## Problem

The platform exposes RPC + HTTP; the TUI is the primary interactive client for terminal users. It needs to support all major workflows without forcing users to memorize commands.

## Behavior

- **Views** (`internal/tui/app.go`): `ViewChat`, `ViewSessions`, `ViewTasks`, `ViewPlans`, `ViewAgents`, `ViewSearch`. Tab-switching via number keys; `?` for help.
- **Pending changes modal** (`internal/tui/modals/pending_changes.go`): `ctrl+d` lists staged changes of the current session (via `changes.*` RPC); `j`/`k` navigate, `v` views the full diff, `a` accepts, `r` rejects, `esc` closes. Status bar shows a count indicator while changes await review. See [change review](change-review.md).
- **Chat view** (`internal/tui/models/chat.go`): message rendering, input textarea, in-session find via `ctrl+f` (Spec A). Find bar supports case-sensitive (`alt+c`), regex (`alt+r`), prev/next (`shift+enter`/`enter`), and ANSI highlighting.
- **Sessions view** (`internal/tui/models/sessions.go`): list sessions, switch, delete. Press `f` to open global search.
- **Search view** (`internal/tui/models/search.go`): debounced semantic search (250ms) across all scopes. Scope cycling via `tab`, navigate via `up`/`down`/`j`/`k`, open via `enter`, close via `esc`.
- **Tasks view** (`internal/tui/models/tasks.go`): three modes cycled with `tab` — tasks, scheduled jobs, lineage (`t` toggles lineage). Each mode has its own column set; switching modes reinstalls the columns and re-renders the cached rows together. Rows are only ever written for the mode that is displayed, so a fetch that lands after a mode switch is ignored.
- **Table rendering** (`internal/tui/models/*.go`, `internal/tui/agents_panel.go`): every table is sized on both axes through `internal/tui/tableutil.Size` and its rows written through `tableutil.SetRows`. Two failures come from skipping that: a table given only a height keeps a zero-width viewport and renders a header over an empty body, and a row with more cells than the column list panics (`index out of range`) inside `bubbles/table`. Changing columns must clear the rows first (`SetColumns` re-renders them) and repopulate from cache afterwards, or every terminal resize blanks the table.
- **RPC client** (`internal/tui/rpc.go`): calls `search.semantic` and other RPC methods on the platform.

## Configuration

Keybindings configurable via `~/.meept/client.json5`. Default leader key: `space`.

## Edge Cases

- Search model nil-safe when RPC unavailable: shows "search unavailable" instead of crashing.
- Find bar auto-closes on session change.
- Search result navigation for non-message types (task/memory/plan): logs debug; MVP-deferred.

---

*Updated with Global Semantic Search spec (search view).*
