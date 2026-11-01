# Embedding-Ranked Skill Order - Implementation Leaf (GATED on Q4)

> **For the implementing agent:** Implement ALL tasks using TDD. Do NOT
> commit. GATED: this leaf dispatches only after ../master.md Q4 is answered
> affirmatively; the dispatch context carries the answer. If Q4 says
> "listing-only until 100+ skills", this leaf is cancelled - orchestrator
> marks it CANCELLED in the tracking table and does not dispatch.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** order the C4 listing by embedding similarity to the latest user task at episode start; never hide skills
- **Dependencies:** 01-skill-listing COMPLETE; Q4 answer
- **Estimated Context:** 85K
- **Concurrency Group:** solo

## Goal

At episode start only, rank visible skills by cosine similarity between the
latest user task text and each skill's name+description embedding. Output
order changes; the SET never does (no cap reintroduced). Ranking must be
cache-safe: computed once per episode, frozen for the episode, so the skills
prompt section stays byte-stable within the episode.

## Context

- Embedding source: local embedding endpoint if configured (models.json5
  capability-based resolution - check for an embedding capability; if none
  exists, this leaf includes adding the capability slot per the meept-design
  references/model-slots guidance, or falls back to the classifier model's
  hidden states ONLY if a cheap path exists - decide in T1 design note).
- Cache: skill embeddings change only when SKILL.md frontmatter changes -
  embed at discovery time, cache in the LazySkillLoader index (in-memory +
  ftstore if a store pattern already exists).
- Failure mode: embedding unavailable -> alphabetical order (today's
  behavior). Ranking is an optimization, never a gate.

## Verified Anchors

- internal/agent/context_injector.go - listing build site (post leaf 01)
- internal/skills/index.go - SkillIndex (embedding cache home)
- config/models.json5 - capability resolution precedent

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/skills
func (idx *SkillIndex) SetEmbedder(e Embedder)          // nil guard required
type Embedder interface { Embed(ctx context.Context, texts []string) ([][]float32, error) }
func (idx *SkillIndex) RankedNames(taskText string, fallback []string) []string // cosine-ranked; fallback order on any error/empty
// context_injector consumes RankedNames at episode start only
```

### What This Leaf Consumes

- C4 line shape (order-only change); episode-boundary rule (branch 03 parent contract)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Embedder seam + cache | Modify: internal/skills/index.go | `go test -p 2 ./internal/skills/ -run TestEmbed` | RED | embeddings cached per skill; re-embed only on frontmatter hash change | Given same SKILL.md twice; When indexed; Then one embed call |
| T2 | RankedNames | Modify: internal/skills/index.go | `go test -p 2 ./internal/skills/ -run TestRankedNames` | RED | cosine order; complete set preserved; deterministic ties (name asc) | Given stub embedder with fixed vectors; When ranked; Then expected order exactly |
| T3 | Injector wiring (episode-start) | Modify: internal/agent/context_injector.go | `go test -p 2 ./internal/agent/ -run TestSkillsRanking` | RED | ranking computed once per episode; within-episode builds byte-identical | Given two turns one episode; When built; Then identical skills section |
| T4 | Graceful degradation | Modify: index.go | `go test -p 2 ./internal/skills/ -run TestRankedNamesFallback` | RED | embedder nil/error -> fallback order, no episode failure | Given erroring embedder; When ranked; Then fallback order returned |
| T5 | Config + docs | Modify: internal/config/schema.go (embedding endpoint ref), docs/workflows/skills.md | `go test -p 2 ./internal/config/` | RED | opt-in config key default OFF; docs updated | Given key absent; When loaded; Then alphabetical behavior (leaf 01) |

## Self-Verification Checklist

- [ ] Set never shrinks (no cap reintroduced) - test asserts set equality
- [ ] Float determinism: epsilon compare, sorted map keys before accumulation, -count=10 clean (Pitfall 26)
- [ ] Within-episode byte stability tested
- [ ] Opt-in default OFF (opt-in-defaults invariant)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. verify tails; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] Ranking is order-only; C4 lines unchanged in shape
- [ ] Episode-boundary computation (no per-turn embed calls)
- [ ] Degradation path silent-safe (log warn, alphabetical)
- [ ] Determinism under -count=10
