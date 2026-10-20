# RPC API Reference

Meept uses JSON-RPC 2.0 over Unix sockets for communication between the CLI and platform.

## Overview

The RPC server runs on a Unix socket (default: `~/.meept/meept.sock`) and handles requests from CLI clients and other integrations.

## Connection Details

- **Protocol**: JSON-RPC 2.0
- **Transport**: Unix socket
- **Timeout**: 10 minutes per operation
- **Max idle**: 5 minutes
- **Frame format**: Length-prefixed JSON messages

## Authentication

Currently uses Unix socket permissions for authentication:
- Socket file permissions: `0600` (owner read/write only)
- Client must have access to socket file

## Methods

### Built-in Methods

#### `ping` - Health Check

Simple ping/pong for connectivity testing.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "ping",
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": "pong"
}
```

#### `status` / `daemon.status` - Platform Status (daemon process status)

Get comprehensive platform status information.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "status",
  "id": 2
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "status": "running",
    "version": "0.2.0-go",
    "uptime_seconds": 3600.5,
    "model": "claude-opus-4-5-20251101",
    "default_model": "claude-sonnet-4-5-20241022",
    "tokens_used": 15000,
    "tokens_remaining": 985000,
    "budget_used": 0.15,
    "budget_remaining": 9.85,
    "registered_methods": ["ping", "status", "chat", "chat.submit"],
    "bus_subscribers": 5
  }
}
```

#### `bus.publish` - Publish Message

Publish a message to the message bus.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "bus.publish",
  "id": 3,
  "params": {
    "topic": "agent.task.completed",
    "payload": {
      "task_id": "task-123",
      "status": "completed",
      "agent_id": "coder"
    }
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "delivered": 3
  }
}
```

#### `bus.stats` - Bus Statistics

Get message bus statistics.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "bus.stats",
  "id": 4
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "_total": 5,
    "agent.task.created": 2,
    "agent.task.completed": 1,
    "memory.stored": 10,
    "tool.executed": 50
  }
}
```

### Chat Methods

> `chat.send` and `chat.stream` were never registered handlers and have been
> removed from this page; the live chat surface is `chat` (legacy blocking
> proxy) and `chat.submit` (async ack + event delivery).

#### `chat` - Send Message (Legacy Sync Proxy)

Blocking round-trip: forwards the request onto the bus as `chat.request` and
waits for the matching `chat.response` (bounded by the chat proxy timeout).
Deprecated in favour of `chat.submit` + `turn.terminal` events.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "chat",
  "id": 5,
  "params": {
    "message": "What's the weather like?",
    "session_id": "session-abc123",
    "conversation_id": "conv-abc123",
    "agent_id": "chat"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "reply": "I don't have access to real-time weather data, but I can help you find weather APIs or build a weather application.",
    "conversation_id": "conv-abc123",
    "session_id": "session-abc123"
  }
}
```

#### `chat.submit` - Submit Message (Async Turn)

Fire-and-forget submit: validates, publishes `chat.request` with a `turn_id`,
and returns the ack immediately — it never waits for agent work. The result
arrives later on the `turn.terminal` bus topic (via `bus.subscribe`/`bus.poll`
or the WebSocket feed). There is no per-chunk RPC streaming; incremental
progress comes from `agent_progress` events on those subscriptions.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "chat.submit",
  "id": 6,
  "params": {
    "message": "Write a Go function to calculate Fibonacci",
    "session_id": "session-abc123",
    "agent_id": "coder",
    "turn_id": "turn-optional-for-idempotent-retry"
  }
}
```

**Response (ack):**
```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "result": {
    "turn_id": "turn-…",
    "conversation_id": "conv-…",
    "session_id": "session-abc123",
    "accepted": true,
    "note": ""
  }
}
```

Optional params: `conversation_id`, `source_client`, `model`, `parts`.
Passing an already-seen `turn_id` is an idempotent retry: the same ack
returns with `"accepted": true` and note `"turn already submitted; result
arrives via turn.terminal"` — the work is never published twice.

### Session Methods

> The plural `sessions.list` / `sessions.create` / `sessions.attach` names on
> this page were stale; the registered surface is singular `session.*`
> (only `sessions.archive` / `sessions.designated` /
> `sessions.designated_acknowledge` are plural).

#### `session.list` - List Sessions

