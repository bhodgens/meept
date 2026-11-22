# Activity (execution-history) lane — PARENT-VERIFIED takeover

Provenance: child sa-0-3af89a8a's report file (01-activity-lane.md) contains ZERO
findings — it is a grep dump + the BRIEF-RULES paste, and its closing "key finding"
(the lane uses string-typed topics with no bus.Topic[T]) is FALSE:
internal/activity/execution_projector.go:44-50 subscribes via bus.SubscribeT with
agent.TopicBotResult / agent.TopicEmployeeResult (typed Topic[agent.ExecutionResultEvent]).
The child never read execution_projector.go. Parent audited the lane directly.
PROVENANCE = parent-verified, NOT independent.

## Parent findings at HEAD 7a5da96e

### A1 (LOW) internal/comm/http/execution_handlers.go:44,80 — all RPC errors -> HTTP 500
handleExecutionRuns maps every RPC error to 500. A browser sending ?since=garbage
gets 500 where 400 is correct (the RPC refuses it as a client fault:
execution_runs_rpc.go:47-51 "since: <parse err>"). Same shape as pre-existing
activity_handlers.go — a pattern bug, not new debt. Fix shape: classify parse/
validation errors to 400 or mirror the NoWorkingDir->400 precedent (54b4d271).

### A2 (LOW, documented posture) projector does INSERT+DELETE in the subscriber callback
execution_projector.go:42-51 calls store.Append (2 SQL statements) inside the
SubscribeT reader goroutine. Publishers never block (bus publish is non-blocking,
internal/bus/bus.go:52-57), so the cost of a slow disk is LAG, and lag past channel
buffer size = dropped rows counted at bus.go:57 (DroppedCount). A dropped run is a
history gap with no signal (the WARN only fires on bus side, never surfaced to the
agents.executionRuns consumer). Falsifiable test: fill the subscriber channel, publish,
assert either delivered or a surfaced gap indicator. Not product-breaking at 5k rows
retention; noted as accepted with an unmonitored failure mode.

## Parent-verified CLEAN list (all at file:line at HEAD)
- Typed topics used correctly (execution_projector.go:44,47); tier derived from topic
  name only — matches the #140 one-payload-no-kind contract.
- Zero-timestamp events fill server time, never dropped (projector comment + store
  Append :122-124).
- DSN uses modernc _pragma= keys — passes scripts/audit-sqlite-dsn-pragmas.py; WAL +
  busy_timeout(5000) + synchronous(NORMAL) + MaxOpenConns(1) (execution_store.go:91-94).
- Append: nil-store guard, UTC second-truncate stamping, parameterized INSERT,
  retention trim (id-based, MAX(id)-5000) with warn-not-fail.
- List: limit clamp (<=0->50, >1000->1000), parameterized where, ORDER BY id DESC,
  COUNT before limit for paging parity, rows.Err() checked, unparseable created_at
  warn + zero-time fallback.
- Since-bound compare safety (parent checked the trap the easy way): both sides of
  created_at >= ? are UTC "Z" RFC3339 (store normalizes q.Since.UTC() at List:159),
  so lexicographic order == chronological. Verified with /tmp/lexcheck2.go.
- Histogram: 8 cells over last hour, idx clamped 0..7, rows.Err() checked.
- Teardown: executionProjectorSubs unsubscribed in stopActivityRing
  (activity_wiring.go:141-151, called from components.go:7348 shutdown path).
- Wiring: store open failure degrades the surface with a WARN, boot continues
  (activity_wiring.go:101-110); registerExecutionRunsRPC nil-guards both args.
- HTTP bridge: rpcCall nil -> 503 clean; limit parsed with bounds 1..1000
  (parseIntParam); histogram requires agent_id at the edge too.
- ws_schema_drift_test.go landed in b37bc202 with the schema — drift gate armed.

## gates_run (parent):
- go build ./... — clean (full tree, this session).
- go test -p 2 -count=1 ./... — 158 ok; 2 flakes (internal/signal
  TestSupervisorRestartOnCrash, internal/worker TestWorker_RetryWakesIdleWorker),
  both 3/3 green on isolated re-run — timing fragility, not regression.
- go test -tags e2e -count=1 ./e2e/suites/manifest/... — FAIL confirmed (prior H6
  STILL-OPEN at 7a5da96e: 3 missing path_map keys + 2 narrow-key rule violations).
