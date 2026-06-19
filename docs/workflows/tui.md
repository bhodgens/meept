# TUI

## Overview

Terminal UI built with bubbletea v2 (`internal/tui/`). Provides chat, sessions, tasks, plans, agents, and search views.

## Problem

The daemon exposes RPC + HTTP; the TUI is the primary interactive client for terminal users. It needs to support all major workflows without forcing users to memorize commands.

## Behavior

- **Views** (`internal/tui/app.go`): `ViewChat`, `ViewSessions`, `ViewTasks`, `ViewPlans`, `ViewAgents`, `ViewSearch`. Tab-switching via number keys; `?` for help.
- **Chat view** (`internal/tui/models/chat.go`): message rendering, input textarea, in-session find via `ctrl+f` (Spec A). Find bar supports case-sensitive (`alt+c`), regex (`alt+r`), prev/next (`shift+enter`/`enter`), and ANSI highlighting.
- **Sessions view** (`internal/tui/models/sessions.go`): list sessions, switch, delete. Press `f` to open global search.
- **Search view** (`internal/tui/models/search.go`): debounced semantic search (250ms) across all scopes. Scope cycling via `tab`, navigate via `up`/`down`/`j`/`k`, open via `enter`, close via `esc`.
- **RPC client** (`internal/tui/rpc.go`): calls `search.semantic` and other RPC methods on the daemon.

## Configuration

Keybindings configurable via `~/.meept/client.json5`. Default leader key: `space`.

## Edge Cases

- Search model nil-safe when RPC unavailable: shows "search unavailable" instead of crashing.
- Find bar auto-closes on session change.
- Search result navigation for non-message types (task/memory/plan): logs debug; MVP-deferred.

---

*Updated with Global Semantic Search spec (search view).*
