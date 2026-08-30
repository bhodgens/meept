# Dispatcher deferral + auto-resume - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** When every provider is quota-blocked, the dispatcher defers the
  task (step checkpoint) and auto-resumes at the reset time via the
  scheduler; model switches during a task are recorded in run metadata with
  auto-revert to the primary when it unblocks.
- **Dependencies:** 03 (broker blocks), 05 (tracker/states)
- **Estimated Context:** 70K
- **Concurrency Group:** D

## Goal

"Fail the model, not the agent." A quota wall must never fail a task while
the design's fallbacks still exist, and must never kill a task outright when
nothing is available — the task parks with a checkpoint and resumes
automatically. This leaf owns the dispatcher/executor side.

## Context

Task execution lives in `internal/agent` (loop, executor, dispatcher) with
tasks visible via `internal/queue`/`internal/scheduler` and the `meept
agents` CLI. VERIFY with search_files: how a running task step reports LLM
errors today, whether tasks have resumable state (step index / message
history persisted), and how the scheduler accepts delayed work. The
checkpoint requirement: resume continues at the failed step, not from
scratch — if the task runtime is inherently resume-safe (conversation
history persists per session), the checkpoint may be just "retry this
task/session"; document what you find.

Key files to understand before implementing:

- `internal/agent/` executor + dispatcher — where Chat errors decide task
  failure today.
- `internal/scheduler/` + `internal/queue/` — delayed/enqueue mechanisms.
- `internal/agent/quota_episode.go` (leaf 05) — tracker API.
- `internal/llm/resolver.go` — ActiveQuotaBlocks, ResolveForModel (leaf 03).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/agent (executor/dispatcher file chosen after exploration)

// On a task step's Chat error:
//   var qe *llm.QuotaResetError
//   if errors.As(err, &qe) AND broker has no unblocked candidates:
//       - DO NOT fail the task
//       - tracker.Enter(agentID, credentialKey, modelID, unblockAt, taskID)
//       - persist deferral: {taskID, sessionID, unblockAt, credentialKey}
//         via the scheduler with cadence min(DeferCheckInterval, time-to-
//         unblock) re-checks; hard stop at MaxWait -> task + agent -> blocked
//       - on resume tick: re-dispatch the SAME task; the broker naturally
//         serves the primary model if unblocked (auto-revert) or falls
//         back otherwise; if the resumed attempt still hits the wall,
//         re-defer (idempotent Enter extends the episode)
//   if errors.As(err, &qe) AND rotation found a fallback (err is nil,
//       response succeeded): record the switch in task run metadata:
//       {step, from_model, to_model, switched_at} (transparency only —
//       never interrupt the task)

// Model-switch recording helper:
func recordModelSwitch(runMeta, step int, fromModel, toModel string)

// Deferral re-check uses config llm.quota_retry.DeferCheckInterval (leaf 02).
```

### What This Leaf Consumes

```
// leaf 03: broker.ActiveQuotaBlocks / an "any candidate available" query
// leaf 05: QuotaEpisodeTracker.Enter/Clear
// leaf 02: config.QuotaRetryConfig
```

## Tasks

### Task 1: Quota error -> deferral path

**Objective:** A task whose Chat hits an all-blocked quota wall defers
instead of failing.

**Files:**
- Modify: the executor/dispatcher file that handles Chat errors
- Test: `internal/agent/quota_deferral_test.go`

**Step 1: Write failing test**

Fake broker whose chatter returns QuotaResetError (future horizon) and
reports all-blocked. Dispatch a task: assert (a) task state is deferred/
suspended (per existing task-state vocabulary — VERIFY names; do not invent
a new status if an existing one fits, e.g. paused/scheduled), NOT failed;
(b) tracker.Enter was called with the right unblockAt; (c) a scheduler
entry exists with a re-check time; (d) no failure/abort events for the task.

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestQuotaDeferral -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Insert the errors.As branch BEFORE generic failure handling in the error
path. Keep the branch narrow: only QuotaResetError + no-candidates defers;
anything else falls through to existing behavior.

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run TestQuotaDeferral -v`
Expected: PASS

### Task 2: Auto-resume at unblock

**Objective:** The scheduled re-check re-dispatches the task; success
completes it; still-blocked re-defers.

**Files:**
- Modify: same dispatcher file + scheduler wiring
- Test: same test file

**Step 1: Write failing test**

(a) Simulate the resume tick with the fake broker now succeeding: task
re-dispatches from its checkpoint, completes, agent state quota_wait ->
running (tracker.Clear called). (b) Resume tick while still blocked:
re-defer (idempotent), no duplicate episodes, scheduler re-entry created.
(c) Resume tick finds the task manually cancelled meanwhile: drop the
deferral without dispatching.

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestQuotaResume -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Resume handler validates the task is still pending/deferrable, then
re-dispatches through the normal path. Verify the checkpoint story you
found in Context and record it in the leaf report (what "resume at failed
step" concretely means in this codebase).

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run TestQuotaResume -v`
Expected: PASS

### Task 3: Model-switch metadata + auto-revert

**Objective:** Fallback usage during a task is recorded; primary reclaims on
unblock.

**Files:**
- Modify: same dispatcher/executor file
- Test: same test file

**Step 1: Write failing test**

(a) Chat succeeds via fallback while primary is blocked: run metadata gains
a switch record {step, from, to, at}; task NOT interrupted. (b) Next step
with primary unblocked: primary used again (auto-revert — this falls out of
broker rotation naturally; assert via fake broker call order) and metadata
does NOT grow a "revert" record (revert is implicit; only switches are
recorded). (c) Multiple switches accumulate as ordered records.

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestQuotaModelSwitch -v`
Expected: FAIL

**Step 3: Write minimal implementation**

If the broker/API exposes which model served the response (VERIFY — the
Response type may carry a model field; if not, derive from the chatter
used), record switches. Auto-revert requires no code if rotation prefers
the primary when available — assert that; if the broker prefers a
"sticky" last-used model, add primary-first ordering per the contract and
note the deviation.

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run TestQuotaModelSwitch -v`
Expected: PASS

### Task 4: MaxWait soft-stop for tasks

**Objective:** A task deferred past MaxWait transitions to blocked with its
agent.

**Files:**
- Modify: same dispatcher file
- Test: same test file

**Step 1: Write failing test**

Injected clock: task deferred; all resume attempts still blocked; at
MaxWait the resume handler transitions task + agent to blocked (tracker
BlockedByEscalation path), stops scheduling re-checks, and emits the final
escalation event (escalation="24h").

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestQuotaMaxWait -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Enforce MaxWait in the resume handler.

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run 'TestQuota' -count=1 -v`
Expected: PASS
Then: `go test -race ./internal/agent/ -run TestQuota -count=1`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] No path fails a task for QuotaResetError while candidates remain
- [ ] Non-quota errors take the pre-existing failure path untouched

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly
- [ ] Deferral branch is narrow (quota + all-blocked only)
- [ ] Resume is idempotent; cancelled tasks drop cleanly
- [ ] Switch metadata never interrupts execution
- [ ] Race-clean

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The hardest unknown is the task checkpoint story. Explore BEFORE writing
  the failing tests (Task 1) and record findings in the leaf report. If
  tasks are inherently session-resumable, "checkpoint" = re-dispatch same
  session; if not, persist the minimal step marker. Do not build a
  general-purpose checkpoint framework — the narrowest mechanism that
  resumes at the right step wins.
- The user-visible phrasing for deferral (already approved in design):
  "model X quota exhausted, resets in ~Nh — task paused, will resume
  automatically." Lowercase; surface via task status, not chat bubbles.
