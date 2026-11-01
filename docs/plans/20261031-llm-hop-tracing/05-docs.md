# Leaf 05 - Documentation: docs/workflows/llm-tracing.md

## DISPATCH INSTRUCTION

**You are implementing this leaf as a dispatched documentation agent.**
Write ONE user-facing workflow document plus targeted cross-reference
edits. Do NOT commit. Do NOT run git add.

- Parent: `master.md` (contracts C5-C9 and the Architecture section apply)
- Verify command: the self-verification checks below (cross-reference
  resolution + grep gates) - docs-only leaf, no tests/build.
- **Line-number corruption rule:** inspect existing files with
  `search_files` or `terminal cat` - NEVER pipe `read_file` output into
  `write_file`. Write once and stop.

## Meta

- **Parent:** master.md
- **Dependencies:** leaf 03 COMPLETE (leaf 04 recommended, may run in parallel)
- **Estimated Context:** ~35K
- **Concurrency Group:** D

## Goal

Author `docs/workflows/llm-tracing.md` (the feature doc required by the
repo's Feature Documentation Requirements: `internal/metrics` feature ->
`docs/workflows/`), document Jaeger/Tempo (and Langfuse) as export targets,
and wire cross-references from the adjacent docs. Docs-only: NO code, NO
config template changes beyond the documented example, NO schema edits.

## Dependencies

- Leaf 03 COMPLETE (final attribute names from RESEARCH-FINDINGS.md are
  implemented; document what EXISTS, not intentions).
- Leaf 04 recommended (suite name referenced) but may run in parallel -
  reference the suite by its manifest-declared name.

## Estimated Context

~35K

## Context

Read first:
- `docs/workflows/` sibling docs for tone/structure (e.g.
  docs/workflows/session-affinity.md, docs/workflows/migrations.md - short
  form: goal, config, behavior, verification, troubleshooting).
- `internal/config/otlp_traces.go` - full config surface (enabled, endpoint,
  headers, sample_rate, export_interval; Langfuse note + ingestion header).
- `docs/plans/20261031-llm-hop-tracing/master.md` C6/C7/C8 - span types,
  wiring posture, attribute tables (use the FINAL names from
  RESEARCH-FINDINGS.md).
- `docs/plans/20261031-llm-hop-tracing/RESEARCH-FINDINGS.md` - normative
  attribute reference + Jaeger/Tempo lookup notes.
- `docs/generated/bus-topology.md` - the agent.event.tool_execution_end /
  turn.terminal topics now consumed by the tracing wiring.

## Verified Anchors

- internal/config/otlp_traces.go:32 - OTLPTracesConfig fields + defaults
- internal/daemon/otlp_traces_wiring.go:52/98 - disabled Warn / enabled Info
- internal/metrics/otlp_trace_producer.go - span types chat.turn, task.step,
  llm.call, tool.call
- docs/plans/20261031-llm-hop-tracing/RESEARCH-FINDINGS.md - FINAL ATTRIBUTE
  TABLE (normative names)
- docs/configuration/ - where config references live (add a pointer if a
  metrics section exists; do not restructure existing docs)

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `docs/workflows/llm-tracing.md` - the complete user-facing doc (structure
  below).
- Cross-reference edits: AGENTS.md feature-doc mapping is implicit (no AGENTS
  edit needed unless the review finds a stale claim); a one-line pointer
  from docs/configuration (or docs/workflows index if one exists) to the
  new page, matching how sibling docs cross-link.

### What This Leaf Consumes

- C6/C7/C8 final state; RESEARCH-FINDINGS.md names; leaf 04's suite name
  `llm-hop-tracing` and its verification command.

## Tasks

### Task 1: Write docs/workflows/llm-tracing.md

Required sections (ASCII only, lowercase UI-style tone for commands):

1. **What you get** - one trace per chat turn; span types table
   (chat.turn, task.step, llm.call, tool.call) with what each carries
   (GenAI attributes: provider, model, token counts incl. cache
   read/write + reasoning, latency; tool name + argument SHAPE - key names
   and byte count, never values).
2. **Configuration** - the `[metrics.otlp_traces]` block: enabled,
   endpoint (default http://localhost:4318), headers (THE sanctioned secret
   channel - auth tokens go here and are never logged), sample_rate
   (deterministic per trace), export_interval. Explicit default-off
   statement: disabled = zero network, zero goroutines, one boot-time Warn.
3. **Export targets** - Jaeger (OTLP/HTTP :4318 ingest), Grafana Tempo,
   self-hosted Langfuse (`/api/public/otel` + `x-langfuse-ingestion-version:
   4` header example). One minimal working config snippet per target.
4. **Correlating traces with SQL** - `llm_calls.trace_id` / `span_id`
   (metrics.db): find the slow/expensive call in SQL, open the trace by ID
   in Jaeger/Tempo UI (Tempo lookup-by-trace-ID / TraceQL note from
   RESEARCH-FINDINGS); "" trace_id = legacy row or pre-mint path. Mention
   `session_tool_calls.trace_id/span_id` columns exist for tool-call rows.
5. **Verification** - `make e2e-fast-area AREA=llm-hop-tracing` proves the
   pipeline hermetically; what it asserts (single shared trace, attributes,
   SQL join, quiet disabled boot).
6. **Troubleshooting** - armed boot but no spans (sampling, receiver URL,
   circuit-open Warns from the sink), missing tool.call spans (event flow),
   partially sampled traces explained (deterministic per-trace sampling
   keeps all-or-nothing per trace).
7. **Privacy guarantees** - span attributes carry NO prompt text, NO
   completions, NO raw tool arguments/results; only shapes and counts;
   secret material only via headers.

### Task 2: Cross-references

Add one-line pointers TO the new doc from the most relevant existing pages
(docs/configuration metrics section if present; the workflows doc that
discusses metrics or OTLP today - grep docs/ for "otlp" and update those
hits' "see also" areas). Do not restructure any existing page.

## Self-Verification Checklist

- [ ] llm-tracing.md contains all 7 required sections
- [ ] Every config field documented matches OTLPTracesConfig exactly
      (names, defaults, types)
- [ ] Attribute names in the doc match RESEARCH-FINDINGS.md FINAL TABLE
      byte-for-byte (grep-verify each name you write against the file)
- [ ] No secret material examples (use placeholder tokens like
      "<your-auth-header>")
- [ ] ASCII only (grep -P '[^\x00-\x7F]' docs/workflows/llm-tracing.md -> empty)
- [ ] Cross-reference edits are additive one-liners; no page restructured
- [ ] All relative links resolve (check target files exist)
- [ ] Suite name referenced: llm-hop-tracing; command:
      make e2e-fast-area AREA=llm-hop-tracing
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (For Review Agent)

- [ ] Doc reflects IMPLEMENTED reality (spot-check 3 claims against code)
- [ ] Config snippet copies load cleanly as JSON5 (balanced braces, no
      comments inside values)
- [ ] Jaeger/Tempo/Langfuse instructions match RESEARCH-FINDINGS sources
- [ ] No duplication of sibling docs' content (links, not copies)
- [ ] Tone/structure matches docs/workflows siblings; ASCII throughout

Output: APPROVED or specific gaps.

## Notes

- The doc is the user's window into the feature - prefer concrete commands
  and config over architecture prose (the master plan holds the design).
- If RESEARCH-FINDINGS renames any attribute vs master C8, the findings
  version is what users will see exported - document THAT.
