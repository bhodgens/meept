# Semantic Cache Two-Stage Verifier - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Implement the R0 research addendum's two-stage verifier:
  candidates near (below) the 0.95 threshold get an embedder-verify pass
  before serving; verifier failure degrades to miss.
- **Dependencies:** none (Embedder seam exists).
- **Estimated Context:** 35K

## Goal

Reduce near-threshold false hits: a candidate scoring in a verify band
(below the serve threshold but above a verify floor, e.g. 0.85-0.95)
re-embeds the query and compares against the candidate's stored embedding
(direct cosine, no cache traversal). Direct-verify pass -> serve; fail ->
miss. Verifier (embedder) failure -> miss, never wrong content, never
parks.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Config: verify band on llm.cache.semantic (verify_floor default 0.85, enabled when < threshold; 0 disables the band = today's behavior). Clamp + normalize alongside threshold. | Modify: internal/llm/semantic_cache.go, internal/config/schema.go, schema_semantic_test.go | `go test -p 2 ./internal/llm/ -run Semantic; go test -p 2 ./internal/config/ -run Semantic` | band absent | band configured + clamped | Given verify_floor 0.85; When candidate scores 0.90; Then verify pass engages |
| T2 | Verifier: in-band candidate -> re-embed query (same embedder), direct cosine vs stored embedding >= threshold -> serve; else miss. Embedder error -> miss (existing degradation rule). Hit path above threshold unchanged (no re-embed). | Modify: internal/llm/semantic_cache.go + test | same | near-miss false hits served | band verified before serving | Given candidate 0.90 with unrelated stored vec; When verify runs; Then miss (direct cosine < threshold) |
| T3 | Proofs: (a) in-band candidate w/ matching embedding serves (verify pass); (b) in-band candidate w/ divergent embedding misses (false hit prevented - the R0 scenario); (c) embedder error in verify -> miss; (d) verify_floor=0 disables the band (byte-identical to today). | Modify: internal/llm/semantic_cache_test.go | same | unproven | 4 proofs green | Given each fixture; When lookup; Then outcome matches |

## Self-Verification Checklist

- [ ] go build ./... green
- [ ] semantic cache tests green incl -race
- [ ] config tests green
- [ ] gofmt clean, ASCII only
- [ ] disabled/band-off = byte-identical behavior

## Review Checklist (orchestrator)

- [ ] No new config keys beyond verify_floor
- [ ] Verifier failure -> miss (never serve, never park)
- [ ] Above-threshold path untouched (no extra embed call)
- [ ] RED witnessed for (b)

## Do NOT commit. The orchestrator stages and commits after review.
