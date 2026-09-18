# Multi-Participant Agent Communication

## Overview
Meept supports multi-participant sessions where multiple clients (TUI, external AI agents via MCP, Telegram, HTTP API) can interact with the same agent session simultaneously. Each client has a distinct identity, sees messages from other participants, and can send messages that are attributed to them.

## Problem
Single-client sessions limit collaboration. Multi-participant communication enables:
- External agent platforms (Claude Code, etc.) connecting to meept sessions
- Multiple interfaces interacting with the same agent conversation simultaneously
- Bilateral visibility — all participants see who sent what
- Multi-agent handoff — the dispatcher routes work between specialist agents based on structured reports

## Behavior

### Client Identity

Every message flowing through the system carries a `source_client` field identifying which client sent it. This is for attribution and display only — the agent system treats all participants as equal peers.

**`ChatRequest` includes `SourceClient`:**

```go
type ChatRequest struct {
    Message        string `json:"message"`
    ConversationID string `json:"conversation_id"`
    SourceClient   string `json:"source_client,omitempty"` // "tui", "claude", "mcp", etc.
}
```

**`BusMessage` includes `SourceClient`:**

```go
type BusMessage struct {
    // ... existing fields ...
    SourceClient string `json:"source_client,omitempty"`
}
```

Each transport sets `SourceClient` automatically:
| Transport | SourceClient Value |
|-----------|-------------------|
| TUI | `"tui"` |
| MCP server | `"mcp"` or client-provided |
| Telegram | `"telegram"` |
| HTTP API | `"http:<api_key_name>"` |
| Default | empty (backwards compatible) |

### Bilateral Visibility

When a client sends a message, the `ChatHandler` broadcasts a `chat.message.received` event to all session participants before dispatching to the agent. This gives every attached client visibility into what others have sent.

**Flow:**
```
Client A sends "fix the auth bug"
  -> chat.request with source_client: "client_a"
  -> ChatHandler broadcasts chat.message.received
     -> all session clients see "[client_a] fix the auth bug"
  -> Dispatcher routes to debugger agent
  -> Agent responds
  -> chat.response is delivered as the RPC reply to the requesting
     client; the WS relay deliberately does NOT re-broadcast it
     (de671eb2: transformBusEventToWS drops the chat.response topic
     before classification — the legacy chat.* prefix match used to
     classify it as agent_progress and double-deliver the reply to
     HTTP+WS clients). Session clients see the reply via the
     chat_message push published by ChatHandler.publishChatMessage.
     Pinned by TestTransformBusEventToWS_ChatResponseNotRelayed and
     the WSClassParity fence.
```

**Event types:**

| Event | Type Constant | Data Struct |
|-------|--------------|-------------|
| Message received | `AgentEventChatMessageReceived` | `ChatMessageReceivedData` |
| Client disconnected | `AgentEventChatClientDisconnected` | `ChatClientDisconnectedData` |

**`ChatMessageReceivedData`:**
```json
{
  "session_id": "conv-abc123",
  "source_client": "claude",
  "content": "fix the auth bug"
}
```

**`ChatClientDisconnectedData`:**
```json
{
  "session_id": "conv-abc123",
  "source_client": "claude"
}
```

### Report Router (Multi-Agent Handoff)

The `ReportRouter` replaces the previous dead-end behavior where `DetermineRouteAction` was computed but never acted on. It executes routing decisions after an agent completes its work.

**Route actions:**

| Action | Behavior |
|--------|----------|
| `RouteActionClose` | Agent completed. Format response from `Accomplished` + `Observations`. |
| `RouteActionRoute` | Hand off to the next suggested agent. Increments depth. |
| `RouteActionNotifyUser` | User input needed. Force notification with `DecisionContext` + `NotDone`. |
| `RouteActionNotifyError` | Agent failed. Force notification with `Issues`. |

**Max handoff depth: 5** — prevents infinite agent-to-agent routing loops. After 5 handoffs, the router forces `RouteActionNotifyUser` and presents the accumulated report.

**Context accumulation:** Each agent handoff passes the previous agent's `Accomplished`, `Issues`, `Observations`, and `DecisionContext` as context so the next agent knows what already happened.

**Handoff flow:**
```
Agent completes -> ExtractReport() -> DetermineRouteAction()
                                         |
                    ┌────────────────────┼──────────────────────┐
                    |                    |                      |
              RouteActionClose    RouteActionRoute     RouteActionNotifyUser
                    |                    |                      |
              format response     route to next agent    push to all clients
              return to caller    accumulate context     await response
                                         |
                                   loop back to
                                   DetermineRouteAction()
```

## Configuration

```json5
// In meept.json5
{
  mcp_chat_server: {
    enabled: true,
    socket_path: "~/.meept/meept.sock",
  },
}
```

## MCP Chat Server

The MCP (Model Context Protocol) chat server exposes meept sessions to external agent platforms. It communicates via JSON-RPC over stdin/stdout and connects to the meept platform via the existing Unix socket RPC transport.

### Starting the server

```bash
meept mcp-chat-server
```

The server:
1. Connects to the platform via Unix socket RPC
2. Subscribes to bus topics: `chat.message.received`, `chat.response`, `agent.event.*`, `worker.*`
3. Reads JSON-RPC from stdin, writes responses to stdout
4. Logs diagnostic info to stderr

### Registering with Claude Code

Add to `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "meept": {
      "command": "meept",
      "args": ["mcp-chat-server"]
    }
  }
}
```

### MCP Tools

| Tool | Description |
|------|-------------|
| `meept_sessions` | List, create, or attach to chat sessions |
| `meept_send` | Send a message to an attached session (includes `source_client`) |
| `meept_events` | Poll events since last call (agent progress, other participants' messages) |
| `meept_status` | Get platform status (active agents, queue depth, connected clients) |
| `meept_session_history` | Get recent messages from a session |

**`meept_sessions` actions:**
- `list` — show all sessions
- `create` — make a new session (optional `name`, defaults to `"mcp-session"`)
- `attach` — join an existing session by `session_id`; auto-fetches last 50 messages

**`meept_send` parameters:**
- `session_id` (required) — session to send to
- `message` (required) — message text
- `source_client` (optional) — client identifier, defaults to `"mcp"`

## Observability

### Logging
- Client connection/disconnection events with `source_client`
- Agent handoff decisions with depth tracking
- Route action outcomes (close, route, notify, error)

### Metrics
- Multi-agent handoff depth per conversation
- Route action distribution (close vs route vs notify)
- MCP tool call frequency

### Debug Info
- Current routing depth per active handoff chain
- Connected client identities per session
- Bus subscription state for MCP connections

## Edge Cases

### Max Route Depth Exceeded
- Forces `RouteActionNotifyUser` with accumulated response
- Logs warning with depth and max depth values
- User sees "routing depth limit reached after N handoffs" plus what was accomplished

### MCP Server — Platform Not Running
- Clear error message with remediation instructions
- Exit code 1

### MCP Server — Empty SourceClient
- No `chat.message.received` broadcast emitted
- Backwards compatible — existing single-client behavior unchanged

### Session with No Attached MCP Clients
- Broadcasts still emitted (no-op if no subscribers)
- TUI-only sessions behave identically to before

## Related
- [Agent Orchestration](agent-orchestration.md) — agent discovery and delegation
- [External Integrations](external-integrations.md) — Telegram, web, calendar
- [Agent Lateral Interrogation Howto](agent-lateral-interrogation-howto.md) — how AI agents can communicate with and debug meept
