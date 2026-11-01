# Turn-Level Durable Journal - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 5 (R0-research.md, 01-journal-schema.md, 02-loop-integration.md, 03-rehydrate-interlock.md, 04-e2e-turn-journal.md)
- **Scope:** A turn-level durable journal (append-only step ledger) so a daemon
  killed mid-turn resumes at the NEXT STEP, not a full job re-run.

## Goal

Today, `kill -9` of the daemon mid-turn loses all in-flight turn progress: the
queue's `ResetStaleClaimsAtStartup` (internal/daemon/daemon.go:2258) re-queues
the job and the worker re-runs it from step 1, re-invoking the LLM for every
already-completed step (cost, latency, and divergence risk on a non-deterministic
model). This tree adds a turn-level durable journal: an append-only ledger of
step boundaries per turn, written at each step start/finish by the agent loop,
rehydrated at boot, and consulted by the resume path so execution continues at
the next unexecuted step.

This is the approved replacement for a rejected Temporal proposal: the daemon is
a single-node process over local SQLite stores (parks.db, effects.db, the
session store), so an external workflow server (Temporal/Restate) is the wrong
shape - research leaf R0 pins the durable-execution findings that justify the
DB-embedded journal pattern (the DBOS/Persistasaurus family) over the external
server family, and pins the determinism/quarantine-of-side-effects lessons that
shape the replay interlock.

Scope guardrails (approved):

- New package `internal/agent/turnjournal`, ~400-700 LOC.
- Journal writer integrated at agent-loop step boundaries; boot rehydrate +
  resume at daemon startup; a replay interlock that prevents duplicate side
  effects; a kill -9 e2e suite proving resume-at-next-step.
- Opt-in config flag, default FALSE (docs/agents/opt-in-defaults.md rule -
  flipping a default is a product decision).

## Architecture

Single-node, DB-embedded durable execution, scoped to ONE granularity: the TURN.
Not a general workflow engine, not a Temporal replacement in scope - just a
step ledger so a crashed turn resumes at the next step.

Control flow:

1. A turn's first step START writes `run_started` (journal row per turn+step).
2. Every step boundary writes `step_started` / `step_completed` (result blob)
   to a dedicated `turnjournal.db` SQLite store in `cfg.Daemon.DataDir` - the
   parks.db pattern (internal/daemon/components.go:2610): own WAL handle, own
   file, best-effort open with a Warn on failure, journal disabled when nil.
3. A completed job closes its journal rows (terminal write). An interrupted job
   leaves a dangling open run.
4. On boot, AFTER queue claim reset (daemon.go:2231-2256) and AFTER the client
   transports are already up, a rehydration pass scans open journal runs and
   hands them to the step pipeline: the loop skips steps whose
   `step_completed` row exists and resumes at the next step.
5. Replay interlock: skipped steps are not re-executed; terminal completion
   dedup stays on the queue's `CompleteAttempt`/`CompletionIsFresh`
   (internal/queue/queue.go:491, 539) claim tokens; quota errors during replay
   PARK via the existing TurnParker (internal/agent/parked_turn.go:98-167) -
   never short-retry (internal/llm/AGENTS.md).

Two invariants bound the whole design:

- **Reuse, never a second mechanism:** parks via TurnParker/ParkKind, dedup via
  queue CompleteAttempt/CompletionIsFresh, boot ordering via the existing
  daemon.go seams. No second park path, no agent-side dedup reimplementation.
- **Quarantine of side effects (R0 finding F4):** the journal records step
  boundaries; it does not replay LLM outputs into new executions. A step whose
  result row exists is SKIPPED, never "replayed from its recorded output" -
  recorded results restore the step's transcript state only, and the remaining
  work executes live.

## Interface Contracts

<!-- Each contract: ID + ONE signature block. Owning + consuming leaves.
     Leaves list consumed contract IDs + exact signatures only - never
     restate full definitions in a leaf; the parent holds them once. -->

### C1: Journal Record and Store

