# Leaf 03 - Span Producer: llm.call + tool.call Spans and Daemon Wiring

## DISPATCH INSTRUCTION

**You are implementing this leaf as a dispatched implementation agent.**
Read this document fully, then implement every task in order with TDD
(failing pin -> run to confirm failure -> minimal implementation -> run to
confirm pass). Test additions go into the EXISTING test files of the
touched packages (repo policy: no new unit test files for new features).

- Parent: `master.md` (contracts C5, C6, C7, C8, C9 apply to you; C8 as
  refined by leaf 00's RESEARCH-FINDINGS.md FINAL ATTRIBUTE TABLE - if the
  findings correct any attribute name, the corrected name is NORMATIVE and
  this leaf's C8 quotes are superseded)
- Verify command: `go test -p 2 ./internal/metrics/... ./internal/daemon/... -count=1`
- **Do NOT commit. Do NOT run git add.** Write code, run tests, report
  results only. The orchestrator handles all git operations.
- **Line-number corruption rule:** inspect existing files with
  `search_files` or `terminal cat` - NEVER pipe `read_file` output into
  `write_file`. After writing a file, do NOT read it back - write once and
  stop. Patch with full verbatim context strings.

## Meta

- **Parent:** master.md
- **Dependencies:** leaves 00, 01, 02 COMPLETE
- **Estimated Context:** ~85K
- **Concurrency Group:** C

## Goal

Two new span types on the existing TraceProducer (`llm.call`, `tool.call`)
with OpenTelemetry GenAI semantic-convention attributes; turn span adopts
the minted trace/span IDs; daemon wiring: SetLLMCallObserver -> producer
bridge and a raw-topic subscription for tool-execution-end events.
Nil-exporter no-op posture preserved and proven by tests. NO schema work
(leaf 02), NO threading (leaf 01 - its fields/events already exist).

## Dependencies

- Leaf 01 COMPLETE: TurnSpanSpec.TraceID/SpanID, ToolExecutionEndData
  fields, SetLLMCallObserver, LLMCallRecord.TraceID/SpanID must exist.
- Leaf 02 COMPLETE: llm_calls columns (the e2e joins them; producer itself
  does not read SQL).
- Leaf 00 COMPLETE: RESEARCH-FINDINGS.md FINAL ATTRIBUTE TABLE is the
  normative attribute reference for this leaf.

## Estimated Context

~85K (producer + wiring + capture-server test extensions; decode helper in
daemon for the AgentEvent envelope).

## Context

Read first:
- `internal/metrics/otlp_trace_producer.go` (whole file - TurnSpanSpec :18,
  RecordTurnSpan :74, RecordStepSpan :124; nil-check posture; sampler
  application BEFORE RecordSpan; startRef floor).
- `internal/metrics/otlp_trace_sink.go:70-85` - Span struct: TraceID
  [16]byte / SpanID [8]byte are BINARY - convert your 32/16-hex strings
  with encoding/hex.Decode into fixed arrays; Attributes map[string]string.
- `internal/metrics/otlp_trace_test.go` - newCaptureTraceServer +
  parseProtobuf lenient decode + makeSpan helpers; EXTEND these, do not
  reinvent.
- `internal/daemon/otlp_traces_wiring.go` - wireOTLPTraces :47 (disabled
  Warn :52; enabled Info ~:98), turn sub pattern, stepCompletedPayload
  lenient decode pattern. components.go:847 call site.
- Leaf 01's additions: `ToolExecutionEndData` json tags (`trace_id`,
  `span_id`, `arguments_keys`, `arguments_bytes`), `SetLLMCallObserver`,
  `LLMCallRecord.TraceID/SpanID`, `TurnSpanSpec.TraceID/SpanID`.
- AgentEvent envelope: `internal/agent/emitter.go` bridgeToBus publishes
  `models.BusMessage` on topic `agent.event.tool_execution_end` with
  Payload = serialized `agent.AgentEvent` JSON (Data holds
  ToolExecutionEndData). Decode leniently - do not import internal/agent
  types into the decoder (mirror how recordStepSpanFromBus avoids it).

## Verified Anchors

- internal/metrics/otlp_trace_producer.go:18/38 - TurnSpanSpec/StepSpanSpec
- internal/metrics/otlp_trace_producer.go:74/124 - RecordTurnSpan/
  RecordStepSpan (posture to mirror)
- internal/metrics/otlp_trace_sink.go:70-85 - Span struct (binary IDs,
  string attributes)
- internal/metrics/otlp_trace_test.go:40/162/203 - parseProtobuf, decode
  chain, newCaptureTraceServer
