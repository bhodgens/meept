# Memory Usefulness Scoring + Voting - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** memory_vote tool + usefulness score driving consolidation eviction (replacing age-only culling).
- **Deps:** none | **Context:** 60K | **Group:** C

## Goal

Consolidation currently evicts by age/recency heuristics. atomic-agent semantics: explicit votes (useful/harmful + reason) feed a usefulness score; eviction targets low-usefulness percentile instead of oldest-first. Opt-in flag until validated.

## Context

internal/memory: manager.go, ftstore (FTS5), consolidation/merge paths (features.md documents MergeRelated 3-tier). Locate actual eviction decision point via search_files "consolidat" under internal/memory.

Key files: internal/memory/*.go, tools/builtin/memory.go (tool family), config [memory.usefulness].

## Interface Contracts (From Parent)

```go
// internal/memory/usefulness.go:
type VoteRecord struct{ MemoryID string; Delta int /*+1|-1*/; Reason string; CreatedAt time.Time }
func (m *Manager) RecordVote(id string, delta int, reason string) error // votes table in existing sqlite db
func Usefulness(v votes, accesses int, ageDays float64, w Weights) float64
// clamp01( base + Wv*sumVotes + Wa*log1p(accesses) - Ws*ageDays )
// Defaults: base .5, Wv .08, Wa .05, Ws .005 (config-overridable).
```

Eviction integration: consolidation's candidate-ranking function swaps to usefulness ordering when [memory.usefulness] enabled=true (default FALSE this tree); below-floor percentile (config floor_pct default 0.05) evicted BEFORE any age-based rule; harmful-vote-heavy memories (<= -2 net) evict regardless of age.

Tool: memory_vote{memory_id, delta(-1|1), reason?} -> confirmation-free, evidence emitted.

## Tasks
1. Failing tests scoring math (table incl. clamps), vote persistence roundtrip.
2. Failing tests eviction ordering with flag on: high-vote survivor beats newer low-vote; harmful eviction; flag-off -> byte-identical legacy ordering.
3. Tool registration + tests.
4. Config plumbing + docs paragraph (memory.md workflow page).

## Self-Verification Checklist
- [ ] -race green internal/memory internal/tools/builtin
- [ ] Flag-off path untouched (prove via existing consolidation tests unmodified)
- [ ] Reasons length-capped (512B)

## Review Checklist
- [ ] No N+1 queries in re-scoring pass (batch)
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: LongMemEval will judge this later — keep score formula isolated/testable.