```
// Package turnjournal, file internal/agent/turnjournal/journal.go
type RecordKind string // "run_started" | "step_started" | "step_completed" | "run_closed"

type Record struct {
    TurnID         string    // task/turn identifier (task ID from the queue)
    SessionID      string
    StepID         string    // stable step identifier from the task's step list
    StepIndex      int
    Kind           RecordKind
    ResultPayload  []byte    // step_completed only; opaque JSON, nil otherwise
    Timestamp      time.Time
}

type SQLiteJournal struct{ ... } // implements JournalStore

func NewSQLiteJournal(dbPath string, logger *slog.Logger) (*SQLiteJournal, error)
func (j *SQLiteJournal) Append(ctx context.Context, rec Record) error
func (j *SQLiteJournal) OpenRuns(ctx context.Context) ([]OpenRun, error)
func (j *SQLiteJournal) CompletedSteps(ctx context.Context, turnID string) (map[string][]byte, error)
func (j *SQLiteJournal) CloseRun(ctx context.Context, turnID string) error
func (j *SQLiteJournal) Close() error

type OpenRun struct {
    TurnID    string
    SessionID string
    TurnPayload []byte // the class payload persisted at run start
}
// Owner: 01-journal-schema.md
// Consumers: 02-loop-integration.md, 03-rehydrate-interlock.md
```

Schema: table `turn_journal` with columns
`(id INTEGER PRIMARY KEY AUTOINCREMENT, turn_id TEXT NOT NULL, session_id TEXT NOT NULL, step_id TEXT NOT NULL, step_index INTEGER NOT NULL, kind TEXT NOT NULL, result_payload BLOB, created_at INTEGER NOT NULL)`,
index on `(turn_id, step_id, kind)`, `result_payload` CHECK `(kind <> 'step_completed' OR result_payload IS NOT NULL)`.
DB path `<DataDir>/turnjournal.db`, DSN
`?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)`
(modernc.org/sqlite `_pragma=` form - see internal/agent/park_store_sqlite.go:121-129).
Follow parks.db conventions: own file, own handle, best-effort open at wiring
with a Warn on failure, journal disabled (nil) when the store fails to open.

### C2: JournalWriter - Loop Seam

```
// File internal/agent/turnjournal/writer.go
type JournalWriter struct{ ... }

func NewJournalWriter(store JournalStore, logger *slog.Logger) *JournalWriter
// Called by the step pipeline around each step execution:
func (w *JournalWriter) OnStepStart(ctx context.Context, turnID, sessionID, stepID string, stepIndex int)
func (w *JournalWriter) OnStepComplete(ctx context.Context, turnID, sessionID, stepID string, stepIndex int, result []byte)
func (w *JournalWriter) OnRunStart(ctx context.Context, turnID, sessionID string, turnPayload []byte)
func (w *JournalWriter) OnRunClose(ctx context.Context, turnID string)
// Never blocks or fails a turn: every method is best-effort, error-logged at
// Warn, and runs with an op timeout (park-store opTimeout pattern, 5s).
// Owner: 02-loop-integration.md
// Consumers: 03-rehydrate-interlock.md, 04-e2e-turn-journal.md
```

### C3: Resume Hook - Boot Rehydrate

```
// File internal/agent/turnjournal/rehydrate.go
// Called from internal/daemon/daemon.go Run(), AFTER ResetStaleClaimsAtStartup
// (daemon.go:2231-2256) and before/alongside status=running. It requeues
// interrupted turns onto the EXISTING worker path - it does not execute turns
// itself and does not create a second park path.
func Rehydrate(ctx context.Context, store JournalStore, requeue func(turnID, sessionID string, payload []byte) error, logger *slog.Logger) (resumed int, err error)
// Owner: 03-rehydrate-interlock.md
// Consumers: 04-e2e-turn-journal.md (asserts resumed turns continue at next step)
```

### C4: Skipped-Step Seam (task pipeline)

```
// internal/task (or the step-execution seam the leaf pins): a turn carrying
// journal metadata learns which steps are already done:
func CompletedStepIDs(turnID string) map[string]struct{} // snapshot at turn start
// The step executor consults it before dispatching a step; a completed step
// is skipped and its journaled result restored into the step record without
// re-invoking the LLM.
// Owner: 02-loop-integration.md
// Consumers: 03-rehydrate-interlock.md, 04-e2e-turn-journal.md
```