- internal/daemon/otlp_traces_wiring.go:47-99 - wireOTLPTraces (disabled
  Warn at :52, enabled Info at end)
- internal/daemon/otlp_traces_wiring.go:66-80 - turn subscription pattern
- internal/daemon/otlp_traces_wiring.go:82-95 - raw-topic step subscription
  + goroutine pattern (mirror for tool events)
- internal/daemon/components.go:847 - wireOTLPTraces call site (needs the
  llm client handle for SetLLMCallObserver - inspect how the client is
  reachable there; if not, thread it as a parameter to wireOTLPTraces,
  updating the single call site)
- internal/agent/emitter.go - BusTopic convention `agent.event.<type>`
- internal/metrics/store.go:757 - LLMCallRecord (TraceID/SpanID fields from
  leaf 01)

## Interface Contracts (From Parent)

### What This Leaf Exposes

C6 (signatures frozen in master.md): `LLMCallSpanSpec`, `ToolCallSpanSpec`,
`RecordLLMCallSpan`, `RecordToolCallSpan` - span names `llm.call` /
`tool.call`, nil-exporter no-op, sampler applied with TraceID set, ev.SpanID
reused as exported span ID, ErrorMessage/Error truncated to 256 chars,
LatencyMS/DurationMS > 0 drive Start/End (0 = instantaneous span).
C7 (wiring): observer bridge + raw-topic subscription inside the Enabled
branch only; no new steady-state log lines.

### What This Leaf Consumes

- C1 `pkg/id`: hex decode only (IDs arrive as strings from leaf 01).
- C4 `SetLLMCallObserver(fn func(appmetrics.LLMCallRecord))` - leaf 01.
- C5 event fields: `ToolExecutionEndData` json tags - leaf 01.
- C5 `TurnSpanSpec.TraceID/SpanID` fields - leaf 01 (you USE them).
- C8 FINAL ATTRIBUTE TABLE from RESEARCH-FINDINGS.md (normative).

## Tasks

### Task 1: Producer span methods + turn-span ID adoption

**Files:** Modify `internal/metrics/otlp_trace_producer.go`; extend pins in
`internal/metrics/otlp_trace_test.go`.

