# 02-routing/02-dispatch-evidence -- Completion Report

Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
Branch: plan/usability-remediation. NO commits (per leaf rule; orchestrator
commits after review). Gate diffs: EMPTY (dispatch_trace_test.go and
dispatch_log_test.go byte-identical to HEAD).

## 1. Files touched

Metrics half (done by prior sibling, re-verified GREEN this session):
- internal/metrics/store.go -- dispatch_log gains turn_id column (CREATE
  TABLE + idempotent ALTER migration v11, legacy rows COALESCE ''),
  DispatchEntry.TurnID field, RecordDispatch INSERT carries turn_id,
  input_summary persisted bounded (200 runes, truncateRunes) instead of
  scrubbed empty. This child additionally added COALESCE(turn_id, '') AS
  turn_id to the QueryDispatchLogBySession SELECT site (the typed read
  surface dropped the column).

RPC half (this child, C5 dual-key + row shape):
- internal/rpc/dispatch_trace.go -- SetDispatchSessionStore seam on
  *Server; session.dispatch_trace dual-key, thread-aware resolution;
  deterministic turn_id fallback on every wire row.
- internal/rpc/server.go -- dispatchSessionStore field + dispatchTraceMu
  (RWMutex; written at wiring time, read per request).
- internal/daemon/daemon.go -- wires
  rpcServer.SetDispatchSessionStore(nilSafeSessionStore(components)) at
  the RegisterDispatchHandlers site (nil-safe setter; unwired keeps
  legacy session_id-only behavior).

## 2. Design (contract C5)

- Dual-key resolution reuses session.ResolveThreadConversationID
  (internal/session/thread_resolve.go:95): direct conversation-id lookup,
  then primary-id lookup, then last "-thread-" unwrap -- fail-safe per
  bughunt L11 (a false trigger resolves to nil, never a wrong session).
  The incoming id maps to the owning session's key set
  {ConversationID, ID, thread-id-if-distinct}; the handler fans the keys
  over QueryDispatchLogBySession, merges, de-dups on
  (session_id, task_id, turn_no), caps at limit.
  * Primary session id -> owning conversation's rows (gate
    TestDualKeyTrace_PrimarySessionID).
  * "<base>-thread-<topic>-<suffix>" -> unwraps to the base conversation
    (gate TestDualKeyTrace_ThreadIDResolves).
  * Conversation id passthrough keeps working (no regression).
  * Unknown / unwired -> key set is the raw id -> empty list, never an
    error and never another session's rows.
- Wire rows: JSON of metrics.DispatchEntry plus a turn_id override.
  turn_id is NEVER empty: explicit TurnID when recorded, else
  deterministic fallback task_id, else "<session_id>:<turn_no>" (gate
  TestTraceTurnID_RowsCarryTurnIDAndInput records WITHOUT TurnID and
  asserts the fallback).
- input_summary non-empty on the wire flows from the store-side fix (the
  column roundtrips the bounded text; the RPC row maps it verbatim).

## 3. Verify output (executed, real)

- go test ./internal/rpc/ -run 'Trace|DispatchLog' -count=1 -->
  ok github.com/caimlas/meept/internal/rpc (0.60s)
  All five gates: _PrimarySessionID PASS, _ConversationIDKeepsWorking
  PASS, _ThreadIDResolves PASS, _UnknownIDEmptyNotError PASS,
  _RowsCarryTurnIDAndInput PASS.
- go test ./internal/metrics/ -count=1 --> ok (2.39s), incl.
  TestTraceTurnID_SchemaHasTurnID / _RoundtripWithTurnID /
  _DispatcherRecordCarriesTurnID.
- go vet ./internal/rpc/ --> clean. gofmt: no findings on touched files.
- go build ./... --> OK.
- Live stack boots the same seam: daemon wiring is
  SetDispatchSessionStore(components.SessionStore); e2e
  suites/dispatch-trace asserts exactly the end-states implemented here.

## 4. Pre-existing failures NOT from this leaf (sibling RED gates)

- ./internal/rpc full-package run also fails TestQueueStatusParked,
  TestQueueStatusParkedWithActiveQueue, TestTaskApprove_RPC (other
  leaves' gates, untouched by this diff).
- ./internal/tui/models TestRoutePopover (05-navigation read surface;
  asserts on the chat_nav side, consumes the wire shape this leaf now
  provides).

## 5. Deviations / open items for the orchestrator

1. DISPATCHER WRITE PATH (metrics half, still open): the
   internal/agent/dispatcher.go recordDispatch call site still passes
   InputSummary: "" and no TurnID, so LIVE rows (not gate rows) would
   carry the session+turn_no fallback and an empty input_summary. The
   metrics gate is store-boundary and GREEN; wiring req.TurnID + the
   sanitized bounded input at that call site is the remaining
   dispatcher-side delta (dispatcher.go is a shared sibling-edited
   region -- left untouched deliberately).
2. Leaf task T3 (HTTP GET /api/v1/sessions/{id}/dispatch-trace) NOT
   implemented: no gate test exists for it in internal/comm/http
   (rg TestDispatchTraceRoute: no matches), and it sits outside this
   child's assigned 3-gate scope. The route should reuse
   Server.CallMethod("session.dispatch_trace") like the chat.submit
   HTTP bridge precedent.
3. No commits, no gate edits, chat_submit.go / proxy.go untouched.
