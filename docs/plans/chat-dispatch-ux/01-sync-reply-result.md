# Sync Reply Carries Real Result - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** waitForTaskCompletion returns the terminal step's real Result text to the chat caller instead of the "Task <id> completed." stub.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Audit references:** finding F1 (stub replies), 2026-09-04 comparison

## Goal

When a chat request dispatches synchronously, the user must receive the
coder/specialist's actual user-facing summary. Today `waitForTaskCompletion`
polls the task store and returns `fmt.Sprintf("Task %s completed.", taskID)`
(internal/agent/handler.go:1682), so the CLI prints a task-ID stub even
though the executed step's `Result` field holds the real reply. This leaf
makes the reply come from the step data, with the stub as last-resort
fallback only.

## Context

Meept is a Go daemon. Chat requests flow: RPC "chat" proxy → bus
chat.request → ChatHandler (internal/agent/handler.go) → dispatcher →
orchestrator/tactical → step jobs. In sync mode (handler.go:756-765) the
handler publishes a plan request and blocks in `waitForTaskCompletion`.

The step store (`internal/task/step.go`) has `ListByTaskID(taskID)
([]*TaskStep, error)` at line 658; `TaskStep.Result` is the agent's final
text (struct at step.go:135-170). Steps carry `State` (StepCompleted /
StepApproved / StepFailed per internal/task).

