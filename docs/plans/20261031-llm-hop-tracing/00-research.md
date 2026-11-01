# Leaf 00 - Research: OpenTelemetry GenAI Semantic Conventions for LLM-Hop Tracing

> **For the implementing agent:** You are a RESEARCH agent. You write ONE
> document (RESEARCH-FINDINGS.md in this directory) and touch NO code. Do
> NOT commit. Do NOT run git add. Use web_search / web_extract to ground
> every claim in a citable source URL.

## Meta

- **Parent:** master.md
- **Scope:** Produce the authoritative attribute-name reference for leaf 03's
  span implementation, plus span-hierarchy and trace-correlation design notes.
- **Dependencies:** none. This leaf runs FIRST; its C8 refinement gates leaf 03.
- **Estimated Context:** ~25K
- **Concurrency Group:** A (alone)

## Goal

Leaf 03 will emit two new span types with OpenTelemetry GenAI
semantic-convention attributes. The attribute names must be verbatim-correct
against upstream (paraphrased names are a defect - Pitfall 21), and the
hierarchy/correlation design must follow how OTel expects LLM+tool hops to
relate. This leaf pins those facts so the implementation leaves never guess.

## Context

- Repo prior art: `internal/metrics/otlp_trace_producer.go` (chat.turn /
  task.step spans), `internal/metrics/otlp_trace_sink.go` (Span struct,
  Attributes map[string]string), `internal/daemon/otlp_traces_wiring.go`
  (bus-driven producer posture). The existing sink flattens attribute values
  to strings; that is a meept implementation detail, not an OTel constraint.
- Upstream attribute registry lives at
  https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/
  and the source-of-truth repo
  https://github.com/open-telemetry/semantic-conventions-genai
  (docs/gen-ai/gen-ai-spans.md + model/gen-ai/registry.yaml).
- The orchestrator's C8 table (master.md) is the DRAFT this leaf verifies.

## Verified Anchors

- docs/plans/20261031-llm-hop-tracing/master.md - C8 draft attribute table
  (the object under verification)
- internal/metrics/otlp_trace_sink.go:70-85 - Span struct (TraceID [16]byte,
  SpanID [8]byte, Attributes map[string]string, correlation fields)
- internal/metrics/otlp_trace_producer.go:74,124 - RecordTurnSpan /
  RecordStepSpan (the posture new Record* methods will mirror)

## Tasks

### Task 1: Verify and finalize the C8 attribute table

Use web_search / web_extract against the GenAI semantic-conventions registry
and gen-ai-spans doc. For EVERY attribute in master.md C8 confirm: exact key
string, type, stability, and whether the current draft name is current vs
deprecated/replaced. Specifically confirm:

- `gen_ai.provider.name` (replacement of deprecated `gen_ai.system`)
- `gen_ai.operation.name` value sets (what constant is recommended for a
  chat completion call; whether `execute_tool` is sanctioned for tool spans
  or whether tool spans use a different operation value)
- `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`,
  `gen_ai.usage.cache_read.input_tokens`,
  `gen_ai.usage.cache_write.input_tokens`,
  `gen_ai.usage.reasoning.output_tokens` (note inclusion rules: cache and
  reasoning counts are SUBSETS of the totals)
- `gen_ai.request.model`, `gen_ai.response.model`, `gen_ai.tool.name`,
  `gen_ai.tool.call.id`
- Span naming convention for GenAI client spans (the
  `{operation} {model}` pattern, e.g. `chat gpt-4`) and whether `llm.call`
  is an acceptable deviation (document the trade-off; meept's existing
  spans use dotted lowercase names - consistency vs convention).
- Error/status conventions: `error.type` vs `error.message` on failed GenAI
  spans; what OTel recommends for the span STATUS on provider errors.

### Task 2: Span hierarchy for LLM + tool hops

Research and summarize (1-2 paragraphs each, with source URLs):
- OTel's parent/child model for an agent turn: turn span as parent,
  per-LLM-call spans and per-tool spans as children; whether sibling
  spans referencing a parent that exports later is valid (the "parent not
  yet received" case in Jaeger/Tempo).
- Deterministic head sampling per trace (trace-ID-derived decisions)
  practice and how Jaeger/Tempo handle partially-sampled traces.

### Task 3: Trace-ID correlation with SQL stores

Summarize best practice for joining exported traces to application SQL
rows by trace_id: storing trace_id as a plain indexed TEXT column (hex)
vs binary(16); query patterns (find slow calls in SQL, open the trace by
ID in Jaeger/Tempo UI); any documented Jaeger/Tempo guidance on
lookup-by-trace-ID (e.g. Tempo's TraceQL / "search by trace ID" flow).

## Deliverable

Write `docs/plans/20261031-llm-hop-tracing/RESEARCH-FINDINGS.md` containing:
1. FINAL ATTRIBUTE TABLE (the corrected C8, same two-table shape, one
   "source URL" column; mark any name the master draft got wrong with
   `DRAFT-WRONG: <old> -> <new>`).
2. Hierarchy design notes (Task 2) with a one-line RECOMMENDATION for
   meept's ParentSpanID question (master.md Open Questions #2).
3. Correlation notes (Task 3) with a one-line RECOMMENDATION for the
   llm_calls column type choice (TEXT hex is the plan default; confirm or
   refute).
4. Source list (every URL consulted).

## Self-Verification Checklist

- [ ] Every C8 attribute row re-verified against a URL; no unverifiable names
- [ ] Recommendations present for Open Questions #2 and the column-type choice
- [ ] RESEARCH-FINDINGS.md is the ONLY file written; ASCII only
- [ ] No code files touched (`git status --short` shows only RESEARCH-FINDINGS.md)
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (For Review Agent)

- [ ] Each attribute name has a working source URL
- [ ] Any DRAFT-WRONG corrections are unambiguous (old -> new pairs)
- [ ] Recommendations are decisions, not hedged options
- [ ] No code changes anywhere in the repo

Output: APPROVED or specific gaps.

## Notes

Budget ~25K: this is search-heavy but write-light. Do not paste entire web
pages; extract only the verified rows and the 1-2 paragraph notes.
