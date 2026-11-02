# Branch 04 - Skill Routing Without the Cap - Orchestrator

> **For the executing agent:** You are the orchestrator. Leaf 00T (gate) and
> 01 (listing fix) are dispatchable now. Leaf 02 (embedding-rank) is GATED on
> ../master.md Q4. Review in-session; implementation agents never commit.

## Meta

- **Parent:** ../master.md
- **Gap:** GAP-4: skill relevance matches the SYSTEM PROMPT BASE (wrong
  signal), caps at 5, and falls back to ALL cached skills on zero matches
  (internal/agent/context_injector.go:15,121-166). Pi lists every skill as one
  line, no cap, model pulls bodies on demand (skills.ts:358-388).

## Design Decision (pi-parity, cap removed)

Replace the relevance-filtered 5-skill list with a pi-shape listing: ONE LINE
per visible cached skill (name + description + load path), NO CAP, matched
against nothing - the model decides relevance itself from descriptions.
Contract C4 (../master.md) pins the line shape. `disable-model-invocation: true`
frontmatter hides a skill (pi parity; user-triggered skills stay reachable via
the skill executor path, just not listed).

Why this is cheap: meept ships 15 skills (config/skills/); at ~25 tokens/line
the full listing is ~400 tokens - far below today's fallback (all cached) and
more accurate than fuzzy-matching the prompt base. The 5-cap and the
MatchAll(base) signal are both removed. `relevantSkillNames` shrinks to
"all visible skills, ordered".

Ranking at scale (leaf 02, GATED on Q4): when the library exceeds ~100
skills, order lines by embedding similarity to the LATEST USER TASK (not the
prompt base), never hide. Reuses the classifier/embedding infra if a local
embedding endpoint exists; otherwise deferred.

## Child Index

| # | Child | Type | Depends on | Est. context | Concurrency |
|---|-------|------|-----------|--------------|-------------|
| 00T | 00T-gate.md | test leaf | none | 40K | solo, FIRST |
| 01 | 01-skill-listing.md | leaf | 00T VALIDATED | 75K | solo |
| 02 | 02-embedding-rank.md | leaf (GATED) | Q4 answer + 01 | 85K | GATED |

## Dispatch Protocol

1. Dispatch 00T -> validate against Test Gate matrix -> commit -> TESTS_VALIDATED.
2. Dispatch 01; review in-session (contract C4 line shape byte-exact; cap
   constant removed; MatchAll(base) signal removed; disable-model-invocation
   honored); commit.
3. Leaf 02 only after Q4 recorded in ../master.md Open Questions.
4. Cache-stability note: the skills listing lives in the system prompt. It
   must be built ONCE per episode and marked stable (AddSectionWithStability
   or the ordered stable prefix) so per-turn prompt bytes do not churn the
   cache. Review asserts this.

## Interface Contracts

C4 (../master.md) FROZEN: line shape, no cap, disable-model-invocation key.
Additional pinned behavior:
- Zero-match fallback DELETED (there is no matching anymore; listing is
  unconditional).
- Skill order: alphabetical by name (deterministic = cache-stable). Embedding
  ranking (leaf 02) reorders only at episode start.
- Frontmatter parsing: `disable-model-invocation` accepted in
  internal/skills discovery alongside existing keys; unknown-key policy
  unchanged (no hard failure on old skill files).

## Test Gate

| Feature | Gate leaf | Test target | Status |
|---------|-----------|-------------|--------|
| no-cap listing + line shape | 00T-gate.md | internal/agent/context_injector tests (existing file, bug-fix class) | PENDING |
| disable-model-invocation honored | 00T-gate.md | internal/skills discovery tests (existing files) | PENDING |
| listing stable across turns (cache) | 00T-gate.md | context_injector tests | PENDING |

## Coding Conventions

Inherited from ../master.md. Branch-specific:
- context_injector.go:15 maxRelevantSkills const REMOVED (not raised) - dead
  code deleted in the same change (staticcheck U1000 blocks otherwise).
- No new config key for the listing itself (it replaces existing behavior);
  disable-model-invocation is frontmatter, not config.

## Completion Tracking Table

| Child | Status | Commit | Notes |
|-------|--------|--------|-------|
| 00T-gate | TESTS_VALIDATED | pending | 4 pinned C4 tests in internal/agent/context_injector_test.go (T4 re-pinned to per-turn base churn - the cache-stability contract - which is RED today; two-identical-builds variant would have been green and defeated the gate) + TestSkillsFrontmatterDisableModelInvocation in internal/skills/parser_test.go (field asserted via reflection so the gate compiles against current code; turns green when leaf 01 T1 adds the field). All 5 fail RED behaviorally; pre-existing tests green; no implementation touched |
| 01-skill-listing | COMPLETE | 6169fe4a | C4 no-cap listing + disable-model-invocation frontmatter; maxRelevantSkills const + MatchAll(base) signal removed; 4 TestSkillsListing* GREEN, agent+skills suites green, 0 cap hits |
| 02-embedding-rank | GATED | - | needs Q4 |

## Integration Test Plan

1. `go test -p 2 ./internal/agent/ ./internal/skills/` green.
2. Prompt-stability check: two consecutive BuildSystemPrompt calls in one
   episode produce byte-identical skills sections.
3. Token delta: listing 15 skills measured before/after (expect ~ -1K tokens
   vs the all-cached fallback, +accuracy vs the 5-cap).

## Review Checklist

- [ ] C4 line shape byte-exact (including the `[load: ...]` hint)
- [ ] Cap const gone; MatchAll(base) gone; fallback-to-all gone
- [ ] disable-model-invocation skills absent from listing but still executable via explicit invocation paths
- [ ] Skills section cache-stable within an episode
