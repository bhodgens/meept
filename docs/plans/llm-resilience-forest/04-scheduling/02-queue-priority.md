# Interactive-First Queue Ordering - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Job.Interactive column + claim ordering (interactive DESC,
  priority DESC, created_at ASC) + enqueue stamping from the session
  signal.
- **Dependencies:** 01-session-signal.md
- **Estimated Context:** 70K
- **Concurrency Group:** B
- **Decision references:** D11, Q1

## Goal

An interactive job beats older, higher-priority background jobs.
Three seams:

1. **Schema:** `interactive` INTEGER DEFAULT 0 column on jobs + the
   claim-covering index. Job struct gains `Interactive bool` (json
   omitempty). Migration follows store.go's existing migration style
   (grep `ALTER TABLE jobs` / CREATE INDEX for the pattern; new-file
   migration if the store versions them).
2. **Claim ordering:** every pending-claim query (store.go:379, 390,
   624, 684 — the four ORDER BY sites) gains `interactive DESC` as the
   FIRST term. PRECISION (audit 2026-09-01): site 379 is
   `ORDER BY CASE WHEN agent_id = ? THEN 0 ELSE 1 END, priority DESC,
   created_at ASC` — an agent-affinity CASE precedes priority there.
   The composite becomes
   `ORDER BY interactive DESC, CASE WHEN agent_id = ? THEN 0 ELSE 1 END,
   priority DESC, created_at ASC` (interactive leads affinity —
   D11's user-first intent; NOTE this means another agent's interactive
   job can beat a job targeted at THIS agent — call this out in the docs
   task). Sites 390/624/684 become
   `ORDER BY interactive DESC, priority DESC, created_at ASC`.
   Add a regression test that affinity still precedes priority among
   same-interactivity jobs. The stats query at store.go:734 optionally
   gains interactive counts (nice-to-have; do it if trivial).
3. **Stamping:** the enqueue paths that carry an originating session
   stamp Interactive = session.IsInteractive(originSession, now,
   cfg.InteractiveWindow). AUDIT FACT (2026-09-01) — the real producers:
   `queue.PersistentQueue.Enqueue` (queue/queue.go:147),
   `services/queue_service.go:33 QueueService.Enqueue` (the ONLY site
   whose request carries session_id today, :50-52), and
   `internal/agent/tactical.go:310,532` (planner/analyst job creation —
   NOTE: `StepJobPayload` (tactical.go:32-40) has NO SessionID field, so
   stamping there requires FIRST adding session provenance to the
   payload/step store; do that as part of Task 3 or those sites stamp
   false by construction). `EnqueueJob` does not exist; `SubmitJob`
   exists only in internal/cluster (different mechanism);
   `internal/queue/dispatcher.go` is a worker-notify loop for CLAIMED
   jobs — it enqueues nothing. Enqueue sites WITHOUT a session
   (scheduler, system jobs) stamp false.

## Context

Key files:
- `internal/queue/store.go` — schema DDL (lines 63-77), ClaimNextForAgent
  (line 351) and its query variants, insert (line 301), stats (line 734).
- `internal/queue/job.go:44-67` — Job struct; WithPriority pattern for
  a WithInteractive builder if call sites construct jobs fluently.
- Enqueue call sites: grep `NewJob(` / store insert callers across
  internal/ (dispatcher, services, scheduler).
- `internal/session/interactive.go` — IsInteractive (leaf 01).
- Config: QueueConfig.InteractiveWindow (leaf 01).

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 2:

```go
// internal/queue/job.go
Interactive bool `json:"interactive,omitempty"`
func (j *Job) WithInteractive(v bool) *Job
// store: column interactive INTEGER DEFAULT 0 (NOT NULL via migration
// backfill), index idx_jobs_claim ON jobs(state, interactive DESC,
// priority DESC, created_at) replacing/augmenting idx_jobs_state_priority
// (keep the old index if other queries use it — audit first).
// Enqueue helper (queue package or services):
//   StampInteractive(job *Job, origin *session.Session, now time.Time, window time.Duration)
```

### What This Leaf Consumes

```go
// From 01: session.IsInteractive, QueueConfig.InteractiveWindow
```

## Tasks

### Task 1: Column + migration + Job field

**Objective:** Persistence + struct.

**Files:**
- Modify: `internal/queue/store.go` (DDL + migration)
- Modify: `internal/queue/job.go`
- Test: `internal/queue/store_park_test.go` (or a new
  store_interactive_test.go — prefer new file)

