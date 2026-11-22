# 02-routing/02-dispatch-evidence -- Progress

Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
Branch: plan/usability-remediation (NO commits; orchestrator commits after review)

## Checkpoint 0 -- leaf + gate tests read (2026-11-20, prior child)

- Read docs/plans/20261119-usability-remediation/02-routing/02-dispatch-evidence.md
- Read internal/metrics/dispatch_log_test.go (gate T3, committed 89e996e2):
  - dispatch_log must gain turn_id column (idempotent ALTER; legacy COALESCE '')
  - input_summary must roundtrip NON-EMPTY (currently scrubbed empty)
  - QueryDispatchLogBySession SELECT sites must include new column
- Read internal/rpc/dispatch_trace_test.go (gate T3):
  - Server needs SetDispatchSessionStore(chatSubmitSessionStore) seam
  - session.dispatch_trace dual-key: primary session id -> owning conversation rows
  - thread id "<base>-thread-<topic>-<suffix>" unwraps to owning conversation
  - conversation-id query keeps working; unknown id -> empty list, no error
  - wire rows must carry non-empty turn_id + input_summary
  - NOTE: fixture records DispatchEntry WITHOUT TurnID but asserts non-empty
    wire turn_id -> store/handler must provide a deterministic fallback join
    key for rows lacking an explicit turn id (task_id / session+turn_no).

## Checkpoint 1 -- resumed (2026-11-20, new child)

- Re-read leaf + both gate test files verbatim. Plan:
  T1: metrics/store.go -- DispatchEntry.TurnID field; CREATE TABLE + idempotent
      ALTER for turn_id; RecordDispatch INSERT turn_id; stop scrubbing
      input_summary empty (bounded 200, sanitized); QueryDispatchLogBySession
      SELECT turn_id COALESCE ''. dispatcher.go RecordDispatch call site wires
      req.TurnID + input text.
  T2: rpc/dispatch_trace.go -- SetDispatchSessionStore(chatSubmitSessionStore)
      seam on Server; dual-key resolution (primary id -> conversation; thread
      unwrap; conv id passthrough; unknown -> empty); wire turn_id fallback
      (task_id or session+turn_no) when row turn_id empty.
  T3: HTTP route GET /api/v1/sessions/{id}/dispatch-trace (leaf task T3).
- Next: inspect store.go, dispatch_trace.go, chat_submit.go seam, dispatcher.go.

## Checkpoint 2 -- resumed (2026-11-20, rpc-half child)

- Baseline confirmed: metrics half GREEN (go test ./internal/metrics/ -run
  TestTraceTurnID ok; turn_id column + input_summary roundtrip landed).
- rpc gates: TestDualKeyTrace_ConversationIDKeepsWorking PASS,
  _UnknownIDEmptyNotError PASS, _PrimarySessionID FAIL, _ThreadIDResolves
  FAIL, TestTraceTurnID_RowsCarryTurnIDAndInput FAIL (turn_id empty only;
  input_summary already non-empty -- store-side fixed it).
- Findings:
  * chatSubmitSessionStore (chat_submit.go:57) = Get + GetByConversationID;
    session.Store satisfies it; test seam asserts exactly
    SetDispatchSessionStore(store chatSubmitSessionStore) on *Server.
  * session.ResolveThreadConversationID (thread_resolve.go:95) is the
    fail-safe dual+thread resolver to reuse (conv -> primary -> last
    "-thread-" unwrap).
  * QueryDispatchLogBySession is single-key WHERE session_id = ?; handler
    will fan the resolved key set over it and merge.
  * DispatchEntry.TurnID exists; wire fallback when row turn_id == "":
    task_id, else session_id+":"+turn_no (deterministic).
  * Gate files dispatch_trace_test.go + dispatch_log_test.go untouched.
  * NOT in this half (flagged): dispatcher.go RecordDispatch call site
    still passes InputSummary "" and no TurnID (metrics-half remainder);
    leaf T3 HTTP route (no gate test exists for it).
- Implementing: Server.dispatchSessionStore field + setter/lookup in
  dispatch_trace.go; dual-key resolution in handler; turn_id fallback;
  daemon.go wiring at RegisterDispatchHandlers site.
## Checkpoint 3 -- rpc half GREEN (2026-11-20, rpc-half child)

- Implemented (dispatch_trace.go rewrite + seams):
  * Server.dispatchSessionStore field (server.go) guarded by
    dispatchTraceMu; SetDispatchSessionStore(chatSubmitSessionStore)
    setter -- exactly the interface the gate test asserts.
  * dispatchTraceKeys: session.ResolveThreadConversationID (conv ->
    primary -> last "-thread-" unwrap, fail-safe nil) -> key set
    {conversation, primary, thread-id-if-distinct}; unwired/unresolvable
    id -> raw id only (legacy, never cross-returns).
  * queryDispatchTrace: fan keys over QueryDispatchLogBySession, merge,
    dedup on (session_id, task_id, turn_no), cap at limit.
  * dispatchTraceRow: wire = DispatchEntry JSON + turn_id override with
    deterministic fallback (task_id, else session_id+":"+turn_no).
  * store.go QueryDispatchLogBySession SELECT now carries
    COALESCE(turn_id, '') AS turn_id.
  * daemon.go: rpcServer.SetDispatchSessionStore(nilSafeSessionStore(components))
    at the RegisterDispatchHandlers site.
- Verify: go test ./internal/rpc/ -run 'Trace|DispatchLog' ok;
  go test ./internal/metrics/ ok; go build ./... ok; gofmt clean;
  gate test diff EMPTY.
- Pre-existing sibling-leaf failures untouched: TestQueueStatusParked*,
  TestTaskApprove_RPC (queue/task leaves), TestRoutePopover (05-nav TUI
  read surface).
- STILL OPEN for this leaf (NOT this child's scope): dispatcher.go
  RecordDispatch call site passes InputSummary "" + no TurnID (write-path
  C5; metrics gate TestTraceTurnID_DispatcherRecordCarriesTurnID is
  store-boundary and GREEN); leaf T3 HTTP route (no gate test exists).
