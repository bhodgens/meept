# SearchSemantic keyword fallback AND-binds multi-word queries — semantic-ish recall misses near-matches

Found: 2026-10-13, memory capability forest (tree 02 leaf L2, commit 4cd352fb).
Status: OPEN — not fixed by the forest (workaround shipped in the conflict path only).

## The defect

`Manager.SearchSemantic` (internal/memory/manager.go:1946) falls back to
keyword search when no vector store is configured:

```go
return m.Search(ctx, MemoryQuery{
    Query: query,
    Limit: limit,
})
```

`MemoryQuery.MatchAny` defaults to false, so the FTS path AND-binds every
query token (internal/memory/episodic.go:204: `op = "AND"` unless MatchAny).
A multi-word query therefore matches only documents containing ALL tokens.

Consequence: a user who stores "user deploys on Fridays" and later searches
"deploy Friday afternoons" gets ZERO results — "afternoons" never appears in
the stored text. Semantic search's whole purpose is matching near-misses, and
its own fallback structurally cannot see them. `SearchHybrid` shares the same
fallback path (same construction at manager.go, same missing MatchAny).

## Measured evidence

Tree 02's e2e leg probe-verified this: recall for the conflict detector over
a seeded "Fridays" claim returned 0 hits for the candidate "Friday
afternoons" until the adapter bypassed SearchSemantic and queried the
episodic FTS store directly with `MatchAny: true`
(internal/memory/conflict_wiring.go:79, commit 4cd352fb).

## Affected consumers

- `search.semantic` RPC (internal/daemon/search_rpc.go:21) → SearchService →
  `SearchSemantic` — the TUI search and `POST /api/v1/search/semantic` (GUI)
  when no embeddings are configured.
- `memory.query` HTTP service path (internal/services/memory_service.go:234).
- `SearchHybrid` keyword fallback (same construction).
- The distilled-memory injection path already uses MatchAny correctly
  (423479e4) — this issue is about the OTHER callers that do not.

## Options

(a) MatchAny in the fallback (small): set `MatchAny: true` in
    SearchSemantic/SearchHybrid keyword fallbacks. OR-matching widens
    recall; relevance ranking still orders best-first. Risk: noisier
    results for genuinely multi-token intents.
(b) Ranking-preserving split (medium): run MatchAny OR-query, then re-rank
    by fraction of query tokens present (the conflict adapter's approach:
    FTS recall + token-overlap rerank, mirroring graph.go canonical-text
    scoring). Better precision than (a), more code.
(c) Leave as-is and document (smallest): declare keyword fallback
    intentionally strict; require embeddings for semantic behavior.

Recommendation: (b) for SearchSemantic, and reuse the conflict adapter's
rerank as the shared helper. (a) is acceptable as a first step if done
before the helper lands.

## Repro sketch

With vector store disabled: store memory "user deploys on Fridays";
`POST /api/v1/search/semantic {"query": "deploy friday afternoons"}` → 0
results. With MatchAny: ≥1 result.
