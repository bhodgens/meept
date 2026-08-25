# Memory Lessons & Procedures Distillation - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** New memory types lesson/procedure distilled from reflection outcomes; injected via existing tiers with type tags.
- **Deps:** 14 (usefulness scoring ranks them) | **Context:** 55K | **Group:** D

## Goal

atomic-agent distinction: lessons = distilled principles ("always X before Y because Z"), procedures = reusable how-to templates (NOT auto-executed). Meept's reflection collector already proposes improvements; extend the memory side so recurring successful patterns condense into these two types, ranked by leaf-14 usefulness, injected when relevant.

## Context

internal/agent/reflection_collector.go (proposals queue), internal/memory task domains + injection tiers (recall_mode auto). TF-IDF dedup exists in skill evolver — reuse its similarity helper if exported, else local cosine.

Key files: reflection_collector.go, internal/memory (types/store/injection), docs/workflows/memory.md + learning.md cross-ref.

## Interface Contracts (From Parent)

```go
// internal/memory/types.go additions:
const TypeLesson = "lesson"; const TypeProcedure = "procedure"
// Lesson: {principle string, because string, evidence_ids []string}
// Procedure: {title, steps []string, trigger_hints []string} stored as
//   structured JSON in existing content field + domain="procedure"|"lesson".

func (m *Manager) Distill(ctx context.Context, src []Memory) (*Memory, error)
// LLM summarizer path (existing summarization infra): prompt template per type;
// dedupe vs existing via cosine > .85 on embeddings when available else token-Jaccard.
```

Injection: existing context injector gains type-aware section — lessons render as one-line principles under "learned constraints"; procedures as titled outlines under "known procedures" ONLY when query/intent similarity passes threshold (reuse confidence threshold pattern from skills).

Reflection wiring: collector proposals of kind "pattern" route to Distill queue drained by existing evolver cycle timing (no new scheduler entry).

## Tasks
1. Failing tests store roundtrip for new types incl. malformed-JSON rejection at read.
2. Failing tests Distill w/ fake summarizer: dedupe suppresses near-duplicate; evidence ids preserved; length caps enforced (lesson<=280 chars principle, procedure<=20 steps).
3. Failing tests injection: relevant procedure appears once; irrelevant absent; flag [memory.distill] enabled default FALSE gates everything.
4. Reflection routing plumbing + docs sections both pages.

## Self-Verification Checklist
- [ ] -race green touched pkgs
- [ ] Flag-off zero behavior change
- [ ] No auto-execution path for procedures anywhere (grep asserts)

## Review Checklist
- [ ] Summarizer failure -> skip distill gracefully (queue retains)
- [ ] Injection caps total added tokens (existing budget respected)
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: quality bar matters more than volume — verifier-style rubric NOT in scope here (skill evolver's judge covers promotion elsewhere); keep distill conservative.
