# TUI Flows Extension - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. After completing, report what
> you built, what files you touched, and any deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Add three e2e flows to the existing `tui-flows` suite: chat
  steer mid-turn, plans view render, task submit -> tasks view row.
- **Dependencies:** none
- **Estimated Context:** ~70K
- **Concurrency Group:** A

## Goal

`tui-flows` covers 5 flows; the operator-critical surfaces chat steering,
the plans view, and the tasks view have zero e2e. This leaf adds one test
per surface, reusing the existing `tuiDriver` mechanics unchanged (same
construction, same settle/finish discipline, same assertion style).

## Context

Repo: /Users/caimlas/git/meept. The suite drives the REAL
`tui.NewApp` under a headless bubbletea v2 Program against the harness
daemon. Input arrives via `hp.send(...)` (ordered, blocking); assertions
run on the FINISHED app (`hp.finish(w, h)` quits the loop first).

Key files to understand before implementing:

- `e2e/suites/tui-flows/tui_flows_test.go` - the 5 existing flows; copy
  their structure exactly (test names map to manifest scenario ids)
- `e2e/suites/tui-flows/driver_test.go` - `tuiDriver`, `newStack`-equivalent
  setup, `WithTargetSession`, `settleAsync`
- `e2e/harness/fakellm.go` - `EnqueueFileWrite`, `SetPostToolText`,
  predicate scripts; this is how turns are scripted
- `internal/tui/models/chat.go` - steerMode (ctrl+s toggle), SteeringResultMsg
- `internal/tui/models/plans.go`, `tasks.go` - the two views under test

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: e2e/suites/tui-flows/flows_extension_test.go (new file)
//go:build e2e
package tuiflows

func TestChatSteerMidTurnInjectsIntoQueue(t *testing.T)   // scenario tui-steer-01
func TestPlansViewRendersCreatedPlan(t *testing.T)         // scenario tui-plans-01
func TestTaskSubmitShowsInTasksView(t *testing.T)          // scenario tui-tasks-01
// Additive tuiOption constructors ONLY if a flow needs one;
// driver_test.go changes must be strictly additive.
```

### What This Leaf Consumes

```
e2e/harness: Start, RegisterProject, CreateSession, SubmitChatHTTP,
             Tasks, Steps, WaitFor, TaskRow.State/TotalJobs
tuiDriver:   newDriver-equivalent setup used by existing flows,
             hp.send, hp.settle, hp.finish, settleAsync
```

## Tasks

### Task 1: Chat steer mid-turn flow (tui-steer-01)

**Objective:** Prove ctrl+s steer mode + a queued message reaches the
daemon while a turn is in flight, observable as an injected queue entry.

**Files:**
- Create: `e2e/suites/tui-flows/flows_extension_test.go`
- Test: same file

**Step 1: Write the flow**

Structure: start a stack + session; script a LONG turn (fake-LLM
predicate that delays or a multi-tool queue so the turn is still running);
send chat text via the input path the existing streaming flow uses; while
in flight press `ctrl+s` then send steer text; assert on the finished
model that the steer badge/queue state appeared AND the daemon-side effect
is observable (chat-submit HTTP log or the turn's queued-message record -
use whichever seam `chat.go`'s SteeringInjectedMsg actually feeds; read it
first and assert on the REAL downstream seam, not the TUI field alone).

**Step 2: Verify**

Run: `go test -count=1 -tags e2e -run TestChatSteerMidTurn ./e2e/suites/tui-flows -v`
Expected: PASS (if the steering seam turns out to need a daemon-side
capability the fake daemon lacks, STOP and report the gap - do not mock
daemon behavior in the test).

### Task 2: Plans view flow (tui-plans-01)

**Objective:** A created plan renders in the plans view.

**Files:**
- Test: `e2e/suites/tui-flows/flows_extension_test.go` (Task 2 appended)

**Step 1: Write the flow**

Create a plan through the real RPC surface the TUI's plan flow uses (read
`internal/tui/models/plans.go` first: it lists plans via an RPC method -
use `s.RunCLI` or the harness HTTP equivalent to create a plan the same
way the e2e plan-lifecycle suite does). Then open the plans view via the
palette key flow, finish, and assert the finished view contains the
plan's title and the plans table is non-empty (the zero-width-viewport
regression class applies here too - assert at least one row renders).

**Step 2: Verify**

Run: `go test -count=1 -tags e2e -run TestPlansViewRenders ./e2e/suites/tui-flows -v`
Expected: PASS

### Task 3: Tasks view flow (tui-tasks-01)

**Objective:** A submitted task appears as a row in the tasks view.

**Files:**
- Test: `e2e/suites/tui-flows/flows_extension_test.go` (Task 3 appended)

**Step 1: Write the flow**

Submit a task (chat message that classifies to work, or the task-create
RPC if that is the TUI's real path - read `internal/tui/models/tasks.go`),
poll `harness.Tasks` until the task row exists, switch to the tasks view,
finish, and assert the finished view contains the task's description
fragment AND at least one rendered table row separator.

**Step 2: Verify**

Run: `go test -count=1 -tags e2e -run TestTaskSubmitShows ./e2e/suites/tui-flows -v`
Expected: PASS

### Task 4: Suite green

Run: `go test -count=1 -tags e2e ./e2e/suites/tui-flows`
Expected: ok (all 8 flows: 5 existing + 3 new)

## Self-Verification Checklist

- [ ] All 3 new flows pass with `-count=1`
- [ ] Existing 5 flows untouched and still passing
- [ ] No driver_test.go rewrite (additive only, or zero changes)
- [ ] No wall-clock sleeps without bounded polls
- [ ] Assertions run on FINISHED model or store reads, never live loop state
- [ ] No daemon code modified

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Three new test functions present with the exact names in the contract
- [ ] Each asserts a REAL downstream seam (store row, RPC effect), not just
      in-memory TUI fields
- [ ] Suite green with -count=1

## Notes

- If ctrl+s steering cannot be scripted against the fake daemon, report
  the exact seam gap instead of faking it - that is a finding, not a
  failure.
- settleAsync is 250ms; keep the same idiom, do not introduce new sleeps.
