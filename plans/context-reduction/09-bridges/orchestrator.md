# Branch 09 — IndexRAG Bridging Facts (P10)

> **Orchestrator document.** Dispatches B01-B06 in dependency order, reviews
> in-session, commits after review. Root: `plans/context-reduction/master.md`
> (read its Interface Contracts section first — contract 4 (bridge record) and
> 6-7 (conventions) bind this branch).

**Subsystem goal:** Precompute cross-memory "bridging facts" offline in the
Consolidator (IndexRAG, arXiv:2603.16415), store them as retrievable units in a
new BridgeShard, cap their share of recall at k_b, and prove value with a
meept-native eval before anyone considers enabling by default. Default OFF.

**Key design decisions (from docs/research/03-indexrag-integration.md):**
- Offline-only: generation runs in `internal/memory` Consolidator background
  pass — NEVER on the chat inference path.
- No sqlite-vec: reuse `internal/memory/vector/store.go` flat cosine + a new
  shard type. Fine at meept scale (single-digit ms flat scans).
- Bridges are derived + node-local: EXCLUDE from dual_store gossip.
- Stage-1 AKU extraction = new DistillSummarizer variant (paper Table 8 shows
  Stage-2 gains are Stage-1-agnostic — existing memory representation stays).
- Provenance mandatory: every bridge carries evidence IDs; source updates
  cascade-invalidate. No provenance, no ship.

## Interface Contracts (branch-level)

1. Bridge record: see root contract 4. Metadata keys exactly:
   `unit_type`, `evidence_ids`, `created_at`, `verified`.
2. Generation pass signature (B02):
   `GenerateBridges(ctx, store MemoryStore, gen LLMClient, cfg BridgesConfig) (generated int, rejected int, err error)`
   in `internal/memory/bridges.go`. df window: `2 <= df(e) <= cfg.BridgeDFMax`.
   Per bridge entity: max 5 source memories, max 8 facts each (paper §3.2.2).
3. Verification (B03): entailment verdict is grammar-constrained JSON
   `{"supported": bool, "reason": string}`; unsupported bridges are deleted
   before ever becoming retrievable; `verified=true` set on survivors.
4. Invalidation (B03): hook into source-memory update/delete/expiry paths —
   delete bridges whose `evidence_ids` intersect the mutated memory IDs.
   `staleness.go` is the expected host; locate the exact seam with search_files.
5. Recall (B04): after retrieval, before prompt assembly — greedy: all AKUs
   pass; bridges admitted while `bridges_included < cfg.KB`. Tag check via
   metadata `unit_type == "bridge"`.
6. Config keys: `memory.bridges.enabled` (false), `.kb` (2), `.bridge_df_max`
   (10), `.verify` (true) — schema.go, JSON5 tags, root knob table wins.

## Child Index

| # | Document | Depends on | Status |
|---|---|---|---|
| B01 | `01-bridge-store.md` | — | NOT STARTED |
| B02 | `02-bridge-generation.md` | B01 | NOT STARTED |
| B03 | `03-verify-invalidate.md` | B02 | NOT STARTED |
| B04 | `04-balanced-recall.md` | B01 | NOT STARTED |
| B05 | `05-bridge-eval.md` | B02, B03, B04 | NOT STARTED |
| B06 | `06-wiring-docs.md` | B04 | NOT STARTED |

## Dispatch Protocol

Same as root. B01 first (everything depends on the store). Then B02 and B04 in
parallel (different seams: Consolidator vs recall path). B03 after B02. B05 and
B06 last, in parallel. B05's eval report is delivered to the USER as a gate —
the orchestrator does NOT flip any default based on it; adoption is the owner's
call.

## Review Checklist

Root checklist applies, plus:
- [ ] No bridge generation code reachable from the chat/inference path (grep the call graph)
- [ ] Bridges excluded from dual_store gossip (verify gossip serialization skips unit_type=bridge)
- [ ] Every persisted bridge row has non-empty evidence_ids (reject otherwise at write time)
- [ ] Invalidations fire on update, delete, AND expiry of source memories

## Completion Tracking Table

| Document | Status | Reviewed | Committed |
|---|---|---|---|
| 01-bridge-store.md | NOT STARTED | — | — |
| 02-bridge-generation.md | NOT STARTED | — | — |
| 03-verify-invalidate.md | NOT STARTED | — | — |
| 04-balanced-recall.md | NOT STARTED | — | — |
| 05-bridge-eval.md | NOT STARTED | — | — |
| 06-wiring-docs.md | NOT STARTED | — | — |

## Integration Test Plan

`go test -p 2 ./internal/memory/...` after each leaf; full sweep per root plan.
B05 eval run instructions are in its leaf; the eval artifact (JSON report) is
committed under `docs/research/eval/bridges-<date>.json` alongside the B05
commit.

## Coding Conventions

Root conventions apply. Grammar-constrained generation reuses the distill
grammar wiring (distill_grammar_wiring_test.go shows the pattern). No mutex
across I/O in Consolidator passes (repo rule) — batch DB writes outside locks.
