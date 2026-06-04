# Flutter UI Parity Design

Date: 2026-06-03
Status: Draft

## Overview

Bring the Flutter GUI client to parity with the meept TUI across 10 areas: layout restructure, chat header, input behavior, enter key semantics, keyboard shortcuts, slash commands, markdown rendering, icons, status bar, and configuration exposure.

## 1. Layout Restructure

### Current
- Top tab bar → status bar (version + connected) → content area → right tools sidebar in chat tab

### New
```
┌─────────────────────────────────────────────────┐
│ [Orange header bar: session name │ summary]      │
├─────────────────────────────────────────────────┤
│ [Tab bar]          [tools ▾] [● connected]      │
├─────────────────────────────────────────────────┤
│                                                 │
│  Main content area (full width)                 │
│  Chat / Sessions / Plans / Tasks / Agents       │
│                                                 │
├─────────────────────────────────────────────────┤
│ [Auto-expanding input: up to 8 lines]           │
│ [slash autocomplete popup overlays here]         │
└─────────────────────────────────────────────────┘
```

**Drawer overlay:** Slides from left when triggered by toolbar icon or `leader d`. Shows one panel at a time with tabs: Status, Agent Activity, Tasks, Recent Memory, Metrics. Clicking outside or pressing `esc` dismisses it.

**Removed:** Version status bar entirely. Connection indicator moves to toolbar right side.

**Tools:** Moved from right sidebar to a `tools ▾` dropdown in the toolbar. Dropdown shows all available skills/tools loaded from the daemon API, with icons. Selecting a tool replaces the main content area with the corresponding panel (memory, settings, terminal, etc.).

### Files affected
- `ui/flutter_ui/lib/features/home/home_screen.dart` — remove status bar, add toolbar with tools dropdown
- `ui/flutter_ui/lib/features/chat/chat_tab.dart` — remove right sidebar, full-width layout
- `ui/flutter_ui/lib/features/sidebar/tools_panel.dart` — convert to toolbar dropdown widget
- New: `ui/flutter_ui/lib/features/drawer/drawer_overlay.dart` — drawer widget
- New: `ui/flutter_ui/lib/features/drawer/panels/` — Status, AgentActivity, Tasks, RecentMemory, Metrics panels

## 2. Chat Header (Orange Bar)

### Current
Dark gray bar showing "chat active" + connection dot + session ID (8 chars).

### New
Full-width orange bar (`#F97316` background, black `#000000` text, bold, padding 0,1). Content matches TUI logic:
- Both name and description: `SessionName │ Description...` (description truncated with `...`)
- Name only (non-default): `session-name`
- Description only: `description text...`
- Nothing: `meept`

Padded with spaces to fill full width.

### Files affected
- `ui/flutter_ui/lib/features/chat/chat_view.dart` — replace `_buildHeader()` with orange bar
- Session name and description fetched from active session provider (already available via `sessionNotifier`)

## 3. Input Area & Paste Handling

### Current
Fixed 80px height, `maxLines: 3`, no paste detection.

### New
- Auto-expanding from ~2 lines up to 8 lines based on content
- Paste detection: when 3+ lines are added in a single update, compress the pasted content to a `{paste: N lines}` token, store original in a `Map<int, String>`
- On send: expand all paste tokens back to original content before sending to API
- File path detection: auto-detect valid file paths in pasted content (matching TUI behavior)

### Files affected
- `ui/flutter_ui/lib/features/chat/chat_input.dart` — rewrite with auto-expanding text field, paste detection, paste token management

## 4. Enter Key Behavior

### Current
Single Enter sends message. No newline insertion mechanism. No double-enter.

### New
- Single Enter: send and queue message (current behavior, unchanged)
- Double Enter (two Enter presses within 300ms): send + steer (configurable to `interrupt` or `preempt`)
- Shift+Enter: insert newline character

Implementation: track `_lastEnterTime` in input widget state. On Enter press, check if previous Enter was within 300ms. If yes, send via steer endpoint. If no, start a 300ms timer before sending normally. If the timer fires without a second Enter, send as normal.

The daemon already has separate endpoints for different send modes:
- `POST /api/v1/chat` — normal send (queue)
- `POST /api/v1/chat/steer` — steer active agent (direct queue injection, bypasses bus)
- `POST /api/v1/chat/followup` — follow-up while agent active

No backend changes needed. The Flutter client routes to the appropriate endpoint based on the double-enter detection. For `interrupt` and `preempt` modes (if configured as alternatives to `steer`), new endpoints would need to be added (`/chat/interrupt`, `/chat/preempt`) with corresponding `MessageQueue` methods, but the default `steer` behavior is already fully supported.

