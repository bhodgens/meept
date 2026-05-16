# bus.subscribe creates subscription then immediately deletes it — bus.subscribe/bus.poll/bus.unsubscribe are broken
**Date**: 2026-05-16
**Phase**: 4
**Severity**: critical
**Component**: mcp/rpc

## Description
The `bus.subscribe`, `bus.poll`, and `bus.unsubscribe` RPC methods are completely non-functional. `bus.subscribe` creates a subscription and stores it in `p.subscriptions`, but the subscription is immediately deleted via the cleanup goroutine because the subscription context (`subCtx`) inherits from the operation-scoped context (`opCtx`) which is cancelled when `dispatch()` returns.

This breaks the MCP server's event polling (`meept_events` tool) because the subscription created by `ConnectAndSubscribe` is immediately cleaned up.

## Reproduction
1. Create a Unix socket RPC client connected to the daemon
2. Call `bus.subscribe` with topics `["chat.message.received"]`
3. Immediately call `bus.poll` with the returned `subscription_id`
4. Result: `error: [-32603] subscription not found: sub-xxx`

Confirmed via raw Python socket → daemon Unix socket test.

## Evidence
Debug log shows subscription created THEN immediately unsubscribed:
```
time=... level=DEBUG msg="bus: new subscriber" id=sub-xxx-chat.message.received topic=chat.message.received
time=... level=INFO msg="Created TUI event subscription" subscription_id=sub-xxx ...
time=... level=DEBUG msg="bus: unsubscribed" id=sub-xxx-chat.message.received topic=chat.message.received
```

All timestamps are at the same millisecond (`12:32:44.237`), confirming immediate teardown.

## Root Cause
The bug is in `internal/rpc/server.go` at `dispatch()`:

```go
opCtx, cancel := context.WithTimeout(ctx, operationTimeout)
defer cancel()  // Called when dispatch() returns!
result, err := handler(opCtx, req.Params)
```

In `internal/rpc/proxy.go` at `handleBusSubscribe()`:

```go
subCtx, cancelFunc := context.WithCancel(ctx)  // ctx is opCtx (operation-scoped!)
```

When `handleBusSubscribe` returns, `dispatch()` continues, calls `defer cancel()`, which cancels `opCtx`, which cancels `subCtx`, which triggers the cleanup goroutine at line 277-291 of proxy.go, which calls `p.subscriptions.Delete(subID)`.

The entire flow happens in the same operation — the dispatch function is synchronous.

## Impact
- **MCP `meept_events` tool is completely non-functional** — no event polling works through MCP
- **TUI event stream is also affected** — but the TUI runs in a long-lived process on the same connection, so its first event stream poll might work on the same connection before a new one is needed
- Any code relying on `bus.subscribe`/`bus.poll`/`bus.unsubscribe` is broken
- The daemon logs "Created TUI event subscription" at INFO but the subscription never works

## Proposed Fix
In `handleBusSubscribe`, use the parent context (`ctx` passed to dispatch, which is connection-scoped from `context.Background()`) instead of `opCtx` for subscription management:

Option A: Change `handleBusSubscribe` signature or add a separate `subCtx` using `context.Background()`:
```go
subCtx, cancelFunc := context.WithCancel(context.Background())
```

Option B: Pass the connection-scoped context through the handler (requires RPC server changes).

Option C: Use `context.WithTimeout(context.Background(), <long_timeout>)` instead of inheriting from `opCtx`.

## Phase 4 Testing Notes (2026-05-16)

### Tests executed
| # | Test | Result | Notes |
|---|------|--------|-------|
| 1 | `meept mcp-chat-server --help` | PASS | Command exists, help text correct |
| 2 | Daemon logs for MCP connection | PARTIAL | Only 1 log entry: `Session created mcp-test-session` |
| 3 | `meept session list` | FAIL | No `session` CLI subcommand; `meept session list` fails with "accepts at most 1 arg(s), received 2" |
| 4 | `meept chat "MCP test message"` | PASS (partial) | Chat works via CLI; session messages empty for bus-published messages (expected — chat handler doesn't save via bus.publish) |
| 5   | Event polling (`bus.subscribe` → `bus.poll`) | FAIL | Context cancellation bug — subscription deleted immediately after creation |
| 6 | `session.attach` (RPC) | PASS | Attaches with status "attached" |
| 7 | `source_client` attribution | PASS | `ChatRequest.SourceClient` accepted; broadcast as `chat.message.received` event |
| 8 | `session.messages.get` (via `meept_session_history` tool) | PASS (empty) | Returns `{"messages": null, "total": 0}` for MCP-created sessions (no chat history) |
| 9 | `meept_status` RPC / `meept status` CLI | PASS | Status shows 98 registered methods, 64 bus subscribers |

### Secondary issue found
**No `session` CLI subcommand**: The RPC client has `ListSessions()`, `CreateSession()`, `AttachSession()`, `GetSessionMessages()`, etc., and the proxy registers `session.list`/`session.create`/`session.attach`, but there is no `cmd/meept/session.go` to expose these via the CLI. This means CLI users cannot list, create, or attach sessions at all.

## Classification
[ ] Harness bug  [ ] Model quality  [x] Design gap