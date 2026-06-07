# Persistent Bot Framework

Meept supports persistent autonomous bots that execute on schedules and respond to events. Bots have independent memory, cost isolation, and security boundaries.

## Overview

A persistent bot is an autonomous agent that runs without direct user interaction. Bots are triggered by:

- **Cron schedules** - Recurring time-based execution (e.g., every 15 minutes)
- **Bus events** - React to internal system events (e.g., calendar reminders, task completion)
- **Webhooks** - HTTP POST triggers from external systems (e.g., CI status, GitHub webhooks)

## Bot Definition

A bot is defined by a JSON document with these fields:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | Unique identifier |
| `name` | string | no | Human-readable name |
| `description` | string | no | What the bot does |
| `prompt` | string | yes | System prompt / behavioral instructions |
| `model` | string | no | Model alias or direct reference (empty = default) |
| `triggers` | array | yes | List of triggers (at least one required) |
| `memory_scope` | string | no | Memory isolation mode (default: "private") |
| `tools` | array | no | Tools the bot can use |
| `constraints` | object | no | Operational limits |
| `enabled` | boolean | no | Whether the bot is active (default: true) |

The `created_at` and `updated_at` timestamps are set automatically by the system.

## Trigger Types

### Cron Trigger

Executes the bot on a recurring schedule using standard cron expressions:

```json
{
  "type": "cron",
  "schedule": "*/15 * * * *",
  "enabled": true
}
```

Schedules are validated using the standard cron parser (minute, hour, day-of-month, month, day-of-week). Invalid schedules are rejected at bot creation time.

### Bus Event Trigger

Activates the bot when a message is published to a specific bus topic:

```json
{
  "type": "bus_event",
  "topic": "calendar.reminder",
  "prompt_template": "Calendar event: {{.summary}} starts in {{.starts_in}}",
  "enabled": true
}
```

The `prompt_template` field supports `{{.key}}` substitution from the event payload. If no template is provided, a default message is generated from the topic and source.

The `EventActionRouter` subscribes to bus topics and routes matching events to registered bots. When a bot is deleted or paused, its subscriptions are automatically cleaned up.

### Webhook Trigger

Exposes an HTTP endpoint that triggers the bot:

```json
{
  "type": "webhook",
  "prompt_template": "New PR #{{.number}}: {{.title}} by {{.user.login}}",
  "enabled": true
}
```

Webhook endpoint: `POST /api/v1/bot/{bot_id}/trigger`

The webhook handler accepts JSON payloads up to 1MB. If the webhook trigger defines a `prompt_template`, the payload fields are substituted into the template to build the trigger context. If the bot is paused or its budget is exhausted, the handler returns HTTP 429 (Too Many Requests).

## Memory Isolation

Bots can be configured with three memory isolation modes via the `memory_scope` field:

| Mode | Description |
|------|-------------|
| `private` | Bot can only read/write its own memories (default) |
| `shared` | Bot can read and write all memories (no prefix isolation) |
| `read_only` | Bot can read shared memories but only write to its own |

Memory isolation is implemented by `MemoryNamespace`, which prefixes all bot memory with `bot:{id}`. The `ScopeQuery` method adjusts queries based on the memory scope:

- **private/read_only**: Queries are prefixed with the bot's namespace to isolate results
- **shared**: Queries run against the full memory store without prefix isolation

The `TagMemory` method automatically tags stored memories with the bot's ID for tracking.

## Budget and Limits

Control bot execution costs with constraints:

| Field | Default | Description |
|-------|---------|-------------|
| `max_iterations` | 0 (unlimited) | Maximum reasoning cycles per invocation |
| `timeout` | 0 (unlimited) | Maximum duration per invocation |
| `max_tokens_per_turn` | 0 (unlimited) | Maximum tokens per LLM call |
| `daily_budget_cents` | 0 (unlimited) | Maximum daily spend in cents |
| `max_invocations_per_day` | 0 (unlimited) | Maximum daily invocations |

### Auto-Pause Behavior

Bots auto-pause after **10 consecutive failures** (`maxConsecutiveFailures`). The `BotRunner.ShouldRun()` method checks the bot's `BotState` before each invocation and returns `false` if:

