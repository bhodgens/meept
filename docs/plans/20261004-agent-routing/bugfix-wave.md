# Bug Fix Wave - 2026-10-04 Routing Review Findings

STATUS (2026-10-05): COMPLETE, committed.
- fix(queue) commit: B1 (claim paging past parked head-of-line via
  Store.ListByStateAfter keyset pagination + 500-row bound), B2 queue side
  (Store.Complete state guard + ErrJobNotClaimable, no publish on stale),
  B4 (retryBackoffCap 30s).
- fix(agent) commit: B2 tactical side (OnJobCompleted drops completions
  whose job is no longer claimed/processing/completed — completed is FRESH
  because the queue sets it before publishing), B3 (blockedBy log fix).
- Orchestrator correction during review: the leaf's step-state guard shape
  would have dropped EVERY legitimate completion (job is already
  'completed' when the event arrives); fixed to discriminate on job state
  with completed treated as fresh, and the control test now mirrors the
  production Complete-then-publish sequence. Caught by the task-queue e2e
  timing out; unit suite alone was blind to it.

Parallel fix wave for the four bugs found in the 2026-10-04 agent-routing
review. These run as fixer subagents in the SAME worktree as the
20261004-agent-routing improvement tree — file ownership below is
EXCLUSIVE and the leaves of that tree are forbidden from these regions.

## Findings

### B1. Claim slow-path 50-row window starvation (HIGH)

- **Where:** internal/queue/queue.go:204-241 (Claim slow path), using
  internal/queue/store.go:805 ListByState LIMIT 50.
- **Symptom:** The task-cancel callback is ALWAYS installed in production
  (internal/daemon/components.go:2231), so every Claim takes the slow path.
  The in-memory filter skips future-due_at / future-next_retry_at /
  other-agent / cancelled jobs, but only within the first 50 pending rows.
  50 head-of-line parked jobs (quota-deferred with future next_retry_at,
  interactive-stamped sort FIRST) permanently hide claimable jobs beyond
  row 50; workers spin ErrNoJobAvailable while work sits claimable.
- **Fix contract:** Page past the window: if no claimable job found in the
  first 50, continue fetching subsequent batches (ListByState offset or a
  new store method `ListByStateAfter(state, limit, lastCreatedAt, lastID)`)
  until a claimable job is found or rows exhaust. Bound total scan (e.g.
  500 rows) and log at Debug when a full bounded scan found nothing. Keep
  the cancel-callback check per candidate. Add a test: 60 parked jobs
  (future next_retry_at, mixed) + 1 claimable at the tail → Claim returns
  the tail job.
- **Files owned:** internal/queue/store.go (NEW list method allowed),
  internal/queue/queue.go (Claim slow path), internal/queue/*_test.go.
  CONFLICT NOTE: leaf 01 of the improvement tree owns queue.go Enqueue/
  struct regions (wake plumbing) — B1 owns ONLY the Claim function body
  (lines ~204-245). Do not touch Enqueue or the struct.

### B2. Missing stale-completion state guard in OnJobCompleted (MEDIUM)

- **Where:** internal/agent/tactical.go:1205 (OnJobCompleted entry) and
  internal/queue/store.go:485 (Store.Complete unconditional UPDATE).
- **Symptom:** Jobs keep the same ID across Retry/Requeue. A stale or
  duplicate queue.job.completed event for a job whose step was requeued
  (state StepScheduled for attempt 2) is processed as a fresh completion:
  attempt-1 result stored, CompletedJobs double-incremented, promote/
  schedule run from the wrong state. The rest of the file guards
  terminalization paths ("never flip a terminal task's state"); this
  entry point has no guard.
- **Fix contract (two layers):**
  1. store.Complete gains a state guard: `WHERE id = ? AND state IN
     ('claimed','processing')` — a completed/pending/failed job cannot be
     re-completed; RowsAffected==0 → return a sentinel error
     (ErrJobNotClaimable or similar) and DO NOT publish queue.job.completed.
     Check callers of PersistentQueue.Complete for behavior on error
     (worker logs and returns — acceptable).
  2. OnJobCompleted entry: if the step's current state is StepScheduled or
     StepReady (i.e., requeued for another attempt), log a Warn
     ("stale completion event for requeued job") and return nil WITHOUT
     processing. States StepRunning/StepClaimed etc. proceed normally.
  Add tests for both layers (queue-level: complete twice → second errors;
  tactical-level: stale event with step in StepScheduled → no counter
  change, no promotion).
- **Files owned:** internal/queue/store.go (Complete), internal/queue
  tests, internal/agent/tactical.go (OnJobCompleted ENTRY ONLY — first
  ~15 lines). CONFLICT NOTE: successors-hints leaf owns OnJobCompleted's
  evidence-tail region (~1275-1345); you own the entry. Read current file
  state before editing; siblings may have landed adjacent edits.

### B3. Cascade log prints wrong blocking dependency (LOW)

- **Where:** internal/agent/tactical.go:2854 (failBlockedDependents log line).
- **Symptom:** `"failed_dep", failedStepID` prints the ORIGINAL failed
  step at every cascade level, not the step that actually blocked s.
- **Fix contract:** Track the actual blocking dep inside the dep loop
  (`blockedBy := ""` set when failedIDs[dep] hits) and log
  `"failed_dep", blockedBy`. Add/adjust a test asserting the log field
  (or refactor the log into the test-visible shape per file conventions —
  prefer a unit test calling failBlockedDependents on a 3-level chain and
  capturing logs via slog test handler if the package already does this
  somewhere; otherwise assert via a simpler observable).
- **Files owned:** internal/agent/tactical.go (failBlockedDependents
  function ONLY) + test. No conflicts with improvement-tree leaves.

### B4. Rate-limit retry backoff caps at 8s (LOW)

- **Where:** internal/queue/store.go:645-647 (Store.Retry backoff).
- **Symptom:** `2s * 2^retryCount` capped at 8s hammers a provider on
  429s that miss the quota-class regex.
- **Fix contract:** Raise the cap to 30s (`min(base*2^n, 30s)`). Keep
  base at 2s. Adjust any test pinning the 8s cap; grep `8\*time.Second`
  / `8 * time.Second` in internal/queue tests first. If a config surface
  for the cap already exists, prefer wiring it; otherwise hardcode 30s
  with a comment.
- **Files owned:** internal/queue/store.go (Retry only), internal/queue tests.

## Execution Order and Ownership Map

B1 and B4 both touch internal/queue/store.go (DISJOINT functions:
ListByState/Claim-path vs Retry) — parallel allowed with explicit
function ownership. B2 touches store.go Complete + tactical.go entry —
parallel allowed with the region notes above. B3 touches tactical.go
failBlockedDependents — parallel allowed.

Dispatch all four in one wave with the region-ownership lines verbatim.
Serialization fallback if hooks/collisions bite: B3 → B4 → B1 → B2.

## Verification Gate (orchestrator)

1. go build ./...
2. go test -p 2 -short -count=1 ./internal/queue/... ./internal/agent/...
3. gofmt -l on touched files
4. Commit per finding with message `fix(queue|agent): <finding>` +
   finding ID in the body.
