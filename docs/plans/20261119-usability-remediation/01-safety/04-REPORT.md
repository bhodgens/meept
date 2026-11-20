# 01-safety/04-watchdog-exemption -- Implementation Report (orchestrator-authored)

Status: IMPLEMENTED, VERIFIED GREEN. Child sa-1-38130b72 wrote all code to disk,
stalled during verification (sibling build churn), was steered then stopped;
orchestrator verified the on-disk implementation GREEN and authored this report.

## Files touched
- internal/agent/turn_watchdog.go: SetTaskStateFunc seam + exemption predicate;
  turns in planning / awaiting_approval / awaiting_seal are NOT reaped as stale.
- internal/agent/plan_draft.go: StrategicPlanner.turnLivenessTouch field +
  SetTurnLivenessTouch setter + nil-safe touchTurnLiveness helper; Touch sites
  at SaveDraft (after persist) + SealDraft (after persist).
- internal/agent/strategic.go: Touch at awaitUserApproval (gate entry) +
  ApprovePlan (after schedule).

## Verification (orchestrator independent runs)
- go build ./... : OK
- go test -overlay ./internal/agent/ -run 'TestApprovalWaitNotReaped|TestSealActivityTouch'
  -count=1 -v : --- PASS TestApprovalWaitNotReaped; --- PASS TestSealActivityTouch; ok
  (overlay blocks sibling gate files ralph_storm/registry_remap/queue_lifecycle
  whose seams belong to undispatched leaves 02/07; siblings untouched on disk)

## Gate tests made GREEN
T1 (TestApprovalWaitNotReaped), T2 (TestSealActivityTouch).

## Deviations
- Child stalled 30+ min during verify (sibling review_evidence_gate.go /
  handler.go churn broke the agent package test build transiently); orchestrator
  verified via overlay once siblings stabilized. No code deviation.

## Remaining
None for T1/T2. Commit pending orchestrator review + gate commit.