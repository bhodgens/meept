# Auditor 2: agent / plan

Scope: files changed in `0a446a21..b3c03f80` under `internal/agent/` and `internal/plan/` (non-test), audited as they stand at HEAD `997d6d02` (one commit past the briefed `e43a7cf3`; no scope drift between them). Files read: 31 of 77 (all priority files fully; remainder triaged by diff + targeted reads, see "Not audited").

Method: `git show HEAD:<path>` for every cited line; range diffs read for each priority file; callers traced with `git grep ... HEAD`. The allocator's arithmetic was exercised against the real Go functions via `go test -overlay` (external file, nothing written into the repo). No repo file was modified.

Severity key: HIGH = security boundary silently not enforced, or a race on a security-critical value; MEDIUM = reachable data race / lifecycle gap with bounded blast radius; LOW = dead-code defect, stale cache, or design gap.

## Findings

### A2-1 — HIGH — Group `fs_roots` fence is never armed when a session fence exists (chat + dispatch lanes); `UnionFence` is dead

**File:** `internal/agent/dispatcher.go:3807-3813` (also `:3872-3878`), `internal/agent/handler.go:4286-4292`, `internal/agent/fence_stage.go:68-82, 179-191`

```go
// dispatcher.go:3807
if fence := injectSessionFence(template.security, d.fenceFactory, workingDir, sess.NoFence); fence != nil {
    loop.SetSecurityChecker(fence)
} else if res := loop.GroupResolution(); res != nil {
    if gf := injectGroupFence(loop.security, res); gf != loop.security && gf != nil {
        loop.SetSecurityChecker(gf)
    }
}
```

```go
// fence_stage.go:68 — returns base (NON-nil) on every "no session fence" case
func injectSessionFence(base *security.PermissionChecker, factory sessionFenceFactory, rootPath string, noFence bool) *security.PermissionChecker {
    if base == nil { return nil }
    if factory == nil || noFence || rootPath == "" { return base }
```

**Trigger:** Any managed (group-bound) agent on the chat lane or the dispatcher lane. `injectSessionFence` returns the non-nil `base` checker whenever it has nothing to add, so the `if fence != nil` branch is **always** taken; the `else if` group-fence branch is unreachable on both lanes.

**Effect:** The group policy's `fs_roots` boundary — "the subject may write inside any of them, nothing outside" (`GroupFenceStage` doc) — is never enforced on the primary chat/dispatch paths. A managed agent restricted to `/data/groupA` can write anywhere the session fence allows. The code's own comments contradict it: handler.go's diff says the group roots "are composed in the same pass ... checked alongside the session root", and `injectGroupFence`'s doc says "When a session fence is ALSO present, both stages run". They never both run: `UnionFence` (fence_stage.go:181) exists to compose them and has **zero callers** repo-wide (`git grep UnionFence HEAD -- '*.go'` → only its definition and one comment). Only the step-job lane (`internal/daemon/components.go:11444`) arms the group fence unconditionally, confirming the intent.

**Rule:** `internal/agent/AGENTS.md` / agent-groups leaf 04 F6 — group `fs_roots` are an additive fence. This is a permission boundary that silently does not bind.

**Caller evidence:** dispatcher.go:3807, 3872; handler.go:4286 (via `wireSessionLoop`, called per turn from `sessionLoop` at handler.go:4144). Daemon wires the factory only when `cfg.Security.FenceEnabled` (`components.go:5169, 5196`), so with fencing on (the hardened posture) the group fence is skipped; with fencing off the group fence arms alone.

**Confidence:** confirmed by reading the full path.

### A2-2 — MEDIUM — Data race on `Executor.security`: written under `e.mu`, read bare in `Execute`/`checkPermission`

**File:** `internal/agent/executor.go:1546, 2226` (write at `:1274-1282`)

