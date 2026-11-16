# 06 Pool-Scoped Failure Memory - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass; do NOT delete, skip, weaken,
> or rewrite any gate test (report suspected-wrong gate tests to the
> orchestrator instead). Do NOT commit - the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. After writing a file, do NOT
> read it back - write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Add a failure taxonomy with a SCOPE dimension (Contract C5): every upstream failure classifies into `account_dead | congested | transient | request_fault | entry_missing` at `model | pool | account | request` scope, and the resolver's block memory widens from a two-tier endpoint/alias cooldown to a `(scope, target)` map. A 401 blocks the whole ACCOUNT; a 400 blocks nothing.
- **Dependencies:** 00T gate tests committed (RED); 05-data-posture.md COMPLETE (this leaf serializes behind 05 because both edit `resolver.go`)
- **Estimated Context:** 65K
- **Concurrency Group:** A3 (wave 3 - serialized behind 05)
- **Source reference:** Sovereign Router `src/pools.py:60-206` (`Pool`, `classify_failure`, `SCOPES`) and `:228-410` (`DemotionTracker`). meept implements the classification + enforcement; the observe-only demotion tracker is NOT ported (see Non-goals).

## Goal

meept's failure memory is two-tier: an endpoint block keyed on `host|credential` (`Resolver.endpointBlocks`) and a per-alias timeout block (`AliasHealth`). That is close to Sovereign Router's *account* scope, and it is genuinely good. What it cannot express:

1. **Pool scope.** Two providers can share one upstream quota pool (OpenRouter's `:free` routes share one account pool; Cloudflare's routes share a 10,000-Neuron/day pool). To meept these are independent endpoints, so a pool-wide exhaustion rotates through every member instead of skipping the pool.
2. **Account scope on 401/402/403.** A dead credential is an account-shaped failure: every model on that account is dead. meept blocks one endpoint and keeps trying siblings.
3. **Request-fault classification.** A 400/413/422 is the *caller's* fault and must never count against a provider. meept has no explicit branch for this - `VerdictForFailure` (`resolver.go:1192`) unwraps `*APIError` into throttle/server-error classes, so a 400 can currently arm a cooldown it does not deserve.

The measured failure this fixes: on 2026-11-09 one dead account absorbed 369 attempts because nothing remembered the failure was account-shaped (Sovereign Router `docs/incidents/2026-11-09-rate-limit-cascade.md`).

### What meept keeps

The frozen precedence (quota blocks > endpoint cooldowns > alias blocks > stickiness) is UNCHANGED. Pool/account blocks are a NEW tier that slots in beside endpoint blocks, not above the whole chain.

## Context

- `internal/llm/resolver.go:1192-1230` - `VerdictForFailure(err) PolicyVerdict`: unwraps `*APIError` from the wrap chain, classifies throttle vs server-error. This is where the new classifier plugs in.
- `internal/llm/resolver.go:1230+` - `RecordAliasFailure(aliasName, err, failedModel)`: arms the endpoint cooldown for throttle and server-error classes. This is where scope-aware blocking arms.
- `internal/llm/resolver.go:1102-1127` - `hasEndpointBlocks`, `isEndpointBlocked`, `releaseQuotaBlockedPin`: the block-consult sites. `isEndpointBlocked` is the single reader every resolve path goes through.
- `internal/llm/resolver.go:970-1012` - `AliasHealth.inCooldown`, `failedModelMatches`, `releasePinsForModel`: the alias-level (model-scope) cooldown, unchanged.
- `internal/llm/errors_tier.go` / `internal/llm/errors_quota.go` - `ErrAllModelsQuotaBlocked` / `ErrAllEndpointsBlocked` sentinel pattern. A pool-wide block needs its own distinct sentinel, `ErrAllPoolsBlocked`, checked with `errors.Is`.
- `internal/llm/AGENTS.md` - "Endpoint-level cooldown identity (tree 02 leaf 04, D10)": `EndpointKey` = host + credential fingerprint; blocks live ON THE RESOLVER, never on `AliasHealth`; a timeout on `openai/model-1` must also skip `openai/model-2`. **This leaf generalizes that rule from "endpoint" to "scope" and must keep the existing behavior for existing keys.**

## Verified Anchors