### C5: Config Flag

```
// internal/config: new opt-in field, default false
daemon.turn_journal_enabled bool // JSON5: daemon { turn_journal_enabled: false }
// When false: no store is opened, no writer exists, no rows are written,
// boot writes one Info line, and the rehydrate pass is skipped. Byte-identical
// legacy behaviour. Flipping the default is a product decision
// (docs/agents/opt-in-defaults.md rule).
// Owner: 01-journal-schema.md (field + config template docs)
// Consumers: 02-loop-integration.md, 03-boot-rehydrate.md
```

### C6: Wiring Points (frozen, from verified anchors)

```
// components.go: store construction beside parks.db (internal/daemon/components.go:2610),
// exposed as c.TurnJournal; writer wiring beside the park persistence wiring
// (internal/daemon/components.go:3484 SetParkPersistence pattern).
// daemon.go: Rehydrate call AFTER the ResetStaleClaimsAtStartup block
// (internal/daemon/daemon.go:2231-2256), before status=running.
// Owner: 02-loop-integration.md (writer) + 03-rehydrate-interlock.md (rehydrate)
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| R0 | R0-research.md | leaf (research) | none | 30K | A |
| 01 | 01-journal-schema.md | leaf | R0 (cite findings) | 70K | B |
| 02 | 02-loop-integration.md | leaf | 01, R0 | 105K | C |
| 03 | 03-rehydrate-interlock.md | leaf | 01, 02, R0 | 95K | D |
| 04 | 04-e2e-turn-journal.md | leaf | 01, 02, 03 | 90K | E |

No two children share a concurrency group: every leaf depends on its
predecessor's store or seam. R0 runs first and alone; 01 follows; 02 and 03
are serialized by the C3 wiring overlap in daemon.go/components.go; 04 gates
the tree.

## Dispatch Protocol

Research-gated tree: R0 pins the durable-execution findings BEFORE any code
leaf dispatches; every code leaf's brief cites R0. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 1: R0 (concurrency group A)

Dispatch R0-research.md via `delegate_task`: goal = "Produce the pinned
research findings doc per R0-research.md"; context = full leaf text +
conventions + the no-commit rule. On return, review in-session: findings doc
exists at `docs/research/turn-journal-findings.md`, has F1-F6 + D1-D3 with
sources, and the scope guardrails match this master's Architecture. Then
commit R0's output, set status REVIEWED.

### Phase 2: 01 (concurrency group B)

Dispatch after R0 REVIEWED. Context includes contract C1/C5 verbatim and the
R0 findings URL/path to cite in code comments where a finding drove a decision.

### Phase 3: 02 (concurrency group C)

Dispatch after 01 REVIEWED. Context includes C1/C2/C4/C5/C6 verbatim.

### Phase 4: 03 (concurrency group D)

Dispatch 03-rehydrate-interlock.md after 02 REVIEWED (the rehydrate wiring
shares components.go/daemon.go with 02's wiring; serialize to avoid
stale-read conflicts).

### Phase 5: 04 (concurrency group E)

Dispatch 04-e2e-turn-journal.md after 03 REVIEWED. Context includes C1-C6
verbatim and the R0 doc path. The kill -9 mid-turn test is the tree's
acceptance gate.

### Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check the
leaf spec, contracts, Review Checklist; run the leaf's verify commands; the
invariant checks below are automatic NEEDS_FIXES:

- Any new park path (second TurnParker-like mechanism) or agent-side
  completion dedup not going through queue CompleteAttempt/CompletionIsFresh.
- Any QuotaResetError short-retry path in the journal/resume code.
- Any client transport moved below the F-D8 gate, or rehydrate running before
  the queue claim reset.
- Journal writes failing/blocking a turn (best-effort contract violated).
- Config flag not default-false, or journal active when the flag is false.

Pass: commit + status REVIEWED. Gaps: re-dispatch with findings, max 3
cycles, then escalate.

### Phase 6: Integration Review

After ALL children REVIEWED: verify contracts between children end-to-end,
run the full suite (`go test -p 2 ./...` short mode + `make e2e-fast`),
check no import cycles. Gaps: re-dispatch the failing child with contract
fixes (uncommitted). Pass: normalize formatting, run the line-number
corruption check, commit integration changes, set children COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders, commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Invariant checks (Dispatch Protocol) all pass
- [ ] Nothing is a stub: every feature wires to a real surface (store opened
      at wiring, writer called from the loop, rehydrate called from Run,
      e2e suite in the manifest)

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go (module root; gofmt before reporting).
- **ASCII only:** no em-dashes, arrows, or unicode in code or comments -
  pre-commit gate.
- **Naming:** exported for the store/writer/rehydrate API, unexported internals;
  package `turnjournal` under `internal/agent/turnjournal/`.
- **Imports:** stdlib, then module-internal; no unused imports.
- **Error handling:** wrap with `%w` and package prefix (`turnjournal: ...`);
  writer methods are best-effort (log Warn, never propagate to the turn).
- **SQLite:** `_pragma=` DSN form; WAL + busy_timeout(5000) +
  synchronous(NORMAL); op timeout context on every statement
  (park_store_sqlite.go:155 opTimeout pattern).
- **ID generation:** never `time.Now().UnixNano()`/`math/rand` (pkg/id.Generate
  or stable turn/step IDs already in scope).
- **Testing:** new feature tests in the hermetic e2e tier
  (`e2e/suites/turn-journal/` + `e2e/manifest.json` path_map entry in the same
  commit); unit tests within the new package are fine. Go tests use `-p 2`.
- **Do NOT commit / do NOT git add:** every implementation agent reports
  results only; the orchestrator commits after review.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | "append-only step ledger so a daemon kill mid-turn resumes at the NEXT STEP, not a full job re-run" | C1 store + C2 writer + C4 skipped-step seam; 04 T3 | e2e turn-journal kill -9 test |
| R2 | "New package internal/agent/turnjournal (~400-700 LOC): schema, journal writer at loop step boundaries, boot rehydrate/resume, replay interlock" | 01 (schema/store), 02 (writer + seam), 03 (rehydrate + interlock) | leaf verify commands + review |
| R3 | "DBOS/Restate/DB-backed workflow journal patterns, why Temporal SDK is rejected for single-node SQLite daemons; determinism/quarantine-of-side-effects lessons" | R0 T2, findings F1-F6/D1-D3 | R0 review in Phase 1 |
| R4 | "QuotaResetError mid-replay must PARK (quota_resume path), never short-retry" | 02 T4; R0 finding F3 | 02 unit test + review grep |
| R5 | "reuse TurnParker/ParkKind scoping - never a second park path" | Architecture invariant; review checklist | review NEEDS_FIXES rule |
| R6 | "completion dedup goes through queue CompleteAttempt/CompletionIsFresh, never reimplemented in internal/agent" | Architecture invariant; 03 (rehydrate) T3 constraint | review + 03 test |
| R7 | "client transports still start BEFORE the F-D8 gate" | 03 (rehydrate) T2 constraint (rehydrate after claim reset, transports untouched) | 03 review + e2e |
| R8 | "opt-in flag default FALSE" | C5; 01 T5 | 01 unit test (default false) |
| R9 | "new feature tests in hermetic e2e tier (e2e/suites/<name>/ + e2e/manifest.json path_map entry in same commit)" | 04 T1-T4 | manifest entry present; make e2e-fast |
| R10 | "FIRST leaf is a RESEARCH leaf ... producing a pinned findings doc that later leaves CITE" | R0; leaf Briefs cite docs/research/turn-journal-findings.md | Phase 1 review |

## Completion Tracking Table

<!-- Single source of truth for subtree status; parent reads this. -->

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| R0-research.md | PENDING | 0 | |
| 01-journal-schema.md | PENDING | 0 | |
| 02-loop-integration.md | PENDING | 0 | |
| 03-rehydrate-interlock.md | PENDING | 0 | |
| 04-e2e-turn-journal.md | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. Full unit suite: `go test -p 2 ./internal/agent/turnjournal/... ./internal/daemon/... ./internal/config/...`
   - Expected: all PASS, including the new package tests and the default-false
     config test.
2. Hermetic e2e: `make e2e-fast-area AREA=turn-journal`
   - Expected: 4/4 PASS including the kill -9 mid-turn test asserting
     resume-at-next-step and no duplicate side effects.
3. Flag-off regression: the e2e suite's flag-off scenario asserts a full job
   re-run (legacy behaviour byte-identical) and no turnjournal.db rows.
4. Cross-boundary contract checks:
   - kill -9 between step k and k+1, restart: resumed turn's steps 1..k come
     from the journal (no LLM re-invocation - asserted via the fake-LLM call
     counter), step k+1 executes live.
   - a resumed turn hitting QuotaResetError parks on the existing TurnParker
     (quota_resume path) and no short-retry fires.
   - a resumed turn completing twice cannot double-publish:
     CompleteAttempt/CompletionIsFresh reject the stale attempt.
5. CI gates: `make e2e-fast` (suite member), `make graphs` if any bus/RPC
   surface changed (expected: none), `make lint-ci`.

## Test Gate

This tree uses per-leaf TDD (each leaf writes its tests first within its own
tasks) plus leaf 04 as the tree-level e2e gate; there are no separate 00T-
test-gate leaves. Coverage matrix (validated by leaf 04 before the tree is
COMPLETE):

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | journal schema + store CRUD (C1) | 01-journal-schema.md T1-T3 | turnjournal package tests | e2e kill -9 resume test | PENDING |
| F2 | flag off = legacy behaviour (C5) | 01-journal-schema.md T5 | config default-false test | e2e flag-off scenario | PENDING |
| F3 | writer at step boundaries, best-effort (C2) | 02-loop-integration.md T1-T3 | loop seam tests | e2e kill -9 resume test | PENDING |
| F4 | skipped-step restore without LLM re-invocation (C4) | 02-loop-integration.md T4 | seam unit test | e2e call-counter assertion | PENDING |
| F5 | boot rehydrate after claim reset (C3) | 03-rehydrate-interlock.md T1-T2 | rehydrate unit test | e2e restart test | PENDING |
| F6 | quota parks on replay (R4) | 02-loop-integration.md T4 | quota park test | e2e quota-park scenario | PENDING |
| F7 | no duplicate side effects on resume (R1/R6) | 03 T3 + 04 T3 | dedup seam test | e2e duplicate-effect assertion | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda (dated, appended to the dispatch brief). Never edit a
dispatched leaf document mid-execution.

## Notes

- Verified anchors (grep-confirmed 2026-10-31; re-verify if a leaf finds them
  drifted): TurnParker internal/agent/parked_turn.go:98-167; ParkedTurnRecord
  :71; SQLiteParkStore over parks.db internal/agent/park_store_sqlite.go:129;
  park store construction internal/daemon/components.go:2610; SetParkPersistence
  :3484; reArm internal/agent/parked_turn.go:606-645; PersistentQueue
  internal/queue/queue.go:131; CompleteAttempt :491; CompletionIsFresh :539;
  ResetStaleClaimsAtStartup :728, called daemon.go:2231-2256; GoalLoop state
  machine internal/employee/goal_loop.go:4,646; boot-window/ClassifierBootReadiness
  internal/daemon/classifier_boot_gate.go (type at
  internal/agent/classifier_boot_readiness.go:57, wired daemon.go:2035-2038).
- Leaf 02 and leaf 03 both touch internal/daemon/components.go and daemon.go:
  serialize (concurrency groups C, D), never dispatch in parallel.
- The journal is TURN-scoped by design. GoalLoop (internal/employee) and other
  runtimes are OUT of scope for this tree; C1's turn_id is the task/turn ID the
  queue already carries, so a future employee-scoped journal reuses the same
  package without schema change.
- Kill -9 e2e hygiene: the harness already reaps scratch daemons on abnormal
  exits (e2e/harness/daemon.go:429); leaf 04 must use the standard harness
  Start/Stop path plus an explicit SIGKILL step, never a raw exec.Command.