**Step 1:** Failing tests: fresh DB has the column; migrated old DB
(fixtures with pre-migration schema, if the store has migration tests —
else test the migration statement directly) backfills 0; insert +
round-trip of Interactive=true. **Step 2:** FAIL. **Step 3:** Implement.
**Step 4:** PASS.

### Task 2: Claim ordering

**Objective:** Interactive first, everywhere pending jobs are claimed.

**Files:**
- Modify: `internal/queue/store.go` (all four claim queries + index use)
- Test: `internal/queue/store_interactive_test.go`

**Step 1:** Failing table tests: (a) interactive(new) vs
background(high priority, older) → interactive wins; (b) two
interactive → priority then FIFO; (c) all background → existing order
byte-identical (regression); (d) ClaimNext (no agent) and
ClaimNextForAgent both honor it; the requeue/peek variants too.
**Step 2:** FAIL. **Step 3:** Implement all four ORDER BY sites — grep
`priority DESC` to catch every query; miss none. **Step 4:** PASS +
`go test ./internal/queue/... -count=1`.

### Task 3: Enqueue stamping

**Objective:** Jobs carry the signal at creation.

**Files:**
- Modify: the enqueue call sites with an originating session (REAL
  producers, audit 2026-09-01: services/queue_service.go:33 Enqueue —
  session_id present; internal/agent/tactical.go:310,532 — StepJobPayload
  lacks SessionID, ADD it (payload + step store persistence) as part of
  this task, else stamp false; queue/queue.go:147 PersistentQueue.Enqueue
  is the low-level sink, no stamping there)
- ⚠️ AUDIT R4 PLUMBING (mandatory scope): `Job` and `StepJobPayload`
  carry NO originating session today and the worker has zero session
  context (no matches in internal/worker/worker.go). Origin for task
  jobs is recoverable only via `Task.LinkedSessions` (task.go:146,
  linked in the dispatcher's createTask); one_off jobs have none. So:
  (a) add `SessionID string` to `StepJobPayload` (tactical.go:32-40) +
  its step-store persistence; (b) in `scheduleStep`, populate it from
  TaskID→LinkedSessions where a task exists; (c) one_off/system jobs
  stamp false BY CONSTRUCTION (documented, not a gap). Without (a), the
  tactical.go sites stamp false for every job — the feature would be
  dead for exactly the planner/analyst traffic it exists for.
- Also document the SEMANTICS choice (audit R4): the flag is evaluated
  ONCE at enqueue. A job enqueued while the session is idle never
  upgrades if the user returns before the claim. This matches "work
  was user-adjacent when created" but weakens live-recency; record the
  accepted semantics in the docs task and this leaf's Deviations.
- Create: the StampInteractive helper (where the store is consumed —
  keep the queue package free of a session import if it has one;
  if queue cannot import session, put the helper in services and note
  the layering in Deviations)
- Test: the consuming package's test (services + tactical) with a stub
  session; a step-job round-trip proving SessionID survives the step
  store

**Step 1:** Failing tests: job enqueued for an interactive session →
Interactive=true; tactical step job inherits the task's linked session
(interactive → true); one_off/system jobs → false. **Step 2:** FAIL.
**Step 3:** Implement. **Step 4:** PASS.

### Task 4: Docs

**Files:**
- Modify: `docs/workflows/job-scheduling.md` (the queue workflow doc —
  there is no docs/workflows/queue.md) —
  ordering semantics + the stamped-at-enqueue lifetime rule.
- Modify: `docs/configuration/` — the knob cross-reference.

**Verify:** docs match tests (the E2E ordering table).

## Self-Verification Checklist

- [ ] All claim sites ordered interactive-first (grep-audited, none missed)
- [ ] Background-only ordering regression-identical
- [ ] Migration tested on a pre-existing DB shape
- [ ] No session import added to the queue package without Deviations note
- [ ] gofmt/vet/analyzers clean; package suites green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contract matches master Contract 2 exactly (column name, index name, ORDER BY terms)
- [ ] Stats query updated or consciously skipped (noted)
- [ ] No drive-by refactor of claim logic beyond ORDER BY

Output: APPROVED or specific gaps with file + line references.

## Notes

- SQLite DESC indexes: `CREATE INDEX ... (state, interactive DESC,
  priority DESC, created_at)` is valid in modern SQLite; verify the
  bundled version supports it (store.go establishes the SQLite
  feature baseline — match its pragmas; if unsure, order in SQL and
  let the index be (state, interactive, priority, created_at)).
- Interactive jobs jumping the line can starve background work under
  constant interactivity — acceptable per D11's intent (user first);
  note in docs that scheduler/system jobs remain fairness-bounded by
  their own retry/due mechanics.