1. Failing pins (extend existing test file):
   (a) RecordLLMCallSpan with a live capture exporter emits exactly one
   span named `llm.call` with TraceID == decoded ev.TraceID, SpanID ==
   decoded ev.SpanID (NOT a random fill), ParentSpanID == decoded
   ev.ParentSpanID, attributes exactly per C8 (gen_ai.usage.input_tokens =
   strconv of TokensSent, etc.; omitted when zero; error.message only when
   IsError, truncated at 256).
   (b) RecordToolCallSpan same shape for `tool.call` (gen_ai.tool.name,
   gen_ai.tool.call.id, meept.tool.args.keys, meept.tool.args.bytes,
   meept.tool.blocked only-when-true, error.message only on failure).
   (c) Nil producer / nil exporter: both methods are no-ops (no panic, no
   emit).
   (d) Sampler: with a sampler that rejects a given trace ID, BOTH hop spans
   and the turn span for that trace are dropped; with an accepting sampler
   all are kept (determinism per trace).
   (e) TurnSpanSpec with TraceID/SpanID set -> chat.turn span uses them
   (byte-identical to the hop spans' trace), not a random fill; with "" ->
   random fill (unchanged legacy behavior).
   (f) A 300-char ErrorMessage truncates to exactly 256.
2. Implement per C6; hex-decode with encoding/hex (invalid hex -> fall back
   to random fill for that span and note nothing - malformed IDs are a
   programming error upstream, degrade silently).
3. Verify: `go test -p 2 ./internal/metrics/... -count=1` -> PASS.

Given LLMCallSpanSpec{TraceID: "a1...", SpanID: "b1...", TokensSent: 52,
TokensRecv: 47, IsError: false}; When recorded through a capture exporter;
Then the decoded span has name llm.call, exact binary IDs, attributes
gen_ai.usage.input_tokens "52" / gen_ai.usage.output_tokens "47", and no
error.message.

### Task 2: Daemon wiring - observer + tool-event subscription

**Files:** Modify `internal/daemon/otlp_traces_wiring.go` (+ its existing
test file), `internal/daemon/components.go` (only the :847 call site if a
parameter is threaded).

1. Failing pins:
   (a) disabled config -> SetLLMCallObserver is NOT attached (client
   receives no observer; assert via the wiring return or a seam) and the
   boot log contains exactly one disabled Warn line;
   (b) enabled config -> boot log contains exactly one
   "OTLP trace export enabled" Info; a produced LLMCallRecord routed
   through the attached observer results in a queued llm.call span
   (use a capture sink; Flush deterministically);
   (c) a bus message on "agent.event.tool_execution_end" (build the
   AgentEvent JSON envelope with ToolExecutionEndData incl. trace_id /
   arguments_keys) -> one tool.call span queued with the decoded values;
   malformed envelope -> no panic, no span;
   (d) enabled wiring + turn event carrying trace_id -> chat.turn span
   carries that trace ID.
2. Implement: in the Enabled branch (C7): turn sub passes ev.TraceID /
   ev.SpanID into TurnSpanSpec; add observer attach (map C4 record -> C6
   LLMCallSpanSpec; ParentSpanID = the turn span ID when the wiring can
   associate it - i.e., keep a last-seen turnSpanID per conversation in the
   wiring closure, else ""); add the raw-topic subscription mirroring the
   step-sub goroutine pattern with a lenient map-based decode of the
   AgentEvent envelope (type + data discrimination by json keys; never
   import internal/agent). Disabled path: attach nothing.
3. Verify: `go test -p 2 ./internal/daemon/... -count=1` -> PASS.

Given otlp_traces.enabled=true and one observed LLMCallRecord; When the
exporter flushes; Then exactly one llm.call span exists whose attributes
match the record and whose trace_id equals the record's TraceID.

### Task 3: Secret-free attribute guarantee

**Files:** pins in the same metrics/daemon test files.

1. Failing pins: (a) a ToolCallSpanSpec populated from an end-event whose
   arguments contained values (e.g. `{"path":"/etc/shadow","token":"x"}`)
   exposes ONLY arguments_keys "path,token" and arguments_bytes - assert
   NO captured attribute value contains "shadow", "x", or any raw value
   substring; (b) LLMCallRecord.ErrorMessage with secret-looking content is
   present only in the truncated error.message attribute (documented C8
   exception: provider error strings are not secret material; assert the
   256-char truncation bound holds); (c) attribute KEY SET of each span
   type is exactly the C8/FINAL-TABLE allowlist (no extra keys even when
   spec fields are populated).
2. Verify: `go test -p 2 ./internal/metrics/... ./internal/daemon/... -count=1` -> PASS.

Given a tool event whose raw arguments embed secrets; When the span is
captured; Then no attribute carries any argument value - only key names
and byte count.

### Task 4: Leaf-package verification

1. `go test -p 2 ./internal/metrics/... ./internal/daemon/... -count=1` -> PASS.
2. `go build ./...` -> clean. `gofmt -l` -> empty. `go vet` -> clean.
3. `make mutexio && make predid` -> clean.
4. `git status --short` -> only declared files.

## Self-Verification Checklist

- [ ] Verify commands pass with `-count=1`
- [ ] Span names exactly `llm.call` / `tool.call`; attribute keys exactly
      the FINAL ATTRIBUTE TABLE allowlist (leaf 00 corrections applied)
- [ ] ev.SpanID reused as exported span ID; hex-decode fallback documented
- [ ] Sampler determinism per trace pinned (hop + turn sampled together)
- [ ] Nil-exporter / disabled wiring: no panics, no spans, no observer attach
- [ ] Boot log lines: one Warn (disabled) / one Info (enabled) - asserted
- [ ] Truncation at 256 pinned for both error fields
- [ ] No raw argument values reach any attribute (negative pins pass)
- [ ] No raw-topic import of internal/agent in the daemon decoder
- [ ] make mutexio / predid / gofmt / go vet clean
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (For Review Agent)

- [ ] C6 signatures match master.md byte-for-byte (field names, types)
- [ ] Attribute emission follows inclusion rules: omitted when zero/empty,
      subset-token counts not double-reported, error.message only on error
- [ ] Turn span keeps minted IDs when provided (pin e) - no double minting
- [ ] Observer bridge never blocks or fails the LLM call path (async
      posture preserved; no new mutex across I/O)
- [ ] Raw-topic decode is lenient (malformed payload -> no panic, no span)
- [ ] Disabled posture: nothing attached, nothing built (pin a)
- [ ] Only declared files under internal/metrics, internal/daemon changed
- [ ] No debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- The wiring closure's turnSpanID-per-conversation association is
  best-effort (Open Questions #2): if RESEARCH-FINDINGS recommends
  ParentSpanID = "" instead, implement that and say so in your report.
- components.go:847: inspect what handles exist in scope. If the llm
  *client is not reachable, add a parameter to wireOTLPTraces and update
  the one call site - do NOT reach for globals.
- The capture-server helpers in otlp_trace_test.go decode a subset of the
  protobuf; extend the attribute iteration if string-typed new attributes
  are not surfaced yet.