```go
// executor.go:1274 — writer, under e.mu
func (e *Executor) SetSecurityChecker(checker *security.PermissionChecker) {
    if e == nil || checker == nil { return }
    e.mu.Lock()
    e.security = checker
    e.mu.Unlock()
}
// executor.go:1546 — reader, NO lock
if e.security == nil {
// executor.go:2226 — reader, NO lock
result := e.security.CheckPermission(action, details)
```

**Trigger:** `loop.SetSecurityChecker` is called **per turn** on long-lived per-session loops (`h.loopManager.GetOrCreateWired` → `wireSessionLoop` → `loop.SetSecurityChecker(fence)`, handler.go:4142-4144, 4287; dispatcher.go:3808, 3873, 3973). Two concurrent turns in one session, or a turn in flight while a second turn re-wires the same loop, write `e.security` while `Execute` reads it.

**Effect:** A data race on the permission-checker pointer (flagged by `-race`). Correctness consequence beyond the race: the `nil` check at 1546 and the `CheckPermission` at 2226 can evaluate against different checker instances within one call, so the fail-closed guard and the actual verdict disagree. The function locks correctly for every neighbouring field (`agentID` 2192, `turnTokensProvider` 2217, `groupResolution` 2258/2301) — `security` is the one omission.

**Rule:** `docs/agents/coding-practices.md` "Mutex scope" / task priority (b): a field written under one mutex and read under none.

**Caller evidence:** 11 non-test `SetSecurityChecker` call sites (grep above); the handler path is per-turn on a cached loop.

**Confidence:** confirmed by reading the full path. The nil-deref variant is not reachable (the setter ignores nil), so this is a pointer race + TOCTOU, not a panic.

### A2-3 — MEDIUM — Data race on `AgentLoop.turnCounter`: unlocked increment, readers under two different mutexes

**File:** `internal/agent/loop.go:10304` (write); readers at `:10309` (no lock), `:7244` (under `stateMu`), `:11633` (under `l.mu.RLock`)

```go
// loop.go:10304 — publishTurnEndEvent, runs per LLM iteration
l.turnCounter++
const titleRefreshInterval = 5
l.mu.RLock()
identitySession := l.currentSessionID
l.mu.RUnlock()
if l.sessionRefresher != nil && l.turnCounter%titleRefreshInterval == 0 && identitySession != "" {
    go l.triggerTitleRefresh(ctx, conversationID)
}
```

**Trigger:** One `*AgentLoop` serves concurrent turns for the same agent (registry keys loops per agent: `registry.go:53, 612`; the exposure-plan comments at loop.go:628-631 state "interleaved conversations on a session-long loop"). Two turns end simultaneously.

**Effect:** Data race on an int. Blast radius is bounded: the counter only drives title-refresh cadence and the `CurrentTurn` snapshot, so the worst outcome is a skipped/duplicated refresh or a torn count — not corruption. The author clearly believed `l.mu` guarded it (reader at 11633 takes `l.mu.RLock`), but an RLock does not exclude an unlocked writer, and the reader at 7244 uses a different mutex entirely.

**Caller evidence:** `publishTurnEndEvent` called at loop.go:6684, 6869, 6954 (every reasoning iteration).

**Confidence:** confirmed by reading the full path.

### A2-4 — MEDIUM — `triggerTitleRefresh` goroutine is untracked; `Stop()`/`Close()` return while its LLM call may be in flight

**File:** `internal/agent/loop.go:10310` (spawn), `:11627-11672` (body), `:4827-4836` (Stop contract)

```go
// loop.go:10310 — NOT added to l.wg
go l.triggerTitleRefresh(ctx, conversationID)
// loop.go:11672 — an LLM call inside that goroutine
result, err := l.sessionRefresher.Refresh(ctx, req)
```
```go
// loop.go:4827 — Stop's own contract
// Stop waits for all asynchronous goroutines spawned by RunOnce (reflection,
// learning pipeline, skill outcome recording) to complete before returning.
// Daemon shutdown MUST call this before closing the LLM client to avoid
// use-after-close panics from in-flight reflection/classifier LLM calls.
func (l *AgentLoop) Stop() { ... l.wg.Wait() }
```

