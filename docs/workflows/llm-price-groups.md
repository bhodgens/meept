# Price groups and the paid spillway (sovereign-router-parity tree, leaf 09)

Requirement R9, Contract C10, file: `internal/llm/pricegroup.go`.

A price group tags a model_aliases entry as a routing tier: "free", "paid", or "premium". Untagged ("") is legacy: the alias resolves only when explicitly selected and never participates in a spillway step.

```
// inside model_aliases.<alias> (sibling of `models`):
"model_aliases": {
  "classifier": {
    "models": ["local-gguf/lfm-8b-gguf"],
    "price_group": "free"          // "" | "free" | "paid" | "premium"
  }
}
```

(The `model_aliases` map in `config/models.json5` lists per-alias entries with `models`/`timeout`/etc.; `price_group` sits alongside them. The exact key spelling is pinned by `ModelAliasEntry.PriceGroup` in `internal/llm/providers.go` (leaf 01 declares it, inert).)

## What the spillway does

`ResolveSpillway(ctx, groups, fc)` (*Resolver, on the existing resolver) picks the cheapest eligible alias whose configured group is in `groups`, applying the same selection filters as the live path (posture, quality, admission) for the supplied FilterCtx. It does NOT reorder alias rotation, touch sticky pins, or mutate AliasHealth.

```
ev := &llm.FilterCtx{...}
mc, alias, err := resolver.ResolveSpillway(ctx, []string{"free"}, ev)
if errors.Is(err, llm.ErrNoEligibleSpillwayGroup) { ... re-invoke with []string{"paid","premium"} ... }
```

Tier stepping is caller-driven: `["free"]` then, on exhaustion, `["paid","premium"]` is "free first, paid on exhaustion". No automatic spillway exists in the default resolution path.

`AliasesByPriceGroup(g)` returns the sorted tag list for one group.

## Contract

- Untagged = invisible to the spillway; default single-alias resolution is byte-identical.
- Eligibility: the alias's `PriceGroup` parses into `groups` and ALL member models pass `runSelectionFilters` (VerdictAllow) for the supplied fc.
- Sort key: `(min member TotalCost asc, max member quality desc)`; float compare uses costEpsilon = 1e-9 (never a bare !=). Member quality reads `ModelConfig.Quality.QualityFor(QualityClass(fc.Capability))`; absent quality scores are 0 (inert).
- `ResolveSpillway` returns the chosen config, the winning alias name, and an error; `ErrNoEligibleSpillwayGroup` joins the ErrAll*Blocked sentinel family (errors.Is).
- `ResolveSpillway` is exported but has no caller in v1 - it is an API surface for future daemon chat options or CLI flags. Available-but-uncalled is a decision, not accidental dead code (leaf 09 T8).
- This is tier ordering, not a multi-tenant gate: no per-identity policy, no premium gate, no per-client lanes.

## Wiring status (as of the leaf's landing)

Not reachable from chat/task turns or from the CLI. A future consumer (a daemon chat option like "free_first" or a `meept` flag) calls ResolveSpillway at the call site. Do not wire it into ResolveForAlias without a product decision - it is deliberately opt-in per caller.

## Related contracts

- C10 in `docs/plans/20261115-sovereign-router-parity/master.md` (frozen sorts, 3-return shape)
- Leaf 01 declares `ModelAliasEntry.PriceGroup` (inert) and the `internal/llm/selection.go` filter chain consumed here.
