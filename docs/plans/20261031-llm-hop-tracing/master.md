# LLM-Hop Tracing (OTel-Style Per-Hop Spans + Trace-ID Correlation) - Master Plan

> **For the executing agent:** You are the orchestrator for this tree. Your
> job: (1) dispatch implementation agents per the Dispatch Protocol, (2)
> review their work in-session (never via a delegated reviewer), (3)
> re-dispatch on gaps (max 3 iterations), (4) commit after review, (5) track
> completion in the Completion Tracking Table. Do NOT implement code yourself.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 6 leaves (flat tree, this directory)
- **Scope:** Full LLM-hop tracing for meept: a trace ID minted at turn start,
  threaded through client -> loop -> tool executor; per-hop `llm.call` and
  `tool.call` OTLP spans carrying OpenTelemetry GenAI semantic-convention
  attributes (provider, model, token counts, latency, argument shape);
  `trace_id`/`span_id` columns on `llm_calls` and `session_tool_calls` so
  OTLP traces join SQL records by trace ID; hermetic e2e live-verification
  against a local OTLP receiver; user-facing workflow doc.

## Goal

Today meept exports only two COARSE span types over OTLP (`chat.turn`,
`task.step` via `internal/metrics/otlp_trace_producer.go`) with random
exporter-generated trace IDs - there is no way to correlate an exported trace
with the rich per-call SQL ledger (`llm_calls` has tokens_sent/received/
cached, reasoning_tokens, cache_creation_tokens, latency_ms - but zero
trace correlation), and individual LLM hops and tool executions are invisible
in traces.

This tree closes that gap: one trace per turn, minted at turn start, shared
by the turn span, every `llm.call` span (one per completed LLM call attempt,
success or terminal failure), and every `tool.call` span; the same trace ID
lands in `metrics.db llm_calls.trace_id` (and `session_tool_calls.trace_id`)
so Jaeger/Tempo traces join SQL rows 1:1. The existing opt-in posture is
preserved exactly: `metrics.otlp_traces.enabled=false` (default) means zero
network, zero goroutines, and one disabled-Warn line at boot; enabling it
emits exactly one armed-Info line.

Non-goals (hard boundaries):
- Do NOT touch the evolver trace store (`internal/memory/trace_store.go`,
  `SpanRecord` in `trace_span_record.go`) - it is a deliberately parallel,
  opt-in, off-inference-path system. No merging, no exporting from it.
- Do NOT change bus payload shapes in breaking ways; `TurnTerminalEvent` is
  CLOSED and may only gain additive `omitempty` fields with its contract doc
  updated (see C5).
- Span attributes NEVER carry secret material. Headers are the sanctioned
  secret channel (`OTLPTracesConfig.Headers`). Tool arguments are recorded as
  SHAPE ONLY (sorted key names + byte count), never raw values.

## Architecture

```
[handler.go:763 area] turn start
    traceID = id.TraceID()          turnSpanID = id.SpanID()
    opts += llm.WithTraceID(traceID)         (request-scoped ChatOption)
    publishTurnTerminal(... TraceID)          (additive field, C5)
        |
        v
[loop.go executeToolCalls ~7914 + llm call site ~6391]
    traceID = llm.EffectiveTraceID(opts)
    ctx = agent.WithTraceContext(ctx, traceID)     (tool-executor hand-off)
    emits AgentEventToolExecutionStart/End          (+TraceID/SpanID/arg shape)
        |                                |
        v                                v
[llm client recordUsageStore ~1992]   [session store]
    spanID = id.SpanID()               session.ToolCall += TraceID/SpanID
    llm_calls row: trace_id/span_id    (store-level roundtrip pins; see
    SetLLMCallObserver(record)  -->    Open Questions: no runtime writer
        |                              today)
        v
[daemon otlp_traces_wiring.go]
    observer -> producer.RecordLLMCallSpan(LLMCallSpanSpec)
    raw topic "agent.event.tool_execution_end" -> RecordToolCallSpan
    turn sub passes TraceID/SpanID -> chat.turn span keeps minted IDs
        |
        v
[TraceExporter -> OTLP/HTTP sink -> Jaeger / Tempo / Langfuse]
    all spans of one trace share trace_id; hop spans parent on turnSpanID;
    deterministic per-trace sampling unchanged
```

