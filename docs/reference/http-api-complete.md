# Meept HTTP API - Complete Reference

Comprehensive documentation for the Meept HTTP API, exposing full daemon functionality via REST.

## Base URL

```
http://localhost:8081
```

## Authentication

API key authentication is **enabled by default** with an intentionally obvious placeholder key.

**⚠️ SECURITY WARNING:** The default key `d@ng3r_NOT_A_Secure_key_REGENERATE_M3` must be changed before production use!

### Configure API Keys

1. **Generate a secure key:**
   ```bash
   openssl rand -base64 32
   ```

2. **Update your config** (`~/.meept/meept.json5`):
   ```json5
   {
     transport: {
       http: {
         enabled: true,
         require_auth: true,
         api_keys: ["your-generated-secure-key-here"]
       }
     }
   }
   ```

3. **Restart the daemon**

### Using Authentication

Include the API key in the `Authorization` header:

```bash
curl -H "Authorization: Bearer your-secure-key" http://localhost:8081/api/v1/chat
```

Or set as environment variable:
```bash
export MEEPT_API_KEY="your-secure-key"
curl -H "Authorization: Bearer $MEEPT_API_KEY" http://localhost:8081/api/v1/chat
```

### Default Configuration

The default config (`config/meept.json5`) includes:
```json5
transport: {
  http: {
    enabled: false,  // Enable for web/menubar clients
    require_auth: true,
    api_keys: ["d@ng3r_NOT_A_Secure_key_REGENERATE_M3"] // ⚠️ CHANGE THIS!
  }
}
```

The daemon will log a **security warning** at startup if you're using the default key.

---

## Health

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |
| GET | `/api/v1/health` | Health check (versioned) |

---

## Chat

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/chat` | Send a chat message |
| GET | `/api/v1/chat/stream` | Stream chat responses (SSE) |
| POST | `/api/v1/chat/steer` | Steer conversation |
| POST | `/api/v1/chat/followup` | Send follow-up |
| GET | `/api/v1/chat/queue/{id}` | Get chat queue status |
| POST | `/api/v1/chat/with-agent` | Chat with specific agent |

---

## Memory

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/memory/query` | Search memories |
| GET | `/api/v1/memory/recent` | Get recent memories |
| POST | `/api/v1/memory/export` | Export memories |

---

## Sessions

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/sessions` | List sessions |
| POST | `/api/v1/sessions` | Create session |
| GET | `/api/v1/sessions/{id}` | Get session |
| DELETE | `/api/v1/sessions/{id}` | Delete session |
| POST | `/api/v1/sessions/{id}/resume` | Resume session |
| POST | `/api/v1/sessions/{id}/compact` | Compact session |
| POST | `/api/v1/sessions/{id}/branch` | Create branch |
| POST | `/api/v1/sessions/{id}/fork` | Fork session |
| GET | `/api/v1/sessions/{id}/branches` | List branches |
| GET | `/api/v1/sessions/{id}/tree` | Get session tree |
| POST | `/api/v1/sessions/{id}/attach` | Attach to session |
| POST | `/api/v1/sessions/{id}/detach` | Detach from session |

---

## Tasks

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/tasks` | List tasks |
| POST | `/api/v1/tasks` | Create task |
| GET | `/api/v1/tasks/{id}` | Get task |
| PUT | `/api/v1/tasks/{id}` | Update task |
| DELETE | `/api/v1/tasks/{id}` | Delete task |
| GET | `/api/v1/tasks/{id}/steps` | Get task steps |
| POST | `/api/v1/tasks/{id}/cancel` | Cancel task |

---

## Queue

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/queue/jobs` | List jobs |
| GET | `/api/v1/queue/jobs/{id}` | Get job |
| POST | `/api/v1/queue/jobs` | Create job |
| POST | `/api/v1/queue/jobs/{id}/claim` | Claim job |
| POST | `/api/v1/queue/jobs/{id}/complete` | Complete job |
| POST | `/api/v1/queue/jobs/{id}/fail` | Fail job |
| POST | `/api/v1/queue/jobs/{id}/retry` | Retry job |
| GET | `/api/v1/queue/stats` | Queue statistics |
| GET | `/api/v1/queue/status/{id}` | Job status |
| POST | `/api/v1/queue/steer` | Steer job |
| POST | `/api/v1/queue/followup` | Job follow-up |

---

## Workers

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/workers/stats` | Worker statistics |
| POST | `/api/v1/workers` | Create worker |
| POST | `/api/v1/workers/scale` | Scale workers |
| DELETE | `/api/v1/workers/{id}` | Delete worker |

---

