# Leaf 04 - Hermetic E2E: llm-hop-tracing Suite + Manifest (Live OTLP Receiver Verification)

## DISPATCH INSTRUCTION

**You are implementing this leaf as a dispatched implementation agent.**
Read this document fully, then implement every task with TDD (the suite IS
the test - write it first against the contracts, watch it fail if run
before leaves 01-03 land, then confirm GREEN after they land).

- Parent: `master.md` (contracts C1-C9 apply to you)
- Verify command: `make e2e-fast-area AREA=llm-hop-tracing`
- **Do NOT commit. Do NOT run git add.** Write code, run tests, report
  results only. The orchestrator handles all git operations.
- **Line-number corruption rule:** inspect existing files with
  `search_files` or `terminal cat` - NEVER pipe `read_file` output into
  `write_file`. After writing a file, do NOT read it back - write once and
  stop. Patch with full verbatim context strings.

## Meta

- **Parent:** master.md
- **Dependencies:** leaves 01, 02, 03 COMPLETE
- **Estimated Context:** ~90K
- **Concurrency Group:** D

## Goal

One new hermetic e2e suite `e2e/suites/llm-hop-tracing/` + manifest entries:
boot the daemon with `metrics.otlp_traces.enabled=true` against an in-suite
local OTLP/HTTP capture receiver, drive one tool-call turn and one plain
turn through the fake LLM, and assert the FULL tracing story end-to-end:
single shared trace ID across chat.turn + llm.call + tool.call spans,
GenAI attributes (tokens, latency, model, provider, argument shape),
SQL join `llm_calls.trace_id`/`span_id` == captured span IDs, zero raw
argument values in attributes, and the disabled-posture control scenario
(enabled=false -> zero HTTP requests at the receiver + disabled Warn).
Plus the harness OTLP capture receiver helper.

## Dependencies

- Leaves 01, 02, 03 COMPLETE (threading, columns, spans, wiring all exist).
  This leaf adds NO production code under internal/ - only e2e/ + manifest.

## Estimated Context

~90K (harness receiver ~150 lines, suite ~300 lines, manifest edits,
debugging against real daemon boot).

## Context

Read first:
- `e2e/harness/` - daemon.go (scratch daemon manager, scratch MEEPT_HOME),
  fakellm.go (OpenAI-compatible fake incl. tool_calls + usage; script one
  tool-call response then a plain completion), clients.go (chat submit +
  terminal await), tasksdb.go (sqlite reader pattern - copy its read
  approach for metrics.db).
- An existing small suite for shape: `e2e/suites/breakers/breakers_test.go`
  and `e2e/suites/chat-submit/` (build tag `e2e`, harness usage, t.TempDir
  worlds). Manifest: `e2e/manifest.json` (suites[] + path_map + scenarios;
  comment documents coverage policy - internal/metrics, internal/llm,
  internal/daemon, internal/session mappings gain this suite).
- `docs/workflows/e2e-testing.md` - tiers + manifest format (read it).
- Config: the daemon config file in the scratch home needs
  `[metrics.otlp_traces] enabled = true, endpoint = <receiver URL>`.
  Check how existing suites write daemon config (harness daemon.go).
- Receiver prior art (unit-tier, e2e cannot import): internal/metrics/
  otlp_trace_test.go newCaptureTraceServer - re-implement a minimal capture
  receiver in the harness (httptest.Server at /v1/traces; content-type
  application/x-protobuf; collect raw bodies; lenient protobuf walk for
  span name/trace-id/attributes - copy the field-number walk approach,
  simplified: you need name (field 2 in ScopeSpans' spans? follow the
  existing parseProtobuf), trace_id (field 1, bytes), and one attribute
  map).
- e2e rule from AGENTS.md: harness builds binaries into $TMPDIR; the
  watchdog reaper handles scratch daemons; do not spawn raw exec.Command.

## Verified Anchors

- e2e/harness/fakellm.go - ChatCompletionResponse/Choice/RawCall shapes;
  scriptable responses + usage map
- e2e/harness/daemon.go - scratch daemon lifecycle (boot, config, ports)
- e2e/suites/chat-submit/chat_submit_test.go - suite skeleton to mirror
- e2e/suites/breakers/breakers_test.go - minimal suite shape
- e2e/manifest.json - suites[] entries {name, dir, status}; path_map
  prefixes -> suite names; scenarios[] {id, suite, diff, title, paths}
- internal/config/otlp_traces.go:32 - OTLPTracesConfig (enabled, endpoint,
  headers, sample_rate); endpoint normalized at load boundary
- internal/metrics/otlp_trace_test.go:40-200 - parseProtobuf walk (decode
  approach to port into the harness)
- internal/daemon/otlp_traces_wiring.go:52/98 - the disabled Warn / enabled
  Info lines the control scenarios assert

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `e2e/harness/otlpreceiver.go` (new): `OTLPCaptureReceiver` - httptest
  server capturing export requests; accessors: `URL() string`,
  `Requests() int`, `Spans() []CapturedSpan` where CapturedSpan carries
  Name string, TraceIDHex string, SpanIDHex string, Attrs map[string]string.
  Deterministic: drain + decode per request; no background timers.
- `e2e/suites/llm-hop-tracing/llm_hop_tracing_test.go` (new): scenarios
  T1-T4 below.
- Manifest: suites[] += {name: "llm-hop-tracing", dir:
  "e2e/suites/llm-hop-tracing", status: "implemented"}; path_map entries
  for internal/llm/, internal/agent/, internal/metrics/, internal/session/,
  internal/daemon/ gain "llm-hop-tracing"; scenarios[] += 4 rows (diff "M").

### What This Leaf Consumes

- C1-C8 behaviors as implemented by leaves 01-03 (spans on the wire,
  columns in metrics.db, armed/disabled log lines).