Key decisions (frozen):
1. Trace/span IDs are minted with `crypto/rand` via new `pkg/id` helpers
   (16-byte/8-byte hex). Never `time.Now`/`math/rand` (predid analyzer).
2. Request-scoped propagation only, mirroring the proven
   `llm.WithResolvedModel` pattern (`internal/llm/client.go:1285`): the
   trace ID rides `chatOptions` per call and a context value for the
   executor. No shared mutable state, no client fields.
3. New span types are produced by the EXISTING bus-driven TraceProducer
   posture: the daemon wiring decodes events and calls nil-safe Record*
   methods. Nil exporter = disabled = nothing built, nothing published.
4. Schema changes use the existing migration machinery ONLY: metrics.db via
   the `storeMigrations` registry (next Version 7, `columnMissingGuard`
   pattern, index step after column step), sessions.db via
   `migrationAddColumn`. Fresh-database CREATE scripts gain the columns
   inline in the same leaf.
5. `llm.call` span identity: `recordUsageStore` mints one span ID per
   completed call; the SAME hex string goes to `llm_calls.span_id` and to
   the exported span, so SQL and trace join exactly.
6. Latency becomes real: every `recordUsageStore` call site passes its
   request/attempt start time; `llm_calls.latency_ms` and the span duration
   derive from it (today all 12 call sites pass 0 - verified).

## Interface Contracts

These contracts are FROZEN at dispatch. Leaf 00 (research) may refine ONLY
the C8 attribute table (names/omission rules) before leaf 03 dispatches; it
may not change any signature. Any other deviation goes through the
orchestrator and is recorded under Notes before code is written.

### C1: ID minting (leaf 01; consumed by 01, 02, 03)

```go
// pkg/id/trace.go
// TraceID returns 32 lowercase hex chars (128 bits, crypto/rand).
// SpanID returns 16 lowercase hex chars (64 bits, crypto/rand).
// On entropy failure both return the documented zero-suffix fallback
// ("000..."), mirroring Generate()'s catastrophic-failure convention.
func TraceID() string
func SpanID() string
// Owner: 01-trace-id-threading.md
// Consumers: 01 (handler, loop, llm client), 02 (tests), 03 (wiring tests)
```

### C2: Request-scoped trace threading (leaf 01)

```go
// internal/llm/client.go
// WithTraceID carries the turn's trace ID for THIS ONE request. Empty
// string is a no-op. Never mutates client state (WithResolvedModel rules).
func WithTraceID(traceID string) ChatOption

// EffectiveTraceID folds the per-call options and returns the request's
// trace ID ("" when none). Read-only helper for the agent loop.
func EffectiveTraceID(opts []ChatOption) string

// internal/agent/tracectx.go
// WithTraceContext returns ctx carrying traceID; TraceIDFromContext
// retrieves it ("" when absent). Unexported key, exported accessors.
func WithTraceContext(ctx context.Context, traceID string) context.Context
func TraceIDFromContext(ctx context.Context) string
// Owner: 01-trace-id-threading.md
// Consumers: 01 (loop emit sites), 03 (wiring tests)
```

### C3: SQL schema + record fields (leaf 02)

