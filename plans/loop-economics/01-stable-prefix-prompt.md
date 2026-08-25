# Stable-Prefix Prompt Assembly - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Reorder prompt sections stable-first; expose stable-prefix hash for provider caching + drift detection.
- **Deps:** none | **Context:** 55K | **Group:** A

## Goal

Provider prompt caches hit only on byte-identical prefixes. Meept assembles system prompts with volatile content (memory context, tool lists) interleaved with stable content, forfeiting cache hits every turn. Reorder: all stable sections first in fixed order, volatile last; expose sha256 of the stable prefix so callers can log drift and tests can pin stability.

## Context

internal/agent/prompt.go — PromptBuilder (WithMemoryContext etc.) + BuildSystemPrompt + BuildSystemPromptWithOverride (~line 379). Sections currently appended in call order. Callers: loop.go system prompt build sites. AGENTS.md: no os.Getwd in daemon paths; lowercase UI strings.

Key files: internal/agent/prompt.go (+_test.go), internal/agent/loop.go (call sites), internal/llm (request assembly if hash should ride along — keep OUT of llm this leaf).

## Interface Contracts (From Parent)

```go
// internal/agent/prompt.go additions:
type PromptSection struct { Name string; Stable bool; Body string }
func AssembleOrdered(sections []PromptSection) (prompt string, stablePrefixHash string)
// Stable sections first preserving given order; then unstable in given order.
// stablePrefixHash = sha256 hex over exact bytes of the concatenated stable prefix.
// Empty stable set -> hash of "".
```

Behavioral requirements:
- Existing BuildSystemPrompt output unchanged by default path (compat) unless a new config flag [agent] cache_stable_prefix=true routes through AssembleOrdered. Default flag TRUE for NEW ordering? DECISION: default true — ordering change is the feature; document that override-paths unchanged.
- formatToolDescription output must not embed per-turn values (verify; if memory refs appear inside tool descriptions, they belong to unstable section).

## Tasks

1. **Failing tests** (prompt_test.go): ordering (stable before unstable regardless of input order); byte-stability (two calls same inputs -> identical string+hash); volatility isolation (changing unstable body leaves prefix hash equal); empty-stable hash = sha256(""); override path untouched.
2. **Implement** AssembleOrdered + wire BuildSystemPrompt to construct sections (name each existing chunk: identity/rules/skills/tools/memory/conversation-hints) marking tools/memory unstable ONLY if they vary per turn — inspect actual builders and classify honestly; record classification in code comment.
3. **Call-site wiring**: loop.go builds use new path when config enabled ([agent] cache_stable_prefix bool default true); expose l.LastStablePrefixHash() for logging/metrics hook (metrics optional debug log fine).
4. **Drift test**: simulated two-turn conversation where only conversation tail changes -> same prefix hash across turns (the actual point).
5. Docs paragraph in docs/workflows/context-firewall.md or nearest page.

## Self-Verification Checklist
- [ ] -race green internal/agent
- [ ] Hash deterministic; documented
- [ ] No behavior change when flag false
- [ ] Docs updated

## Review Checklist
- [ ] Contracts exact; classification comments honest
- [ ] No perf regression on prompt build (no new allocations per section beyond slice)
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: do NOT touch llm request layer; hash surfaces via loop method only.
