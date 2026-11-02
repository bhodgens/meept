# llm tracing (metrics.otlp_traces)

Every chat turn becomes one distributed trace: the turn itself, the llm
calls inside it, and the tool executions it performed, exported as
OTLP/HTTP spans to Jaeger, Grafana Tempo, or self-hosted Langfuse. The
same trace id is written to the local sqlite metrics store, so a slow or
expensive call found in SQL opens directly as a trace in your tracing UI.

The feature is opt-in and default-off: `metrics.otlp_traces.enabled =
false` (the default) constructs no exporter, no producer, and no bus
subscriptions - zero network activity, zero goroutines, and exactly one
boot-time Warn line. This is the documented opt-in posture (see
`docs/agents/opt-in-defaults.md`).

## what you get

One trace per chat turn (direct lane; see "known limitations" for async
dispatch). Four span types, all produced bus-side - the agent loop never
blocks on export:

| span | emitted when | carries |
|------|--------------|---------|
| `chat.turn` | a turn reaches terminal | `handler_case`, `status`, `turn_id`, `intent_type`, `task_id`, `model`, `error` (only on failure) |
| `task.step` | a task step completes or fails | `task_id`, `step_id`, `state` |
| `llm.call` | one completed llm call | GenAI attributes below |
| `tool.call` | one finished tool execution | GenAI tool attributes below |

Every span also carries shared base attributes: `trace_name`,
`gen_ai.system` (service name), `gen_ai.conversation.id`,
`langfuse.user.id` (agent id), and `langfuse.session.id` (session id,
falling back to the conversation id) - the attributes Langfuse filters
and grouping operate on.

### `llm.call` attributes

Token counts include cache and reasoning subsets per the GenAI
conventions: cache read/write and reasoning tokens are subsets of the
input/output totals, not additions. Zero-valued attributes are omitted.

| attribute | meaning |
|-----------|---------|
| `gen_ai.operation.name` | always `chat` |
| `gen_ai.provider.name` | provider (e.g. `openai`, `anthropic`) |
| `gen_ai.request.model` | model id |
| `gen_ai.usage.input_tokens` | total input tokens (includes cached) |
| `gen_ai.usage.output_tokens` | total output tokens (includes reasoning) |
| `gen_ai.usage.cache_read.input_tokens` | cache-read tokens |
| `gen_ai.usage.cache_write.input_tokens` | cache-write tokens |
| `gen_ai.usage.reasoning.output_tokens` | reasoning tokens |
| `error.type` | low-cardinality class on failure: `quota`, `refusal`, `empty_response`, `_OTHER` |
| `error.message` | truncated to 256 chars, only on failure (see below) |

On failure the span status is set to Error and the span status
DESCRIPTION carries the error message - the convention-preferred
channel. The `error.message` attribute is kept in addition as a
documented deviation so the SQL-join workflow (below) can read failures
without a tracing backend; `error.type` stays low-cardinality by
classification against meept's own sentinel failure paths, never by
parsing provider payload content.

### `tool.call` attributes

Argument VALUES never reach span attributes - only the argument key
NAMES and a byte count. Raw tool results are likewise never exported.

| attribute | meaning |
|-----------|---------|
| `gen_ai.operation.name` | always `execute_tool` |
| `gen_ai.tool.name` | tool name |
| `gen_ai.tool.call.id` | provider tool-call id |
| `meept.tool.args.keys` | argument key names (shape only) |
| `meept.tool.args.bytes` | serialized argument byte count |
| `meept.tool.blocked` | `"true"` only when the tool was blocked |
| `error.type` | `_OTHER` on failed executions |
| `error.message` | truncated to 256 chars, only on failure |

### span naming deviation (documented)

The GenAI conventions suggest span names shaped `{operation} {model}`
(e.g. `chat gpt-4o`). Meept keeps `llm.call` / `tool.call`:
low-cardinality dotted names consistent with the existing `chat.turn` /
`task.step` posture. The convention shape stays recoverable from
attributes (`gen_ai.operation.name` + `gen_ai.request.model` /
`gen_ai.tool.name`), so any consumer can rename on ingest.

## configuration

All knobs live in `meept.json5` under `[metrics.otlp_traces]`:

