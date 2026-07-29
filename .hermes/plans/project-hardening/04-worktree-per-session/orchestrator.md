# Orchestrator: Worktree/Branch Per Session

## Scope

Wire the existing `project/worktree.go` infrastructure into the session lifecycle.
This enables each session to work on an isolated git worktree with a session-scoped branch.

## Interface Contracts

### Session struct changes
```go
// internal/session/session.go
type Session struct {
    // ... existing fields ...
    WorktreeID   string `json:"worktree_id,omitempty"`
    WorktreePath string `json:"worktree_path,omitempty"`
}
```

### RPC methods
```
project.worktree.create — params: {session_id, project_id} → {worktree_id, path, branch}
project.worktree.remove — params: {session_id} → {status: "removed"}
```

### TUI command
```
/worktree          — create a session-scoped worktree (uses current session's project)
/worktree remove   — remove the session's worktree and revert to main project path
```

### sessionLoop() integration
```go
// internal/agent/handler.go sessionLoop()
// Use WorktreePath if set, otherwise ProjectPath
workingPath := sess.ProjectPath
if sess.WorktreePath != "" {
    workingPath = sess.WorktreePath
}
loop, err := h.loopManager.GetOrCreateWired(conversationID, workingPath, h.loop)
```

### Store changes
- `session.Store.SetWorktree(sessionID, worktreeID, worktreePath string) error`
- SQLite UPDATE: `UPDATE sessions SET worktree_id = ?, worktree_path = ? WHERE id = ?`

## Child Index

| # | Document | Est. Context | Dependencies |
|---|----------|-------------|--------------|
| 4a | `01-go-backend.md` | ~60K | None |
| 4b | `02-tui-flutter.md` | ~50K | 4a (needs RPC + store methods) |

## Dispatch Protocol

1. Dispatch leaf 4a (Go backend). Wait for completion + review.
2. Dispatch leaf 4b (TUI + Flutter). Wait for completion + review.
3. Commit both.

## Review Checklist

- [ ] `CreateWorktree` is called from an RPC handler, not just available on the manager
- [ ] Session struct has WorktreeID and WorktreePath fields with JSON tags
- [ ] `SetWorktree` method on session store (nil-guarded)
- [ ] `sessionLoop()` prefers WorktreePath over ProjectPath
- [ ] `/worktree` command in TUI creates worktree + updates session
- [ ] Flutter: `/worktree` slash command or button in chat input
- [ ] Worktree is created with session-scoped branch name (`session/<sessionID-prefix>`)
- [ ] Removing a worktree reverts session to ProjectPath
- [ ] `go build ./...` passes
- [ ] `go test ./internal/agent/... ./internal/rpc/... ./internal/project/...` passes
- [ ] `flutter analyze lib/` — no new errors

## Coding Conventions

- Go: follow CLAUDE.md (nil-guard setters, no mutex across I/O, lowercase UI text)
- Flutter: kIsWeb guards, no top-level dart:io, Riverpod patterns
- New RPC methods registered in `RegisterProjectMethods`

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01-go-backend | COMPLETE 2026-07-29 100% | session+store+RPC+handler+TUI command |
| 02-tui-flutter | COMPLETE 2026-07-29 100% | slash cmd+status bar+sidebar+SDK methods |

## Integration Test Plan

- Create a worktree for a session, verify the agent operates in the worktree path
- Verify two sessions in the same project can work on different branches
- Remove a worktree, verify the session reverts to the project path
