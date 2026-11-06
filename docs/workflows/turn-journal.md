# Turn-Level Durable Journal

`internal/agent/turnjournal` is meept's turn-scoped durable-execution ledger:
an append-only SQLite journal of step boundaries per turn, so a daemon killed
mid-turn (`kill -9`) resumes at the **next unexecuted step** instead of
re-running the job from step 1. Plan tree: `docs/plans/20261031-turn-journal/`;
research findings: `docs/research/turn-journal-findings.md` (the DBOS/
Persistasaurus DB-embedded family over the external-workflow-server family —
the daemon is a single-node process over local SQLite stores, so Temporal/
Restate is the wrong shape).

## Problem

Without the journal, `ResetStaleClaimsAtStartup` (internal/daemon/daemon.go)
re-queues a dead daemon's in-flight job and the worker re-runs it from step 1,
re-invoking the LLM for every already-completed step: cost, latency, and
divergence risk on a non-deterministic model. The journal records what already
happened so the resume path can skip it.

## What gets journaled

A dedicated SQLite store over `<DataDir>/turnjournal.db`, constructed beside
`parks.db` with the same best-effort posture (internal/daemon/components.go —
own WAL handle, own file, Warn on open failure, journal disabled when nil).
DSN pragmas: `journal_mode(WAL)`, `busy_timeout(5000)`,
`synchronous(NORMAL)`.

Table `turn_journal` rows are one of four kinds per turn+step:

| Kind | Written when |
|------|--------------|
| `run_started` | the turn's first step starts (carries the class payload) |
| `step_started` | each step boundary begins |
| `step_completed` | each step finishes (with the opaque JSON result blob) |
| `run_closed` | the job completes (terminal write) |

The writer (`writer.go`) is wired at agent-loop step boundaries through the
`agent.NewTurnJournalHooks` seam (`internal/agent/turn_journal_hooks.go`);
every method is best-effort — logged at Warn, op-timeout bounded, it never
blocks or fails a turn. A completed job closes its rows; an interrupted job
leaves a dangling open run.

**Quarantine of side effects (finding F4):** the journal records step
boundaries; it does not replay LLM outputs into new executions. A step whose
`step_completed` row exists is SKIPPED — its recorded result restores the
step's transcript state only, and the remaining work executes live. The
skip/restore seam is `PlanResume` (`replay.go`), consulted by the executor via
`TurnJournalHooks.PlanResume` before dispatching each step.

## Config

`daemon { turn_journal_enabled: false }` — `TurnJournalEnabled` in
`internal/config/schema.go:857`. Opt-in, default **false**
(docs/agents/opt-in-defaults.md: flipping a default is a product decision).
When false the store is never opened, no writer exists, no rows are written,
boot logs one Info line, and the rehydrate pass is skipped — legacy behaviour
is byte-identical.

```json5
"daemon": {
  "turn_journal_enabled": false, // durable step ledger over <DataDir>/turnjournal.db
}
```

## Boot rehydrate and kill -9 resume semantics

On boot, AFTER the queue's `ResetStaleClaimsAtStartup` and before
`status=running`, `Daemon.rehydrateTurnJournal` (internal/daemon/daemon.go)
scans open journal runs (`OpenRuns`) and requeues each interrupted turn
through the EXISTING worker path (`Rehydrate` in `rehydrate.go`). It never
executes turns itself and never creates a second park path. The resume
sequence after `kill -9` mid-turn:

1. The claim reset re-queues (or the rehydrate pass resets) the original job.
2. The worker picks the turn up; `PlanResume` consults the journal and the
   executor skips steps whose `step_completed` row exists, restoring their
   journaled payloads without any LLM re-invocation.
3. Execution continues at the first step with no journal record.
4. On completion the run closes (`run_closed`); a boot-time retention sweep
   (`PruneClosedRuns`, 7-day window) prunes closed runs.

Quota errors during a replayed turn PARK via the existing TurnParker
(`agent.quota_wait` surface) — never short-retry (internal/llm/AGENTS.md).

## Replay dedup interlock

Two reuse-only invariants bound the design — no second mechanism anywhere:

- **Original job IDs.** The rehydrate requeue looks up the turn's ORIGINAL
  queue job by task ID and resets it to pending; it never enqueues a new row,
  because `Enqueue` would mint a new job ID and break the dedup. Jobs keep
  their ID across requeue/claim — that IS the interlock.
- **Queue-owned completion dedup.** Terminal completion dedup stays on the
  queue's `CompleteAttempt`/`CompletionIsFresh` claim tokens
  (internal/queue/queue.go). Nothing in `internal/agent` or `turnjournal`
  re-implements it. A resumed turn completing twice cannot double-publish.

## Tests

Hermetic e2e suite `e2e/suites/turn-journal/` (manifest entry in
`e2e/manifest.json`; `make e2e-fast-area AREA=turn-journal`), the tree's
acceptance gate. Four scenarios: true SIGKILL mid-turn → restart over the
SAME data dir → resume at the next step with a fake-LLM call counter proving
no re-invocation; no-duplicate-completion; quota-park-on-replay; and a
flag-off legacy control (full job re-run, `turnjournal.db` never created).
Package unit tests live in `internal/agent/turnjournal/`.

## Known gap: RecoverStaleTasks force-fails the journaled in-flight step

Recorded in the e2e manifest suite note and observed by the kill-9 scenario:
boot-time `RecoverStaleTasks` force-fails every non-terminal in-flight step
BEFORE the reclaim re-runs the turn. The WORK completes correctly after
resume, but the resumed turn's task ROW can read `failed` — a status/bookkeep
ing divergence between the task store and the journaled execution. Related
shape: the writer keys `run_started`/`run_closed` per step-job round, so a
multi-round turn carries multiple close rows and `OpenRuns()` can miss the
parked round (the queue reclaim is the working resume path in that shape).
Tracked as a GitHub issue against this page; the fix belongs at the source —
either RecoverStaleTasks learns about journaled in-flight steps, or the
journal round-keying collapses to one run per turn.

## See also

- `docs/plans/20261031-turn-journal/master.md` — contracts C1-C6 and the
  completion table (issue #80 tree)
- `docs/research/turn-journal-findings.md` — durable-execution findings F1-F6
- `docs/workflows/job-scheduling.md` — the PersistentQueue the journal reuses
- `docs/agents/opt-in-defaults.md` — why the flag defaults false