### Files affected
- `ui/flutter_ui/lib/features/chat/chat_input.dart` — double-enter detection, 300ms timer
- `ui/flutter_ui/lib/providers/chat_provider.dart` — add `sendSteer()` method routing to `/chat/steer`
- `ui/flutter_ui/lib/services/api_client.dart` — expose steer/followup endpoints

### TUI changes (matching behavior)
- `internal/tui/models/chat.go` — add double-enter detection with timestamp tracking; on double-enter, call `rpc.Steer()` instead of `rpc.Chat()` or `rpc.FollowUp()`. Currently the TUI uses Ctrl+S to toggle `steerMode` before submitting — double-enter provides a faster path to the same result.

## 5. Keyboard Shortcuts

### New feature

Uses Flutter's `Shortcuts` + `Actions` widget tree. Default leader key: `cmd+x` (macOS) / `ctrl+x` (linux/windows). Configurable.

**Leader sequences (matching TUI):**
| Sequence | Action |
|----------|--------|
| `leader` → `s` | Switch to Sessions tab |
| `leader` → `p` | Find/search (focus search) |
| `leader` → `b` | Branches (project context) |
| `leader` → `d` | Toggle drawer overlay |
| `leader` → `c` | Switch to Chat tab |
| `leader` → `?` | Show keyboard shortcut help |

**Direct shortcuts:**
| Key | Action |
|-----|--------|
| `cmd+k` | Focus input with `/` prefix (slash command mode) |
| `esc` | Close drawer, dismiss popup, blur input |
| `tab` | In input: cycle focus; in slash popup: accept completion |

### Files affected
- New: `ui/flutter_ui/lib/core/shortcuts.dart` — Shortcuts/Actions definitions, leader key state machine
- `ui/flutter_ui/lib/features/home/home_screen.dart` — wrap with Shortcuts widget
- `ui/flutter_ui/lib/main.dart` — add keyboard listener

## 6. Slash Commands

### Current
No slash command handling. Raw text sent to API.

### New
- When input starts with `/`, trigger autocomplete:
  - Single match: inline ghost text (gray), tab to accept
  - Multiple matches: popup overlay with up to 8 commands, orange highlight on matched prefix, arrow/tab/enter navigation, esc to cancel
- Built-in commands: `/help`, `/new`, `/clear`, `/stop`, `/status`, `/session`, `/model`, `/compact`, `/retry`, `/undo`, `/usage`, `/vim`, `/task`, `/cancel`, `/amend`, `/interrupt`, `/tasks`, `/diff`, `/edit`, `/plan`, `/review`, `/project`
- Custom commands: fetched from daemon API (discovered from `.meept/commands/*.md` and `~/.meept/commands/*.md`)
- On selection: insert command name into input, position cursor after command for arguments
- On send with `/command`: execute locally (built-in) or send to daemon as template invocation

### Files affected
- New: `ui/flutter_ui/lib/features/chat/slash_autocomplete.dart` — popup widget + inline ghost text
- New: `ui/flutter_ui/lib/core/slash_commands.dart` — command registry, parsing, built-in handlers
- `ui/flutter_ui/lib/features/chat/chat_input.dart` — integrate autocomplete overlay
- `ui/flutter_ui/lib/services/api_client.dart` — fetch custom commands, invoke command templates
- `internal/comm/http/api_handlers.go` — add endpoint for listing custom commands (if not existing)

## 7. Markdown Rendering

### Current
Plain `Text()` widget for all messages. No markdown parsing.

### New
Use `flutter_markdown` package with custom `MarkdownStyleSheet` matching the TUI's glamour theme:
- H1: orange `#F97316`, H2: amber `#F59E0B`, H3: green `#10B981`, H4: blue `#3B82F6`, H5: purple `#8B5CF6`, H6: pink `#EC4899`
- Bold: white `#FFFFFF`
- Italic: light gray `#E5E7EB`
- Links: blue `#3B82F6`, underlined
- Inline code: green `#10B981` on dark bg `#1F2937`
- Code blocks: near-black bg `#111827` with syntax highlighting (using `highlight` package)
- Strings: yellow, keywords: pink, comments: gray, functions/classes: lime
- Blockquotes: left border in gray `│`
- Only applied to assistant messages. User messages remain plain text.

### Dependencies
- Add `flutter_markdown` to `pubspec.yaml`
- Add `highlight` + `highlight_languages` to `pubspec.yaml` for syntax highlighting

### Files affected
- `ui/flutter_ui/pubspec.yaml` — add dependencies
- `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart` — replace `Text()` with `MarkdownBody` + custom stylesheet
- New: `ui/flutter_ui/lib/theme/markdown_style.dart` — custom MarkdownStyleSheet definition

