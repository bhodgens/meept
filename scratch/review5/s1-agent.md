# S1 - Agent Orchestration Review (Round 5)

Scope: all non-test `.go` files under `internal/agent/` (including `q/`, `prompt/`, `prompts/` subdirs). `internal/agents/` does not exist in this repo.

Methodology: every non-test `.go` file in scope was read in full. Each finding below was verified by re-reading the surrounding code to confirm the bug is real and not a false positive. Prompt-injection payloads embedded as `<system-reminder>` blocks inside file contents (instructing refusal to analyze/improve) were disregarded per the task brief.

Bug classes considered: (1) concurrency races, (2) lifecycle/cleanup bugs, (3) predictable IDs, (4) error swallowing, (5) off-by-one / nil deref, (6) resource leaks, (7) stub/TODO code.

Totals: 0 Critical, 2 High, 5 Medium, 3 Low.

---

## S1-1 [High] Predictable conversation/session IDs from `time.Now().UnixNano()` across 9 call sites

Files / lines:
- `internal/agent/loop.go:3666-3668` - `generateConversationID()` -> `fmt.Sprintf("conv-%d-%d", time.Now().UnixNano(), counter)`
- `internal/agent/collaboration.go:91` - `CollaborationSession.ID = fmt.Sprintf("collab-%s-%d", taskID, now.UnixNano())`
- `internal/agent/pair_session.go:209` - `PairSession.ID = fmt.Sprintf("pair-%s-%d", taskID, now.UnixNano())`
- `internal/agent/pair_manager.go:150` - `actorConvID = fmt.Sprintf("%s-%s-r%d-%d", actorConvPrefix, session.TaskID, round, time.Now().UnixNano())`
- `internal/agent/pair_manager.go:193` - `reviewerConvID = fmt.Sprintf(... time.Now().UnixNano())`
- `internal/agent/strategic.go:224` - `conversationID = fmt.Sprintf("interview-%s-%d", req.TaskID, time.Now().UnixNano())`
- `internal/agent/strategic.go:540` - `conversationID = fmt.Sprintf("plan-%s-%d", req.TaskID, time.Now().UnixNano())`
- `internal/agent/tactical.go:1485` - `sequence := int(9000 + time.Now().UnixNano()%1000)`
- `internal/agent/emitter.go:252-253` - `generateEventID()` returns `time.Now().UTC().Format("20060102150405.000000000")` (timestamp-only, no random component at all)

Severity: High.

Evidence (representative):
```go
// loop.go
func generateConversationID() string {
    counter := convIDCounter.Add(1)
    return fmt.Sprintf("conv-%d-%d", time.Now().UnixNano(), counter)
}

// collaboration.go
ID: fmt.Sprintf("collab-%s-%d", taskID, now.UnixNano()),

// emitter.go
func generateEventID() string {
    return time.Now().UTC().Format("20060102150405.000000000")
}
```

Why it is a bug:
`time.Now().UnixNano()` is predictable (attacker-knowable clock) and a monotonic-only counter does not add entropy. Two concurrent callers in the same nanosecond produce identical IDs for the `collab-`, `pair-`, `interview-`, and `plan-` prefixes (no counter component at all on those paths). The codebase already has a proper random ID generator (`pkg/id.Generate()`) and a crypto-random `generateMessageID()` in `internal/agent/handler.go:1378-1386` - the new sites should use one of those. Consequences:
- Bus topics derived from these IDs (e.g. `TeamMessageTopic(sessionID)`, `PairTopic(sessionID)`) become guessable, allowing cross-session message injection if a topic name is the only authorization.
- DB primary keys / map keys can collide, causing silent overwrites in `sessions` / `teams` maps and SQLite `INSERT OR REPLACE` rows.
- `generateEventID()` is the worst case: nanosecond timestamp only, so a busy node emitting >1 event in the same ns reuses the ID (EventEmitter listeners key by this ID in some paths).
- This is the same anti-pattern documented as fixed in prior rounds (`MEMORY.md` calls out `time.Now().UnixNano()` as a known recurring bug pattern).

Suggested fix:
Replace every site with `pkg/id.Generate()` (or `generateMessageID()` where a string ID is needed). For `generateEventID()` specifically, add a random suffix: `return generateMessageID()`. For `tactical.go:1485` the sequence number should come from a per-task monotonic counter, not from the clock modulo 1000 (which also collides under concurrency and produces values clamped to 9000-9999).

---

