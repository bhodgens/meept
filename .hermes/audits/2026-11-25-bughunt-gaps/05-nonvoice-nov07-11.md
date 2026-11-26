# Auditor 5: non-voice (range 2026-11-06..11-11)

Scope: non-voice Go production code changed in 7b510ed4..ea3d7082 (ledger, dsh wiring, contact/comm, queue, tools/mcp, fix(agent...) commits), read at HEAD for confirmation.
Files read: 14 of ~380 changed files in scope (risk-ranked). Line-level diffs read for: internal/ledger/{store,ops,transitions}.go, internal/daemon/{ledger_brief_records,brief_commitments,ledger_integrity,guided_intake_wiring,ledger_publish,ledger_audit,ledger_rpc,ledger_brief_source,contact_wiring,messaging_arming,dsh_wiring}.go, internal/contact/{chain,store,resolve}.go, internal/comm/http/messaging.go, internal/rpc/ledger.go, internal/tools/builtin/ledger.go (partial). HEAD-only (no range diff) spot reads: internal/queue/{store,queue,cluster_queue,store_cluster_claim,transitions}.go, internal/worker/worker.go, internal/agent/{park_backend_queue,park_store_sqlite,loop_park,gate_park,parked_turn,delegate,registry}.go, internal/employee/goal_round_driver.go, internal/voice/{voiceattempt,manager}.go.

Note on HEAD: the task states HEAD e43a7cf3; the live HEAD is 997d6d02 (one commit later, scheduler-only). All file:line below are from the live tree, read via `git show HEAD:` for files with uncommitted edits (handler.go, loop_park.go, components.go, contact_wiring.go, skills/watcher.go).

Note on scope mismatches in the brief (important for the parent):
- There is NO `internal/dsh` package at HEAD and no Go code reads/writes `.hermes/dsh-exec-state.json` (`git grep` over `*.go` returns nothing). That file is an orchestrator state file, not a daemon file. Priority (b) as written is not auditable; I audited the dsh *wiring* (`internal/daemon/dsh_wiring.go`) instead.
- `internal/tools/mcp` has ZERO changes in 7b510ed4..ea3d7082. `version_pin.go` and `version_check.go` were created by dd1f5e7c on 2026-11-22 (after the range) and have uncommitted edits right now. Priority (e) belongs to the Nov 12-24 auditors, not this range. Not audited here.
- `internal/comm/slack` changed by 3 lines in range (lint only). No inbound-message handler exists there (only the signed interactions endpoint and an outbound push channel).

## Findings

### A5-1 — MEDIUM — Contact tool blocks the agent turn for the whole duration of an answered call
- File: `internal/daemon/contact_wiring.go:84-116` (`Run`), `internal/voice/voiceattempt.go:66-73` (`Lookup` wait), introduced by 256b2be9 (2026-11-08, in range).
- Code:
  ```go
  // contact_wiring.go Run
  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
  defer cancel()
  ...
  report := w.Runner.Run(ctx, rec, message, override)
  ```
  ```go
  // voiceattempt.go Attempt
  out, ok := a.outcome(ctx, ccid)   // blocks until the release hook fires
  switch out {
  case OutcomeAnswered, OutcomeHangup:
      return string(OutcomeAnswered), nil
  ```
- Trigger: the `contact` tool dials `telnyx_voice`; the callee answers and talks. The outcome registry is only written from `manager.SetReleaseHook` (`voice_wiring.go:889-892` → `w.outcomes.Record`), i.e. at call **teardown** (hangup). So `Lookup` polls until the caller hangs up, up to the 10-minute cap.
- Effect: the employee's turn that called `contact` is blocked for the entire live call, contradicting the tool description ("A connected voice call does NOT block the result", `builtin/contact.go:80-81`) and the chain comment ("the chain does not wait", `contact/chain.go:177-179`). If the call exceeds the 10-min cap the attempt is misreported as `no_answer` with `errOutcomeUnknown` and the chain **advances to the next channel** (e.g. sends an SMS / dials Signal) to a person who is currently on the phone.
- Caller evidence: `internal/tools/builtin/contact.go:110` (`t.chain.Run(person, message, channels)`), sole production `ChannelAttempt` for telnyx_voice at `contact_wiring.go:276`.
- Later commits touching these lines: `contact_wiring.go` touched by dd1f5e7c, 223dd85c, d526f6c5 (egress gates / lint) — none change the wait semantics. `voiceattempt.go` unchanged since 256b2be9.
- Confidence: **likely** — read the full wait path; I did NOT get to confirm whether any code path records an outcome at `call.answered` time (the grep for `outcomes.Record` callers was the command that was blocked). `voice_wiring.go:889` is the only `SetReleaseHook` caller I found, and the `AddAnsweredObserver` lane is documented as a timer-cancel seam, so I believe no answer-time record exists, but this is the one link I could not close.