- `internal/llm/resolver.go:1192` - `VerdictForFailure`
- `internal/llm/resolver.go:1230` - `RecordAliasFailure`
- `internal/llm/resolver.go:1102` - `hasEndpointBlocks`
- `internal/llm/resolver.go:1115` - `releaseQuotaBlockedPin`
- `internal/llm/resolver.go:970` - `AliasHealth.inCooldown`
- `internal/llm/AGENTS.md` - the endpoint-cooldown identity contract (D10)
- `/tmp/SovereignRouter/src/pools.py:60-100` - `Pool` + `pool_for`
- `/tmp/SovereignRouter/src/pools.py:176-206` - `classify_failure` (the frozen table)
- `/tmp/SovereignRouter/src/pools.py:228-410` - `DemotionTracker` (observe-only; NOT ported)
- `e2e/suites/sovereign-parity/poolfailure_test.go` - gate tests T12, T13, T14

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/llm/pool_failure.go
type FailureScope string
const (
    ScopeModel   FailureScope = "model"
    ScopePool    FailureScope = "pool"
    ScopeAccount FailureScope = "account"
    ScopeRequest FailureScope = "request"
)

type FailureClass string
const (
    ClassAccountDead  FailureClass = "account_dead"   // 401 / 402 / 403
    ClassCongested    FailureClass = "congested"      // 429
    ClassTransient    FailureClass = "transient"      // 5xx, timeout, transport
    ClassRequestFault FailureClass = "request_fault"  // 400 / 413 / 422
    ClassEntryMissing FailureClass = "entry_missing"  // 404
)

// ClassifyFailure is the frozen table from pools.py:176-206.
// bodyExcerpt is a BOUNDED excerpt of the response body used only to
// distinguish a pool-wide 429 from a model-scoped 429. It is pattern-
// matched for signal phrases and then DISCARDED - never stored, never
// logged (pools.py docstring: "keeps only the resulting class/scope").
func ClassifyFailure(providerID, modelID, poolID string, status int, bodyExcerpt string) (FailureClass, FailureScope, string)

// PoolIDFor returns the pool a provider's model draws from. v1: one pool
// per provider, named "provider:<id>" (Q4). The field is reserved so a
// future config can map two providers onto one pool without a migration.
func PoolIDFor(providerID string) string

// PoolExhaustionSignal reports whether a 429 body implicates the whole
// pool rather than one model (Cloudflare 4006; OpenRouter per-day free
// phrasing - the two observed shapes in pools.py:161-174).
func PoolExhaustionSignal(providerID, bodyExcerpt string) bool
```

Block memory widening:

```
// Resolver.endpointBlocks gains a scope dimension. The key becomes
// (scope, target) instead of host|credential alone.
type blockKey struct {
    Scope  FailureScope
    Target string   // modelID | poolID | providerID
}
// Existing host|credential keys are keyed ScopeAccount + providerID and
// keep working unchanged (D10 behavior preserved).
```

New sentinel:

```
var ErrAllPoolsBlocked = errors.New("llm: every candidate pool is blocked")
// Distinct from ErrAllModelsQuotaBlocked and ErrAllEndpointsBlocked.
// Checked with errors.Is, never string matching.
```

### Frozen classification table (verbatim from `pools.py:176-206`)

```
transport failure / status 0   -> (ClassTransient,    ScopeModel,   modelID)
400 / 413 / 422                -> (ClassRequestFault, ScopeRequest, "-")
404                            -> (ClassEntryMissing, ScopeModel,   modelID)
401 / 403                      -> (ClassAccountDead,  ScopeAccount, providerID)
402                            -> (ClassAccountDead,  ScopeAccount, providerID)
429 + pool-exhaustion phrase   -> (ClassCongested,    ScopePool,    poolID)
429 otherwise                  -> (ClassCongested,    ScopeModel,   modelID)
5xx and unmapped               -> (ClassTransient,    ScopeModel,   modelID)
```

Cooldown bases (frozen from `pools.py:222`, adapted to meept's existing defaults):

```
account  -> 3600s (1h)   - meept's alias `timeout` default is 30s; account
                           death is a longer-lived fact, so it uses 1h
