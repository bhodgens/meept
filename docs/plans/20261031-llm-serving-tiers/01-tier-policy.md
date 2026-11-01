# Tier Policy: Degradation-Depth Limits, Per-Turn Ceilings, Degraded-Tier Signaling - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** master.md
- **Scope:** Explicit degradation-depth limits + per-turn tier ceilings on alias rotation, plus user-visible "served by degraded tier" signaling in per-step response metadata.
- **Dependencies:** R0-research.md (findings doc; cite in code comments where relevant)
- **Estimated Context:** 95K
- **Concurrency Group:** B
- **Audit references:** proposal 9 (tier policy)

## Goal

Today alias rotation is unbounded: a turn silently falls through any number
of alias members. This leaf adds an explicit policy: a per-request
degradation depth limit, optional per-capability per-turn ceilings, and a
typed floor error that stops rotation beyond the limit. When enabled, the
serving tier is surfaced to the user through per-step response metadata
(`llm_tier`, `llm_tier_degraded`, `llm_tier_depth`). When
`llm.tiers.enabled=false` (the default), behavior is byte-identical to
today.

## Context

- The resolver/alias machinery lives in internal/llm/resolver.go and
  internal/llm/provider_manager.go. Alias rotation on failure (empty
  responses, provider errors) happens in the provider-manager retry loop.
- internal/llm/AGENTS.md is the package invariant doc: quota blocks,
  refusals, alias resolution, session affinity, sticky rotation. READ IT
  FIRST (terminal cat) before touching anything.
- Config structs live in internal/config/schema.go under the LLMConfig
  neighborhood (llm.session_affinity / llm.cache fields are nearby
  examples of the established pattern, including doc comments naming the
  owning tree).
- Errors internal/llm/errors_overflow.go shows the established typed-error
  pattern (ContextOverflowError) this leaf mirrors for the floor error.

## Verified Anchors

- internal/llm/resolver.go:616,659 - FROZEN precedence: quota blocks >
  endpoint cooldowns > alias blocks > stickiness - NEVER reorder; this
  leaf only BOUNDS how far rotation may proceed.
- internal/llm/client.go:1979 - ErrEmptyResponse sentinel: empty completions
  rotate to the fallback; that rotation is what depth counting observes.
- internal/llm/errors_overflow.go:25 - ContextOverflowError - the typed
  error pattern to mirror.
- internal/config/schema.go:1043-1050 - LLMConfig feature-field pattern
  (llm.session_affinity example) - where llm.tiers lands.
- internal/llm/AGENTS.md - package invariants (quota-reset-resilience,
  sticky rotation). Update it in the same change if you alter any
  documented behavior (the enabled=false path alters nothing).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/config/schema.go (json5 key: llm.tiers) - Contract C1
type TierPolicyConfig struct {
    Enabled        bool           // default false
    MaxDepth       int            // default 2 when Enabled
    TurnCeilings   map[string]int // capability -> per-turn max total degradations
    SignalDegraded bool           // default true when Enabled
}

// internal/llm/errors_tier.go - Contract C2
var ErrTierFloor = errors.New("llm: tier degradation floor reached")
// classified terminal error; reachable via errors.Is; never retried,
// never surfaced as a completed turn, never parks a turn.

