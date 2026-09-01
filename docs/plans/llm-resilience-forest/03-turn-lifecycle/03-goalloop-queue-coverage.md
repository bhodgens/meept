# Goal-Loop + Queue Turn Coverage - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Park/resume coverage for goal-loop assessment/reflect calls
  and queue-job execution on quota + throttle waits.
- **Dependencies:** 01-generalize-parker.md
- **Estimated Context:** 75K
- **Concurrency Group:** B (parallel with 02 — disjoint files)
- **Decision references:** D9

## Goal

Two turn types still die or hang on provider waits:

1. **Goal loop** (internal/employee/goal_loop.go): the reflector's Chat
   calls (assessment ~line 633, reflect ~line 1049) return
   QuotaResetError / ErrAllModelsQuotaBlocked / ThrottleBackoffError and
   the episode errors out. D9: the EPISODE parks and resumes.
2. **Queue jobs** (internal/queue/): a job executing an agent turn that
   hits a provider wait currently exhausts job retries against a
   hours-long provider window. D9: the job PARKS (releases the worker
   slot) and requeues at the class schedule; the job's OWN retry counter
   is not consumed by provider waits (a provider wait is not a job
   failure — it is the machine being patient).

Coverage matrix (both sites, both classes):
| Site | QuotaResetError / ErrAllModelsQuotaBlocked | ThrottleBackoffError |
|------|--------------------------------------------|----------------------|
| goal_loop reflector | park quota (resetAt) | park throttle (plan) |
| queue worker execution | requeue at resetAt (no retry-count) | requeue at plan time (no retry-count) |

Give-up (park/requeue impossible — beyond MaxWait/horizon): the existing
error propagates UNCHANGED so the site's own failure policy applies
(goal episode failure handling; queue dead-letter).

## Context

Key files:
- `internal/employee/goal_loop.go` — reflector.Chat call sites (633,
  1049); the Reflector interface (line ~198-200, satisfied by llm.Chatter)
  allows a stub — tests use it. Episode/scheduler state around those calls
  determines where a
  "parked" marker lives (investigate the goal-loop state model before
  writing tests; the goal system may already have a paused/deferred
  episode concept — REUSE it rather than inventing a parallel one).
- `internal/queue/store.go` — ClaimNextForAgent (line 351), requeue/dead_letter
  paths (dead-letter around line 799-883); job retry counters
  (retry_count/max_retries). NOTE: requeue scheduling rides the EXISTING
  `next_retry_at` column — the claim query already filters
  `next_retry_at <= now` (store.go:375), so the leaf should set
  next_retry_at (NOT a new NotBefore column; note the naming mismatch
  with the contract text in Deviations if it matters).
- `internal/agent/parked_turn.go` — TurnParker (leaf 01). The queue
  path may prefer REQUEUE-over-park (durable by construction) — that is
  the intended design: queue jobs requeue; non-queue turns park.
  CROSS-TREE NOTE (SHARED-CONVENTIONS §4.4, audit 2026-09-01): tree 04
  adds an `interactive` column to jobs (its leaf 02). When THIS leaf
  adds requeue/UPDATE paths, include `interactive` in the column lists
  so a requeued job does not silently lose its interactive flag; also
  note that tree 04 edits store.go claim queries while this leaf edits
  its requeue paths — same file, disjoint statements (forest root
  treats 04 and 03 as independent; keep it that way by not touching
  each other's queries).
- Bus topics for goal-loop/job lifecycle — reuse existing lifecycle
  topics if present; do not add new topics without updating
  `make graphs` + AGENTS.md.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 3 (coverage parity) plus concrete seams:

```go
// internal/employee/goal_loop.go
// parkGoalEpisodeOnProviderWait: on classified provider wait, marks the
// episode parked (existing episode-state mechanism) and registers a
// TurnParker resume that re-triggers the episode's pending phase.
// Returns (parked bool, err error) — err non-nil ONLY on give-up.

// internal/queue/worker execution path (locate the job-execution error
// handling; grep the worker package for retry handling):
// requeueOnProviderWait: on classified wait, requeues with NotBefore =
// schedule time, WITHOUT incrementing retry_count. Give-up → existing
// failure path (dead-letter semantics untouched).
```

### What This Leaf Consumes

```go
// From 01: TurnParker
// From tree 02: FailureClass classification helpers (IsQuotaResetError,
// AsThrottleBackoffError accessors), BackoffPlan/DefaultBackoffPlan
```

## Tasks

### Task 1: Goal-loop coverage

**Objective:** Episodes park + resume on provider waits.

**Files:**
- Modify: `internal/employee/goal_loop.go` (reflector error paths)
- Modify: the goal-loop scheduler/enforcement seam where episodes re-arm
  (locate; likely scheduler_jobs.go or enforcement.go — investigate
  FIRST, do not guess)
- Test: `internal/employee/goal_loop_park_test.go` (stub the Reflector
  interface — goal_loop.go:197-200, satisfied by llm.Chatter — returning
  QuotaResetError, then ThrottleBackoffError, then success)

**Step 1:** Failing tests: quota wait → episode marked parked (existing
state enum value or documented new one), no error surfaces, resume
re-enters the same phase on clock advance; throttle wait → same with
plan schedule; give-up (MaxWait exceeded) → existing episode error
behavior byte-identical. **Step 2:** FAIL. **Step 3:** Implement.
**Step 4:** PASS + `go test ./internal/employee/... -count=1`.

### Task 2: Queue coverage

**Objective:** Provider waits requeue without consuming retries.

**Files:**
- Modify: the queue worker execution error path (internal/queue/ worker
  file — locate via search_files on retry_count handling)
- Test: `internal/queue/store_park_test.go` + worker test extension

**Step 1:** Failing tests: job hits ThrottleBackoffError → requeued with
NotBefore set, retry_count UNCHANGED, worker slot released (next
ClaimNext does not return it before NotBefore); quota wait → same with
resetAt; give-up → dead-letter via the EXISTING path (assert
dead_letter row). **Step 2:** FAIL. **Step 3:** Implement (store
requeue helper if absent). **Step 4:** PASS +
`go test ./internal/queue/... -count=1`.

### Task 3: Wiring

**Objective:** One TurnParker serves goal episodes; queue uses requeue.

**Files:**
- Modify: internal/daemon/ components (goal subsystem gains the parker
  reference; queue worker gains the classifier dependency)
- Test: `go build ./...`; daemon wiring smoke (existing daemon tests).

**Verify:** no second TurnParker instance for the goal path; queue
classifier receives the SAME policy config the clients use.

## Self-Verification Checklist

- [ ] Coverage matrix complete: both sites × both classes (tested)
- [ ] Queue retries NOT consumed by provider waits (explicit test)
- [ ] Dead-letter path untouched for genuine failures
- [ ] Give-up propagates existing errors byte-identically
- [ ] Reused episode state mechanism (or documented new state in Deviations)
- [ ] gofmt/vet/analyzers clean; package suites green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contracts match master Contract 3 exactly
- [ ] No new bus topics without graphs regen + AGENTS.md line
- [ ] No inline retries added on either path

Output: APPROVED or specific gaps with file + line references.

## Notes

- If the goal loop ALREADY handles quota gracefully (an episode
  defer/pause landed with the quota plan), verify with a test first —
  the leaf then only adds the throttle class + the missed gaps, and
  documents the existing mechanism in Context instead of building one.
- Queue NotBefore: confirm the store's claim query honors a not-before
  timestamp; if it lacks one, add the column + index migration in this
  leaf (store.go owns migrations — follow its pattern) and note it.
