# IndexRAG (arXiv:2603.16415) — Integration Assessment for internal/memory

Research date: 2026-10-12. Full leg-4 report:
/Users/caimlas/.hermes/cache/delegation/subagent-summary-3-*.txt (transcript:
/Users/caimlas/.hermes/cache/delegation/live/deleg_0c5086a2/task-3.log).
Paper full text cached at ~/.hermes/cache/web/arxiv.org-e79a45fdf1.md.

**RECOMMENDATION: CONDITIONAL GO.**

## 1. What the paper actually claims (with numbers)

- Idea: cross-document reasoning is query-independent → precompute it at index
  time. "Bridging facts" are NEW retrievable units (avg 166 chars vs 634 for
  AKUs) stored alongside atomic knowledge units in a FLAT vector store.
- Pipeline: Stage 1 = one LLM call per document extracting QA-pair facts +
  entities (AKU). Stage 2 = for entities with document frequency df ∈ [2, tau]
  (tau=10), generate bridging facts linking documents (max 5 docs, 8 facts/doc
  per bridge entity). Training-free; incremental updates only touch affected
  entities (§3.2.2).
- Balanced context selection (Alg. 1, App. B): bridging facts are short so they
  dominate top-k; cap them at k_b. Peak at k_b=2-3 (App. C, Table 10); declines
  at k_b=5. Defaults: retrieve 20, k=10, k_b=3.
- Gains (Table 4, F1): +4.6 avg over Naive RAG (51.7 vs 47.1). Online cost =
  1 retrieval + 1 LLM call, 0.30s vs Naive 0.29s (Table 5); HippoRAG needs
  3.13s/2 calls, FastGraphRAG 2.55s. IRCoT+IndexRAG avg 55.0 beats HippoRAG 54.1.
- CAUTION (Table 9): bridges REDUCE Recall@10 (MuSiQue 62.0→52.8) while RAISING
  EM — bridges displace source passages in retrieval. Benign for QA, risky when
  the agent needs underlying evidence.
- The paper states NO token savings anywhere. This is a quality-per-fixed-budget
  technique, not a KV/token trick.
- Stated limitations (§6): hallucinated bridging facts (indexing-LLM quality),
  noisy entity extraction, English-only QA eval.

## 2. Ecosystem check

- HippoRAG: alive, MIT (~4k stars, MCP server added recently). FastGraphRAG:
  alive, MIT. IndexRAG code: repo listed at Continuum-AI-Corp/IndexRAG; license
  NOT verified this pass — check before borrowing code (the technique itself is
  described fully in the paper; a Go reimplementation needs no dependency).
- Follow-ups: MIDR (arXiv:2609.01316) treats index-time enrichment as prior art.
  Nobody has applied this to AGENT memory yet — young idea, open niche.
- Closest production analogs: Zep/Graphiti precomputes cross-session fact edges
  but as a GRAPH (query-time traversal); Letta sleep-time compute rewrites prompt
  memory blocks offline. Neither does flat-store retrievable derived facts with a
  proportion cap.

## 3. Mapping onto internal/memory (source-verified)

- Stage 1 (AKU + entities) → distill.go: the pipeline already has
  QueueDistill/DrainDistillQueue, pluggable SetDistillSummarizer,
  grammar-constrained generation, evidence-ID collection, cosine/Jaccard dedupe.
  A new DistillSummarizer variant = QA extraction + entity list. Table 8 shows
  Stage-2 gains are Stage-1-agnostic — meept does NOT need to change its existing
  memory representation.
- Stage 2 (bridges) → Consolidator.Run periodic offline pass
  (StartPeriodicConsolidation already groups related memories through an LLM).
  Bridge entities via clustering.go cosine machinery + Stage-1 entity sets +
  df∈[2,10] filter.
- Generator model: local LFM2.5-8B GGUF (grammar-constrained) or the 8B MLX —
  offline, latency-insensitive. NOTE the paper used GPT-4o-mini; an 8B generator
  raises the hallucination risk → entailment verification gate is mandatory.
- Vector store: NO new dependency. internal/memory/vector/store.go is already a
  flat cosine store in Go over SQLite blobs; shard_manager.go shards are the
  right home for a new BridgeShard type. sqlite-vec unnecessary below ~10^6
  units (flat scan is single-digit ms at 384-768 dims).
- Balanced selection → vector_searcher.go results, then the agent loop's recall
  path: tag unit_type=aku|bridge, greedy pass-through for AKUs, cap bridges at
  k_b=2. Composes with the context firewall.
- Constraints per repo rules: bridges are derived/node-local → EXCLUDE from
  dual_store gossip; bridge generation only via the Consolidator writer
  discipline (no mutex-across-I/O); opt-in config key (memory.bridges.enabled,
  default false); CLI/TUI visibility + e2e suite + docs/workflows/memory.md in
  the same change.

## 4. Conditions (all mandatory before adoption)

1. Offline-only: generation lives in the Consolidator, never the chat path.
2. Provenance: every bridge stores evidence IDs; source-memory update/delete/
   expiry cascade-invalidates dependent bridges (staleness.go is the natural
   host). No provenance, no ship.
3. Anti-hallucination: grammar-constrained generation + entailment check against
   cited AKUs; drop unverifiable bridges (expect meaningful rejection rates even
   at GPT-4o-mini quality: non-empty rate was only 79-88%).
4. k_b cap (start 2) enforced in recall before prompt assembly.
5. No sqlite-vec; reuse vector.Store + BridgeShard.
6. Evaluate first: meept-native eval (cross-session recall questions over real
   episodic memories, bridges on/off, tokens measured) via internal/eval. The
   paper's QA-benchmark F1 does NOT transfer automatically — static corpora,
   short gold answers, no query drift.

## 5. Honest expected value

- Same-context-budget quality win on cross-session multi-hop recall ("what did
  we decide about X's dependency on Y" spanning sessions).
- Real context savings ONLY where recall currently over-retrieves (larger top-k)
  to compensate for multi-hop misses — measure before claiming savings.
- Risks: evidence displacement (Table 9), stale bridges when facts change,
  8B-generator hallucination entering the permanent index.
