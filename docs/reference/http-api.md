# HTTP API Reference

The Meept HTTP API exposes full daemon functionality over REST for web/remote clients while preserving the existing RPC transport for CLI/TUI.

## Base URL

```
http://localhost:8081
```

## Authentication

API key authentication is available but disabled by default for local development. To enable:

1. Set `require_auth: true` in your HTTP server config
2. Configure API keys via the `api_keys` list

When enabled, include the API key in the Authorization header:

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" http://localhost:8081/api/v1/chat
```

## Endpoints

### Health

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |
| GET | `/api/v1/health` | Health check (versioned) |

### Chat

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/chat` | Send a chat message |
| GET | `/api/v1/chat/stream` | SSE stream of tool/agent progress events |
| GET | `/api/v1/chat/queue/{id}` | Get queue status for a conversation |
| POST | `/api/v1/chat/with-agent` | Send a message with agent steering |

**Example:**
```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello", "conversation_id": "conv-123"}'
```

**Chat Stream (SSE):**
`GET /api/v1/chat/stream` returns a Server-Sent Events stream subscribing to `tool.execution.progress`, `agent.progress`, and `tool.execution.complete` bus topics. Includes a 15-second heartbeat.

**IMPORTANT: SSE and WebSocket use different payload schemas for progress events.** SSE subscribes to the legacy `agent.progress` topic, while WebSocket subscribes to the newer `agent.progress.synthesized` topic. See [Transport-Specific Progress Schemas](#transport-specific-progress-schemas) below for details.

```bash
curl -N http://localhost:8081/api/v1/chat/stream
```

### Transport-Specific Progress Schemas

The agent progress event uses **two different payload schemas** depending on the transport mechanism: SSE (Server-Sent Events) uses the legacy schema, while WebSocket uses the synthesized schema from the ProgressSynthesizer.

#### SSE Progress Format (legacy)

SSE subscribes to the `agent.progress` bus topic directly. Payloads use the original event structure emitted by the agent loop:

```json
{
  "conversation_id": "abc-123",
  "iteration": 3,
  "stage": "executing",
  "detail": "ReadFile",
  "token_count": 245
}
```

Fields:
- `conversation_id` - the conversation/session the event belongs to
- `iteration` - the agent loop iteration number
- `stage` - current stage (e.g., `executing`, `llm`, `complete`, `error`)
- `detail` - specific action or tool name
- `token_count` - tokens used in this step

#### WebSocket Progress Format (synthesized)

WebSocket subscribes to the `agent.progress.synthesized` bus topic via the ProgressSynthesizer. Payloads use a session-scoped, human-readable format designed for real-time UI display:

```json
{"type": "agent_progress", "session_id": "abc-123", "agent_id": "coder", "message": "coder: executing ReadFile (internal/file/read)", "tier": 1, "source_event": "tool_execution_start", "timestamp": "2026-06-15T10:30:00Z"}
```

Fields:
- `type` - always `"agent_progress"` to distinguish from other WebSocket message types
- `session_id` - the session the progress event belongs to
- `agent_id` - the agent performing the action (e.g., `dispatcher`, `coder`, `analyst`)
- `message` - human-readable description of current activity
- `tier` - verbosity level: `0` (quiet), `1` (normal), `2` (verbose)
- `source_event` - original event type that triggered this synthesis (e.g., `tool_execution_start`, `tool_execution_complete`, `llm_response`)
- `timestamp` - RFC 3339 timestamp of the event

#### Schema Comparison

| Aspect | SSE (legacy) | WebSocket (synthesized) |
|--------|-------------|------------------------|
| Bus topic | `agent.progress` | `agent.progress.synthesized` |
| Format source | AgentLoop direct emission | ProgressSynthesizer |
| Session identifier | `conversation_id` | `session_id` |
| Agent identity | Not included | `agent_id` |
| Human-readable message | No (structured fields only) | Yes (`message` field) |
| Verbosity control | No | Yes (`tier` field) |
| Timestamp | No | Yes (`timestamp` field) |

**Chat with Agent:**
```bash
curl -X POST http://localhost:8081/api/v1/chat/with-agent \
  -H "Content-Type: application/json" \
  -d '{"message": "Analyze this code", "conversation_id": "conv-123", "source": "analyst"}'
```

### Memory

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/memory/query` | Search memories |
| GET | `/api/v1/memory/recent` | Get recent memories |
| POST | `/api/v1/memory/export` | Export memories |

**Example:**
```bash
curl -X POST http://localhost:8081/api/v1/memory/query \
  -H "Content-Type: application/json" \
  -d '{"query": "project setup", "limit": 10}'
```

### Memory Vector

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/memory/vector/search` | Semantic vector search over memories |
| POST | `/api/v1/memory/vector/store` | Store a memory vector entry |
| DELETE | `/api/v1/memory/vector/{id}` | Delete a memory vector entry by ID |
| GET | `/api/v1/memory/vector/stats` | Vector store statistics |

**Search:**
```bash
curl -X POST http://localhost:8081/api/v1/memory/vector/search \
  -H "Content-Type: application/json" \
  -d '{"query": "embedding query text", "limit": 10}'
```
Response: `{"results": [...]}`

**Store:**
```bash
curl -X POST http://localhost:8081/api/v1/memory/vector/store \
  -H "Content-Type: application/json" \
  -d '{"content": "memory text", "metadata": {"key": "value"}}'
```
Response: `{"status": "stored"}`

**Stats:**
```bash
curl http://localhost:8081/api/v1/memory/vector/stats
```

### Tasks

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/tasks` | List tasks |
| POST | `/api/v1/tasks` | Create task |
| GET | `/api/v1/tasks/{id}` | Get task by ID |
| PUT | `/api/v1/tasks/{id}` | Update task |
| DELETE | `/api/v1/tasks/{id}` | Delete task |
| POST | `/api/v1/tasks/{id}/cancel` | Cancel task |
| GET | `/api/v1/tasks/{id}/steps` | Get task steps |
| POST | `/api/v1/tasks/{id}/link-session` | Link a task to a session |
| POST | `/api/v1/tasks/{id}/unlink-session` | Unlink a task from a session |

**Link Session:**
```bash
curl -X POST http://localhost:8081/api/v1/tasks/task-123/link-session \
  -H "Content-Type: application/json" \
  -d '{"session_id": "sess-456"}'
```
Response: `{"status": "linked"}`

**Unlink Session:**
```bash
curl -X POST http://localhost:8081/api/v1/tasks/task-123/unlink-session \
  -H "Content-Type: application/json" \
  -d '{"session_id": "sess-456"}'
```
Response: `{"status": "unlinked"}`

### Queue

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/queue/jobs` | List jobs |
| POST | `/api/v1/queue/jobs` | Enqueue job |
| GET | `/api/v1/queue/jobs/{id}` | Get job by ID |
| POST | `/api/v1/queue/jobs/{id}/claim` | Claim next job |
| POST | `/api/v1/queue/jobs/{id}/complete` | Complete job |
| POST | `/api/v1/queue/jobs/{id}/fail` | Fail job |
| POST | `/api/v1/queue/jobs/{id}/retry` | Retry job |
| GET | `/api/v1/queue/stats` | Queue statistics |
| POST | `/api/v1/queue/steer` | Steer a conversation (convenience alias) |
| POST | `/api/v1/queue/followup` | Send a follow-up message (convenience alias) |
| GET | `/api/v1/queue/status/{id}` | Get queue status for a conversation (convenience alias) |

**Steer:**
```bash
curl -X POST http://localhost:8081/api/v1/queue/steer \
  -H "Content-Type: application/json" \
  -d '{"message": "try a different approach", "conversation_id": "conv-123"}'
```
Response: `{"status": "queued"}`

**Follow-up:**
```bash
curl -X POST http://localhost:8081/api/v1/queue/followup \
  -H "Content-Type: application/json" \
  -d '{"message": "what about edge cases?", "conversation_id": "conv-123"}'
```
Response: `{"status": "queued"}`

### Sessions

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/sessions` | List sessions |
| POST | `/api/v1/sessions` | Create session |
| GET | `/api/v1/sessions/most-recent` | Get the most recent session |
| GET | `/api/v1/sessions/{id}` | Get session |
| DELETE | `/api/v1/sessions/{id}` | Delete session |
| POST | `/api/v1/sessions/{id}/attach` | Attach to session |
| POST | `/api/v1/sessions/{id}/detach` | Detach from session |
| POST | `/api/v1/sessions/{id}/resume` | Resume a session |
| POST | `/api/v1/sessions/{id}/branch` | Branch to a point in the session tree |
| GET | `/api/v1/sessions/{id}/branches` | List branches for a session |
| POST | `/api/v1/sessions/{id}/fork` | Fork a session from a specific message |
| GET | `/api/v1/sessions/{id}/tree` | Get session tree structure |
| GET | `/api/v1/sessions/{id}/messages` | Get session messages (query params: `offset`, `limit`) |
| POST | `/api/v1/sessions/{id}/compact` | Trigger compaction on a session |

**Most Recent:**
```bash
curl http://localhost:8081/api/v1/sessions/most-recent
```

**Resume:**
```bash
curl -X POST http://localhost:8081/api/v1/sessions/sess-123/resume
```

**Branch:**
```bash
curl -X POST http://localhost:8081/api/v1/sessions/sess-123/branch \
  -H "Content-Type: application/json" \
  -d '{"target_message_id": 42}'
```

**Fork:**
```bash
curl -X POST http://localhost:8081/api/v1/sessions/sess-123/fork \
  -H "Content-Type: application/json" \
  -d '{"from_message_id": 42, "name": "experiment branch"}'
```
Response: `201 Created` with the new session object.

**Messages (paginated):**
```bash
curl "http://localhost:8081/api/v1/sessions/sess-123/messages?offset=0&limit=50"
```
Response: `{"messages": [...], "total": N}`

**Compact:**
```bash
curl -X POST http://localhost:8081/api/v1/sessions/sess-123/compact
```

### Workers

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/workers` | List workers |
| GET | `/api/v1/workers/stats` | Worker statistics |
| POST | `/api/v1/workers` | Add worker |
| DELETE | `/api/v1/workers/{id}` | Remove worker |
| POST | `/api/v1/workers/scale` | Scale workers |

### Skills

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/skills` | List skills |
| GET | `/api/v1/skills/{slug}` | Get skill details |
| GET | `/api/v1/skills/{slug}/ui` | Get skill UI descriptor |
| POST | `/api/v1/skills/{slug}/execute` | Execute skill |

**UI Descriptor:**
```bash
curl http://localhost:8081/api/v1/skills/my-skill/ui
```
Returns a UI descriptor object describing the skill's interface for frontend rendering.

### Self-Improve

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/selfimprove/status` | Get status |
| POST | `/api/v1/selfimprove/trigger` | Trigger improvement |
| POST | `/api/v1/selfimprove/analyze` | Analyze for improvements |
| POST | `/api/v1/selfimprove/generate` | Generate improvement |
| POST | `/api/v1/selfimprove/validate` | Validate improvement |
| POST | `/api/v1/selfimprove/apply` | Apply improvement |
| POST | `/api/v1/selfimprove/reject` | Reject improvement |

### Cache

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/cache/stats` | Cache statistics |
| POST | `/api/v1/cache/clear` | Clear cache |
| POST | `/api/v1/cache/invalidate` | Invalidate specific cache entries |
| GET | `/api/v1/cache/inspect` | Inspect cache entry by hash (query param: `hash`) |

**Invalidate:**
```bash
curl -X POST http://localhost:8081/api/v1/cache/invalidate \
  -H "Content-Type: application/json" \
  -d '{"keys": ["key1", "key2"]}'
```
Response: `{"status": "invalidated"}`

**Inspect:**
```bash
curl "http://localhost:8081/api/v1/cache/inspect?hash=abc123"
```

### Security

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/security/check` | Check action security |

### Scheduler

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/scheduler/jobs` | List scheduled jobs |
| POST | `/api/v1/scheduler/jobs` | Add scheduled job |
| DELETE | `/api/v1/scheduler/jobs/{id}` | Remove scheduled job |
| POST | `/api/v1/scheduler/jobs/{id}/enable` | Enable a scheduled job |
| POST | `/api/v1/scheduler/jobs/{id}/pause` | Pause a scheduled job |
| POST | `/api/v1/scheduler/jobs/{id}/resume` | Resume a paused job |

### Bus

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/bus/publish` | Publish message |
| POST | `/api/v1/bus/call` | Call an RPC method via the bus (requires RPC) |
| GET | `/api/v1/bus/stats` | Bus statistics |

**Bus Call (RPC proxy):**
```bash
curl -X POST http://localhost:8081/api/v1/bus/call \
  -H "Content-Type: application/json" \
  -d '{"method": "daemon.status", "params": {}}'
```
Response: `{"result": ...}` or `{"error": "..."}`

### Daemon Control

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/daemon/status` | Get daemon status |
| POST | `/api/v1/daemon/restart` | Restart daemon |
| POST | `/api/v1/daemon/start` | Start daemon |
| POST | `/api/v1/daemon/stop` | Stop daemon |

### Models

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/models` | List all configured models |
| GET | `/api/v1/models/providers` | List available model providers |
| GET | `/api/v1/models/default` | Get default model |
| POST | `/api/v1/models/default` | Set default model |
| DELETE | `/api/v1/models/{provider}/{model}` | Remove a model |
| GET | `/api/v1/models/credentials/{provider}` | Get credentials for a provider |
| POST | `/api/v1/models/credentials/{provider}` | Set credentials for a provider |
| DELETE | `/api/v1/models/credentials/{provider}` | Delete credentials for a provider |

**List Models:**
```bash
curl http://localhost:8081/api/v1/models
```
Response: `{"models": [...], "count": N}`

**Set Default:**
```bash
curl -X POST http://localhost:8081/api/v1/models/default \
  -H "Content-Type: application/json" \
  -d '{"provider": "openai", "model": "gpt-4"}'
```
Response: `{"status": "updated"}`

**Set Credential:**
```bash
curl -X POST http://localhost:8081/api/v1/models/credentials/openai \
  -H "Content-Type: application/json" \
  -d '{"api_key": "sk-..."}'
```
Response: `{"status": "updated"}`

### Runtime

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/runtime/status` | Get status of all runtime providers |
| GET | `/api/v1/runtime/status/{provider}` | Get status of a specific provider |
| POST | `/api/v1/runtime/start/{provider}` | Start a runtime provider |
| POST | `/api/v1/runtime/stop/{provider}` | Stop a runtime provider |
| POST | `/api/v1/runtime/restart/{provider}` | Restart a runtime provider |

Start, stop, and restart default to the `"local"` provider when not specified in the path.

**Start Provider:**
```bash
curl -X POST http://localhost:8081/api/v1/runtime/start/ollama
```
Response: `{"status": "started"}`

### Metrics

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/metrics/live` | Live metrics snapshot |
| GET | `/api/v1/metrics/historical` | Historical metrics (query params: `from`, `to`, `resolution`) |
| GET | `/api/v1/metrics/stream` | WebSocket/SSE metrics stream |
| GET | `/api/v1/metrics/rate-limits` | Rate limit summary |
| GET | `/api/v1/metrics/firewall` | Context firewall stats |

**Rate Limits:**
```bash
curl http://localhost:8081/api/v1/metrics/rate-limits
```

**Firewall Stats:**
```bash
curl http://localhost:8081/api/v1/metrics/firewall
```
Returns counters for summarization failures, dropped messages, compaction events, and tokens saved.

### Configuration

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/config/client` | Get client config |
| POST | `/api/v1/config/client` | Save client config |
| GET | `/api/v1/config/models` | Get models config |
| POST | `/api/v1/config/models` | Save models config |
| GET | `/api/v1/config/menubar` | Get menubar config |
| POST | `/api/v1/config/menubar` | Save menubar config |
| POST | `/api/v1/config/normalize` | Normalize JSON5 config content |
| GET | `/api/v1/config/agents` | List agents |
| GET | `/api/v1/config/agents/{id}` | Get agent config |
| POST | `/api/v1/config/agents/{id}` | Save agent |
| DELETE | `/api/v1/config/agents/{id}` | Delete agent |

### Agents

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/agents` | List agents known to the daemon |
| POST | `/api/v1/agents/{id}/delegate` | Delegate a task to a specific agent |

**List Agents:**
```bash
curl http://localhost:8081/api/v1/agents
```
Response: `{"agents": [...], "count": N}`

Each agent entry contains:
- `id` — agent ID (e.g., `coder`, `researcher`, `code-reviewer`)
- `name` — display name
- `role` — one of `Dispatcher`, `Executor`, `Reviewer`
- `description` — short description
- `enabled` — whether the agent is enabled
- `capabilities` — optional capability tags (omitted when empty)

When the daemon is running with a live agent registry (default), the list reflects the discovered AGENT.md files (8 standard executors plus `researcher` and 5 reviewers, plus any user-defined). Falls back to a static 14-entry list if the registry is unavailable.

**Delegate Task:**
```bash
curl -X POST http://localhost:8081/api/v1/agents/coder/delegate \
  -H "Content-Type: application/json" \
  -d '{"message": "add a login form"}'
```
Returns 503 if agent delegation is not configured.

**Menubar Config:**
```bash
curl http://localhost:8081/api/v1/config/menubar
```
Response: `{"content": "{ ... }"}`

**Normalize Config:**
```bash
curl -X POST http://localhost:8081/api/v1/config/normalize \
  -H "Content-Type: application/json" \
  -d '{"content": "// my config\n{ key: value }"}'
```
Response: `{"normalized": "{\n  \"key\": \"value\"\n}"}`

### Calendar

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/calendar/events` | List events (query params: `time_min`, `time_max`, `max_results`) |
| GET | `/api/v1/calendar/events/{id}` | Get event by ID |
| POST | `/api/v1/calendar/events` | Create event |
| PUT | `/api/v1/calendar/events/{id}` | Update event |
| DELETE | `/api/v1/calendar/events/{id}` | Delete event |
| GET | `/api/v1/calendar/today` | Get today's events |
| GET | `/api/v1/calendar/upcoming` | Get upcoming events (query params: `duration`, `max_results`) |
| POST | `/api/v1/calendar/quickadd` | Quick-add event from natural language text |

**List Events:**
```bash
curl "http://localhost:8081/api/v1/calendar/events?time_min=2025-01-01T00:00:00Z&time_max=2025-01-31T23:59:59Z&max_results=20"
```

**Quick Add:**
```bash
curl -X POST http://localhost:8081/api/v1/calendar/quickadd \
  -H "Content-Type: application/json" \
  -d '{"text": "Meeting with team tomorrow at 3pm"}'
```
Response: `201 Created` with the created event object.

**Upcoming:**
```bash
curl "http://localhost:8081/api/v1/calendar/upcoming?duration=48h&max_results=5"
```

### Terminal

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/terminal/history` | Get command history (query param: `limit`) |
| POST | `/api/v1/terminal/exec` | Execute a shell command |
| GET | `/api/v1/terminal/sessions` | List terminal sessions |
| POST | `/api/v1/terminal/clear` | Clear command history |

**Execute:**
```bash
curl -X POST http://localhost:8081/api/v1/terminal/exec \
  -H "Content-Type: application/json" \
  -d '{"command": "ls -la", "working_dir": "/tmp"}'
```

**History:**
```bash
curl "http://localhost:8081/api/v1/terminal/history?limit=20"
```
Response: `{"history": [...], "count": N}`

### Templates

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/templates` | List templates (query param: `limit`) |
| GET | `/api/v1/templates/{name}` | Get template by name |
| POST | `/api/v1/templates/{name}/invoke` | Invoke a template with parameters |
| DELETE | `/api/v1/templates/{name}` | Delete a template (query param: `conversation_id` for session-scoped) |

**Invoke:**
```bash
curl -X POST http://localhost:8081/api/v1/templates/code-review/invoke \
  -H "Content-Type: application/json" \
  -d '{"params": {"file": "main.go"}}'
```

### Projects

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/projects` | List registered projects |
| GET | `/api/v1/projects/{id}` | Get project by ID |
| POST | `/api/v1/projects` | Register a project |
| DELETE | `/api/v1/projects/{id}` | Unregister a project |
| POST | `/api/v1/projects/{id}/sync` | Pull latest changes for a project |
| GET | `/api/v1/projects/{id}/status` | Get project git status |
| GET | `/api/v1/projects/{id}/branches` | List project branches |
| POST | `/api/v1/projects/{id}/checkout` | Checkout a branch |
| POST | `/api/v1/projects/detect` | Auto-detect project at a given path |

**Register:**
```bash
curl -X POST http://localhost:8081/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{"path": "/path/to/project", "name": "my-project"}'
```
Response: `201 Created` with the project object.

**Detect:**
```bash
curl -X POST http://localhost:8081/api/v1/projects/detect \
  -H "Content-Type: application/json" \
  -d '{"path": "/path/to/repo"}'
```

**Checkout:**
```bash
curl -X POST http://localhost:8081/api/v1/projects/my-project/checkout \
  -H "Content-Type: application/json" \
  -d '{"branch": "feature/new-ui"}'
```
Response: `{"status": "checked out"}`

### Search

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/search` | Cross-resource search |

**Search:**
```bash
curl -X POST http://localhost:8081/api/v1/search \
  -H "Content-Type: application/json" \
  -d '{"query": "auth middleware", "types": ["memory", "tasks"], "limit": 20}'
```
Response: `{"results": [...], "count": N}`

### Notifications

| Method | Path | Description |
|--------|------|-------------|
| GET | `/ws/notifications` | WebSocket for real-time notifications |
| GET | `/api/v1/notifications` | Poll notifications (query param: `since`) |

Notifications are available when a `NotificationEmitter` is configured. The WebSocket endpoint pushes events as they occur; the HTTP endpoint supports polling.

**WebSocket:**
```bash
wscat -c ws://localhost:8081/ws/notifications
```

**Poll (HTTP):**
```bash
curl "http://localhost:8081/api/v1/notifications?since=2025-06-10T00:00:00Z"
```
Response: `{"events": [...], "count": N}`. Defaults to the last hour when `since` is omitted.

**Notification Event Format:**
```json
{
  "id": "notif-123",
  "timestamp": "2025-06-10T12:00:00Z",
  "type": "info",
  "title": "task completed",
  "message": "task build-deploy finished successfully",
  "data": {},
  "agent_id": "coder",
  "task_id": "task-456",
  "session_id": "sess-789"
}
```

**Notification Types:** `info`, `success`, `warning`, `error`

### Plans

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/plans` | List plans (query params: `project_id`, `limit`) |
| POST | `/api/v1/plans` | Create plan |
| GET | `/api/v1/plans/{id}` | Get plan by ID |
| POST | `/api/v1/plans/{id}/approve` | Approve plan |
| POST | `/api/v1/plans/{id}/reject` | Reject plan |
| POST | `/api/v1/plans/{id}/confirm` | Confirm plan sign-off |
| POST | `/api/v1/plans/{id}/revise` | Revise plan |
| GET | `/api/v1/sessions/{id}/plans` | List plans for a session |

**Example:**
```bash
curl -X POST http://localhost:8081/api/v1/plans \
  -H "Content-Type: application/json" \
  -d '{"title": "refactor auth module", "description": "break auth into separate services", "project_id": "my-project"}'
```

### Agents (AI Employees)

Endpoints under `/api/v1/agents/*` manage AI employees — persistent, constitution-bound autonomous agents. These replace the legacy `/api/v1/bot/{id}/trigger` endpoint (hard cutover). See [AI Employees](../workflows/employees.md) for the full feature spec.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/agents` | List employees |
| POST | `/api/v1/agents` | Create employee (validates constitution) |
| GET | `/api/v1/agents/{id}` | Show employee detail |
| PATCH | `/api/v1/agents/{id}` | Update employee definition |
| DELETE | `/api/v1/agents/{id}` | Delete employee (stops if running) |
| POST | `/api/v1/agents/{id}/trigger` | Webhook trigger (existing semantics, moved from `/api/v1/bot/{id}/trigger`) |
| POST | `/api/v1/agents/{id}/pause` | Operator pause |
| POST | `/api/v1/agents/{id}/resume` | Operator resume (only un-pause path) |
| GET | `/api/v1/agents/{id}/constitution` | View constitution |
| PATCH | `/api/v1/agents/{id}/constitution` | Propose amendment (routes to Plan signoff) |
| GET | `/api/v1/agents/{id}/goals` | List goals with health |
| GET | `/api/v1/agents/{id}/goals/{gid}` | Goal detail |
| POST | `/api/v1/agents/{id}/goals/{gid}/plans/{pid}/approve` | Approve plan |
| POST | `/api/v1/agents/{id}/goals/{gid}/plans/{pid}/reject` | Reject plan |
| GET | `/api/v1/agents/{id}/audit` | Audit findings (filter: `?since=&severity=`) |
| POST | `/api/v1/agents/{id}/audit/{fid}/resolve` | Resolve finding |
| POST | `/api/v1/agents/migrate` | Run migration scan, returns proposed constitutions |

Authenticated via the existing API key mechanism when `require_auth: true`.

**Webhook trigger example:**

```bash
curl -X POST http://localhost:8081/api/v1/agents/ci-monitor/trigger \
  -H "Content-Type: application/json" \
  -d '{"event": "push", "ref": "refs/heads/main", "head_commit": {"id": "abc123"}}'
```

**List employees example:**

```bash
curl http://localhost:8081/api/v1/agents \
  -H "Authorization: Bearer $MEEPT_API_KEY"
```

### WebSocket

| Method | Path | Description | Config |
|--------|------|-------------|--------|
| GET | `/ws` | WebSocket connection for real-time events | `websocket: true` |

The WebSocket endpoint provides bidirectional real-time communication for the Flutter UI and other web clients.

**Connection:**
```bash
wscat -c ws://localhost:8081/ws
```

**Message Format:**
```json
{"type": "ping", "data": {}}
```

**Client → Server Messages:**
- `ping` - Keep-alive, responds with `pong`
- `subscribe` - Subscribe to real-time updates (supports `channel: "chat"`, `"progress"`, `"notifications"`)

**Server → Client Messages:**

**chat events:** Bus events are forwarded with their original topic as the `type` field.

**agent_progress:** Session-scoped progress messages from the agent progress synthesizer. These are emitted when the dispatcher and specialist agents perform work during request processing. Use the synthesized schema (see [WebSocket Progress Format](#websocket-progress-format-synthesized)) — this differs from the SSE progress format ([SSE Progress Format](#sse-progress-format-legacy)).

```json
{"type": "agent_progress", "session_id": "abc-123", "agent_id": "coder", "message": "coder: executing ReadFile (internal/file/read)", "tier": 1, "source_event": "tool_execution_start", "timestamp": "2026-06-15T10:30:00Z"}
```

Fields:
- `session_id` - the session the progress event belongs to
- `agent_id` - the agent performing the action (e.g., `dispatcher`, `coder`, `analyst`)
- `message` - human-readable description of current activity
- `tier` - verbosity level: `0` (quiet), `1` (normal), `2` (verbose)
- `source_event` - original event type that triggered this synthesis (e.g., `tool_execution_start`, `tool_execution_complete`, `llm_response`)
- `timestamp` - RFC 3339 timestamp of the event

**Bus Event Broadcasting:**
When enabled, the WebSocket handler subscribes to the message bus and broadcasts general bus events to all connected clients in real-time. The handler also subscribes to `agent.progress.synthesized` for session-scoped progress events (filtered by client subscription via `SubscribeSession`).

### MCP (Model Context Protocol)

| Method | Path | Description | Config |
|--------|------|-------------|--------|
| POST | `/mcp` | MCP JSON-RPC requests | `mcp: true` |
| GET | `/mcp/sse` | MCP Server-Sent Events stream | `mcp: true` |
| GET | `/api/v1/mcp/servers` | List configured MCP client servers + runtime stats | — |
| PUT | `/api/v1/mcp/servers/{name}/enabled` | Toggle a client server's enabled flag | — |

The MCP endpoints allow AI agents (Claude Code, Cline, etc.) to interact with meept via the Model Context Protocol.

**Initialize:**
```bash
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize"}'
```

**List Tools:**
```bash
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
```

**Call Tool:**
```bash
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"meept_sessions","arguments":{"action":"list"}}}'
```

**MCP Tools:**
- `meept_sessions` - Session management (list/create/attach)
- `meept_send` - Send messages to sessions
- `meept_events` - Poll bus events
- `meept_status` - Get daemon status
- `meept_session_history` - Get session message history

**SSE Stream:**
The `/mcp/sse` endpoint provides a stream of server-sent events for async notifications:
```bash
curl -N http://localhost:8081/mcp/sse
```

### MCP Server Management

These endpoints manage meept's own MCP *client* catalog (the servers meept launches as subprocesses). They are distinct from the `/mcp` JSON-RPC endpoint above, which exposes meept as an MCP *server* to external agents.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/mcp/servers` | List all configured MCP servers with runtime state and stats |
| PUT | `/api/v1/mcp/servers/{name}/enabled` | Toggle a server's enabled flag (persists atomically + reloads manager) |

