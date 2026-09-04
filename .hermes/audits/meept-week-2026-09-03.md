# Bughunt Audit — Week of 2026-08-27 + Agent Loop

**Date:** 2026-09-03
**Scope:** ~40 commits `1d94e2da..HEAD` (llm-resilience-forest close-out, parkers,
memory fixes, bus demote, config duration keys) + the agent loop, including the
uncommitted working-tree diff on `internal/agent/loop.go`.
**Method:** 7 parallel read-only auditor subagents + parent source-verification of
every CRITICAL/HIGH claim. Baseline: `go build ./...` green; `go test -p 2
-count=1` on all 10 changed package groups green (no cache inflation).
**Mode:** REPORT ONLY. No fixes applied. Fixes require user go-ahead.

---

## CRITICAL (2)

### C1. Worker requeue path returns while holding the mutex — one throttled job permanently deadlocks the worker
- `internal/worker/worker.go:313-326` — **parent-verified**
- `w.mu.Lock()` at 313; the provider-wait branch `if requeued := w.requeueOnProviderWait(...); requeued { return true, nil }` (324-325) exits without `Unlock`. Both sibling branches unlock (330, 356). Worker state also never leaves `StateProcessing` (`Processing→Idle` is not a legal transition).
- Effect: the next `tryProcessJob` blocks on `w.mu.Lock()` forever; `Stop()`, `GetState()`, `GetStats()` hang. One throttle event bricks the worker and blocks daemon shutdown.
- Repro: enqueue a job whose processor returns `llm.ThrottleBackoffError{RetryAt: now+5m}` on a requeueable queue → subsequent `GetState()` hangs.

### C2. Chat throttle parker is never `Start()`ed, and per-session chat loops never receive a parker — chat throttle-parking is dead end-to-end (universal-parking invariant D9 violated)
- `internal/daemon/components.go:2436-2439` + `internal/agent/loop.go:6228-6345` — **parent-verified; independently found by 3 of 7 auditors**
- Leg 1: `throttleParker := agent.NewTurnParker(...)` is constructed, configured, bus-wired, and handed to `SetThrottleParker` — but nothing in the repo calls `.Start(ctx)` (repo-wide grep). `Park()` only appends; `drainDue` runs only from the ticker goroutine `Start()` spawns. `TurnParker.Start` also silently no-ops when `resumeFunc == nil` — which is how this hid from smoke checks.
- Leg 2: `ConfigSnapshot()` (loop.go:6228-6345) propagates `WithInteractiveTurns` but omits `WithTurnParker` — the per-session loops that actually serve chat get no parker; their throttled turns fall back to the visible-error pass-through.
- Net effect on the singleton path (goal/queue loops that DO get the parker): turn returns `("", nil)` — caller sees success — and the record sits parked forever until shutdown drops it with a warn. The user's message is silently lost.
- Repro: force a throttled provider response on a chat turn → turn reports empty success; `Pending()` stays ≥ 1 permanently; no `throttle_resumed` ever fires.

---

## HIGH (10)

### H1. Third TTSR-retry LLM call lacks the parked-turn `(nil, nil)` check — park + error double-execution
- `internal/agent/loop.go:3498-3510` — **parent-verified**
- The first TTSR retry handles parking (`err == nil && response == nil → return "", nil`, 3483-3487); the follow-up call at 3498 only checks `err != nil` and then treats `response == nil` as "empty response" error (3506-3510). `chatWithFailover` returns `(nil, nil)` after a successful park → state machine says parked (`StateQuotaWait`), a resume record exists, yet the caller receives an error and may retry → the turn runs twice (now + on resume).
- Repro: guardrail violated twice, then the third call throttles with a parkable wait → error to user + later duplicate execution.

### H2. Parked turns flow through the entire success pipeline — false reflection/learning data, blank assistant message, worker marked completed
- `internal/agent/loop.go:2397-2489` + `internal/agent/handler.go:832-834` — **parent-verified**
- Park returns `("", nil)`; `err == nil` then gates: reflection records a success trajectory (2397-2416), learning capture fires (2461-2485), `conv.AddAssistantMessage("")` appends a blank entry to history (2489), and the handler sets `worker.State = "completed"` (833).
- Effect: skill learning on incomplete data, history pollution (`user: X → assistant: ""`), and worker accounting that says the turn finished.

