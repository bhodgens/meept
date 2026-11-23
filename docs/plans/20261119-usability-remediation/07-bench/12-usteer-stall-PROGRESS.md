# 07-bench/12-usteer-stall PROGRESS

## Task
Root-cause + fix: u-steer task stalls after post-steer revision approval
(run 7, task-20261123064815.478538000-0008 / u-steer-midrun-a1). No
task.completed/task.failed, turn reaped at +10m by the watchdog.

## Evidence extracted (daemon-run7.log, transcript usability-u-steer-midrun-a1)
- 23:48:15.5 task 0008 dispatched, plan 3 steps (0/1/2), root step-0 promoted+scheduled.
- 23:48:23.839 step-0 REJECTED -> revision rev-1001 created, promoted, scheduled
  ("Promoted steps after review" count=1, "Scheduling ready steps" count=1).
- 23:48:26.018 rev job DONE (file_write result.txt=7, evidence_count=2).
- 23:48:28.835 "Review completed" rev-1001 status=approved.
- THEN: log silence for task 0008 until the 23:58:44 watchdog reap of the
  turn (conv-f85bea049afbd1a0, stale_for=10m0s). No Task finalized, no
  task.completed, no further tactical activity for this task ever.

## Root cause (pinned)
Steps 1 and 2 are PENDING with DependsOn=[step-0]. After step-0 was
rejected and its revision APPROVED, the rejection is "resolved"
(AreAllCompleted and allStepsTerminalWithFailures both implement the
rejected-original-resolved-by-successful-revision rule), BUT
StepStore.PromoteReadySteps only exempts the REVISION step itself from
its own rejected original (M5 scoping: `isRevision && depID == lastDep`).
A plain sibling step blocked by a rejected dep that now has a successful
revision can NEVER be promoted:
- IsSuccessfullyTerminal(rejected) = false -> allDepsCompleted=false -> stays PENDING.
- Pending steps are never terminal, so AreAllCompleted stays false and
  allStepsTerminalWithFailures stays false -> no finalization, ever.
- The F10-style failBlockedDependents cascade never fires because nothing
  ever FAILED (the recovery was approval-shaped).
- OnJobCompleted for the approved rev has allDone=false -> only publishes
  task.progress; there is no event left in the pipeline, so the turn
  registry record goes stale and the watchdog reaps at +10m.
Task 0003 in the same run escaped only because its revision FAILED (a
failed step triggers the failure-finalization paths); the approved-
revision shape is the stall.

## Fix plan
PromoteReadySteps: a rejected dependency is resolved for ANY dependent
step when a successful revision of that dep exists (mirror AreAllCompleted's
rule). Compute the resolved-rejected set from revision steps' own-original
edge (last DependsOn entry of a successful revision step), then apply it
for all pending steps, not only revisions. The revision's own-original
exception stays.

## Files
- internal/task/step.go (PromoteReadySteps) -- the fix
- internal/task/step_revision_promotion_test.go -- regression tests

## Fix implemented (2026-11-23)
PromoteReadySteps now computes `resolvedRejected`: every successfully-
terminal REVISION step resolves the rejection of its own original (LAST
DependsOn entry, the same CreateRevision edge M5 keys on). The dep-gate
loop passes a rejected dep when `resolvedRejected[depID]` (any dependent)
OR the pre-existing M5 own-original rule (`isRevision && depID ==
lastDep` -- still needed because the revision itself is pending at
promotion time and cannot appear in resolvedRejected). Unrelated rejected
steps without a successful revision still block; failed deps still block.

Regression tests (internal/task/step_revision_promotion_test.go):
- TestStepStore_PromoteReadySteps_SiblingsPastRejectedOriginalWithApprovedRevision
  (the run-7 stall shape: 2 pending siblings + approved revision)
- TestStepStore_PromoteReadySteps_SiblingStillBlockedByRejectedWithoutRevision
  (boundary: rejected revision resolves nothing)

## Verification status
- gofmt clean; go build ./... ok.
- go test ./internal/task/ -count=1: PASS (all 7 PromoteReadySteps tests
  incl. the 2 new).
- go test ./internal/agent/ -count=1: baseline recorded GREEN before the
  fix (98s). NOTE: sibling's WIP internal/agent/queue_alias_arm_gate_test.go
  (ArmQueueAliases lane) breaks the package compile with an import cycle
  while in flux; parked at /tmp/usteer-stall-work/ during verification,
  restored after. Post-fix full package run in progress.

## Report
See 12-usteer-stall-REPORT.md.