### A5-2 — MEDIUM — Goal approval-gate park rows are never released when the goal leaves `active`
- File: `internal/daemon/gate_park_wiring.go:210-256` (`handleGateRelease` / `pumpGateReleases`), added by 407932d6 (2026-11-09, in range, commit `fix(agent,daemon): gate parks release on plan rejection/cancel`). Key identity in `internal/agent/gate_park.go:322-345`.
- Code:
  ```go
  // handleGateRelease
  var payload struct { PlanID string `json:"plan_id"` }
  ...
  released, err := c.GateParker.ReleaseByKey(payload.PlanID, reason)
  ```
  ```go
  // goal_loop.go:1671-1672 — goal gates are parked under the GOAL id
  ok := parkFn(agent.GateParkData{
      GateKind:  "goal",
      GateID:    goal.ID,
  ```
- Trigger: a goal with an `approval` gate parks a `goal|<goalID>` row (`employee/goal_loop.go:1670`). The operator then pauses / blocks / retires the goal via `goal_update` (legal transitions in `employee/goal.go:109-115`).
- Effect: the release pump only listens on `plan.rejected` / `plan.cancelled` and keys on `plan_id`, which can never match a `goal|` key. `ReleaseByKey` has exactly one production caller (this handler). The parked row survives forever (in memory + `parked_turns` / queue backend, re-armed on every restart). Consequences: (1) `Manager.ResumeGoalGateParked` (`employee/manager.go:2803`) and `resumeGoalGateFromDrain` (`goal_gate_wiring.go:95-121`) check no goal state, so a late `agents.goals.approve_gate` on a retired goal re-runs `Reflect` with the approval marker armed; (2) the fix is INCOMPLETE relative to its own stated goal ("no gate row outlives its plan's terminal decision").
- Caller evidence: `git grep ReleaseByKey` → only `gate_park_wiring.go:222`. `git grep GateKind:` → `gate_park_wiring.go:188` ("plan") and `employee/goal_loop.go:1672` ("goal").
- Later commits: none touch `gate_park_wiring.go` after the range.
- Confidence: **confirmed by reading** the key construction and the sole release caller. The *impact* of re-running Reflect on a retired goal is unverified (I did not trace whether `Reflect`'s `lookupActiveGoal` miss makes it a no-op).

### A5-3 — MEDIUM — `RunAgentWithModelRef` defer-clear races concurrent turns on the shared per-agent loop
- File: `internal/agent/registry.go:1253-1263`, added by 02fa667b (2026-11-11, in range, `fix(agent,daemon): regression repairs R3`).
- Code:
  ```go
  defer func() {
      if !loop.IsModelOverridePersistent() {
          loop.ClearModelOverride()
      }
  }()
  if modelRef != "" {
      loop.ApplyRequestModel(modelRef)
  }
  return loop.RunOnce(ctx, message, conversationID)
  ```
- Trigger: two delegate children of the **same** specialist agent run concurrently (async spawn at `delegate.go:578` launches `go m.runChild(...)` per child; nothing serializes runs per agent loop — the codebase itself acknowledges overlapping runs on one loop, `loop_park.go` F8 comment). Child A finishes first.
- Effect: A's deferred `ClearModelOverride` wipes B's armed one-shot override before B's first reasoning cycle consumes it → B silently runs on the child spec's own model instead of the allowlist-gated route (C10 enforcement silently dropped). The same defer also wipes a *user* `chat.request model:` directive armed for an unrelated in-flight turn on that loop (`dispatcher.go:3473`, `handler.go:4065` are the other `ApplyRequestModel` callers).
- Caller evidence: `dm.SetRunFunc(c.AgentRegistry.RunAgentWithModelRef)` at `dsh_wiring.go:313`; `runChildTurn` at `delegate.go` (02fa667b hunk) is the only `runFn` caller.
- Later commits: none.
- Confidence: **likely** — the race is real by construction (shared loop field, no per-turn scoping, concurrent goroutines). I did not run a reproduction. The code comment itself admits "a full per-turn context-carried mechanism does not exist on the RunOnce API".

### A5-4 — MEDIUM — Commitment dedupe (`HasCommitment`) silently fails open once an employee has >1000 commitment rows
- File: `internal/daemon/brief_commitments.go:220-245`. (Added by 453f8b21 on 2026-11-11 20:53 — **after** ea3d7082; listed here because the parent asked for "current HEAD bugs" and it sits on the in-range ledger surface. Drop if strictly range-scoped.)
- Code:
  ```go
  rows, err := f.store.QueryLog(ctx, ledger.LogQuery{
      EmployeeID: f.employeeID,
      Kinds:      []ledger.Kind{ledger.KindCommitment},
      Limit:      1000,
  })
  ...
  if p.Idx == index { return true, nil }
  return false, nil
  ```
- Trigger: `QueryLog` is `ORDER BY id ASC LIMIT 1000` (`ledger/store.go:539`). Once an employee accumulates >1000 `commitment` rows, the newest rows fall outside the window and `HasCommitment` returns `false` for every recent `(ccid, idx)`.
- Effect: the "exactly-once per (ccid, index)" contract (`voice/brief_seam.go` C7) is void for all new calls; any re-finalize (replayed hangup, restart re-arm, release fan-out race) duplicates commitment rows.
- Caller evidence: `voice.fileCommitmentOnce` → `filer.HasCommitment`; only implementation is this one.
- Later commits: none.
- Confidence: **confirmed by reading** the query and the ordering.

### A5-5 — LOW — `HasPendingForConversation` disambiguation is conversation-scoped, not turn-scoped
- File: `internal/agent/handler.go:3454`, `:3983`; `internal/agent/loop_park.go:744`. Added by ea3d7082 (2026-11-11, in range, `fix(scheduler,agent,plan): regression repairs R2`).
- Code:
  ```go
  if strings.TrimSpace(reply) == "" && err == nil {
      if loop.TurnParker().HasPendingForConversation(turn.ConversationID) {
          h.logger.Info("quota resume re-parked mid-flight — deferring delivery", ...)
          return
      }
  ```
- Trigger: turn A is throttle-parked on the TurnParker (row present for conversation C, resume in minutes). User sends turn B in the same session; B quota-parks on the **QuotaResumeWatcher** (a different parker, in-memory map, not the TurnParker), resumes, and the state layer absorbs an LLM failure → `("", nil)`.
- Effect: the guard sees A's unrelated park row for conversation C and treats B's absorbed failure as a re-park → B never persists, never sends a response, never emits `turn.terminal`. The R2 fix explicitly exists to stop this hang and still hangs in this interleaving. The payload already carries `TurnID` (`loop_park.go:51`), so a turn-keyed check was available.
- Caller evidence: the three sites above are the only callers.
- Later commits: none.
- Confidence: **likely** — read all three sites and `HasPendingForConversation` (`parked_turn.go:444-456`, which iterates by `ConversationID` only). Whether the turn watchdog eventually reaps B is unverified.

### A5-6 — LOW — `OutcomeRegistry.outcomes` grows without bound
- File: `internal/voice/voiceattempt.go:103-128` (256b2be9, in range).
- Code: `Record` does `r.outcomes[ccid] = out`; `Lookup` reads but never deletes.
- Effect: one map entry per phone call for the daemon's lifetime; a late outcome after `Lookup` timed out is stored and never consumed. Small (string key + string value), so LOW, but it is a monotonic leak on a long-lived daemon with no eviction path.
- Confidence: confirmed by reading the two methods.

### A5-7 — LOW — `Store.ClaimNextByID` has no `due_at` / `next_retry_at` predicate (TOCTOU on the slow claim path)
- File: `internal/queue/store.go:2408-2442`. **Pre-existing** (introduced 2026-05, last touched 56294aba on 2026-10-06) — not introduced by this range, but the range's queue work (cf83d64e "full claim-column reset on all requeue paths") did not cover it, and the queue-backend parked rows (3a4964a3, post-range) now make the `due_at` guard load-bearing.
- Code:
  ```go
  result, err := tx.Exec(`UPDATE jobs SET state = 'claimed', claimed_by = ?, claim_token = ?, updated_at = ?
      WHERE id = ? AND state = 'pending'`, workerID, claimToken, now, jobID)
  ```
- Trigger: slow path (`PersistentQueue.Claim`, active in production via `SetTaskCancelledCallback` at `components.go:3774`) selects job J as due; before `ClaimNextByID` runs, another worker claims J, fails it, and `Retry` puts it back to `pending` with `next_retry_at = +2s`.
- Effect: the first worker's claim succeeds against a job that is in backoff, bypassing the retry delay. Window is milliseconds; impact is one early retry. The `due_at` sentinel is safe today only because the slow path filters future-due rows in Go.
- Confidence: confirmed by reading; reachability narrow.

## Already fixed by a later wave

None that I confirmed as "bug present at ea3d7082, fixed after". Candidates I checked and found were either already fixed *inside* the range or never buggy:

1. **86daca98 C3 "defer on empty reply" hang** — the original `("", nil)` defer (86daca98, 2026-11-10) hung clients on absorbed failures; fixed *within* the range by ea3d7082 (R2-2) with the `HasPendingForConversation` guard. The residual of that fix is A5-5 above.
2. **Ledger close ordering (D1 HIGH)** — `EmployeeLedger.Close()` ran before `Voice.Stop`, so live release-hook appends hit `sql.ErrConnDone`. Fixed by 0a2a2760 (2026-11-10, in range); verified at HEAD `components.go:7943-7960` that the close is after the voice block.
3. **`ledgerReaderAdapter` typed-nil** — `rpc.NewLedgerHandler(ledgerReaderAdapter{store: nil})` boxes a non-nil interface holding a nil store. Checked: the adapter methods each guard `a.store == nil` and return `errLedgerDisabled`, so the handler's own `h.reader == nil` check being unreachable is harmless. Not a bug.
4. **`brief_commitments.go` nil analyzer goroutine** — `llmBriefAnalyzer.Analyze` spawns a goroutine calling `a.llm(prompt)` with a timeout wrapper; a hung call leaks one goroutine until the underlying `llm.Client` 120s timeout fires (`client.go:29`). Bounded, not a leak. Not reported.

## Verified clean

- `internal/ledger/store.go:316-472` `Append` — one transaction covers resolves-check, transition-check, INSERT, FTS insert, state UPSERT; hook and tail fan-out fire only after `Commit`. `ErrIllegalTransition` / `ErrResolveNotFound` paths roll back with zero rows. `resolves` is scoped `AND employee_id = ?` so cross-employee closes are refused. `due_at` overwrite semantics match the doc comment. Idempotency is by design NOT provided (append-only log) — callers that need it (commitment filer) own it (see A5-4).
- `internal/ledger/ops.go:269-305` `RepairState` — writes only `VerifyState`-flagged rows inside one transaction; the D5 `have.AsOf > want.AsOf` skip (`:178`) prevents rewinding a fresh append.
- `internal/ledger/store.go:284-295` `Close` + `ops.go:522-552` `fanoutTailers` / `wakeTailersLocked` — mutex is not held across channel sends or DB close; double-Close is guarded by `closed` / `stopped`. `Tail` has no production callers (only `activity_rpc.go:115` `ring.Tail`, a different method), so the 1024-buffer backpressure on `Append` is currently unreachable.
- `internal/daemon/ledger_publish.go` + `ledger_audit.go` — hooks run outside `Store.mu` (`hook()` copies under lock, `:486-490`), audit write is bounded by a 5s ctx, failures are WARN-only and cannot reach `Append` callers. `storage.StorageRecordChangedTopic` is the typed topic the AGENTS.md routing expects.
- `internal/daemon/ledger_integrity.go:115-150` — `cfg` nil cannot reach `cfg.Ledger` because `buildLedger` returns nil for nil cfg and the nil-store guard returns first. `@every 168h` is a valid robfig/cron duration.
- `internal/comm/http/messaging.go:134-206` — signature verified on raw bytes BEFORE parse (401), nonce checked only after a valid signature, nonce released before the 500 so redelivery re-reaches the handler, `nonceUsable` handles typed-nil. The route is auth-exempt at `server.go:2163` but `NewMessagingRoutes` refuses a nil `Verify`, and `voice_wiring.go:408-422` only builds `verify` when the phone leg is enabled, so a messaging-enabled/local-only boot fails closed ("route unserved" WARN) rather than running signature-optional.
- `internal/daemon/messaging_arming.go:238-301` — `OnInbound` returns an error on store failure (→ nonce release + 500), returns nil on duplicate, and swallows reply-hook errors after the durable append by design. No lock held across the hook; the slot getters copy under mutex.
- `internal/comm/slack/interactions.go:130-148` — raw body read first, HMAC verified with a 5-min window before parse, 401 on failure. Only the signed interactions endpoint exists in this package; no inbound message handler to audit.
- `internal/contact/store.go:221-256` `Put` — single-statement upsert on the `phone` UNIQUE key; no Go mutex held across I/O. `LookupByPhone` is exact-match by contract. Note the comment at `:232-234` says `voice_approved` / `messaging_opt_out` are "second-wins": a `Put` that does not round-trip the flags will silently reset them. The only production `Put` callers (`sms_reply_glue.go:146,185`) do a Get-modify-Put, so it is currently safe, but it is a foot-gun for the next writer.
- `internal/queue/store.go:638-758` `ClaimNextForAgent`, `:780-804` `MarkProcessing`, `:907-974` `CompleteAttempt`, `:1005-1032` `CompletionIsFresh` — claim is CAS'd on `state='pending'` with a fresh token per attempt; the pool is `_txlock=immediate` + `SetMaxOpenConns(1)` so two workers cannot both win. Completion idempotency and supersession are distinguished by `(state, token)`. `ResetStaleClaimsAtStartup` only touches `claimed`/`processing`, so `pending` parked rows are not reset.
- `internal/queue/transitions.go` + `store.go:1281-1352` `Requeue` / `:1354-1430` `Retry` — every mutator folds its source set from `jobTransitions`; `Retry` is in a tx with a read of `retry_count` and refuses an exhausted budget. The worker-Retry vs `OnJobFailed`-Retry race cannot double-increment because `pending` is not a legal source of `pending`. The stale-failure → `Requeue` path (the M7 class) is closed at the publisher: `PersistentQueue.FailAttempt` only publishes `queue.job.failed` when the store accepted the write under the presented token.
- `internal/worker/worker.go:341-364` — heartbeat goroutine is joined via `heartbeatDone` before the lock is taken; the provider-wait requeue path releases `w.mu` before returning.
- `internal/agent/delegate.go` `Cancel` (c30f3ed4) — `m.mu` is released before `persistRecord` / `publishResult` / `close(e.settle)`; `settleWithError` no-ops on an already-settled entry, so the cancel-vs-finish race is benign. `02fa667b` R3-1 `delete(m.resuming, rec.ChildID)` in `resumeRegister` closes the stale-claim regression in the same critical section as the insert.
- `internal/agent/park_backend_queue.go:187-256` — parked rows carry a far-future `due_at` sentinel and are `state='pending'`; every claim path filters `due_at <= now`, `SweepAbandonedTerminalStates` only selects `failed`/`dead`, `ResetStaleClaimsAtStartup` only `claimed`/`processing`. Prune in `LoadOwnedKeyed` is owner-scoped and inside the same tx as the select.
- `internal/daemon/dsh_wiring.go:199-217` `goalRoundDeferredScheduler.ScheduleOneShot` — the interval job unschedules itself before running `fn`; `GoalRoundDriver.Stop` waits on `pollDone`. `planModeStorePersistence` keeps the memory lane in sync on both branches.

## Not audited

Files in the scope list I did not reach (by priority order the parent should dispatch):
- **`internal/tools/mcp/*`** — zero changes in range; `version_pin.go` / `version_check.go` / `manager.go` are post-range (dd1f5e7c, 2026-11-22) and currently have uncommitted edits. Priority (e) needs re-routing to the Nov 12-24 window.
- **`internal/dsh`** — does not exist. The `.hermes/dsh-exec-state.json` state file has no Go reader/writer.
- **`internal/scheduler/*`** — `timing.go` (+839), `receipts.go` (+370), `jobs.go`, `claims.go`, `scheduler.go` were NOT read. Several post-range fixes land there (`40819154`, `2d2f3d08`, `81f8e13e`, `86102201`, plus 997d6d02 at HEAD), so it has had eyes, but not as a range read.
- **`internal/skills/watcher.go`** (+408, in range; has uncommitted edits now) — not read. Goroutine lifecycle and fs event handling are exactly the bug classes in the brief.
- **`internal/session/*`** — `projection.go` (+390), `child_descriptor.go` (+202), `projection_cache_sqlite.go`, `store_sqlite.go`, `format_migration.go`, `schema_version_sqlite.go` not read.
- **`internal/plan/*`** — `manager.go` (+686/-?), `store_check.go` (+284), `transitions.go` not read. (ea3d7082 itself touches plan.)
- **`internal/context/sessionref/*`**, **`internal/signal/*`** (+757 rpc, +430 supervisor), **`internal/telnyx/sms_attempt.go`** (+289), **`internal/telnyx/messaging.go`** (+282) — not read.
- **`internal/agent` fix commits not fully traced**: 0b5cc1cd (PII masking / tier metadata / delegation leaks), 663b633a (exposure/scope holes), 993f69de (`ExposurePlan.Promote` mutexio), cc06827f (spoken-lane guard), e7dc20be (fork-seed replay), 50b4309e (reply-guard JSON shapes), 80659bb5 (budget park persistence — only the stat was read; `budget_resume.go` +408 not traced). For each I read the commit message and file list only.
- **`internal/agent/delegate.go` / `delegate_descriptor.go`** (+890 / +651) — only the `Cancel`, `Spawn` depth, and `runChildTurn` hunks were read; `ResumeInFlightDescriptors`, `resumeRegister` beyond the R3-1 hunk, and the ACP child path were not.
- **`internal/daemon/components.go`** (+756) and **`daemon.go`** (+258) — only the ledger close block, messaging store block, and `SetTaskCancelledCallback` site were read.
- **`internal/llm/*`** — `client.go`, `anthropic.go`, `codex.go`, `retry_after.go`, `lfm_loader.go` changes not read (the llm AGENTS.md was read for rules).
- **`internal/comm/http/server.go`** (+141) — only the auth-exemption block at `:2140-2170` was read.
- **`internal/memory/vector/*`**, **`internal/selfimprove/applier.go`**, **`internal/employeemd/md.go`**, **`internal/weather/weather.go`**, **`internal/moment/moment.go`**, **`internal/daystate/daystate.go`**, **`cmd/meept/voice.go`** (+1330 — voice CLI, arguably the voice auditor's), **`cmd/meept/agents_cmd.go`**, **`internal/rpc/{voice_link,voice_local,delegate_children,proxy}.go`** — not read.

One process note for the parent: my final verification grep (confirming that nothing records a contact-chain outcome at `call.answered` time) was blocked by a user-denied command. That is the single unverified link behind A5-1's "likely" grade.