- C9 test commands.

## Tasks

### Task 1: Harness OTLP capture receiver

**Files:** Create `e2e/harness/otlpreceiver.go`; pins in
`e2e/harness/harness_selftest_test.go` (extend it).

1. Failing pins: receiver captures a hand-built protobuf body (construct a
   minimal ExportTraceServiceRequest body in the pin, mirroring the unit
   test's encoder direction) and exposes Name/TraceIDHex/SpanIDHex/Attrs;
   Requests() counts POSTs; Close() is clean.
2. Implement `OTLPCaptureReceiver` per the contract above; port the lenient
   protobuf walk from internal/metrics/otlp_trace_test.go parseProtobuf
   (copy the approach, keep it ASCII-simple; trace_id is 16 bytes ->
   hex, span_id 8 bytes -> hex).
3. Verify: `go test -tags e2e -count=1 ./e2e/harness/... -p 2` -> PASS.

### Task 2: Suite scenarios T1-T4

**Files:** Create `e2e/suites/llm-hop-tracing/llm_hop_tracing_test.go`.

T1 - armed boot + single shared trace (tool turn):
  Boot daemon with otlp_traces enabled -> receiver; script the fake LLM:
  first response = one tool call (arguments `{"path":"docs/notes.md","limit":5}`),
  second = plain completion; submit a chat turn; await terminal. Assert:
  captured spans include exactly one chat.turn, >= 1 llm.call, >= 1
  tool.call; ALL share ONE TraceIDHex == the 32-hex trace; llm.call span
  attrs carry gen_ai.provider.name / gen_ai.request.model /
  gen_ai.usage.input_tokens / gen_ai.usage.output_tokens (>= 1);
  tool.call span attrs carry gen_ai.tool.name, meept.tool.args.keys ==
  "limit,path" (sorted, key names only), meept.tool.args.bytes > 0, and NO
  attribute value contains "docs/notes.md" or "5" as a raw argument value
  (allowlist check: keys exactly from C8 table);
  tool.call span_id != llm.call span_id.

T2 - SQL join by trace ID:
  Same T1 daemon run (or a fresh one): open the daemon's metrics.db from
  the scratch MEEPT_HOME (harness sqlite read pattern); assert the latest
  llm_calls row has trace_id == captured llm.call TraceIDHex and span_id ==
  its SpanIDHex (byte-exact); latency_ms > 0. (Leaf 02's columns; if the
  suite runs before leaf 02 lands this fails - that is the RED state.)

T3 - quiet boot (disabled posture):
  Boot daemon with otlp_traces absent (default false) pointing at a
  receiver; run one plain chat turn; await terminal; assert receiver
  Requests() == 0, no spans, and the daemon log contains the disabled Warn
  line "OTLP trace export disabled" exactly once and no "OTLP trace export
  enabled".

T4 - armed boot log:
  In T1's daemon log assert exactly one "OTLP trace export enabled" Info
  line at boot (the armed-line assertion; event-driven exporter means the
  boot must NOT emit span-related lines later - assert log stays quiet
  apart from turn activity).

Each scenario mirrors existing suites' structure (harness setup, t.Cleanup,
await-terminal helpers). diff: "M".

### Task 3: Manifest registration

**Files:** Modify `e2e/manifest.json` (suites[], path_map, scenarios[] per
the contract above).

Verify: `python3 -c "import json;json.load(open('e2e/manifest.json'))"`
parses; `scripts/e2e-affected.sh` dry behavior unchanged for unrelated
paths (no need to run; manifest JSON validity + shape parity with existing
entries is the gate).

### Task 4: Full-suite verification

1. `make e2e-fast-area AREA=llm-hop-tracing` -> PASS.
2. `make e2e-fast` -> PASS (no regressions across all 69 suites).
3. `gofmt -l e2e` -> empty; `go vet -tags e2e ./e2e/...` -> clean.

## Self-Verification Checklist

- [ ] `make e2e-fast-area AREA=llm-hop-tracing` passes with -count=1
- [ ] `make e2e-fast` passes (full hermetic tier green)
- [ ] T1 asserts single shared trace ID across all three span types
- [ ] T1 negative assertion: no raw argument values in any captured attr
- [ ] T2 asserts byte-exact trace_id AND span_id join to llm_calls
- [ ] T3 asserts zero receiver requests + exactly one disabled Warn
- [ ] T4 asserts exactly one enabled Info at armed boot
- [ ] Manifest entries well-formed; suite registered in suites[] +
      path_map + scenarios[]
- [ ] No production code under internal/ touched
- [ ] Do NOT commit. Do NOT run git add.

## Review Checklist (For Review Agent)

- [ ] Receiver helper lives in e2e/harness (not the suite), with selftest pins
- [ ] Suite follows harness conventions (no raw exec.Command; scratch
      worlds; cleanup handlers)
- [ ] Assertions are exact, not lenient (count ==, byte-exact hex joins)
- [ ] No flaky sleeps: terminal-await + deterministic Flush/drain (exporter
      flush is 5s-batched - the suite must force or await flush without
      busy-wait; if needed stop the daemon gracefully to flush, or poll
      receiver with deadline)
- [ ] Manifest diff minimal and shape-consistent
- [ ] No debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Exporter batches on a 5s interval: prefer deterministic flush (graceful
  daemon Stop flushes the remainder per TraceExporter.Stop) over wall-clock
  sleeps; a bounded poll (deadline ~15s) on the receiver is the fallback.
- The fake LLM must return a usage block so llm.call token attributes are
  non-zero (fakellm Usage map).
- If the tool-execution end event does not reach the bus in the T1 flow,
  check leaf 01's emitter path first - do not weaken the assertion to
  "llm.call only"; tool.call presence is a core acceptance criterion.