- `consecutive_failures >= 10`
- Today's cost has exceeded `daily_budget_cents`
- Today's invocation count has exceeded `max_invocations_per_day`

The budget counter resets daily based on the `today_date` field in `BotState`.

### Runtime State Tracking

Each bot maintains a `BotState` with runtime counters:

| Field | Description |
|-------|-------------|
| `status` | Current status: running, paused, error, stopped |
| `last_run_at` | Timestamp of the most recent invocation |
| `last_error` | Error message from the most recent failure |
| `total_runs` | Cumulative invocation count |
| `total_tokens_used` | Cumulative token usage |
| `total_cost_cents` | Cumulative cost |
| `consecutive_failures` | Failure streak counter |
| `today_runs` | Invocations today |
| `today_cost_cents` | Spend today (cents) |
| `today_date` | Date string for daily budget tracking |

## System Prompt Construction

The `BotRunner` constructs the system prompt for each invocation by combining:

1. The bot's `prompt` field
2. Bot identity (ID and name)
3. Description
4. Current trigger context
5. Timestamp
6. Instructions for autonomous behavior

Example constructed prompt:

```
{user-defined prompt}

## Bot Identity
You are bot "ci-monitor" (CI Pipeline Monitor).
Description: Monitors CI pipeline status and reports failures

## Current Invocation
Trigger context: Webhook triggered with payload: ...
Timestamp: 2026-06-06T12:00:00Z

## Instructions
Perform your task and store any important observations in memory for future invocations.
Be concise. You are running autonomously - there is no user to interact with.
```

## Architecture

The bot framework consists of these components:

| Component | File | Description |
|-----------|------|-------------|
| **BotDefinition** | `internal/bot/types.go` | Configuration for a bot (triggers, prompt, constraints) |
| **BotTrigger** | `internal/bot/types.go` | Trigger configuration with validation |
| **BotConstraints** | `internal/bot/types.go` | Operational limits (budget, iterations, timeout) |
| **BotState** | `internal/bot/types.go` | Runtime state tracking (runs, costs, failures) |
| **BotRunner** | `internal/bot/runner.go` | Executes a single bot invocation with budget enforcement |
| **MemoryNamespace** | `internal/bot/memory_scope.go` | Isolates bot memory with `bot:{id}` prefix |
| **BotStore** | `internal/bot/store.go` | SQLite-backed persistence for bot definitions and state |
| **EventActionRouter** | `internal/bot/router.go` | Subscribes to bus topics and routes events to bots |
| **Manager** | `internal/bot/lifecycle.go` | Orchestrates lifecycle: create, start, stop, pause, resume |
| **WebhookHandler** | `internal/bot/webhook.go` | HTTP endpoint for webhook triggers |
| **RPCHandler** | `internal/bot/handler.go` | JSON-RPC handlers for bot management |

### Lifecycle Flow

```
BotDefinition (JSON)
    |
    v
Manager.CreateBot() --> Store.Create() --> SQLite (bot_definitions + bot_states)
    |
    v
Manager.startBot() --> NewBotRunner() --> EventActionRouter.Register()
    |                                        |
    |                                        +--> Cron triggers (scheduled execution)
    |                                        +--> Bus event triggers (topic subscription)
    |                                        +--> Webhook triggers (HTTP endpoint)
    v
BotRunner.ShouldRun() --> Budget/failure check
    |
    v
BotRunner.BuildSystemPrompt() --> LLM invocation
    |
    v
BotState update --> Store.UpdateState() --> counters/budget tracking
```

### Data Model

SQLite schema (managed by `BotStore`):

```sql
CREATE TABLE IF NOT EXISTS bot_definitions (
    id          TEXT PRIMARY KEY,
    data        TEXT NOT NULL,         -- JSON-encoded BotDefinition
    created_at  TEXT NOT NULL,         -- RFC3339 timestamp
    updated_at  TEXT NOT NULL          -- RFC3339 timestamp
);

CREATE TABLE IF NOT EXISTS bot_states (
    definition_id TEXT PRIMARY KEY REFERENCES bot_definitions(id) ON DELETE CASCADE,
    data          TEXT NOT NULL         -- JSON-encoded BotState
);
```

