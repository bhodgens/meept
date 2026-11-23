# 07-bench/12-usteer-stall REPORT

## Defect

u-steer-midrun, run 7 (task-20261123064815.478538000-0008, transcript
usability-u-steer-midrun-a1.json): after the daemon's own revision step
(rev-1001, the file_write of result.txt=7 with real tool_evidence) was
APPROVED at 23:48:28.835, the task never finalized -- no
task.completed/task.failed, no further tactical activity for the task
ever, and the turn (turn-ee5d2461b26aff2e, conv-f85bea049afbd1a0) was
reaped by the turn watchdog at 23:58:44 (+10m, stale_for=10m0s).

## Timeline (daemon-run7.log, all real)

- 23:48:15.508 task 0008 dispatched; plan parsed steps=3; root step-0
  promoted to ready and scheduled (steps 1 and 2 stay PENDING with
  DependsOn=[step-0]).
- 23:48:23.839 step-0 REJECTED by review; revision rev-1001 created,
  promoted, scheduled ("Promoted steps after review" count=1,
  "Scheduling ready steps" count=1).
- 23:48:26.018 rev-1001 job DONE (file_write result.txt=7,
  evidence_count=2 -- the executor-evidence chain held).
- 23:48:28.835 "Review completed" rev-1001 status=approved.
- SILENCE. No log line for task 0008 between 23:48:28 and the 23:58:44
  reap except unrelated traffic. No "Task finalized", no replan trigger,
  no task.progress carrying the stall.

## Root cause

`StepStore.PromoteReadySteps` (internal/task/step.go) and
`StepStore.AreAllCompleted` (same file) disagreed about what resolves a
rejected step:

- AreAllCompleted: a StepRejected step is resolved once a
  successfully-terminal revision exists (rejectedSteps map, populated
  from successful steps' DependsOn edges).
- allStepsTerminalWithFailures (internal/agent/tactical.go): same rule
  via IsRevisionStep + last-DependsOn.
- PromoteReadySteps: the rejection exemption (M5, 2026-09-07) applied
  ONLY to the revision step itself and ONLY for its own original
  (`isRevision && depID == lastDep`).

So after rev-1001 was approved:

1. Steps 1 and 2 (plain, non-revision) depend on step-0 (rejected).
   IsSuccessfullyTerminal(rejected) = false for every non-revision
   dependent and M5 exempted only rev-1001 -- they were never promoted
   to ready. They stay StepPending forever.
2. The approved rev's OnJobCompleted ran the finalization block:
   AreAllCompleted = false (pending steps are not terminal) and
   allStepsTerminalWithFailures = false (same) -- no finalization. That
   is the correct outcome AT THAT MOMENT, but the only actor that could
   ever change the state (promotion) was permanently blocked.
3. The F10-style failBlockedDependents cascade never fires because
   nothing FAILED -- the recovery was approval-shaped. The ralph replan
   path is gated on ralphLoop.TaskOutcome (not terminal, not completed ->
   no trigger with evidence for a "succeeded" shape).
4. The ChatHandler's turn-touch sources (worker lifecycle events keyed
   on the session conversation, task.progress liveness) went quiet with
   the task, so the turn registry record aged out and the watchdog
   reaped the turn at +10m.

Task 0003 in the same run escaped the same shape only because its
revisions FAILED (a failed step triggers the failure-finalization
paths). The approved-revision shape is the stall; it was latent since
the M5 scoping landed and surfaced in run 7 because run 7 was the first
run where a revision actually got approved.

This is NOT causally linked to the sibling's ArmQueueAliases fix: the
steer RPC was rejected with "queue not found" in run 7 (their lane),
but the stall happens entirely in the task pipeline after the daemon's
own revision approval -- no steer delivery is involved in the stall
mechanism.

## Fix

internal/task/step.go, PromoteReadySteps only (surgical; no other
package touched):

- New `resolvedRejected` set computed from the task's steps: a
  successfully-terminal step that IsRevisionStep resolves the rejection
  of its own original (LAST DependsOn entry -- the same CreateRevision
  edge M5 keys on).
- The dep-gate loop now passes a rejected dependency when
  `resolvedRejected[depID]` (ANY dependent step) OR the pre-existing M5
  own-original rule holds (`isRevision && depID == lastDep`; still
  required because the revision itself is typically PENDING at promotion
  time and cannot yet appear in resolvedRejected).

Boundaries preserved:

- An unrelated rejected step WITHOUT a successful revision still blocks
  everything (pinned by the existing
  TestStepStore_PromoteReadySteps_RevisionBlockedByUnrelatedRejectedDep
  and the new boundary test).
- Failed deps still block everything (pinned by
  TestStepStore_PromoteReadySteps_RevisionStillBlockedByFailed).
- Non-revision steps with a merely-rejected (unresolved) dep stay
  blocked (pinned by TestStepStore_PromoteReadySteps_NonRevisionStillBlockedByRejected
  and the new boundary test).
- Promotion now agrees with AreAllCompleted / allStepsTerminalWithFailures:
  once the revision is approved, the pending siblings promote, run,
  terminalize, and the task finalizes through the existing OnJobCompleted
  flow (allDone -> StateCompleted + task.completed).

## Regression tests

internal/task/step_revision_promotion_test.go (new):

- TestStepStore_PromoteReadySteps_SiblingsPastRejectedOriginalWithApprovedRevision
  -- the exact run-7 stall shape: rejected original + APPROVED revision
  + two pending siblings depending on the original. Asserts both
  siblings promote and the already-approved revision is not re-promoted.
- TestStepStore_PromoteReadySteps_SiblingStillBlockedByRejectedWithoutRevision
  -- boundary: with only a REJECTED revision (no successful one), the
  sibling stays blocked.

## Verification (all real runs)

- gofmt -l: clean on both touched Go files.
- go build ./...: ok.
- go test ./internal/task/ -count=1: PASS (all PromoteReadySteps tests
  including the 2 new ones; full package).
- go test ./internal/agent/ -count=1: exactly ONE failure,
  TestTitleRefreshPublishesUserDescription ("SetSessionStore did not
  wire a store with the real Get signature"). A/B-proven PRE-EXISTING
  and unrelated: reverting my step.go to HEAD and rerunning fails
  identically -- the cause is the sibling's IN-FLIGHT uncommitted
  internal/agent/loop.go change (sessionStore interface now embeds
  SessionStoreReader), not my fix. Failure set did not grow vs the
  no-fix baseline.
- Coordination note: the sibling's WIP
  internal/agent/queue_alias_arm_gate_test.go transiently broke the
  package compile with an import cycle; it was parked to
  /tmp/usteer-stall-work/ during the verification runs and RESTORED
  afterward (byte-identical). I did not touch their lane.

## Files touched

- internal/task/step.go (PromoteReadySteps: resolvedRejected set + dep-gate rule; comments)
- internal/task/step_revision_promotion_test.go (2 new regression tests)
- docs/plans/20261119-usability-remediation/07-bench/12-usteer-stall-PROGRESS.md
- this report

Nothing committed (per constraint).