```go
// internal/metrics/store.go
// LLMCallRecord gains two fields (zero = unknown, "" when unset):
type LLMCallRecord struct {
    // ... existing fields unchanged ...
    TraceID string // 32 hex, "" when the caller had no turn trace
    SpanID  string // 16 hex, "" when the caller minted none
}

// Schema (frozen):
//   llm_calls:           trace_id TEXT NOT NULL DEFAULT '',
//                        span_id  TEXT NOT NULL DEFAULT ''
//   session_tool_calls:  trace_id TEXT NOT NULL DEFAULT '',
//                        span_id  TEXT NOT NULL DEFAULT ''
// metrics.db: storeMigrations registry Version 7 (column step, guarded by
//   columnMissingGuard x2) + Version 8 (idx_llm_calls_trace on
//   llm_calls(trace_id), guarded by indexMissingGuard) - index AFTER column,
//   matching the version-4/6 ordering rule. Fresh-database CREATE TABLE for
//   llm_calls gains both columns inline.
// sessions.db: s.migrationAddColumn ALTERs for both columns; CREATE TABLE
//   script for session_tool_calls gains both columns inline.
// internal/session.ToolCall gains TraceID/SpanID string fields (json
//   "trace_id"/"span_id", omitempty); SaveToolCalls persists them;
//   GetToolCalls / GetToolCallsForMessages / ForkSession copy SELECTs
//   read them.
// Owner: 02-schema-migrations.md
// Consumers: 01 (stamps LLMCallRecord at call sites), 03 (e2e join), 04
```

### C4: LLM call observation seam (leaf 01; consumed by 03)

```go
// internal/llm/client.go
// SetLLMCallObserver attaches a callback invoked (async, never blocking the
// call path) once per COMPLETED LLM call - success, refusal, empty-response
// failure, and terminal all-failed alike - carrying exactly the record that
// was persisted to llm_calls (same TraceID/SpanID/tokens/latency).
// Nil or re-set is allowed; nil-guarded setter (SetUsageStore pattern,
// client.go:296). The callback must never log record contents.
func (c *Client) SetLLMCallObserver(fn func(appmetrics.LLMCallRecord))
// Owner: 01-trace-id-threading.md (signature), 01 stamps the values
// Consumer: 03 (daemon wiring maps record -> metrics.LLMCallSpanSpec)
```

### C5: Additive event/spec field extensions (leaf 01; consumed by 03)

```go
// internal/agent/events.go - additive, JSON-omitempty, non-breaking:
type ToolExecutionEndData struct {
    // ... existing fields unchanged ...
    TraceID        string `json:"trace_id,omitempty"`
    SpanID         string `json:"span_id,omitempty"`
    ArgumentsKeys  string `json:"arguments_keys,omitempty"`  // sorted, comma-joined argument KEY NAMES only
    ArgumentsBytes int    `json:"arguments_bytes,omitempty"` // raw JSON byte length of arguments
}

// internal/agent/handler.go - TurnTerminalEvent gains ONE field:
    TraceID string `json:"trace_id,omitempty"` // 32 hex; "" on pre-mint early-failure paths
// REQUIRED same-leaf updates: the frozen-field-set contract in
// docs/plans/20260916-turn-lifecycle-events/master.md (its own rule:
// extension is allowed when every consumer + that contract doc are
// updated), and every publishTurnTerminal call site stamps TraceID.

// internal/metrics/otlp_trace_producer.go:
type TurnSpanSpec struct { // gains:
    TraceID string // minted turn trace; "" keeps exporter-random fill
    SpanID  string // minted turn span id; "" keeps exporter-random fill
}
// Owners: 01 (events.go, handler.go), 01 declares TurnSpanSpec fields,
// 03 implements the producer's use of them.
```

### C6: New span types on TraceProducer (leaf 03)

```go
// internal/metrics/otlp_trace_producer.go
type LLMCallSpanSpec struct {
    TraceID, SpanID       string
    ParentSpanID          string // turn span id; "" = no parent linkage
    Provider, ModelID     string
    AgentID, SessionID    string
    ConversationID        string
    TokensSent, TokensRecv        int
    TokensCached, ReasoningTokens int
    CacheCreationTokens           int
    IsError       bool
    ErrorMessage  string // truncated to 256 chars by the producer
    LatencyMS     int64  // > 0 drives Start/End; 0 = instantaneous span
}
type ToolCallSpanSpec struct {
    TraceID, SpanID, ParentSpanID string
    ToolName, ToolCallID          string
    ArgumentsKeys                 string
    ArgumentsBytes                int
    Success, Blocked, Cached      bool
    Error                         string // truncated to 256 chars
    DurationMS                    int64
}
func (p *TraceProducer) RecordLLMCallSpan(ev LLMCallSpanSpec)
func (p *TraceProducer) RecordToolCallSpan(ev ToolCallSpanSpec)
// Span names: "llm.call" and "tool.call". Nil exporter = no-op. Both respect
// the producer sampler (deterministic per trace: every span of one trace
// shares the sampled/not-sampled decision because TraceID is set BEFORE the
// sampler runs). RecordLLMCallSpan reuses ev.SpanID as the exported span ID
// (SQL span_id == OTLP span id); RecordToolCallSpan likewise.
// TurnSpanSpec.TraceID/SpanID (C5), when non-empty, override the
// exporter-random IDs on the chat.turn span so the whole trace shares one ID.
// Owner: 03-span-producer.md. Consumers: 03 wiring, 04 e2e.
```

