# Observability: Trace Spans + Critical Log Fixes

**Date**: 2026-05-08
**Status**: Draft
**Goal**: Answer "why is it doing X?" for any agent action, enabling both post-incident debugging and AI-assisted workflow analysis.

## Problem Statement

Meept has ~1,000 slog calls across 30+ packages, but reconstructing "what happened and why" for any given request is nearly impossible because:

1. **No unified correlation ID** — identifiers fragment across subsystems (conversation_id, task_id, job_id, step_id) with no single thread
2. **~20 critical decision points are silent** — agent routing, model selection, cache hits, budget gates, memory injection
3. **Metrics store silently swallows all errors** — zero slog calls in `internal/metrics/`
4. **HTTP auth is completely silent** — failed API key attempts produce no log
5. **No structured query tool** — even though rich data exists in SQLite, there's no CLI to reconstruct flows
6. **No JSON log option** — text-only output can't be machine-parsed
7. **No log rotation** — `meept.log` grows indefinitely

## Design

### Architecture

```
User Input
    │
    ▼
┌─────────────────────────────────────────────────────┐
│  TraceContext (trace_id propagated everywhere)       │
│  ┌───────────┐  ┌───────────┐  ┌────────────────┐  │
│  │ slog field│  │ trace span│  │ slog field      │  │
│  │ trace_id  │  │ → traces.db│  │ trace_id       │  │
│  └───────────┘  └───────────┘  └────────────────┘  │
│                                                     │
│  Dispatcher → Orchestrator → AgentLoop → Tools      │
│       ↓            ↓             ↓          ↓       │
│  [span: route] [span: plan] [span: reason] [span: exec]│
│       ↓            ↓             ↓          ↓       │
│  traces.db    traces.db    traces.db    traces.db   │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
              ┌──────────────────┐
              │ meept trace <id> │
              │ CLI command      │
              └──────────────────┘
```

### Component 1: Trace Context & Span System

**New package**: `internal/trace/`

**TraceContext** — a lightweight context value carrying a `trace_id` (UUID) and `span_id` (auto-incrementing within a trace). Propagated via Go `context.Context`.

```go
// internal/trace/context.go
type TraceContext struct {
    TraceID string
    // parent/child span tracking handled internally
}

func NewContext(ctx context.Context) context.Context    // creates trace_id
func FromContext(ctx context.Context) *TraceContext      // retrieves it
func WithSpan(ctx context.Context, span *Span) context.Context
```

**Span** — a structured record of a decision point or operation:

```go
// internal/trace/span.go
type Span struct {
    TraceID   string
    SpanID    string      // auto: trace_id.N
    ParentID  string      // parent span, empty for root
    Operation string      // e.g. "agent.route", "llm.call", "tool.execute"
    Input     string      // JSON or text summary (truncated to 2KB)
    Output    string      // JSON or text summary (truncated to 2KB)
    Decision  string      // the "why": "selected coder agent (confidence=0.9, matched keywords)"
    StartTime time.Time
    EndTime   time.Time
    Error     string      // empty if success
    Tags      map[string]string  // e.g. agent_id, model, tool_name
}
```

**Span lifecycle**: `trace.StartSpan(ctx, operation) → (*Span, context.Context)` returns a span and a new context with that span as parent. `span.End()` writes to `traces.db`.

### Component 2: Trace Store

**New file**: `~/.meept/traces.db` (SQLite, separate from metrics.db)

**Schema**:

```sql
CREATE TABLE traces (
    trace_id    TEXT PRIMARY KEY,
    root_operation TEXT NOT NULL,
    conversation_id TEXT,
    session_id      TEXT,
    created_at  DATETIME NOT NULL,
    completed_at DATETIME,
    status      TEXT NOT NULL,  -- 'running', 'completed', 'failed'
    user_input  TEXT,           -- truncated to 500 chars
    final_output TEXT           -- truncated to 500 chars
);

CREATE TABLE spans (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id    TEXT NOT NULL,
    span_id     TEXT NOT NULL,  -- e.g. "abc123.4"
    parent_id   TEXT,           -- null for root
    operation   TEXT NOT NULL,  -- e.g. "agent.route"
    input       TEXT,           -- truncated to 2KB
    output      TEXT,           -- truncated to 2KB
    decision    TEXT,           -- the "why"
    started_at  DATETIME NOT NULL,
    ended_at    DATETIME,
    duration_ms INTEGER,
    error       TEXT,
    tags        TEXT,           -- JSON
    FOREIGN KEY (trace_id) REFERENCES traces(trace_id)
);

CREATE INDEX idx_spans_trace ON spans(trace_id);
CREATE INDEX idx_spans_operation ON spans(operation);
CREATE INDEX idx_spans_started ON spans(started_at);

-- Retention: auto-purge traces older than 7 days (configurable)
CREATE TABLE trace_config (
    key   TEXT PRIMARY KEY,
    value TEXT
);
```

