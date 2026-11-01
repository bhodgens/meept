# Leaf 00 Research Findings: OpenTelemetry GenAI Semantic Conventions

> Verified 2026-11-01 against the upstream OpenTelemetry GenAI semantic
> conventions sources listed at the bottom. This document pins the
> attribute names, span shapes, and correlation guidance that leaf 03
> implements. All claims carry a source URL. ASCII only.

## 1. Final Attribute Table (corrected C8)

Verification summary: every draft name in master.md C8 was checked
against the upstream registry and the client-inference / execute-tool
span specs. **No DRAFT-WRONG corrections are required** - the draft
already uses the current names (`gen_ai.provider.name`,
`gen_ai.usage.input_tokens`, etc.). Two upstream-reality refinements
are recorded below the tables (error attribute and span names); they
change annotations, not attribute keys.

### 1.1 Span `llm.call` attributes (omit attribute when value is zero/empty)

| Attribute | Source | Type | Stability | Source URL |
|-----------|--------|------|-----------|------------|
| `gen_ai.provider.name` | LLMCallRecord.Provider | string | Development | https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/ |
| `gen_ai.operation.name` | constant `"chat"` (sanctioned well-known value) | string | Development | https://raw.githubusercontent.com/open-telemetry/semantic-conventions-genai/main/docs/gen-ai/client-inference.md |
| `gen_ai.request.model` | LLMCallRecord.ModelID | string | Development | https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/ |
| `gen_ai.usage.input_tokens` | TokensSent | int | Development | https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/ |
| `gen_ai.usage.output_tokens` | TokensRecv | int | Development | https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/ |
| `gen_ai.usage.cache_read.input_tokens` | TokensCached | int | Development | https://raw.githubusercontent.com/open-telemetry/semantic-conventions-genai/main/docs/gen-ai/client-inference.md |
| `gen_ai.usage.cache_write.input_tokens` | CacheCreationTokens | int | Development | https://raw.githubusercontent.com/open-telemetry/semantic-conventions-genai/main/docs/gen-ai/client-inference.md |
| `gen_ai.usage.reasoning.output_tokens` | ReasoningTokens | int | Development | https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/ |
| `error.type` (see note E1) | meept low-cardinality error class | string | Stable | https://opentelemetry.io/docs/specs/semconv/general/recording-errors/ |
| `error.message` (see note E1) | ErrorMessage truncated to 256, only when IsError | string | Development | https://opentelemetry.io/docs/specs/semconv/registry/attributes/error/ |

Note E1 - error conventions (verified, changes the draft annotation but
not a key): on a failed GenAI operation OTel says instrumentation
SHOULD (a) set the span STATUS to `Error`, (b) set `error.type` to a
low-cardinality error class (`_OTHER` is the sanctioned fallback), and
(c) set the span status DESCRIPTION to the error message rather than an
`error.message` attribute - the registry explicitly marks `error.message`
as "NOT RECOMMENDED for metrics or spans due to its unbounded
cardinality and overlap with span status". Leaf 03 therefore: sets span
status = Error when IsError; sets span status description = truncated
ErrorMessage; optionally sets `error.type` (e.g. `quota`, `refusal`,
`empty_response`, `_OTHER`). Keeping the draft's `error.message`
attribute is a defensible deviation for the SQL-join workflow but it is
NOT convention-preferred; the C8 table above keeps it listed so leaf 03
may emit it, with status+error.type as the primary signal.

Token inclusion rules (verified verbatim from footnotes):
- `gen_ai.usage.cache_read.input_tokens`: "The value SHOULD be included
  in `gen_ai.usage.input_tokens`." (subset of the total)
- `gen_ai.usage.cache_write.input_tokens`: "The value SHOULD be included
  in `gen_ai.usage.input_tokens`." (subset of the total)
- `gen_ai.usage.input_tokens`: "This value SHOULD include all types of
  input tokens, including cached tokens." (total)
- `gen_ai.usage.reasoning.output_tokens`: "The value SHOULD be included
  in `gen_ai.usage.output_tokens`." (subset of the total)

MEEPT MAPPING WARNING (this is the one real correctness risk in C8):
meept's `TokensSent` is the raw request prompt count and
`TokensCached` / `CacheCreationTokens` are provider-reported cache
splits. Under the OTel rule above, if the provider's total
`input_tokens` already includes cached tokens (OpenAI and Anthropic
usage.prompt_tokens / input_tokens both do), then populating
`gen_ai.usage.input_tokens` from TokensSent is correct ONLY if
TokensSent is the provider's inclusive total. Leaf 03 must emit the
provider TOTAL in `gen_ai.usage.input_tokens`, never
`total - cached_read - cache_write`; if meept's TokensSent is a
non-inclusive sum, the implementation must add the cache values back
before emitting. Same logic for `gen_ai.usage.output_tokens` vs
ReasoningTokens (reasoning tokens are a subset of output in Anthropic
and OpenAI o-series usage).

