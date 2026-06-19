# metrics

**status**: implemented
**date**: 2026-06-18

## overview

meept collects time-series and event-level metrics for agent iterations, tool executions, llm requests, security events, and task outcomes. metrics are stored in a local sqlite database and exposed via the http api for live dashboards and historical queries.

## architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                         meept daemon                              │
│                                                                   │
│  ┌──────────────┐    ┌──────────────┐    ┌────────────────────┐   │
│  │ message bus  │───▶│  collector   │───▶│  store (sqlite)    │   │
│  │ (pub/sub)    │    │  (bus events │    │  ~/.meept/         │   │
│  │              │    │   + polling) │    │    metrics.db      │   │
│  └──────────────┘    └──────┬───────┘    └─────────┬──────────┘   │
│                             │                       │              │
│                             │              ┌────────▼──────────┐   │
│  ┌──────────────────────┐   │              │ taskcollector      │   │
│  │ agent event emitter  │───┘              │ (async buffer,     │   │
│  │ (typed events)        │                  │  agent_task_       │   │
│  └──────────────────────┘                  │   outcomes table)  │   │
│                                            └───────────────────┘   │
│                                                                    │
│  ┌──────────────────────────────────────┐                          │
│  │ http api                             │◀─── subscribers           │
│  │ /api/v1/metrics/live                 │                          │
│  │ /api/v1/metrics/historical           │                          │
│  └──────────────────────────────────────┘                          │
└────────────────────────────────────────────────────────────────────┘
```

**key properties:**
- sqlite single-file database at `~/.meept/metrics.db`
- batched writes with configurable batch size and flush interval
- hourly aggregation rolls up raw metrics and prunes old data
- pub/sub subscriber model for real-time metric snapshots
- separate `taskcollector` for async task outcome recording

## configuration

### main config (`~/.meept/meept.json5`)

```json5
{
  "llm": {
    "metrics": {
      "enabled": true,                        // master switch
      "db_path": "~/.meept/metrics.db",       // sqlite database path
      "retention_days": 7,                    // how long to keep raw data
      "stats_refresh_minutes": 5,             // how often to refresh stats
    },

    "adaptive_timeout": {
      "enabled": true,                        // adaptive timeouts based on metrics
      "stddev_multiplier": 3.0,               // timeout = avg + (multiplier * stddev)
      "stddev_token_rate_timeout": true,      // use token rate for timeout calc
      "min_timeout_seconds": 10,              // floor for adaptive timeout
      "max_timeout_seconds": 300,             // ceiling for adaptive timeout
      "warmup_requests": 20,                  // requests before adaptive kicks in
      "window_hours": 24,                    // lookback window for stats
    },
  },
}
```

### store defaults

| parameter | default | description |
|-----------|---------|-------------|
| `DatabasePath` | `~/.meept/metrics.db` | sqlite database path |
| `BatchSize` | 100 | metrics buffered before flush |
| `FlushInterval` | 10 seconds | max time between flushes |
| `RetentionDays` | 30 | raw data retention (hourly aggregates kept longer) |

## behavior

### metric collection

the `collector` gathers metrics from two sources:

1. **message bus subscriptions**: subscribes to `metrics`, `step.*`, `llm.request`, `llm.error`, `agent.iteration`, `tool.call`, `model.failover` topics
2. **typed agent events**: registered via `collector.registerEventListeners(emitter)` for async notifications on `after_provider_response`, `turn_end`, `session_end`, `tool_execution_start`, and `tool_execution_end` events
3. **polling**: a background goroutine polls `getQueueDepth()` and `getActiveAgents()` every 5 seconds

### batching and flushing

metrics are batched in memory and flushed to sqlite either when the batch reaches `BatchSize` or every `FlushInterval`. the flush swaps the batch under a lock, then writes to the database without holding the lock (per the mutex scope rule from `CLAUDE.md`).

### hourly aggregation

a background goroutine runs every hour to:
- aggregate raw `metrics_live` rows into `metrics_hourly` (sum, avg, min, max, count)
- delete raw data older than 24 hours (aggregates are retained)

### subscriber notifications

after each successful flush, the store notifies all subscribers with a fresh `LiveMetricsSnapshot`. subscribers receive updates via a buffered channel (capacity 10).

### task outcome collection

the `taskcollector` provides async, buffered persistence of `AgentTaskMetrics` to the `agent_task_outcomes` table:

- metrics are queued via a 1000-deep channel
- a 5-second flush ticker drains the queue and writes in a single transaction
- `Shutdown()` triggers a final flush and waits via `WaitGroup`
- if the queue is full, the metric is dropped with a warning log

**agent_task_outcomes fields:**

| field | type | description |
|-------|------|-------------|
| `task_id` | text | task identifier |
| `agent_id` | text | agent that processed the task |
| `skill_name` | text | skill used |
| `status` | text | completed, failed, timeout, abandoned |
| `success` | boolean | whether the task succeeded |
| `iterations` | integer | agent loop iterations |
| `duration_ms` | integer | total execution time |
| `tokens_input` | integer | input tokens consumed |
| `tokens_output` | integer | output tokens produced |
| `estimated_cost_cents` | real | estimated cost in cents |
| `response_well_formed` | boolean | response passed quality checks |
| `lazy_response_detected` | boolean | response was lazy/abbreviated |
| `model_id` | text | model used |
| `edit_format` | text | edit format (editblock, udiff, etc.) |

### adaptive timeouts

meept implements adaptive timeouts based on historical llm performance. the `store.GetAverageStepDuration(agentType)` method queries the `step.duration` metric from the last 24 hours to compute average execution time per agent type. the adaptive timeout system uses this data with a standard deviation multiplier to set dynamic timeouts:

- `WarmupRequests`: number of requests before adaptive timeouts activate (default: 20)
- `StddevMultiplier`: multiplier for standard deviation (default: 3.0)
- `MinTimeoutSeconds` / `MaxTimeoutSeconds`: floor and ceiling for computed timeout

## database schema

| table | purpose |
|-------|---------|
| `metrics_live` | 1-second resolution time-series metrics |
| `metrics_hourly` | aggregated hourly stats (sum, avg, min, max, count) |
| `events` | discrete event log (info, warn, error) |
| `response_quality` | llm response quality analysis results |
| `model_performance` | aggregated model performance per period |
| `error_records` | error records for retry tracking |
| `lint_runs` | lint run results for auto-lint reflection |
| `test_runs` | test run results for auto-test reflection |
| `agent_task_outcomes` | per-task outcome metrics (task collector) |
| `agent_errors` | agent execution errors with resolution tracking |

## http api

### live metrics

```
GET /api/v1/metrics/live
```

returns a `LiveMetricsSnapshot`:

```json
{
  "timestamp": "2026-06-18T12:00:00Z",
  "active_agents": 2,
  "requests_per_sec": 0.5,
  "token_usage_rate": 150.0,
  "queue_depth": 3,
  "model_failovers": 0
}
```

### historical metrics

```
GET /api/v1/metrics/historical?from=2026-06-17T00:00:00Z&to=2026-06-18T00:00:00Z&resolution=hour
```

query parameters:
- `from`: start time (RFC3339)
- `to`: end time (RFC3339)
- `resolution`: `minute`, `hour`, `day`, or `week` (default: `hour`)

returns a list of `MetricPoint` objects:

```json
{
  "points": [
    {
      "timestamp": "2026-06-17T12:00:00Z",
      "name": "agent.active",
      "value": 3.0,
      "tags": {"agent_id": "coder"}
    }
  ]
}
```

### metric stream (websocket)

```
GET /api/v1/metrics/stream
```

websocket endpoint that pushes `LiveMetricsSnapshot` updates after each flush.

### firewall stats

```
GET /api/v1/metrics/firewall
```

returns context firewall statistics (summarization failures, dropped messages, compaction events).

## implementation details

### go package: `internal/metrics/`

| file | purpose |
|------|---------|
| `store.go` | sqlite-backed metrics store, schema init, batching, aggregation, subscriber model |
| `collector.go` | message bus event collection, typed agent event listeners, `taskcollector` for async task outcomes |
| `analyzer.go` | response quality analysis (lazy detection, edit format identification, code token estimation) |

### key types

```go
type Store struct {
    // sqlite database, batch buffer, flush loop, subscriber management
}

