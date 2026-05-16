# Phase 12: CLI/TUI & Sessions Testing

**Date**: 2026-05-16
**Duration**: ~20 minutes of testing

## Test Approach

Ran CLI commands for session management, memory queries, and models listing.

## Results

### 1. Non-interactive Chat (PASS)
- Command: `meept "hello"`
- Response: "Hello! I'm here to help you with questions, tasks, or conversation. What can I assist you with today?"
- Clean, correct response in non-interactive mode

### 2. Session List (FAIL)
- Command: `meept session list`
- Error: `Error: accepts at most 1 arg(s), received 2`
- The `session` command appears to accept a positional argument but doesn't document or implement a `list` subcommand properly
- **Note**: The `session` command is NOT listed in `meept help` output, suggesting it may be a deprecated/hidden command
- The `branch` command IS properly implemented and documented

### 3. Branch Navigation (FAIL with specific error)
- Command: `meept branch list`
- Error: `failed to get most recent session: [-32601] method not found: session.get_most_recent`
- **Root cause**: The RPC server does NOT register `session.get_most_recent` as an RPC method
- The session handler subscribes to `session.get_most_recent` on the **bus** (not RPC)
- The RPC client calls `session.get_most_recent` directly via RPC method dispatch
- **This is a wiring bug**: `internal/rpc/server.go` only registers 6 methods (`ping`, `status`, `daemon.status`, `bus.publish`, `bus.stats`, `task.amend.submit`). All session methods (`session.get_most_recent`, `session.create`, `session.list`, `session.get`, `session.attach`, `session.delete`, etc.) are handled by the session's bus subscription handler but are never exposed as RPC methods.
- The daemon needs to either:
  (a) Register bus-topic-based RPC handlers for session methods that forward to the bus via `bus.publish`, OR
  (b) Register session methods directly on the RPC server with callbacks into `SessionHandler`

### 4. Session Persistence (PARTIAL PASS)
- Created a session message, restarted daemon, sent follow-up message
- The daemon correctly maintained conversation context after restart
- Message persisted across daemon restart
- **Caveat**: Memory persistence has a known issue (see below)

### 5. Memory Query (PASS, with caveat)
- Command: `meept memory query "test"`
- Response: "No memories found"
- During session persistence test, response included: "The `episodic_memories` table is missing a `last_accessed_at` column, which prevented the memory from being persisted"
- This is a known schema migration issue (#20 in the harness bug list)

### 6. Models List (PASS)
- Command: `meept models list`
- Response: Correctly lists all 7 providers with their models
- Output: gala-llama, ollama, local, zai, gala-mlx providers with correct model details

### 7. Daemon Crash During Testing (NOTED)
- The daemon crashed sometime during Phase 11/12 testing
- Log showed no crash - process simply disappeared
- Socket file became stale, daemon had to be manually restarted
- On restart, daemon started cleanly and sessions continued
- **Investigation needed**: The cause of the daemon crash is not evident from logs (last log entries show normal task processing)

## Wiring Bug Detail: RPC Session Methods Not Registered

File: `/Users/caimlas/git/meept/internal/rpc/server.go`
The RPC server's `registerBuiltinHandlers()` only registers:
- `ping`
- `status`, `daemon.status`
- `bus.publish`
- `bus.stats`
- `task.amend.submit`

Session methods are handled by `internal/session/session.Handler` which subscribes to the **bus**, not RPC. The CLI client calls RPC methods directly (`client.Call("session.get_most_recent", nil)`), so the RPC server returns "method not found".

The RPC bus publish handler could serve as a bridge, but the client must explicitly call `bus.publish` with the topic. The client calls `session.get_most_recent` directly instead.

**Fix options**:
1. Add a generic bus proxy handler: `client.Call("bus.publish", {topic: "session.get_most_recent", payload: {...}})` - requires client changes
2. Register session RPC handlers individually on the RPC server - requires daemon wiring changes

This affects: `branch list`, `branch tree`, `branch navigate`, and any CLI command that needs session info.