Get list of sessions. Optional params: `{"limit": 50}`.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "session.list",
  "id": 7
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "result": {
    "sessions": [
      {
        "id": "session-abc123",
        "name": "refactor auth",
        "conversation_id": "conv-abc123",
        "created_at": "2026-04-18T10:30:00Z",
        "last_activity": "2026-04-18T10:45:00Z",
        "attached_clients": ["tui-1"],
        "project_id": "proj-1",
        "project_path": "/Users/example/app"
      },
      {
        "id": "session-def456",
        "name": "",
        "conversation_id": "conv-def456",
        "created_at": "2026-04-18T09:15:00Z",
        "last_activity": "2026-04-18T09:20:00Z",
        "attached_clients": []
      }
    ]
  }
}
```

#### `session.create` - Create Session

Create a new session. Params: `{"name": "...", "description": "...", "project_id": "...", "project_path": "...", "detection_context": {"cwd": "..."}, "no_fence": false}` — all optional. Project binding is per-session: explicit `project_id`/`project_path`, else the detection-context CWD resolved into a project, else the session stays unbound (never the global active project).

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "session.create",
  "id": 8,
  "params": {
    "name": "refactor auth",
    "detection_context": {"cwd": "/Users/example/app"}
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 8,
  "result": {
    "id": "session-ghi789",
    "name": "refactor auth",
    "conversation_id": "conv-ghi789",
    "created_at": "2026-04-18T11:00:00Z",
    "last_activity": "2026-04-18T11:00:00Z",
    "attached_clients": [],
    "project_id": "proj-1",
    "project_path": "/Users/example/app"
  }
}
```

#### `session.attach` - Attach to Session

Attach a client to an existing session.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "session.attach",
  "id": 9,
  "params": {
    "session_id": "session-abc123",
    "client_id": "cli-1"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 9,
  "result": {
    "status": "attached"
  }
}
```

### Job Methods

> The `jobs.*` names on this page were stale; the registered scheduler
> surface is `scheduler.*` (`list_jobs`, `add_job`, `remove_job`, `get_job`,
> `run_job`, `pause_job`, `resume_job`, `validate_schedule`, `status`).

#### `scheduler.list_jobs` - List Jobs

Get list of scheduled and running jobs. Optional params:
`{"type": "shell", "tags": ["ops"], "enabled": true}` (filters).

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "scheduler.list_jobs",
  "id": 10
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "result": {
    "jobs": [
      {
        "id": "job-123",
        "name": "Daily backup",
        "type": "shell",
        "schedule": "0 2 * * *",
        "enabled": true,
        "next_run": "2026-04-19T02:00:00Z",
        "last_run": "2026-04-18T02:00:00Z",
        "run_count": 3,
        "is_running": false
      }
    ],
    "count": 1
  }
}
```

#### `scheduler.add_job` - Create Job

Create a new scheduled job. Params: `{"name", "type", "schedule", "enabled", "tags", "id"}` plus exactly one type-specific config — `agent_config` (`{"prompt", "context", "model", "agent_id", "max_tokens", "temperature"}`), `shell_config` (`{"command", "args", "work_dir", "env", "timeout", "capture_output"}`), or `reminder_config`. Job types: `agent`, `shell`, `reminder`, `optimization`, `security`, `learning`, `interval`.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "scheduler.add_job",
  "id": 11,
  "params": {
    "name": "Health check",
    "schedule": "*/5 * * * *",
    "type": "agent",
    "enabled": true,
    "agent_config": {
      "prompt": "Check system health and report any issues"
    }
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "result": {
    "job_id": "job-456",
    "next_run": "2026-04-18T11:05:00Z"
  }
}
```

#### `scheduler.remove_job` - Cancel Job

Remove (cancel) a scheduled job.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "scheduler.remove_job",
  "id": 12,
  "params": {
    "job_id": "job-123"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "result": {
    "success": true,
    "job_id": "job-123"
  }
}
```

### Memory Methods

> `memory.search` was stale; the registered search twin is `memory.query`
> (plus `memory.recent` and `memory.vector.search`). `memory.store` was
> stale; content storage is registered on the epistemic surface as
> `memory.retainClaim` / `memory.retainDecision` / `memory.retainPrediction`.

#### `memory.query` - Search Memory

Search long-term memory. The bus consumer currently parses only
`{"query": "...", "limit": 10}` (limit default 10; empty query or `"*"`
returns recent memories). The richer `MemoryQuery` fields (`type`,
`category`, `domain`, `min_relevance`, `match_any`, `prefix_terms`) exist on
the internal search model, not all of them on this RPC path.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "memory.query",
  "id": 13,
  "params": {
    "query": "authentication patterns",
    "limit": 10
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "result": {
    "results": [
      {
        "id": "mem-abc123",
        "content": "We implemented JWT authentication middleware",
        "type": "task",
        "category": "code",
        "relevance_score": 0.85,
        "created_at": "2026-04-18T10:00:00Z",
        "source": "task:code"
      }
    ],
    "count": 1
  }
}
```

#### `memory.retainClaim` - Store Memory (Epistemic Claim)

Store a claim in long-term memory. Params:
`{"text", "premises", "source", "confidence", "tags"}`. Related:
`memory.retainDecision` (`{"call", "alternatives", "expected_outcome", "review_at", "premises"}`)
and `memory.retainPrediction` (`{"forecast", "horizon", "related_decision"}`).

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "memory.retainClaim",
  "id": 14,
  "params": {
    "text": "User prefers concise responses",
    "source": "observation",
    "confidence": 0.9,
    "tags": ["preferences"]
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 14,
  "result": {
    "id": "mem-def456"
  }
}
```