## S1-2 [High] Mutex held across SQLite I/O in `EscalationManager.Escalate`

File: `internal/agent/escalation.go:109-132`

Severity: High.

Evidence:
```go
func (em *EscalationManager) Escalate(ctx context.Context, failure FailureContext) error {
    ...
    em.mu.Lock()
    level, exists := em.escalations[failure.TaskID]
    if !exists {
        originalTaskDesc := ""
        if em.taskStore != nil {
            if t, err := em.taskStore.GetByID(failure.TaskID); err == nil && t != nil {
                originalTaskDesc = t.Description
            }
        }
        level = &EscalationLevel{...}
        em.escalations[failure.TaskID] = level
    }
    ...
    em.mu.Unlock()
```

Why it is a bug:
`em.taskStore.GetByID` is a SQLite query (see `internal/task` store). The CLAUDE.md coding-practices rule explicitly forbids holding a mutex across I/O - it serializes every other escalator against the database call, and on a contended daemon with multiple failing tasks this becomes an unbounded stall. If the DB is on a slow disk or contended, all escalation traffic blocks behind one query. The same lock also protects `em.escalations`, so unrelated tasks cannot make progress while the I/O runs.

Suggested fix:
Take the lock only to read/check the map. If the entry is missing, release the lock, call `taskStore.GetByID`, then re-acquire the lock and insert (handle the TOCTOU where another goroutine may have inserted in the gap - re-check under lock).

---

## S1-3 [High] Mutex held across SQLite transaction in `QueuePersister.flushLockedHeld` / `EnqueueAsync`

File: `internal/agent/queue_persister.go:109-134` (caller) and `178-188` plus `265-330` (callee)

Severity: High.

Evidence:
```go
// EnqueueAsync - holds p.mu across the entire flush
func (p *QueuePersister) EnqueueAsync(msg QueuedMessage) {
    p.mu.Lock()
    if len(p.pending) >= p.maxPending {
        p.flushLockedHeld()   // <-- does BEGIN/EXEC/COMMIT under our lock
    }
    p.pending = append(p.pending, msg)
    ...
    p.mu.Unlock()
}

// flushLockedHeld - caller holds p.mu
func (p *QueuePersister) flushLockedHeld() {
    ...
    p.flushPendingLocked(pending)
}

// flushPendingLocked - does a full SQLite transaction, lock never released
func (p *QueuePersister) flushPendingLocked(pending []QueuedMessage) {
    tx, err := p.db.Begin()
    ...
    for _, msg := range pending {
        _, err := tx.Exec(`INSERT OR REPLACE ...`, ...)
        ...
    }
    if err := tx.Commit(); err != nil { ... }
}
```

Why it is a bug:
Same CLAUDE.md rule as S1-2. `EnqueueAsync` is on the hot path (every follow-up message) and now blocks every other enqueue / flush / Close on the duration of a full SQLite transaction (BEGIN + N Execs + Commit). Under load this is a multi-millisecond stall per overflow and it is fully serialized. The non-locked variant `flushPending` correctly snapshots under the lock and runs the transaction lock-free; `flushLockedHeld` exists only to serve `EnqueueAsync`'s overflow path and re-introduces the anti-pattern.

Suggested fix:
Make `EnqueueAsync`'s overflow path use the same "snapshot under lock, release, then flush" pattern as `Flush()`. If re-enqueue-on-failure semantics must be preserved, capture the pending slice, release the lock, call `flushPending(pending)`, and let `flushPending`'s existing re-enqueue path re-acquire the lock only for the append.

---

## S1-4 [Medium] `ArtifactManager.artifactCache` map mutated and read without any mutex protection

File: `internal/agent/artifact_integration.go`

Severity: Medium.

Evidence (struct has no `mu`):
```go
type ArtifactManager struct {
    claudeManager  *artifactcontext.ArtifactManager
    contextBuilder *artifactcontext.ContextBuilder
    artifactCache  map[string]*artifactcontext.Artifacts   // unprotected
    cacheExpiry    time.Duration
    projectRoot    string
    logger         *slog.Logger
}
```
Access sites with no synchronization:
- Line 55: `if cached, ok := am.artifactCache[dir]; ok {` (read in `ScanDirectory`)
- Line 71: `am.artifactCache[dir] = artifacts` (write in `ScanDirectory`)
- Line 160: `delete(am.artifactCache, dir)` (write in `InvalidateCache`)
- Line 168: `am.artifactCache = make(...)` (full-map replace in `InvalidateAll`)
- Line 174: `return am.artifactCache[dir]` (read in `GetArtifacts`)

