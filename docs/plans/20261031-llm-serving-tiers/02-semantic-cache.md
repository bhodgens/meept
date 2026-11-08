# Semantic Cache Tier: Embedding-Similarity Lookup Behind the Exact-Key Miss - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** master.md
- **Scope:** Opt-in semantic cache tier (embedding-similarity second-chance lookup) strictly ADDITIVE behind the exact-key miss, with hard tool-call and embedder-failure constraints.
- **Dependencies:** R0-research.md (findings doc Section 1: threshold + false-hit mitigations - READ IT FIRST and use its recommended threshold if it differs from 0.92)
- **Estimated Context:** 100K
- **Concurrency Group:** C
- **Audit references:** proposal 9 (semantic cache tier)

## Goal

The exact-key response cache only hits on byte-identical prompts. This leaf
adds an opt-in semantic tier: on an exact MISS, embed the prompt, compare
cosine similarity against cached prompt vectors, and serve the cached
response when similarity clears the threshold. Hard constraints, all
non-negotiable: a semantic hit NEVER serves a response carrying tool calls
(mirror the classifyAnthropicEmpty tool_use exemption shape); embedder
failure degrades to exact-only and NEVER errors or parks a turn (the
embedder is itself an LLM-tier dependency that can be quota-blocked);
ErrEmptyResponse accounting and the rotation path are untouched; disabled
(default) means the code path is structurally unreachable.

## Context

- The cache path: internal/llm/token_cache.go (ResponseCache interface
  :70, TokenCacheCoordinator L1+L2-SQLite :88,99 over token_cache.db,
  wired in internal/daemon/components.go:943-993).
- All FOUR client cache paths consult the cache BEFORE the budget gate via
  BuildPrefixAwareKeyWithFiles: internal/llm/client.go:652,849,909,1104.
- internal/llm/cache_key_builder.go:249,320 is the prefix-aware key
  builder - do not modify it; the semantic tier keys off the prompt text
  side, not the exact key.
- The anthropic client's classifyAnthropicEmpty tool_use exemption
  (internal/llm/anthropic.go) is the shape to mirror for the tool-call
  exemption: inspect the cached response's content blocks / tool_calls
  field and refuse semantic serving when any tool call is present.
- internal/llm/AGENTS.md is the package invariant doc - cat it first.

## Verified Anchors

- internal/llm/client.go:649-657 - exact-key lookup BEFORE budget gate -
  the wiring site: semantic lookup slots immediately AFTER this miss and
  BEFORE the budget gate proceeds.
- internal/llm/token_cache.go:70,88,99 - ResponseCache interface +
  TokenCacheCoordinator - the coordinator this leaf extends or wraps
  (wrap preferred: keep the existing types untouched).
- internal/llm/client.go:1979 - ErrEmptyResponse sentinel rotation - MUST
  remain untouched by semantic-cache code.
- internal/llm/anthropic.go - classifyAnthropicEmpty tool_use exemption -
  the shape to mirror.
- internal/daemon/components.go:943-993 - coordinator wiring; metrics
  store is built BEFORE the coordinator (:940-990) - if the semantic
  cache wants a metrics store handle it is available at that point.
- internal/config/schema.go:1107-1113 - LLMSimpleFeatureConfig (llm.cache)
  - the neighborhood where llm.cache.semantic lands.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/config/schema.go - Contract C4 (json5 key: llm.cache.semantic)
type SemanticCacheConfig struct {
    Enabled       bool    // default FALSE
    Threshold     float64 // cosine floor; default 0.92 unless R0 findings override
    MaxEntries    int     // default 1024
    TTLMin        int     // minutes; default mirrors llm.cache.default_ttl_min
    EmbedderAlias string  // alias/capability resolving the embedder model
}