### Task Methods

> The plural `tasks.*` names on this page were stale; the registered surface
> is singular `task.*` (`create`, `get`, `list`, `list_extended`, `update`,
> `cancel`, `delete`, `link`, `unlink`, `steps`, plus direct `task.approve` /
> `task.reject` / `task.amend.submit`). Task status lives in the `state`
> field (pending, planning, awaiting_approval, executing, testing, completed,
> failed, rejected, cancelled).

#### `task.list` - List Tasks

Get list of background tasks. Optional params:
`{"state": "executing", "limit": 100}` (limit default 100).

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "task.list",
  "id": 15
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 15,
  "result": {
    "tasks": [
      {
        "id": "task-123",
        "name": "Fix authentication bug",
        "description": "JWT validation failing for expired tokens",
        "state": "executing",
        "assigned_agent": "coder",
        "created_at": "2026-04-18T09:00:00Z",
        "updated_at": "2026-04-18T10:30:00Z",
        "total_jobs": 3,
        "completed_jobs": 1,
        "failed_jobs": 0
      }
    ]
  }
}
```

#### `task.create` - Create Task

Create a new background task. Params: `{"name", "description"}` only — the
registry returns the full task object.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "task.create",
  "id": 16,
  "params": {
    "name": "Implement rate limiting",
    "description": "Add rate limiting middleware to API"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 16,
  "result": {
    "id": "task-456",
    "name": "Implement rate limiting",
    "description": "Add rate limiting middleware to API",
    "state": "pending",
    "created_at": "2026-04-18T11:00:00Z",
    "updated_at": "2026-04-18T11:00:00Z"
  }
}
```

#### `task.update` - Update Task

Update task properties. Params: `{"id", "name", "description", "state", "project_dir"}` — `id` selects the task; there is no notes field.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "task.update",
  "id": 17,
  "params": {
    "id": "task-123",
    "state": "completed"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 17,
  "result": {
    "id": "task-123",
    "state": "completed",
    "updated_at": "2026-04-18T11:15:00Z"
  }
}
```

#### `task.get` - Get Task

Get detailed task information.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "task.get",
  "id": 18,
  "params": {
    "id": "task-123"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 18,
  "result": {
    "id": "task-123",
    "name": "Fix authentication bug",
    "description": "JWT validation failing for expired tokens",
    "state": "completed",
    "project_dir": "/Users/example/app",
    "assigned_agent": "coder",
    "created_at": "2026-04-18T09:00:00Z",
    "updated_at": "2026-04-18T11:15:00Z",
    "goal_id": "goal-1",
    "total_jobs": 3,
    "completed_jobs": 3,
    "failed_jobs": 0
  }
}
```

### ACP Methods

#### `acp.list` - List ACP Agents

Reports the ACP client catalog and live session state. Registered even when the ACP manager is nil (disabled config): the result is then the `{"enabled": false, "agents": []}` envelope, never an error.

**Response (enabled):**
```json
{
  "jsonrpc": "2.0",
  "id": 21,
  "result": {
    "enabled": true,
    "agents": [
      {"id": "codex", "enabled": true, "running": true, "state": "ready"}
    ]
  }
}
```

#### `acp.permissions.list` - Pending Human-Relay Permissions

With `[acp] permission_mode = "human"` (or decision-lane escalation), each inbound `session/requestPermission` becomes a pending record until a person answers or `relay_timeout` fails it closed. This method returns the pending queue. Rows carry the additive `options` key (agent-offered `{optionId,name,kind}` labels; omitted when the agent offered none) so a reconnecting surface can render the `allow_always` affordance from the snapshot alone. Nil/disabled manager: `{"enabled": false, "pending": []}`.

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 22,
  "result": {
    "enabled": true,
    "pending": [
      {
        "id": "perm28ba51714f5ec50b",
        "agent_id": "codex",
        "session_id": "sess-…",
        "summary": "Agent: codex\nPermission request: {\"sessionId\":\"sess-…\"}",
        "created_at": "2026-10-19T21:44:02Z",
        "options": [
          {"optionId": "allow-once-1", "name": "Allow once", "kind": "allow_once"},
          {"optionId": "always-1", "name": "Always allow in session", "kind": "allow_always"}
        ]
      }
    ]
  }
}
```

#### `acp.permissions.resolve` - Answer a Permission Request

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "acp.permissions.resolve",
  "params": {"id": "perm28ba51714f5ec50b", "decision": "allow"},
  "id": 23
}
```