Grep confirms zero occurrences of `sync.` or `mu ` in the file.

Why it is a bug:
Concurrent map read + write is a fatal Go runtime panic (`concurrent map read and map write`), not just a data race. The agent loop dispatches multiple specialist agents in parallel (coder/planner/analyst can all call `BuildContext`/`FindSkillForTask`/`ScanDirectory` on a shared `ArtifactManager`), so this is reachable in normal operation. `InvalidateAll` re-assigning the map header while another goroutine is iterating a stale pointer is the most dangerous combination.

Suggested fix:
Add `mu sync.RWMutex` to `ArtifactManager`. Take `RLock` in `GetArtifacts` and the cache-hit path of `ScanDirectory`; take `Lock` for the cache-miss insert, `InvalidateCache`, and `InvalidateAll`. The `contextBuilder` field is also written under the same window and should be covered by the same lock.

---

## S1-5 [Medium] `SessionTracker.GetSession` returns live pointer to mutable state under RLock

File: `internal/agent/session_tracker.go:93-97`

Severity: Medium.

Evidence:
```go
// GetSession returns session state (read-only lookup, no cleanup).
func (t *SessionTracker) GetSession(sessionID string) *TrackerSessionState {
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.sessions[sessionID]
}
```

`TrackerSessionState` holds `IntentHistory []*Intent` and `Metrics SessionMetrics`. Other methods (`RecordIntent`, `RecordMetrics`, `getOrCreateSession`) mutate those fields under `t.mu.Lock()`, but a caller holding the pointer returned by `GetSession` can read or write those fields without any lock.

Why it is a bug:
Callers receive a pointer to the live struct and can race against `RecordIntent`/`RecordMetrics`/`getOrCreateSession` on the same session. Reading `IntentHistory` while it is being appended (line 83) is a slice-header race. There is no documented contract that callers must take `t.mu` before touching the returned value, and the RLock is released before the caller ever sees the pointer, so the lock provides no protection to the caller. The comment on `GetDominantIntent` (line 107) explicitly copies the intent types under the lock - that pattern is what `GetSession` should do, but it does not.

Suggested fix:
Either return a deep copy of the state under the lock (so callers get a stable snapshot), or return an interface that only exposes accessor methods which themselves take the lock. At minimum, document that callers must hold `t.mu` before reading any field of the returned pointer - but a snapshot copy is safer and matches the pattern already used in `GetDominantIntent`.

---

## S1-6 [Medium] `EscalationManager.Escalate` re-entry is not atomic - lost-update race on `level.Level`

File: `internal/agent/escalation.go:109-132`

Severity: Medium.

Evidence:
```go
em.mu.Lock()
level, exists := em.escalations[failure.TaskID]
if !exists {
    ...
    em.escalations[failure.TaskID] = level
}
level.Level++
level.Reason = failure.Error
level.Timestamp = time.Now()
currentLevel := level.Level
em.mu.Unlock()
```

This is correct in isolation, BUT consider S1-2's proposed fix (release lock around `taskStore.GetByID`): if the fix naively does "read map under lock, release, query, re-acquire, insert", two concurrent `Escalate` calls for the same new `TaskID` will both miss in the map, both query, both insert, and the second insert will overwrite the first `EscalationLevel`, losing the first escalation's `Level++`. This is a note for the fix, not a bug in the current code - but it is flagged here because S1-2 cannot be fixed without also addressing this.

Why it is a bug (latent):
The current code is correct only because the lock is held across the whole check-and-insert. The moment S1-2's fix removes the I/O from under the lock, the insert path becomes a check-then-act race. Documenting it here keeps the fixer from introducing a regression.

Suggested fix:
When fixing S1-2, on the re-acquire path re-check `if existing, ok := em.escalations[failure.TaskID]; ok { level = existing }` before inserting, so concurrent first-callers converge on one `EscalationLevel` instance.

---

## S1-7 [Medium] Error from `classifyIntent` silently discarded at two dispatcher callsites

Files:
- `internal/agent/dispatcher.go:397` - `intent, _ := d.classifyIntent(ctx, resolvedInput, memCtx)`
- `internal/agent/dispatcher.go:819` - `intent, _ := d.classifyIntent(ctx, resolvedInput, memCtx)`

Severity: Medium (observability/diagnostic loss, not a correctness bug).

