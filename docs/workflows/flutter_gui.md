# Flutter GUI

## Overview

The Flutter desktop UI (`ui/flutter_ui/`) is the graphical counterpart to the terminal TUI. It targets macOS, linux, and windows from a single dart codebase, communicating with the platform over HTTP + WebSocket. Where the TUI leads on a feature, the Flutter GUI follows, and vice versa — see [tui](tui.md) for the terminal surface and the "ui conventions" section of `CLAUDE.md` for the parity rule.

## Problem

Power users want a keyboard-driven terminal experience; everyone else wants a pointer-friendly window. Maintaining two clients is only sustainable when feature parity is explicit, so neither surface silently regresses.

## Connection

The GUI talks to the daemon over HTTP + WebSocket and verifies the daemon
certificate by SHA-256 fingerprint pinning. The GUI endpoint must equal
`transport.http.addr` in `~/.meept/meept.json5`, including host and port: the
shipped template binds `127.0.0.1:8081`, and the GUI defaults to
`localhost:8081` (see `scripts/gui-daemon-connect.py`). If they differ, the GUI
sits in "connecting..." forever and no request reaches the daemon.

Authentication uses the per-installation dev key when
`transport.http.api_keys` is empty. The key is stored at `$MEEPT_HOME/dev_key`
(`$MEEPT_HOME` defaults to `~/.meept`) and honors the same `MEEPT_HOME`
override as the daemon, so set `MEEPT_HOME` consistently for both processes. A
mismatched home makes the GUI send one key while the daemon expects another,
which surfaces as a hard HTTP 418.

### First-run pairing (distribution builds)

