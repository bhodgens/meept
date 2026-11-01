# Leaf 01 - Trace-ID Minting and Request-Scoped Threading (turn -> loop -> LLM client -> executor)

## DISPATCH INSTRUCTION

**You are implementing this leaf as a dispatched implementation agent.**
Read this document fully, then implement every task in order with TDD
(failing pin -> run to confirm failure -> minimal implementation -> run to
confirm pass). Pins are ADDED to existing test files; do not create new
unit test files.

- Parent: `master.md` (contracts C1, C2, C4, C5, C8, C9 apply to you)
- Verify command: `go test -p 2 ./pkg/id/... ./internal/llm/... ./internal/agent/... -count=1`
- **Do NOT commit. Do NOT run git add.** Write code, run tests, report
  results only. The orchestrator handles all git operations.
- **Line-number corruption rule:** inspect existing files with
  `search_files` or `terminal cat` - NEVER pipe `read_file` output into
  `write_file`. After writing a file, do NOT read it back - write once and
  stop. Patch with full verbatim context strings.

## Meta

- **Parent:** master.md
- **Dependencies:** leaf 00 reviewed (research); leaf 02 concurrent (group B)
- **Estimated Context:** ~95K
- **Concurrency Group:** B

## Goal

Mint a trace ID + turn span ID at chat-turn start; thread the trace ID
request-scoped into every LLM call (`llm.WithTraceID`), into tool-executor
hand-off (context value), into the tool-execution end event, and into
`TurnTerminalEvent`; stamp `LLMCallRecord` with TraceID/SpanID and REAL
latency at every recordUsageStore call site; add the `SetLLMCallObserver`
seam; regenerate connectivity graphs for the payload key additions.
NO span production (leaf 03), NO schema changes (leaf 02), NO daemon wiring.

## Dependencies

- Leaf 00 (research) reviewed - its findings do not gate this leaf.
- Leaf 02 is CONCURRENT (group B): it owns the llm_calls columns. Your
  records set TraceID/SpanID fields unconditionally; whether the columns
  exist yet does not matter for your compile (the fields are struct-level).

## Estimated Context

~95K (loop.go is ~8K lines but seams are narrow; llm/client.go record
sites are 12 mechanical stamps; pkg/id greenfield; agent handler/events
additive fields; graphs regen).

## Context

Read first:
- `pkg/id/id.go` - Generate() + zero-suffix fallback convention (predictable
  ID prevention rule; predid analyzer).
- `internal/llm/client.go:1150-1330` - chatOptions struct + ChatOption
  pattern; `:1285` WithResolvedModel (the request-scoped pattern to mirror);
  `:1992` recordUsageStore (12 call sites at :708, :758, :854, :867, :976,
  :1010, :1117, :1127, :2330, :2345, :2396, :2481); `:296` SetUsageStore
  (nil-guard setter pattern); `:306` codex.go and `:300` anthropic.go have
  their OWN direct `usageStore.RecordLLMCall` sites - stamp those too.
- `internal/agent/handler.go:763` - turnID mint site (trace ID mints beside
  it); `publishTurnTerminal` call sites (grep them all - every one stamps
  TraceID); `:280-302` TurnTerminalEvent CLOSED contract comment.
- `internal/agent/events.go:190-222` - ToolExecutionStart/EndData payloads.
- `internal/agent/loop.go:6335-6394` - attemptOpts construction (append
  WithTraceID here); `:7914` executeToolCalls (context-value hand-off and
  event emission enrichment); `:4205` flushDeferredToolResults (cached-hop
  path - events may already exist there; enrich if present).
- `internal/agent/emitter.go` - events bridge to bus topic
  `agent.event.<type>` as serialized AgentEvent JSON (leaf 03 subscribes).

## Verified Anchors

- pkg/id/id.go:18 - `func Generate(prefix string) string` - zero-suffix
  fallback convention to mirror
- internal/llm/client.go:1285 - WithResolvedModel - request-scoped
  ChatOption pattern (nil/empty no-op, never shared state)
- internal/llm/client.go:1992-2019 - recordUsageStore - the single choke
  point through which 10 of 12 call sites persist llm_calls rows
- internal/llm/codex.go:306, internal/llm/anthropic.go:300 - direct
  RecordLLMCall sites outside recordUsageStore
- internal/llm/client.go:296-303 - SetUsageStore - nil-guard setter pattern
- internal/agent/handler.go:763 - turnID mint (`id.Generate("turn-")`)
- internal/agent/handler.go:280-302 - TurnTerminalEvent frozen contract
- internal/agent/loop.go:6391/6394 - l.llm.Chat(ctx, messages, attemptOpts...)
- internal/agent/loop.go:7914-8027 - executeToolCalls
- internal/agent/events.go:190-222 - ToolExecutionStart/EndData
- internal/metrics/store.go:757-775 - LLMCallRecord struct (you ADD two
  fields; leaf 02 owns the columns/INSERT)

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C1 `pkg/id/trace.go`: `TraceID() string` (32 hex), `SpanID() string`
  (16 hex), crypto/rand, documented zero-suffix fallback.
