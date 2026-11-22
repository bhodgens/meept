# 07-bench / 05 pre-fail fix -- PROGRESS

Residual-fix wave: the pre-existing internal/agent failures (proven at clean
HEAD by earlier A/B leaves).

## Failure 1: TestTacticalScheduler_VacuousValidatorPassNotValidated (nil panic)

### Root cause (empirically proven via scratch worktree /tmp/vacbase)
- 6c4e17fc~1 (BEFORE leaf 06 evidence-review): test PASSES.
- 6c4e17fc (leaf 06 fileEvidenceGate landed): test PANICS.
- Panic path: OnJobCompleted -> ReviewStep -> fileEvidenceGate (file_write
  step, ZERO file-tool evidence, no registry wired) -> ReviewRejected ->
  HandleReviewResult creates a revision -> handleReviewResult ->
  PromoteReadySteps + ScheduleReadySteps -> scheduleStep ->
  ts.queue.Enqueue on NIL queue -> SIGSEGV (tactical.go:955).
- The harness (newGateTestScheduler) is legitimately Queue-less; the
  established nil-queue posture already exists at sweepReadyStepsForRetry
  (tactical.go:616).

### Fix applied (PRODUCT, internal/agent/tactical.go ScheduleReadySteps)
- Nil-queue entry guard (no-op + Debug log), matching the
  sweepReadyStepsForRetry posture. This fixes the REAL defect: a review
  rejection in any Queue-less scheduler crashed with SIGSEGV.

### Residual after the panic fix
- The pin then failed on its ASSERTION: state "rejected" vs want "approved".
  Contract supersession, NOT a product defect: leaf 06
  (01-safety/06-evidence-review, commit 6c4e17fc) DELIBERATELY redefined the
  verdict for exactly this shape (file_write + "produced the artifact" +
  zero tool evidence = the p6 fabricated-success control). Keeping the
  approved expectation would re-open the hole leaf 06 closed.
- Pin updated to the superseding contract, F10/F11 spirit preserved:
  rejected + actionable copy in Result + Validated=false + revision promoted
  to ready + no panic on the Queue-less promotion path.

### Third failure of the same class found during verification
- TestHeuristicReview_ApprovalWithoutEvidenceNotValidated (same file) ALSO
  fails at clean HEAD 6c4e17fc (verified in /tmp/vacbase, no local changes):
  same leaf-06 supersession (file_write approval vehicle now rejected).
- HARNESS RE-POINT (not a weakening): the F11 contract is about the
  validation stamp on a heuristic approval, so the approval vehicle moved
  from file_write (gate-rejected shape) to coder + non-claim result; the
  persisted approved-state + Validated=false assertions are unchanged. The
  file-shape rejection verdict stays pinned by the leaf-06 gate tests and
  the vacuous pin.

## Failure 2: TestGuardRetry_RetryParksSkipsPostTurnPipeline (park-shape lane)

### Root cause
- M3 pin (2026-09-29) freezes the pre-T8 park sentinel: RunOnce on a parked
  guard-retry must return ("", nil).
- 01-safety/05 (Gate T8) changed the parked shape at the RunOnceWithParts
  boundary to ("", ErrTurnParked) and lane-split the RunOnce translation:
  autonomous ctx + injected-clock harness keep ("", nil); every other (chat)
  lane gets throttleParkNotice. 05's regression sweep regex
  (TestPark|TestThrottle|...) never matched this test name, so the pin was
  never re-based.

### Product-defect check (done, none found)
- The M3 branch (loop.go ~3956) correctly returns ("", ErrTurnParked) and
  skips the post-turn pipeline (no blank assistant append) -- the defect M3
  guarded against stays fixed.
- RunOnce's lane split is deliberate (05-REPORT design deviation): this
  loop has no autonomous marker and no injected clock -> it IS a chat lane
  -> throttleParkNotice is the correct surface. No principled product lane
  can distinguish a "guard-retry park" from a first-cycle park at the
  RunOnce boundary (both are mid-turn provider waits; the reply-guard retry
  is an internal loop detail invisible to RunOnce callers).

### Fix (harness re-point to the T8 lane contract)
- The pin now drives RunOnceWithParts (the seam M3 actually guards) and
  asserts the EXPLICIT parked shape: ("", ErrTurnParked) + StateQuotaWait +
  no blank assistant append. RunOnce's chat-lane notice translation is
  pinned by the T8 park tests (unchanged).

## Verification (all GREEN)
- go test ./internal/agent/ -run 'TestGuardRetry|TestTacticalScheduler' -count=1 -> ok
- go test ./internal/agent/ -run 'TestGuardRetry|TestTacticalScheduler|TestHeuristicReview|TestFileStep|TestErroredFileToolGate|TestDeniedWriteGate|TestZeroEvidence' -count=1 -> ok
- go test ./internal/agent/ -count=1 -> ok 101.5s (ZERO failures)
- go test ./internal/daemon/ -run 'TestAgentJobProcessor_ParkedTurn|TestParkedTurn' -count=1 -> ok
- go build ./... -> exit 0
- gofmt -l internal/agent/ -> empty

## Files touched
- internal/agent/tactical.go (PRODUCT: nil-queue guard in ScheduleReadySteps)
- internal/agent/tactical_validation_gate_test.go (2 pins re-based to the
  leaf-06 superseding contract; documented in-file)
- internal/agent/reply_guard_retry_fallback_test.go (M3 pin re-based to the
  T8 explicit parked shape; documented in-file)

## Status: DONE (report written, no commit per constraints)
