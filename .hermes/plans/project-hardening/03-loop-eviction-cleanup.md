# Leaf: Loop Eviction Cleanup

DISPATCH INSTRUCTION: Implement this leaf. Do NOT commit. Do NOT run git add. Write code, run tests, report results only.

## Parent
`master.md` (root)

## Scope
`internal/agent/loop.go` (new `Close()` method) and `internal/agent/manager.go` (`GetOrCreateWired` eviction).

## Problem
When `GetOrCreateWired` evicts a stale loop (project changed), it deletes the loop from the map without calling any cleanup. If the loop has an active message queue, goroutines, or open subscriptions, they leak until GC.

## Tasks

### Task 1: Add Close() method to AgentLoop
File: `internal/agent/loop.go`

```go
// Close releases resources held by this loop: cancels subscriptions,
// stops background goroutines, and signals shutdown. Safe to call
// multiple times (idempotent).
func (l *AgentLoop) Close() {
    if l == nil {
        return
    }
    // Cancel any active context (if the loop uses one for lifecycle).
    // Close message queue if present.
    // Cancel WebSocket subscriptions.
    // The loop's executor, cache, etc. are not owned by the loop and
    // are shared via ConfigSnapshot — do NOT close those.
    l.mu.Lock()
    defer l.mu.Unlock()
    // Mark as closed to prevent further use.
    // Check for existing stop/cancel patterns in the loop.
}
```

Read the existing `AgentLoop` struct to find what resources it owns vs shares:
- Message queue (if any) — close it
- Context cancel func (if any) — call it
- Background goroutines — signal stop via a done channel
- Shared resources (LLM client, tool registry, security) — do NOT close

### Task 2: Call Close() before eviction
File: `internal/agent/manager.go` `GetOrCreateWired` (~line 80-82)

In the stale-loop eviction code added in BUG 3 fix:

```go
if loop.GetWorkingDir() == workingDir {
    return loop, nil
}
// Evict stale loop — call Close() to release resources.
loop.Close()
delete(m.loops, sessionID)
```

### Task 3: Test
File: `internal/agent/manager_test.go`

Add a test that:
- Creates a loop via GetOrCreateWired
- Calls GetOrCreateWired again with a different workingDir (triggers eviction)
- Verifies the old loop's Close() was called (check a flag or resource state)

## Self-Verification Checklist
- [ ] `go build ./internal/agent/...` compiles
- [ ] `go test ./internal/agent/...` passes
- [ ] Close() is idempotent (safe to call multiple times)
- [ ] Close() does NOT close shared resources (tool registry, LLM client)
- [ ] GetOrCreateWired calls Close() before delete