**List Servers:**
```bash
curl http://localhost:8081/api/v1/mcp/servers
```
Response:
```json
{
  "servers": [
    {
      "config": {
        "name": "github",
        "enabled": false,
        "category": "vcs",
        "description": "github repos, issues, prs",
        "type": "stdio",
        "command": ["npx", "-y", "@modelcontextprotocol/server-github"]
      },
      "stats": {
        "state": "disabled",
        "requests": 0,
        "errors": 0
      }
    }
  ]
}
```

Each entry is a `ServerStatusEntry` pairing a `config` (the on-disk JSON5 entry) with a `stats` block. The `stats.state` field is one of `active`, `inactive`, `error`, `disabled`. Counters are in-memory only and reset on daemon restart.

**Set Enabled:**
```bash
curl -X PUT http://localhost:8081/api/v1/mcp/servers/github/enabled \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'
```
Response: the updated `ServerStatusEntry` for that server.

The handler reads the on-disk config fresh, mutates only the named entry's `Enabled` field, writes the file atomically (temp file + rename), then triggers `Manager.Reload`. Lost-update is avoided because the on-disk file — not a cached copy — is the source of truth for the write.

## Error Responses

All errors return JSON with an `error` field:

```json
{"error": "error message here"}
```

| Status Code | Description |
|-------------|-------------|
| 400 | Bad Request - Invalid input |
| 401 | Unauthorized - Missing or invalid API key |
| 404 | Not Found - Resource doesn't exist |
| 405 | Method Not Allowed |
| 500 | Internal Server Error |
| 501 | Not Implemented - Endpoint not yet implemented |
| 503 | Service Unavailable - Service not ready |

## OpenAPI Specification

Full OpenAPI 3.0 specification available at:
- `docs/reference/http-api/openapi.yaml`
