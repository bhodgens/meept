# Honest Completion States - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Error-steps are never auto-approved and tasks carrying them complete as failed, with the error text as the user-visible result.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Audit references:** finding F2 (errored tasks reported completed), 2026-09-04 comparison

## Goal

The 2026-09-04 test showed: a step whose `file_write` tool call errored got
"Skipped review — step has execution error" → **ReviewApproved** → "Task
completed", and the user saw a success stub while nothing was built. This
leaf makes the review skip path return a rejected/failed outcome and makes
task completion honest: any failed step fails the task, and the completion
payload carries the error text.

## Context

ReviewManager.ReviewStep (internal/agent/review_manager.go) skips full
review for error steps at lines 150-163, returning ReviewApproved with
feedback "Skipped review — step has execution error". The heuristic path
(165-188) can also approve a short non-empty error string ("error: xyz" is
> 3 chars) because heuristicReviewPasses (861-871) only checks length.
TacticalScheduler completes the task at internal/agent/tactical.go:853-891,
publishing task.completed with a "result" summary from
buildResultSummary (tactical.go:1421).

Error detection exists: `stepHasError` (review_manager.go:810-825) checks
the step Result for error indicators.

Key files to understand before implementing:
- internal/agent/review_manager.go - ReviewStep, stepHasError, heuristicReviewPasses, ReviewResult/ReviewStatus types
- internal/agent/tactical.go - step completion flow (~840-900), buildResultSummary (1421)
- internal/task - Task states (StateCompleted/StateFailed), StepStates

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/review_manager.go
//   - ReviewStep: error steps return ReviewRejected with feedback naming
//     the execution error (NEVER ReviewApproved).
//   - heuristicReviewPasses: returns false when stepHasError(step) is true.
// internal/agent/tactical.go
//   - Task with >=1 StepFailed step -> task.SetState(task.StateFailed),
//     task.completed payload still published with "result" carrying the
//     step error text, "status": "failed" key added to the payload.
// Owner: 02. Consumers: 01 (failed branch text), 10 (e2e asserts).
```

### What This Leaf Consumes

```
// rm.stepHasError(step *task.TaskStep) bool — existing.
// task.StateFailed, task.StepFailed — existing.
```

## Tasks

### Task 1: error steps reject review

**Objective:** ReviewStep returns ReviewRejected for error steps.

**Files:**
- Modify: `internal/agent/review_manager.go:150-163`
- Test: `internal/agent/review_manager_test.go` (create if absent — check
  existing review tests in spec_review_integration_test.go for patterns)

**Step 1: Write failing test**

```go
func TestReviewManager_ErrorStepNeverApproved(t *testing.T) {
	rm := newTestReviewManager(t) // mirror existing review-manager test setup
	step := &task.TaskStep{
		ID: "step-err-1", TaskID: "task-err-1", Sequence: 0,
		State:  task.StepCompleted,
		Result: "error: file_write failed: no path specified",
	}
	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status == ReviewApproved {
		t.Fatalf("error step was APPROVED: %+v", res)
	}
	if res.Status != ReviewRejected {
		t.Errorf("status = %v, want ReviewRejected", res.Status)
	}
	if !strings.Contains(res.Feedback, "execution error") {
		t.Errorf("feedback should name the execution error, got %q", res.Feedback)
	}
}
```

**Step 2: Run test to verify failure**

Run: `go test -p 2 ./internal/agent/ -run TestReviewManager_ErrorStepNeverApproved -v`
Expected: FAIL - status is ReviewApproved today.

**Step 3: Write minimal implementation**

Replace the skip block (150-163):

```go
// FAIL: Tool execution failed — nothing meaningful to review. The step
// must not pass review: an approved error is how "Task completed" stubs
// lied to users (2026-09-04 finding F2).
if rm.stepHasError(step) {
	rm.logger.Info("Review rejected: step execution produced an error",
		"step_id", step.ID,
		"tool_hint", step.ToolHint,
	)
	if err := rm.stepStore.SetState(step.ID, task.StepFailed); err != nil {
		rm.logger.Error("Failed to set error step to failed", "error", err)
	}
	return &ReviewResult{
		Status:     ReviewRejected,
		Feedback:   "Rejected: step execution error — " + firstLine(step.Result),
		Confidence: 1.0,
	}, nil
}
```

Add `firstLine(s string) string` helper (strings helper, ~5 lines: up to
the first '\n', capped at 200 chars).

**Step 4: Run test to verify pass**

Run: `go test -p 2 ./internal/agent/ -run TestReviewManager -v`
Expected: PASS (including pre-existing review tests — fix interactions if any
assert the old approved-on-error behavior; report any such test as a deviation).

### Task 2: heuristic cannot approve errors

**Objective:** heuristicReviewPasses returns false for error-shaped results.

**Files:**
- Modify: `internal/agent/review_manager.go:861-871`
- Test: `internal/agent/review_manager_test.go`

**Step 1: Write failing test**

```go
func TestReviewManager_HeuristicRejectsErrorResult(t *testing.T) {
	rm := newTestReviewManager(t)
	step := &task.TaskStep{Result: "error: unable to open database file"}
	if rm.heuristicReviewPasses(step) {
		t.Fatal("heuristic passed an error-shaped result")
	}
}
```

**Step 2: verify failure** (run; expect FAIL — length 38 > 3 passes today).

**Step 3: implement** — first line of heuristicReviewPasses:

```go
if rm.stepHasError(step) {
	return false
}
```

**Step 4: verify pass** — same run command, PASS.

### Task 3: task completion reflects failed steps

**Objective:** Task with failed steps completes as StateFailed; payload carries the error.

**Files:**
- Modify: `internal/agent/tactical.go` (completion block ~840-891)
- Test: `internal/agent/tactical_test.go` (or the file where tactical
  completion is tested — search for `task.completed` in _test files)

**Step 1: Write failing test**

```go
func TestTactical_FailedStepFailsTask(t *testing.T) {
	ts := newTestTactical(t) // mirror existing tactical test setup
	// Seed: task with 2 steps, one StepFailed (Result: "error: file_write
	// failed"), one StepApproved.
	// Drive the step-completion path for the approved step so the task
	// finalizes.
	// Assert: taskStore shows StateFailed; the published task.completed
	// payload has "status"=="failed" and "result" containing
	// "file_write failed".
}
```

**Step 2: verify failure** (run; expect FAIL — task completes StateCompleted today).

**Step 3: implement**

In the completion block before `t.SetState(...)`:

```go
failedSteps, err := ts.failedStepsForTask(step.TaskID) // small helper: ListByTaskID, filter State==StepFailed
if err != nil {
	ts.logger.Error("Failed to list failed steps", "task_id", step.TaskID, "error", err)
}
taskFailed := len(failedSteps) > 0
if taskFailed {
	t.SetState(task.StateFailed)
} else {
	t.SetState(task.StateCompleted)
}
```

Publish the same task.completed event (subscribers depend on it) with two
additions: `"status"` key ("failed"/"completed") and, when taskFailed,
`"result"` replaced by the first failed step's error text (firstLine,
capped 400 chars). Keep execution_time/agents_used/token_usage as-is.

Also raise buildStepSummaries Result truncation 100 → 400 (tactical.go:1412)
so reply-path payloads carry usable text.

**Step 4: verify pass** — run tactical tests, PASS.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] Orchestrator/review subscribers to task.completed still work (event topic unchanged)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Error steps return ReviewRejected and step state StepFailed
- [ ] heuristicReviewPasses gates on stepHasError
- [ ] Task with failed steps -> StateFailed, payload status "failed", error text in result
- [ ] buildStepSummaries truncation raised to 400
- [ ] Pre-existing review/tactical tests pass or deviations documented
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- ChatHandler.handleTaskCompleted (handler.go:1161) formats payload steps —
  it will now surface the failure text via leaf 01's C1 contract; do not
  modify handleTaskFailed here.
- stepHasError checks the step's OWN Result; error text set by the job
  processor on failure. Confirm the failed-step Result carries "error:"
  (components.go AgentJobProcessor error path) — if it stores a different
  shape, extend stepHasError rather than the producer.
- Pair path (tactical.go:997-1012) already marks steps failed; do not touch.
