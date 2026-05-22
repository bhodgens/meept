# Phase 6: TUI/SharedClient Code Duplication Audit

**Date:** 2026-05-21
**Scope:** `internal/tui/` vs `internal/sharedclient/`

## Executive Summary

After the autocomplete migration, Phase 6 is approximately **35-40% complete**. The shared library (`internal/sharedclient/`) already holds pure data/logic types that were previously duplicated. This audit identifies that most remaining duplication is **inherently framework-specific** (Bubble Tea vs. termbox-go) and should not be migrated.

### Key Finding

The architecture is sound: **pure data/logic belongs in sharedclient**, while **UI framework integration belongs in each client's package**. The only remaining shareable item is a small utility function (`formatRelativeTime`).

---

## Migration Target Table

### Already Shared (No action needed)

| # | Target | Location (shared) | TUI Equivalent | Status |
|---|--------|-------------------|----------------|--------|
| 1 | Slash command parsing | `sharedclient/slash.go` | `tui/slash.go` (re-exports) | COMPLETE |
| 2 | Slash autocomplete data | `sharedclient/slash_autocomplete.go` | `tui/slash_autocomplete.go` (wraps data layer) | COMPLETE |
| 3 | History management | `sharedclient/history.go` | None (TUI uses `bubbles/textarea` built-in history) | COMPLETE |
| 4 | Per-session history | `sharedclient/session_history.go` | None (TUI tracks history in chat model directly) | COMPLETE |
| 5 | Session management | `sharedclient/session.go` | `tui/modal.go` (has its own session picker with RPC calls) | COMPLETE |
| 6 | Color constants (termbox) | `sharedclient/colors.go` | `tui/styles.go` (lipgloss palette) | FRAMEWORK-SPECIFIC (different color systems) |
| 7 | Prompt rendering | `sharedclient/prompt.go` | None (TUI renders prompt differently via Bubble Tea) | FRAMEWORK-SPECIFIC |
| 8 | Modal (termbox) | `sharedclient/menus/modal.go` | `tui/modal.go` (~1200 lines) | FRAMEWORK-SPECIFIC (different UI frameworks) |
| 9 | Command palette | `sharedclient/menus/palette.go` | `tui/modal.go` `CommandPaletteModal()` | FRAMEWORK-SPECIFIC |
| 10 | Session menu | `sharedclient/menus/session.go` | `tui/modal.go` `SessionPickerModal` | FRAMEWORK-SPECIFIC |
| 11 | Tasks menu | `sharedclient/menus/tasks.go` | `tui/models/tasks.go` | Framework-specific data, but see below |
| 12 | Queue menu | `sharedclient/menus/queue.go` | `tui/models/queue.go` | Framework-specific data, but see below |
| 13 | Memory menu | `sharedclient/menus/memory.go` | `tui/models/memory.go` | Framework-specific data, but see below |
| 14 | Chat menu | `sharedclient/menus/chat.go` | `tui/modal.go` `ConfirmModal` + chat view | Framework-specific |

### Shareable (Low effort, low impact)

| # | Target | Shared Location | TUI Location | Effort | Priority | Notes |
|---|--------|----------------|--------------|--------|----------|-------|
| 15 | `formatRelativeTime` | (new: `sharedclient/time.go`) | `tui/modal.go:1169-1193` | **Low** | **Low** | One 25-line pure function. Could move to sharedclient and re-export. Minimal lines saved. |
| 16 | `config.go` config structs | `sharedclient/config.go` | `tui/config.go` (201 lines) | **Medium** | **Low** | ClientConfig, KeybindingsConfig, SessionConfig, VimConfig, RenderingConfig, SidebarConfig. TUI uses them for Bubble Tea rendering config. Sharedclient doesn't currently hold these. Worth considering if another termbox client or external consumers would benefit, but no clear demand. |
| 17 | `resolveConnection` (conn wiring) | (new: `sharedclient/connection.go`) | `tui/connection.go` (162 lines) | **Medium** | **Low** | `DaemonClient` interface, `ConnectionConfig`, `RetryConfig`, `resolveConnection`, `NewDaemonClient()`. This is used by CLI commands, not by the Bubble Tea TUI (which connects via message bus). No clear shareability benefit. |
| 18 | `sortStrings` | `sharedclient/slash.go:136` | `tui/slash.go:34-40` (redundant local copy) | **Low** | **Low** | TUI has its own `sortStrings` local function kept for backward compat. Could remove it and use `sharedclient.SortStrings`. ~7 lines saved. |

