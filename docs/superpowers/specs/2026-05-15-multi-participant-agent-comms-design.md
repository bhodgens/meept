# Multi-Participant Agent Communication Design

**Date:** 2026-05-15
**Status:** Draft

## Problem

Meept's multi-agent pipeline has incomplete wiring: agents produce structured reports but the dispatcher never acts on routing decisions (the computed `RouteActionRoute` is logged and discarded). There is no synthesis of agent progress back to users, no mechanism for external agent platforms (Claude, etc.) to interact with meept sessions, and no concept of multiple participants in a single session.

## Goals

1. **Fix the multi-agent dispatch loop** — make report routing actually execute agent handoffs
2. **Enable multi-participant sessions** — the TUI user and external clients (Claude via MCP) are equal peers in a session, seeing each other's messages and agent responses
3. **Add an MCP server** — standard agent-to-agent protocol for external platforms to connect
4. **Improve TUI progress reporting** — tiered verbosity for agent activity

## Non-Goals

- ACLs or role-based permissions — all session participants are peers
- SSE or other push-based transport (polling is sufficient; can add later)
- Changes to the agent loop, bus, session store, or tool system
- LLM-based progress synthesis for MCP clients (they consume raw events directly)

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   Clients                           │
│                                                     │
│  ┌──────────┐  ┌──────────────┐  ┌───────────────┐ │
│  │   TUI    │  │ Claude (MCP) │  │ Other Agents  │ │
│  │ (RPC)    │  │ (MCP)        │  │ (MCP)         │ │
│  └────┬─────┘  └──────┬───────┘  └──────┬────────┘ │
└───────┼───────────────┼─────────────────┼───────────┘
        │               │                 │
        ▼               ▼                 ▼
┌───────────────┐  ┌──────────────────────────────────┐
│  Unix Socket  │  │     MCP Chat Server               │
│  JSON-RPC     │  │  (stdin/stdout, MCP protocol)     │
└───────┬───────┘  └──────────────┬───────────────────┘
        │                         │
        ▼                         ▼
┌─────────────────────────────────────────────────────┐
│              Service Layer (existing)                │
│                                                     │
│  ChatService ──▶ Dispatcher ──▶ Agent Registry      │
│       │              │                               │
│       │         ┌────▼─────────────────────┐         │
│       │         │ Report Router (NEW)       │         │
│       │         │ - Execute RouteAction     │         │
│       │         │ - Multi-agent handoff     │         │
│       │         │ - Synthesize progress     │         │
│       │         └────┬─────────────────────┘         │
│       │              │                               │
│  ┌────▼──────────────▼──────────────────────┐        │
│  │        Message Bus (existing)             │        │
│  │  + source_client on messages              │        │
│  │  + chat.message.received broadcast        │        │
│  │  + agent.progress.synthesized events      │        │
│  └──────────────────────────────────────────┘        │
└─────────────────────────────────────────────────────┘
```

## Design

### 1. Client Identity & Bilateral Visibility

All messages carry a `source_client` field identifying who sent them. This is for display/attribution only — the agent system treats all participants as equal peers.

**Changes to `ChatRequest`:**

```go
type ChatRequest struct {
    Message        string `json:"message"`
    ConversationID string `json:"conversation_id"`
    SourceClient   string `json:"source_client,omitempty"` // "tui", "claude", "telegram", etc.
}
```

Each transport sets `SourceClient`:
- TUI → `"tui"`
- MCP server → `"claude"` (or whatever the MCP client identifies as)
- Telegram → `"telegram"`
- HTTP API → `"http:<api_key_name>"`
- Default → `"unknown"`

**New bus event: `chat.message.received`**

Published by `ChatHandler` before dispatching to the agent. Gives all session participants visibility into what others have sent.

```go
type MessageReceivedEvent struct {
    SessionID    string `json:"session_id"`
    SourceClient string `json:"source_client"`
    Content      string `json:"content"`
    Timestamp    string `json:"timestamp"`
}
```

Flow:
```
Client A sends "fix the auth bug"
    → chat.request with source_client: "client_a"
    → ChatHandler broadcasts chat.message.received → all session clients see "[client_a] fix the auth bug"
    → Dispatcher routes to debugger agent
    → Agent responds
    → chat.response broadcast to all session clients
