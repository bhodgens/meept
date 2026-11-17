# Sovereign Router Parity: Quality, Posture, Admission, and Tier Routing

User-facing doc for plan tree `docs/plans/20261115-sovereign-router-parity/`
(requirements R1-R9, contracts C1-C11 in that tree's `master.md`).
Ported capability set from the Sovereign Router design; meept routes now on
cost, quality, data posture, and pool capacity in addition to health,
declared alias order, and cooldowns.

## The one rule that binds everything

All nine features are OPT-IN. With no `quality` block, no `data_posture`,
no `budget.mode`, no `pool`, and no `price_group` configured anywhere, every
routing decision is byte-identical to pre-change `main` — pinned by the
`TestFlagOffByteIdenticalControl` golden test in
`e2e/suites/sovereign-parity/`. The frozen selection precedence
(quota blocks > endpoint cooldowns > alias blocks > stickiness,
`resolver.go`) is untouched; every new filter or gate slots around it.

## The selection chain (C1)

`internal/llm/selection.go` defines one `Filter` interface and one
registration order, fixed at three positions:

1. `postureFilter` (R3) — a sensitive turn never reaches a non-cleared model.
2. `qualityFilter` (R1) — the quality floor for the classified difficulty.
3. `admissionFilter` (R2) — context fit.

`runSelectionFilters` returns the FIRST non-Allow verdict with that
filter's `Reason()`. Callers: `ResolveForAlias` (after winner selection,
before return) and `ResolveForSkill` (inside the candidate comparator).
Advisory budget (R6) is NOT a chain filter — it binds at the
provider-manager tier gate (see `llm-advisory-budget.md`).

## R1 quality (C2) — `quality.go`, `errors_quality.go`

Per-model scores `quality: {chat, code, reasoning, tooling, long-context}`
(0-5, measured — "leaderboards nominate, our own probes score") plus free-text
`quality_source` / `quality_verified_at` provenance. `QualityFloorFor` maps
difficulty 1-2 -> floor 1, 3 -> 2, 4 -> 3, 5 -> 4; difficulty 0 (unclassified)
or an absent block -> floor 0 = inert. The shared comparator
`byCostThenQuality` orders by (per-turn cost estimate, -quality, latency);
with a zero estimate it reduces to the legacy `TotalCost()` order exactly.
When every measured candidate sits below the floor, `ResolveForSkill`
surfaces `*QualityFloorError` (wraps `ErrQualityFloor`, NonRetryable) —
never a best-effort sub-floor serve. `config/models.json5` ships the shape
documented but UNSET (plan Q1 ruling: ship EMPTY).

## R2 admission (C3) — `admission.go` + rotation/bare-client gates

Pre-dispatch context-fit rejection, the frozen rule
`EstInput + min(MaxTokens, 64) > ContextLimit -> skip`;
`ContextLimit <= 0` (unknown) always allows (never fabricate a bound).
`EstimateInputTokens` is a deliberate over-estimate (runes/4 rounded up,
flat 128-token allowance per offered tool). Wired at the `ProviderManager`
rotation loops (Chat, ChatWithProgress, streaming) AND the bare
`Client.Chat`/`ChatWithProgress` paths (commit 755bfecc) — a
single-provider daemon must not escape the gate. `*ContextTooSmallError`
is NonRetryable: never retried, never parked, never a
`RecordAliasFailure` (a size verdict is not provider ill-health; the agent
loop surfaces it verbatim beside the tier-floor branch).

## R3 data posture (C4) — `posture.go` (+ type home in `shadow.go`)

Per-model `data_posture`: `private` (default when absent), `public_only`,
`non_sensitive`. Sensitive detection reuses the security package's secret
scanner through a narrow `SecretScanner` interface (direct
`internal/security` import is a real cycle: security -> preferences ->
tools -> llm); the daemon wires the scanner via `SetPostureScanner`. The
matched text is NEVER logged — count/rule-name only, and the routing row
carries no prompt material. On a sensitive turn the resolver RE-ROTATES
past non-cleared candidates before the winner finalizes (a plain chain
Skip would serve a non-cleared rotation head); when every candidate is
non-cleared the turn is refused — a sensitive prompt has no legal
destination.

## R4 shadow routing (C6) — `shadow.go`, `internal/rpc/shadow_route.go`

`ResolveShadow` mirrors the full `ResolveForAlias` ladder read-only:
`r.mu.RLock` only, sticky pins simulated never stored, cursor never
advanced. Hard zero-side-effect contract (pinned by
`TestShadow_NoSideEffects`): no `llm_calls` row, no metrics write, no
upstream HTTP, no budget consumption, no health or pin mutation. Exposed
as the `llm.route.shadow` RPC (frozen JSON view) and
`meept routing shadow --alias ... --prompt ...` (CLI). Cost estimator:
`(estIn/1e6)*in + (estOut/1e6)*out`; it never touches the tier gate.

## R5 pool-scoped failure (C5) — `pool_failure.go`, `errors_pool.go`

