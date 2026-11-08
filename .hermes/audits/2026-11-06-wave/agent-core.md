# Agent core bug audit — internal/agent
- Scope: dynamic-tools exposure engine (f8c862e9, 316b23f7, 78c8aeed, 1c8ff3e2, fe04b160, a8b4b374), loop.go/executor.go/dispatcher diffs, contextalloc, turnjournal
- Range: 0a446a21..HEAD
- HEAD: 6b42213b
- Captured: 2026-11-06T18:42:00Z
- Excluded (already closed): PB-1 (voice_call perms, 6b42213b), PB-2 (rewake eviction flake, 6b42213b), 2026-10-30 wave

## Findings

## F-1 [MEDIUM] §6 row-4 starvation guard is dead code: known-but-inactive tools execute silently
- internal/agent/executor.go:1310-1317 (guard placement), internal/agent/loop.go:2177-2182 (known set)
- Evidence: the row-4 branch lives INSIDE `if tool == nil {` (executor.go:1297). But the plan's `knownNames` is built from `l.registry.List()` (loop.go:2178-2181) and the executor holds the SAME registry (`NewExecutor(loop.registry, ...)`, loop.go:2698) — dynamic exposure only filters the LLM **definitions payload** (`scopeDefinitionsToActive`, loop.go:2244), never the execution registry. Therefore for any tool the plan knows, `registry.Get(name)` succeeds and the guard is never reached; for any tool the registry rejects, `IsKnown` is false (same name set) and the guard also never fires. The only way it triggers is registry drift after plan arm (MCP connect/disconnect, skill-filter swap at loop.go:3334 — the plan is frozen at first arm), where it emits the misleading "call tool_view first" for a tool that genuinely vanished.
- Why it is a bug: design §6 row 4 intends a hard execution gate ("the actionable starvation recovery vehicle"); as wired, in predicted/search modes the model can call ANY in-ceiling tool directly without ever paying the tool_view round-trip — the active set is advisory for the prompt only. Either the enforcement is silently absent (defeating the promotion protocol the e2e canary claims to prove) or the guard was meant to sit BEFORE `registry.Get`.
- Fix shape: check `plan != nil && plan.IsKnown(name) && !plan.IsActive(name)` before attempting `registry.Get`, or have the exposure engine swap in a plan-gated execution view for the turn. Add an e2e: predicted mode, call an in-ceiling non-active tool, assert refusal.

## F-2 [MEDIUM] Role ceiling built from raw grant names, compared against canonical registry names — alias grants refused at Promote
- internal/agent/loop.go:2183-2186 (`ceiling = l.spec.AllTools()`), tool_exposure.go:189-193 (Promote ceiling check), spec.go:255-260 vs spec.go:289-296 (AllTools vs GrantedToolNames)
- Evidence: `AllTools()` returns the DECLARED names ("as declared... not normalized to registered registry names — use GrantedToolNames for that", spec.go:252-254). The registry filter path (registry.filterTools → ScopedToolNames → GrantedToolNames) canonicalizes (`toolGrantAliases: shell_execute -> shell`, spec.go:271-283). But Promote's ceiling map is keyed on the raw grant vocabulary while `activateViewedTool` promotes the name the model used = the registry name (loop.go:2219-2228). An agent granting `shell_execute`: `shell` is in known/active via DefaultAlwaysFullTools so this one instance is masked — but every alias'd/non-baseline granted tool is mis-refused: "outside this agent's role ceiling" for a tool the agent was granted. Also the ceiling ignores the group intersection applied at loop-creation (registry.go:960-985), so a group-denied-but-spec-granted tool passes Promote's ceiling (harmless only because F-1 makes the executor gate dead; if F-1 is fixed by gating execution, this becomes a permission bypass — fix both together).
- Fix shape: `ceiling = l.spec.GrantedToolNames()` (and union the meta pair or keep the pre-seed), and intersect with `l.groupResolution` the same way filterTools does.
- Corollary (same root cause, add to F-2 scope): the meta pair is only in the ceiling if AGENT.md grants it — grep shows only `config/agents/coder/AGENT.md` grants `tool_view`; NO agent grants `tool_search`. `knownNames` also comes from the FILTERED registry (loop.go:2178), not the "FULL registry" its docblock claims. Result: for most agents the scoped payload silently drops the meta pair (intersection in `scopeDefinitionsToActive`), so §1-layer-3 "always active discoverer" is false in production topology — and once F-1 is fixed (execution gating), those agents have no recovery path at all.