### H3. Throttle resume re-adds the original user message — duplicated turn in conversation history
- `internal/agent/loop_park.go:331` + `internal/agent/loop.go:2347-2351` — **parent-verified**
- `resumeThrottledTurn` re-enters `RunOnceWithParts` with the same `conversationID`; `RunOnceWithParts` unconditionally `conv.AddUserMessage(wrappedMessage)`. Resumed context becomes `user: X → assistant: "" → user: X`.

### H4. Employee parker has no per-employee dedup — N scheduler ticks during a provider wait = N duplicate full executions at resume
- `internal/employee/park.go:189-197` — **mechanism parent-verified** (`TurnParker.Park` appends unconditionally, parked_turn.go:249)
- While a tier-1/tier-3 episode is parked, every 15-min scheduler tick hits the same quota error and parks a fresh record with the same resumeAt. At reset, all N drain sequentially, each re-running the full episode. A 24h quota window with a 15-min schedule ≈ 96 identical executions.

### H5. Tier-2 parked episodes resume into a no-op — the episode is silently lost
- `internal/employee/park.go:264-269` + `internal/employee/goal_loop.go:648-654, 1225` — **parent-verified**
- Park assigns tier-2 the phase `"assess"` (the comment claims "tier-2 re-dispatches Assess"); the resume switch calls `l.Assess(...)` and discards the returned candidates. But tier-2's `Assess` only *proposes* — `decideTier2` converts candidates to pending plans and is never reached. Trigger reported success; nothing was produced.

### H6. Employee resume bypasses pause/enable gating — paused employees execute parked episodes
- `internal/daemon/components.go:4045-4052` vs `internal/employee/manager.go:1365-1373` — **parent-verified**
- The resume callback goes straight to `loop.ResumeGoalEpisode(...)` with no `Enabled` check; the normal `Trigger` path rejects paused employees ("employee is paused"). Operator pauses an employee mid-park → bot still executes at resume.

### H7. `migrateSchema` adds `version`/`is_current` as TEXT — version numbering stalls at 9 on migrated memory DBs
- `internal/memory/ftstore.go:198` — **parent-verified (latent: only DBs created before the column existed)**
- Every column is added with hardcoded `"TEXT"` while the fresh DDL (episodic.go:27-29) declares `version INTEGER`. TEXT affinity + `COALESCE(MAX(version), 0)` (manager.go:1438-1442) compares strings: `MAX('10','9') = '9'` → every subsequent StoreVersioned mints duplicate "version 10" rows.

### H8. a13526d8's intent-type fix is a no-op in production — `Intent.Type` is still polluted with agent IDs
- `internal/agent/capability_matcher.go:313` + `internal/agent/capabilities_builder.go:130` — **parent-verified**
- The fix's inverse `DefaultAgent()` mapping is guarded by `len(caps.IntentTypes) > 0`, but the builder unconditionally seeds `spec.ID` as the first intent type — so the guard always passes and `IntentTypes[0]` (the agent ID) wins. The dispatch-log corruption the commit message says it fixed still happens; the test only uses hand-built maps with curated `IntentTypes`.

### H9. The only production `NewHTTPHook` call passes a nil allowlist — every configured HTTP hook is structurally unable to execute
- `internal/daemon/epistemic_wiring.go:219` + `internal/agent/http_hooks.go:151-152` — **parent-verified**
- `isURLAllowed` returns false when `len(h.allowed) == 0`; the config schema has no allowlist field to populate it. Every hook fails before any request is sent ("URL not in allowlist") while wiring logs success. df1cb627's retry fix protects a dead path.

### H10. Park events emit `resume_at`; both surfaces read `unblock_at` — the retry time never renders
- `internal/agent/parked_turn.go:425` vs `internal/tui/app.go:1646` + `ui/flutter_ui/lib/models/api_models.dart:171` — **parent-verified**
- `ParkTurnEvent.ResumeAt` serializes as `resume_at`; TUI parses `unblock_at`, Flutter parses `unblock_at`. The leaf-04 "throttle retry HH:MM" label can never render from a park event (TUI falls back to plain "quota wait"; Flutter renders nothing).

### H11. Throttle park stores conversation_id as session_id — WS filter drops park events for TUI/CLI-originated turns
- `internal/agent/loop_park.go:239-245` vs `internal/comm/http/server.go:567-572` — **parent-verified mechanism; trigger condition per auditor**
- When `currentSessionID` is empty the park falls back to `throttleTurnCtx.conversationID` and stamps it into BOTH `SessionID` and `ConversationID`. The WS filter matches the event's session_id against the client's subscribed session_id; a `conv-*` value never matches. Flutter-origin turns mask this (they pass session_id as conversation_id per the documented convention). Violates the session_id/conversation_id dual-handling invariant.