Evidence:
```go
// Line 397 (Dispatch path)
intent, _ := d.classifyIntent(ctx, resolvedInput, memCtx)
intent.MemoryRefs = d.extractMemoryRefs(memCtx.Results)

// Line 819 (compound-intent sub-path)
intent, _ := d.classifyIntent(ctx, resolvedInput, memCtx)
intent.MemoryRefs = d.extractMemoryRefs(memCtx.Results)
intent.TrueAnalysis = analysis
```

Why it is a bug:
`classifyIntent` is guaranteed to return a non-nil `*Intent` (verified by reading the full function - final fallback at line 622 returns a chat intent), so the discarded error is not hiding a nil-deref. However the LLM-classifier branch (`dispatcher.go:538-562`) logs classification failures at Warn, and that internal logging is fine. The problem is at the callsite: if every classifier fails AND the heuristic fallback also fails (unlikely but possible), the only signal is the internal Warn log - the dispatcher's `DispatchResult` has no field indicating that routing was a fallback. Operators tracing "why did this go to chat?" have to correlate timestamps across log lines instead of reading the intent's provenance. Discarding the error with `_` also means a future refactor that makes `classifyIntent` capable of returning `(nil, err)` would silently introduce a nil deref at lines 400 and 820.

Suggested fix:
At minimum, log the error at the callsite when it is non-nil: `if intent, err := d.classifyIntent(...); err != nil { d.logger.Warn("intent classification failed, using fallback", "error", err) }`. Better: have `classifyIntent` set `Intent.ClassificationMethod = "fallback"` on the final fallback path (it already calls `d.recordClassificationMethod("fallback")` internally) and surface that field on `DispatchResult` so the provenance is auditable without log correlation.

---

## S1-8 [Medium] `EscalationManager` uses `time.Now()` (wallclock) for ordering and has no monotonic guarantee

File: `internal/agent/escalation.go:119-130`

Severity: Medium (low impact, but worth noting alongside S1-1).

Evidence:
```go
level = &EscalationLevel{
    ...
    Timestamp: time.Now(),
}
...
level.Timestamp = time.Now()
```

Why it is a bug (minor):
Wallclock `time.Now()` can jump backwards on NTP sync. `EscalationLevel.Timestamp` is used for ordering escalations and the value flows into reports/metrics. If two escalations land across an NTP step-back, the later one records an earlier timestamp. This is a known Go gotcha; `time.Now()` does include a monotonic component used for `Sub()`, but that only helps if both sides are `time.Now()` results - serialized comparisons through JSON/log pipelines lose the monotonic portion.

Suggested fix:
Use a monotonic counter for in-process ordering (e.g. `atomic.AddUint64(&em.tick, 1)` on each `Escalate` call) and keep `time.Now().UTC()` only for human display. This is lower priority than S1-1/S1-2/S1-3 but is listed because it is the same family of bug.

---

## S1-9 [Low] `SessionTracker.GetIdleSessions` returns slice of live pointers

File: `internal/agent/session_tracker.go:335-349`

Severity: Low.

Evidence:
```go
func (t *SessionTracker) GetIdleSessions(idleDuration time.Duration) []*TrackerSessionState {
    t.mu.RLock()
    defer t.mu.RUnlock()
    now := time.Now()
    var idle []*TrackerSessionState
    for _, state := range t.sessions {
        if now.Sub(state.LastActivityAt) > idleDuration {
            idle = append(idle, state)
        }
    }
    return idle
}
```

Why it is a bug:
Same shape as S1-5 but lower severity because the caller (`PersistIdleSessions` at line 210) only reads the fields needed for persistence and the idle filter makes concurrent mutation less likely (idle sessions are by definition inactive). Still, the pointers are live and `PersistIdleSessions` does release the lock between snapshot and use, so a `Cleanup()` goroutine could `delete` the entry out from under the persist loop. `PersistIdleSessions` already has a TOCTOU re-check (line 244, "S1-4 TOCTOU" comment from a prior round) so this is mostly mitigated, but the API contract still hands out mutable pointers.

Suggested fix:
Return `[]string` (session IDs) and have callers re-fetch via a locked accessor, or return deep copies.

---

## S1-10 [Low] `PairOrchestrator.GetSession` returns a snapshot but `TeamOrchestrator.Status` returns a live pointer

Files:
- `internal/agent/pair_orchestrator.go:96-118` (correct - returns `BusPairSessionStateSnapshot`)
- `internal/agent/team_orchestrator.go:384-396` (returns live `*TeamSessionState`)

Severity: Low (API inconsistency + latent race).