`gen_ai.operation.name` value sets (verified): the well-known value list
includes `chat` ("Chat completion operation such as OpenAI Chat API") -
correct constant for the llm.call span - and `execute_tool` ("Execute a
tool") IS a sanctioned operation value; tool spans are NOT a deviation.
Other well-known values (for reference): `create_agent`, `embeddings`,
`fetch_response`, `generate_content`, `invoke_agent`,
`invoke_workflow`, `plan`, `retrieval`, `text_completion`, plus memory
operations. A custom value is permitted when no predefined value
applies; the convention RECOMMENDS documenting system-specific names.

### 1.2 Span `tool.call` attributes

| Attribute | Source | Type | Stability | Source URL |
|-----------|--------|------|-----------|------------|
| `gen_ai.operation.name` | constant `"execute_tool"` (sanctioned well-known value) | string | Development | https://raw.githubusercontent.com/open-telemetry/semantic-conventions-genai/main/docs/gen-ai/gen-ai-spans.md |
| `gen_ai.tool.name` | ToolExecutionEndData.ToolName | string | Development | https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/ |
| `gen_ai.tool.call.id` | ToolCallID | string | Development | https://raw.githubusercontent.com/open-telemetry/semantic-conventions-genai/main/docs/gen-ai/gen-ai-spans.md |
| `meept.tool.args.keys` | ArgumentsKeys | string (namespace) | n/a (meept custom) | repo-defined; custom namespaces are allowed (meept.* is a valid vendor namespace) |
| `meept.tool.args.bytes` | ArgumentsBytes (int rendered as string) | int as string | n/a (meept custom) | repo-defined |
| `meept.tool.blocked` | `"true"` only when Blocked | string | n/a (meept custom) | repo-defined |
| `error.type` (see note E1) | meept low-cardinality error class | string | Stable | https://opentelemetry.io/docs/specs/semconv/general/recording-errors/ |
| `error.message` (see note E1) | Error truncated to 256, only when failed | string | Development | https://opentelemetry.io/docs/specs/semconv/registry/attributes/error/ |

Execute-tool span notes (verified): upstream span name SHOULD be
`execute_tool {gen_ai.tool.name}` and span kind SHOULD be `INTERNAL`
(see section 2). Requirement level of the whole execute-tool span spec
is "Recommended"; `gen_ai.operation.name` and `gen_ai.tool.name` are
Required attributes of that span, `gen_ai.tool.call.id` is Conditionally
Required. `gen_ai.tool.call.arguments` and `gen_ai.tool.call.result`
exist upstream but are Opt-In AND carry the upstream sensitive-content
warning - meept's prohibition on recording raw argument values/results
is consistent with (stricter than) upstream; keep the shape-only
`meept.*` attributes.

### 1.3 Span naming convention (verified, RECOMMENDATION)

Upstream span name rules:
- Client inference span name SHOULD be `{gen_ai.operation.name}
  {gen_ai.request.model}` - e.g. `chat gpt-4`.
- Execute-tool span name SHOULD be `execute_tool {gen_ai.tool.name}`.
- Providers MAY specify a different format; the overall OTel span-name
  guideline (low-cardinality, dotted lowercase like `http.client` style
  names) still applies.

`llm.call` and `tool.call` are therefore NOT convention-shaped. However
meept's existing spans are `chat.turn` / `task.step` (dotted lowercase),
and the master plan freezes `llm.call` / `tool.call` in C6.

RECOMMENDATION: keep `llm.call` / `tool.call` as the span names (frozen
C6, internal consistency beats partial convention conformance), and put
the convention shape in the attributes: `gen_ai.operation.name` = `chat`
/ `execute_tool` plus `gen_ai.request.model` / `gen_ai.tool.name` allow
any consumer to reconstruct or rename. Do NOT use `chat gpt-4o` as the
meept span name: it mixes a high-cardinality token into a name OTel
guidelines want low-cardinality, and it breaks consistency with the
existing `chat.turn` posture. Document the deviation in
docs/workflows/llm-tracing.md (leaf 05).

## 2. Span hierarchy for LLM + tool hops (Task 2)

### 2.1 Parent/child model and the "parent not yet received" case

OTel's model is a single DAG of spans per trace: a span carries
trace_id, span_id, and parent_span_id; parentage is a data property of
each span, not a delivery-order property. Nothing in the OTel spec
requires the parent span to arrive at, or be stored by, the backend
before its children - a child span that references a parent span ID
that has not been exported yet is a normal, valid span. The GenAI
client-inference spec describes exactly this nesting for agent turns:
the client inference span represents "a logical client call" and "one
client request may involve multiple server-side inference operations",
which providers "may record ... as nested inference spans"; the
execute-tool span is likewise defined as a step an agent performs and
SHOULD be associated with the agent entity. Jaeger's own FAQ
documentation describes traces as a tree of spans assembled by shared
trace IDs and parent span IDs (https://www.jaegertracing.io/docs/faq/ -
"Why do Jaeger trace IDs look differently..." section documents the ID
model; the UI assembles the tree from parent_span_id fields regardless
of arrival order).

Jaeger/Tempo handling of the not-yet-received parent: Tempo
documentation explicitly deals with traces whose spans straddle time -
"Long-running traces are created when Tempo receives spans for a trace,
followed by a delay, and then Tempo receives additional spans for the
same trace" (https://grafana.com/docs/tempo/latest/troubleshooting/querying/long-running-traces/)
- and Tempo's TraceID lookup "searches for all parts of a trace in all
matching blocks" (https://grafana.com/docs/tempo/latest/api_docs/),
merging them into one trace on read. Search indexes may show a
placeholder root ("root span not yet received" - observed in Tempo's
search/drilldown UX, github.com/grafana/tempo issue #7494), but opening
the trace by ID assembles whatever spans share the trace ID. A hop span
exported BEFORE its turn-span parent is therefore valid: Jaeger/Tempo
render children under the parent as long as the parent eventually
arrives with that span ID; a permanently absent parent renders the
span(s) as orphans/roots-of-a-fragment, which is cosmetic, not fatal.

RECOMMENDATION (master.md Open Question #2): keep ParentSpanID = minted
turn span ID on every llm.call / tool.call span even though the turn
span materializes only at turn terminal. The turn span DOES export later
with exactly that span ID (C5/C6), so Jaeger/Tempo assemble the full
tree after the turn completes. Fallback to ParentSpanID = "" only if a
specific receiver is observed to misrender; decide that at leaf 03
review with evidence, not preemptively.

### 2.2 Deterministic head sampling per trace

Head sampling makes the sampling decision as early as possible; the
most common form is Consistent/Deterministic Probability Sampling where
"the sampling decision is made based on the trace ID and the desired
percentage of traces to sample. This ensures that whole traces are
sampled - no missing spans" (https://opentelemetry.io/docs/concepts/sampling/).
Jaeger's classic doc states the same property operationally: the
decision is made once by the trace initiator and "propagated with the
requests ... so those services will not be making the sampling decision
again but instead will respect the decision made by the top service"
guaranteeing "that if a trace is sampled, all its spans will be recorded
in the backend" (https://www.jaegertracing.io/docs/1.76/architecture/sampling/).
Because meept's producer runs the sampler AFTER the trace ID is known
and derives the decision from the trace ID, every span of one trace
shares the sampled/not-sampled decision - this is exactly the
deterministic head-sampling property, and it is what makes
partially-sampled traces impossible within meept itself. What Jaeger /
Tempo CAN still show: a trace missing spans because of an external
collector's tail sampling, ingestion drops, retention expiry, or
per-span export failures; Tempo's troubleshooting doc names head/tail
sampling drops as "the most common cause of missing traces"
(https://grafana.com/docs/grafana/latest/datasources/tempo/troubleshooting/).
Design conclusion: per-trace determinism is sufficient; no additional
coordination is needed for hierarchy correctness.

## 3. Trace-ID correlation with SQL stores (Task 3)

Best practice for joining exported traces to application SQL rows by
trace ID:

- Storage representation: OTel trace IDs are 16 random bytes, canonically
  rendered as 32-char lowercase hex. Backends do not agree on one wire
  representation - Jaeger documents that "at the data model level ...
  trace IDs are a sequence of 16 bytes" that appear as hex strings in
  the UI, as two big-endian uint64 in the Go domain model, and as
  base64 in Protobuf-JSON (https://www.jaegertracing.io/docs/faq/).
  Tempo's HTTP API takes the hex string form
  (`GET /api/traces/<traceid>`,
  https://grafana.com/docs/tempo/latest/api_docs/). An application SQL
  ledger therefore stores the HEX STRING - it is the one representation
  every human workflow (paste into Jaeger/Tempo UI search, Tempo API
  curl, TraceQL) accepts verbatim.
- TEXT hex vs BINARY(16): BINARY(16) is marginally smaller and faster to
  index in high-volume systems, but meept's ledger is a per-installation
  SQLite db where every consumer is either human (paste the ID into the
  tracing UI) or Go code comparing strings (C3 already freezes
  `trace_id TEXT NOT NULL DEFAULT ''`). Convertibility is trivial and
  lossless in both directions (`hex(decode(...))` in SQLite, hex.DecodeString
  in Go), so the migration cost of being "wrong" is near zero.
- Query pattern: SQL first - `SELECT latency_ms, model_id, trace_id FROM
  llm_calls WHERE trace_id = ?` (indexed) or find slow/failing rows
  `WHERE latency_ms > ? OR is_error = 1 ORDER BY latency_ms DESC`, then
  open the trace by ID in the Jaeger UI (TraceID search field) or Tempo
  (`GET /api/traces/<traceid>` or the Grafana Tempo data source "TraceID"
  query type). Tempo additionally offers TraceQL for attribute-side
  search (https://grafana.com/docs/tempo/latest/api_docs/), but
  lookup-by-trace-ID remains the documented direct flow and works even
  when attribute search indexes lag or the root span has not been
  received yet (section 2.1).

RECOMMENDATION: CONFIRM the plan default - `trace_id TEXT` storing
32-char lowercase hex (and `span_id TEXT`, 16-char hex). Do not use
BINARY(16); the plan default is correct for SQLite + human paste-into-UI
workflows, and it matches the already-frozen C3 schema text.

## 4. Source list

Primary (attribute names, span shapes, requirement levels):
1. https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/ - full GenAI attribute registry incl. deprecated `gen_ai.system` -> `gen_ai.provider.name`, `gen_ai.usage.prompt_tokens` -> `gen_ai.usage.input_tokens`, `gen_ai.usage.completion_tokens` -> `gen_ai.usage.output_tokens`; `gen_ai.operation.name` well-known values; token-subset footnotes.
2. https://raw.githubusercontent.com/open-telemetry/semantic-conventions-genai/main/docs/gen-ai/client-inference.md - client inference span: name `{operation} {model}`, span kind CLIENT, status rules, full usage attribute table incl. `cache_write.input_tokens`, subset footnote [22][23][27][29].
3. https://raw.githubusercontent.com/open-telemetry/semantic-conventions-genai/main/docs/gen-ai/gen-ai-spans.md - span index; execute-tool span: name `execute_tool {tool.name}`, kind INTERNAL, `gen_ai.tool.name` Required, `gen_ai.tool.call.id` Conditionally Required, Opt-In sensitive arguments/result.
4. https://opentelemetry.io/docs/specs/semconv/general/recording-errors/ - span status MUST be unset on success; on error set status Error + `error.type` + status description.
5. https://opentelemetry.io/docs/specs/semconv/registry/attributes/error/ - `error.type` definition (low cardinality, `_OTHER` fallback); `error.message` NOT RECOMMENDED for spans.

Hierarchy / sampling:
6. https://opentelemetry.io/docs/concepts/sampling/ - head sampling / deterministic (trace-ID-derived) decisions, "no missing spans".
7. https://www.jaegertracing.io/docs/1.76/architecture/sampling/ - consistent upfront sampling, decision propagation, complete-trace guarantee; adaptive sampling.
8. https://www.jaegertracing.io/docs/2.dev/architecture/sampling/ - current head-based / remote sampling doc.
9. https://grafana.com/docs/tempo/latest/troubleshooting/querying/long-running-traces/ - spans of one trace arriving across blocks; TraceID lookup reassembles all parts.
10. https://grafana.com/docs/tempo/latest/api_docs/ - `GET /api/traces/<traceid>` lookup-by-ID; TraceQL search; search-vs-lookup behavior.
11. https://grafana.com/docs/grafana/latest/datasources/tempo/troubleshooting/ - "trace not found" causes: sampling drops (most common), retention; TraceID query settings.
12. https://github.com/grafana/tempo/issues/7494 - observed "root span not yet received" placeholder in search results while trace-by-ID is complete.

Trace-ID representation:
13. https://www.jaegertracing.io/docs/faq/ - 16-byte trace IDs, hex in UI, uint64 pairs internally, base64 in protobuf-JSON.
14. https://github.com/jaegertracing/jaeger-idl/blob/main/proto/storage/v2/trace_storage.proto - backend `bytes trace_id` representation.