// Per-step response metadata keys - Contract C3 (PINNED channel)
//   "llm_tier"          string - canonical model id that served
//   "llm_tier_degraded" bool   - true when depth > 0
//   "llm_tier_depth"    int    - degradations from primary
// written only when Enabled && SignalDegraded.
```

### What This Leaf Consumes

- None from sibling leaves (leaf 05 consumes this leaf's contracts).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add TierPolicyConfig to internal/config/schema.go with defaults (Enabled=false, MaxDepth=2 when enabled, SignalDegraded=true when enabled) + defaults wiring | Modify: internal/config/schema.go; Test: internal/config/schema_tiers_test.go | `go test -p 2 ./internal/config/ -run TestTierPolicy` | compile error: TierPolicyConfig undefined | defaults test green | Given a fresh LLMConfig; When defaults apply; Then Enabled=false, MaxDepth=0 (inactive while disabled), SignalDegraded=true-with-enabled documented, and json5 keys are llm.tiers.* |
| T2 | Depth counter + typed ErrTierFloor in the alias-rotation path: count only rotations that DISPATCHED and failed (quota-block skips consume NO depth); exceeding MaxDepth (or the capability's TurnCeiling) stops rotation with ErrTierFloor as a classified terminal error | Create: internal/llm/errors_tier.go; Modify: internal/llm/provider_manager.go (rotation loop), internal/llm/resolver.go (only if the capability ceiling needs plumbing - keep minimal); Test: internal/llm/tier_policy_test.go | `go test -p 2 ./internal/llm/ -run TestTier` | compile error: ErrTierFloor undefined | depth + floor tests green | Given an alias chain of 4 members where each returns an empty response; When MaxDepth=2; Then the loop dispatches at most 3 members (primary + 2 degradations) and errors.Is(err, ErrTierFloor); and a quota-BLOCKED member is skipped with zero depth consumed; and TurnCeilings["small"]=1 caps the small capability to 1 degradation per turn (per-turn counter resets each turn) |
| T3 | Degraded-tier signaling: when Enabled && SignalDegraded, write llm_tier / llm_tier_degraded / llm_tier_depth into the per-step response metadata on every completed step (degraded or not); write NOTHING when disabled | Modify: internal/llm/provider_manager.go or the step-result plumbing it already feeds (whichever carries existing per-step metadata today); Test: internal/llm/tier_policy_test.go (metadata cases) | `go test -p 2 ./internal/llm/ -run TestTierSignal` | no llm_tier key in metadata map | metadata tests green | Given a turn served by the 2nd alias member with tiers enabled+signaling; When the step completes; Then metadata carries llm_tier=<member-2 id>, llm_tier_degraded=true, llm_tier_depth=1; and with tiers disabled the metadata map contains none of the three keys (zero-behavior rule) |
| T4 | Precedence regression guard: prove a quota-blocked endpoint is skipped BEFORE depth counting and before any cache-affinity consideration (precedence stays strictly above everything new) | Test: internal/llm/tier_policy_test.go | `go test -p 2 ./internal/llm/ -run TestTierPrecedence` | test absent | precedence test green | Given member-1 quota-blocked and MaxDepth=2; When resolution runs; Then member-1 is skipped as unavailable (no request dispatched, depth 0 consumed) and rotation starts at member-2 |

**Worked example (T2 shape):**

1. Write the failing test first (internal/llm/tier_policy_test.go):
   build a 4-member stub alias chain returning empty responses; set
   MaxDepth=2; assert the loop stops with errors.Is(err, ErrTierFloor)
   after 3 dispatched members.
2. Verify RED: `go test -p 2 ./internal/llm/ -run TestTierFloor`
   Expected: FAIL - ErrTierFloor undefined / loop unbounded.
3. Implement: add errors_tier.go with the sentinel; thread a depth
   counter through the rotation loop; classify the floor error exactly
   where ErrAllModelsQuotaBlocked-style terminal errors are classified.
4. Verify PASS: `go test -p 2 ./internal/llm/` all green.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2` on touched packages)
- [ ] Interface contracts satisfied exactly (C1/C2/C3 names and keys)
- [ ] All files at exact specified paths
- [ ] Precedence invariant holds: quota blocks > cooldowns > alias blocks >
      stickiness, all strictly above any tier/depth logic; NO cache-affinity
      preference introduced anywhere
- [ ] Zero behavior change when llm.tiers.enabled=false (default config
      byte-identical behavior; metadata keys absent)
- [ ] Depth counts only dispatched-and-failed rotations (quota skips are free)
- [ ] ErrTierFloor is classified terminal: never retried, never a completed
      turn, never parks a turn
- [ ] gofmt clean; ASCII only in every touched file
- [ ] No scope creep - only what the tasks specify
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Contracts C1/C2/C3 match exactly (names, json5 keys, metadata keys)
- [ ] FROZEN precedence untouched (resolver.go:616,659 block ordering)
- [ ] Disabled path is byte-identical (grep: no new code executes when
      tiers.enabled=false)
- [ ] Depth semantics: quota skips consume no depth; TurnCeilings reset per turn
- [ ] No bugs, no security issues, no debug artifacts, no line-number
      corruption, ASCII only

Output: APPROVED or specific gaps with file + line references.

## Notes

- Do NOT touch internal/llm/client.go cache paths (leaf 02 owns them) and
  do NOT touch the streaming delta path (leaf 04 owns it). If you find
  yourself editing either, stop and report.
- The pinned design decision (master Open Questions): only
  dispatched-and-failed rotations consume depth. Do not reopen it.
- per-turn ceiling bookkeeping must live on the request/turn context, not
  package-global state - concurrent turns must not share counters.