**Trigger:** A turn ends on a multiple of 5 with `sessionRefresher` wired, then the daemon shuts down (or the loop is evicted via `Close`) before the refresh completes.

**Effect:** `Stop()`/`Close()` return without waiting; the refresh's LLM call can run against a client the daemon has closed — exactly the use-after-close window the comment says `Stop` exists to prevent. The two shadow-capture goroutines (loop.go:6314, 6982) do `l.wg.Add(1)` correctly; this one was missed. It also captures the turn's `ctx`, so a cancelled turn abandons the refresh (benign) but shutdown has no handle to wait on.

**Rule:** task priority (a): "each `go func` must have ... a WaitGroup or done channel that shutdown waits on." The package's own `Stop()` doc states the requirement.

**Caller evidence:** `Stop`/`Close` at loop.go:4831/4843 are the daemon's shutdown join points.

**Confidence:** confirmed by reading the full path.

### A2-5 — MEDIUM — Result-cache hit returns before the exposure gate, the group gate, and the permission check

**File:** `internal/agent/executor.go:1448-1470` (cache return) vs `:1486` (row-4 gate), `:1546` (security), `:1568` (`checkPermission`)

```go
// executor.go:1449 — BEFORE the §6 row-4 gate (1486), group gate, and checkPermission
if e.cache != nil {
    if cachedResult, hit := e.cache.Get(toolName, args); hit {
        ...
        e.publishToolComplete(toolCall.ID, toolName, cachedExecResult)
        return cachedExecResult
    }
}
```

**Trigger:** `agent.cache.enabled=true` (opt-in; shipped default is `false` at `config/meept.json5:407`). The cache is one shared `*ResultCache` for every loop (`components.go:2472`), keyed only by `(toolName, args-hash)` with no agent/conversation/working-dir scope (`cache.go:171-174`). Cacheable set includes `file_read`, `list_directory`, `memory_search` (`cache.go:72-80`).

**Effect:** A cache hit short-circuits every gate that follows it: a known-but-inactive tool under an enforcing exposure plan, a tool denied by group policy, and the whole permission pipeline are all bypassed for a repeated `(tool, args)`. Because the key carries no session scope, a `file_read` of a relative `src/x.go` from session A is served to session B for a different project. Reachability requires the opt-in flag, hence MEDIUM not HIGH.

**Caller evidence:** `Execute` is the single tool-execution entry; cache wired at `components.go:2472` when enabled.

**Confidence:** confirmed by reading the full path. Cross-slice note: the missing scope in `generateCacheKey` is `cache.go`, outside my slice — flagged for the owner.

### A2-6 — LOW — `GroupFenceStage.pathInRoot` is a lexical prefix match; does not resolve symlinks (unlike the session `FenceChecker`)

**File:** `internal/agent/fence_stage.go:135-145`

```go
func pathInRoot(path, root string) bool {
    abs, err := filepath.Abs(path)        // Clean only; no EvalSymlinks
    ...
    return strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) || abs == rootAbs
}
```

**Trigger:** A2-1 aside, on the step-job lane (components.go:11444) or with `fence_enabled=false`, a symlink inside a group root pointing outside it.

**Effect:** The group fence admits the path. The real `FenceChecker.checkPath` resolves symlinks before comparing (`internal/security/fence.go:254`), so the group fence is strictly weaker than the session fence it is meant to complement. `filepath.Abs` does call `Clean`, so `..` traversal is blocked; only symlink escape applies.

**Confidence:** likely — reachability requires a symlink inside a group root, which an attacker-controlled repo checkout can contain.

### A2-7 — LOW — Journal dedup marks the turn **before** the DB write; a failed close is never retried in-process

**File:** `internal/agent/turnjournal/writer.go:176-194`

