# Streaming Semantic Cache Sites - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Wire the shared semanticLookupBehindMiss/semanticStoreAfterSuccess
  seam at the 2 remaining exact-miss sites: ChatWithDeltaCallback
  (streaming) + the third variant site in client.go. Leaf 02 of
  20261031-llm-serving-tiers landed the non-streaming sites; its report
  names these as "2-line follow-up each".
- **Dependencies:** none (seam helpers exist).
- **Estimated Context:** 30K

## Goal

Streaming turns currently bypass the semantic cache entirely. Wire the
same seam so a streaming exact-miss consults the semantic cache, and a
successful streaming completion stores to it.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Locate the 2 remaining exact-miss sites (read client.go around the existing seams: ChatWithDeltaCallback + the third variant). Insert semanticLookupBehindMiss after the exact-key miss and semanticStoreAfterSuccess at the completion site - identical shape to the non-streaming wiring. Tool-call responses: same hard exemption (never lookup-serve, never store). | Modify: internal/llm/client.go | `go test -p 2 ./internal/llm/ -run Semantic` | streaming misses never consult | streaming consults + stores | Given enabled cache; When streaming exact-miss; Then semantic lookup runs |
| T2 | Proofs: (a) streaming turn with a paraphrase-hit cached response serves from cache (zero live consults - stub counters, mirroring the e2e suite's pattern); (b) streaming tool-call turn bypasses (no lookup, no store); (c) disabled = byte-identical. Extend semantic_cache_test.go or client tests. | Modify: internal/llm/semantic_cache_test.go or client tests | same | unproven | 3 proofs green | Given each fixture; When streaming; Then outcome matches |
| T3 | TTFT sentinel: a cache-HIT streaming turn keeps the unarmed-clock sentinel (-1) semantics - pin in the TTFT test if not already covered. | Modify: internal/llm/ttft_test.go if needed | `go test -p 2 ./internal/llm/ -run TTFT` | unproven | sentinel pinned for hit path | Given cache-hit streaming turn; When ledgered; Then ttft_ms = -1 |

## Self-Verification Checklist

- [ ] go build ./... green
- [ ] semantic cache + TTFT tests green incl -race
- [ ] gofmt clean, ASCII only
- [ ] disabled = byte-identical
- [ ] tool-call exemption identical on streaming path

## Review Checklist (orchestrator)

- [ ] Both sites use the shared seam (no new logic)
- [ ] Streaming deltas: cache hit short-circuits BEFORE first delta (TTFT semantics documented)
- [ ] 3 proofs witnessed
- [ ] e2e llm-serving-tiers spot green

## Do NOT commit. The orchestrator stages and commits after review.