## F-3 [MEDIUM, latent until F-1 fixed] One executor field holds one plan while plans are per-conversation — cross-episode adjudication
- internal/agent/loop.go:4698-4702 (arm site), executor.go:1160-1168 (SetExposurePlan)
- Evidence: `l.exposurePlans` is keyed per conversationID, but the executor has a single `exposurePlan` overwritten at each `reasoningCycle` entry, and nothing resets it at turn exit. Loop objects live for a whole session and can serve multiple conversations (resume/interleaved turns on the same loop); a tool executed while episode B's plan is stamped but the turn belongs to episode A is adjudicated against the wrong active set (A's earned promotions invisible → false refusal; B's promotions leak into A → the opposite). Today the harm is masked by F-1 (guard unreachable); the moment F-1 is fixed this becomes a live sticky-state bug.
- Fix shape: carry the plan through the Execute call path (context value or parameter) rather than shared executor state; at minimum clear it in the turn teardown next to the existing `clearThrottleTurnContext` defer.

## F-4 [LOW] exposurePlans / exposureDowngradeWarned maps grow unboundedly per loop — never pruned on conversation end
- internal/agent/loop.go:602-603, 2162-2188, 628
- Evidence: `grep delete(l.exposurePlans` finds no removal anywhere in loop.go. Every distinct conversationID that ever runs a dynamic turn appends a plan (active slice + two full name sets) retained for the loop's lifetime; long-lived session loops accumulating conversations grow monotonically. Bounded by session churn, not a crash, but it is exactly the "sticky state leaking across sessions/conversations" class: a recycled conversationID would resume a stale frozen episode instead of starting fresh (design §2.2 says a new conversation is a new episode — a *re-opened* conversation id silently keeps old promotions; arguably intended, but there is no documented contract either way, and no archive/delete hook prunes it).
- Fix shape: prune on conversation archive/delete/teardown bus events, or cap with an LRU keyed by last use.

## F-5 [LOW] JournalWriter dedup maps keyed by turnID never shrink — daemon-lifetime unbounded growth
- internal/agent/turnjournal/writer.go:36-37, 84-88, 147-151
- Evidence: `runStartedRuns`/`closedRuns` are written `= true` per turnID and there is no delete/eviction path (grep: only the two inserts). The writer is a single daemon-wide instance wired "beside parks.db" (f3460f33), so every turn ever run leaves two map entries until restart. Entries are tiny, but the growth is monotonic and the file's own docblock frames the maps as "once-per-turn dedup" for *retries*, not as a permanent turn registry.
- Fix shape: prune entries when the run closes successfully (closedRuns) or on the same 7d `PruneClosedRuns` boot sweep with a periodic tick; or bound with a small LRU since the interlock only needs recent turns.

## Audited-clean

