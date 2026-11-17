# Drift Audit Rulings (2026-11-16)

Read-only audit of all ten leaf docs against source at 4ec87974.
Full findings delivered to the orchestrator; this file carries the BINDING
corrections. Every dispatch context cites this file. Where a leaf doc
contradicts a ruling here, this file wins.

## Contract rulings (plan bugs 1-6)

1. **ResolveSpillway = 3 returns** `(*ModelConfig, string, error)` -
   alias name as second return. Master C10 updated; leaf 09 + 00T already
   held this shape.
2. **ClassifyFailure = 5 args WITH poolID** (REVISED 2026-11-16 by user
   + back-lane coordination; the original 4-arg freeze here is void):
   `ClassifyFailure(providerID, modelID, poolID string, status int,
   bodyExcerpt string)` - poolID in the THIRD position, matching the
   00T gate seam and leaf 06. poolID feeds the 429 pool-exhaustion
   branch. Gate tests T12/T14 pin this arity.
3. **ShadowDecision = union shape**: master C6 keeps `Posture Posture`;
   `ShadowCandidate` gains `Quality int` and `EstCostUSD float64`
   (leaf 04's richer candidate shape). Leaf 04 implements the union.
4. **budgetFilter is NOT a selection-chain filter.** Master C1 position 4
   removed. Leaf 08 binds advisory budget at the provider-manager tier
   gate (budget.go + provider_manager.go); no resolver.go writer is added
   (preserves the file-write serialization). Selection chain = 3 filters:
   posture, quality, admission - in that order.
5. **ErrProviderLimit is dropped.** No leaf owns it; rate-window handling
   stays with the existing cooldown machinery. Removed from master C3.
- **C8 wording**: today `StickyPins` is `map[string]int` (models.go:440).
  Leaf 07's per-pin difficulty storage (map conversion) is the accurate
  description; "StickyPin struct gains Difficulty" was wrong.

## Wave-1 rulings (2026-11-16, post-review)

10. **Health checker `Verdict` renamed `HealthVerdict`** (commit c04596c8):
    Contract C1's chain `Verdict` and the health checker's verdict type
    collided in-package. Wire strings unchanged ("healthy"/"unhealthy"/
    "hung"); AGENTS.md refs updated.
11. **Leaf 04 declares the `Posture` type** (+3 constants) in shadow.go -
    C6's union needs it before leaf 05 dispatches. posture.go MUST NOT
    redeclare; it adds parsing/filtering only.
12. **Leaf 01 floor difficulty defaults to 5 in ResolveForSkill** until
    leaf 07 threads the per-turn `DeriveDifficulty` value (gate T3
    requires the floor to bind without the difficulty channel).
13. **Comparator cost term = per-turn estimate** (`estIn*in + estOut*out`),
    falling back to the legacy `TotalCost()` order at zero estimate -
    flag-off byte-identical; quality breaks equal-cost ties only.
14. **Gate helper bug fixed in-session** (commit c04596c8): toolcheck
    streamed-args test called an undefined `jsonMarshal`; helper added.

## Wave-2 rulings (2026-11-16, post-review)

15. **Leaf 05 uses the narrow-interface fallback** for the secret
    scanner: `internal/security -> internal/preferences -> internal/tools
    -> internal/llm` makes the leaf doc's direct import impossible
    (verified via go list -deps). posture.go declares `SecretScanner`;
    the daemon wires `*security.SecretScanner` into it.
16. **The posture prompt seam lives at the resolver resolve-time hook**,
    not the chain hook: a plain chain Skip cannot re-rotate past a
    non-cleared rotation head. Resolver-level scan + re-rotation is the
    mechanism; `FilterCtx.PromptText` carries the request-scoped text.
17. **Leaf 08 T7 WIRED** (2f34d16d): steerReorderedTail at the three
    rotation loops; primary candidate never moves; enforce byte-identical.
    docs/workflows/llm-advisory-budget.md "Wiring" item 3 is now stale
    (says deliberately-not-wired) - flip to as-built at integration.

## Wave-3 rulings (2026-11-17, post-review)

18. **Model-scope arming is deduped into the existing alias cooldown**
    (d7a6e166): a separate model-scope key would convert the
    throttle-only D10 endpoint key into a blocks-everything key and broke
    TestCooldownCursorStaysOnHealthyMember. ClassifyFailure still
    classifies model scope verbatim; only the arming dedupes.
19. **Leaf 06 named the vocabulary type `FailureCategory`**: package
    `llm` already owns `FailureClass` (int enum, failure_policy.go).
    Constant VALUES stay the frozen lowercase strings; gate tests
    pin values, not the type name.
20. **Leaf 06 gap-filled leaves 07/09's compile blockers** (2dcf3045):
    difficulty.go (C11 table + WithDifficulty option) and pricegroup.go
    (full C10 resolution surface + the ModelAliasEntry.PriceGroup field
    leaf 01 declared-in-doc but never wrote). Leaf 07's remaining work is
    the resolve-side release predicate; leaf 09 closes by review against
    the back lane's staged file (their spillwayCostEpsilon rename applies).
21. **402 quota short-circuit (observed, not acted on)**: resolver.go
    VerdictForFailure classifies 402 as quota (D5, frozen contract of the
    quota tree), so account-scope arming fires for 401/403; a 402 reaches
    the scoped path only via non-quota routes. Left as-is deliberately.

## Line-anchor corrections (fold into dispatch contexts)

- 00T: validation.py streamed-outcome fn is at :104-112 (not 113-126);
  checklists referencing "T22's golden fixture" mean T25 (T22 is
  DeriveDifficulty); manifest path_map is 189 entries, not 182.
- 01: resolver.go sort.Slice is :369-371 (not 376-383); providers.go
  ModelConfig construction at :734 (not 737-740); ModelDef struct :74
  (73 = comment); ModelConfig struct range ends :321.
- 01 T10 / 09: ModelAliasEntry struct at providers.go:169-176
  (not :174-182 / :174).
- 02 + 08: `chatWithFailoverRaw` lives in internal/agent/loop.go:6854;
  provider_manager.go:525 is the ordered-provider snapshot inside
  `ProviderManager.Chat` - the leaf-doc label is wrong.
- 02: errors_overflow.go function at :91 (:10 is a naming comment).
- 05: NewSecretScanner() at components.go:8440; wiring :8442/:8533
  (not 8419-8421).
- 06: DemotionTracker at :239-400 (not :228-410).
- 07: Intent.RequiresPlanning at dispatcher.go:111 (not :112).
- 08: anthropic.go sites at :408/:656 (not :431/:679); schema.go
  BudgetConfig at :1858-1870 (not :1809-1821); schema has 11 fields /
  10 limits (not "8 limits").
- 09: the `ErrAll*Blocked` sentinel family lives in resolver.go:28-40,
  not errors_quota.go.

## Verified good (no action)

- All of /tmp/SovereignRouter's frozen constants quoted in 00T verified
  exact against the source (MIN_OUTPUT_TOKENS=64 at gateway.py:246;
  :706 skip text; quality skip :700-701; sort_key :746-749;
  pools.py:176-206 row-for-row; validation codes :78-110).
- No file/symbol ownership collisions across leaves 01-09.
- models.go:229, resolver.go:528/:500/:324/:1014-1101/:1128-1142,
  budget.go:867-914, errors_tier.go:15-45, routing_log.go:91-105,
  secret_scanner.go:42, tier_policy.go:42-47, dispatcher.go:101-140,
  loop.go:5309/:6943 - all exact.
