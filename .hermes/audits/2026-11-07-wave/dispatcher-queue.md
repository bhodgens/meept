# Dispatcher / queue / worker / turnjournal-persistence spine audit — 2026-11-07 wave

Scope: commits 0a446a21..7b510ed4 touching internal/agent/dispatcher*.go,
internal/queue/, internal/worker/, internal/agent/turnjournal/store_sqlite.go,
internal/agent/turn_journal_hooks.go — the region the 2026-11-06 agent-core
auditor time-boxed and explicitly did NOT audit (see
.hermes/audits/2026-11-06-wave/agent-core.md "Not audited" note).
HEAD: 7b510ed4ffb2374d8609ade9ce569ef0e17ba49f (branch main)
Started: 2026-11-07T00:06Z (UTC)
Method: committed state read via `git show HEAD:<path>` (dispatcher.go and
dispatcher_scope.go are sibling-staged WIP; working-tree reads not trusted).

## F-1 [MEDIUM] Rehydrate's failed-state "requeue" never requeues: ResetStaleClaimsAtStartup excludes `failed`, yet the run counts as resumed every boot
- internal/daemon/daemon.go:2524-2529 (HEAD 7b510ed4)
- Evidence: the leaf-03 requeue closure does
  ```go
  case queue.StateClaimed, queue.StateProcessing, queue.StateFailed:
      if _, err := pq.ResetStaleClaimsAtStartup(ctx, time.Now().Add(-time.Second)); err != nil {
          return fmt.Errorf("reset job to pending: %w", err)
      }
      return nil
  ```
  but the reset's UPDATE (internal/queue/store.go:1166-1168) filters `WHERE state IN ('claimed', 'processing') AND updated_at < ?` — a job in `failed` state matches nothing. The call returns (0, nil), the closure returns nil, and turnjournal/rehydrate.go:68 counts `resumed++`. A turn whose step job exhausted retries (FailAttempt -> failed) but never reached OnRunClose (components.go:10498 closes only on the success path) therefore: (a) is never reset to pending by boot recovery (daemon.go:2394 uses the same filter), (b) is re-"resumed" (counted, not requeued) on EVERY boot, (c) keeps its journal run open forever — PruneClosed only reclaims rows with a `run_closed` row (store_sqlite.go:248-255), so this open run is retained indefinitely. The queue never re-claims a `failed` job (claim SQL selects `state = 'pending'` only), so the user's turn silently never re-runs. The code's own comment (daemon.go:2521-2523, "stuck in a claim state the claim reset above could not cover (e.g. failed)") states the intent the SQL cannot deliver.