### C7: Daemon wiring (leaf 03)

```go
// internal/daemon/otlp_traces_wiring.go (inside the cfg.Enabled branch):
//   1. turn sub: pass ev.TraceID/ev.SpanID into TurnSpanSpec.
//   2. NEW: client.SetLLMCallObserver -> producer.RecordLLMCallSpan
//      (map C4 record -> C6 spec; ParentSpanID = turn span id when the
//      wiring can associate one, else ""). The observer is attached ONLY
//      when enabled - disabled posture stays zero-hook.
//   3. NEW: raw-topic subscription "agent.event.tool_execution_end"
//      (lenient decode of the agent.AgentEvent envelope's
//      ToolExecutionEndData, mirroring the task.step_completed pattern)
//      -> producer.RecordToolCallSpan.
// Log lines (asserted by tests): disabled = exactly one Warn
//   "OTLP trace export disabled ..." (existing, wiring.go:52);
//   enabled = exactly one Info "OTLP trace export enabled"
//   (existing, ~wiring.go:98). No new steady-state log lines.
// Owner: 03-span-producer.md
```

### C8: GenAI semantic-convention attribute table (leaf 00 refines names; leaf 03 implements)

Span `llm.call` attributes (omit attribute entirely when value is zero/empty):

| Attribute | Source |
|-----------|--------|
| `gen_ai.provider.name` | LLMCallRecord.Provider |
| `gen_ai.operation.name` | constant `"chat"` |
| `gen_ai.request.model` | LLMCallRecord.ModelID |
| `gen_ai.usage.input_tokens` | TokensSent |
| `gen_ai.usage.output_tokens` | TokensRecv |
| `gen_ai.usage.cache_read.input_tokens` | TokensCached |
| `gen_ai.usage.cache_write.input_tokens` | CacheCreationTokens |
| `gen_ai.usage.reasoning.output_tokens` | ReasoningTokens |
| `error.message` | ErrorMessage, truncated 256, only when IsError |

Span `tool.call` attributes:

| Attribute | Source |
|-----------|--------|
| `gen_ai.operation.name` | constant `"execute_tool"` |
| `gen_ai.tool.name` | ToolExecutionEndData.ToolName |
| `gen_ai.tool.call.id` | ToolCallID |
| `meept.tool.args.keys` | ArgumentsKeys |
| `meept.tool.args.bytes` | ArgumentsBytes (int rendered as string) |
| `meept.tool.blocked` | `"true"` only when Blocked |
| `error.message` | Error, truncated 256, only when failed |

PROHIBITED in any attribute: raw argument values, tool results, prompts,
completions, headers, tokens, keys. Secret material reaches the exporter
ONLY via `OTLPTracesConfig.Headers`. The `audit-secrets-value-callers.py`
spirit applies: attribute values are allowlisted shapes from the tables
above, nothing else.

Note: the deprecated `gen_ai.system` / `gen_ai.usage.prompt_tokens` /
`gen_ai.usage.completion_tokens` names MUST NOT be used (upstream replaced
them with the names above).

### C9: Test commands (pinned, every leaf)

```bash
go test -p 2 ./internal/llm/... ./internal/agent/... ./internal/metrics/... ./internal/session/... ./pkg/id/...
make e2e-fast-area AREA=llm-hop-tracing   # leaf 04's suite
```

