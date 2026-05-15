# Agent Lateral Interrogation Howto

How external AI agents (Claude, GPT, etc.) can communicate with meept, inspect its state, debug sessions, and participate as equal peers in agent conversations.

## Overview

Meept exposes an MCP (Model Context Protocol) server that allows external AI agents to connect to running meept sessions. This enables:

- **Lateral communication** — an external agent can send messages to a meept session and see responses from meept's agent system
- **Session inspection** — query session history, daemon status, and active workers
- **Event monitoring** — poll for agent progress events, messages from other participants, and agent responses
- **Multi-participant collaboration** — multiple agents (human via TUI, Claude via MCP, etc.) share the same session

## Setup

### 1. Start the meept daemon

```bash
meept daemon start
# or foreground: meept daemon -f
```

The daemon must be running before the MCP server can connect.

### 2. Register meept as an MCP server

For Claude Code, add to `~/.claude/settings.json`:

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

For other MCP clients, run `meept mcp-chat-server` as a subprocess. It communicates via JSON-RPC over stdin/stdout.

### 3. Verify connection

The MCP server sends diagnostic output to stderr. Check for:

```
meept mcp-chat-server: connected (subscription: sub-xxxxx)
```

If you see an error about the daemon not running, start it first.

## Communication Patterns

### Pattern 1: Start a new session

```
1. Call meept_sessions(action: "create", name: "debug-auth")
   → returns { session_id: "sess-abc123" }

2. Call meept_send(session_id: "sess-abc123", message: "check the auth module for bugs", source_client: "claude")
   → meept dispatches to the debugger agent
   → returns { response: "agent response text" }

3. Call meept_events(subscription_id: "sub-xxxxx")
   → returns any events since last poll (progress updates, other participant messages)
```

### Pattern 2: Join an existing session

```
1. Call meept_sessions(action: "list")
   → returns all active sessions

2. Call meept_sessions(action: "attach", session_id: "sess-abc123", client_id: "claude")
   → attaches to the session
   → auto-fetches last 50 messages for context

3. Call meept_send(session_id: "sess-abc123", message: "I see the TUI user asked about auth. Here's what I found...", source_client: "claude")
   → TUI user sees "[claude] I see the TUI user asked about auth..."
```

### Pattern 3: Monitor and observe

```
1. Attach to a session (see Pattern 2)

2. Poll for events:
   meept_events(subscription_id: "sub-xxxxx", since: "2026-05-15T10:30:00Z")
   → returns events after the timestamp

3. Read session history:
   meept_session_history(session_id: "sess-abc123", limit: 100)
   → returns last 100 messages
```

### Pattern 4: Check system health

```
1. Call meept_status()
   → returns daemon status: active agents, queue depth, connected clients, uptime
```

## Debugging Guide

### Problem: MCP server won't start

**Check:** Is the daemon running?

```bash
meept status
```

**Check:** Is the socket accessible?

```bash
ls -la ~/.meept/meept.sock
```

**Check:** Config has MCP chat server enabled?

```bash
grep -A3 mcp_chat_server ~/.meept/meept.json5
```

### Problem: Messages not reaching the agent

**Check:** Is the session ID valid?

```
meept_sessions(action: "list")
```

Verify the `session_id` you're sending to exists in the list.

**Check:** Was `source_client` set? If empty, no `chat.message.received` broadcast is emitted, but the message still processes.

**Check:** Daemon logs for the routing decision:

```
# Look for: "Agent completed" and "action" entries in daemon output
meept daemon -f  # foreground with visible logs
```

### Problem: Not seeing events from other participants

**Check:** Are you polling with the correct `subscription_id`?

The subscription ID is returned by `ConnectAndSubscribe` and logged to stderr when the MCP server starts.

**Check:** Are you using the `since` parameter correctly?

The `since` parameter is an RFC3339 timestamp. Events before this time are skipped.

**Check:** Are you polling frequently enough?

Events are delivered via polling. If you don't poll, events queue up in the bus subscription buffer.

### Problem: Agent handoff seems stuck

The report router has a max depth of 5. If an agent chain exceeds 5 handoffs, the router forces a user notification.

**Check daemon logs for:**

```
max route depth reached, forcing user notification (depth=5, max=5)
```

### Problem: Response is empty or unexpected

**Check session history** for the full conversation context:

```
meept_session_history(session_id: "sess-abc123", limit: 50)
```

The agent may have produced a structured report that was processed by the report router. The displayed response is the `StripReport` output — the raw agent text with the structured report section removed.

## Architecture Reference

```
External Agent (e.g., Claude Code)
    |
    | MCP protocol (stdin/stdout JSON-RPC)
    |
    v
meept mcp-chat-server (stateless protocol bridge)
    |
    | Unix socket RPC
    |
    v
meept-daemon
    |
    +-- ChatHandler (broadcasts chat.message.received)
    +-- Dispatcher (routes to specialist agents)
    +-- ReportRouter (multi-agent handoff with depth limit)
    +-- Message Bus (pub/sub for events)
    +-- Agent Registry (coder, debugger, planner, etc.)
```

The MCP server is stateless — it translates between MCP protocol and meept's existing RPC. All state lives in the daemon.

## Event Types

| Bus Topic | Trigger | What You See |
|-----------|---------|-------------|
| `chat.message.received` | Any client sends a message | `[source_client] message content` |
| `chat.response` | Agent completes | Full agent response text |
| `agent.event.*` | Agent lifecycle events | Tool calls, turns, progress |
| `worker.*` | Worker state changes | `worker.started`, `worker.completed` |

## Tips

1. **Always set `source_client`** — This ensures your messages are attributed and broadcast to other session participants.

2. **Poll frequently** — Event data is buffered but not persisted indefinitely. Poll every few seconds during active sessions.

3. **Use session history for context** — When attaching to an existing session, the `attach` action auto-fetches the last 50 messages. For longer context, use `meept_session_history` with a higher limit.

4. **Check status before sending** — A quick `meept_status` call tells you if the daemon is healthy and which agents are active.

5. **The `since` parameter is exclusive** — Events at exactly the `since` timestamp are not included. Use the timestamp from the last event you received.

6. **Multi-agent handoffs are transparent** — When the dispatcher routes through multiple agents (e.g., coder -> reviewer), you see the final synthesized response, not intermediate agent outputs.

## Related
- [Multi-Participant Communication](multi-participant-comms.md) — full feature documentation
- [Agent Orchestration](agent-orchestration.md) — agent discovery and delegation
- [External Integrations](external-integrations.md) — Telegram, web, calendar integrations
