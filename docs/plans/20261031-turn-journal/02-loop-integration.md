# Loop Step-Boundary Integration - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** The JournalWriter seam + its integration at the step-execution
  boundary (write on step start/finish, skip already-journaled steps), plus
  the components.go/daemon.go wiring that constructs and installs it when the
  flag is on.
- **Dependencies:** 01-journal-schema.md (store + config flag)
- **Estimated Context:** 105K
- **Concurrency Group:** C
- **Audit references:** master.md Requirements R1, R2, R4

## Goal

Make the journal WRITE happen on the real turn path: every step execution
under the flag emits `run_started` (first step), `step_started`,
`step_completed` (with the step result blob), and `run_closed` (turn terminal).
The same leaf adds the skip seam: a turn resuming with journaled
`step_completed` rows skips those steps, restoring their recorded results
without re-invoking the LLM (R0 findings F3/F4, D2). Quota errors anywhere on
this path PARK via the existing mechanisms - never short-retry (R4).

## Context

- internal/agent/loop.go is the agent loop; the daemon's step-job execution
  enters it via the step pipeline in internal/daemon/components.go
  (stepJobTurnContext at :9036-9146 builds the execution context for a queued
  job's turn). The step list and per-step results live on the task/step
  records (internal/task).
- The park machinery shows the house pattern for turn-level durable records:
  internal/agent/parked_turn.go:71 ParkedTurnRecord (TurnPayload carries the
  original request so a resume can re-run without reconstructing history).
- Quota invariants: internal/llm/AGENTS.md - a *llm.QuotaResetError is never
  an alias failure and never short-retried; existing parks go through
  TurnParker (internal/agent/parked_turn.go:98-167) wired at
  internal/daemon/components.go:3484 (SetParkPersistence) and the quota resume
  watcher.
- The writer is BEST-EFFORT (master C2): a journal write failure logs Warn and
  the turn proceeds - the journal must never break the turn it records.

## Verified Anchors

<!-- Exact, grep-confirmed at authoring time. No approximate pointers. -->

- internal/daemon/components.go:2610 - parks.db store construction
  (best-effort open, Warn on failure); construct the journal store beside it.
- internal/daemon/components.go:3484 - SetParkPersistence wiring site; wire
  the JournalWriter onto the step pipeline beside it.
- internal/daemon/components.go:9036-9146 - stepJobTurnContext: the queued
  job's turn execution context; the journal metadata (turn's completed-step
  set) rides INTO the loop here.
- internal/agent/parked_turn.go:98-167 - TurnParker type + semantics (the
  ONLY park path; this leaf adds none).
- internal/queue/queue.go:491 - PersistentQueue.CompleteAttempt; :539
  CompletionIsFresh - the ONLY completion dedup; this leaf does not touch it,
  only avoids duplicating it.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
Contract C2 (JournalWriter) + C4 (skipped-step seam), exactly as frozen in
../master.md:
internal/agent/turnjournal/writer.go:
  func NewJournalWriter(store JournalStore, logger *slog.Logger) *JournalWriter
  func (w *JournalWriter) OnStepStart(ctx, turnID, sessionID, stepID string, stepIndex int)
  func (w *JournalWriter) OnStepComplete(ctx, turnID, sessionID, stepID string, stepIndex int, result []byte)
  func (w *JournalWriter) OnRunStart(ctx, turnID, sessionID string, turnPayload []byte)
  func (w *JournalWriter) OnRunClose(ctx, turnID string)
  All best-effort: 5s op timeout, Warn on error, never fail the turn.
internal/agent/turnjournal/replay.go:
  func PlanResume(ctx context.Context, store JournalStore, turnID string, steps []StepRef) (skip map[string][]byte, next int, err error)
  type StepRef struct { StepID string; StepIndex int }
  skip maps completed StepID -> recorded result; next = index of the first
  step without a step_completed row (or len(steps) if all done).
Daemon wiring (C6): c.TurnJournal store beside parks.db
  (components.go:2610 area); JournalWriter installed on the step pipeline
  beside SetParkPersistence (components.go:3484 area); both gated on
  cfg.Daemon.TurnJournalEnabled, both nil when the flag is off or the store
  failed to open.