A distributed GUI build carries NO embedded API key (`make build-gui-dist`,
issue #59). On first launch with no stored key:

1. **desktop** (kIsWeb false): the client auto-pairs by reading the local
   `$HOME/.meept/dev_key` file when it is readable (same machine, same
   `MEEPT_HOME`). Web never does this (no filesystem).
2. otherwise the **pairing gate** screen asks for the one-time pairing code
   the daemon prints to its console on startup (valid 15 minutes, single
   use, `crypto/rand`, loopback-only endpoints `GET /api/v1/pair/status` +
   `POST /api/v1/pair/exchange`).

The exchanged key is stored in client storage (keychain on macOS, local
storage on web) and the connection stack re-creates with it. The dev
`make build-gui` / `make devbuild` dart-define path keeps working for
development — it is not the distribution path; the pairing code is never
logged at info level.

## Surfaces

### Status bar

Bottom of every screen (`ui/flutter_ui/lib/widgets/status_bar.dart`). Mirrors `internal/tui/app.go:2236-2289` (`renderStatusBar`). The widget takes `selectedTabIndex` as a constructor param — the rest of the state is read via Riverpod.

**Transient override:** when `statusMessageProvider` is non-null (e.g. "session archived"), the entire bar is replaced by that message and all other segments are hidden (`status_bar.dart:20-23`).

When no transient message is set, segments render in code order, joined by `' · '`:

| # | segment | source | notes |
|---|---------|--------|-------|
| 1 | connection | `connectionStateProvider` + `connectionStatusProvider` | `● connected` or `○ disconnected` followed by the status string |
| 2 | session | `activeSessionProvider` | `session: <title.lowercase>`; **omitted entirely** when no session active or title is empty or `"default"` |
| 3 | keybind hint | derived from `selectedTabIndex` (constructor param) | tab-specific: chat=`^k focus · / cmd · ^f find · ^v verbosity`, sessions=`dbl-click open · ⌫ archive`, other=`j/k navigate · enter select` |
| 4 | project | `currentProjectProvider` | `[name branch*]` (git mode, `*` appended when dirty) or `[local:name]`; **omitted entirely** when project is not active |
| 5 | verbosity | `verbosityProvider` | `verbosity: quiet\|normal\|verbose` — see [verbosity](#verbosity) below |

The status bar is always rendered on the home scaffold; it does not disappear on modals or dialogs. Project names are truncated to 16 grapheme clusters (`chars.length > 16`) using `String.characters` (grapheme-aware) to avoid splitting surrogate pairs.

### Command palette

Triggered by `Cmd+X` on macOS, `Ctrl+X` everywhere else (`ui/flutter_ui/lib/widgets/command_palette.dart`). This matches the TUI `ctl-x` leader key intentionally — keyboard shortcuts stay uniform across surfaces per `CLAUDE.md`.

The palette is a modal overlay with a queryable list. Items:

- chat
- sessions
- plans
- tasks
- agents
- find…
- new session
- edit description
- projects

Keyboard navigation:

| key | action |
|-----|--------|
| `↑` / `↓` | move selection (wraps around) |
| `enter` | activate the highlighted item |
| `esc` | close the palette without activating |

Typing filters the list case-insensitively against item labels.

### Tool panels

Every tool panel opens through the router (`/tools/<name>`, and `/settings` for the settings screen) and renders inside the shared chrome in `ui/flutter_ui/lib/widgets/tool_panel_shell.dart`, so one control set works identically in every panel:

| control | action |
|---------|--------|
| back arrow (tooltip `back (esc)`) | leave the panel, return to chat |
| `esc` | same as the back arrow |

`exitToolPanel` clears `activeToolProvider` first, then pops a pushed detail page or returns to `/`. Key events reach the primary focus first, so an inner `esc` consumer (the find bar, the command palette, a modal) keeps priority and the panel stays open.

The hamburger menu offers `memory`, `changes`, `calendar`, `metrics`, `prompts`, and `settings`. `ui/flutter_ui/lib/features/home/tools_dropdown.dart` maps each name to its route (`toolRoutePaths` / `toolRouteFor`) and both home layouts open menu picks through the same `openToolFromMenu` helper, which is what keeps the exit behaviour identical between the top-tabs and sidebar layouts.

The terminal tool is not part of the GUI: there is no `/tools/terminal` route, no menu entry, no sidebar case, and no layout-4 quick-access item. The daemon-side PTY service and its HTTP endpoints are unchanged, so the TUI terminal surface still works.

### Verbosity

Cycles through three levels (`ui/flutter_ui/lib/providers/verbosity_provider.dart`):

| level | tier | what shows (per `verbosity_provider.dart` docstring) |
|-------|------|------------|
| `quiet` | 0 | only high-level completion events |
| `normal` | 1 | tool results + agent completions (default) |
| `verbose` | 2 | everything including tool starts |

Cycled by **`Ctrl+V` on every platform** — deliberately not `Cmd+V` on macOS so the shortcut matches the TUI verbatim (`CLAUDE.md` UI conventions). The active level is shown in the status bar and gates which `agent_progress` WebSocket events the UI surfaces: events with `tier` greater than the current level are dropped client-side.

**Persistence:** each cycle fire-and-forgets a `PATCH /api/v1/config/client` with `{"chat": {"verbosity": "<name>"}}` so the choice survives app restarts (RFC 7396 merge-patch — unrelated keys in `client.json5` are preserved). The TUI does the equivalent via a direct disk write in its own Ctrl+V handler. UI state updates immediately; persistence failures are swallowed (best-effort — see `verbosity_provider.dart`).

### Agent tiles

The agents tab (`ui/flutter_ui/lib/features/agents/agents_tab.dart`) renders one tile per registered employee using a `SliverGridDelegateWithMaxCrossAxisExtent`:

```dart
maxCrossAxisExtent: 150
crossAxisSpacing:   8
mainAxisSpacing:    8
childAspectRatio:   2.6
```

Each tile is a single row: a 20px agent icon followed by the agent name in `bodySmall` with text ellipsis. Tiles are keyed by `ValueKey(agent.id)` so Riverpod rebuilds are stable across list mutations.

### Session archive UI

Mirrors the TUI's `d` / `shift+d` keys with pointer affordances. See [session.md → archive](session.md#archive) for the full semantics, RPC, and HTTP details. In short:

- default icon: `Icons.archive_outlined` — tap to toggle soft-archive
- archived tiles render at `Opacity(0.5)` (greyed)
- long-press opens a context menu with "delete permanently" (hard `DELETE`)
- double-tap activates the session and routes to chat (`tabActivationProvider = HomeTab.chat`, `context.go('/')`)

### Cached detail providers

A `FutureProvider.family<Session, String>` (`sessionDetailFamily` in `ui/flutter_ui/lib/providers/session_detail.dart`) provides per-id caching for the sessions detail pane.

`SessionsDetailPane` accepts an optional `sessionId`; when provided it consumes `sessionDetailFamily(sessionId)` instead of re-fetching, so navigation from the sessions list into a detail view reuses the cached row data. `HomeScreen` also warms the cache for the `default` session on connect.

### Layout modes

The Flutter GUI supports two layout modes controlled by the `gui.layout` config option in `~/.meept/client.json5`.

**Top-tabs (default):** Traditional horizontal tab bar with tabs for chat, sessions, plans, tasks, and agents. This is the original navigation pattern.

**Sidebar:** Alternative left-sidebar layout featuring:

```
+--------------+------------------------------------------+
|  SESSIONS    |  Header: Session Title                   |
|  ──────────  +──────────────────────────────────────────+
|  [+] sess1   |                                          |
|  ├─ task1    │           Chat Message Area              |
|  │ └─ plan1  │                                          |
|  ▼ sess2     │                                          |
|  ├─ plan1    │                                          |
|  │ └─ task1  ├──────────────────────────────────────────+
|  │ └─ task2  │  [ Chat Input Area ]                     |
+──────────────+──────────────────────────────────────────+
|  [Status Bar]                                           |
+----------------------------------------------------------+
```

The sidebar layout includes:
- **Left sidebar** (220px wide): Expandable/collapsible session tree showing sessions, tasks, and plans with lazy-loaded child nodes
- **Header bar**: Session title with description, connection status indicator, and hamburger menu for tool access
- **Chat area**: Same `ChatTab` component used in top-tabs layout
- **Session info overlay**: Click the [i] icon next to any session to view scoped plans, tasks, and agents in a tabbed dialog

**Switching layouts:**

Change `gui.layout` in `~/.meept/client.json5`:

```json5
{
  "gui": {
    "layout": "sidebar"  // or "toptabs" for the default
  }
}
```

The layout switch takes effect immediately without restarting the app (the router listens for changes to `guiLayoutProvider` and rebuilds the shell). After a full app restart the persisted preference is loaded from storage.

**Component locations:**
- `ui/flutter_ui/lib/features/home/sidebar_home_screen.dart` — Sidebar home screen
- `ui/flutter_ui/lib/features/home/session_info_overlay.dart` — Session info overlay dialog
- `ui/flutter_ui/lib/core/router.dart` — `_LayoutShell` selects layout based on config
- `ui/flutter_ui/lib/providers/preferences_provider.dart` — `GuiLayoutNotifier` for config management

## Formatting

Dart code under `ui/flutter_ui/` is formatted with `dart format` and kept that
way. Two make targets cover it:

| command | what it does |
|---------|--------------|
| `make fmt-gui` | formats the tree in place (`dart format ui/flutter_ui`) |
| `make fmt-check-gui` | check only; exits non-zero when any Dart file needs formatting |

`make fmt-check-gui` is part of `make lint-ci`, so the CI-shaped gate fails on
unformatted Dart. It also runs at commit time: the pre-commit hook
`.githooks/pre-commit-dart-format` (step 17 of `.githooks/pre-commit`) checks
only the Dart files staged in the commit and blocks the commit when one is
unformatted, so an unrelated unformatted file elsewhere does not stop you.

Both targets resolve the dart binary the same way: `dart` on PATH first, then
the dart bundled with the Flutter SDK
(`flutter/bin/cache/dart-sdk/bin/dart`). Override with `DART=/path/to/dart` on
the make command line. When no dart is found the check fails with a clear error
instead of passing silently.

Run `make fmt-gui` before staging Dart changes.

The tree is briefly red while this is adopted: a one-time reformat of the
existing unformatted files is being applied in a separate change, so until it
lands `make fmt-check-gui` and `make lint-ci` fail on those files. After it
lands the tree is clean and both gates stay green.

## Edge cases

- **Grey transcript on session swap:** `ChatMessageList` previously showed "no messages yet" during the brief window between selecting a new session and the messages RPC resolving. The empty-state now checks `chatState.isLoading` before rendering the placeholder, so a loading session never shows a stale empty message.
- **Platform key parity:** `Ctrl+V` (not `Cmd+V`) cycles verbosity on macOS; the TUI and Flutter surfaces use identical shortcuts. Document deviations from this rule explicitly.

---

*Initial version covers status bar, command palette, verbosity, agent tiles, session archive UI, cached detail providers, and layout modes (2026-06 Flutter GUI gap fixes + 2026-07 sidebar layout).*
