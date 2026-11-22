# 07-bench / 05 pre-fail fix -- REPORT

Residual-fix wave for plan 20261119-usability-remediation: the pre-existing
internal/agent test failures. No commit (per constraints). Never git stash.

## Failures assigned vs found

Assigned: 2 (TestTacticalScheduler_VacuousValidatorPassNotValidated,
TestGuardRetry_RetryParksSkipsPostTurnPipeline). During full-package
verification a THIRD failure of the same class surfaced and was also
resolved: TestHeuristicReview_ApprovalWithoutEvidenceNotValidated (proven
pre-existing at clean worktree HEAD 6c4e17fc in the scratch worktree
/tmp/vacbase with zero local changes -- it fails there identically; it was
invisible to earlier baselines because leaf 06 landed at 6c4e17fc, after the
A/B baselines earlier leaves took).

## Failure 1: VacuousValidatorPassNotValidated (nil panic)

### Root cause (proven, not guessed)
A/B in /tmp/vacbase: 6c4e17fc~1 PASSES, 6c4e17fc PANICS. Leaf 06
(01-safety/06-evidence-review, commit 6c4e17fc) added fileEvidenceGate. The
pin's file_write step claims "produced the artifact" with zero tool-issued
evidence; the gate (no registry wired -> reviewerAvailable=false,
RevisionCount 0 < retry budget) returns ReviewRejected. HandleReviewResult
creates a revision step; handleReviewResult promotes + calls
ScheduleReadySteps; scheduleStep dereferences ts.queue (nil in the
validation-gate harness) at Enqueue -> SIGSEGV. The Queue-less harness is
legitimate: these tests drive OnJobCompleted semantics, and the scheduler
already had an established nil-queue posture (sweepReadyStepsForRetry,
tactical.go:616).

### Product fix (internal/agent/tactical.go, ScheduleReadySteps entry)
Nil-queue guard (Debug log + no-op return), matching the existing posture.
This is the real defect fix: any review rejection under a Queue-less
scheduler crashed the process instead of degrading.

### Residual assertion (documented contract supersession)
After the panic fix the pin failed on state "rejected" vs want "approved".
NOT a product defect: leaf 06 deliberately redefined this exact verdict
(T1: a file-shaped step with zero file-tool ToolEvidence can never
auto-approve -- the p6 fabricated-success control). Keeping the approved
expectation would re-open the fabricated-success hole leaf 06 closed. The
pin was re-based to the superseding contract with its F10/F11 spirit
preserved: rejected + actionable rejection copy in the persisted Result +
Validated=false (nothing was verified) + revision promoted to StepReady +
OnJobCompleted nil (no panic). The approved-expectation removal is a
supersession, not a weakening: the new assertions are strictly stronger on
honesty (a fabricated file-effect claim must be rejected).

## Failure 2: GuardRetry_RetryParksSkipsPostTurnPipeline (park lane)

### Root cause
The M3 pin (2026-09-29) froze the pre-T8 sentinel: RunOnce on a parked
guard-retry returns ("", nil). 01-safety/05 (Gate T8) moved the parked shape
to ("", ErrTurnParked) at the RunOnceWithParts boundary and lane-split the
RunOnce translation (autonomous / injected-clock keep ("", nil); chat lanes
get throttleParkNotice). 05's regression sweep regex
(TestPark|TestThrottle|TestStrategic|TestPlan_|TestPair|TestAgentLoop_Throttle|...)
never matched this test name, so the pin survived un-rebased.

### Product-defect check (performed; none found)
- The M3 branch still returns ("", ErrTurnParked) and skips the post-turn
  pipeline (no blank assistant append) -- M3's defect class stays fixed.
- This loop is a chat lane (no autonomous marker, no injected clock), so
  throttleParkNotice is the deliberate T8 surface. No principled product
  lane can distinguish a guard-retry park from a first-cycle park at the
  RunOnce boundary: the reply-guard retry is an internal loop detail
  invisible to RunOnce callers, and splitting the surface on it would be
  incoherent. The notice also cannot lie here: ErrTurnParked implies a park
  happened, which implies a wired parker (without one parkThrottledTurn
  passes the throttle error through and ErrTurnParked never surfaces).

### Harness fix (documented in-file)
The pin now drives RunOnceWithParts -- the seam M3 actually guards -- and
asserts the explicit T8 parked shape: ("", ErrTurnParked) + StateQuotaWait +
no blank assistant append. RunOnce's chat-lane notice translation stays
pinned by the T8 park tests (untouched).

## Third failure (found, same class): HeuristicReview_ApprovalWithoutEvidenceNotValidated

Same leaf-06 supersession: the pin used a file_write step as its approval
vehicle; the gate now rejects that shape. HARNESS RE-POINT (not a weakening):
F11 is about the validation stamp on a heuristic approval, so the vehicle
moved to hint "coder" + non-claim result ("summarized the findings for the
user") -- not file-shaped, not conversational-skip-listed, so the flow still
reaches the trivial-task heuristic that owns the Validated stamping being
pinned. Persisted approved-state + Validated=false assertions unchanged.
The file-shape rejection verdict stays pinned by the leaf-06 gate tests
(TestFileStepZeroEvidenceGate etc.) and by the re-based vacuous pin.

## Files touched
- internal/agent/tactical.go -- PRODUCT fix: nil-queue guard in
  ScheduleReadySteps (entry, before GetReadySteps).
- internal/agent/tactical_validation_gate_test.go -- 2 pins re-based to the
  leaf-06 superseding contract, each with an in-file CONTRACT UPDATE /
  HARNESS RE-POINT comment explaining why.
- internal/agent/reply_guard_retry_fallback_test.go -- M3 pin re-based to
  the T8 explicit parked shape, with an in-file CONTRACT UPDATE comment.
- docs/plans/20261119-usability-remediation/07-bench/
  05-prefail-fix-PROGRESS.md + this report.

Sibling-owned files (dispatcher.go, handler.go, queue_resolve.go,
registry.go, internal/rpc/queue.go, internal/services/chat_service.go, bench
runners) were NOT touched.

## Verification (real runs, this tree)
- go test ./internal/agent/ -run 'TestGuardRetry|TestTacticalScheduler'
  -count=1 -> ok
- go test ./internal/agent/ -run 'TestGuardRetry|TestTacticalScheduler|
  TestHeuristicReview|TestFileStep|TestErroredFileToolGate|
  TestDeniedWriteGate|TestZeroEvidence' -count=1 -> ok
- go test ./internal/agent/ -count=1 -> ok (101.5s) -- ZERO failures in the
  full package (the two assigned failures plus the third same-class failure
  are gone; no new failures).
- go test ./internal/daemon/ -run 'TestAgentJobProcessor_ParkedTurn|
  TestParkedTurn' -count=1 -> ok (the failure-2 lane's daemon consumers).
- go build ./... -> exit 0.
- gofmt -l internal/agent/ -> empty. All new lines ASCII; errors wrapped %w
  where applicable (the guard returns/logs, no new error paths).

## Issues encountered
- Sibling edits: internal/agent had live sibling modifications
  (review_manager.go user-language copy, plan_draft.go, registry.go,
  max_revision_guard_test.go); no transient compile break was hit during my
  runs, so no wait/retry was needed. My files and theirs are disjoint.
- scratch worktree /tmp/vacbase was left on 6c4e17fc with the temporary
  probe patch reverted (clean checkout state); it can be deleted.