type Collector struct {
    // bus subscriptions, store reference, polling goroutine
}

type TaskCollector struct {
    // async buffered flush of AgentTaskMetrics
    flushQueue  chan *AgentTaskMetrics  // 1000-deep
    flushTicker *time.Ticker            // 5-second interval
}

type ResponseAnalyzer struct {
    // lazy response detection, edit format identification
}
```

### response analyzer

the `ResponseAnalyzer` inspects llm responses for quality signals:

- **edit format detection**: identifies `editblock`, `editblock-fenced`, `udiff`, or plain output
- **lazy response detection**: regex patterns catch `// rest of code`, `# rest of the file`, `... existing code`, `// etc.`
- **code token percentage**: estimates what fraction of the response was code vs. explanation
- **well-formedness**: validates that edit blocks have matching `<<<<<<< SEARCH` / `>>>>>>> REPLACE` markers

### daemon wiring

the metrics store and collector are created during daemon component initialization:

1. `Store` is created via `NewStore(StoreConfig)` with the configured database path
2. `Collector` is created via `NewCollector(store, messageBus, CollectorConfig)` which starts polling and bus subscriptions
3. `Collector.RegisterEventListeners(emitter)` wires typed agent events
4. `TaskCollector` is created via `NewTaskCollector(dbPath, logger)` for async task outcome recording
5. the http server's `MetricsService` interface delegates to the store

## testing

```bash
# unit tests
go test ./internal/metrics/... -v

# with race detection
go test -race ./internal/metrics/... -v

# test http endpoints
go test ./internal/comm/http/... -v -run Metrics
```

## troubleshooting

**"failed to create metrics directory"**
- check that the parent directory of `db_path` is writable
- if using `~/.meept/metrics.db`, ensure `~/.meept/` exists or is creatable

**"metrics service not available" (http api)**
- verify `llm.metrics.enabled` is `true` in config
- check daemon logs for store initialization errors
- confirm the http transport is enabled in `transport.http.enabled`

**high memory usage from metrics**
- reduce `BatchSize` to flush more frequently
- lower `RetentionDays` to prune raw data sooner
- the hourly aggregation loop automatically deletes raw data older than 24 hours

## related

- llm management: `docs/workflows/llm-management.md`
- context firewall: `docs/workflows/context-firewall.md`
- job scheduling: `docs/workflows/job-scheduling.md`
- worker pool: `docs/workflows/worker.md`
- http api reference: `docs/reference/http-api.md`