macOS rule: always `-p 2` (unbounded localhost sockets exhaust ephemeral
ports). e2e tier: `go test -tags e2e -count=1 ./e2e/... -p 2`.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00 | 00-research.md | leaf (research, no code) | none | ~25K | A (first, alone) |
| 01 | 01-trace-id-threading.md | leaf | 00 | ~95K | B |
| 02 | 02-schema-migrations.md | leaf | none | ~60K | B |
| 03 | 03-span-producer.md | leaf | 01, 02 | ~85K | C |
| 04 | 04-e2e-live-verify.md | leaf | 01, 02, 03 | ~90K | D |
| 05 | 05-docs.md | leaf | 03 | ~35K | D |

Concurrency groups: 00 first (its C8 refinement gates 03). Then 01 + 02 in
parallel (B). Then 03 alone (C). Then 04 + 05 in parallel (D).

## Dispatch Protocol

Execute in dependency order. Full recipe: skill `hierarchical-plan-execution`.

1. **Dispatch leaf 00** (research). Review its RESEARCH-FINDINGS.md
   in-session; fold any C8 attribute-name corrections into C8 above BEFORE
   dispatching 03 (and note them under Notes). 00 writes ONLY its findings
   file inside this tree directory - no code.
2. **Dispatch group B (01 + 02) in parallel** via `delegate_task`:
   goal = "Implement all tasks from <leaf path>", context = the full leaf
   text + the C1-C9 contracts it consumes + this file's Coding Conventions.
   Every dispatch context MUST include verbatim: "Do NOT commit. Do NOT run
   git add. Write code, run tests, report results only. The orchestrator
   handles all git operations. Never pipe read_file output into write_file;
   explore with search_files or terminal cat; write once and stop."
3. **Review in-session** (main model, never a delegated reviewer): run the
   leaf's verify commands yourself, diff signatures against C1-C9, check the
   leaf's Review Checklist, run `git status --short` to catch stray files.
4. **Re-dispatch on gaps** with specific findings; reset the tracking-table
   status to IN_PROGRESS on re-dispatch; max 3 cycles, then escalate.
5. **Commit** (orchestrator only, after review): stage exactly the files the
   leaf lists; message `feat(tracing): <leaf name>`. Before the first commit
   of each leaf: `gofmt -l <touched dirs>` empty, `go vet ./...` clean,
   `make mutexio`, `make predid` clean (remove any unreferenced helper the
   leaf left behind - U1000 pre-commit hook rejects it).
6. **Record status** in the Completion Tracking Table after every
   transition. The parent (orchestrator) determines completion - never the
   implementer.
7. After group C and D reviews: integration review per Integration Test
   Plan, then final commit + statuses COMPLETE.

## Review Checklist

The orchestrator verifies each child in-session:

- [ ] All tasks from the leaf document are implemented; verify commands pass
      with `-count=1`.
- [ ] Interface contracts satisfied EXACTLY (diff every signature/field
      against C1-C9; JSON tags included).
- [ ] All specified files created/modified at exact paths; `git status
      --short` shows no files outside the leaf's declared list.
- [ ] No secret material in span attribute values (C8 prohibition list).
- [ ] No debug artifacts (fmt.Println, TODOs, placeholders, commented-out
      code); `make predid` and `make mutexio` clean.
- [ ] No line-number corruption: `grep -rcE '^\s+[0-9]+\|' --include='*.go'
      <touched dirs>` returns zero hits.
- [ ] Nil-exporter no-op posture intact: with `metrics.otlp_traces.enabled`
      absent/false, tests prove zero network and zero goroutines.
- [ ] Evolver trace store untouched: `git diff --stat` shows no changes
      under internal/memory/.
- [ ] TUI/GUI parity not affected (no UI-surface changes in this tree).

Output per leaf: APPROVED or a specific gap list with file:line references.

## Coding Conventions

Match repo AGENTS.md + docs/agents/coding-practices.md:

