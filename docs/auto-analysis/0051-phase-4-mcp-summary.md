# Phase 4 (MCP Communication) — QA Summary

**Date**: 2026-05-16
**CLI binary**: `/Users/caimlas/go/bin/meept`
**Daemon**: PID 35729, uptime ~12 min at time of testing

## Test Results

| # | Test | Result | Notes |
|---|------|--------|-------|
| 1 | `meept mcp-chat-server --help` | PASS | Command registered, help text correct, shows register-instructions for Claude Code |
| 2 | Daemon logs for MCP connection | INFO | Only 1 MCP-related log: `Session created mcp-test-session` |
| 3 | Session list via CLI | FAIL | No `session` CLI subcommand exists (`cmd/meept/session.go` missing) |
| 4 | `meept chat "MCP test message"` | PASS | Chat response received (default model: zai/glm-4.7) |
| 5  | Event polling (`bus.subscribe` → `bus.poll`) | FAIL | **Critical bug**: subscription context cancelled immediately |
| 6 | `session.attach` (RPC) | PASS | `session.attach` returns `{"status":"attached"}` |
| 7 | `source_client` attribution | PASS | Field accepted in `ChatRequest`, broadcast in `chat.message.received` event |
| 8 | `meept_session_history` tool | PASS (empty) | Returns `{"messages": null, "total": 0}` for sessions without chat history |
| 9 | `meept_status` RPC / CLI | PASS | 98 registered RPC methods, 64 bus subscribers |

## Unit Tests

All MCP package tests pass:
```
ok  github.com/caimlas/meept/internal/mcp 0.288s
```
13 tests in `internal/mcp/` covering: initialize, tools/list, tools/call, unknown method, notification, EOF, invalid params, tool definitions, message read/write.

## RPC Method Coverage

Verified via socket RPC that the following methods work:
- `status` — daemon status (98 methods registered)
- `session.list` — returns 28 sessions
- `session.create` — creates named session
- `session.attach` — attaches client to session
- `session.messages.get` — retrieves messages (may be empty)
- `session.detach` — detaches client
- `session.delete` — deletes session
- `bus.subscribe` — creates subscription (then immediately broken)
- `bus.poll` — broken due to context cancellation
- `bus.unsubscribe` — works (returns null when already cleaned up)
- `bus.publish` — delivers to 1 subscriber

## Issues Found

### 1. Context-cancellation destroys bus subscriptions (CRITICAL)
- **File**: `internal/rpc/proxy.go` lines 241-291
- **Impact**: `bus.subscribe`/`bus.poll`/`bus.unsubscribe` are completely non-functional
- **Root cause**: `handleBusSubscribe` creates `subCtx` from `opCtx` (operation-scoped), which is cancelled when `dispatch()` returns. The cleanup goroutine then removes the subscription before `bus.poll` can use it.
- **Documented**: `docs/auto-analysis/0051-bus-subscription-context-cancellation.md`

### 2. No `session` CLI subcommand (DESIGN GAP)
- **File**: `cmd/meept/main.go` — no `newSessionCmd()` registered
- **Impact**: Users cannot list, create, attach, or manage sessions from the CLI despite full RPC support
- The transport client has all session methods (`ListSessions`, `CreateSession`, `AttachSession`, `GetSessionMessages`, etc.) but the CLI has no command to invoke them
- **Related**: `branch` subcommands exist (`meept branch list`, `meept branch fork`) but a top-level `session` command does not

### 3. `bus.publish` does not trigger chat handler (MINOR)
- Published `chat.request` via `bus.publish` delivers to the bus but does not result in session message persistence
- This is expected behavior — only the dedicated `chat` proxy handler processes `chat.request` through the agent loop and `SessionStore.SaveMessages`. The `bus.publish` is a fire-and-forget bus mechanism.

## Recommendations

1. **Fix context cancellation (priority: P0)**: Change `handleBusSubscribe` to derive `subCtx` from `context.Background()` instead of the operation-scoped `opCtx`. The subscription should live until explicitly unsubscribed or the connection closes. The connection-scoped context could be passed through, or a separate lifecycle be managed.

2. **Add `meept session` CLI subcommand (priority: P1)**: Create `cmd/meept/session.go` with subcommands: `list`, `create`, `attach`, `detach`, `delete`, `history`, `get`. This aligns with the existing `branch` command and fills a functional gap.

3. **Add event subscription to mcp-chat-server auto-initialization**: The `ConnectAndSubscribe` in `mcp_chat_server.go` line 313 should detect `bus.subscribe` failure and log a warning so MCP operators know events are unavailable.

4. **Consider adding a `meept session history` CLI command**: To complement `meept_session_history` tool, allow CLI users to view session message history.
