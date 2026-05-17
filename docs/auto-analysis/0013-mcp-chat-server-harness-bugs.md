# 0013: MCP Chat Server Harness Bugs

**Date**: 2026-05-15
**Component**: `internal/mcp/`, `cmd/meept/mcp_chat_server.go`
**Severity**: Medium (2 bugs, 1 cosmetic)

## Summary

The MCP chat server (`meept mcp-chat-server`) successfully starts, connects to the daemon, and exposes 5 tools. The MCP protocol handshake, tool listing, and most tool calls work correctly. Three issues were identified during live testing.

## Test Results

### Passing
- Server startup and daemon connection (subscription created)
- MCP `initialize` handshake (protocol version 2024-11-05)
- `notifications/initialized` handled correctly (no response, per spec)
- `tools/list` returns all 5 tools with correct schemas
- `meept_status` returns daemon status
- `meept_sessions` list/create/attach all functional
- `meept_session_history` returns messages (empty for new sessions)
- `meept_send` routes through daemon to agent loop successfully
- All 12 unit tests pass
- `source_client` parameter is correctly forwarded and used for broadcast attribution

### Registered MCP Tools
1. `meept_sessions` - list, create, attach to sessions
2. `meept_send` - send message to session (includes `source_client`)
3. `meept_events` - poll bus events (requires `subscription_id`)
4. `meept_status` - daemon status
5. `meept_session_history` - fetch session message history

## Bug 1 (Medium): meept_events subscription_id not accessible to MCP clients

**File**: `internal/mcp/server.go`, `cmd/meept/mcp_chat_server.go`

The subscription ID is created during `ConnectAndSubscribe()` and printed to stderr:
```
meept mcp-chat-server: connected (subscription: sub-XXX)
```

The MCP client (e.g., Claude Code) has no way to discover this ID:
- The subscription ID is not stored on the `Server` struct
- No MCP tool returns the subscription ID
- `meept_events` requires `subscription_id` as a required parameter
- The ID is only in the stderr log, which MCP clients do not read

This means `meept_events` is unusable from any MCP client. The subscription exists on the daemon side but cannot be referenced.

**Impact**: Multi-participant event streaming is broken. MCP clients cannot receive real-time notifications of messages from other participants.

**Fix direction**: Store the subscription ID on the `Server` struct and either:
- (a) Make `meept_events` default to the server's own subscription when `subscription_id` is omitted, or
- (b) Add a `meept_subscribe` tool that returns the subscription ID, or
- (c) Return the subscription ID as part of the `initialize` response or a `meept_status` field.

## Bug 2 (Low): Tool response values serialized with Go %v formatting

**File**: `internal/mcp/server.go:179`

**Status: FIXED (2026-05-16).** Replaced `fmt.Sprintf("%v", result)` with a type switch on the result value. String and `json.RawMessage` types pass through as-is; other types are serialized via `json.Marshal(result)` to produce proper JSON output instead of Go struct formatting.

**Impact**: MCP clients receive poorly formatted, hard-to-parse responses. LLM-based consumers may misinterpret the data.

**Fix direction**: Use `json.Marshal` on the result before embedding it in the MCP content text, e.g.:
```go
data, _ := json.Marshal(result)
"text": string(data)
```

## Bug 3 (Cosmetic): No graceful shutdown / subscription cleanup on stdin close

**File**: `cmd/meept/mcp_chat_server.go`

When the MCP server process exits (stdin closes), the daemon-side subscription is not explicitly cleaned up. The daemon does eventually clean up subscriptions via context cancellation when the RPC connection drops, but there is a brief window where the subscription lingers.

The daemon log shows the unsubscribe happens promptly when the connection drops, so this is not a resource leak in practice.

**Impact**: Negligible in practice.

## Files Examined

- `cmd/meept/mcp_chat_server.go` - CLI command entry point
- `internal/mcp/server.go` - MCP server core, tool implementations
- `internal/mcp/tools.go` - Tool definitions and schemas
- `internal/mcp/transport.go` - JSON-RPC message read/write
- `internal/mcp/server_test.go` - 8 unit tests
- `internal/mcp/tools_test.go` - 2 unit tests
- `internal/mcp/transport_test.go` - 2 unit tests
- `internal/transport/client.go` - Client interface
- `internal/transport/rpc_client.go` - RPC adapter
- `internal/rpc/proxy.go` - bus.subscribe, bus.poll handlers
- `internal/agent/handler.go` - ChatRequest with SourceClient
- `internal/agent/events.go` - ChatMessageReceivedData

## Daemon RPC Handlers Verified

All required daemon-side RPC handlers are registered:
- `chat` - message routing with source_client support
- `session.create`, `session.list`, `session.get`, `session.attach`, `session.detach`
- `session.messages.get`, `session.messages.save`
- `bus.subscribe`, `bus.poll`, `bus.unsubscribe`
