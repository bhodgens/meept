# Agent Reference: UI, Flutter, and Feature Documentation Rules

Moved from the root `AGENTS.md` to keep the root under the size cap. The root
keeps one-line hard rules and points here. Update both in the same commit.

## UI Conventions

- **All UI text must be lowercase** (e.g., "switch" not "Switch", "ok" not "OK")
- For TUI, use bubblezone for positioning
- Default to clickable elements for context switching
- **TUI and Flutter GUI features must be kept at parity.** When a feature is
  added or changed in one surface, the other surface gets the same capability.
  This includes: status bar elements, command palette items, keyboard shortcuts
  (prefer identical keys across surfaces — e.g., `Ctrl+V` for verbosity on all
  platforms, not `Cmd+V` on mac), session/agent/tab semantics (e.g., archive vs
  delete), and tab affordances. Document surface-specific deviations explicitly
  with a justification.
- **TUI tables are sized on both axes.** `bubbles/table` owns a viewport that
  starts at width 0, and a width-0 viewport renders no lines: a table given only
  `SetHeight` draws its header over an empty body while cursor navigation still
  works. Size every table through `internal/tui/tableutil.Size` and write rows
  through `tableutil.SetRows`, which normalizes each row to the current column
  count (a row longer than the column list panics inside `bubbles/table`). A
  column change clears the rows first (`SetColumns` re-renders them) and
  repopulates from cache afterwards. See `docs/workflows/tui.md`.

## Flutter Multi-Platform (Web + Desktop)

When modifying Flutter UI code, ensure web compatibility alongside desktop
(macOS/Linux/Windows):

- **Avoid top-level `dart:io` imports in shared code** — use `kIsWeb` guards
  or conditional imports
- **Platform detection:** use `bool.fromEnvironment('dart.library.io')` for
  compile-time checks, `Platform.isMacOS` only in `!kIsWeb` guarded code
- **File I/O:** wrap in `if (kIsWeb) return;` guards; web uses file pickers,
  not direct paths
- **Platform abstraction:** for shared platform abstractions, use a singleton
  service pattern (e.g., `PlatformService`) that provides safe null/default
  returns on web

**See also:** `ui/flutter_ui/lib/core/platform/platform_service.dart` and
`platform_native_helpers.dart` for the platform abstraction layer pattern.

## Configuration

All config is JSON5. Full paths and defaults: `docs/agents/configuration.md`,
`docs/configuration/`. Hard rules:

- Home: `$MEEPT_HOME` else `~/.meept`. Set it for daemon, CLI, and
  `make sync-config`.
- Main config: `~/.meept/meept.json5`; log level is a config field, not an env var.
- Kanban and models ship default-on in `config/meept.json5` and
  `config/models.json5`; see `docs/agents/configuration.md`.

## Static Analyzers

Analyzer and audit script inventory with run commands:
`docs/agents/static-analyzers.md`. Run them via `make lint-ci`.

## Feature Documentation Requirements

All code changes to feature implementations must have corresponding
documentation updates.

**Documentation locations:**
- `docs/workflows/` — Feature specifications
- `docs/concepts/` — Architecture
- `docs/reference/` — CLI, API, tools
- `docs/research/` — living research maps (harness techniques)
- `docs/generated/` — Auto-generated connectivity graphs (do not edit)

**Feature mapping:** `internal/<pkg>/` → `docs/workflows/<pkg>.md`

