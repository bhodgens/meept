# Memory subsystem bug audit — wave 2026-11-06

- Scope: commits 0a446a21..HEAD (memory subsystem changes): 9b1b94f9 (Scope enum + WithScopeFilter + migrations episodic v6/task v7), 4cefbfc9 (memory_max_scope + scoped recall at ALL sites), 6b000e97 (group memory ceiling EffectiveMemoryScope), 7e4748d4 (recall_outcomes.jsonl telemetry), c24db7d9 (tierpolicy daemon wiring), e23fd815 (streaming-cache semantic sites), 5da890a1 (BaselineTools memory additions).
- HEAD SHA: 6b42213b
- Captured: 2026-11-06T18:42:01Z
- Method: read-only; audit committed state via git show / git diff. Excludes already-closed findings in .hermes/audits/2026-10-30-bughunt-wave.md and PB-1/PB-2 (fixed 6b42213b).

## Findings

## F-1 [HIGH] Bridge recall appends after the scope filter — cross-scope leak bypasses C2 at every Manager search surface
- File: internal/memory/manager.go:749-764 (Search), :777-820 (mergeBridges), ftstore/GetRelevantContext path
- Evidence: in `Manager.Search` (and the semantic/hybrid fallbacks), the pattern is:
  ```go
  results = filterScopeResults(results, scopeFilter)
  return m.mergeBridges(ctx, query.Query, results)
  ```
  `mergeBridges` runs `bs.SearchBridgesByQuery` (an **unscoped** KNN over the bridge shard) and appends each hit as a `MemoryResult` whose Metadata carries only `BridgeMetaUnitType` — no `scope` key, so `scopeFromMetadata` → `ScopeGlobal`, and the append happens **after** `filterScopeResults`, so the C2 predicate is never re-applied to bridge rows. `BridgeRecord.Content` is "the human-readable bridging fact text" derived from evidence memories that themselves carry project/user scope (bridges_store.go:44-63).
- Why it is a bug: a project-scope caller (or a Flag-3 fail-closed multiuser caller with empty userID) in project B can receive bridge facts distilled from project-A / user-scope source memories — the exact content class the scope filter exists to hide. Commit 9b1b94f9's message claims "T4: the same filter everywhere" and 4cefbfc9 "scoped recall at ALL sites"; bridge recall is a recall site that is unfiltered and implicitly global. Additionally `PutBridge`-time metadata scope (if ever stamped) is dropped when the row is converted in `mergeBridges`, so even a data-side fix wouldn't bind without filtering here.
- Fix shape: propagate source-memory scope into bridge records at generation (or a conservative `scope=user/project` + `project_id` key), and in `mergeBridges` either (a) accept the resolved `*scopeRecallFilter` and run `f.visible()` on each bridge candidate before appending, or (b) re-apply `filterScopeResults` to the merged output. Fail-closed default: treat scope-less bridge rows as `ScopeProject`-invisible when a filter is active.

## F-2 [MEDIUM] memory tools skip the chief-of-staff global→user-purview mapping — a declared-global agent's tool recall contradicts its loop recall (fail-closed inconsistency)
- File: internal/tools/builtin/memory.go:26-37 (scopeSearchOptions) vs internal/agent/scope_wiring.go:78-99 (memoryScopeOptions)
- Evidence: `scopeSearchOptions` passes `memory.Scope(id.MaxScope)` **verbatim** into `WithScopeFilter`, so a `memory_max_scope: global` agent gets `maxScope=global` at the filter. In `scope_filter.go` `visible()`, a global-scope caller sees ONLY global rows (`case ScopeProject: default: return false`; `case ScopeUser: if f.maxScope != ScopeUser { return false }`). The loop/dispatcher injection path deliberately maps `ScopeGlobal → memory.ScopeUser` ("chief-of-staff spans projects via the user purview", scope_wiring.go:80-84) because the C2 predicate treats a global CALLER as identity-less.
- Why it is a bug: the same session gets two different recall vocabularies: auto-injected context spans the user's projects, but `memory_search` / `memory_recall` / `memory_get_context` (the tools the chief-of-staff agent uses on demand) return only global rows — project/user facts the agent is authorized for and already sees in its prompt vanish from its tool results. Direction is fail-closed (no leak), but it silently defeats the documented D2 chief-of-staff contract at three tool sites.
- Fix shape: apply the same `global→user` purview mapping in `scopeSearchOptions` (or better, export one helper used by both surfaces so there is a single global-caller interpretation, as C2 demanded "ONE predicate implementation").

## F-3 [HIGH] Manager.GetRelevantContext memvid path ignores the scope filter entirely — fail-open at a documented "ALL sites" recall surface
- File: internal/memory/manager.go:1059-1068 (GetRelevantContext, useMemvid branch)
- Evidence:
  ```go
  if useMemvid && m.memvid != nil {
      results, err := m.searchViaMemvid(ctx, MemoryQuery{Query: query, Limit: maxItems})
      if err == nil {
          return results, nil          // <-- scopeFilter never applied
      }
      ...
  }
  ```
  `GetRelevantContext(ctx, query, maxItems, opts...)` resolves `scopeFilter := resolveScopeOptions(opts)` at the top but applies it ONLY in the SQLite-fallback path (`results = filterScopeResults(results, scopeFilter)`, line ~1131). `searchViaMemvid` takes no options and converts memvid rows with `Metadata` carried through but no `Scope` field set and no filter applied (it is also consumed by `Search` — which DOES filter the converted slice — but GetRelevantContext returns it raw).
