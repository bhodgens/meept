# Research: Tiered Agent Memory Architectures - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Execute the research tasks below and write the pinned findings doc. This
> is a RESEARCH leaf: use web_search extensively; produce a findings
> document, not code. Do NOT commit - the orchestrator handles all git
> operations after review.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Research tiered agent memory architectures (MemGPT/Letta-style
  working/episodic/semantic), eviction policies, compression/distillation,
  and promotion/demotion, pinned in a findings doc siblings cite.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A
- **Audit references:** R11

## Goal

Produce `docs/plans/20261031-context-memory-engine/02-memory-tiers/
findings-memory-tiers.md`: a pinned, sourced summary of how production and
research systems structure memory tiers, when they evict vs compress vs
demote, and what this tree should adopt. Siblings (01, 02, 03) MUST cite
this doc by recommendation ID (R-Bx) in their Notes.

## Context

Meept today: episodic store (internal/memory/episodic.go, FTS5), async L1
claims + L2 scenes via queue jobs memory.l1_extract / memory.l2_scene
(internal/memory/distill_async.go:31-33), L0 raw JSONL opt-in
(internal/config/schema.go:1593-1597), usefulness eviction
(internal/memory/consolidation.go:537-566), single-start Consolidator
(:60,96), staleness scoring (internal/memory/staleness.go). Missing: a
working tier bound to the conversation buffer, automated policy execution,
demotion. Research should cover MemGPT/Letta memory hierarchy, eviction
policy literature (LRU/LFU/usefulness hybrids), compression/distillation
thresholds, and promotion/demotion criteria.

## Verified Anchors

- internal/memory/consolidation.go:537-566 - PlanUsefulEviction - eviction ranking to reuse
- internal/memory/distill_async.go:31-33 - L1/L2 queue jobs - compression pipeline to reuse
- internal/memory/staleness.go - staleness scoring - demotion signal
- internal/config/schema.go:1593-1597 - l0_enabled opt-in prior art - the opt-in pattern to follow

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
findings-memory-tiers.md with REQUIRED sections (exact headings):
  ## Tier Architectures
  ## Eviction Policies
  ## Compression and Distillation
  ## Promotion and Demotion
  ## Recommendations for the Tier Engine
Each recommendation numbered R-B1, R-B2, ... so siblings cite by ID.
```

### What This Leaf Consumes

- none (research leaf)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Survey tier architectures via web_search: MemGPT paper, Letta docs, at least 2 more tiered-memory systems (e.g. generative agents memory stream, HippoRAG-style, commercial agent memory products) | none (notes) | manual review | n/a (research) | >=5 distinct cited sources | Given results; When synthesized; Then each architecture gets a tier-boundary description |
| T2 | Research eviction policies: LRU/LFU/usefulness-based hybrids, capacity triggers (items vs tokens), TTL/retention, small-store edge behavior | none (notes) | manual review | n/a | Eviction section >=4 policies with URLs + applicability notes | Given policies; When recorded; Then token-vs-item trigger tradeoffs stated |
| T3 | Research compression/distillation: when to compress vs evict, summarization thresholds, lossy-vs-lossless tier transitions (episodic->semantic claims), cost bounds | none (notes) | manual review | n/a | Compression section >=3 policies | Given thresholds; When recorded; Then each has a trigger condition and a loss note |
| T4 | Research promotion/demotion: criteria for semantic->episodic demotion (staleness, low retrieval hit rate, contradiction), promotion criteria, idempotency concerns | none (notes) | manual review | n/a | Demotion section covers >=3 criteria with sources | Given criteria; When recorded; Then idempotency requirement explicit |
| T5 | Write findings-memory-tiers.md with the five required headings + numbered Recommendations R-B1..N mapping to C4/C5/C6 design (ring sizing, trigger ratios, compress-vs-evict rules, demotion criteria) | Create: docs/plans/20261031-context-memory-engine/02-memory-tiers/findings-memory-tiers.md | `grep -c '^## ' docs/plans/20261031-context-memory-engine/02-memory-tiers/findings-memory-tiers.md` | file absent | >=5 headings, >=6 recommendations, all ASCII | Given notes; When written; Then recommendations concrete (numbers) and ASCII-only |

## Self-Verification Checklist

- [ ] All tasks completed; findings doc exists at the exact path
- [ ] All five required headings present, exactly as specified
- [ ] Every factual claim carries a source URL; >=6 distinct sources
- [ ] At least 6 numbered recommendations (R-B1..)
- [ ] ASCII only; no code changes

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [grep heading count + source count]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Findings doc exists with exact required headings
- [ ] Sources cited per claim
- [ ] Recommendations concrete and implementable; by-ID citation scheme
- [ ] ASCII only; no code changes anywhere

Output: APPROVED or specific gaps.

## Notes

Search at least: "MemGPT operating system memory LLM", "Letta memory
hierarchy", "generative agents memory stream retrieval", "agent memory
eviction policy", "memory consolidation agents distillation", "semantic
episodic memory demotion AI". Tie recommendations to the C4/C5/C6
contracts in ../orchestrator.md (ring sizing, EvictOverRatio defaults,
compress-vs-evict precedence, demotion idempotency).