```

### What This Leaf Consumes

- [C1 Journal Record and Store] - `NewSQLiteJournal(dbPath string, logger *slog.Logger) (*SQLiteJournal, error)`, `Append/OpenRuns/CompletedSteps/CloseRun/Close` - owning leaf 01.
- [C5 Config Flag] - `daemon.turn_journal_enabled bool (default false)` - owning leaf 01.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | JournalWriter: NewJournalWriter + the four On* methods, each best-effort (5s ctx timeout, Warn log, no error to caller); OnRunStart writes run_started exactly once per turn (idempotent append - second call is a no-op) | Create: internal/agent/turnjournal/writer.go; Test: internal/agent/turnjournal/writer_test.go | `go test -p 2 ./internal/agent/turnjournal/ -run TestJournalWriter -v` | writer does not exist | tests: happy-path writes all four kinds; a store whose Append fails leaves the On* call returning silently (no panic, Warn logged); double OnRunStart produces one run_started row; OnRunClose is idempotent | Given a writer over a live store; When the four On* methods run in order; Then the expected rows exist; Given a failing store; When On* runs; Then no panic and the turn path is unaffected |
| T2 | Loop seam, flag-gated: when the writer is installed, the step executor calls OnRunStart before the first step, OnStepStart/OnStepComplete around each step (result blob = the step's serialized result), OnRunClose at the terminal state; when nil (flag off or store failed), zero calls and byte-identical legacy behaviour | Modify: internal/agent/loop.go (or the step-execution seam in internal/daemon where steps execute - pin the exact site after reading the call path), internal/daemon/components.go (store + writer construction/wiring); Test: internal/agent/loop_turnjournal_test.go | `go test -p 2 ./internal/agent/ ./internal/daemon/ -run TestLoopJournalSeam -v` | seam does not exist | with a fake store: one run_started, N step_started + N step_completed, one run_closed per turn; with nil writer: rows table untouched and the turn result identical to pre-change behaviour | Given a turn with 3 steps and a wired fake store; When the turn runs; Then the store shows the exact row sequence; Given writer nil; When the turn runs; Then zero rows and unchanged output |
| T3 | Skip seam: PlanResume consumes CompletedSteps and returns skip/next; the step executor, when a resume plan exists, skips completed steps (restoring their journaled result into the step record) and executes from `next` | Create: internal/agent/turnjournal/replay.go; Modify: the same step-execution seam as T2; Test: internal/agent/turnjournal/replay_test.go + internal/agent/loop_turnjournal_test.go (extend) | `go test -p 2 ./internal/agent/turnjournal/ ./internal/agent/ -run TestPlanResume -v` | PlanResume does not exist | unit: steps 0,1 completed -> skip={s0,s1}, next=2; all completed -> next=len; none -> next=0, empty skip. Seam: resumed turn with skip set executes only remaining steps and the restored results appear in the task record | Given journaled steps 0-1 of 3; When PlanResume + resume run; Then steps 0-1 are NOT re-executed (executor call count == 1) and their recorded results are restored; execution starts at step 2 |
| T4 | Quota + park invariant on the journaled path: a *llm.QuotaResetError raised during a journaled turn (fresh or resumed) takes the EXISTING park path (TurnParker/quota resume watcher) and the journal leaves the run open (no run_closed, no new retry loop) - never a short-retry | Modify: none expected (assert existing behaviour through the seam; add a regression test only); Test: internal/agent/loop_turnjournal_test.go (quota case) | `go test -p 2 ./internal/agent/ -run TestLoopJournalQuotaPark -v` | quota case not pinned on the journaled path | with a client returning QuotaResetError mid-turn: the turn parks via the existing parker (park recorded, no journal run_closed, no step short-retry counter increment), rows for completed steps remain | Given a journaled turn hitting QuotaResetError at step 2; When the error surfaces; Then the turn parks on the existing TurnParker path and the journal still shows steps 0-1 completed with the run open |
| T5 | Daemon wiring, flag-gated: beside parks.db construction open the journal store (best-effort: failure logs Warn and disables), build the JournalWriter when both flag and store are non-nil, install on the step pipeline; flag off = no store open, no writer, one Info line | Modify: internal/daemon/components.go (construction near :2610, wiring near :3484); Test: internal/daemon/components_turnjournal_test.go | `go test -p 2 ./internal/daemon/ -run TestTurnJournalWiring -v` | wiring does not exist | flag on + store ok: c.TurnJournal non-nil and the pipeline holds a writer; flag off: c.TurnJournal nil, no turnjournal.db file created, single Info log; store open fails: nil + Warn, daemon boots normally | Given flag true; When components build; Then store+writer installed; Given flag false; When components build; Then no store, no db file, one Info line |

**Worked example (canonical row expansion for T3):**

1. Write failing test:

```go
func TestPlanResumeSkipsCompleted(t *testing.T) {
    store := newFakeStore(t) // or SQLiteJournal over t.TempDir()
    // seed: steps s0, s1 completed for turn t1
    skip, next, err := turnjournal.PlanResume(ctx, store, "t1", []turnjournal.StepRef{
        {StepID: "s0", StepIndex: 0}, {StepID: "s1", StepIndex: 1},
        {StepID: "s2", StepIndex: 2},
    })
    if err != nil { t.Fatal(err) }
    if next != 2 || len(skip) != 2 {
        t.Fatalf("next=%d skip=%v", next, skip)
    }
}
```

2. Verify failure: `go test -p 2 ./internal/agent/turnjournal/ -run TestPlanResume -v`
   Expected: FAIL - PlanResume undefined.
3. Minimal implementation: PlanResume reads CompletedSteps, builds skip +
   first-uncompleted index.
4. Verify pass: same command. Expected: PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C2 signatures, C4 seam)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify (NO rehydrate/boot path -
      that is leaf 03; NO e2e suite - leaf 04)
- [ ] Writer is best-effort everywhere: grep your diff for error returns
      from the On* path reaching a turn-failing branch (must be none)
- [ ] No second park path introduced; no agent-side completion dedup added
- [ ] gofmt clean; pure ASCII; `go vet ./internal/agent/ ./internal/daemon/` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Interface contracts match exactly (C2, C4, C6 wiring points)
- [ ] Flag-off path is byte-identical legacy behaviour (nil writer, no db file)
- [ ] Quota errors never short-retry on the journaled path (T4 test exists)
- [ ] No mutex held across journal I/O (mutexio analyzer: `make analyzers`)
- [ ] Conventions followed; no bugs; no predictable IDs; no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- The exact step-execution seam (loop.go vs the daemon step-job path) must be
  pinned by reading the call path FIRST (stepJobTurnContext at
  components.go:9036 -> Process -> RunOnce); put the seam where the step
  result is finalized, so skip/restore and journaling see the same data.
- The TurnPayload written by OnRunStart is the queued job's request payload -
  the same role turn_payload plays in ParkedTurnRecord (parked_turn.go:77).
- Do NOT touch ResetStaleClaimsAtStartup ordering or the queue package; the
  resume interlock with the queue is leaf 03's contract (C3).