**Write path**: Spans are buffered in memory and flushed every 5 seconds (same pattern as metrics store). Uses a single-writer SQLite connection with WAL mode.

**Retention**: A background goroutine purges traces older than `trace.retention_days` (default 7). Configurable.

### Component 3: Instrumentation Points

These are the ~20 decision points that need span emission. Ordered by impact:

**Tier 1 — Must-have (request flow backbone):**

| Location | Operation | What to capture |
|----------|-----------|-----------------|
| `agent/dispatcher.go` — `Dispatch()` | `dispatch.route` | Input summary, selected agent, confidence, routing reason |
| `agent/orchestrator.go` — `handlePlanRequest()` | `orchestrator.plan` | Task intent, plan strategy, step count |
| `agent/strategic.go` — `Decompose()` | `plan.strategic` | Decomposition decision (fast-path vs full), step details |
| `agent/loop.go` — `RunOnce()` | `agent.reasoning` | Iteration count, model used, token budget state |
| `agent/loop.go` — LLM call | `llm.call` | Model, token counts, latency, cache hit/miss |
| `agent/loop.go` — tool execution | `tool.execute` | Tool name, args summary, result summary, security decision |
| `agent/tactical.go` — `scheduleStep()` | `tactical.schedule` | Step assignment: which agent, which job, dependencies |
| `llm/resolver.go` — `Resolve()` | `llm.resolve` | Required capabilities, resolved model+provider, escalation reason |

**Tier 2 — High-value (decision visibility):**

| Location | Operation | What to capture |
|----------|-----------|-----------------|
| `llm/client.go` — cache path | `llm.cache` | Hit/miss, cache key summary |
| `llm/budget.go` — `CheckBudget()` | `llm.budget` | Budget state, allowed/denied, remaining tokens |
| `memory/manager.go` — context injection | `memory.inject` | Memories injected, count, relevance scores |
| `security/engine.go` — `Check()` | `security.check` | Already logged to decision_log — emit span with decision summary |
| `agent/dispatcher.go` — classification | `dispatch.classify` | Intent type, classification chain results, confidence scores |
| `agent/review_manager.go` — review | `review.decide` | Auto-approve vs human, confidence, reviewer |
| `agent/escalation.go` — escalate | `agent.escalate` | Level, reason, re-plan decision |

**Tier 3 — Nice-to-have (completeness):**

| Location | Operation | What to capture |
|----------|-----------|-----------------|
| `skills/` — skill matching | `skill.match` | Matched skills, confidence scores |
| `llm/context_firewall.go` — compress | `context.compress` | Trigger reason, before/after token counts |
| `agent/session_tracker.go` — outcome | `session.outcome` | Completed/partial/failed, metrics summary |
| `queue/store.go` — job lifecycle | `queue.job` | Enqueue → claim → complete/fail, duration |

### Component 4: Slog Integration

**Trace ID as slog field**: Every slog call made within a `context.Context` that carries a `TraceContext` automatically includes `trace_id`. This is done via a custom `slog.Handler` wrapper:

```go
// internal/trace/slog_handler.go
type traceHandler struct {
    inner slog.Handler
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
    if tc := FromContext(ctx); tc != nil {
        r.AddAttrs(slog.String("trace_id", tc.TraceID))
    }
    return h.inner.Handle(ctx, r)
}
```

This means existing slog calls automatically gain `trace_id` — no changes to individual log statements needed. Just wrap the handler at daemon initialization.

**JSON log format option**: Add `log_format: "json"` config option. When set, use `slog.NewJSONHandler` instead of `slog.NewTextHandler`. The trace handler wraps whichever format is chosen.

### Component 5: Log Rotation

**Log rotation for `meept.log`**: Use `gopkg.in/natefinch/lumberjack.v2` (or a simple size-based rotation using stdlib) to rotate `~/.meept/meept.log` at 50MB with 3 backups.

Config:
```json5
{
  daemon: {
    log_rotation_size_mb: 50,
    log_rotation_backups: 3,
  }
}
```

### Component 6: Critical Log Fixes

Targeted fixes to the worst logging gaps (not spans, just slog fixes):

| Fix | File | Change |
|-----|------|--------|
| HTTP auth failures | `internal/comm/http/auth.go` | Add `slog.Warn("auth failed")` with remote addr, path |
| Metrics store errors | `internal/metrics/store.go` | Add logger, log DB write failures at Error level |
| RPC method dispatch | `internal/rpc/server.go` | Log method name + duration at Debug level |
| Service layer errors | `internal/services/*.go` | Add logger, log errors at Warn level |
| Tool results | `internal/agent/executor.go` | Log result summary (truncated) at Debug level |
| Agent loop entry | `internal/agent/loop.go` | Log incoming message at Info level (with trace_id) |
| Bus publish | `internal/bus/bus.go` | Log topic at Debug level (not payload) |

### Component 7: `meept trace` CLI Command

**New command**: `meept trace <conversation_id|trace_id>`

Reads from `traces.db` + joins with existing stores to produce a complete timeline:

