# Plan: Remediate Unused Linter Issues

**Date:** 2026-05-10
**Scope:** 52 unused items found by `golangci-lint` (unused linter)
**Goal:** Remove dead code, wire up partially implemented features, reduce lint noise

## Summary

Investigation of all 52 unused items reveals:
- **39 items** are clear dead code (duplicates, superseded implementations, stub code never wired)
- **2 items** should be wired up (partially implemented features with active plans)
- **11 items** are "wire up or delete" (legitimate functions that need integration or removal)

## Phase 1: Delete Dead Test Helpers

**Files:** `internal/agent/testhelpers_test.go`, `internal/agent/q/agent_designer_test.go`
**Risk:** None (test-only code, no production impact)

| Item | File:Line | Reason |
|------|-----------|--------|
| `testToolRegistry` type + all methods | `testhelpers_test.go:12-51` | Dead duplicate of `PlaceholderToolRegistry` in `executor.go` |
| `mockTool` type + all methods/ctors | `testhelpers_test.go:54-99` | Dead duplicate of exported `MockTool` in `executor.go` |
| `makeAnalysis` func | `q/agent_designer_test.go:12` | Dead helper; tests use `makeAnalysisOnly` instead |

**Action:** Delete the entire `testhelpers_test.go` file. Delete `makeAnalysis` from `agent_designer_test.go`.

---

## Phase 2: Delete Dead Alias Wrappers

**File:** `internal/agent/loop.go` (lines ~2668-2760)
**Risk:** None (`chatWithFailover` calls resolver directly, these wrappers are bypassed)

| Item | File:Line | Reason |
|------|-----------|--------|
| `getAliasName` | `loop.go:2668` | Dead wrapper; `chatWithFailover` calls `resolver.HasAlias()` directly |
| `resolveAliasModel` | `loop.go:2684` | Dead wrapper; calls to `resolver.ResolveForAlias()` are inline |
| `recordAliasFailure` | `loop.go:2697` | Dead wrapper; `chatWithFailover` calls `resolver.RecordAliasFailure()` directly |
| `recordAliasSuccess` | `loop.go:2737` | Dead wrapper; `chatWithFailover` calls `resolver.RecordAliasSuccess()` directly |

**Action:** Delete all four methods.

---

## Phase 3: Delete Dead TUI Code

**Files:** Multiple files under `internal/tui/`
**Risk:** Low (unused rendering/selection code)

| Item | File:Line | Reason |
|------|-----------|--------|
| `getSlashCommands` func | `app.go:20` | Superseded by `SlashAutocomplete` component |
| `generateAutocompletePopup` func | `app.go:1081` | Superseded by `SlashAutocomplete` component |
| `hasMarkdown` field | `models/chat.go:61` | Never wired; markdown controlled at model level |
| `err` field | `models/chat.go:75` | Never assigned or read |
| `lastInputValue` field | `models/chat.go:123` | Superseded by `compressedPastes`/`pasteCounter` |
| `lastInputLines` field | `models/chat.go:124` | Superseded by `compressedPastes`/`pasteCounter` |
| `buildPositionIndex` func | `models/chat_selection.go:22` | Only caller is `messageAtY` (also dead) |
| `messageAtY` func | `models/chat_selection.go:68` | Never called from event handlers |
| `extractInputSelectedText` func | `models/input_selection.go:234` | Mouse tracking exists but consume functions never wired |
| `clearInputSelection` func | `models/input_selection.go:256` | Same as above |
| `hasInputSelection` func | `models/input_selection.go:264` | Same as above |
| `applyInputSelectionHighlight` func | `models/input_selection.go:269` | Same as above |
| `siDiffStyle` var | `selfimprove.go:41` | Diff display never implemented |
| `mu` field | `handlers/task_events.go:42` | Protected now-removed rate-limiting state; also remove `sync` import |

**Action:** Delete all listed items.

---

## Phase 4: Delete Dead Shadow Code

**Files:** `internal/shadow/exporter.go`, `internal/shadow/selector.go`
**Risk:** None (superseded implementations)

| Item | File:Line | Reason |
|------|-----------|--------|
| `hashRecord` func | `exporter.go:448` | Superseded by `textHash()` |
| `hashPair` func | `exporter.go:465` | Superseded by `textHash()` |
| `selectWithinBudget` func | `selector.go:237` | Superseded by `selectWithMMR` |

