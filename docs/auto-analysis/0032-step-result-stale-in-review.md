# Step Result Stale During Review (SetResult Updates DB Only, Not In-Memory Struct)

**Date**: 2026-05-15
**Phase**: 11 (playground integration)
**Severity**: high
**Component**: `internal/agent/tactical.go` (OnJobCompleted), `internal/task/step.go` (SetResult), `internal/agent/review_manager.go` (buildReviewPrompt)
**Status**: FIXED

## Description

When a step job completes, `OnJobCompleted` calls `stepStore.SetResult(step.ID, resultStr)` which updates the database row but does NOT update the in-memory `step` struct. The same stale `step` pointer is then passed to `reviewManager.ReviewStep(ctx, step)`. The review prompt reads `step.Result` which is still empty, causing the reviewer to always see an empty result and reject the step.

This affects ALL step completions where review is enabled, not just security-blocked tool calls. The reviewer consistently rejects valid work because it sees an empty Result field.

## Reproduction

1. Start the daemon with review enabled
2. Send any chat message that triggers a task with step decomposition (e.g., "review ~/git/meept-playground/buggy-app/main.go and identify any bugs")
3. Observe the daemon log:
   - Agent executes tools and produces a response
   - `SetResult` is called successfully
   - Review starts
   - Reviewer rejects with "Result field is empty" or "No output provided"

## Evidence

**`internal/agent/tactical.go`** lines 354-371 and 519 (BEFORE fix):
```go
// Line 354: step fetched from DB
step, err := ts.stepStore.GetByJobID(jobID)

// Line 371: DB updated, but `step` pointer NOT updated
if err := ts.stepStore.SetResult(step.ID, resultStr); err != nil {
    ts.logger.Error("Failed to set step result", "step_id", step.ID, "error", err)
}

// Line 519: same stale `step` passed to review
reviewResult, err := ts.reviewManager.ReviewStep(ctx, step)
```

**`internal/task/step.go`** lines 578-587:
```go
func (s *StepStore) SetResult(id, result string) error {
    // Only updates DB, does not update any in-memory struct
    _, err := s.db.Exec(`UPDATE task_steps SET result = ?, updated_at = ? WHERE id = ?`,
        result, now, id)
    return err
}
```

**`internal/agent/review_manager.go`** line 186:
```go
fmt.Fprintf(&sb, "Result:\n%s\n\n", step.Result)  // step.Result is empty!
```

**Daemon log evidence** (Test 5 - security block):
```
21:49:00 INFO  Tool blocked by security agent=coder tool=file_read reason="Path does not match any allowed path pattern" risk=SAFE
21:49:23 INFO  Agent loop complete agent=coder iterations=2
21:49:23 INFO  DONE job completed agent_id=coder
21:49:35 INFO  Step rejected issues="[Result field is empty - no file content shown...]"
```

The agent produced a valid text response explaining the security block, but the reviewer rejected it because `step.Result` was empty (stale pointer).

## Fix Applied

Applied Option A (minimal) -- after each `SetResult` call in `tactical.go`, the in-memory `step.Result` is updated:

1. **OnJobCompleted** (line ~371): After `SetResult(step.ID, resultStr)`, added `step.Result = resultStr`
2. **OnJobFailed retry path** (line ~807): After `SetResult(step.ID, "")` (clearing result for retry), added `step.Result = ""`
3. **OnJobFailed** (line ~828): After `SetResult(step.ID, jobErr)` (error result), added `step.Result = jobErr`

## Root Cause

`SetResult` performs a DB update but does not update the Go struct in memory. The `step` pointer obtained from `GetByJobID` retains its original (empty) `Result` field. When this stale pointer is passed to the review manager, the review prompt contains an empty Result section.

This is a classic stale-cache bug: the in-memory representation and database state diverge after `SetResult`.

## Impact

This bug causes the reviewer to reject ALL step completions where the result is non-trivial. This leads to:
- Unnecessary revision cycles that consume token budget
- False rejections of valid work
- Budget exhaustion from retry loops (compounds with bug 0021)
- User-facing errors and lost responses

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