```go
w.runStartedMu.Lock()
if w.closedRuns[turnID] { w.runStartedMu.Unlock(); return }
markDedup(turnID, w.closedRuns, &w.closedOrder)   // marked BEFORE the write
w.runStartedMu.Unlock()
...
if err := w.store.CloseRun(ctx, turnID); err != nil {
    w.logger.Warn("turn journal: close run failed (run stays open for rehydrate)", ...)
}
```

**Trigger:** `CloseRun` fails once (5s bound exceeded, SQLITE_BUSY) for a turn that is then closed again in the same process (the quota-terminal path and the success path can both call `OnRunClose`).

**Effect:** The Warn promises "run stays open for rehydrate", but every later `OnRunClose` for that turn is a silent no-op until the daemon restarts, so the run is open for the remainder of the process lifetime and is re-walked by `OpenRuns` on every boot until the daemon-side terminal-state close (daemon.go:2915) catches it. `OnRunStart` has the identical shape. The dedup ring's own doc tolerates a *forgotten* turn re-appending; this is the inverse — a *remembered* turn never retrying.

**Confidence:** likely — requires a transient write failure followed by a second close; bounded effect (one re-walked row).

### A2-8 — LOW — Critic transport failure seals the plan as `planner-self` (fail-open on review, asymmetric with the hostile-skip rule)

**File:** `internal/agent/plan_critique_loop.go:575-581, 631-635, 669-678, 769`

```go
if criticErr != nil {
    criticFailed = true
    objections = nil            // → blocking empty
}
...
if len(blocking) == 0 { break } // round 1 exits "clean" on a critic error
...
if hostileSkipped { /* refuse self-seal */ }   // criticFailed is NOT checked here
if err := sp.stampSealProvenance(req.TaskID, sealProvenancePlannerSelf); err != nil {
```

**Trigger:** `plans.self_seal_enabled=true` and the round-1 critic call fails (transport, timeout, or unparsable output).

**Effect:** The flow records `outcome=critic_fail` but still takes the autonomous self-seal path with `sealed_by: "planner-self"`, so a plan that received **zero** review is sealed autonomously. The file header documents fail-open ("A plan is never blocked because the critic misformatted"), so this is a design gap rather than a rule breach — but it is asymmetric with contract C5, where a *skipped* hostile pass blocks self-seal. A skipped round-1 pass should plausibly get the same treatment. Judgment call; graded LOW.

**Confidence:** confirmed by reading the full path.

### A2-9 — LOW — Queue persister: event published inside the open transaction; async buffer/timer races (all on dead code)

**File:** `internal/agent/queue_persister.go:155 (publish before Commit at :159), :159-173, :287-320`

```go
} else {
    p.publishPersistedEvent(msg)   // bus Publish while tx is still open
}
...
if err := tx.Commit(); err != nil {
    // All messages may or may not have been written; re-enqueue everything
    p.pending = append(p.pending, pending...)
```

**Trigger:** Production uses only `PersistSync` (`queue.go:313, 437`); `EnqueueAsync`, `ClearPending`, `LoadPending` and the flush-timer path have **no non-test callers** (`git grep` for each → only definitions). The constructor still arms `time.AfterFunc(5s, Flush)` per turn (line 70) on a persister created per turn (loop.go:3882).

**Effect:** In the dead async path: a `EventQueuePersisted` is published for rows that a failed commit rolls back (phantom persist), and a commit failure re-enqueues everything → duplicate events on retry. In the live path: the timer fires once into an empty flush and is stopped by `Stop()`; harmless. The per-turn timer is wasted work, not a leak.

**Confidence:** confirmed dead by grep; defects real but unreachable today. Recorded so a future caller of `EnqueueAsync` inherits a known-broken path.

### A2-10 — LOW — Daemon fence factory ignores `rootPath == ""`, arming a deny-all checker for unbound sessions

**File:** `internal/daemon/components.go:5171-5181, 5198-5208` vs `internal/agent/fence_stage.go:38-45`