---

## MEDIUM (14)

| # | Location | Claim |
|---|----------|-------|
| M1 | loop_park.go:332-341 | Throttle-resume failure is log-only — no user surface, unlike the quota path's "Auto-resume failed" push. Combined with C2's `("", nil)`, the message vanishes silently. |
| M2 | loop.go:4334 | `StateBlocked` recovery breaks the error chain (`%s` not `%w` behind `ErrAgentBlocked`) — `errors.As` can't classify quota/budget causes; handler loses park-offer UX. |
| M3 | client.go retry loops + provider_manager.go:416-424 | `NonRetryable()`/`IsNonRetryable` are never consulted in the failover switch — a give-up throttle burns every provider's short-retry budget and records spurious failures. |
| M4 | provider_manager.go:532-571 | `ChatWithProgress` lacks the auth-error arm `Chat` has — 401/403 rotate without marking the provider Unhealthy; divergent health policy per entry point. |
| M5 | client.go:605 vs 1623 | `ThrottleBackoffError.RetryAt` uses a 1-based attempt on the non-stream path and 0-based on the stream path — same throttle parks streaming turns 2× longer (240s vs 120s at defaults). |
| M6 | AGENTS.md:186-188 vs server.go:689-696 | Doc says `chat.response` → `chat_message`; the code deliberately excludes it (would double-deliver). Stale invariant doc — anyone "fixing" the code to match breaks the GUI. |
| M7 | bus.go:124-126, 165-187 | d48d89b7's demote also silences `PublishBlocking` no-subscriber — the documented "security-critical, drops unacceptable" path now fails silently at DEBUG. |
| M8 | tui/agents_panel.go:512-535 | TUI treats a throttle give-up (`to=""`) as quota_cleared → shows "running" after the turn died. Flutter correctly ignores it — surfaces disagree. |
| M9 | tui/quota_status.go:86-91 vs quota_status.dart:35-41,139-143 | HH:MM rendered in three different zones (daemon-local / device-local / UTC) — breaks the parity claim for remote daemons. |
| M10 | components.go:2693-2711 | Single-agent mode (`Orchestrator == nil`) gets neither `SetParkEventBus` nor a throttle parker — silent parking and disabled throttle park in that mode. |
| M11 | config/json5_loader.go:79-96 | Line-based duration rewriting breaks compact JSON5 and skips rewriting after a quoted value on the same line — parse errors on legal configs. |
| M12 | memory/ftstore.go:268-271 | FTS5 rebuild uses an undocumented multi-column INSERT form; failure swallowed at Debug. Works only because the later backfill + trigger re-index compensate. |
| M13 | pkg/sqlite/fts5.go:206-211 + tools/builtin/memory.go | `NormalizeRank` decreases as matches improve — any positive `min_relevance` floor filters the STRONGEST matches first. 7ee95a53's 0.3→0.1 moved the cliff; didn't remove it. |
| M14 | pacing.go:130-160 | `Wait` claims the slot with a pre-sleep `now` and no reservation — concurrent Waits for one provider all wake together (zero enforced gap), and slow requests erase gaps. (Parent independently flagged the same mechanism pre-report.) |

---

## LOW (16, condensed)