## Skills

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/skills` | List skills |
| GET | `/api/v1/skills/{slug}` | Get skill |
| POST | `/api/v1/skills/{slug}/execute` | Execute skill |

---

## Self-Improve

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/selfimprove/status` | Get status |
| POST | `/api/v1/selfimprove/analyze` | Analyze |
| POST | `/api/v1/selfimprove/generate` | Generate fix |
| POST | `/api/v1/selfimprove/validate` | Validate fix |
| POST | `/api/v1/selfimprove/apply` | Apply fix |
| POST | `/api/v1/selfimprove/reject` | Reject fix |
| POST | `/api/v1/selfimprove/trigger` | Trigger cycle |

---

## Cache

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/cache/stats` | Cache statistics |
| GET | `/api/v1/cache/inspect` | Inspect cache |
| POST | `/api/v1/cache/clear` | Clear cache |
| POST | `/api/v1/cache/invalidate` | Invalidate entry |

---

## Security

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/security/check` | Check permissions |

---

## Scheduler

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/scheduler/jobs` | List jobs |
| POST | `/api/v1/scheduler/jobs` | Create job |
| DELETE | `/api/v1/scheduler/jobs/{id}` | Remove job |
| POST | `/api/v1/scheduler/jobs/{id}/enable` | Enable/disable |
| POST | `/api/v1/scheduler/jobs/{id}/pause` | Pause job |
| POST | `/api/v1/scheduler/jobs/{id}/resume` | Resume job |

### Create Job Request
```json
{
  "id": "daily-backup",
  "name": "Daily Backup",
  "schedule": "0 0 * * *",
  "type": "agent",
  "agent_config": {
    "prompt": "Run daily backup",
    "model": "openai/gpt-4"
  }
}
```

---

## Bus

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/bus/publish` | Publish message |
| GET | `/api/v1/bus/stats` | Bus statistics |

---

## Config

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/config/client` | Get client config |
| POST | `/api/v1/config/client` | Save client config |
| GET | `/api/v1/config/models` | Get models config |
| POST | `/api/v1/config/models` | Save models config |
| GET | `/api/v1/config/menubar` | Get menubar config |
| POST | `/api/v1/config/menubar` | Save menubar config |
| GET | `/api/v1/config/agents` | List agents |
| GET | `/api/v1/config/agents/{id}` | Get agent |
| POST | `/api/v1/config/agents/{id}` | Save agent |
| DELETE | `/api/v1/config/agents/{id}` | Delete agent |

---

## Daemon

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/daemon/status` | Get daemon status |
| POST | `/api/v1/daemon/start` | Start daemon |
| POST | `/api/v1/daemon/stop` | Stop daemon |
| POST | `/api/v1/daemon/restart` | Restart daemon |

### Status Response
```json
{
  "status": "running",
  "pid": 12345,
  "uptime_seconds": 3600.5
}
```

---

## Models

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/models` | List all models |
| GET | `/api/v1/models/providers` | List providers |
| GET | `/api/v1/models/default` | Get default model |
| POST | `/api/v1/models/default` | Set default model |
| DELETE | `/api/v1/models/{provider}/{model}` | Remove model |
| GET | `/api/v1/models/credentials/{provider}` | Get credential (masked) |
| POST | `/api/v1/models/credentials/{provider}` | Set credential |
| DELETE | `/api/v1/models/credentials/{provider}` | Delete credential |

### Set Default Model Request
```json
{
  "provider": "openai",
  "model": "gpt-4"
}
```

---

## Calendar

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/calendar/events` | List events |
| GET | `/api/v1/calendar/events/{id}` | Get event |
| POST | `/api/v1/calendar/events` | Create event |
| PUT | `/api/v1/calendar/events/{id}` | Update event |
| DELETE | `/api/v1/calendar/events/{id}` | Delete event |
| GET | `/api/v1/calendar/today` | Get today's events |
| GET | `/api/v1/calendar/upcoming` | Get upcoming events |
| POST | `/api/v1/calendar/quickadd` | Quick add event |

### List Events Query Parameters
- `time_min` - Start time (RFC3339)
- `time_max` - End time (RFC3339)
- `max_results` - Maximum results (default: 50)

### Create Event Request
```json
{
  "summary": "Team Meeting",
  "description": "Weekly sync",
  "start": "2026-05-17T10:00:00Z",
  "end": "2026-05-17T11:00:00Z",
  "attendees": ["alice@example.com", "bob@example.com"]
}
```

**Note:** Calendar integration requires Google Calendar OAuth setup. Disabled by default.

---

## Metrics

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/metrics/live` | Live metrics |
| GET | `/api/v1/metrics/historical` | Historical data |
| GET | `/api/v1/metrics/stream` | Stream metrics (SSE) |
| GET | `/api/v1/metrics/firewall` | Firewall statistics |

---

## Error Responses

All endpoints return errors in this format:

```json
{
  "error": "Error message describing what went wrong"
}
```

### HTTP Status Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 201 | Created |
| 400 | Bad Request (invalid input) |
| 401 | Unauthorized (invalid/missing API key) |
| 404 | Not Found |
| 503 | Service Unavailable (service not initialized) |
| 500 | Internal Server Error |
