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

**Example:**
```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello", "conversation_id": "conv-123"}'
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

### Sessions

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/sessions` | List sessions |
| POST | `/api/v1/sessions` | Create session |
| GET | `/api/v1/sessions/{id}` | Get session |
| DELETE | `/api/v1/sessions/{id}` | Delete session |
| POST | `/api/v1/sessions/{id}/attach` | Attach to session |
| POST | `/api/v1/sessions/{id}/detach` | Detach from session |

### Workers

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/workers/stats` | Worker statistics |
| POST | `/api/v1/workers` | Add worker |
| DELETE | `/api/v1/workers/{id}` | Remove worker |
| POST | `/api/v1/workers/scale` | Scale workers |

### Skills

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/skills` | List skills |
| GET | `/api/v1/skills/{slug}` | Get skill details |
| POST | `/api/v1/skills/{slug}/execute` | Execute skill |

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

### Security

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/security/check` | Check action security |

### Scheduler

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/scheduler/jobs` | List scheduled jobs |
| POST | `/api/v1/scheduler/jobs` | Add scheduled job |

### Bus

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/bus/publish` | Publish message |
| GET | `/api/v1/bus/stats` | Bus statistics |

### Daemon Control

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/daemon/status` | Get daemon status |
| POST | `/api/v1/daemon/restart` | Restart daemon |

### Metrics

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/metrics/live` | Live metrics snapshot |
| GET | `/api/v1/metrics/historical` | Historical metrics |
| GET | `/api/v1/metrics/stream` | WebSocket metrics stream |

### Configuration

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/config/client` | Get client config |
| POST | `/api/v1/config/client` | Save client config |
| GET | `/api/v1/config/models` | Get models config |
| POST | `/api/v1/config/models` | Save models config |
| GET | `/api/v1/config/agents` | List agents |
| GET | `/api/v1/config/agents/{id}` | Get agent config |
| POST | `/api/v1/config/agents/{id}` | Save agent |
| DELETE | `/api/v1/config/agents/{id}` | Delete agent |

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

**Supported Message Types:**
- `ping` - Keep-alive, responds with `pong`
- `subscribe` - Subscribe to real-time updates

**Bus Event Broadcasting:**
When enabled, the WebSocket handler subscribes to the message bus and broadcasts events to all connected clients in real-time.

### MCP (Model Context Protocol)

| Method | Path | Description | Config |
|--------|------|-------------|--------|
| POST | `/mcp` | MCP JSON-RPC requests | `mcp: true` |
| GET | `/mcp/sse` | MCP Server-Sent Events stream | `mcp: true` |

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
| 500 | Internal Server Error |
| 501 | Not Implemented - Endpoint not yet implemented |
| 503 | Service Unavailable - Service not ready |

## OpenAPI Specification

Full OpenAPI 3.0 specification available at:
- `docs/reference/http-api/openapi.yaml`