- Defect: the failed-state resume path is a no-op that reports success, silently losing the turn and leaking its journal rows across boots.
- Fix shape: add a failed-aware reset (e.g. ResetToPending's state filter extended, or a dedicated ResetFailedAtStartup) rather than routing failed through a claimed/processing-only sweep; and make the closure return an error when the reset count for THAT job is zero so Rehydrate's Warn-and-continue surfaces it instead of counting it resumed.
- Verification: quote re-read from `git show HEAD:` of daemon.go/store.go/rehydrate.go; package tests green (`go test ./internal/agent/turnjournal/ ./internal/queue/ ./internal/worker/` all ok) — no existing test pins the failed-state requeue, consistent with it never working.

## F-2 [HIGH] Cluster claim timeout is wired from the GOSSIP heartbeat (default 30s) while worker heartbeats fire every 60s — live, long-running jobs are reclaimed and double-dispatched
- internal/daemon/daemon.go:598 (`DefaultClaimTimeout: clusterCfg.Gossip.HeartbeatInterval`),
  internal/config/cluster_config.go:127 (`HeartbeatInterval: 30 * time.Second` default),
  internal/worker/worker.go:345 (`ticker := time.NewTicker(60 * time.Second)`),
  internal/queue/cluster_queue.go:167-174 (claim stamps `timeout_at = now + DefaultClaimTimeout`),
  cluster_queue.go:569-577 (sweeper interval = same value, clamped [1s,60s]),
  store_cluster_claim.go:144-152 (remote reclaim resets rows whose `timeout_at < now`).
- Evidence: the dedicated field `clusterCfg.Queue.DefaultClaimTimeout` exists with a sane 5-minute default
  (cluster_config.go:133) but the daemon wiring passes the gossip heartbeat instead — an older wiring
  (blame d41800691, 2026-06-06) that was LATENT until this range made it load-bearing:
  92a486a1 moved the worker pool onto ClusterQueue (`Pool.SetQueue`) and started `StartReclaimSweeper`,
  d4f1deec began stamping `timeout_at` on the claim path and added `ReclaimRemoteStale`. With the
  production defaults the claim deadline is now+30s, the first follow-up heartbeat lands at +60s, and
  the sweeper ticks at 30s — every job whose `Process` exceeds 30 seconds (agent turns routinely take
  minutes) is reset to pending (`reclaimJobUnlocked` -> `Store.ResetToPending`, claim_token retired)
  while its worker is still executing, then re-claimed: concurrent double execution of the same step
  job's side effects. The superseded attempt's completion is refused (H5 guard — that half works), but
  the re-execution itself is the defect. The sweeper's own comment (cluster_queue.go:547) states the
  intent: "Interval = DefaultClaimTimeout (the heartbeat cadence of live workers)" — the worker cadence
  is a hardcoded 60s unrelated to that config value, so the invariant the code documents is false at HEAD.
- Defect: on any cluster-enabled install with default config, every long-running job is stale-reclaimed
  mid-flight and double-dispatched (locally every 30s tick, cross-node via ReclaimRemoteStale on the
  same expired `timeout_at`).
- Verification: all line quotes re-read from `git show HEAD:`; cluster is opt-in
  (`Enabled: false`, cluster_config.go:116) which bounds blast radius; no test pins heartbeat-cadence <
  claim-timeout (worker/pool_setqueue_test.go + queue xnode tests pass — `go test ./internal/worker/
  ./internal/queue/` green at HEAD). Fix shape: wire `DefaultClaimTimeout: clusterCfg.Queue.DefaultClaimTimeout`
  and derive/parameterize the worker heartbeat interval from it (or enforce timeout >= 2x heartbeat at
  NewClusterQueue with a loud default-uplift).

## F-3 [MEDIUM] Journal open-runs for terminal-queue-state turns are never closed and never pruned — permanent per-boot re-walk + unbounded retention hole
- internal/daemon/daemon.go:2529-2544 (requeue default branch returns error for completed/dead jobs),
  internal/agent/turnjournal/store_sqlite.go:248-253 (`PruneClosed` deletes only turns WITH a
  `run_closed` row older than the cutoff), rehydrate.go:60-67 (failed requeue -> row stays open).
- Evidence: when the terminal close write fails (writer.OnRunClose logs Warn and the run stays open —
  by design) or a job reaches `failed`/`dead` at the queue without ever closing its journal run, the
  boot rehydrate takes the default branch and RETURNS AN ERROR so the row "stays open ... until
  boot-time retention prunes it via CloseRun aging" (daemon.go's own comment). That never happens:
  `PruneClosed`'s subquery selects `WHERE kind = 'run_closed' AND created_at < ?` — a run with NO
  run_closed row is invisible to the sweep, and OpenRuns deliberately never excludes it. The rows of
  such runs (run_started + every step row) are retained forever, re-listed by OpenRuns and re-Warned
  on every single boot, forever. The 7d retention contract (research F6, journal.go docblock "an
  append-only journal without retention is a slow leak") has an unpruned class.
- Defect: the retention sweep's exclusion (closed-only) and the rehydrate's exclusion (terminal-queue-
  state-only) intersect into a set of journal runs that no code path ever removes.
- Verification: SQL text re-read from `git show HEAD:`; no existing test exercises prune-after-terminal-
  without-close (turnjournal suite green, so this is uncovered, not failing). Fix shape: have the
  rehydrate default branch write the run_closed row (the queue's terminal state IS the authoritative
  close signal per research F4, which the comment itself invokes) so the row enters the prunable set;
  or extend PruneClosed with an age cutoff for open runs whose job is queue-terminal.

## F-4 [LOW] OpenRuns' "newest payload wins" is not what the SQL implements (tie-dependent, indeterminate row)
- internal/agent/turnjournal/store_sqlite.go:134-142 + docblock 122-125.
- Evidence: `SELECT r.turn_id, MAX(r.session_id), r.result_payload ... GROUP BY r.turn_id` — under
  SQLite's bare-column rule the un-aggregated `result_payload` comes from the row that satisfied the
  MAX aggregate (of session_id), NOT from the newest row by `created_at`. Duplicate run_started rows
  (the documented append-retry / post-ring-eviction case) share the same session_id, so the MAX ties
  and the payload comes from an arbitrary (scan-order-dependent) row. The docblock claim "yields its
  newest payload" — relied on by writer.go's ring-eviction tolerance argument ("a forgotten turnID
  re-appending one duplicate journal row is tolerated by OpenRuns (GROUP BY newest)") — is therefore
  not guaranteed. In practice both duplicate payloads are the same immutable job.Payload, which is why
  this is LOW rather than higher: the mis-stated semantics only bite if a future caller mutates the
  payload between attempts.
- Defect: documented tie-break rule ("newest") is unimplemented; the query's actual behavior is
  indeterminate-row selection.
- Verification: SQL read from `git show HEAD:`; SQLite bare-column semantics are the documented
  min/max optimization behavior (only deterministic when the MAX is the ordering you want). Fix shape:
  `MAX(created_at)` + a join, or `ORDER BY created_at DESC` subselect with LIMIT 1 per turn.

## F-5 [LOW] dispatcher.go RouteToAgent builds contextMsg twice on the executing path — the first build's value is unconditionally overwritten
- internal/agent/dispatcher.go:3191 (`contextMsg := d.buildContextMessage(result, sessionConversationID)`)
  vs dispatcher.go:3282 (`contextMsg = d.buildContextMessage(result, sessionConversationID)`).
- Evidence: the leaf-03 memory-re-scope commit moved the authoritative build BELOW
  `rescopeMemoryContextForAgent` (line 3282, with a comment "The context MESSAGE itself is built after
  the per-agent memory re-scope below"), but the pre-existing build at 3191 was left in place.
  `contextMsg` has NO consumer between the two assignments at HEAD (awk sweep: the only references in
  the span are the two assignments), so the 3191 build is dead on every path — duplicated
  store reads/work inside buildContextMessage per dispatch, and a maintenance trap: any future edit
  to the first call's arguments silently no-ops.
- Defect: dead double-build on the hot dispatch path; two sites must now stay in sync for no reason.
  (Corrected mid-audit: the recall-continuity early return does NOT consume the 3191 value — the awk
  sweep over HEAD shows no `contextMsg` reference between 3191 and 3282 — so the first build is dead
  on EVERY path, pure duplicated store reads per dispatch.)
- Verification: both lines confirmed in `git show HEAD:internal/agent/dispatcher.go`; dispatcher.go is
  sibling-staged WIP, line numbers are HEAD-exact. An `awk` sweep of HEAD between the two assignments
  shows `contextMsg` referenced ONLY at those two lines — the first value is provably dead on the
  executing path. Fix shape: delete the 3191 build and let the recall branch call buildContextMessage itself.

## F-6 [LOW] HEAD's internal/worker/worker.go is gofmt-dirty (semicolon-packed lines from the SetQueue-race fix)
- internal/worker/worker.go:438 (`w.mu.RLock(); qh := w.queue; w.mu.RUnlock()`), same pattern in
  failJob/completeJob (HEAD ~:691, :705).
- Evidence: `git show HEAD:internal/worker/worker.go` piped through `gofmt -l` flags it — the ONLY
  scope file that fails at HEAD (queue/turnjournal/pool files are clean). Introduced by the
  queue-swap race fix (d614d0d3 era) hand-packing three statements onto one line.
- Defect: HEAD violates the repo's own fmt gate (pre-commit fmt-check / `make lint-ci` will flag
  whoever touches the file next).
- Verification: gofmt output above; quoted lines from `git show HEAD:`. Fix shape: gofmt the file in
  the next touching commit.

## Park/resume × dynamic-exposure interaction (the un-swept cross-boundary) — CLEAN, with one design note
Traced per scope item 2. The §6 row-4 hard gate reads the plan from the TURN CONTEXT
(executor.go:1283 `exposurePlanFromContext(ctx)`), and the only arming site is reasoningCycle entry
(loop.go:4723-4731: flag-gated `ctx = WithExposurePlan(ctx, plan)`). Every queue/worker replay path
re-enters the loop through `RunOnce` -> `reasoningCycle`:
- Journal-rehydrate requeue -> worker -> AgentJobProcessor.Process -> `agentLoop.RunOnce(ctx, ...)`
  (components.go:10390). The worker ctx carries NO plan, but reasoningCycle re-arms
  `ensureExposurePlan(conversationID)` BEFORE any Execute call-site downstream. No
  `Executor.Execute` call-site exists upstream of that arming point (the other `ExecuteAll` grep hit,
  loop.go:2961, is the HTTP-hook executor, a different payload/type).
- Quota-parked turns never close the journal run (turn_journal_hooks.go:66-68 "NEVER called on the
  park path") and ride Requeue with the same job ID, so park + boot-rehydrate cannot double-resume.
=> Resume does NOT bypass the plan seam; the row-4 gate stays armed on resumed turns. Design note
(not a defect, surfaced per the surface-unacted rule): across a daemon RESTART the fresh loop rebuilds
the plan (core + meta pair), so pre-crash earned promotions are gone and a resumed turn must re-earn
them via tool_view — recovery exists by construction, and journaled-step replays skip tool invocation
entirely, so they never trip row-4. Consistent, worth a product decision if replayed side effects
should keep earned scope.

## Audited-clean (scope-verified this wave, previously un-swept)
- store_sqlite.go connection posture: `?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)`
  DSN (modernc.org/sqlite honors only `_pragma=` keys — the docblock at :42-46 is accurate); every
  statement bounded by opTimeout; nil-receiver guards on all methods; concurrent writes ride
  database/sql + WAL. No fan-out hazard found.
- PruneClosed × OpenRuns exclude-join: a prune deletes ALL rows of a closed turn
  (`turn_id IN (SELECT ... WHERE kind='run_closed' ...)`), so no lingering run_started can re-appear
  as OPEN; OpenRuns' NOT-EXISTS keys on turn_id + run_closed correctly. The residual hole is F-3
  (never-closed runs), not re-appearance.
- Schema-vs-writer CHECK contract: step_completed always carries non-nil payload (Append pre-check
  :93-95 + writer nil->"{}" at writer.go:148-152); CloseRun's empty-string session_id/step_id satisfy
  the NOT-NULL schema. Duplicate run_started tolerated by GROUP BY (semantics caveat = F-4).
- 8e2b4121 sibling hunt: ClaimNextClustered's SELECT lists exactly columns baseSchema+clusterSchema
  create (claimed_by_node/timeout_at/last_heartbeat_at in clusterColumnNames, store.go:158-161,
  idempotent ALTER migration) — no second daemon-vs-assumed-schema gap in range.
- d614d0d3 queue-swap race fix: all `w.queue` reads in tryProcessJob/failJob/completeJob are
  mutex-guarded snapshots; Pool.SetQueue pushes the handle to live workers; the stale-handle window
  (worker captured old PersistentQueue, cluster reclaim fires meanwhile) is benign BY DESIGN —
  ResetToPending/ResetStaleClaimsAtStartup retire claim_token (H5) and CompleteAttempt keys on it, so
  the superseded attempt's write is refused.
- 92a486a1 Stats-shadowing fix: ClusterQueue.Stats forwards to the wrapped queue (QueueStats shape),
  ClusterStats renamed with its sole RPC caller updated (rpc/cluster_handler.go:91), and
  `_ Queue = (*ClusterQueue)(nil)` re-asserted — the silent-degradation hole closed properly.
- H2 capability-forwarding regression: PersistentQueue.CompletionIsFresh -> store (queue.go:539-541)
  and ClusterQueue's forwarder (cluster_queue.go:380) intact at HEAD; queue.job.completed carries
  claim_token.
- ReclaimIfStale CAS re-check + ReclaimRemoteStale per-row WHERE re-check are race-safe against
  concurrent sweepers.
- dispatcher.go in-range diff (236 lines): fence-factory swap (configureFence removal — zero stale
  callers at HEAD, grep-verified), digestContextEnabled() guards at both analyzer sites, memory
  re-scope ordering, stable/volatile recall split — logic reviewed clean; the only defect found is
  the dead double-build (F-5).
- Gates run: `go build` + `go vet` on ./internal/queue/... ./internal/worker/...
  ./internal/agent/turnjournal/... clean at the (sibling-dirty) tree; `go test -count=1` on the three
  packages ALL ok (0.88s / 3.32s / 3.04s). No finding depends on sibling edits — all evidence quoted
  from `git show HEAD:<path>`.

AUDIT COMPLETE — 6 findings: F-1 MEDIUM, F-2 HIGH, F-3 MEDIUM, F-4 LOW, F-5 LOW, F-6 LOW.
All findings cite file:line at HEAD 7b510ed4 with quoted committed-source evidence. The
park/resume × exposure cross-boundary (the flagged gap): CLEAN, traced above.


## Closeout (2026-11-06, fix pass)
- F-1 CLOSED: d43dd7d6 (failed -> pq.Retry, real requeue; pin TestBootRehydrateFailedJobActuallyRequeues).
- F-2 CLOSED: 420b09b4 (claim timeout from clusterCfg.Queue.DefaultClaimTimeout, <=0 floor; no unit pin — wiring-level, cluster is opt-in; sweeper/heartbeat values documented).
- F-3 CLOSED: 420b09b4 (rehydrate default branch writes the run_closed row; pin TestBootRehydrateTerminalJobClosesJournalRun, mutation-proven).
- F-4 CLOSED: 23061fd4 (OpenRuns explicit MAX(created_at) subselect; pin TestOpenRunsNewestPayloadWins, mutation-proven).
- F-5 CLOSED (by sibling): the dead double-build was deleted in the dispatcher-staged sibling work already in the index (single build at HEAD-3283, re-scope comment moved with it). Not committed by this session; verified present in `git diff --cached internal/agent/dispatcher.go`.
- F-6 CLOSED: 23061fd4 (gofmt -w, formatting-only).
AUDIT CLOSED - 6/6 (F-5 landed via sibling WIP).