```

No privacy concern — all attached clients opted in by attaching. Private sessions are achieved by detaching unwanted clients.

### 2. Report Router — Fixing the Multi-Agent Loop

Currently `DetermineRouteAction` is computed but never acted on (`dispatcher.go:700-702`). The report router fixes this.

**New file: `internal/agent/report_router.go`**

```go
type ReportRouter struct {
    registry    *AgentRegistry
    dispatcher  *Dispatcher
    synthesizer *ProgressSynthesizer
    bus         *bus.MessageBus
    aggregator  *TaskReportAggregator
    logger      *slog.Logger
    maxDepth    int // default: 5
}
```

**Behavior — replaces dead-end code in `dispatcher.go:699-710`:**

```
agent completes → ExtractReport() → DetermineRouteAction()
                                               │
                    ┌─────────────────────────┼───────────────────────┐
                    │                         │                       │
              RouteActionClose         RouteActionRoute        RouteActionNotifyUser
                    │                         │                       │
              aggregate report          run next agent           push to all clients
              synthesize summary        aggregate report         await response
              return to caller          synthesize progress      (new chat.request when
                                               │                  user responds)
                                        loop back to
                                        DetermineRouteAction()
```

Properties:
- **Max handoff depth: 5** — prevents infinite agent-to-agent routing. After 5 handoffs, force `RouteActionNotifyUser` and present accumulated report to all clients.
- **Context accumulation** — each agent handoff passes the previous agent's `Accomplished`/`Observations` as context so the next agent knows what already happened.
- **Single response to caller** — `ChatHandler` gets back one final synthesized response, not N intermediate ones.

**Modified file: `internal/agent/dispatcher.go`**

The block at lines 699-710 where `DetermineRouteAction` is computed and discarded is replaced with a call to `ReportRouter.Route()`, which handles the full handoff loop.

### 3. Progress Synthesis & Configurable Verbosity (Track 2)

Turns raw agent events into tiered human-readable summaries. Primarily for TUI consumption — MCP clients consume raw events directly.

**New file: `internal/agent/progress_synthesizer.go`**

```go
type VerbosityLevel int

const (
    VerbosityQuiet   VerbosityLevel = 0  // task-level only
    VerbosityNormal  VerbosityLevel = 1  // task + notable tool calls
    VerbosityVerbose VerbosityLevel = 2  // everything: turns, tools, tokens, timing
)

type ProgressSynthesizer struct {
    bus      *bus.MessageBus
    client   *llm.Client  // separate from agent client (reuses classifier pattern)
    model    string
    logger   *slog.Logger
}
```

**Input:** subscribes to existing agent events on the bus.

**Output:** publishes new `agent.progress.synthesized` events.

```go
type SynthesizedProgressEvent struct {
    SessionID   string         `json:"session_id"`
    AgentID     string         `json:"agent_id"`
    Tier        VerbosityLevel `json:"tier"`          // 0, 1, or 2
    Message     string         `json:"message"`       // human-readable summary
    SourceEvent AgentEventType `json:"source_event"`  // original event type
    Timestamp   time.Time      `json:"timestamp"`
}
```

Clients filter client-side: TUI at quiet tier ignores events with `tier > 0`. Verbose tier shows everything. All subscribe to the same `agent.progress.synthesized` topic.

**Two synthesis modes:**

1. **Template-based (fast, no LLM)** — for most events. Pattern-match event type and format a string. E.g., `tool_execution_end` with `tool_name: "shell_execute"` → "ran shell command: {first line of result}". Covers quiet and normal tiers without latency.

2. **LLM-summarized (agent completions only)** — when an agent finishes with an `AgentReport`, use the classifier model to summarize into 1-2 sentences. E.g., "coder: implemented auth module (3 files changed, 12s)". Only triggered on `agent_end`.

**Example output by tier:**

```
Quiet:    [debugger] completed: implemented fix (3 files, 14s)

Normal:   [debugger] investigating auth.go...
          [debugger] ran tests: 47 passed, 2 failed
          [debugger] completed: implemented fix (3 files, 14s)

Verbose:  [debugger] turn 1: 2 tool calls, 1.2k tokens
          [debugger] executing shell_command: go test ./...
          [debugger] shell_command complete (3.2s): 47 passed, 2 failed
          [debugger] turn 2: 1 tool call, 800 tokens
          [debugger] executing file_write: auth.go
          [debugger] completed: implemented fix (3 files, 14s)
