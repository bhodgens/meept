# Leaf: Worktree Go Backend

DISPATCH INSTRUCTION: Implement this leaf using TDD. Do NOT commit. Do NOT run git add. Write code, run tests, report results only.

## Parent
`04-worktree-per-session/orchestrator.md`

## Scope
Go daemon: session struct, store methods, RPC handlers, sessionLoop integration.

## Tasks

### Task 1: Add WorktreeID/WorktreePath to Session struct
File: `internal/session/session.go` (~line 76)

Add two fields after `ProjectPath`:
```go
WorktreeID   string `json:"worktree_id,omitempty"`
WorktreePath string `json:"worktree_path,omitempty"`
```

### Task 2: Add SetWorktree to session stores
Files:
- `internal/session/session.go` (MemoryStore) — add method matching SetProject pattern
- `internal/session/store_sqlite.go` — add `SetWorktree(sessionID, worktreeID, worktreePath string) error`

SQLite implementation:
```go
func (s *SQLiteStore) SetWorktree(sessionID, worktreeID, worktreePath string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    _, err := s.db.Exec(
        `UPDATE sessions SET worktree_id = ?, worktree_path = ?, last_activity = ? WHERE id = ?`,
        worktreeID, worktreePath, time.Now().UTC().Format(time.RFC3339), sessionID,
    )
    if err != nil {
        return fmt.Errorf("failed to set worktree for session: %w", err)
    }
    return nil
}
```

Add the method to the `Store` interface in `internal/session/store.go`.

### Task 3: Add RPC handlers
File: `internal/rpc/projects.go`

Add two new handlers registered in `RegisterProjectMethods`:

`project.worktree.create`:
```go
// handleWorktreeCreate handles project.worktree.create RPC calls.
// Creates a session-scoped git worktree and binds it to the session.
func (h *ProjectHandler) handleWorktreeCreate(ctx context.Context, params json.RawMessage) (any, error) {
    var req struct {
        SessionID string `json:"session_id"`
        ProjectID string `json:"project_id"`
    }
    // Parse params
    // Look up project
    // Call pm.CreateWorktree(ctx, projectID, sessionID)  — the method already exists in project/worktree.go
    // Call sessionStore.SetWorktree(sessionID, worktree.ID, worktree.Path)
    // Return {worktree_id, path, branch}
}
```

`project.worktree.remove`:
```go
// handleWorktreeRemove handles project.worktree.remove RPC calls.
// Removes the session's worktree and reverts to the project path.
func (h *ProjectHandler) handleWorktreeRemove(ctx context.Context, params json.RawMessage) (any, error) {
    var req struct {
        SessionID string `json:"session_id"`
    }
    // Look up session, get WorktreeID
    // Call pm.RemoveWorktree(ctx, worktreeID)
    // Call sessionStore.SetWorktree(sessionID, "", "")
    // Return {status: "removed"}
}
```

Read `internal/project/worktree.go` for the existing CreateWorktree/RemoveWorktree API.
Read `internal/project/manager.go` for how to access worktree methods from the ProjectManager.

Register in `RegisterProjectMethods`:
```go
server.RegisterHandler("project.worktree.create", h.handleWorktreeCreate)
server.RegisterHandler("project.worktree.remove", h.handleWorktreeRemove)
```

### Task 4: Update sessionLoop to prefer WorktreePath
File: `internal/agent/handler.go` `sessionLoop()` (~line 1576)

Before calling GetOrCreateWired:
```go
workingPath := sess.ProjectPath
if sess.WorktreePath != "" {
    workingPath = sess.WorktreePath
}
loop, err := h.loopManager.GetOrCreateWired(conversationID, workingPath, h.loop)
```

Do the same in `internal/agent/dispatcher.go` `resolveAgent()`.

### Task 5: Add /worktree command to TUI
File: `internal/tui/command_handler.go`

Add `"worktree"` case to the command switch (~line 211):
```go
case "worktree":
    return h.executeWorktree(cmd.Args)
```

Implement `executeWorktree`:
- No args: call `project.worktree.create` RPC with the current session ID and project ID
- `remove` arg: call `project.worktree.remove` RPC
- Return CommandResult with SetWorktreePath (new flag on CommandResult) or similar

File: `internal/tui/rpc.go`
Add RPC client methods:
```go
func (c *RPCClient) CreateWorktree(sessionID, projectID string) (map[string]any, error)
func (c *RPCClient) RemoveWorktree(sessionID string) error
```

### Task 6: Tests
- Test handleWorktreeCreate creates a worktree and binds it to the session
- Test handleWorktreeRemove removes the worktree and clears the binding
- Test sessionLoop prefers WorktreePath when set

## Self-Verification Checklist
- [ ] `go build ./...` compiles
- [ ] `go test ./internal/session/... ./internal/rpc/... ./internal/project/... ./internal/agent/...` passes
- [ ] Session struct has WorktreeID and WorktreePath
- [ ] SetWorktree on both MemoryStore and SQLiteStore
- [ ] RPC handlers registered and functional
- [ ] sessionLoop prefers WorktreePath over ProjectPath
- [ ] /worktree command in TUI command_handler.go