`decision` must be exactly `"allow"`, `"deny"`, or `"always"` — anything else is rejected at the RPC boundary (a typo never silently denies). `always` echoes the agent's own `allow_always` option when it offers one (otherwise it resolves exactly like `allow`; meept persists nothing — see [Acp](../workflows/acp.md) "Always-allow semantics"). One answer per id: a second resolve returns a `permission already answered` error; an unknown id returns `permission not found`. The resolved frame publishes to the `acp.permission.resolved` topic so every surface (TUI/GUI) dismisses the entry.

See [Acp](../workflows/acp.md) for the full relay flow and fail-closed semantics.

### Effects Methods

#### `effects.list` - Effect Ledger Records

Lists external-effect ledger records (oldest `claimed_at` first). Params: `{"state": "claimed"|"receipted"|"completed"|"abandoned", "key": "<effect key>"}` — both optional; empty state defaults to pending (claimed + receipted). Terminal states resolve only with a `key` (the pinned ledger surface cannot enumerate them; without a key the answer is an empty list). An unwired ledger answers `{"effects": [], "count": 0}`, never an error. CLI: `meept effects list`.

**Response:**
```json
{"jsonrpc": "2.0", "id": 24,
 "result": {"effects": [{"key": "…", "state": "claimed", "tool": "git_push", "task_id": "…", "session_id": "…", "provider_idempotent": false, "claimed_at": "2026-10-19T21:44:02Z"}], "count": 1}}
```

#### `effects.reconcile` - Reconcile One Record

Human reconciliation of a crash-left record. Params: `{"key": "<effect key>", "action": "complete"|"abandon", "receipt": <optional JSON receipt>, "reason": "<optional, abandon>"}`. `complete` records an optional receipt (claimed -> receipted) then completes; `abandon` records the reason. Nothing here re-executes the effect. Errors: `key is required`, `invalid action`, `effects service not available` (nil ledger). CLI: `meept effects reconcile <key> --complete` / `--abandon --reason '…'`.

See [Effects](../workflows/effects.md) for the ledger state machine.

### Routing Methods

#### `routing.recent` - Recent Routing Decisions

Last N model-routing decisions from the routing log. Params: `{"limit": 20}` (default 20). Result: `{"decisions": [...], "count": N}`. A daemon running without a routing log registers nothing — the method is absent, not empty.

#### `routing.by_model` - Decisions Filtered by Model

Params: `{"model_id": "<chosen model id>", "limit": 50}` — `model_id` required, `limit` default 50. Result: `{"decisions": [...], "count": N}`. CLI: `meept routing recent` / `meept routing by-model`.

See [Routing decisions](../workflows/routing-decisions.md).

## Error Handling

### Error Response Format

```json
{
  "jsonrpc": "2.0",
  "id": 999,
  "error": {
    "code": -32601,
    "message": "method not found: invalid.method"
  }
}
```

### Common Error Codes

- `-32600` - Invalid Request
- `-32601` - Method Not Found
- `-32602` - Invalid Parameters
- `-32603` - Internal Error
- `-32700` - Parse Error

## Client Libraries

### Go Client Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net"
    "encoding/json"
)

type RPCClient struct {
    conn net.Conn
}

func NewRPCClient(socketPath string) (*RPCClient, error) {
    conn, err := net.Dial("unix", socketPath)
    if err != nil {
        return nil, err
    }
    return &RPCClient{conn: conn}, nil
}

func (c *RPCClient) Call(ctx context.Context, method string, params any) (any, error) {
    req := map[string]any{
        "jsonrpc": "2.0",
        "method": method,
        "params": params,
        "id": 1,
    }

    data, err := json.Marshal(req)
    if err != nil {
        return nil, err
    }

    // Send request and read response
    // ... implementation details

    return result, nil
}

func main() {
    client, err := NewRPCClient("/home/user/.meept/meept.sock")
    if err != nil {
        log.Fatal(err)
    }

    status, err := client.Call(context.Background(), "status", nil)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Daemon status: %v\n", status)
}
```

## Security Considerations

- RPC server only accepts connections from local users with socket file access
- All operations are subject to security engine checks
- Sensitive operations require confirmation based on risk level
- Audit logging can be enabled for compliance