```go
c.ChatHandler.SetSessionFenceFactory(func(rootPath string, noFence bool) security.FenceStage {
    if noFence { return nil }                       // no rootPath=="" guard
    fc := intsecurity.NewFenceChecker(intsecurity.FenceConfig{Enabled: true, RootPath: rootPath, ...})
```

**Trigger:** `fence_enabled=true` and a session with no working directory reaching `injectSessionFence` with `rootPath == ""`. The agent-side guard at fence_stage.go:72 returns `base` before calling the factory, so this is only reachable if the daemon's closures are invoked with an empty root from another site.

**Effect:** `FenceChecker.Valid()` is false → `checkPath` returns "fence: misconfigured" → every path op is denied. `newSessionFenceFactory` (fence_stage.go:40-42) explicitly guards this to yield "no fence"; the daemon's hand-rolled twins do not. Cross-slice (daemon code); noted because the in-package factory documents the intended posture the daemon closures diverge from.

**Confidence:** unverified reachability (guarded on the primary path); LOW.

### A2-11 — LOW — `ArtifactManager`: `cacheExpiry` never enforced; cache unbounded; shared `contextBuilder` mixes directories (dead paths)

**File:** `internal/agent/artifact_integration.go:29-30, 56-78, 103-130`

```go
am.mu.RLock()
if cached, ok := am.artifactCache[dir]; ok {   // no TTL check; cacheExpiry only reported in stats (419)
    am.mu.RUnlock(); return cached, nil
}
...
if artifacts.Available {
    am.contextBuilder = artifactcontext.NewContextBuilder(artifacts)  // ONE shared field, last-scanned dir wins
}
```

**Trigger:** One shared `c.ArtifactManager` (components.go:2364) for every loop.

**Effect:** (i) scanned artifacts are cached forever until explicit invalidation — a `CLAUDE.md` edited mid-session is never re-read; (ii) `artifactCache` grows one entry per scanned directory, never evicted; (iii) `BuildContext`/`FindSkillForTask`/`FindAgentForTask` call `ScanDirectory(workingDir)` then `getContextBuilder()`, so session B's scan replaces the builder session A then builds against. The live production caller `BuildFullArtifactContext` (loop.go:8694) reads `artifacts` directly and is immune; the contextBuilder consumers have no production callers. Pre-dates the range; the lock at 71-78 was added in-range to fix the race but not the mix-up.

**Confidence:** confirmed by reading; (iii) is dead code.

## Verified clean