### Framework-Specific (Do not migrate)

| # | Target | Reason |
|---|--------|--------|
| 19 | All modal UI (`tui/modal.go`) | ~1200 lines of Bubble Tea-specific rendering with lipgloss, `tea.Msg`, `tea.Cmd`, mouse handling. Terminbox menus use `termbox-go` primitives. Different rendering paradigms with no shared benefit. |
| 20 | TUI styles (`tui/styles.go`) | 315 lines of lipgloss style definitions. Sharedclient uses raw termbox attribute constants. |
| 21 | TUI models (`mui/models/chat.go`) | Chat view uses `bubbles/textarea`, `bubbles/viewport`, Bubble Tea message types. Completely different widget system. |
| 22 | TUI models (`tui/models/tasks.go`) | Tasks view uses lipgloss tables, Bubble Tea viewport. Separate termbox task display. |
| 23 | TUI models (`tui/models/queue.go`) | Queue view uses lipgloss table rendering. |
| 24 | TUI models (`tui/models/memory.go`) | Memory view uses lipgloss table rendering. |
| 25 | Vim mode (`tui/vim/`) | Vim mode keys integrate with Bubble Tea's `bubblezone` routing. |
| 26 | Syntax/markdown rendering (`tui/render/`) | Bubble Tea specific rendering pipeline. |
| 27 | Visualization (`tui/viz/`) | Bubble Tea canvas/diagram components. |
| 28 | Progress state (`tui/progress.go`) | Bubble Tea text rendering with emoji. |
| 29 | Fuzzy finder (`tui/modal.go` `FuzzyFinderModal`) | Bubble Tea modal, mouse handling, `tea.Cmd`. |
| 30 | Branch picker (`tui/modal.go` `BranchPickerModal`) | Bubble Tea modal with scroll support. |
| 31 | Sidebar (`tui/sidebar.go`) | Bubble Tea layout with `bubblezone`. |
| 32 | App orchestrator (`tui/app.go`) | Bubble Tea `Model` interface implementation. |

---

## What Should BE Migrated (Recommendations)

### Priority 1: Eliminate the local `sortStrings` copy

**File:** `/Users/caimlas/git/meept/internal/tui/slash.go`
**Change:** Remove the 7-line local `sortStrings` function. It shadows what is already exported as `sharedclient.SortStrings`.

```go
// Remove this function:
func sortStrings(s []string) { ... }

// Usage in slash_autocomplete.go already imports sharedclient,
// so just change the call to sharedclient.SortStrings if needed.
```

**Effort:** Trivial (~2 min)
**Lines saved:** 7
**Risk:** None

### Priority 2 (optional): `formatRelativeTime` as a shared utility

**File to create:** `/Users/caimlas/git/meept/internal/sharedclient/time.go`
**Current location:** `/Users/caimlas/git/meept/internal/tui/modal.go:1169-1193`

This is a pure function with no UI dependencies. It could be moved to sharedclient and called from the TUI.

**Effort:** Low (~5 min: extract + update import + test)
**Lines saved:** 25
**Risk:** Minimal. Function is self-contained.

**Decision factor:** Only worth doing if there are other consumers or if it would be needed by a third client in the future. For a single-use function in one place, duplication may be acceptable.

---

## What Should NOT Be Migrated (Justification)

### Config structs (`ClientConfig`, `KeybindingsConfig`, etc.)

The sharedclient package currently has no config type definitions. TUI's `config.go` defines config structs that are used throughout the Bubble Tea app (keybinding resolution, session settings, vim mode, sidebar preferences). These are **TUI-specific concerns** -- the termbox-lite client has its own separate config handling. While the struct shapes overlap, their usage patterns differ enough that abstraction overhead would exceed benefits.