// internal/llm/semantic_cache.go - Contract C5
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
}
type SemanticCache struct { /* unexported fields */ }
func NewSemanticCache(embedder Embedder, cfg config.SemanticCacheConfig) *SemanticCache
// LookupBehindMiss: (response, true, nil) hit | (nil, false, nil) miss |
// (nil, false, nil) on ANY embedder failure (exact-only degradation).
```

### What This Leaf Consumes

- None from sibling implementation leaves (05 consumes this leaf's contracts).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add SemanticCacheConfig to internal/config/schema.go with all defaults (Enabled=false) and json5 key llm.cache.semantic; default-threshold test | Modify: internal/config/schema.go; Test: internal/config/schema_semantic_test.go | `go test -p 2 ./internal/config/ -run TestSemanticCache` | compile error: SemanticCacheConfig undefined | defaults test green | Given a fresh config; When defaults apply; Then llm.cache.semantic.enabled=false and Threshold=0.92 (or the R0 findings value), MaxEntries=1024, TTLMin mirrors llm.cache.default_ttl_min |
| T2 | Implement internal/llm/semantic_cache.go: in-memory vector store (cosine similarity, MaxEntries with simple LRU or TTL eviction), NewSemanticCache, LookupBehindMiss, Store (records prompt vector + response after a LIVE exact-miss response completes, skipping tool-call-bearing responses entirely so they can never be served); disabled = all methods return the miss immediately without touching the embedder | Create: internal/llm/semantic_cache.go; Test: internal/llm/semantic_cache_test.go | `go test -p 2 ./internal/llm/ -run TestSemanticCache` | compile error: NewSemanticCache undefined | similarity tests green | Given cached vector for "what is the capital of France" with a stored response; When LookupBehindMiss runs with the paraphrase "what's France's capital city?" at similarity above threshold; Then it returns the stored response + true; and a below-threshold paraphrase returns miss; and a disabled cache returns miss WITHOUT calling the embedder |
| T3 | Tool-call exemption: responses carrying tool calls are neither STORED nor SERVED semantically (mirror the classifyAnthropicEmpty tool_use inspection shape) | Test: internal/llm/semantic_cache_test.go | `go test -p 2 ./internal/llm/ -run TestSemanticCacheToolExemption` | no exemption logic | exemption tests green | Given a cached response whose content includes tool_calls / tool_use blocks; When a semantically near-identical prompt misses the exact key; Then LookupBehindMiss returns miss (never the tool-call response) and Store skipped persisting its vector |
| T4 | Embedder-failure degradation + client wiring: embedder errors (including a quota-blocked embedder dependency) return miss with nil error - the turn proceeds exact-only, never parked; wire LookupBehindMiss into the exact-miss site internal/llm/client.go:649-657 behind the disabled check; ErrEmptyResponse accounting untouched | Modify: internal/llm/client.go (miss site + store-after-success), internal/llm/semantic_cache.go; Test: internal/llm/semantic_cache_test.go | `go test -p 2 ./internal/llm/ -run TestSemanticCacheDegraded` | wiring absent: embedder error surfaces / cache never consulted | degraded + wiring tests green | Given an embedder whose Embed returns an error (or a stub LLM dependency returning quota-block); When the exact key misses; Then LookupBehindMiss returns (nil,false,nil), the live call proceeds normally, no error surfaces from cache code, no turn parks; and ErrEmptyResponse rotation behaves exactly as before |
| T5 | Daemon wiring: construct SemanticCache in internal/daemon/components.go near the TokenCacheCoordinator wiring (metrics store is built before the coordinator, so the handle exists) and pass it to the client; a nil/disabled cache must leave the client behavior identical | Modify: internal/daemon/components.go; Test: existing daemon wiring tests still pass | `go test -p 2 ./internal/daemon/ -run TestComponents` | wiring absent (client gets nil cache -> compile ok but unexercised) | wiring green, existing tests green | Given llm.cache.semantic.enabled=false (default); When the daemon boots; Then SemanticCache is nil or disabled and the client takes byte-identical paths; Given enabled=true; When it boots; Then the client holds a live SemanticCache and paraphrase misses hit — **LANDED**: internal/daemon/semantic_cache_wiring.go (buildSemanticCacheTier; embedder reuse > alias > none, Warn + exact-only degraded posture) wired into components.go clientOpts; pinned by TestSemanticCacheWiring* + the live-daemon e2e Warn-line assertion |

**Worked example (T4 shape):**

1. Write the failing test: stub Embedder returning errors.New("quota");
   enabled cache; assert LookupBehindMiss returns (nil,false,nil) and a
   wrapped client call still completes via the live path.
2. Verify RED: `go test -p 2 ./internal/llm/ -run TestSemanticCacheDegraded`
   Expected: FAIL - error surfaces or turn parks.
3. Implement: wrap Embed errors into a plain miss return (no error
   propagation); keep the embedder call off the hot disabled path.
4. Verify PASS: `go test -p 2 ./internal/llm/` all green.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2` on touched packages)
- [ ] Interface contracts satisfied exactly (C4/C5 names, json5 key, defaults)
- [ ] All files at exact specified paths
- [ ] HARD constraints verified by tests: tool-call responses never stored
      nor served semantically; embedder failure = silent exact-only
      degradation, never an error, never a parked turn
- [ ] Lookup order is exact-key FIRST, semantic strictly behind the miss
- [ ] ErrEmptyResponse accounting and rotation path untouched (client.go:1979 region)
- [ ] Zero behavior change when llm.cache.semantic.enabled=false (default):
      no embedder calls, no vector math, byte-identical responses
- [ ] No mutex held across I/O (the embedder call must NOT happen under lock)
- [ ] gofmt clean; ASCII only in every touched file
- [ ] No scope creep; no deviations (or deviations documented)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Contracts C4/C5 match exactly (names, json5 keys, defaults, signatures)
- [ ] Tool-call exemption mirrors classifyAnthropicEmpty shape and is tested
- [ ] Embedder-failure path tested (error + quota-block case); no error/park
- [ ] Exact-key lookup strictly precedes semantic lookup at ALL FOUR cache
      paths or at the single shared miss site they funnel through
- [ ] Disabled path structurally unreachable (no embedder call, no vector work)
- [ ] No mutex-across-I/O; no predictable IDs; no debug artifacts; ASCII only

Output: APPROVED or specific gaps with file + line references.

## Notes

- R0 findings Section 1 is authoritative for the threshold default and the
  false-hit mitigation list; cite it in the SemanticCacheConfig doc
  comment. If R0 recommends a value other than 0.92, the orchestrator's
  dated addendum to this leaf wins over the master's C4 default.
- Vector math: keep it stdlib-only (float32 dot products + norms). No new
  dependencies.
- Do NOT modify cache_key_builder.go, the ErrEmptyResponse region, or the
  streaming delta path (leaf 04 owns it). If a change there seems needed,
  stop and report.
- Embedder construction (turning EmbedderAlias into a live Embedder) may
  reuse the existing client/alias machinery read-only; if the alias
  machinery does not expose a clean embed-only path, implement a minimal
  adapter INSIDE this leaf and note it in the report.