```
$ meept trace abc-123-def

Trace: abc-123-def
Status: completed
Duration: 12.4s
Conversation: conv-456
Session: sess-789

Timeline:
  0.000s  dispatch.route     → agent=coder (confidence=0.92, matched: "write file")
  0.012s  dispatch.classify  → intent=coding, keywords=[file,create]
  0.045s  plan.strategic     → fast-path (simple request), 1 step
  0.048s  llm.resolve        → model=gpt-4o (requires: [code,tool_use])
  0.051s  llm.cache          → miss
  0.052s  llm.call           → gpt-4o, 1.2s, 450/180 tokens
  1.253s  security.check     → allow file_write (risk=medium, source=base_rule)
  1.254s  tool.execute       → write_file(/tmp/test.go), 23ms
  1.277s  llm.call           → gpt-4o, 0.8s, 380/120 tokens
  2.120s  memory.inject      → 3 episodic memories, 1 task memory
  2.451s  agent.reasoning    → completed in 4 iterations, 1010/300 tokens

Related data:
  Security decisions: 1 allow (see: meept audit query --conv conv-456)
  Jobs: job-001 (completed, 2.1s)
  Session messages: 4 (user:1, assistant:3)

$ meept trace --last          # trace the most recent conversation
$ meept trace --json <id>     # machine-readable output for AI analysis
$ meept trace --follow <id>   # stream new spans as they arrive (live debugging)
```

**Data sources**:
- `traces.db` — span timeline (primary)
- `session.db` — message content
- `security.db` — permission decisions (join on conversation_id)
- `queue.db` — job lifecycle (join on job_id from tags)
- `metrics.db` — token counts, latencies (join on trace_id from tags)

### Component 8: AI-Assisted Analysis Integration

The `--json` flag on `meept trace` produces structured output designed for LLM consumption:

```json
{
  "trace_id": "abc-123",
  "status": "completed",
  "duration_ms": 12400,
  "spans": [
    {"operation": "dispatch.route", "decision": "...", "duration_ms": 12, "tags": {...}},
    ...
  ],
  "related": {
    "security_decisions": [...],
    "jobs": [...],
    "token_usage": {"input": 450, "output": 180}
  }
}
```

This enables workflows like:
- Feed `meept trace --json <id>` to an LLM and ask "why did the agent choose coder instead of planner?"
- Batch-analyze traces to find patterns ("which routing decisions have low confidence?")
- Use in the self-improve system to identify suboptimal agent behavior

## Scope Exclusions

- Not replacing slog with a third-party library
- Not adding distributed tracing (OpenTelemetry, etc.)
- Not changing the metrics store schema
- Not adding real-time log streaming (the `--follow` flag reads from SQLite, not from a stream)
- Not instrumenting every function — only the ~20 decision points listed above

## Configuration

```json5
// ~/.meept/meept.json5
{
  trace: {
    enabled: true,                // Master switch
    db_path: "~/.meept/traces.db",
    retention_days: 7,            // Auto-purge older traces
    flush_interval_ms: 5000,      // Batch write interval
    max_span_input_bytes: 2048,   // Truncate span input/output
    max_span_output_bytes: 2048,
  },
  daemon: {
    log_format: "text",           // "text" or "json"
    log_rotation_size_mb: 50,
    log_rotation_backups: 3,
  }
}
```

## Implementation Phases

### Phase 1: Foundation
- `internal/trace/` package (TraceContext, Span, Store)
- `traces.db` schema and write path
- Custom slog handler for automatic `trace_id` injection
- JSON log format option
- Log rotation

### Phase 2: Core Instrumentation (Tier 1)
- Dispatcher: `dispatch.route`, `dispatch.classify`
- Orchestrator/Strategic: `orchestrator.plan`, `plan.strategic`
- Agent loop: `agent.reasoning`
- LLM: `llm.call`, `llm.resolve`
- Tools: `tool.execute`
- Tactical: `tactical.schedule`

### Phase 3: Extended Instrumentation (Tier 2)
- LLM cache, budget
- Memory injection
- Security check spans
- Review, escalation

### Phase 4: CLI + Analysis
- `meept trace` command
- `--json` output
- `--last` and `--follow` flags
- Cross-store joins (session, security, queue, metrics)

### Phase 5: Critical Log Fixes
- HTTP auth logging
- Metrics store error logging
- RPC method dispatch logging
- Service layer error logging
- Agent loop entry logging
- Tool result summary logging
- Bus publish topic logging

### Phase 6: Nice-to-have Instrumentation (Tier 3)
- Skill matching, context compression, session outcomes, queue lifecycle

## Testing Strategy

- Unit tests for `internal/trace/` package (store, span lifecycle, context propagation)
- Integration test: fire a message through the daemon, query `traces.db`, verify all expected spans appear in order
- Test `meept trace` CLI output format
- Test log rotation by creating a large log file
- Test retention purge by inserting old traces and verifying cleanup