- **Language:** Go only (module `github.com/caimlas/meept`). ASCII only in
  all files; no unicode dashes/quotes in code or comments.
- **IDs:** `pkg/id` helpers only (C1). NEVER `time.Now().UnixNano()` or
  `math/rand` for IDs, even in fallback paths (`make predid` enforces).
- **Errors:** wrap `fmt.Errorf("context: %w", err)`; no ignored errors; no
  bare `panic(err)`.
- **Logging:** `log/slog`; one boot-time posture line, no steady-state
  noise; never log secrets, headers, or raw arguments/results.
- **Setters:** every `Set*` nil-guards; typed-nil interface guards at
  construction boundaries.
- **Maps:** two-value type assertions only on `map[string]any`.
- **Mutex scope:** no I/O under a Go mutex (`make mutexio`).
- **Request-scoped state:** per-call options and context values only; never
  shared mutable client/loop state for the trace ID.
- **Tests:** table-driven with `t.Run`; pins added to EXISTING `*_test.go`
  files in the touched package (repo policy: no new unit test files for new
  features; the hermetic e2e tier carries new feature tests). New test
  files are allowed ONLY under `e2e/` and `pkg/id` (if no id test file
  exists yet - pure crypto/rand fallback branches are impractical to reach
  from e2e).
- **Test parallelism:** always `-p 2`.
- **Schema:** migrations via the existing registries/helpers ONLY
  (`storeMigrations` + `columnMissingGuard`/`indexMissingGuard` for
  metrics.db; `migrationAddColumn` for sessions.db); fresh-database CREATE
  scripts updated in the same commit; index steps AFTER their columns.
- **Formatting:** `gofmt` before reporting; `go vet` clean.
- **Docs:** feature mapping `internal/<pkg>/` -> `docs/workflows/<pkg>.md`
  (leaf 05 owns the new `docs/workflows/llm-tracing.md`). Any bus payload
  key change requires `make graphs` so `docs/generated/bus-topology.md`
  stays fresh (leaf 01: `ToolExecutionEndData` + `TurnTerminalEvent` field
  additions are payload key changes - run `make graphs` and commit the
  regenerated files; `make graphs-check` is a CI gate).

## Requirement Traceability

| Req | Source (approved scope, verbatim intent) | Plan element | Verified by |
|-----|------------------------------------------|--------------|-------------|
| R1 | trace ID minted at TURN start, threaded client->loop->tool executor (request-scoped, WithResolvedModel pattern) | C1, C2, C5; leaf 01 T1-T4 | leaf 01 unit pins; leaf 04 e2e single-trace assertion |
| R2 | schema migrations: trace_id/span_id on llm_calls AND session_tool_calls (migration + idempotent ALTER + every query site + roundtrip pin) | C3; leaf 02 T1-T4 | leaf 02 roundtrip pins; leaf 04 SQL join |
| R3 | new span types llm.call (tokens, latency, model, provider) and tool.call (name, arguments shape) on TraceProducer | C6, C8; leaf 03 T1-T2 | leaf 03 span-encode tests; leaf 04 receiver capture |
| R4 | NIL-EXPORTER NO-OP posture preserved; zero network when disabled; armed log line asserted | C7; leaf 03 T3; leaf 04 quiet-boot scenario | leaf 03 wiring tests; leaf 04 disabled-boot e2e |
| R5 | Jaeger/Tempo as documented export targets + live-verification e2e against a local OTLP receiver | leaf 04 suite; leaf 05 doc | make e2e-fast-area AREA=llm-hop-tracing |
| R6 | docs/workflows/llm-tracing.md | leaf 05 T1 | doc cross-refs resolve; leaf 05 checklist |
| R7 | span attributes never carry secret material | C8 prohibition; leaf 03 attr allowlist; leaf 04 negative assertion | leaf 04 e2e asserts no raw argument values in captured attributes |
| R8 | evolver trace store untouched; schema via existing migration pattern only; graphs freshness for payload changes | Non-goals; C3; leaf 01 T4 graphs step | review checklist git-diff + make graphs-check |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00-research | COMPLETE | 1 | RESEARCH-FINDINGS.md landed in-tree; C8 semconv refinement sanctioned and implemented by leaf 03 |
| 01-trace-id-threading | COMPLETE | 1 | commit e056cbe5 (trace ID mint, WithTraceID threading, SetLLMCallObserver, real latency, tool-execution event emission); TurnSpanID parent-linkage follow-up landed in fb7e5491 |
| 02-schema-migrations | COMPLETE | 1 | commit e86012cd (llm_calls v7+v8, session_tool_calls migrationAddColumn, roundtrip pins). Honest-scope note: runtime persistence of executed tool calls via session_tool_calls remains unwritten BY DESIGN (Open Question 1); a future writer inherits trace correlation automatically via session.ToolCall |
| 03-span-producer | COMPLETE | 1 | commit 81fc7d7a (llm.call + tool.call spans, gen_ai.* attrs per refined C8, daemon observer bridge + tool_execution_end subscription; disabled posture preserved) |
| 04-e2e-live-verify | COMPLETE | 1 | commit 079ab7ba (e2e suite + live Duration envelope production fix); re-run green in 2026-11-05 audit: make e2e-fast-area AREA=llm-hop-tracing, 20.1s |
| 05-docs | COMPLETE | 1 | docs/workflows/llm-tracing.md (331 lines) landed in 4d8179cf |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE |
BLOCKED. Update after every transition; re-dispatch resets to IN_PROGRESS.

