# B04 — Balanced Context Selection in Recall (P10)

**Proposal:** P10 (branch 09). **Depends on:** B01 (store exists; works with
zero bridges too). **Files (2):**
- Modify: `internal/memory/vector_searcher.go` (ShardManagerVectorSearcher.Search →
  post-retrieval selection) and/or the agent-loop recall consumer — locate with
  search_files where search results are assembled for prompt injection
- Test: `internal/memory/balanced_selection_test.go`

**Problem:** Bridging facts are short (paper: 166 vs 634 chars) so they dominate
top-k and crowd out information-dense source memories; the paper's Table 9 shows
they also displace gold passages in raw retrieval ranking (Recall@10 drops even
as EM rises). Balanced selection (paper Algorithm 1, Appendix B) fixes the
occupancy problem: greedy pass over similarity-ranked results — every source
unit (AKU) passes; a bridge is admitted only while `bridges_included <
cfg.KB` (default 2; paper optimum 2-3, declines at 5).

**Contract (branch #5):** selection runs AFTER retrieval, BEFORE prompt
assembly. Units without `unit_type=bridge` metadata are treated as source units
(backward compatible with all existing memories). Returns the filtered,
order-preserved result list + counts (included/excluded bridges) for logging.

### Tasks

**Task 1 — failing tests (balanced_selection_test.go):**
(i) `TestBalancedSelectionCapsBridges`: 5 bridges ranked above 5 source units,
k_b=2 → result = 2 bridges + 5 source units, original similarity order preserved.
(ii) `TestBalancedSelectionUntypedAreSource`: zero metadata → nothing capped.
(iii) `TestBalancedSelectionZeroBridgesPassthrough`: no bridges → identical list.
Run: `go test -p 2 ./internal/memory/ -run TestBalancedSelection` — FAIL.

**Task 2 — implement:** `SelectBalanced(results []SearchResult, kb int)
(filtered []SearchResult, stats SelectionStats)` in the searcher package;
call it from the recall path after ConsolidateResults/merge, before the
agent-loop injection point. Config read: `memory.bridges.kb`. Note: selection
applies regardless of `enabled` (if bridges exist in the store they should be
governed; `enabled=false` + empty store = no-op naturally).

**Task 3 — observability:** log included/excluded bridge counts at debug; if a
stats/metrics seam exists on the recall path (search_files for metrics on
search), record `memory.bridges.included` / `.excluded`.

**Task 4 — full package:** `go test -p 2 ./internal/memory/` green.

**Task 5 — report.** Do NOT commit.

## Self-Verification Checklist
- [ ] Order preserved; k_b enforced; untyped units unaffected
- [ ] Agent-loop injection point verified with an integration-style test if one exists
- [ ] No retrieval-ranking changes (selection is post-retrieval only)

## Review Checklist (for orchestrator)
- [ ] Works correctly when bridge store is empty (the default deployment)
- [ ] Stats surface exists for B05's eval to consume
