# Session Context Digest - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Build SessionContextDigest — a compact per-session summary (last task, state, agent, result summary, last intent) the dispatcher can hand to the intent analyzer.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A

## Goal

The intent analyzer judges messages in isolation because nothing hands it
session state. This leaf builds the session-state reader: a small digest
struct plus a Dispatcher method that populates it from existing stores
(TaskStore.GetTasksForSession → most recent task; TaskRegistry.StepStore()
→ that task's terminal step result; SessionTracker.GetLastIntent). All
store access is nil-guarded; failures degrade to an empty digest, never
an error path.

## Context

Dispatcher fields: taskStore (internal/agent/dispatcher.go:299 config,
:224 field), taskRegistry (:300/:349), sessionTracker (:236). The
session's tasks: task.Store.GetTasksForSession(sessionID)
(internal/task/store.go:423) returns []*Task ordered by updated_at DESC
— tasks[0] is the most recent. Task steps:
task.Registry.StepStore() (internal/task/registry.go:375) →
StepStore.ListByTaskID (internal/task/step.go:658) returns []*TaskStep;
the useful result is the highest-sequence step in
StepCompleted/StepApproved state with non-empty Result (same selection
rule as bestStepResult in internal/agent/handler.go — mirror it).
Last intent: SessionTracker.GetLastIntent(sessionID) returns *Intent with
Type field (internal/agent/session_tracker.go:135).

Key files to understand before implementing:
- internal/agent/dispatcher.go - fields (224-236), buildMemoryContext pattern (1080) for nil-guard style
- internal/task/store.go:423 - GetTasksForSession
- internal/task/step.go:658 - ListByTaskID; TaskStep: State, Result, Sequence
- internal/agent/session_tracker.go:135 - GetLastIntent
- internal/agent/handler.go - bestStepResult (selection rule to mirror)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/session_digest.go (new file, package agent)
type SessionContextDigest struct {
    LastTaskName      string
    LastTaskState     string
    LastTaskAgent     string
    LastResultSummary string
    LastIntentType    string
}
func (s *SessionContextDigest) IsEmpty() bool
func (d *Dispatcher) buildSessionContextDigest(sessionID string) *SessionContextDigest
// Rules:
//   - sessionID "" or all stores nil → &SessionContextDigest{} (empty)
//   - tasks[0] = most recent (GetTasksForSession is updated_at DESC);
//     name cap 200 chars, result summary = first line of the selected
//     step Result capped 400 (mirror handler.go firstLine/truncateString
//     or reuse them if exported in package agent)
//   - step selection: highest Sequence among StepCompleted/StepApproved
//     steps with non-empty Result; fallback any non-empty Result
//   - any store error → skip that field (Debug log), never propagate
// Owner: 01. Consumers: 02 (analyzer input), 03 (e2e).
```

### What This Leaf Consumes

```
// d.taskStore.GetTasksForSession, d.taskRegistry.StepStore().ListByTaskID,
// d.sessionTracker.GetLastIntent
```

## Tasks

### Task 1: SessionContextDigest type + IsEmpty

**Objective:** The struct and its emptiness check.

**Files:**
- Create: `internal/agent/session_digest.go`
- Test: `internal/agent/session_digest_test.go`

**Step 1: Write failing test** — zero-value digest IsEmpty() == true;
populated digest IsEmpty() == false.

**Step 2: verify failure** (`go test -p 2 ./internal/agent/ -run TestSessionContextDigest -count=1 -v` → undefined) → implement → pass.

### Task 2: buildSessionContextDigest

**Objective:** Dispatcher method populating the digest from stores.

**Files:**
- Modify: `internal/agent/session_digest.go` (method on Dispatcher)
- Test: `internal/agent/session_digest_test.go`

**Step 1: Write failing tests** — table:
(a) nil stores → empty digest, no panic;
(b) one linked task (completed) + one completed step with Result →
digest carries name/state "completed"/agent/result first line;
(c) no steps with results → LastResultSummary empty, other fields set;
(d) two tasks → most recently updated wins;
(e) task name > 200 chars → capped;
(f) step Result multi-line → first line only, >400 chars → capped.

Build with real task.Store + StepStore in t.TempDir() (mirror
internal/daemon/agent_job_processor_test.go store setup) and a real
SessionTracker via NewSessionTracker + RecordIntent.

**Step 2: verify failure → implement → verify pass.**
Run: `go test -p 2 ./internal/agent/ -run TestSessionContextDigest -count=1 -v`

### Task 3: wire into package build

**Objective:** Compiles clean, package green.

**Step 1:** `go build ./internal/agent/ && go test -p 2 ./internal/agent/ -count=1` — green.
(The dispatcher call site is leaf 02's job — do NOT touch it here.)

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] No store error propagates out of buildSessionContextDigest

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Digest fields per contract; caps 200/400 applied
- [ ] Step selection mirrors bestStepResult rule
- [ ] Every store access nil-guarded; errors degrade to empty fields
- [ ] No dispatcher call-site changes (leaf 02 owns those)
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- sessionTracker.GetLastIntent returns a COPY — safe to read without
  holding tracker locks.
- GetTasksForSession errors: log Debug, return empty digest (the caller
  treats empty as "no context", which is the pre-tree behavior).