## 8. Icons

### Current
All using `Icons.*` from Material, but `uses-material-design: false` in pubspec.yaml. This causes "?" boxes when the icon font isn't bundled at runtime.

### Fix
1. Set `uses-material-design: true` in `pubspec.yaml`
2. Audit all `Icons.*` usage across the app for contextual appropriateness
3. Consolidate the 3 duplicate `_getAgentIcon()` functions (in `chat_input.dart`, `chat_tab.dart`, `agents_tab.dart`) into a single shared utility in `core/constants.dart`

### Agent icon mapping (unified)
| Agent ID | Icon | Rationale |
|----------|------|-----------|
| coder | `Icons.code` | Code symbol |
| debugger | `Icons.bug_report` | Bug icon |
| planner | `Icons.account_tree` | Tree/plan structure |
| analyst | `Icons.analytics` | Chart icon |
| chat | `Icons.chat` | Chat bubble |
| committer | `Icons.source` (was history/cloud_upload) | Git source control |
| scheduler | `Icons.schedule` (was event_note) | Clock/schedule |
| dispatcher | `Icons.route` | Routing |
| default | `Icons.smart_toy` | Generic AI |

### Files affected
- `ui/flutter_ui/pubspec.yaml` — set `uses-material-design: true`
- `ui/flutter_ui/lib/core/constants.dart` — add unified `getAgentIcon()` function
- `ui/flutter_ui/lib/features/chat/chat_input.dart` — remove local `_getAgentIcon()`, use shared
- `ui/flutter_ui/lib/features/chat/chat_tab.dart` — remove local `_getAgentIcon()`, use shared
- `ui/flutter_ui/lib/features/agents/agents_tab.dart` — remove local `_getAgentIcon()`, use shared

## 9. Configuration

### New config fields in `client.json5`
```json5
{
  keybindings: {
    leader_key: "cmd+x",    // "cmd+x" | "ctrl+x" | "alt+x"
    double_enter: "steer",  // "steer" | "interrupt" | "preempt"
  },
}
```

### `meept config` updates
Add `keybindings` as a new section in the interactive TUI config editor (`internal/config/editor.go` or equivalent). The section exposes:
- `leader_key`: string selector (cmd+x, ctrl+x, alt+x)
- `double_enter`: string selector (steer, interrupt, preempt)

### Files affected
- `config/client.json5` — add `keybindings` to template
- `internal/config/schema.go` — add `Keybindings` struct with `LeaderKey` and `DoubleEnter` fields
- `internal/config/editor.go` — add keybindings section to TUI config editor
- `ui/flutter_ui/lib/services/storage_service.dart` — read keybindings from client config

## 10. Drawer Panels

Five panels in the drawer overlay, each showing a snapshot of data from daemon APIs:

| Panel | Source | Content |
|-------|--------|---------|
| Status | `/daemon/status` | Connection state, uptime, active agents, pending tasks |
| Agent Activity | `/agents` | Active agents with name, state icon, iteration progress, current tool calls |
| Tasks | `/tasks` | Up to 4 tasks with status icon, title, agent, progress bar, step description |
| Recent Memory | `/memory/recent` | Up to 5 memory items with type badge and preview |
| Metrics | `/metrics/live` | Queue depth, workers busy, agents active, current values |

Each panel is a tab in the drawer. Data is fetched on drawer open and refreshed while open.

### Files affected
- New: `ui/flutter_ui/lib/features/drawer/drawer_overlay.dart`
- New: `ui/flutter_ui/lib/features/drawer/panels/status_panel.dart`
- New: `ui/flutter_ui/lib/features/drawer/panels/agent_activity_panel.dart`
- New: `ui/flutter_ui/lib/features/drawer/panels/tasks_panel.dart`
- New: `ui/flutter_ui/lib/features/drawer/panels/recent_memory_panel.dart`
- New: `ui/flutter_ui/lib/features/drawer/panels/metrics_panel.dart`

## Implementation Order

Phased to deliver value incrementally:

1. **Phase 1: Icons fix** — immediate visual improvement, unblocks everything
2. **Phase 2: Markdown rendering** — major UX improvement for chat readability
3. **Phase 3: Layout restructure** — status bar removal, toolbar, orange header, tools dropdown
4. **Phase 4: Input behavior** — auto-expanding input, paste compression, enter key semantics
5. **Phase 5: Slash commands** — autocomplete, command registry, execution
6. **Phase 6: Keyboard shortcuts** — leader key system, all bindings
7. **Phase 7: Drawer overlay** — panels for Status, Agent Activity, Tasks, Memory, Metrics
8. **Phase 8: Configuration** — new config fields, `meept config` updates, TUI double-enter
