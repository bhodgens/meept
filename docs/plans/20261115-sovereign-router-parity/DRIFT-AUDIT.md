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
6. **C8 wording**: today `StickyPins` is `map[string]int` (models.go:440).
   Leaf 07's per-pin difficulty storage (map conversion) is the accurate
   description; "StickyPin struct gains Difficulty" was wrong.

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