```

### 4. MCP Chat Server

Standard MCP server for external agent platforms to connect to meept sessions.

**New package: `internal/mcp/`**

**MCP tools:**

| Tool | Description |
|------|-------------|
| `meept_sessions` | List sessions, create new session, attach to existing |
| `meept_send` | Send a message to an attached session |
| `meept_events` | Poll events since last call (raw bus events + chat.message.received) |
| `meept_status` | Daemon status, active agents, queue depth |
| `meept_task_status` | Query specific task/step status and reports |
| `meept_session_history` | Get recent messages from a session |

**Transport:** MCP server connects to meept-daemon via existing RPC (same as TUI). Translates between MCP protocol (JSON-RPC over stdin/stdout) and meept's internal RPC (Unix socket JSON-RPC). No new daemon-side transport needed.

**CLI entry point:** `meept mcp-chat-server`

**Auto-catchup on connect:** When `meept_sessions` attaches to a session, the MCP server automatically fetches recent history via `session.messages.get` and includes it in the tool response. The MCP client sees conversation history without an explicit separate call.

**MCP is stateless** — just proxies between stdin/stdout and daemon RPC. Restarting reconnects, reattaches, resumes. No state lost.

**Configuration in `meept.json5`:**

```json5
{
  mcp_chat_server: {
    enabled: true,
    socket_path: "~/.meept/meept.sock",  // override if non-default
  }
}
```

**Claude Code registration (`~/.claude/settings.json`):**

```json
{
  "mcpServers": {
    "meept": {
      "command": "meept",
      "args": ["mcp-chat-server"],
      "env": {
        "MEEPT_SOCKET": "~/.meept/meept.sock"
      }
    }
  }
}
```

### 5. TUI Changes

**5a. Participant badges on messages**

Chat view changes from `you:` / `assistant:` to participant badges:

```
[you] fix the auth bug              ← from TUI
[claude] also check the middleware   ← from Claude via MCP
[debugger] found SQL injection...    ← agent response
[claude] run the tests too           ← from Claude
[debugger] tests passing...          ← agent response
```

Badge comes from `source_client` on `chat.message.received` events. Agent responses use the agent's ID as the badge.

**5b. Verbosity control**

- New keybinding: `ctrl+v` to cycle quiet → normal → verbose
- Status bar shows current level: `verbosity: normal`
- Default from `client.json5`:

```json5
{
  chat: {
    verbosity: "normal",  // "quiet" | "normal" | "verbose"
  }
}
```

### 6. Error Handling & Edge Cases

**6a. Conflicting messages** — Two clients send simultaneously. The agent loop already handles this via the steering/follow-up heuristic. Second message either interrupts (high urgency) or queues (low urgency). No change needed.

**6b. Client disconnects** — Daemon detects RPC connection drop, publishes `chat.client.disconnected`, detaches from session. TUI sees "[client_name disconnected]". Agent work continues — session isn't tied to any single client.

**6c. Session without clients** — Session persists in SQLite. Any client reconnecting can attach and catch up via history. In-flight agent work continues to completion, results stored in session messages.

**6d. MCP server crash/restart** — Stateless proxy. Restart reconnects to daemon, reattaches, resumes polling. No state lost.

**6e. Report router infinite loop** — Max handoff depth of 5 prevents agents from bouncing work indefinitely. At depth limit, forces `RouteActionNotifyUser` and presents accumulated report to all clients.

## File Changes Summary

**New files:**

| File | Track | Purpose |
|------|-------|---------|
| `internal/agent/report_router.go` | 1 | Execute route actions, multi-agent handoff loop |
| `internal/agent/progress_synthesizer.go` | 2 | Tiered progress event synthesis |
| `internal/mcp/server.go` | 1 | MCP server implementation |
| `internal/mcp/tools.go` | 1 | MCP tool definitions (sessions, send, events, status) |
| `internal/mcp/transport.go` | 1 | MCP protocol over stdin/stdout |
| `cmd/meept/mcp_chat_server.go` | 1 | CLI entry point for `meept mcp-chat-server` |

**Modified files:**

| File | Track | Change |
|------|-------|--------|
| `internal/agent/dispatcher.go` | 1 | Replace dead-end report handling with ReportRouter call |
| `internal/agent/handler.go` | 1 | Add `source_client` to ChatRequest, broadcast `chat.message.received` |
| `internal/agent/events.go` | 1+2 | Add new event types for synthesized progress and client messages |
| `pkg/models/bus.go` | 1 | Add `SourceClient` field to BusMessage |
| `internal/tui/models/chat.go` | 2 | Render participant badges, progress tiers |
| `internal/tui/app.go` | 2 | Add verbosity keybinding + status bar indicator |
| `internal/tui/config.go` | 2 | Load verbosity default from client.json5 |
| `config/client.json5` | 2 | Add `chat.verbosity` setting |
| `config/meept.json5` | 1 | Add `mcp_chat_server` section |
| `cmd/meept/main.go` | 1 | Register `mcp-chat-server` subcommand |

**Unchanged:** Session store, bus, agent loop, event emitter, LLM client, tools, memory system, security engine, strategic planner, tactical scheduler, orchestrator.

## Implementation Tracks

**Track 1 (core):** Client identity, bilateral visibility, MCP server, report router. Gets external agents talking to meept and agents actually handing off to each other.

**Track 2 (polish):** Progress synthesis, TUI verbosity tiers. Improves human-facing experience. Independent of Track 1.