- C2 `internal/llm`: `func WithTraceID(traceID string) ChatOption`
  (empty = no-op), `func EffectiveTraceID(opts []ChatOption) string`.
- C2 `internal/agent/tracectx.go`:
  `func WithTraceContext(ctx context.Context, traceID string) context.Context`,
  `func TraceIDFromContext(ctx context.Context) string`.
- C4 `internal/llm`:
  `func (c *Client) SetLLMCallObserver(fn func(appmetrics.LLMCallRecord))`
  (nil-guarded; async fire; never blocks the call path; callback must not
  be invoked synchronously inside the request goroutine's critical section).
- C5 additive fields: `ToolExecutionEndData.TraceID/SpanID/ArgumentsKeys/
  ArgumentsBytes`, `ToolExecutionStartData` stays as-is (shape attributes
  ride the END event only), `TurnTerminalEvent.TraceID` (omitempty),
  `TurnSpanSpec.TraceID/SpanID` (struct fields only - producer use is 03).
- `LLMCallRecord.TraceID` and `.SpanID` string fields (C3's field half;
  leaf 02 owns persistence).

### What This Leaf Consumes

- (none external; C3 columns are leaf 02's - do not write migrations)

## Tasks

### Task 1: pkg/id trace ID minting

**Files:** Create `pkg/id/trace.go`; add pins to `pkg/id/id_test.go` (or
create `pkg/id/trace_test.go` ONLY if no test file exists in the package).

1. Write failing pins: TraceID() returns exactly 32 lowercase hex chars;
   SpanID() exactly 16; two successive calls differ; both parse as hex.
2. Implement with crypto/rand; on ANY entropy error return the zero-suffix
   fallback documented in the same style as Generate() ("000...0" of full
   length) - never time/rand. Document the length + entropy contract in the
   doc comments.
3. Verify: `go test -p 2 ./pkg/id/... -count=1` -> PASS;
   `go run ./tools/analyzers/predid/... ./pkg/id/...` -> clean.

Given pkg/id package exists; When TraceID() is called; Then output is 32
lowercase hex chars unique across calls.

### Task 2: Request-scoped threading through the LLM client + real latency

**Files:** Modify `internal/llm/client.go` (+ test pins in the existing
client test file(s) of internal/llm), `internal/llm/codex.go`,
`internal/llm/anthropic.go`.

1. Failing pins: (a) `WithTraceID("t").(chatOptions)` - via EffectiveTraceID -
   empty options yield ""; WithTraceID("") is a no-op; last-write-wins on
   duplicate WithTraceID (same precedence rules as WithResolvedModel).
   (b) recordUsageStore stamps TraceID/SpanID into the LLMCallRecord.
   (c) SetLLMCallObserver(nil) is a no-op; a set observer receives one
   record per completed call with matching TraceID.
2. Implement: add `traceID`, `spanID` to chatOptions + WithTraceID /
   EffectiveTraceID (mirror WithResolvedModel docs: per-request, never
   shared state, empty = no-op). In recordUsageStore: when chatOpts.traceID
   is non-empty and spanID empty, mint SpanID via pkg/id; stamp both into
   the record. Add SetLLMCallObserver (nil-guarded, mirroring SetUsageStore
   at :296); the observer fires from the same goroutine that calls
   RecordLLMCall (already async per the existing `go func()` at :1997).
3. Latency: add a `start time.Time` captured at the TOP of the Chat request
   path; pass `time.Since(start).Milliseconds()` into every recordUsageStore
   call (replacing the constant 0). Do the same for codex.go:306 and
   anthropic.go:300 direct sites. If a site's start is not naturally in
   scope, capture at the earliest enclosing point and note it in your
   report (master.md Open Questions #3).
4. Verify: `go test -p 2 ./internal/llm/... -count=1` -> PASS (whole package).

Given a chat request with WithTraceID("abc..."); When the call completes;
Then the observer (and llm_calls record) carry traceID "abc...", a fresh
16-hex spanID, and latencyMs > 0 for a non-zero-duration call.

### Task 3: Turn-start mint + loop/executor hand-off + event enrichment

**Files:** Modify `internal/agent/handler.go`, `internal/agent/events.go`,
`internal/agent/loop.go`; create `internal/agent/tracectx.go`; pins in the
existing handler/loop/executor test files.

1. Failing pins: (a) a handled chat turn with no client TurnID still yields
   a 32-hex TraceID on its TurnTerminalEvent (and turnID falls back as
   today); (b) executeToolCalls emits ToolExecutionEndData with
   TraceID == turn trace, SpanID == fresh 16-hex per call, ArgumentsKeys ==
   sorted comma-joined keys of the tool-call arguments JSON, ArgumentsBytes
   == len(raw arguments string); (c) WithTraceContext/TraceIDFromContext
   roundtrip and "" when absent.
2. Implement:
   - handler.go: beside the turnID mint at :763, mint
     `traceID := id.TraceID()` and `turnSpanID := id.SpanID()`; stamp
     TraceID into every publishTurnTerminal call site (grep ALL of them -
     early-failure paths before the mint keep TraceID "", per C5).
   - loop.go: in the attemptOpts construction (~:6335), append
     `llm.WithTraceID(currentTraceID)` alongside WithResolvedModel. The
     loop obtains the turn trace via WithTraceContext set where the turn's
     ctx is built (handler passes it through; if the loop builds ctx itself,
     set it at the same seam that owns turnID - read the actual flow, do not
     assume). In executeToolCalls (:7914): derive traceID from
     TraceIDFromContext (fall back to llm.EffectiveTraceID on the current
     opts if the context path is unavailable at that seam); mint one spanID
     per tool call; compute ArgumentsKeys (parse arguments JSON with
     encoding/json into map[string]any, collect keys, sort.Strings,
     strings.Join ",") and ArgumentsBytes (len of the raw arguments string;
     unparseable arguments -> ArgumentsKeys "" and bytes = raw length);
     stamp all four fields on the emitted ToolExecutionEndData.
   - events.go: add the four C5 fields to ToolExecutionEndData with the
     exact json tags `trace_id`, `span_id`, `arguments_keys`,
     `arguments_bytes` + omitempty.
   - handler.go: add `TraceID string \`json:"trace_id,omitempty"\`` to
     TurnTerminalEvent and UPDATE the CLOSED-field-set contract comment +
     docs/plans/20260916-turn-lifecycle-events/master.md extension note
     (one line: field added by 20261031-llm-hop-tracing, additive/omitempty,
     consumers checked).
   - metrics: add TurnSpanSpec.TraceID/SpanID fields (internal/metrics/
     otlp_trace_producer.go) - producer behavior unchanged (leaf 03).
3. Verify: `go test -p 2 ./internal/agent/... -count=1` -> PASS.

Given one chat turn that executes two tools; When the turn terminates; Then
both ToolExecutionEndData events and the TurnTerminalEvent carry the SAME
32-hex trace_id and distinct span_ids, and arguments_keys is shape-only
(key names, never values).

### Task 4: Regenerate connectivity graphs

The ToolExecutionEndData and TurnTerminalEvent payload key additions are
bus payload changes. Run `make graphs` and confirm `make graphs-check`
passes; include regenerated `docs/generated/*` files in your report's
file list (do NOT commit them - orchestrator stages them).

**Files:** Modify `docs/generated/bus-topology.md`,
`docs/generated/bus-topology.json` (+ siblings the generator touches).

## Self-Verification Checklist

- [ ] `go test -p 2 ./pkg/id/... ./internal/llm/... ./internal/agent/... -count=1` all PASS
- [ ] `go build ./...` clean; `gofmt -l` empty on touched dirs; `go vet` clean
- [ ] `make predid` clean (crypto/rand only, zero-suffix fallback documented)
- [ ] `make mutexio` clean
- [ ] Signatures match C1/C2/C4/C5 EXACTLY (names, types, json tags, omitempty)
- [ ] Empty-trace posture: no WithTraceID -> records carry "" and a spanID
      still mints (span identity is per-call, trace identity is per-turn)
- [ ] No raw argument VALUES anywhere in emitted event fields (keys only)
- [ ] TurnTerminalEvent contract doc + 20260916 tree master.md updated
- [ ] `make graphs` + `make graphs-check` PASS (Task 4)
- [ ] internal/memory/ untouched; no schema/migration edits (leaf 02 owns)
- [ ] No new unit test files (pins in existing *_test.go; pkg/id exception only)
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (For Review Agent)

- [ ] TraceID minted exactly once per turn, at the handler turn seam; every
      publishTurnTerminal site stamps it (or "" on pre-mint early failure)
- [ ] WithTraceID mirrors WithResolvedModel semantics: request-scoped,
      empty no-op, no client/loop shared state (grep for new struct fields
      on Client/Loop - there must be none for trace state)
- [ ] All 12 recordUsageStore call sites + codex/anthropic direct sites
      carry real latency (no remaining constant 0 where a start exists)
- [ ] LLMCallRecord gains exactly TraceID/SpanID; existing fields untouched
- [ ] ToolExecutionEndData: keys shape-only; bytes = raw length; parse
      failure degrades to "" keys without error
- [ ] Event JSON tags match C5 byte-for-byte (leaf 03's decode depends on it)
- [ ] Pins are table-driven where enumerating; no skipped tests; no debug
      artifacts; no line-number corruption in any touched file
- [ ] Only the declared files under internal/llm, internal/agent,
      internal/metrics (TurnSpanSpec), pkg/id, docs/{generated,plans} changed

Output: APPROVED or specific gaps with file + line references.

## Notes

- The loop's trace acquisition seam matters: read how turnID/conversationID
  reach the loop before choosing context vs option. Do NOT add a loop field.
- `flushDeferredToolResults` (loop.go:4205) is the cache-hit path; if it
  emits tool events, enrich identically (same trace, new span IDs).
- recordUsageStore already fires from its own goroutine - observer + record
  stamping stay inside it; do not add new synchronization.
- If handler.go early-return paths make full TraceID stamping ambiguous,
  prefer "" over minting extra IDs - one trace per MINTED turn, never two
  IDs for one turn.
