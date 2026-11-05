# Semantic Cache Daemon Construction - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Construct `llm.SemanticCache` in the daemon and attach via
  `WithSemanticCache` - making `llm.cache.semantic.*` + `verify_floor`
  actually armable in production config. The schema keys, client seams,
  verify band, and streaming sites all exist; only the construction site
  is missing.
- **Dependencies:** none (everything downstream is committed).
- **Estimated Context:** 35K

## Goal

components.go builds the client at ~:1191 (`llm.NewClient(llmCfg,
clientOpts...)`). When `cfg.LLM.Cache.Semantic.Enabled`, construct the
Embedder + SemanticCache and append `llm.WithSemanticCache(cache)` to
clientOpts. Degradation posture: embedder resolution failure or cache
construction failure -> Warn + exact-only (structurally dead cache),
never blocks startup - identical to the token-cache posture.

## Embedder resolution (per the schema doc comment)

- `EmbedderAlias` non-empty: resolve via the alias/capability machinery
  (the same path the decision client uses); failure -> Warn + exact-only.
- Empty: reuse the memory-stack embedding provider when configured -
  `cfg.Memory.Embeddings.Enabled` gates an existing
  `vector.Provider` construction at components.go ~:2560; reuse that
  provider (it satisfies the `Embedder` interface structurally:
  GenerateEmbedding(ctx, text) ([]float32, error) - adapt with a tiny
  adapter if the method names differ; NEVER re-construct a second
  provider when one exists).
- Neither resolves -> Warn "semantic cache disabled: no embedder" +
  exact-only.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Construction block near the token-cache block: read cfg.LLM.Cache.Semantic; resolve embedder per the rules above; `llm.NewSemanticCache(embedder, llmCfgCfgFromSchema(cfg))` mapping every schema field (Enabled/Threshold/MaxEntries/TTLMin->TTL/VerifyFloor/EmbedderAlias); append WithSemanticCache to clientOpts; Info line "Semantic cache initialized" (threshold, verify_floor, max_entries, embedder source). Failure shapes -> Warn + exact-only. | Modify: internal/daemon/components.go, new internal/daemon/semantic_cache_wiring.go if cleaner, new wiring test | `go test -p 2 ./internal/daemon/ -run TestSemanticCacheWiring` | no construction site | enabled cfg -> cache attached to clientOpts; disabled/failed -> absent + Warn | Given enabled cfg + embedder; When components built; Then client has the semantic tier |
| T2 | Config mapping test: schema SemanticCacheConfig -> llm.SemanticCacheConfig field-by-field (Threshold/MaxEntries/TTL/TTLMin conversion/VerifyFloor). TTLMin zero -> mirror llm.cache.default_ttl_min; negative -> clamp (schema doc). | Modify: wiring file + test | `go test -p 2 ./internal/daemon/ -run TestSemanticCacheWiring` | mapping absent | mapping pinned | Given schema cfg; When mapped; Then llm cfg matches field-for-field |
| T3 | Disabled-default test: zero-value semantic cfg -> no WithSemanticCache in clientOpts, no embedder constructed, byte-identical to pre-leaf. Failure test: embedder construction error -> exact-only + Warn, client still built. | Modify: wiring test | same | unproven | both pinned | Given disabled cfg; When built; Then no embedder call, client identical |
| T4 | e2e spot: llm-serving-tiers suite still green (it pins loader acceptance). If the suite has a daemon-construction scenario, extend it to assert the armed log line. | Modify: e2e/suites/llm-serving-tiers only if needed | `go test -p 2 -tags e2e ./e2e/suites/llm-serving-tiers/` | - | suite green | - |

## Constraints

- ONE shared embedder/provider: reuse the existing vector.Provider when
  Memory.Embeddings is enabled - never construct a duplicate.
- Embedder calls on the cache path never park a turn (existing rule).
- Opt-in default false: zero behavior when disabled.
- components.go hunks minimal; prefer the wiring-helper-file pattern
  (slack_wiring.go / tiers_engine.go precedent) over inline blocks.

## Self-Verification Checklist

- [ ] go build ./... green (scoped builds if sibling WIP)
- [ ] wiring tests green incl disabled + failure shapes
- [ ] llm-serving-tiers e2e green
- [ ] gofmt clean, ASCII only
- [ ] no duplicate provider construction

## Review Checklist (orchestrator)

- [ ] Embedder resolution order matches the schema doc (alias > memory stack > none)
- [ ] Mapping covers all 7 schema fields
- [ ] Degradation posture pinned (Warn + exact-only)
- [ ] Info/Warn log lines match the token-cache pattern

## Do NOT commit. The orchestrator stages and commits after review.