Forest closeout 2026-11-05: read-only audit verified all leaves at HEAD;
e2e area green; issue 85 closed.

## Integration Test Plan

After all leaves reach REVIEWED, the orchestrator runs in-session:

1. `go build ./...` clean.
2. `go test -p 2 ./internal/llm/... ./internal/agent/... ./internal/metrics/... ./internal/session/... ./pkg/id/... -count=1` all PASS.
3. `make e2e-fast-area AREA=llm-hop-tracing` PASS (hermetic: fake LLM +
   local OTLP capture receiver; asserts single shared trace ID across
   chat.turn + llm.call + tool.call spans, gen_ai token/latency attributes,
   and the SQL join llm_calls.trace_id == captured trace).
4. `make e2e-fast` (full hermetic suite, CI gate member) - no regressions.
5. `make test` (short mode, -p 2) - no regressions.
6. `make graphs-check` PASS (payload key additions are reflected in
   docs/generated/).
7. `make lint-ci` analyzers subset: `make mutexio && make predid` clean.
8. Cross-boundary contract checks: llm_calls.span_id equals the captured
   llm.call span's span_id (byte-exact); disabled-boot run produces zero
   HTTP requests at the receiver AND exactly the disabled Warn line.

## Test Gate

Repo policy (AGENTS.md e2e Testing Policy) shapes this tree's gate: new
feature tests live in the hermetic e2e tier (leaf 04 owns the
`llm-hop-tracing` suite + manifest entry), and per-leaf pins ride existing
package test files. There are no separate 00T gate leaves; instead the
control agent (orchestrator, in-session) validates coverage BEFORE
dispatching group D using this matrix:

