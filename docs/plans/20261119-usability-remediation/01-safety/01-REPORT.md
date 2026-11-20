# 01-safety/01-reject-honesty -- Implementation Report (orchestrator-authored)

Status: IMPLEMENTED, VERIFIED GREEN. Child sa-0-d98a7253 wrote all code to disk
then died on upstream HTTP 429 before writing this report; orchestrator verified
the on-disk implementation and authored this report from the child's PROGRESS.md
+ live transcript + independent test runs.

## Files touched
- internal/rpc/task_approval.go: RejectFunc seam added; handleReject calls
  RejectFunc (NOT ApproveFunc); nil seam -> actionable error (no false success).
- internal/daemon/daemon.go (~2163): approvalHandler.RejectFunc =
  components.Orchestrator.Strategic().RejectPlan (mirrors ApproveFunc wiring).
- internal/agent/handler.go: handleTaskRejected (line 2402) relays task.rejected
  to turn.terminal {status=failed, handler_case=task_rejected_relay, originating
  turn id, rejection_reason verbatim}; publishTaskTerminalRelay gained 7th param
  rejectionReason; taskRejectedSub subscription + Unsubscribe on ctx.Done.
- internal/comm/http/task_approval.go (NEW) + server.go:1724-1725: POST
  /api/v1/tasks/{id}/approve + /reject twins; unknown id 404.
- schemas/ws_events.schema.json: rejection_reason (string, optional) added in
  SAME change (drift gate); set iff handler_case=task_rejected_relay.
- internal/agent/handler_terminal_event_test.go: two pre-existing call sites
  updated for the 7th param ("" for completed relays) -- signature-compilation
  fix, assertions unchanged (NOT weakening).

## Verification (orchestrator independent runs)
- go build ./... : OK
- go test ./internal/rpc/ -run TestReject -count=1 : ok (TestRejectDoesNotApprove,
  TestRejectWithoutRejectFuncErrors, TestRejectPropagatesSeamError)
- go test -overlay ./internal/agent/ -run TestRejectedRelay : ok
  (TestRejectedRelayTerminal PASS; overlay blocks sibling gate files
  ralph_storm/registry_remap/queue_lifecycle whose seams belong to leaves 02/07)
- go test ./internal/comm/http/ -run TestWSSchema : ok (drift + golden green)
- HTTP twins routes present (server.go:1724-1725); task_approval.go handler.

## Gate tests made GREEN
T1 (rpc reject), T2 (agent relay). T3 HTTP twins implemented (no dedicated gate
test file; routes + handler verified present, WSSchema green).

## Deviations
- publishTaskTerminalRelay signature extended (7th param) -- required two
  pre-existing test call-site updates (compilation only, no assertion change).
- Child died on 429 before report; orchestrator authored from PROGRESS.md.

## Remaining
None for T1/T2/T3. Commit pending orchestrator review + gate commit.