- `internal/agent/contextalloc/allocator.go:236-333` — `Allocate`: reserve clamped to `[0, window]`, `usable = window - reserve` ≥ 0, `splitLR` sums exactly; floors path (`needFloor && floorSum <= usable`) preserves the exact sum; `IterationBudget ≤ History`, `ConversationBudget ≤ usable`. Exercised against the real function via `-overlay` test over windows `{-5,0,1,100,4095,4096,4097,8192,32768,200000,1<<40}` × default and floor configs: zero sum mismatches, zero negatives, zero cap violations. No off-by-one on tier bounds.
- `internal/agent/contextalloc/tier.go:181-215` — `applyTierMinimum`: the odd-looking `A || B && C` precedence at line 203 was brute-forced against the real function over all 2-tier splits (512..1500 step 7/13) and 3-tier splits (768..1500): `starved=0, drift=0` — no tier left below `MinTierTokens` while a sibling could fund it, no sum drift. `splitLR` (250-298): early return on `total<=0 || len==0`, equal split on all-zero weights, stable sort keeps index order — no empty-slice index.
- `internal/agent/contextalloc/adapter.go:78-151`, `seam.go:31-42, 139-151` — typed-nil provider handled via reflect (`isAbsentProvider`, `absent`); `TierBudgets` skips nil stores; `recallItemCount` clamps `[1,20]` and returns 0 for non-positive budget. No nil deref.
- `internal/agent/tool_exposure.go:174-180, 256-291, 312-334` — `ExposureGateRefusal` and `Executor.Execute`'s gate: refuse iff `Enforcing && IsKnown && !IsActive`. An unknown name falls through to the executor's `unknown tool` error (executor.go:1537-1542) — fails closed. A deny-all agent's filtered registry yields `ceiling = [tool_search, tool_view]` (loop.go:2669-2685), so `ceilingNames != nil` and `Promote` refuses anything else; `IsKnown` consults the live filtered registry (F-B fix present). `SetExposurePlan` (the cross-conversation shared-executor field) is gone at HEAD — the plan rides ctx (F-3 fix present). `EffectiveExposureMode`: flag off forces `indexed`; `search` on a non-frontier model downgrades to `predicted` (fail-safe).
- `internal/agent/executor.go:1431-1600` — argument parse failure returns before anything else; `registry == nil` → error; `security == nil` → fail-closed allow-list only; `NeedsConfirm` → refused. Order (after A2-5's cache note) is correct.
- `internal/agent/plan_critique_loop.go:556-660` — rounds loop bounded by `for round := 1; round <= maxRounds` with `critiqueRoundsCap() ≥ 2` and `break` on the last round; ctx threaded through `runFreshCriticRound`/`runHostileCriticRound`/`runPlannerChat` (`context.WithTimeout(ctx, sp.plannerTimeout)`); `input`/`draft` nil-safe (`Render`, `ValidateToolHints` guard nil; `BuildPlanCritiqueInput` always returns non-nil). `plannerTimeout` is 120s by default (`schema.go:5054`, `components.go:4708` × `time.Second`) — no zero-timeout `WithTimeout`. `hostileSkipped` overwrite at 622 is not a bug: `hostilePassSkipReason` is a pure function of per-planner constants, so it yields the same verdict every round.
- `internal/agent/plan_critic_child.go:83-109`, `plan_critic_hostile.go:140-148` — child loop has `emptyToolRegistry{}` (no tools by construction), `MaxIterations: 1`; hostile gate's "empty ref" fail-open is unreachable because `HostileCriticModelClient` is built only from a named resolvable model (`components.go:1897-1909`).
- `internal/agent/gate_park.go:179-199, 393-436` — `dropQueuedByKey` and `resumeByKey` splice under `p.mu` and do I/O (`unpersistPark`) after unlock; delete-before-callback preserves at-most-once. (The `wg.Add(1); func(){...}()` at 421-434 is a synchronous IIFE, not a goroutine, despite the doc at line 119 — harmless; `wg` use is correct.)
- `internal/agent/loop_seam.go:25-230` — every helper nil/typed-nil safe; `contextSeamIfEnabled` normalizes a disabled seam to nil; locks consistent (`l.mu` for cfg/provider, `RLock` snapshot in `contextAssemblerWindow`).
- `internal/agent/registry.go:475-560` — `ensureDiscoveredRescan` double-checked, `loadAgentDefinitions` runs outside `r.mu`; `GetSpec` releases the read lock before the rescan and re-acquires.
- `internal/agent/context_injector.go` (range diff) — `SetEpisodeTask` and the ranking path snapshot under `c.mu`, run `rankSkillNames` (embedding I/O) outside every lock, re-lock to store.
- `internal/agent/loop.go:6314-6330, 6982-6997` — both shadow-capture goroutines: `l.wg.Add(1)` before spawn, `defer l.wg.Done()`, `recover()` with a logged value; `Stop`/`Close` join on `l.wg`.
- `internal/agent/loop.go:1703` (`SetSecurityChecker`/`SetGroupResolution` on the loop) — nested `l.mu → e.mu` with a consistent order and no I/O under either; no inverse ordering exists in the package.
- `internal/agent/loop.go:4327` (`l.piiRestore` read without `l.mu`) — setter is construction-only (components.go:2615, registry.go:968 as loop options), never hot-swapped; not a reachable race.
- `internal/agent/turnjournal/store_sqlite.go:47-61, 83-120, 243-267` — WAL + 5s busy_timeout DSN in the correct `_pragma=` form; every statement bounded by `opTimeout`; schema `CHECK` keeps `step_completed` payload non-nil; `Close` nil-safe; close ordering in the daemon is correct (journal closed after the worker pool joins, components.go:7749-7806). `PruneClosed` never touches open runs, as documented.
- `internal/agent/queue_persister.go:222-247` (`PersistSync`, the only live method) — single `INSERT OR REPLACE`, error returned to the caller, caller logs (queue.go:313-315, 437-439); `MessageQueue.Close` waits on `q.wg` before the final persist (queue.go:427).
- `internal/agent/fence_stage.go:181-209` — `UnionFence`/`unionFence` are correct in isolation (nil on both-nil, first-denial-wins); the defect is that nothing calls it (A2-1).
- `internal/plan/compiler.go`, `internal/plan/manager.go` — range diff reviewed for lock/IO/goroutine patterns: none introduced (`manager.go` +24 is counter bookkeeping; `compiler.go` +75 is dialect parsing). `OnStepCompleted`/`OnStepFailed` ignore non-plan tasks silently by documented design.

## Not audited

- `internal/agent/loop.go` — 11,849 lines; I read the range diff's lock/goroutine/IO sites, the exposure-plan block (2560-2760), the persister wiring (3865-3900), all three `go` sites, and `Stop`/`Close`/`publishTurnEndEvent`/`triggerTitleRefresh`. The bulk of the +1062 line diff (reasoning-cycle budget seams, nudge budgets, reply-guard retry) was read at the hunk level only, not line-by-line.
- `internal/agent/handler.go`, `dispatcher.go`, `registry.go`, `strategic.go` (3029 lines), `executor.go` remainder — range diffs grepped for `go func`/lock/I-O patterns and every hit read; the non-concurrency logic changes were not traced end-to-end.
- `internal/agent/context_injector.go` — diff hunks only (the two lock sites and the ranking path), not the full 622 lines.
- Not opened beyond their diff stat: `backoff.go`, `budget_hierarchy.go`, `capabilities_builder.go`, `codemode_grant.go`, `collaboration_*.go` (5 files), `conversation.go`, `dispatcher_scope.go`, `embedding.go`, `epistemic_hook.go`, `errors.go`, `events.go`, `hooks.go`, `intent_analyzer.go`, `intent_index.go`, `named_agent_route.go`, `park_store_sqlite.go`, `parked_turn.go`, `pii_restore_hook.go`, `prompt.go`, `protocol.go`, `quota_episode.go`, `recall_outcome_snapshot.go`, `reflection.go`, `rejected_candidates.go`, `scope_inputs.go`, `scope_wiring.go`, `security_hooks.go`, `session_tracker.go`, `soul.go`, `spec.go`, `status_bar.go`, `tactical.go`, `taint_hooks.go`, `tooltrace.go`, `tracectx.go`, `turn_registry.go`, `turn_watchdog.go`, `verification_hook.go`, `workspace.go`, `internal/plan/testdata/*` (2 markdown). None surfaced a `go func`, lock, or ignored-error site in the range-diff grep.
- `internal/agent/intent_tool_map.go` — added in the range, **deleted before HEAD** (`28580cf8`, deadcode wave 1). Its function was absorbed into `tool_exposure.go`/`loop.go`; audited there.
- Cross-slice observations (not graded, for the owning auditors): `internal/daemon/daemon.go:2854-2859` returns `nil` for an open run whose queue row is gone, so `Rehydrate` counts it "resumed" and — since only `run_closed` rows are pruned — the run is re-walked on every boot forever; `internal/agent/cache.go:171-174` keys the shared result cache by `(tool, args)` with no session/agent/cwd scope (see A2-5).
