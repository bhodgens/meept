# Plan: Project Hardening — 5 Post-Review Fixes

## Goal

Address 5 issues identified during the project-scoping systemic review:
1. Worktree/branch per session (feature gap)
2. buildTerminateResponse dumps raw JSON (defense-in-depth)
3. WS broadcast has no session filter (defense-in-depth)
4. Stale loop eviction doesn't clean up resources (minor leak)
5. Git probes on every system prompt build (latency)

## Architecture Overview

All 5 fixes are in the Go daemon + Flutter GUI. They are independent of each
other except that items 2 and 5 both modify `internal/agent/loop.go` and must
be serialized. Items 2, 3, 4 are small (<30 lines each). Item 5 is medium
(~50 lines). Item 1 (worktree) is a cross-cutting feature (~500 lines, 8+ files).

## Interface Contracts

### Worktree (Item 1)
- `Session` struct gains `WorktreeID string` field (`session.go`)
- `project.ProjectManager` gains `CreateSessionWorktree(ctx, projectID, sessionID) (*Worktree, error)`
- New RPC: `project.worktree.create` and `project.worktree.remove`
- New TUI command: `/worktree` — creates a session-scoped worktree
- `sessionLoop()` uses `sess.WorktreePath` (if set) instead of `sess.ProjectPath`

### buildTerminateResponse (Item 2)
- No new types. Function signature unchanged. Only changes how non-string
  results are formatted: wraps in a natural-language template instead of raw json.Marshal.

### WS Session Filter (Item 3)
- `handleWSEvent` gains a session filter matching `handleWSProgress`'s `ShouldSendProgress` pattern.
- No new types. Uses the existing `WebSocketConnection.SessionID` field.

### Loop Cleanup (Item 4)
- `AgentLoop` gains `Close()` method that cancels subscriptions and stops goroutines.
- `Manager.GetOrCreateWired` calls `loop.Close()` before `delete(m.loops, sessionID)`.

### Git Probe Cache (Item 5)
- `resolveProjectInfo` caches results with a 5-second TTL.
- Cache is a simple `sync.Map` with timestamp. No new types exposed.

## Coding Conventions

- Go: follow CLAUDE.md (nil-guard setters, no mutex across I/O, lowercase UI text)
- Flutter: kIsWeb guards, no top-level dart:io, Riverpod patterns
- All new RPC methods must be registered in `RegisterProjectMethods`
- All new TUI commands must be in `command_handler.go` switch statement

## Commit Policy

Only the orchestrator commits. Implementation agents must NOT commit.

## Child Index

| # | Document | Est. Context | Dependencies | Concurrency Group |
|---|----------|-------------|--------------|-------------------|
| 1 | `01-loop-fixes/orchestrator.md` | N/A (branch) | — | — |
| 1a | `01-loop-fixes/01-terminate-json.md` | ~25K | None | A (serialized with 1b) |
| 1b | `01-loop-fixes/02-git-probe-cache.md` | ~35K | 1a (both touch loop.go) | A (after 1a) |
| 2 | `02-ws-session-filter.md` | ~20K | None | B (parallel) |
| 3 | `03-loop-eviction-cleanup.md` | ~25K | None | B (parallel) |
| 4 | `04-worktree-per-session/orchestrator.md` | N/A (branch) | — | — |
| 4a | `04-worktree-per-session/01-go-backend.md` | ~60K | None | C |
| 4b | `04-worktree-per-session/02-tui-flutter.md` | ~50K | 4a (needs RPC) | C (after 4a) |

## Dispatch Protocol

### Wave 1 (parallel, concurrency groups B + C-start)
- Dispatch `02-ws-session-filter.md` and `03-loop-eviction-cleanup.md` in parallel
- Dispatch `01-loop-fixes/orchestrator.md` (serializes its own children)
- Dispatch `04-worktree-per-session/orchestrator.md` (dispatches 4a first, then 4b)

### Wave 2 (after wave 1 returns)
- Review all implementations in-session
- Commit each leaf after review passes
- Run full test suite after all leaves committed

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01-loop-fixes/01-terminate-json | COMPLETE 2026-07-29 100% | formatToolResult replaces raw json.Marshal |
| 01-loop-fixes/02-git-probe-cache | COMPLETE 2026-07-29 100% | 5s TTL, sync.RWMutex, per-dir keying |
| 02-ws-session-filter | COMPLETE 2026-07-29 100% | ShouldSendProgress filter on broadcasts |
| 03-loop-eviction-cleanup | COMPLETE 2026-07-29 100% | Close() + mutex-safe eviction |
| 04-worktree-per-session/01-go-backend | COMPLETE 2026-07-29 100% | session+store+RPC+TUI command |
| 04-worktree-per-session/02-tui-flutter | COMPLETE 2026-07-29 100% | slash cmd+status bar+sidebar |

## Review Checklist

- [ ] All 5 fixes implemented and tested
- [ ] `go build ./...` passes
- [ ] `go test ./...` passes (full suite)
- [ ] `flutter analyze lib/` — no new errors
- [ ] No raw JSON in any tool response path
- [ ] No subprocess calls on cached git probes
- [ ] No resource leaks on loop eviction
- [ ] Worktree creation and removal works end-to-end

## Integration Test Plan

After all leaves are committed:
1. `go build ./...` — full compilation
2. `go test ./internal/agent/... ./internal/rpc/... ./internal/tools/... ./internal/comm/... ./internal/project/...` — all packages
3. `cd ui/flutter_ui && flutter analyze lib/` — no new errors
4. Manual: create a worktree via `/worktree`, verify the session switches to it
5. Manual: verify `project_info` tool returns cached git data within 5s
6. Manual: verify terminating tools (platform_agents) return formatted text, not JSON