**Exception:** If the daemon's `internal/config/schema.go` became the single source of truth for all configs, TUI config could be generated from or reference those. But that's a much larger refactoring with diminishing returns.

### Connection/Wiring (`connection.go`)

TUI's `DaemonClient` interface and `NewDaemonClient()` are used by the **RPC subsystem**, not by the Bubble Tea TUI itself. The TUI connects via the message bus (`internal/bus/`). The sharedclient package already has `SessionClient` as an interface, suggesting a different approach. Merging these would require reconciling `transport.Client` with the `DaemonClient` interface.

### All UI Components

The Bubble Tea + lipgloss vs. termbox-go split is the fundamental architectural boundary. Each modal, model, and renderer is tightly coupled to its framework's types (`tea.Msg`, `tea.Cmd`, `tea.KeyMsg`, `termbox.Cell`, etc.). Any attempt to abstract the data layer would create an interface whose overhead exceeds the duplicated code (typically 50-200 lines of rendering per component).

---

## Phase 6 Completion Estimate

| Category | Target count | Complete | Shareable remaining | Framework-specific |
|----------|-------------|----------|-------------------|-------------------|
| Slash commands | 2 | 2 | 0 | 0 |
| History | 2 | 2 | 0 | 0 |
| Sessions | 2 | 2 | 0 | 0 |
| Autocomplete | 2 | 2 | 0 | 0 |
| Colors | 1 | 1 | 0 | 0 |
| Modals/Menu | 8 | 0 | 0 | 8 |
| Config | 1 | 0 | 0.5 (partial) | 0.5 |
| Utilities | 2 | 0 | 1 | 0 |
| **Total** | **22** | **9** | **2** | **8** |

**Current completion: 46% (9/22 targets)**

If the 2 remaining shareable items are migrated: **55% (11/22)**

### Updated completion percentage by line count

| Metric | Value |
|--------|-------|
| Total duplicate-prone lines in TUI | ~7,500 |
| Lines already shared | ~600 (slash, history, session, autocomplete, colors) |
| Lines shareable with low effort | ~67 (sortStrings + formatRelativeTime) |
| **Remaining TUI lines unique per-framework** | **~6,833** |
| **Actual completion: ~9%** of TUI lines are shared code |

**Note:** The "46% completion" above counts migration *targets*, not lines. By line count, only ~9% of the TUI code is actually shared. This is because the large modal/model/sidebar code (the majority of TUI lines) is framework-specific by design.

---

## Architecture Assessment

The current architecture at `/Users/caimlas/git/meept/internal/sharedclient/` is correct:

```
internal/sharedclient/       -- pure data/logic, framework-agnostic
    /menus/                  -- termbox-specific menu implementations (liteclient only)
internal/comm/http/          -- HTTP API service layer
internal/tui/                -- Bubble Tea UI (meept CLI TUI)
cmd/meept-lite/              -- termbox UI (meept-lite)
```

**Sharedclient** now provides:
- Slash parsing (pure logic) -- shared via re-export
- Slash autocomplete (pure logic) -- shared via composition
- History management (pure logic) -- available for use
- Session management (pure logic with interface) -- used by liteclient, TUI has its own UI

**The diminishing returns are significant:** After extracting the pure logic types, the remaining duplication is in the UI layer where Bubble Tea and termbox-go have fundamentally different APIs. This is the correct architectural boundary.

---

## Final Recommendations

1. **Do Priority 1 now:** Remove the local `sortStrings` copy in `tui/slash.go`. It's trivial and the only remaining obvious bug (local shadow of shared export).

2. **Defer `formatRelativeTime`:** Only move if a second consumer appears. Don't create sharedclient/time.go yet.

3. **Accept framework boundary:** The 6,800+ lines of TUI-specific Bubble Tea code should remain in `internal/tui/`. Attempting to extract shared abstractions from modal/model/sidebar code would add interface overhead without real reuse benefit.

4. **Document the boundary:** The `meept-lite-phase6-status.md` file already explains why UI components remain separate. This audit confirms that assessment.

5. **Phase 6 is effectively complete.** No further migration targets offer meaningful benefit.