**Action:** Delete all three functions.

---

## Phase 5: Delete Dead Utility Duplicates

**Files:** Various
**Risk:** None (all have active canonical versions elsewhere)

| Item | File:Line | Reason |
|------|-----------|--------|
| `(*ConfigService).expandPath` | `comm/http/config_service.go:43` | Duplicate of `pathutil.ExpandPath` and `config.expandPath` |
| `expandHomePath` func | `daemon/components.go:2171` | Duplicate; `config.expandPath` is the active version |
| `formatTime` func | `comm/web/handler.go:117` | Never called |
| `ptrTime` func | `comm/web/handler.go:124` | Never called |
| `encodeStepDependsOn` func | `task/step.go:761` | Exact duplicate of `encodeStringSlice` in `store.go:556` |
| `truncateString` func | `task/amendment_handlers.go:261` | Referenced in memory propagation plan but never wired up |
| `ln2` const | `security/taint/patterns.go:444` | Dead constant within `log2()` function |

**Action:** Delete all listed items.

---

## Phase 6: Delete Unused Struct Fields

**Files:** Various
**Risk:** Low (fields never read or written)

| Item | File:Line | Reason |
|------|-----------|--------|
| `lastErr` field | `agent/session_tracker.go:24` | Vestigial; bug fix uses local variable instead |
| `Collector.mu` field | `metrics/collector.go:17` | Never locked; `Store` handles its own concurrency |
| `PeriodicCollector.mu` field | `metrics/collector.go:298` | Never locked |
| `LearningPipeline.trajectories` field | `selfimprove/learning.go:154` | Never initialized or accessed |

**Action:** Delete all listed fields. Remove unused imports if any become dead.

---

## Phase 7: Wire Up or Delete Partially Implemented Features

**Risk:** Medium (requires understanding intended behavior)

| Item | File:Line | Status | Decision |
|------|-----------|--------|----------|
| `fetchStepSummaries` func | `agent/handler.go:770` | Plan exists (compound task ACK), never wired | **Wire up** - call in task completion handler, wire `SetStepStore` in `components.go` |
| `isClickInViewportArea` func | `tui/models/chat.go:2130` | Legitimate guard for mouse selection | **Wire up** - add to mouse event handler in `chat_selection.go` |
| `drawX` func | `tui/viz/robot.go:219` | Visual for `RobotFailed` state | **Wire up** - call in `Draw()` for `RobotFailed` state |
| `drawExclamation` func | `tui/viz/robot.go:234` | Visual for `RobotProblems` state | **Wire up** - call in `Draw()` for `RobotProblems` state |

### Phase 7a: Wire up `fetchStepSummaries`

Plan reference: `docs/superpowers/plans/2026-05-07-improve-compound-task-ack.md`

1. In `internal/daemon/components.go`, find where `ChatHandler` is created and add `chatHandler.SetStepStore(stepStore)`
2. In `internal/agent/handler.go`, find the task completion message path and call `fetchStepSummaries` to populate step summaries

### Phase 7b: Wire up `isClickInViewportArea`

1. In `internal/tui/models/chat_selection.go`, find the mouse event handler
2. Add guard: if `!m.isClickInViewportArea(msg)` return unchanged model

### Phase 7c: Wire up robot visual states

1. In `internal/tui/viz/robot.go`, find the `Draw()` method
2. Add cases for `RobotFailed` -> `drawX()`, `RobotProblems` -> `drawExclamation()`

---

## Execution Strategy

Each phase is independent and can be executed by a separate subagent in parallel:

```
Phase 1 ─┐
Phase 2 ─┤
Phase 3 ─┼─→ Run in parallel (independent files)
Phase 4 ─┤
Phase 5 ─┤
Phase 6 ─┘
Phase 7 ──→ Run after Phases 1-6 (may touch same files, needs care)
```

After all phases:
1. Run `go build ./...` to verify compilation
2. Run `go test ./... -v` to verify tests pass
3. Run `golangci-lint run ./...` to verify unused count drops to 0
4. Commit with message: `chore: remove unused code found by golangci-lint`

## Expected Outcome

- 52 unused items -> 0 (or near-zero if Phase 7 items are wired up and counted as used)
- ~400 lines of dead code removed
- 4 features properly wired up (fetchStepSummaries, isClickInViewportArea, drawX, drawExclamation)
