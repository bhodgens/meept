# Archived Plans Remediation - Meept Client Features

**Date:** 2026-04-24
**Scope:** Features/functionality planned for `meept` (the client) only
**Source:** Analysis of all plans in `/docs/plans/archive/`

---

## Executive Summary

This document isolates remediation items specific to the **meept client** (CLI, TUI, menubar app) from the broader daemon/server-side plans. The client includes:

- `cmd/meept/` - CLI application
- `internal/tui/` - Terminal UI (Bubbletea v2)
- `internal/lite/` - Lightweight TUI
- `menubar/` - macOS SwiftUI MenuBar app

| Category | Count |
|----------|-------|
| TUI Features To Implement | 5 |
| CLI Commands To Implement | 2 |
| Client-Side Tests To Add | 1 |
| Configuration/UI To Fix | 2 |
| Items Removed/Omitted | 11 |

---

## TUI Feature Gaps

### From plan-tui-agent-extension.md

#### 1. Task Lineage View + Task/Subtask Tree + Message Threading
**Status:** TO BE IMPLEMENTED

**Decision:** Combine these three features into a unified task view enhancement:

1. **Task Lineage View** (originally #1) - Tree-view of task inheritance
2. **Task/Subtask Tree** (originally #7 - Enhanced Tasks View) - Tasks with indented subtasks, collapsible via left/right keys
3. **Message Threading** (originally #4) - Visual grouping by conversation turn

**Implementation Approach:**
- Tasks display with subtasks indented underneath
- Left/right keys collapse/expand task trees
- Right pane shows detailed context including:
  - Memory context (inherited memories, explicit refs, context queries, created memories)
  - Models used for the task
  - Error rates and statistics
  - Linked active tasks with verified progression percentage (originally #9 - Session Header Enhancement)

**What's Needed:**
- `TaskLineage` type definition
- Tree rendering logic with expand/collapse support
- Right pane detailed view with stats, models, error rates
- Linked task tracking with progression percentage display

#### 2. Fuzzy Finder (#6)
**Status:** TO BE IMPLEMENTED (revised scope)

**Decision:** Keyword fuzzy match (not full fuzzy algorithm) with unified pane layout.

**Implementation Approach:**
- Accessed via `cmd-x f` keybinding
- Entry must be added to `cmd-x` menu
- **Left pane:** Sessions with tasks nested underneath
- **Right pane:** Specific context for selected item
- Simple keyword matching (substring match)

**What's Needed:**
- `cmd-x` menu entry for "Find"
- Keybinding handler for `cmd-x f`
- Split-pane fuzzy finder modal
- Keyword matching logic for sessions and tasks

#### 3. Bus Event Stream Client (#10)
**Status:** ALREADY IMPLEMENTED

**Note:** Server-side handlers were found to be already implemented in `internal/rpc/proxy.go`:
- `handleBusSubscribe()` at line 238
- `handleBusPoll()` at line 325
- `handleBusUnsubscribe()` at line ~400

Client-side in `internal/tui/events.go` is fully wired. No action needed.

---

## TUI Features REMOVED (No Implementation Needed)

The following features were originally planned but are **no longer required**:

| # | Feature | Reason |
|---|---------|--------|
| 2 | Responsive Layout | Removed - current layout is sufficient |
| 3 | Quick Actions Bar | Removed - status bar is sufficient |
| 5 | Notification System | Removed for now - tab flash indicator works |
| 8 | Task Detail Modal Memory Context | Merged into #1 (right pane implementation) |
| 9 | Session Header Enhancement | Merged into #1 (linked tasks with % in right pane) |

---

## CLI Command Gaps

### From plan-clawskills.md

#### 1. ClawSkills Full Scope Review
**Status:** TO BE IMPLEMENTED

**Decision:** Fix `inspect` command AND review full clawskills integration scope.

**What's Missing:**
- `meept clawskills inspect <slug>` - View LOCAL installed skill detail (currently only `info` for remote exists)
- Full clawskills integration review needed

**Action:** Review entire clawskills implementation to identify gaps in:
- Daemon-side loading
- Namespace isolation (`claw:` prefix)
- Runtime tool restrictions
- Risk level enforcement

---

## CLI Commands REMOVED (No Implementation Needed)

| # | Command | Reason |
|---|---------|--------|
| 1 | Calendar Commands | Omit - calendar integration not prioritized |
| 3 | Self-Improve RPC Wiring | Likely overlaps with Q agent feature; functionality unclear |
| 4 | Validation/Watchdog CLI | From plan-agent-validation-watchdog which is only 25% complete |

---

## Test Gaps (Client-Side)

### From plan-bubbletea-v2-migration.md

#### 1. Teatest Integration Tests
**Status:** CREATE GITHUB ISSUE

**Issue:** `internal/tui/app_test.go` lines 428-429:
```go
// TODO: Re-enable teatest integration tests once a
// bubbletea v2-compatible teatest package is available.
```

**Decision:** Track as GitHub issue for when teatest becomes v2-compatible.

---

### From plan-tui-agent-extension.md

#### 2. Bus Event Stream Tests
**Status:** KEEP

**What's Missing:**
- Event subscription tests
- Event rendering tests
- Metrics collector tests

---

### From plan-clawskills.md

#### 3. ClawSkills Tests
**Status:** REMOVE OPENCLAW - CREATE GITHUB ISSUE

**Decision:** Completely remove OpenClaw compatibility from codebase.

**Action Items:**
1. Create GitHub issue for removing OpenClaw compatibility
2. Do NOT implement clawskills tests until OpenClaw is removed

---

## Configuration/UI Gaps

### From plan-bubbletea-v2-migration.md

#### 1. Clipboard Integration
**Status:** KEEP CURRENT IMPLEMENTATION

**Decision:** Current OSC52 implementation works better than `tea.SetClipboard`. No change needed.

#### 2. Dead Code Cleanup
**Status:** KEEP

**Action:** Remove `setTerminalTitle()` function (line 1155 in `app.go`) - now unused after v2 migration.

---

## Configuration/UI REMOVED (No Implementation Needed)

| # | Feature | Reason |
|---|---------|--------|
| 3 | Layout Configuration | Skip/remove |
| 4 | Namespace Isolation | Remove OpenClaw-related implementation |
| 5 | TUI Approval Panel | Remove (self-improve RPC wiring also removed) |

---

## MenuBar App Gaps

### From plan-go-refactoring.md

#### 1. Architecture Discrepancy
**Status:** NO ACTION NEEDED

**Note:** Plan documented Rust/Tauri, actual is SwiftUI. This was a plan inaccuracy. No remediation needed.

---

## Summary: Action Items

### TUI Implementation Required

| Feature | Description | Priority |
|---------|-------------|----------|
| Task View Enhancement | Combined lineage + subtask tree + message threading + right pane stats | High |
| Fuzzy Finder | Keyword match, `cmd-x f` binding, split-pane | Medium |
| Bus Event Server-Side | Implement `bus.subscribe` / `bus.poll` handlers | Medium |

### CLI Implementation Required

| Feature | Description | Priority |
|---------|-------------|----------|
| ClawSkills Inspect | Local skill detail viewer | High |
| ClawSkills Integration Review | Full scope review and gap analysis | High |

### GitHub Issues To Create

| Issue | Description |
|-------|-------------|
| Teatest Integration Tests | Track bubbletea v2 compatibility for teatest |
| Remove OpenClaw Compatibility | Strip all OpenClaw-related code from clawskills |

### Files To Modify

| File | Change |
|------|--------|
| `internal/tui/app.go` | Remove `setTerminalTitle()` function |
| `internal/tui/models/tasks.go` | Add task/subtask tree, right pane with stats, linked tasks with % |
| `internal/tui/modal.go` | Add `cmd-x f` menu entry and handler |
| `cmd/meept/clawskills.go` | Add `inspect` subcommand |

### Files To Review

| File | Purpose |
|------|---------|
| `internal/clawskills/` | Full review for OpenClaw removal |

---

## Summary by Plan Source (UPDATED)

| Plan | Original Completeness | Revised Status |
|------|----------------------|----------------|
| plan-tui-agent-extension.md | 75% | Features consolidated; ~60% revised scope |
| plan-bubbletea-v2-migration.md | 95% | 97% (only dead code cleanup remaining) |
| plan-calendar-integration.md | 5% | Omitted |
| plan-clawskills.md | 70% | OpenClaw removal needed first |
| plan-selfimprove-integration.md | 55% | Omitted (Q agent overlap) |
| plan-agent-validation-watchdog.md | 25% | Omitted |

**Overall Client-Side Completeness: ~85%** (after removing omitted items)
