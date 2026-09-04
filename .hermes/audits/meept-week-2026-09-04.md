# Bughunt Audit + Fix Wave — Week of 2026-08-28 → 09-04 + Agent Loop

**Date:** 2026-09-04
**Scope:** commits `2f4c2f15..HEAD` (post-prior-wave delta: Codex SSE streaming,
Bedrock SigV4, provider streaming rotation, codex session affinity, agent-loop
fixes) PLUS fix-status re-verification of every finding from the 2026-09-03
report-only wave, PLUS fixes.
**Method:** 9 auditor subagents (disjoint scopes; several executed fixes in
scope — every diff adversarially reviewed by the parent) + parent
source-verification of every fix. Parent-parallel writers landed H8/H9 and
committed C1 independently (c6c07f0b).
**Mode:** FIX AUTHORIZED. All fixes gated below.

---

## Gate status (final)

- `go build ./...` — green
- `go test -p 2 -count=1 -short` — green on agent, daemon, llm, acp, skills,
  worker, employee, memory, bus, tui, config, pkg/sqlite, comm/* (all 0 fail)
- `gofmt -l` — clean; `go vet` — clean
- `predid`, `mutexio` analyzers on all touched packages — clean
- `flutter analyze api_models.dart` — no issues

## Round 2 — auditor report follow-ups (same session, after reports landed)

### D-C3 — reproduced panic in cycle detection (FIXED)
- internal/agent/loop.go:186 — `firstArgs[:8]` panicked on the 5-char
  `"empty"` hash literal (hashArgs("{}") → "empty"); trigger = any tool
  called 3× with `{}` args. Prefix now clamped. Auditor reproduced via
  standalone test; fix verified by build + cycle/guard tests.

### D-H2 — unsynchronized reasoningOverride read (FIXED)
- internal/agent/loop.go:3468 — `l.reasoningOverride` read without l.mu in
  the reasoning loop while Set/ClearReasoningOverride write under l.mu from
  the RPC thread (internal/rpc/reasoning.go:482/524). Now read under RLock
  (snapshot with reasoningForNextTurn in one critical section).

### G-aggravators — legacy memory rows invisible / version restart (FIXED, minimal hardening)
- internal/memory/manager.go:1524 — GetByID now accepts `is_current = 1 OR
  is_current = ''` so pre-migration rows (ADD COLUMN default '') stay
  visible on the primary read path.
- internal/memory/manager.go:1439 — getCurrentVersion uses
  `MAX(CAST(version AS INTEGER))` so TEXT-affinity chains compare
  numerically and Scan(int) can't silently zero the counter.
- Note: the full fix is a copy-and-cast table rebuild for existing legacy
  DBs (SQLite can't ALTER COLUMN TYPE); the code-side hardening above stops
  the data loss. Rebuild migration = follow-up if legacy DBs exist.

### A-HIGH — codex SSE buffering made streaming cosmetic (FIXED)
- internal/llm/codex.go:500 — io.ReadAll moved OFF the streaming path:
  resp.Body is now consumed incrementally by the SSE scanner (deltas fire
  as events arrive; stop-at-terminal ends the read without EOF). This also
  fixes the keep-alive stall (stream left open after response.completed
  wedged the turn until the 120s http timeout) and removes the unbounded
  buffering of server output on the streaming path. Verified: all 17
  TestCodexSSE* tests green (they cover flushed, chunked, idle-cancel,
  abort, garbage-skip, and terminal-stop semantics).
- Probe results during fix: scanner handles chunked/flushed HTTP bodies
  correctly; the failure mode of the naive "just swap the reader" attempt
  (body pre-read then re-parse → empty) was root-caused before landing.

## Fixed this wave (round 1: prior-wave close-out) — every entry verified

### C1 — worker requeue returned holding w.mu
- internal/worker/worker.go tryProcessJob — fixed by auditor E, committed
  c6c07f0b with a deterministic TryLock + FSM-resting-state regression test.
  Unlock precedes return; Processing→Complete is a legal transition
  (state.go:37); JobsComplete correctly not incremented.

### C2 — chat throttle parking dead end-to-end (both legs)
- Leg 1 (never Start()ed): internal/agent/loop_park.go —
  SetThrottleParker/SetTurnParker now Start() the parker when already armed
  with a resume func (daemon wiring order: configure → wire → Start inside
  the setter; no components.go Start call needed). Verified reachable from
  the daemon path.
- Leg 2 (ConfigSnapshot omission): internal/agent/loop.go:6293 — nil-guarded
  closure `func(l2){ if p := l.TurnParker(); p != nil { l2.SetTurnParker(p) }}`
  propagates the shared parker to every per-session clone. The parker is the
  SAME instance (one drain loop), matching the merged-queue design.

### H1 — TTSR third call missing the parked (nil,nil) check
- internal/agent/loop.go:3539 — the skip-and-continue third call now returns
  ("", nil) on `err == nil && response == nil`, same parked semantics as the
  main call and first retry. No more park + failure double-execution.

### H2 — park ("", nil) flowing through the success pipeline
- internal/agent/loop.go:2398 — parked turns are detected and short-circuit
  BEFORE reflection, learning pipeline, LoRA capture, and the blank
  AddAssistantMessage. No phantom success trajectories, no history pollution,
  worker state no longer marked completed for un-executed turns.

### H3 — resume duplicating the user message
- internal/agent/loop_park.go:118 (WithResumedTurn marker) +
  internal/agent/loop.go:2124/2356 (skipUserMessageAdd gate) +
  resumeThrottledTurn marks the resume ctx. History stays
  `user: X → assistant: <real answer>` instead of
  `user: X → assistant: "" → user: X`.

### H4 — employee episode parks with no dedup (≈96x executions on a 24h window)
- internal/employee/park.go — EpisodeParker gains a dedup map keyed on
  `employee:phase:trigger-identity` (FiredAt excluded; scheduler ticks
  re-fire with fresh timestamps). Duplicate parks during a live park are
  absorbed (return true, no error — the episode IS scheduled). Claim cleared
  at drain (ResumeGoalEpisode → clearParkDedup), on park refusal, and
  re-armed by the H6 repoll re-park. Chat turns remain intentionally NOT
  deduped (different user message = different turn).

### H5 — tier-2 parked episodes resumed into a no-op
- internal/employee/park.go:348 — phase "assess" is only produced by tier-2
  parks (goal_loop.go:648 tier switch); the resume case now calls
  decideTier2 (Assess → Plan → pending_approval) instead of Assess-and-
  discard. Tier-1/3 phases unchanged.

### H6 — employee resume bypassing the pause gate
- internal/employee/park.go:331 — ResumeGoalEpisode checks statusFn()
  (botManager.GetBotStatus — same signal the daemon wires at
  components.go:4165) and, when "paused", re-parks the record at
  pausedRepollInterval (15m) with the dedup claim re-armed, instead of
  executing. Matches the normal Trigger path's Enabled rejection
  (manager.go:1366). Execution on refused re-park is logged, never silent.

### H7 — migrateSchema adding version/is_current as TEXT
- internal/memory/ftstore.go:191 — migration columns now carry the fresh-DDL
  types: version/is_current as INTEGER (DEFAULT 1), text columns stay TEXT.
  Migrated legacy DBs no longer hit lexicographic MAX(version) stalling at 9.

### H8 — IntentTypes[0] (agent-ID seed) winning — a13526d8 still a no-op
- internal/agent/capability_matcher.go:310 — getDefaultIntentType now scans
  IntentTypes for the first entry passing IsValidIntentType (real intent
  vocabulary) instead of taking [0] blindly; inverse DefaultAgent() mapping
  is now genuinely reachable. Fixed by parent-parallel writer; verified.

### H9 — only production NewHTTPHook passing a nil allowlist
- internal/daemon/epistemic_wiring.go:219 + internal/config/schema.go:3451 —
  new `allowed_urls` config field; empty → auto-allow the hook's own URL
  (regex-escaped exact match). The HTTP-hook feature is no longer
  structurally dead. Fixed by parent-parallel writer; verified.

### H10 — park events emitting resume_at; both surfaces reading unblock_at
- internal/agent/parked_turn.go:431 — ParkTurnEvent gains UnblockAt
  (`unblock_at`, canonical — the key both surfaces already read) while
  keeping ResumeAt as a legacy alias; emitParkEvent fills both.
- internal/tui/app.go:1643 — quota handler reads unblock_at then resume_at.
- ui/flutter_ui/lib/models/api_models.dart:167 — same dual read.
- The "throttle retry HH:MM" badge now renders from park events on both
  surfaces.

### H11 — conv-id stamped as session_id (WS filter misroute)
- internal/agent/loop_park.go:239 — park records keep the RAW session ID
  (loop.currentSessionID) in SessionID even when empty; the fallback to
  conversationID stamps ONLY ConversationID. The WS filter
  (comm/http/server.go:567) matches the event against the client's
  subscribed session for TUI/CLI-originated turns too.

### M7 — PublishBlocking no-subscriber demoted to DEBUG (silent drop)
- internal/bus/bus.go:140 — blocking publishes with no subscribers log at
  WARN ("event dropped"); informational paths stay DEBUG per d48d89b7's
  intent. The "security-critical, drops unacceptable" contract is visible
  again.

### M13 — NormalizeRank inverted (positive min_relevance kept the weakest)
- pkg/sqlite/fts5.go:206 — score = |rank|/(1+|rank|): rises with match
  strength (rank -9 → 0.9, not 0.1). manager.go's MinRelevance filter and
  distill.go's DefaultDistillMinRelevance now behave as documented. Test
  table updated to the monotonic expectations.

### Also landed by parallel sessions during this wave (audited, not mine)
- c6c07f0b: C1 (above) + prior-wave report committed.
- aca55f78/b3eb4878/892efbe3: chat-reply catalog-dump guard, sync-reply step
  result, errored-step review gating (F2).
- 7ddc56ea + 94cd7b53: lint sweeps (behavior-preserving; 94cd7b53's
  errors.AsType on the streaming retry-after path is a slight improvement).
- 6ad221a0: agent.result annotated as intentional orphan in the graphs.

## Found, NOT fixed (with rationale)

| ID | Location | Why left |
|----|----------|----------|
| D-H1 | duplicate-search rollback can spin at iteration 1 (no rollback-attempt cap) | Needs a per-turn rollback cap + tests; agent-loop policy change, next window. |
| D-H3 | guard state (NoProgressLadder/searchRollbk/reasonWatch) never resets between turns | Reset() exists but no caller; wiring point + cross-turn tests needed; next window. |
| D-M3 | emitResumeEvent "waited" measured against soft-stopped ResumeAt | Display-only; ParkedAt rides the payload but is unused — one-line next wave. |
| A-M | deltas duplicate across provider rotation (PM re-invokes onDelta per attempt) | Structural to PM rotation; needs consumer-side contract (reset/accumulate). Design task. |
| A-M | codex HTTP-status errors lack APIError/RateLimitError typing → 429 classification missed | Pre-existing; change + test in the codex error path; next llm window. |
| A-L | no budget pre-flight on codex; session-id format unvalidated; sanitizeOSVersion edge | Low; batch with the next codex touch. |
| G-L | vote store never closed on Manager.Close (FD leak, standalone-db path); accesses=0 dead weight in eviction scoring; FK delete-order hazard | Low; batch with next memory touch. |
| G-latent | consolidation.go:307 dereferences c.manager before its nil check | Latent (all constructors pass a Manager); one-line swap next touch. |
| I-M8 | NEITHER surface parses `reason` — give-up indistinguishable from tier-escalation on the wire; Flutter copyWith is null-preserving so stale badges stick | Root cause beyond M8; needs reason plumbed into both payloads + copyWith nil override; own PR. |
| M3/M4/M5 | (unchanged from round 1) llm rotation ignores NonRetryable; streaming auth-arm; RetryAt off-by-one | Still open; same rationale. |
| M6/M8/M9/M11/M12/M14 + LOWs | (unchanged from round 1) | Same dispositions. |

## Disclosure ledger

- 9 auditor subagents ran; their consolidated report never returned in-session
  (batch mode), but auditors E and D (and a parent-parallel writer) executed
  fixes directly; every diff was reviewed by the parent and is covered above.
- Two mid-session collisions: my ConfigSnapshot insertion landed on top of the
  parallel writer's equivalent (removed mine, kept theirs — theirs was
  nil-guarded); one bad H6 daemon-side edit was self-reverted (empty-if
  noise); park.go had a duplicated constructor tail from concurrent writes
  (repaired).
- internal/llm was hands-off for the parent from mid-wave: first 8 files, then
  a 39-file sibling lint sweep, committed as 94cd7b53/7ddc56ea. M3/M5 fixes
  are therefore missing despite being trivial.
- The H10 canonical-key choice (add unblock_at to the event, keep resume_at
  as alias) reverses the prior wave's suggested direction (change the
  surfaces) — the dual-read on both surfaces makes either producer shape
  safe, so I did both.
- flutter analyze run only on the touched file, not the full app.
- go test run in -short mode; -race and the full (non-short) suite not run
  (prior-wave disclosure still stands).
- NEXT-STEPS.md at repo root will be replaced with this wave's handoff.