## RPC API

| Method | Description |
|--------|-------------|
| `bot.create` | Create a new bot (validates definition) |
| `bot.get` | Get bot definition by ID |
| `bot.list` | List all bots |
| `bot.update` | Update bot definition (validates, updates timestamp) |
| `bot.delete` | Delete a bot (stops if running, unregisters from router) |
| `bot.pause` | Pause a running bot (sets `enabled = false`) |
| `bot.resume` | Resume a paused bot (sets `enabled = true`) |
| `bot.status` | Get runtime state (running bots return live state, others return stored state) |

## HTTP API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/bot/{bot_id}/trigger` | POST | Trigger a bot via webhook |

Request body: JSON payload with arbitrary fields. If the webhook trigger has a `prompt_template`, payload fields are substituted.

Response:

```json
{
  "status": "triggered",
  "bot_id": "ci-monitor",
  "message": "bot invocation queued"
}
```

Error responses:
- `400` - Missing bot ID, invalid JSON, or bot has no webhook trigger
- `404` - Bot not found
- `405` - Non-POST method
- `429` - Bot paused or budget exhausted

## CLI Commands

```bash
# List all bots
meept bots list

# Show bot details
meept bots show <bot-id>

# Create a bot from a definition file
meept bots create bot-definition.json

# Pause a running bot
meept bots pause <bot-id>

# Resume a paused bot
meept bots resume <bot-id>

# Delete a bot
meept bots delete <bot-id>
```

## Examples

### CI Monitor Bot

```json
{
  "id": "ci-monitor",
  "name": "CI Pipeline Monitor",
  "description": "Monitors CI pipeline status and reports failures",
  "prompt": "You are a CI pipeline monitor. Check the CI status for the main project. If there are failures, store a summary in memory for the team to review. Be concise.",
  "triggers": [
    {
      "type": "cron",
      "schedule": "*/15 * * * *",
      "enabled": true
    },
    {
      "type": "webhook",
      "enabled": true
    }
  ],
  "memory_scope": "private",
  "tools": ["web_fetch", "memory_store", "memory_search"],
  "constraints": {
    "max_iterations": 5,
    "timeout": "2m",
    "daily_budget_cents": 50,
    "max_invocations_per_day": 100
  }
}
```

### Calendar Reminder Bot

```json
{
  "id": "calendar-bot",
  "name": "Calendar Assistant",
  "description": "Prepares summaries for upcoming calendar events",
  "prompt": "You are a calendar assistant. When you receive a calendar event reminder, check memory for any related context from previous meetings or tasks. Prepare a brief summary of what to expect.",
  "triggers": [
    {
      "type": "bus_event",
      "topic": "calendar.reminder",
      "prompt_template": "Upcoming event: {{.summary}} starting in {{.starts_in}}. Location: {{.location}}",
      "enabled": true
    }
  ],
  "memory_scope": "read_only",
  "tools": ["memory_search", "memory_store"],
  "constraints": {
    "max_iterations": 3,
    "timeout": "1m",
    "daily_budget_cents": 20
  }
}
```

### GitHub Webhook Bot

```json
{
  "id": "github-review-bot",
  "name": "PR Review Summarizer",
  "description": "Summarizes incoming pull requests and stores analysis",
  "prompt": "You are a code review assistant. When a new pull request arrives, fetch the diff, analyze the changes, and store a summary in memory. Focus on potential issues and improvement suggestions.",
  "triggers": [
    {
      "type": "webhook",
      "prompt_template": "New PR #{{.number}}: {{.title}} by {{.user.login}}. {{.body}}",
      "enabled": true
    }
  ],
  "memory_scope": "private",
  "tools": ["web_fetch", "memory_store", "memory_search"],
  "constraints": {
    "max_iterations": 5,
    "timeout": "3m",
    "daily_budget_cents": 100
  }
}
```

## See Also

- [Job Scheduling](job-scheduling.md) - Cron-based job scheduling for the agent system
- [Memory System](memory.md) - How memory storage and retrieval works
- [Security Engine](security.md) - Permission checks and tool gating
- [External Integrations](external-integrations.md) - Calendar, Telegram, and web API integrations