Key files to understand before implementing:
- internal/agent/handler.go - contains waitForTaskCompletion (line ~1650-1686) and fetchStepSummaries (line 1441) to model after
- internal/task/step.go - StepStore.ListByTaskID, TaskStep struct
- internal/agent/handler_test.go - existing handler test patterns (38 tests)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/handler.go
// waitForTaskCompletion behavior change (signature unchanged):
//   - terminal StateFailed -> return the failure summary (see Task 3)
//   - terminal success     -> return best step Result (see Task 2)
//   - all step Results empty -> existing "Task %s completed." fallback
// No exported API changes.
```

### What This Leaf Consumes

```
// internal/task StepStore.ListByTaskID(taskID) ([]*TaskStep, error)
// TaskStep fields: State StepState, Result string, Sequence int
```

## Tasks

### Task 1: step result extraction helper

**Objective:** Add a pure helper that selects the user-facing result from steps.

**Files:**
- Modify: `internal/agent/handler.go` (near fetchStepSummaries, ~line 1441)
- Test: `internal/agent/handler_test.go`

**Step 1: Write failing test**

```go
func TestChatHandler_BestStepResult(t *testing.T) {
	tests := []struct {
		name  string
		steps []*task.TaskStep
		want  string
	}{
		{"nil steps", nil, ""},
		{"empty results", []*task.TaskStep{{State: task.StepCompleted}}, ""},
		{"picks completed result", []*task.TaskStep{
			{State: task.StepCompleted, Result: ""},
			{State: task.StepApproved, Result: "I created water_reminder.py for you. Run it with python3."},
		}, "I created water_reminder.py for you. Run it with python3."},
		{"highest sequence wins", []*task.TaskStep{
			{Sequence: 0, State: task.StepApproved, Result: "first"},
			{Sequence: 1, State: task.StepApproved, Result: "second"},
		}, "second"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := bestStepResult(tc.steps)
			if got != tc.want {
				t.Errorf("bestStepResult = %q, want %q", got, tc.want)
			}
		})
	}
}
```

**Step 2: Run test to verify failure**

Run: `go test -p 2 ./internal/agent/ -run TestChatHandler_BestStepResult -v`
Expected: FAIL - undefined: bestStepResult

**Step 3: Write minimal implementation**

```go
// bestStepResult picks the user-facing reply from executed steps: the
// Result of the highest-sequence completed/approved step that has
// non-empty text; falls back to any non-empty Result.
func bestStepResult(steps []*task.TaskStep) string {
	best := ""
	bestSeq := -1
	for _, s := range steps {
		if s == nil || s.Result == "" {
			continue
		}
		done := s.State == task.StepCompleted || s.State == task.StepApproved
		if done && s.Sequence > bestSeq {
			best, bestSeq = s.Result, s.Sequence
		}
	}
	if best != "" {
		return best
	}
	// Fallback: any non-empty result (e.g. approved-state variants).
	for _, s := range steps {
		if s != nil && s.Result != "" {
			return s.Result
		}
	}
	return ""
}
```

**Step 4: Run test to verify pass**

Run: `go test -p 2 ./internal/agent/ -run TestChatHandler_BestStepResult -v`
Expected: PASS

### Task 2: waitForTaskCompletion returns real result

**Objective:** Terminal-success branch fetches steps and returns bestStepResult.

**Files:**
- Modify: `internal/agent/handler.go:1678-1683` (terminal-state branch)
- Test: `internal/agent/handler_test.go`

**Step 1: Write failing test**

Use the existing handler-construction pattern from handler_test.go (build a
ChatHandler with a real sqlite task/step store in a t.TempDir() — mirror
how existing tests construct stores; check `internal/task` test helpers).

```go
func TestChatHandler_WaitForTaskCompletion_ReturnsStepResult(t *testing.T) {
	// Arrange: handler with step store; a task in StateCompleted with one
	// approved step whose Result is the user-facing summary.
	h := newTestChatHandlerWithStores(t) // helper; see existing test patterns
	taskID := seedCompletedTaskWithStepResult(t, h, "I created water_reminder.py. Run: python3 water_reminder.py")

	// Act
	reply := h.waitForTaskCompletion(context.Background(), taskID)

	// Assert
	if reply != "I created water_reminder.py. Run: python3 water_reminder.py" {
		t.Errorf("reply = %q, want the step result text", reply)
	}
	if reply == fmt.Sprintf("Task %s completed.", taskID) {
		t.Fatal("reply is the task-ID stub — regression")
	}
}
```

**Step 2: Run test to verify failure**

Run: `go test -p 2 ./internal/agent/ -run TestChatHandler_WaitForTaskCompletion_ReturnsStepResult -v`
Expected: FAIL - got the stub string.

**Step 3: Write minimal implementation**

Replace the terminal branch (handler.go:1678-1683) with:

```go
if t.State.IsTerminal() {
	if t.State == task.StateFailed {
		return fmt.Sprintf("Task %s failed after reaching terminal state.", taskID)
	}
	if h.stepStore != nil {
		if steps, err := h.stepStore.ListByTaskID(taskID); err == nil {
			if result := bestStepResult(steps); result != "" {
				return result
			}
		} else {
			h.logger.Debug("Failed to fetch steps for sync reply",
				"task_id", taskID, "error", err)
		}
	}
	return fmt.Sprintf("Task %s completed.", taskID)
}
```

Preserve the polling loop, timeouts, and logging exactly. Note: Task 3 of
leaf 02 refines the failed branch — leave the failure string as-is here.

**Step 4: Run test to verify pass**

Run: `go test -p 2 ./internal/agent/ -run TestChatHandler_WaitForTaskCompletion -v`
Expected: PASS

### Task 3: guard the stub against regression

**Objective:** A focused test that any completed task with a non-empty step Result never yields the stub.

**Files:**
- Test: `internal/agent/handler_test.go`

**Step 1: Write test** (table: step-result / empty-result / store-error
cases; assert stub only appears in the empty-result case).

**Step 2: Run** `go test -p 2 ./internal/agent/ -run TestChatHandler_WaitForTaskCompletion -v` — PASS.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] Polling loop / timeout / logging behavior unchanged

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] bestStepResult prefers done steps by sequence, falls back safely
- [ ] waitForTaskCompletion still returns the stub ONLY when all results are empty
- [ ] Failed-task branch untouched (leaf 02 owns it)
- [ ] Code follows project conventions
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The store-error path must not mask a good reply: on ListByTaskID error,
  log Debug and fall through to the stub (same as today).
- `fmt` is already imported in handler.go.
- If handler_test.go lacks a stores-bearing ChatHandler helper, write the
  smallest one (sqlite in t.TempDir()) — look at internal/task test setup.