| # | Feature (from plan) | Source | Unit/pin tests | E2E tests | Status |
|---|---------------------|--------|----------------|-----------|--------|
| F1 | TraceID/SpanID minting (crypto/rand, hex, fallback) | C1 / 01 T1 | pkg/id table tests | via 04 shared-trace assert | VALIDATED |
| F2 | WithTraceID request scoping + EffectiveTraceID | C2 / 01 T2 | internal/llm client pins | via 04 | VALIDATED |
| F3 | Loop tool-event emission with TraceID + args shape | C5 / 01 T3 | internal/agent pins | 04 tool.call span assert | VALIDATED |
| F4 | llm_calls trace/span columns + migration idempotence + index | C3 / 02 T1 | store migration pins | 04 SQL join | VALIDATED |
| F5 | session_tool_calls trace/span columns + roundtrip | C3 / 02 T2 | store_sqlite pins | n/a (no runtime writer; see Open Questions) | VALIDATED |
| F6 | LLMCallRecord stamping at every call site + real latency | C3/C4 / 01 T2 | internal/llm pins | 04 latency>0 assert | VALIDATED |
| F7 | llm.call span w/ gen_ai attrs + SQL span_id identity | C6/C8 / 03 T1 | otlp_trace_test additions | 04 receiver capture | VALIDATED |
| F8 | tool.call span w/ shape attrs | C6/C8 / 03 T1 | otlp_trace_test additions | 04 receiver capture | VALIDATED |
| F9 | TurnSpanSpec keeps minted IDs; trace shares one ID | C5/C6 / 03 T1 | producer pins | 04 single-trace assert | VALIDATED |
| F10 | Wiring: observer + raw-topic sub; armed/disabled log lines | C7 / 03 T2-T3 | daemon wiring tests | 04 quiet-boot + armed-boot | VALIDATED |
| F11 | Secret-free attributes | C8 / 03 T2 | producer pin (prohibited keys absent) | 04 negative assertion | VALIDATED |
| F12 | llm-hop-tracing suite + manifest entry | R5 / 04 T1-T3 | n/a | the suite itself | VALIDATED |
| F13 | docs/workflows/llm-tracing.md + Jaeger/Tempo targets | R6 / 05 T1 | n/a (docs leaf: cross-refs resolve) | n/a | VALIDATED |

Delta protocol: if a returned leaf's tests diverge from this matrix, the
LESSER of plan/tests is updated to reflect the GREATER (test deletion needs
a written rationale in Review Notes). Statuses: PENDING | DELTA | VALIDATED.

Delta log: (none yet)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution. The
ONLY sanctioned pre-dispatch mutation: folding leaf 00's research into C8
(and the leaves' attribute tables) BEFORE group C dispatches.

## Open Questions

1. **session_tool_calls has no runtime writer (verified 2026-10-31):**
   `grep -rn 'session_tool_calls' --include='*.go'` hits only
   `internal/session/store_sqlite.go`; `SaveToolCalls` (store_sqlite.go:2234)
   has zero non-test callers. Leaf 02 therefore delivers the columns, struct
   fields, and roundtrip pins at store level; any future runtime writer
   inherits trace correlation via `session.ToolCall` automatically. If the
   user wants the loop to persist executed tool calls through SaveToolCalls
   as part of this tree, that is a scope extension - do NOT add it silently.
2. **Parent linkage for hop spans:** the turn span is materialized only at
   turn terminal, so hop spans carry ParentSpanID = minted turn span ID
   without a materialized parent at emit time. Jaeger/Tempo render this
   correctly as long as the turn span eventually exports with that span ID
   (it does, via C5/C6). If a receiver complains about orphan parent IDs,
   fall back to ParentSpanID = "" and correlate purely by trace_id - decide
   at leaf 03 review, not silently.
3. **Latency sources:** all 12 recordUsageStore call sites currently pass
   latencyMs = 0 (verified). Leaf 01 must source a real start timestamp per
   site; if a site has no start in scope, it measures from the earliest
   point available and documents the choice in its report.

## Notes

- OTLP exporter prior art to mirror: capture-server + lenient protobuf
  decode lives in `internal/metrics/otlp_trace_test.go`
  (`newCaptureTraceServer`, `parseProtobuf`) - leaf 03 extends those pins;
  leaf 04's harness capture re-implements the decode in `e2e/harness`
  (e2e cannot import internal test files).
- The e2e harness fake LLM (`e2e/harness/fakellm.go`) already speaks the
  OpenAI wire shape incl. tool calls and usage - leaf 04 scripts one
  tool-call turn and one plain turn.
- Config normalization: `metrics.otlp_traces.endpoint` defaults to
  localhost:4318 when enabled with no value (config load boundary) - the
  e2e sets it explicitly to the capture receiver.
- Known idle risk: `internal/agent/loop.go` is ~8K lines; leaf 01 touches
  narrow seams (attemptOpts construction ~6335-6394, executeToolCalls
  ~7914). Instruct the implementer to patch with full verbatim context,
  never line-number feeds.