```json5
metrics: {
  otlp_traces: {
    enabled: true,                          // opt-in; default false
    endpoint: "http://localhost:4318",      // spans POST to <endpoint>/v1/traces
    headers: {                              // THE sanctioned secret channel
      // "authorization": "<your-auth-header>",
    },
    sample_rate: 1.0,                       // fraction of traces exported, 0..1
    export_interval: "5s",                  // batch flush cadence
  },
}
```

| field | type | default | notes |
|-------|------|---------|-------|
| `enabled` | bool | `false` | opt-in; false = one boot Warn, nothing else |
| `endpoint` | string | `http://localhost:4318` | base URL; normalized when empty |
| `headers` | map | - | extra HTTP headers on every export request |
| `sample_rate` | float | `1.0` | values outside (0,1] clamp to defaults |
| `export_interval` | duration | `5s` | zero/negative takes the default |

Headers are the sanctioned secret channel: auth tokens and ingestion
headers go here and are never logged, never placed on span attributes.
`sample_rate` is deterministic per trace id (the decision derives from
the trace id's own bytes), so all spans of one trace sample identically
- a partially sampled trace cannot originate from meept.

Disabled is explicit: `enabled = false` means no exporter, no span
producer, no bus subscriptions, zero network activity, and exactly one
Warn line at boot. Setting `sample_rate` to 0 does NOT disable (it
clamps to the default 1.0) - use `enabled = false`.

## export targets

Spans POST as OTLP/HTTP protobuf to `<endpoint>/v1/traces` (stdlib
`net/http` only - no otel SDK dependency). Any OTLP/HTTP receiver
works; three tested shapes:

### jaeger (all-in-one)

Jaeger ingests OTLP/HTTP on port 4318 out of the box:

```bash
docker run --rm -p 16686:16686 -p 4318:4318 \
  jaegertracing/all-in-one:latest
```

```json5
metrics: {
  otlp_traces: {
    enabled: true,
    endpoint: "http://localhost:4318",
  },
}
```

Open http://localhost:16686 and search by service or paste a trace id.

### grafana tempo

Tempo's OTLP/HTTP receiver also lives on 4318. Minimal `tempo.yaml`:

```yaml
server:
  http_listen_port: 3200
distributor:
  receivers:
    otlp:
      protocols:
        http:
          endpoint: "0.0.0.0:4318"
storage:
  trace:
    backend: local
    local:
      path: /tmp/tempo/blocks
```

```json5
metrics: {
  otlp_traces: {
    enabled: true,
    endpoint: "http://localhost:4318",
  },
}
```

Lookup flows that work with meept traces: paste the trace id into the
Grafana Tempo data source "TraceID" query type, call
`GET /api/traces/<traceid>` directly, or use TraceQL attribute search.
Trace-id lookup is the direct documented flow and works even when
attribute search indexes lag or the root span has not been received yet.

### self-hosted langfuse (v3+)

Langfuse accepts OTLP traces at `/api/public/otel` and requires the
ingestion-version header:

```json5
metrics: {
  otlp_traces: {
    enabled: true,
    endpoint: "http://langfuse:3000/api/public/otel",
    headers: {
      "x-langfuse-ingestion-version": "4",
      "authorization": "Basic <your-auth-header>",
    },
  },
}
```

The `langfuse.session.id` / `langfuse.user.id` / `gen_ai.conversation.id`
base attributes ride on every span, so Langfuse session grouping and
user filtering work without extra setup.

## correlating traces with sql

The local metrics sqlite store (`metrics.db`) persists the trace and
span identity of every llm call:

- `llm_calls.trace_id` / `llm_calls.span_id` - 32-hex / 16-hex TEXT
  columns, indexed on trace_id. The `span_id` is the SAME id the
  `llm.call` span exports under, so the SQL row and the trace span are
  one identity.
- `session_tool_calls.trace_id` / `session_tool_calls.span_id` - the
  same pair on tool-call rows.

sql first, trace second:

```sql
-- find the slow or expensive calls
SELECT latency_ms, model_id, tokens_sent, tokens_recv, trace_id
FROM llm_calls
WHERE latency_ms > 5000 OR is_error = 1
ORDER BY latency_ms DESC;

-- or look one call up directly
SELECT latency_ms, model_id, trace_id, span_id
FROM llm_calls
WHERE trace_id = 'a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6';
```

Then open the trace: paste the trace id into the Jaeger UI search field,
use the Grafana Tempo "TraceID" query type, or curl Tempo directly:

```bash
curl "http://localhost:3200/api/traces/<traceid>" -o trace.zip
```

The hex TEXT form is deliberate: it is the one representation every
human workflow (UI paste, Tempo API, TraceQL) accepts verbatim.

A `trace_id` of `''` means a legacy row (written before this feature
landed) or a pre-mint path (the turn failed before the trace id was
minted). Those rows have no trace to open.

## verification

The hermetic e2e suite proves the whole pipeline against a real daemon:

```bash
make e2e-fast-area AREA=llm-hop-tracing
```

It boots a sandbox daemon with `metrics.otlp_traces` pointed at an
in-suite OTLP capture receiver and asserts:

- one shared trace id across the `chat.turn`, `llm.call`, and
  `tool.call` spans on the wire;
- the GenAI attributes arrive with the names documented above;
- the `llm_calls.trace_id` / `span_id` SQL join matches the exported
  span identity byte-for-byte;
- the disabled boot stays quiet (exactly one Warn, zero network).

## troubleshooting

**armed boot but no spans arriving**

- check `sample_rate`: a value below 1.0 deterministically drops whole
  traces; at 0 it clamps to 1.0 (it does not disable - use
  `enabled = false`).
- check the receiver URL: spans POST to `<endpoint>/v1/traces`; a
  receiver that only serves gRPC on 4317 will refuse the HTTP POST.
- look for circuit-open Warns from the sink: a dead receiver produces
  at most one Warn per failure-streak threshold crossing and one Info on
  recovery; batches arriving while the circuit is open are dropped
  locally with a counter. The daemon keeps running - export is
  best-effort and never fails a turn.

**missing tool.call spans**

`tool.call` spans are produced from the `agent.event.tool_execution_end`
bus topic, emitted by the singleton loop's event emitter. Specialist
loops built by the multi-agent registry carry no emitter today (known
gap, tracked separately), so turns dispatched to those loops produce
`chat.turn` and `llm.call` spans but no `tool.call` spans.

**partially sampled traces**

Deterministic per-trace sampling keeps all-or-nothing per trace within
meept: the decision is a pure function of the trace id, made once, so a
trace missing only SOME of its spans did not lose them to sampling. If
you see partial traces, the cause is downstream: a collector's tail
sampling, ingestion drops, retention expiry, or per-span export
failures after the receiver.

## known limitations

- **async dispatch hops keep exporter-random traces.** Only the direct
  lane shares the turn's minted trace id. Hops dispatched through paths
  that do not carry the trace id (some async dispatch lanes) export
  under the exporter's random trace id - visible as separate one-span
  traces in the UI rather than corruption of the turn's trace.
- **hop spans export with empty parent span id (open question).** The
  turn span materializes only at turn terminal, so `llm.call` /
  `tool.call` spans currently export with an empty `ParentSpanID` and
  render as roots in Jaeger/Tempo. Correlate hops by trace id - every
  span of the turn shares one trace id, and trace-by-id lookup
  assembles all spans that share it regardless of parent linkage. If a
  receiver is later observed to misrender, the fallback (parenting hops
  on the minted turn span id) is a wiring change, not a schema one.

## privacy guarantees

- span attributes carry NO prompt text, NO completions, NO raw tool
  arguments, and NO raw tool results - only shapes (argument key names),
  counts (token counts, byte counts), and ids.
- error strings are truncated to 256 characters and classified into a
  low-cardinality `error.type` set; provider payload content is never
  parsed into attributes.
- secret material (auth tokens, ingestion keys) travels ONLY in
  `metrics.otlp_traces.headers` and is never logged.
- sampling, export failures, and circuit breaks never alter turn
  behavior: the agent loop never touches the exporter.

## related

- metrics overview (local sqlite store, `metrics.otlp` metric export):
  `docs/workflows/metrics.md`
- opt-in defaults policy: `docs/agents/opt-in-defaults.md`
- llm configuration reference: `docs/configuration/llm.md`