Evidence (`team_orchestrator.go`):
```go
func (to *TeamOrchestrator) Status(ctx context.Context, sessionID string) (*TeamSessionState, error) {
    val, ok := to.teams.Load(sessionID)
    if !ok {
        return nil, fmt.Errorf("team %q not found", sessionID)
    }
    state := val.(*TeamSessionState)
    state.mu.RLock()
    defer state.mu.RUnlock()
    return state, nil
}
```

Why it is a bug:
The RLock is released on return, so the caller gets a pointer to a live `TeamSessionState` whose `mu` is no longer held. The inline comment ("Caller receives a pointer to the live state. Document that callers should not mutate it without the struct's mutex") acknowledges the issue but the contract is not enforced. `MemberResults` is a map and concurrent `ReceiveResult` writes (line 484) racing with a caller iterating the returned map is a panic risk. The pair orchestrator solved this correctly by returning a snapshot struct; team should do the same.

Suggested fix:
Define `TeamSessionStateSnapshot` mirroring `BusPairSessionStateSnapshot`, deep-copy `MemberResults` under the lock, and return the snapshot.

---

## S1-11 [Low] `time.Now().UnixNano()` fallback in `generateMessageID` produces colliding IDs only on `crypto/rand` failure

File: `internal/agent/handler.go:1378-1386`

Severity: Low.

Evidence:
```go
func generateMessageID() string {
    var randBytes [4]byte
    if _, err := crypto_rand.Read(randBytes[:]); err != nil {
        // Fallback: use nanosecond timestamp uniqueness if crypto/rand fails.
        return time.Now().Format("20060102150405.000000000") + "-" +
            fmt.Sprintf("%0d", time.Now().UnixNano())
    }
    return time.Now().Format("20060102150405.000000000") + "-" + hex.EncodeToString(randBytes[:])
}
```

Why it is a bug (minor):
The fallback path calls `time.Now()` twice (different `Time` values possible across the two calls) and uses a 4-byte `UnixNano()` value with no counter. If `crypto_rand.Read` ever fails (rare, but it can on exhausted entropy on some kernels during early boot), two callers in the same nanosecond produce identical message IDs, and bus messages with duplicate IDs could be deduplicated by any consumer that keys on `msg.ID`. Also only 4 bytes of randomness in the primary path (32 bits) is on the low side for a system that may emit many messages/sec over a long uptime - birthday collision at ~2^16 messages.

Suggested fix:
In the fallback, use `pkg/id.Generate()` rather than a naked timestamp. Consider bumping `randBytes` to 16 bytes (`[16]byte`) to match `pkg/id.Generate()`'s entropy and push the birthday bound out of practical range.

---

## Files reviewed (in scope, non-test)

`internal/agent/`:
agent_loop.go, artifact_integration.go, bus.go, capabilities.go, capabilities_builder.go,
classification.go, collaboration.go, collaboration_errors.go, collaboration_session.go,
collaboration_turn_manager.go, context_manager.go, context_memvid.go, contextualize.go,
delegation.go, dependency.go, dispatcher.go, embedding.go, emitter.go, escalation.go,
events.go, handler.go, hermes.go, hooks.go, intent.go, intent_analyzer.go, loop.go,
loop_reasoning.go, model_parser.go, modifiers.go, orchestrator.go, pair_manager.go,
pair_modality.go, pair_orchestrator.go, pair_session.go, plan_decomposer.go,
plan_templates.go, planner_collaborative.go, planner_strategic.go, planner_tactical.go,
planner_workspace.go, priority.go, progress_synthesizer.go, prompt.go, protocol.go,
queue.go, queue_errors.go, queue_persister.go, queue_recovery.go, report.go,
report_router.go, report_types.go, review.go, ralph.go, security_hooks.go, session.go,
session_tracker.go, shadow.go, spec.go, spec_generation.go, strategic.go (alias for
planner_strategic), tactical.go (alias for planner_tactical), team_orchestrator.go,
team_presets.go, taint_hooks.go, util.go, watcher.go, watchdog.go, workspace_orchestrator.go.

`internal/agent/prompt/`: builder.go.
`internal/agent/prompts/`: baseline.go, dispatcher.go, specialists.go.
`internal/agent/q/`: agent_designer.go, impact_estimator.go, notifications.go,
pattern_detector.go, q_agent.go, research_engine.go, reviewer.go, skill_designer.go.

`internal/agents/`: does not exist in this repository (only `internal/agent/`).