- **tool_exposure.go pure logic** (EffectiveExposureMode resolution order, capability downgrade, flag-off force-indexed): matches design §3.4/§5.2.1; nil-model fail-safe to non-frontier verified. ExposurePlan mutex discipline (Promote/IsActive/IsKnown/Active all lock; no I/O under lock) sound. Budget truncation `predicted[:budget]` guarded by `budget > 0` — no off-by-one; search-mode budget-0 branch never reaches it.
- **intent_tool_map.go** (fe04b160): For() nil-receiver safe, budget<=0 early-return correct, intersection keeps config order, dedupe preserves first occurrence, drop-with-one-warn deterministic (sortedUnique). NOTE: the predicted set is passed as nil at loop.go:2187 (`NewExposurePlan(mode, nil, ...)` — arm-site comment admits "intent-map selection deferred to a follow-up"), so the map engine is dormant at HEAD; F-1/F-2 describe the armed-but-unenforced interim state.
- **tool_search.go** (a8b4b374): parseLimit clamp (default 8, cap 20, non-positive → default) correct; digest budget math (`budget--` reserve + `cats[budget:]` remainder) consistent; rune-based desc cap safe for UTF-8; nil-scope/nil-tool guards present.
- **Executor.SetExposurePlan**: nil-receiver guard, mutex-held assignment — repo setter convention met.
- **loop.go activateViewedTool / arming site**: JSON-arg parse failures ignored gracefully; append-only promotion respected; loop-side activation avoids the builtin import cycle as documented.
- **turnjournal writer**: OnRunStart/OnRunClose dedup under one mutex (runStartedMu) for both maps — no torn reads; store I/O outside the lock (mutexio-clean); nil-receiver no-ops; detached-context record() correct for hard-kill semantics. PlanResume (replay.go): gap-based Next computation correct for reordered lists (documented degradation).
- **turnjournal rehydrate.go**: fail-soft per-run requeue (error keeps run open for next boot; original job ID preserves the completion interlock per leaf 03); PruneClosedRuns best-effort.
- **contextalloc seam.go**: typed-nil provider guard (reflect isAbsentProvider) disables the WHOLE seam per the daemon typed-nil rule; nil-receiver methods safe; disabled seam takes the byte-identical legacy path.
- **registry.go filterTools / createLoop stamping**: schemaModeCfg stamped under r.mu onto fresh loops (loop.mu uncontended at construction); session-clone path snapshots under RLock then applies on the clone — lock ordering correct.
- **applyExposureGate warn-once** (loop.go:2116): map lazily created under l.mu, dedupe per modelRef correct.
- Gates run: `go build ./internal/agent/...` clean; `go vet ./internal/agent/` clean; package has no in-package unit tests for the exposure engine (coverage is e2e/suites/dynamic-tools only — noted, not run per time-box).
- Not audited (time-boxed at ~27 calls): dispatcher.go full 5993-line diff sweep, park/resume × exposure interaction beyond grep, turnjournal store_sqlite.go internals, e3b2130b siblings.

HUNT STATUS: COMPLETE (F-1..F-5 written; F-1 was initially drafted as a HIGH deadlock claim and downgraded to MEDIUM dead-code after re-verifying that registry.Get still serves inactive tools — definitions-payload filtering only).

## Closeout (2026-11-06, parent fix wave)
- F-1 CLOSED: row-4 guard moved BEFORE registry.Get in Execute as a hard
  gate; plan.Enforcing() (search/predicted only) keeps flag-off/full/indexed
  byte-identical. E2E pin: TestRow4HardGate_RefusesInactiveTool (mutation-
  proven RED with the gate disabled).
- F-2 CLOSED: ceiling now built from the per-loop (filtered) registry name
  set — canonical names, group intersection already applied — unioned with
  the meta pair so every dynamic-mode agent keeps the recovery path. The
  old `if tool == nil` row-4 branch is deleted (unreachable-by-construction
  after the gate; registry-drift names now fall through to the legacy
  unknown-tool/group-gate errors).
- F-3 CLOSED: the plan rides the TURN CONTEXT (WithExposurePlan at
  reasoningCycle arm-site; exposurePlanFromContext at the gate). The shared
  executor field + SetExposurePlan setter are removed; interleaved
  conversations on one loop adjudicate against their own episode.
- F-4 CLOSED (partial): ClearConversation now deletes the conversation's
  exposure plan. exposureDowngradeWarned left as-is (bounded by distinct
  model refs, not conversation churn).
- F-5 OPEN (LOW): deferred — writer.go is under active sibling edits;
  revisit after the sibling wave lands.
- F-5 CLOSED: both dedup rings bounded by dedupRingCap (8192) with
  FIFO eviction (markDedup helper + startedOrder/closedOrder rings).
  Below the cap the semantics are byte-identical to the unbounded
  legacy (retry coalesce, double-close no-op, failed-close marker
  kept). Above it the interlock forgets only turns older than 8192
  writes; a forgotten turn re-appending one duplicate row is
  tolerated by OpenRuns (GROUP BY newest) and reclaimed by
  PruneClosed. Pins: TestJournalWriterDedupSetsStayBounded
  (mutation-proven RED with eviction disabled) +
  TestJournalWriterRingKeepsLegacySemanticsBelowCap.
