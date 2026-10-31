# Optional Langfuse OTLP Trace Sink - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** An opt-in OTLP/HTTP trace exporter that maps chat turns and
  task steps to spans, aimed at (but not coupled to) a self-hosted Langfuse.
- **Dependencies:** none
- **Estimated Context:** 80K
- **Concurrency Group:** A (parallel with 01 and 05)

## Goal

Operators who run Langfuse (open-source LLM observability; accepts OTLP
traces at /api/public/otel) get web-based trace exploration of meept turns
and steps. The exporter is strictly opt-in, disabled by default, best-effort
(it can never fail, slow down, or block a turn), and shares the existing
metrics-OTLP config style so it feels native.

## Context

Meept already ships a metrics-only OTLP pipeline: config key `metrics.otlp`
(top-level Metrics.OTLP; opt-in default false), an HTTP-JSON sink, a
batching exporter, and daemon wiring in internal/daemon/otlp_wiring.go.
There are NO spans/traces anywhere yet - this leaf is the first trace
producer. Langfuse v3+ receives OTLP/HTTP protobuf spans; a plain OTLP
endpoint must also work (Langfuse is just the target we tune attributes
for: langfuse.user.id, langfuse.session.id, trace_name).

## Verified Anchors

Anchors grep-confirmed 2026-10-30. Re-grep before editing.

- internal/config/schema.go (Metrics/OTLP block; PlansStorageConfig at :511-514 shows sibling style) - add OTLPTracesConfig sibling of metrics.otlp.
- internal/config/config.go:68-71 - metrics.otlp normalization - mirror for traces.
- internal/daemon/otlp_wiring.go:31-67 - wireOTLPExporter (metrics) - the wiring pattern to mirror.
- internal/metrics/otlp_http_sink.go - HTTP-JSON sink to copy for the trace sink.
- internal/metrics/otlp_exporter.go - batching pattern to copy.
- internal/daemon/components.go:323 - Components.OTLPExporter field - add a Traces sibling.
- internal/daemon/verdict_audit.go:83 - example of a small self-contained wiring helper.
- docs/generated/bus-topology.md:20-228 - topic/payload reference for attribute sources (agent_id, session/conversation ids, task/step ids).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/config/schema.go - C5 (byte-identical to master)
type OTLPTracesConfig struct {
    Enabled  bool   `json:"enabled"`   // default false - opt-in
    Endpoint string `json:"endpoint"`  // e.g. http://langfuse:3000/api/public/otel
    Headers  map[string]string `json:"headers,omitempty"` // auth etc.
    SampleRate float64 `json:"sample_rate,omitempty"` // 0..1, default 1.0
}
// internal/metrics/otlp_trace_sink.go + otlp_trace_exporter.go
type TraceSink interface {
    ExportSpans(ctx context.Context, spans []Span) error
}
type Span struct {
    TraceID [16]byte; SpanID [8]byte; ParentSpanID [8]byte
    Name string; Start, End time.Time
    Attributes map[string]string // gen_ai.*, langfuse.* per contract
}
// internal/daemon: wireOTLPTraces(cfg OTLPTracesConfig, ...) (*TraceExporter, error)
// Span producers: turn start/terminal -> span "chat.turn"
// (attrs: session_id, agent_id, model); step start/complete ->
// child span "task.step" (attrs: task_id, step_id, state, agent_id).
```

### What This Leaf Consumes

- Existing metrics-OTLP batcher/sink patterns (style only, no shared code
  requirement); turn/step lifecycle events from the agent loop or bus
  (turn.terminal typed topic is the proven terminal signal).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Span types + trace sink + batched exporter (OTLP/HTTP protobuf) | Create: internal/metrics/otlp_trace_sink.go, internal/metrics/otlp_trace_exporter.go; Test: internal/metrics/otlp_trace_test.go | `go test -p 2 ./internal/metrics/ -run TestTrace` | types undefined | spans serialize to valid OTLP export request; httptest server receives and decodes them; batching coalesces; sample_rate honored | Given 3 spans and a capture server; When flushed; Then server decodes 3 spans with correct IDs/names |
| T2 | Config block + daemon wiring + turn/step span producers | Modify: internal/config/schema.go, internal/config/config.go, internal/daemon/components.go, new internal/daemon/otlp_traces_wiring.go; Test: internal/daemon/otlp_traces_wiring_test.go + internal/config otel traces config test | `go test -p 2 ./internal/config/ ./internal/daemon/ ./internal/metrics/` | key absent / wiring nil | disabled by default (zero network activity when off); when enabled + endpoint set, a real turn emits chat.turn and task.step spans to the test endpoint; export failure never fails the turn (error logged once, circuit opens) | Given enabled config pointing at httptest server; When one turn completes; Then server saw chat.turn with session_id attr |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C5 shapes)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Default OFF; zero allocations/network when disabled
- [ ] Export path never blocks the turn (async batch, bounded queue, drop
      on overflow with counter)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Opt-in proven by test (disabled = no sink constructed, no network)
- [ ] Turn path untouched on export failure (circuit + single log line)
- [ ] No external OTel SDK dependency added unless build-graph proves it
      clean (prefer hand-rolled minimal OTLP writer matching the existing
      metrics sink style - report the decision)
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Langfuse needs the `x-langfuse-ingestion-version: 4` header for real-time
  OTLP ingestion - put it in the docs comment and allow it via Headers
  config; do NOT hardcode Langfuse-specific behavior into the exporter.
- Trace/span IDs: crypto/rand (never math/rand - repo predid analyzer).
- Attribute naming: gen_ai.* conventions plus langfuse.user.id /
  langfuse.session.id / trace_name on EVERY span (Langfuse filters operate
  per-observation).
- Secret material (API keys) arrives via Headers config only; never logged.
- This leaf is OPTIONAL to the tree's requirements: master R7. If Langfuse
  scoping is dropped later, only this leaf is discarded - no other leaf
  depends on it.