`ClassifyFailure(providerID, modelID, poolID, status, bodyExcerpt)` returns
the frozen `(FailureCategory, FailureScope, target)` triple, row-for-row
from the design's classify table: 400/413/422 -> request/request (blocks
NOTHING); 404 -> model; 401/402/403 -> account (sibling models on the same
provider get skipped); 429 with a pool-exhaustion body phrase on a
pool-declaring provider -> pool, else model; 5xx/unmapped -> transient.
The resolver's block map gained `(scope, target)` keys (NUL separator)
beside the untouched legacy `host|credential` keys. Model-scope ARMING is
deduped into the existing alias cooldown (a second model-scope key would
convert the throttle-only D10 key into blocks-everything — ruling 18).
Cooldown bases: account 1h, pool 15m, transient 30s, entry_missing 1h,
capped 4h. v1 pools are one-per-provider (`pool` field reserved; Q4).
The distinct `ErrAllPoolsBlocked` sentinel surfaces only pool blocks.

## R6 advisory budget (C7) — `budget.go` + `llm-advisory-budget.md`

`llm.budget.mode: "steer"` turns breaches into advisory: the turn
completes, one `budget_advisory`-class event rides the existing
`agent.quota_wait` topic (never a new topic), and the rotation failover
TAIL stably reorders by ascending cost so the next hop prefers the cheaper
healthy member (`steerReorderedTail`). `enforce` (the default) stays
byte-identical.

## R7 sticky difficulty (C8/C11) — `difficulty.go` + `resolver.go`

Pins store `{Index, Difficulty, CreatedAt}`. `DeriveDifficulty(mode,
requiresPlanning, inputLen)` projects the dispatcher Intent to 0-5 with the
frozen table — zero extra inference. A turn classified ABOVE the pinned
level releases the pin (reason `sticky_difficulty_increase`), which
re-pins from the rotation head; same-or-lower keeps the pin; difficulty 0
keeps R7 inert. Difficulty rides a resolver PARAMETER
(`ResolveForAliasDifficulty`) threaded from the loop's per-turn arm —
resolution happens before any chat call exists, so a ChatOption cannot
reach it (ruling 22). The async/task lane arms via the `turn_difficulty`
task-metadata key. The release check sits INSIDE the sticky stage:
frozen precedence (quota > endpoint > alias > sticky) is untouched.

## R8 tool-call validation (C9) — `stream_toolcheck.go`

`ValidateToolCalls(resp, offered)` returns the BARE `ErrToolCallInvalid`
sentinel (pointer identity, `ErrEmptyResponse` posture) when a call names
an unoffered function or its arguments do not parse as a JSON object.
Empty arguments (`""`) pass — providers send `""` for a no-argument call
(ruling 24). Wired post-parse, pre-executor, on all three clients
(openai-compatible, anthropic, codex incl. the SSE assembler); streamed
calls validate once on the ASSEMBLED calls, never per chunk; a tool-less
request (nil offered) skips byte-identically. It does NOT replace the
empty-completion classification, which keeps its precedence.

## R9 price groups (C10) — `pricegroup.go`

Aliases carry `price_group: "free" | "paid" | "premium"` (untagged =
legacy, resolves only when explicitly selected). `ResolveSpillway(ctx,
groups, fc)` selects the cheapest capable alias across the named groups —
candidates pass the posture/quality/admission filters, sorted by
(TotalCost, -quality) — returning (model, alias, error);
`ErrNoEligibleSpillwayGroup` when nothing qualifies. Free-first,
paid-on-exhaustion is a CALLER step (`groups: ["free"]` then
`["paid","premium"]`): the spillway never reorders alias rotation, never
touches a sticky pin, and never changes what an explicitly-selected alias
resolves to. `AliasesByPriceGroup` lists the sorted names per tier.

## Configuration surface (models.json5, per model inside providers.<id>.models.<name>)

| Key | Feature | Default (absent) |
|---|---|---|
| `quality: {chat,code,reasoning,tooling,long-context}` | R1 floor + order | zero = inert |
| `quality_source`, `quality_verified_at` | R1 provenance | unused |
| `data_posture` | R3 gate | `private` |
| `pool` | R5 pool identity | single pool per provider |
| `context_limit` (existing) | R2 admission | 0 = unknown = allow |
| `price_group` (per alias entry) | R9 tier | untagged = legacy |
| `llm.budget.mode` (meept.json5) | R6 | `enforce` |

## Test gate

Hermetic e2e suite `e2e/suites/sovereign-parity/` (manifest area
`sovereign-parity`): one live test per Test Gate matrix row plus the
flag-off byte-identical control against the checked-in golden
`e2e/fixtures/golden/sovereign-parity-flagoff.golden.json`.
Run: `make e2e-fast-area AREA=sovereign-parity`.

## Related docs

- `llm-shadow-routing.md` — the shadow RPC wire contract + CLI
- `llm-advisory-budget.md` — budget mode mechanics and wiring
- `routing-decisions.md` — the routing log the new reasons ride
- `docs/plans/20261115-sovereign-router-parity/` — master contracts C1-C11,
  leaf docs, and DRIFT-AUDIT.md rulings 1-29 (the binding adjudications)
