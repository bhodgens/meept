# 01-reject-honesty - Progress Checkpoints

Leaf: docs/plans/20261119-usability-remediation/01-safety/01-reject-honesty.md
Worktree: .hermes/worktrees/usability-remediation (branch plan/usability-remediation, base 89e996e2)
Gate tests on disk (untracked, authoritative): internal/rpc/task_approval_reject_test.go,
internal/agent/handler_rejected_relay_test.go. NOT modified.

## T1 - RejectFunc seam + handler fix (DONE, GREEN)

Files changed:
- internal/rpc/task_approval.go: added RejectFunc func(ctx, taskID, reason string) error
  field beside ApproveFunc; handleReject now calls RejectFunc (never ApproveFunc),
  nil seam -> "rejection service not available", seam error wrapped %w,
  response stays {task_id, status:"rejected"} (C2).
- internal/daemon/daemon.go (~2163): approvalHandler.RejectFunc =
  components.Orchestrator.Strategic().RejectPlan (constructor unchanged; field
  assignment mirrors the ApproveFunc wiring site).

Verify: go test ./internal/rpc/ -run TestReject -count=1
Result: ok github.com/caimlas/meept/internal/rpc 0.654s (GREEN;
TestRejectDoesNotApprove, TestRejectWithoutRejectFuncErrors, TestRejectPropagatesSeamError)
gofmt -l on both files: empty.

## T2 - task.rejected relay (DONE, GREEN via overlay)

Files changed:
- internal/agent/handler.go:
  - TurnTerminalEvent gains RejectionReason string `json:"rejection_reason,omitempty"` (C1 additive key).
  - Start subscribes task.rejected (taskRejectedSub), wg.Add(9)->10, new dispatch goroutine.
  - New handleTaskRejected: parses {task_id,reason}, recovers linked sessions from taskStore
    (nil-safe), relays via publishTaskTerminalRelay with handler_case=task_rejected_relay,
    status=failed, error=reason, rejection_reason=reason; originating turn id + completeTurn
    after emit come from the shared funnel (unchanged ordering).
  - publishTaskTerminalRelay gained 7th param rejectionReason; sets RejectionReason on the
    emitted event. Existing completed/failed callers pass "".
- internal/agent/handler_terminal_event_test.go: two PRE-EXISTING call sites (lines 766, 792)
  updated to pass "" as the 7th arg - signature-compilation fix only, assertions untouched
  (per orchestrator directive).
- schemas/ws_events.schema.json: rejection_reason declared in turn_terminal properties
  (string, optional) in the SAME change.

Verify: go test -overlay /tmp/leaf01-overlay/overlay.json ./internal/agent/ -run TestRejectedRelay -count=1 -v
Result: --- PASS: TestRejectedRelayTerminal (0.00s); ok (GREEN).
Overlay substitutes ONLY sibling gate files ralph_storm_test.go + registry_remap_test.go
(undefined MaxReplanExtensions / ResolveActiveQueue / ErrAmbiguousQueue owned by leaves 02/07)
with an empty package-agent file in /tmp; siblings untouched on disk.
Regression: -run 'TestRejectedRelay|TestPublishTaskTerminalRelay|TestHandleTaskFailed|TestHandleTaskCompleted' ok.
go test ./internal/comm/http/ -run TestWSSchema -count=1: ok (drift + golden fixture green).
go build ./... : OK.