1. worker.go:475-486 + 333-348 — give-up quota waits strand fresh jobs in `failed` limbo: not retried, not dead-lettered, unrecoverable.
2. client.go:1152-1157 vs 1691-1700 — RPM reservation leaks on the non-stream concurrency-abort path; `ReleaseRateLimitSlot` pops the most-recent (possibly another request's) slot.
3. runtime_process.go:115-118 — failed PID-file write kills only the group leader (Setpgid) before the reaper exists → zombie + surviving grandchildren; Kill error discarded.
4. errors.go:226-242 vs client.go:2065-2085 — two divergent Retry-After parsers (5m clamp vs none; different format sets) — D6 consolidation incomplete.
5. metrics/store.go:605/638 — `refreshTicker` written outside mutex, read in `Close` without it (data race); `Record`'s ctx arm unreachable due to `default`; stream success latency includes retry backoff.
6. client.go:315-321 — `WithExtraHeaders` stores the caller's map by reference (latent aliasing; no current mutator).
7. runtime_process.go:144-211 — `Stop` never clears `pid`/`cmd` → PID-recycle false-positive in `IsRunning` can SIGTERM an unrelated process group.
8. queue/store.go:578-588 — `Requeue` leaves cluster columns + in-memory claim stale (spurious reclaim events; no corruption today).
9. memory/usefulness.go:202-205 — silent created_at parse swallow, the exact inconsistency 61c21e25 removed elsewhere.
10. memory/ftstore.go:198 — migrated episodic tables lose the `parent_id` FK (ALTER TABLE cannot add it).
11. registry.go:803-808 — `mergeSpec` resets a runtime-disabled spec to enabled when re-load omits `enabled:` (latent; no current producer).
12. http_hooks.go:101-103 — explicit `retry_count: 0` now silently means 3 retries; the `-1` opt-out exists only in a code comment (behavior change, undocumented at the config surface).
13. http_hooks.go:309-323 — `shouldRetryHookError`'s fallback `return true` makes `IsRetryable` dead code; 404s now cost 4 attempts + ~30s backoff.
14. flutter agents_tab.dart:74-99 — WS quota listener leaks on tab re-creation (subscription never stored/cancelled).
15. flutter api_models.dart:167-175 — `model_id` dropped → detail view shows "primary: unknown" (TUI shows it; acknowledged parity gap).
16. server.go:719-723 — `metrics.*`/`job.*` WS classifications unreachable (bridge subscribes neither prefix; zero publishers).

---

## REFUTED during parent verification (1)

- **mergeSpec drops `Constraints.Reasoning` on re-load** (peripherals auditor, HIGH): FALSE. `registry.go:895` does `merged.Constraints = base.Constraints` — a whole-struct value copy that carries `Reasoning` before the field-by-field overrides. The 37a68c22 commit is complete for Reasoning.

## Verified-clean areas

- `slot_gate.go`: 3:1 guard arithmetic, cancel-dequeue accounting, held-count balance under the ctx/token race — all correct (stress test covers both lanes).
- `inuse.go` pre-warm (b96cb8d6): pure function, correct alias-expansion-before-filter order, gate preserved.
- runtime detach (a5312a64): `context.WithoutCancel` bounded by StopAll + health-checker SIGTERM→SIGKILL; no double-close.
- Trace conversion 1d702883: compiler-enforced struct conversion, no field loss. Shadow-test rework 39fcc717: genuinely deterministic.
- Wiring sweep: HTTP-hook retry default, endpoint pre-warm, throttle give-up, StateQuotaWait reachability — all genuinely wired.
- Uncommitted diff: `attemptStateRecovery` change has exactly one production call site (loop.go:3579); blank-stop carve-out degrades safely (non-stream blank content becomes `ErrEmptyResponse` upstream at client.go:1436). One finding against it: M-level — the attached-path contract change (blank + stop now returns whitespace success where it previously errored) is untested and unflagged (auditor #6, M).

---

## Disclosure ledger (what was NOT done)

- Not run: `-race`, `mutexio`/`predid` analyzers, `make lint-ci`, `make graphs-check`, Flutter tests. Findings from code-read only (e.g. metrics ticker race) are marked as such.
- The uncommitted working-tree diff (sibling session, in-flight) was audited as part of the tree and left untouched, as was `cmd/tmp-verify/` (sibling scratch, harmless).
- Memory H7 severity calibrated down from the auditor's framing: it fires only on DBs migrated from a pre-column schema; if no legacy production DB exists, it is dormant.
- H11's trigger condition (conv-keyed TUI/CLI turns) is auditor analysis on a parent-verified filter mechanism; not reproduced end-to-end.
- Two auditor claims were corrected during verification (mergeSpec refutation above; FTS rebuild finding downgraded to undocumented-reliance after the auditor's own empirical probe was denied).
- All 7 auditor reports: parker (12 findings), memory/config (10), wiring sweep (8 features + TODO scan: none new), agent-loop lifecycle (10), peripherals (9), llm resilience (12), surfaces (12). Full texts in `~/.hermes/cache/delegation/subagent-summary-0-20260903_*.txt`.

## Suggested fix order (when authorized)

1. C1 (worker mutex) — 2-line fix, unblocks everything queue-related.
2. C2 (parker Start + ConfigSnapshot parker propagation) — one `Start(ctx)` call + one option line; restores the D9 invariant.
3. H1+H2+H3 (park contract cluster in loop.go) — same file, one coherent fix: suppress success-path side effects on park, add the `(nil, nil)` check at 3498, pass a resume flag so resume skips AddUserMessage.
4. H5+H6+H4 (employee parker: tier-2 phase, Enabled check, dedup key).
5. H8+H9 (one-line guards: filter spec.ID from IntentTypes[0]; wire an allowlist or fail wiring loudly).
6. H7 (TEXT→INTEGER migration types) + H10/H11 (wire-format alignment).