- Why it is a bug: on a memvid/backend-memvid install, `memory_get_context` (builtin/memory.go:371 passes `scopeSearchOptions(ctx)...`), and any loop/dispatcher consumer of GetRelevantContext, get UNFILTERED cross-project/cross-user recall — the exact fail-open the scoping campaign exists to prevent. 9b1b94f9 explicitly claims the memvid/dual-store path is in-scope ("dual-store/memvid paths read metadata" as the portable truth); the metadata IS present in `searchViaMemvid` conversion, so filtering was possible and was simply omitted on this one branch. The legacy no-option path is byte-identical (scopeFilter nil), so this is purely the scoped-caller leak.
- Fix shape: `results = filterScopeResults(results, scopeFilter)` before `return results, nil` in the memvid branch (rows already carry Metadata; `scopeFromMetadata` works unchanged). Add a gate pin that a scoped GetRelevantContext on the memvid path hides foreign-project rows.

## Audited-clean

Checked and found correct (as committed at 6b42213b):

- **C2 predicate (scope_filter.go)**: project/user/global matrix matches its documented pins, including Flag-3 fail-closed (multiUser+empty userID hides user rows); `WithMultiUserEnabled` after `WithScopeFilter` ordering is safe; nil-filter legacy path byte-identical. Unknown-scope-value → treated global is documented, intentional fail-open for writer-less legacy rows.
- **Migration version arithmetic (ftstore.go:330-397)**: `scopeVersion = len(steps)+1` yields episodic v6 / task v7 exactly as the commit message states, and per-shape next-free-slot avoids the renumbering trap (search_text stays v5; Apply skips steps <= current). Fresh DDL + ALTER DEFAULT + guarded backfill UPDATE are idempotent; fact_store.go guarded ALTERs likewise. **Caveat (not a confirmed bug):** the `scope` column is write-never — no INSERT in ftstore/episodic/task sets it, every INSERT relies on the column DEFAULT 'global' while the portable truth lives in metadata. Nothing reads the column today (all recovery goes through `scopeFromMetadata`), so it is dead defense-in-depth rather than drift; flag if any future query filters on the column.
- **Ceiling math (scope_inputs.go:99-159)**: `EffectiveMemoryScope` = min(decl, ceiling) over project<user<global is correct; decl ""/invalid fails safe to project; ceiling ""/invalid never narrows and can never widen above the decl (rankCeil<rank check only ever *lowers*). `resolveScopeInputsWithCeiling` identity legs byte-identical to `resolveScopeInputs`. Loop side reads `l.groupResolution.MemoryMaxScope` under RLock; dispatcher side uses the same `resolveForSpec` record.
- **Flag-3 wiring (scope_wiring.go / dispatcher_scope.go)**: `multi || multiUserCfg` OR-ing at every `WithMultiUserEnabled` call site is present on both loop and dispatcher lanes, so a resolution gap under enabled multiuser fails closed.
- **Loop buildMemoryContext (loop.go:7663-7731)**: all three branches (inherited / refs / auto-search) funnel through `addScoped` → `memory.FilterResults` with the resolved tuple. No bypassed branch found.
- **Telemetry row shape (recall_outcome.go)**: C5 keys only (ts/session_id/memory_id/query/injected/cited/tokens); `Salient []string` is `json:"-"` — non-serialized. Rows carry query text + ids, not memory content or secrets; file is gitignored under the memory data dir; write failure disables the logger, never fails the turn; 10MiB rotation rides the existing mechanism. Drain logs at Debug with counts only (memory_capture.go:227). No secret/scope-content leak found.
- **Gossip stamping (dual_store.go StoreRemoteMemory)**: explicit peer scope preserved, `scope=global` default stamped only when absent, `remote_node` provenance always set — matches the documented "cluster recall explicitly unscoped" decision (docs/workflows/memory.md:587-595), i.e. a documented product decision, not an oversight.
- **Write stamping (builtin/memory.go stampScope)**: explicit scope honored, invalid → session scope (project when bound), project_id/user_id keys ride metadata only for matching scopes; `Manager.Store` backfills metadata["scope"] for unscoped writers. Consistent with the C2 matcher.
- e23fd815 streaming-cache sites and c24db7d9 tierpolicy wiring: not memory-recall-scope surfaces; no scope-filter-relevant defect found in their diffstat files within the time-box (semantic cache keyed on prompt text, tiers engine reads config cutoffs). PARTIAL: only diff-level review performed.
- 5da890a1 BaselineTools additions (knowledge_search/read/impact into default tool list): reads-only grant per D3; default-ON flips honored explicit false overrides; no scope predicate involved (knowledge store is a separate, unscoped surface by design per docs). PARTIAL: distill_async.go models-less skip not deep-reviewed.

Audit stopped at call-budget (~20 calls). F-1..F-3 confirmed with source evidence; remaining un-swept surface: internal/daemon/tiers_engine.go + context_assembler_wiring.go internals, e23fd815 verifier band details.

## Wave closeout (parent, 2026-11-06)
- F-1 CLOSED by 78a1da70 (mergeBridges applies scopeRecallFilter via bridgeVisible; pinned by TestMergeBridgesAppliesScopeFilter + TestManagerSearchScopeFilterHidesForeignBridge).
- F-3 CLOSED by 78a1da70 (GetRelevantContext memvid fast path now applies filterScopeResults).
- F-2 OPEN (memory tools vs loop scope_wiring global->user purview mapping inconsistency).
- F-2 CLOSED: builtin scopeSearchOptions now maps a declared-global
  identity to the user purview exactly like internal/agent/scope_wiring.go
  memoryScopeOptions; pinned by TestGlobalAgentToolRecallMatchesLoopPurview
  (mutation-proven RED on the pre-fix verbatim pass; project-session
  strictness re-checked: project-B caller still cannot see project-A rows).
  internal/tools/builtin full package ok.