pool     -> 900s  (15m)
model    -> 60s
transient-> 30s
entry_missing -> 3600s
cap      -> 4h
```

### Non-goals (explicit)

- **Do NOT port `DemotionTracker`.** Sovereign Router's demotion is observe-only (v0.8.0: "Nothing in selection reads this"). meept blocks directly. Porting an observe-only layer that nothing reads would be dead code, which the repo's deadcode gate blocks.
- **Do NOT add a `pool` config surface.** One pool per provider in v1 (Q4). The field is reserved.
- **Do NOT change the frozen precedence.** Quota blocks > endpoint cooldowns > alias blocks > stickiness, unchanged.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add `FailureScope`, `FailureClass`, the constants, `PoolIDFor`, `PoolExhaustionSignal` | Create: `internal/llm/pool_failure.go` | `go test -p 2 ./internal/llm/ -run TestPoolIDFor` | RED (undefined) | GREEN | Given provider "openrouter"; When asked; Then `openrouter:openrouter` (v1 shape) |
| T2 | Implement `ClassifyFailure` as the frozen table, including the 402 in-flight special case and the 429 pool-phrase branch | Modify: `internal/llm/pool_failure.go` | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run TestPoolFailure_ClassifyTable'` | RED | GREEN | Given each row of the frozen table; When classified; Then the row matches |
| T3 | Body-excerpt discipline: the excerpt is pattern-matched and discarded; assert no function signature retains it beyond `ClassifyFailure` | Modify: `internal/llm/pool_failure.go` | `go test -p 2 ./internal/llm/ -run TestPoolFailure_BodyExcerptNotRetained` | RED | GREEN | Given a body excerpt with a signal phrase; When classified; Then only class/scope/target escape the function |
| T4 | Widen `Resolver.endpointBlocks` to `(scope, target)` keys; migrate existing `host\|credential` keys to `ScopeAccount` | Modify: `internal/llm/resolver.go` (block map + `isEndpointBlocked`) | `go test -p 2 ./internal/llm/ -run 'TestEndpointBlock'` | RED | GREEN | Given an existing endpoint block; When read; Then it still blocks (D10 preserved) |
| T5 | `RecordAliasFailure` consults `ClassifyFailure`: request-fault arms NOTHING; account-dead arms the account; pool-exhaustion arms the pool; 5xx/429-model arm the model | Modify: `internal/llm/resolver.go:1230` | `go test -p 2 ./internal/llm/ -run 'TestRecordAliasFailure_Scope'` | RED | GREEN | Given a 400 from model A; When recorded; Then A remains eligible |
| T6 | Add `ErrAllPoolsBlocked` and return it when every candidate's pool is blocked (distinct from quota/endpoint sentinels) | Create: `internal/llm/errors_pool.go`; Modify: `internal/llm/resolver.go` | `go test -p 2 ./internal/llm/ -run TestAllPoolsBlocked` | RED | GREEN | Given all pools blocked; When resolved; Then `errors.Is(err, ErrAllPoolsBlocked)` |
| T7 | Account-scope enforcement: a 401 on model A skips sibling model B on the same provider | Modify: `internal/llm/resolver.go` (`isEndpointBlocked` consult) | `-run 'TestPoolFailure_'` (T12) | RED | GREEN | Given provider p with A and B; When A returns 401; Then B is skipped |
| T8 | Request-fault enforcement: a 400 from A leaves A eligible on the next resolve | same | `-run TestPoolFailure_RequestFaultBlocksNothing'` (T13) | RED | GREEN | Given a 400; When re-resolved; Then A is eligible |
| T9 | Precedence preservation: quota > endpoint/account > alias > sticky, proven with all four armed at once | - | `go test -p 2 ./internal/llm/ -run TestPrecedence` | n/a | GREEN, matches `main` | Given all four armed; When resolved; Then the winner is the pre-change winner |
| T10 | Cooldown bases table implemented with the frozen values and a comment citing `pools.py` | Modify: `internal/llm/pool_failure.go` | `go test -p 2 ./internal/llm/ -run TestCooldownBases` | RED | GREEN | Given each class; When armed; Then the base matches the table |
| T11 | Cap: no block exceeds 4h | same | `-run TestCooldownCap'` | RED | GREEN | Given a long expiry; When armed; Then it clamps to 4h |

## Self-Verification Checklist

- [ ] T1-T11 complete; gate tests T12, T13, T14 GREEN.
- [ ] The frozen classification table matches `pools.py:176-206` row for row.
- [ ] Request-fault (400/413/422) arms NOTHING - no endpoint, no pool, no account, no alias.
- [ ] Existing `host|credential` endpoint blocks still block (D10 e2e/unit tests unmodified and green).
- [ ] `ErrAllPoolsBlocked` is distinct and checked with `errors.Is`.
- [ ] `DemotionTracker` was NOT ported; no dead code added.
- [ ] The body excerpt never reaches a struct field, a log, or a DB row.
- [ ] Frozen precedence unchanged and pinned by T9.
- [ ] `go build ./...` and `go test -p 2 ./internal/llm/` green.
- [ ] No `_ = f()`, no bare `panic`, no ignored error.

## Review Checklist

- [ ] Classification table is verbatim; no paraphrase drift.
- [ ] Account block (1h) vs pool block (15m) vs model block (60s) vs transient (30s) are implemented with the frozen bases.
- [ ] The 402 in-flight special case exists (OpenRouter concurrency accounting must not be read as account death).
- [ ] The 429 pool-phrase detection covers both observed shapes (Cloudflare 4006, OpenRouter per-day free).
- [ ] No new config surface for pools (Q4).
- [ ] No observe-only layer added.
- [ ] `internal/llm/AGENTS.md` updated: the endpoint-cooldown contract now names scopes.

## Do NOT commit

The orchestrator commits this leaf after review.