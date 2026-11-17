# Sovereign Router Capability Parity - Implementation Orchestrator (Root)

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 1 gate leaf + 9 implementation leaves under this node
- **Scope:** Add nine additive routing capabilities to meept's model configuration and LLM resolver, ported from the Sovereign Router design (github.com/jmason84/SovereignRouter, reviewed 2026-11-15 at `/tmp/SovereignRouter`, spec v0.9.0). No capability is removed; meept's existing alias rotation, cooldown, quota-block, and sticky semantics are preserved byte-identically when the new features are off.
- **Source of requirements:** the comparison report delivered 2026-11-15 (P1-P8 proposals) plus the follow-up analysis (price groups R9, sticky-difficulty source Q7), restated as R1-R9 below.

## Goal

Sovereign Router routes on **cost, quality, data posture, and pool capacity**. meept routes on **health, declared alias order, and cooldowns**. This tree adds the four missing dimensions without disturbing the existing ones.

Nine capabilities, each traceable to a requirement:

| Req | Capability | Sovereign Router source | Leaf |
|-----|-----------|------------------------|------|
| R1 | Per-model quality dimension + quality floor in selection | `src/gateway.py:183-200,700-703,746-759`; `src/registry.json` `quality:{chat,code,infra,long-context,reasoning,tooling}` | 01 |
| R2 | Admission control: pre-dispatch context-fit rejection | `src/gateway.py:706` (`context_too_small`), `quota.py:370` `check_provider_limits` | 02 |
| R3 | Per-model data-posture label + outbound sensitivity scan | `src/sensitivity.py`; registry `lane` + `spillway_sensitive_excluded` | 05 |
| R4 | Shadow routing: full decision, no upstream call, no ledger row | `src/gateway.py:1121-1180` `POST /v1/shadow` | 04 |
| R5 | Pool-scoped failure memory above account scope | `src/pools.py:60-206` `Pool`, `classify_failure`, `SCOPES` | 06 |
| R6 | Advisory budget mode (steer + alert, never refuse) | `src/governor.py:87-207`; owner ruling 2026-10-31 "no spending caps, ever" | 08 |
| R7 | Sticky-pin invalidation on classified difficulty increase | `src/sticky.py:1-60` | 07 |
| R8 | Streamed tool-call validation before execution | `src/validation.py:78-126` `validate_response`, `validate_streamed_text_outcome` | 03 |
| R9 | Alias-level price group + paid spillway (free first, paid on exhaustion) | `src/gateway.py:765-800` `build_spillway_candidates`; registry `lane` `paid`/`premium` | 09 |

### Non-goals (explicit)

- **No removal of meept's budget caps.** R6 adds an advisory mode as an opt-in alternative; `enforce` stays the default and stays byte-identical.
- **No change to the frozen selection precedence.** Quota blocks > endpoint cooldowns > alias blocks > stickiness (`internal/llm/resolver.go:660-661`, contract in `internal/llm/AGENTS.md`). Every new filter slots in without reordering it.
- **No new bus topics and no new WS event types.** New signals ride response metadata, following the `llm_tier*` precedent (`internal/llm/tier_policy.go:42-47`).
- **No `internal/llm` -> `internal/config` import.** New config is mirrored 1:1 as a plain struct inside `internal/llm`, the `TierPolicy` / `FailurePolicyConfig` pattern.
- **No cloud-provider calls in tests.** The repo e2e tier is hermetic (AGENTS.md e2e policy).
- **R9 (price groups) is explicitly NOT the Sovereign Router multi-tenant surface.** R9 adds a *price-tier label on aliases* (`free`/`paid`/`premium`) and a spillway that selects across tiers. It does NOT add per-identity clients, per-client key auth, per-client lanes, per-client daily targets, or a premium gate. meept's identity unit is the *session*, not the *consumer* (`internal/auth/quota.go:10-23` is an undecided no-op); inventing a tenant concept to host those features is a product decision this tree does not make. The portable mechanism (tier ordering + steering) is what R6 and R9 cover.

### Difficulty sourcing (Q7 lock-in)

R7's sticky-difficulty invalidation needs a per-turn difficulty signal *inside the agent loop*, where `ResolveForAlias` is called (`internal/agent/loop.go:5309`, `:6943`). The dispatcher's `Intent` is computed at dispatch, once — a different clock from the per-turn model resolution. **The difficulty derives from existing signals, never a second classifier call:** `Intent.SuggestedMode` (`direct`/`spec_plan`/`quick_plan`/`spec_pair`, `dispatcher.go:2013-2044`) + `Intent.RequiresPlanning` + input length, projected to a 1-5 level by the frozen table in leaf 07 (Contract C11). This costs zero extra inference and reuses the routing decision meept already computes but discards. If no difficulty is derivable, the value is 0 and R7 is inert.

## Architecture

### Where each capability attaches

```
                        chat / task turn
                              |
                     agent.Classifier (agent/dispatcher.go Intent)
                              |
        +---------------------+--------------------+
        |                     |                    |
   [R7] sticky pin       [R1] quality floor   [R2] admission gate
   resolver.go           selection.go        admission.go
   resolveStickyCaller   ResolveForSkill /   Check()
                         FindCheapest
        |                     |                    |
        +----------> Resolver.ResolveForAlias <------+
                              |
                    [R3] posture filter
                    posture.go Filter()
                              |
                    [R5] pool-scoped failure memory
                    pool_failure.go Classify()
                              |
                    [R6] advisory budget (steer, not block)
                    budget.go AdvisoryVerdict()
                              |
                    [R9] paid spillway (free first, paid on exhaustion)
                    pricegroup.go ResolveSpillway()
                              |
                    ProviderManager.chatWithFailoverRaw
                              |
                    [R8] stream tool-call validation
                    stream_toolcheck.go Validate()
                              |
                        LLM client -> upstream
```

`[R9]` is a caller-selected tier step (free → paid → premium), not part of single-alias rotation; `ResolveSpillway` is opt-in at the call site and never reorders `ResolveForAlias`.

`[R4]` shadow routing is a **read-only parallel entry point**: it calls the same `ResolveForAlias` + a new cost estimator and returns the decision. It never enters `chatWithFailoverRaw`.

### The selection chain (Contract C1)

All new pre-dispatch filters implement ONE interface in a new file, `internal/llm/selection.go`. This is the single integration surface; leaves that add a filter add ONE method on their own type and ONE registration line in `NewResolver`.

```go
// internal/llm/selection.go
type Candidate struct {
    Model      *ModelConfig
    Alias      string
    Capability string
    EstInput   int
    EstOutput  int
}

type FilterCtx struct {
    SessionID  string
    Capability string
    EstInput   int
    EstOutput  int
    Sensitive  bool
    Difficulty int
    Now        time.Time
}

type Verdict int
const (
    VerdictAllow Verdict = iota
    VerdictSkip            // candidate skipped; reason recorded
    VerdictFloor           // terminal: no candidate may serve this turn
)

type Filter interface {
    Name() string
    Filter(c *Candidate, fc *FilterCtx) Verdict
    Reason() string
}
```

Registration order in `NewResolver` is FIXED and is part of the contract:

1. `postureFilter` (R3) - a sensitive turn never reaches a non-cleared model
2. `qualityFilter` (R1) - quality floor for the classified difficulty
3. `admissionFilter` (R2) - context fit

There is no position 4. Advisory budget (R6) is NOT a selection-chain
filter: leaf 08 binds it at the provider-manager tier gate (budget.go +
provider_manager.go). Drift-audit ruling #4, 2026-11-16 - adding a
budgetFilter would add a resolver.go writer and break the file-write
serialization.

Order matters: posture is a hard compliance boundary and must be evaluated even when the quality filter would skip, so the skip reason in the routing log names the *binding* constraint. A leaf that adds a filter appends it to this list and updates this contract in the same commit.

### Preserved meept semantics (must not regress)

- Alias rotation position and `CurrentIndex` advancement (`resolver.go:552-580`).
- `DefaultModel` reversion after cooldown (`resolver.go:555-566`).
- Endpoint cooldowns keyed on host + credential fingerprint (`Resolver.endpointBlocks`).
- Quota blocks that lazily clear after expiry plus one success (`internal/llm/AGENTS.md`).
- Sticky pins with quota-block and endpoint-block release (`resolver.go:1014-1101`).
- `ErrAllModelsQuotaBlocked` / `ErrAllEndpointsBlocked` / `ErrTierFloor` sentinels, checked with `errors.Is`.
- Empty-completion classification on every client (`ErrEmptyResponse`, per-client sites in `internal/llm/AGENTS.md`).

## Interface Contracts

### C1: The selection chain (`internal/llm/selection.go`)

Owner: 01-quality-dimension.md creates the file. Every later filter leaf appends one method and one registration line.

```
type Filter interface {
    Name() string
    Filter(c *Candidate, fc *FilterCtx) Verdict
    Reason() string
}

func (r *Resolver) runSelectionFilters(c *Candidate, fc *FilterCtx) (Verdict, string)
// Iterates r.selectionFilters in registration order. Returns the FIRST
// non-Allow verdict with that filter's Reason(). All-Allow returns
// (VerdictAllow, "").
// Callers: ResolveForAlias (after alias resolution, before return),
//          ResolveForSkill (inside the candidate sort comparator).
```

### C2: Quality shape (`internal/llm/quality.go`)

Owner: 01-quality-dimension.md.

```
type QualityClass string
const (
    QualityChat       QualityClass = "chat"
    QualityCode       QualityClass = "code"
    QualityReasoning  QualityClass = "reasoning"
    QualityTooling    QualityClass = "tooling"
    QualityLongContext QualityClass = "long-context"
)

type Quality struct {
    Chat, Code, Reasoning, Tooling, LongContext int
}

// QualityFor returns the score for class. Unknown class -> 0.
func (q Quality) QualityFor(c QualityClass) int

// QualityFloorFor maps a classified difficulty (1..5) to the minimum
// acceptable score for a capability. Difficulty 1-2 -> floor 1;
// 3 -> 2; 4 -> 3; 5 -> 4. A difficulty of 0 (unclassified) -> floor 0
// (feature inert). This table is the frozen contract.
func QualityFloorFor(difficulty int, capability string) int
```

Config keys (`models.json5`, per-model inside `providers.<id>.models.<name>`):

```
"quality": { "chat": 3, "code": 4, "reasoning": 3, "tooling": 5, "long-context": 2 },
"quality_source": "tooling-battery:2026-11-15",   // provenance, free text
"quality_verified_at": "2026-11-15"                // RFC3339 date, advisory
```

Absent `quality` block -> zero Quality -> floor never binds (feature inert). This is the opt-in default: no model in the shipped `config/models.json5` declares quality until leaf 01 seeds it.

### C3: Admission verdict (`internal/llm/admission.go`)

Owner: 02-admission-context.md.

```
var ErrContextTooSmall = errors.New("llm: request does not fit the model context")
// ErrProviderLimit was dropped (drift-audit ruling #5, 2026-11-16):
// rate-window handling stays with the existing cooldown machinery; no
// leaf owns this sentinel and none may declare it.

type ContextTooSmallError struct {
    ModelID, ProviderID string
    EstInput, Reserve, ContextLimit int
}
func (e *ContextTooSmallError) Error() string
func (e *ContextTooSmallError) NonRetryable() bool { return true }  // never retried, never parked
```

Gate rule (frozen): `EstInput + Reserve > ContextLimit` -> skip, where `Reserve = min(ModelConfig.MaxTokens, 64)` mirroring Sovereign Router's `MIN_OUTPUT_TOKENS = 64` (`src/gateway.py:706`). `ContextLimit <= 0` means unknown -> allow (never fabricate a limit).

### C4: Data posture (`internal/llm/posture.go`)

Owner: 05-data-posture.md.

```
type Posture string
const (
    PosturePrivate    Posture = "private"      // no-training, cleared
    PosturePublicOnly Posture = "public_only"  // provider may train -> sensitive barred
    PostureNonSensitive Posture = "non_sensitive" // aggregator, posture unclear
)
// Default when the key is absent: PosturePrivate (byte-identical to today).
```

Sensitive detection reuses `internal/security/secret_scanner.go` `Scan(text) []string` (already written, currently wired only to `internal/tools/builtin/filesystem.go:69` and `shell.go:237`). The posture filter calls it on the outbound prompt; a non-empty match sets `FilterCtx.Sensitive = true`. The match itself is NEVER logged (only the count), mirroring Sovereign Router's "the match itself is never logged".

### C5: Pool-scoped failure (`internal/llm/pool_failure.go`)

Owner: 06-pool-scoped-failure.md.

```
type FailureScope string
const (
    ScopeModel    FailureScope = "model"
    ScopePool     FailureScope = "pool"
    ScopeAccount  FailureScope = "account"
    ScopeRequest  FailureScope = "request"
)

type FailureClass string
const (
    ClassAccountDead   FailureClass = "account_dead"   // 401/402/403
    ClassCongested     FailureClass = "congested"    // 429
    ClassTransient     FailureClass = "transient"     // 5xx, timeout, transport
    ClassRequestFault  FailureClass = "request_fault" // 400/413/422
    ClassEntryMissing  FailureClass = "entry_missing" // 404
)

func ClassifyFailure(providerID, modelID, poolID string, status int, bodyExcerpt string) (FailureClass, FailureScope, string)
// FROZEN 5-arg shape WITH poolID in the THIRD position (user +
// back-lane ruling 2026-11-16, superseding DRIFT-AUDIT.md ruling #2;
// order matches the 00T gate seam and leaf 06: providerID, modelID,
// poolID, status, body). Gate tests T12/T14 pin this arity.
```

Scope resolution: `ScopeRequest` for 400/413/422 (never blocks anything); `ScopeModel` for 404; `ScopeAccount` for 401/402/403; `ScopePool` for a 429 whose body matches a pool-exhaustion phrase and whose provider declares a pool; else `ScopeModel` for 429; `ScopeModel` for 5xx and unmapped.

The block map gains ONE dimension: `endpointBlocks` keys widen from `host|credential` to `(scope, target)` where `target` is `providerID` for account scope, `poolID` for pool scope, `modelID` for model scope. Existing `host|credential` keys keep working unchanged.

### C6: Shadow route (`internal/llm/shadow.go`)

Owner: 04-shadow-routing-rpc.md.

```
type ShadowDecision struct {
    ModelID, ProviderID, Alias string
    Reason       string
    EstInput     int
    EstOutput    int
    EstCostUSD   float64
    Quality      int
    Posture      Posture
    Candidates   []ShadowCandidate  // every candidate with its skip reason
}
type ShadowCandidate struct {
    ModelID, ProviderID string
    Selected            bool
    SkipReason          string
    Quality             int        // union ruling (drift audit #3): leaf 04's richer candidate shape
    EstCostUSD          float64
}
func (r *Resolver) ResolveShadow(ctx context.Context, req ShadowRequest) (*ShadowDecision, error)
```

Hard rules: no `llm_calls` row, no `metrics.db` write, no upstream HTTP, no budget consumption, no alias-health mutation, no sticky-pin mutation. The shadow path takes `r.mu.RLock` only. Any state mutation attempt is a test failure.

### C7: Advisory budget (`internal/llm/budget.go`)

Owner: 08-advisory-budget.md.

```
type BudgetMode string
const (
    BudgetModeEnforce BudgetMode = "enforce" // today's behavior, DEFAULT
    BudgetModeSteer   BudgetMode = "steer"   // warn + prefer cheaper, never refuse
)

type AdvisoryVerdict struct {
    OverTarget bool
    Limit      BudgetLimit
    Used, Cap  float64
}
func (b *Budget) Advisory() AdvisoryVerdict
```

In `steer` mode `BudgetExceededError` is NEVER returned. Instead `Advisory()` reports the breach, the tier gate prefers cheaper aliases, and one `agent.quota_wait`-class event carries the warning (class `budget_advisory`, reusing the existing park-event topic per `internal/llm/AGENTS.md` "no new topic prefix").

### C8: Sticky difficulty (`internal/llm/resolver.go`)

Owner: 07-sticky-difficulty.md.

`StickyPins` is `map[string]int` today (models.go:440). Leaf 07 converts
pin storage to a struct with a `Difficulty` field (the per-pin difficulty
storage described in leaf 07 is the accurate mechanism). `resolveStickyCaller` releases the pin when `fc.Difficulty > pin.Difficulty` and re-pins from the rotation head. A released pin logs `sticky_difficulty_increase` as the routing-log reason (existing `RoutingDecision.Reason` field, `internal/llm/routing_log.go:98` - no schema change).

### C9: Streamed tool-call validation (`internal/llm/stream_toolcheck.go`)

Owner: 03-stream-toolcall-validation.md.

```
var ErrToolCallInvalid = errors.New("llm: tool call failed validation")
// sentinel must be the BARE value (pointer identity), matching the
// ErrEmptyResponse contract in internal/llm/AGENTS.md.

func ValidateToolCalls(resp *Response, offered []string) error
// Returns ErrToolCallInvalid when a tool call names a function absent from
// `offered`, or when its arguments do not parse as a JSON object.
```

This does NOT replace `ErrEmptyResponse`: empty-completion classification stays exactly where it is (every client, before the empty-retry). Tool-call validation runs after a successful parse and before the executor sees the calls.

### C10: Alias price group + paid spillway (`internal/llm/pricegroup.go`)

Owner: 09-price-groups.md. The `ModelAliasEntry.PriceGroup` field is declared in leaf 01 (same `providers.go` edit as `Quality`); leaf 09 implements the resolution.

```
// internal/llm/providers.go (leaf 01 declares the field, leaves it inert)
type ModelAliasEntry struct {
    ...
    // PriceGroup tags this alias as a routing tier. "" | "free" | "paid" |
    // "premium". "" = untagged = legacy behavior (this alias resolves only
    // when explicitly selected). Free/paid/premium become selectable as a
    // TIER by the spillway.
    PriceGroup string `json:"price_group,omitempty"`
}

// internal/llm/pricegroup.go (leaf 09)
type PriceGroup string
const ( PriceGroupFree PriceGroup = "free"; PriceGroupPaid PriceGroup = "paid"; PriceGroupPremium PriceGroup = "premium" )

// ResolveSpillway selects the cheapest capable alias across the given
// groups in cost order, mirroring gateway.py build_spillway_candidates:
//   - candidates = aliases whose PriceGroup is in groups, whose members all
//     pass the posture + quality + admission filters for this request
//   - sorted by (TotalCost, -quality)
//   - a call with groups=["free"] then, on exhaustion/failure,
//     ["paid","premium"] implements "free first, paid on exhaustion"
func (r *Resolver) ResolveSpillway(ctx context.Context, groups []string, fc *FilterCtx) (*ModelConfig, string, error)
// Returns the chosen config, the alias name that produced it, and an
// error. 3-return shape is frozen (drift-audit ruling #1; leaf 09 and
// the 00T contract list already held it).
func (r *Resolver) AliasesByPriceGroup(g PriceGroup) []string
```

Freeze: spillway selection NEVER reorders alias rotation, never touches a sticky pin, and does not change what an explicitly-selected alias resolves to. A `free`-tagged and `paid`-tagged alias both operating for the same caller, with `free` healthy, always yields `free` first — that is the whole point, and it is opt-in (only a caller that names it as a `groups` step gets it; default callers are unchanged).

### C11: Difficulty derivation (`internal/llm/difficulty.go`)

Owner: 07-sticky-difficulty.md. The per-turn difficulty source, locked in by Q7.

```
// DeriveDifficulty projects an Intent into a difficulty level, 0-5.
// It is pure/side-effect-free: no classifier call, no I/O, reads only the
// values the dispatcher already computed. A caller with no Intent or no
// derivable signal returns 0 (R7 inert). This table is the frozen contract.
func DeriveDifficulty(mode string, requiresPlanning bool, inputLen int) int
```

## Test Gate

Repo policy (AGENTS.md e2e policy): **new feature tests go in the hermetic e2e tier, not new unit test files.** The gate leaf writes `e2e/suites/sovereign-parity/` with the `e2e` build tag plus an `e2e/manifest.json` entry, per `docs/workflows/e2e-testing.md`.

| Req | e2e scenario | Suite | Status |
|-----|-------------|-------|--------|
| R1 | cheap model with low `quality.code` loses to a pricier high-quality model on a code turn; quality-absent models keep legacy cost order | sovereign-parity | PENDING |
| R1 | quality floor: difficulty 5 with only score-2 candidates surfaces `ErrQualityFloor`, not a completed turn | sovereign-parity | PENDING |
| R2 | request whose estimated input exceeds the model context is rejected pre-dispatch with `ErrContextTooSmall`; no upstream request is made | sovereign-parity | PENDING |
| R2 | `ContextLimit == 0` (unknown) allows the request through | sovereign-parity | PENDING |
| R3 | a prompt containing an AWS-shaped key never routes to a `public_only` or `non_sensitive` model | sovereign-parity | PENDING |
| R3 | posture absent = `private`; sensitive routing is byte-identical to pre-change | sovereign-parity | PENDING |
| R4 | shadow decision returns the same model the live path would pick, and writes no `llm_calls` row | sovereign-parity | PENDING |
| R4 | shadow does not consume budget, does not mutate alias health, does not create a sticky pin | sovereign-parity | PENDING |
| R5 | a 401 on one model blocks the ACCOUNT (sibling model on the same provider is skipped) | sovereign-parity | PENDING |
| R5 | a 400 (request fault) blocks nothing | sovereign-parity | PENDING |
| R6 | `mode: steer` with the daily cost cap exceeded completes the turn and emits `budget_advisory` | sovereign-parity | PENDING |
| R6 | `mode: enforce` (default) is byte-identical to today: `BudgetExceededError`, turn refused | sovereign-parity | PENDING |
| R7 | a new user message classified above the pinned level releases the pin and re-pins at the rotation head | sovereign-parity | PENDING |
| R7 | difficulty derives from existing Intent signals (no second classifier call) and difficulty 0 keeps R7 inert | sovereign-parity | PENDING |
| R8 | a tool call naming an unoffered function is rejected with `ErrToolCallInvalid` and never reaches the executor | sovereign-parity | PENDING |
| R8 | streamed tool-call arguments split across chunks are validated only after the stream completes | sovereign-parity | PENDING |
| R9 | a free-tagged and paid-tagged alias both eligible: the free one serves while healthy, the paid spillway serves only on free exhaustion/failure | sovereign-parity | PENDING |
| R9 | untagged aliases (no `price_group`) keep legacy resolution exactly; spillway is opt-in per-caller | sovereign-parity | PENDING |
| ALL | flag-off control: with all nine features unconfigured, the routing decision stream is byte-identical to `main` | sovereign-parity | PENDING |

Gate leaf: `00T-gate-tests.md`. Implementation leaves dispatch only after the gate reaches TESTS_VALIDATED.

## Child Index

| # | Document | Requirement | Group | Depends on | Est. context | Writes |
|---|----------|-------------|-------|------------|--------------|--------|
| 00T | 00T-gate-tests.md | all (tests) | Phase 0 | none | 75K | `e2e/suites/sovereign-parity/*`, `e2e/manifest.json` |
| 01 | 01-quality-dimension.md | R1 | A | 00T | 70K | `internal/llm/selection.go`, `quality.go`, `providers.go`, `models.go`, `resolver.go`, `config/models.json5` |
| 02 | 02-admission-context.md | R2 | B | 00T | 55K | `internal/llm/admission.go`, `provider_manager.go` |
| 03 | 03-stream-toolcall-validation.md | R8 | C | 00T | 50K | `internal/llm/stream_toolcheck.go`, `client.go` |
| 04 | 04-shadow-routing-rpc.md | R4 | D | 00T | 60K | `internal/llm/shadow.go`, `internal/rpc/shadow_route.go`, `internal/daemon/shadow_rpc_wiring.go` |
| 05 | 05-data-posture.md | R3 | A2 | 01 | 60K | `internal/llm/posture.go`, `providers.go`, `resolver.go` |
| 06 | 06-pool-scoped-failure.md | R5 | A3 | 05 | 65K | `internal/llm/pool_failure.go`, `resolver.go` |
| 07 | 07-sticky-difficulty.md | R7 | A4 | 06 | 55K | `resolver.go`, `internal/llm/difficulty.go` |
| 08 | 08-advisory-budget.md | R6 | B2 | 02 | 50K | `internal/llm/budget.go`, `provider_manager.go` |
| 09 | 09-price-groups.md | R9 | A5 | 07, 01 | 45K | `internal/llm/pricegroup.go` (reads `resolver.go`, `providers.go`) |

### File-write serialization (verified by intersecting declared write-targets)

`internal/llm/resolver.go` is written by 01, 05, 06, 07 - therefore those four leaves are **serialized in that order**, not parallel. `internal/llm/providers.go` is written by 01 and 05 - same chain. `internal/llm/provider_manager.go` is written by 02 and 08 - 08 follows 02. `internal/llm/pricegroup.go` (new file) is written ONLY by 09; 09 also reads `resolver.go` but does not edit it, so it depends on the A-chain being complete (09 needs 01's `PriceGroup` field and the filters) without adding a writer.

Parallel lanes after the gate:

```
Wave 1 (4 concurrent, file-disjoint):
  A: 01   B: 02   C: 03   D: 04
Wave 2 (2 concurrent):
  A2: 05 (needs 01)      B2: 08 (needs 02)
Wave 3:  A3: 06 (needs 05)
Wave 4:  A4: 07 (needs 06)
Wave 5:  A5: 09 (needs 07, 01)   -- reads resolver.go, writes pricegroup.go only
```

## Dispatch Protocol

### Phase 0 - Test Gate

1. Dispatch `00T-gate-tests.md` as a single implementation agent.
2. The control agent (orchestrator, main model, IN-SESSION - never delegated) validates the returned tests against the Test Gate matrix above. On any delta, the lesser artifact updates to match the greater (coverage converges upward; deleting a scenario needs written rationale in the leaf).
3. Commit the gate, mark TESTS_VALIDATED. Implementation leaves do not dispatch before this.

### Wave 1

Dispatch 01, 02, 03, 04 together (4 concurrent, file-disjoint - verified above). Each brief pins: the leaf path, Contracts C1-C11 that apply, "Do NOT commit", the corruption rule (never `read_file` a file then `write_file` it back; explore with `search_files`/`cat`), and the exact verify command.

### Wave 2-4

Dispatch per the dependency column. Serialize the A-chain. Never dispatch two leaves whose write-targets intersect.

### Per leaf, after implementation returns

1. Read the changed files in-session.
2. Verify the declared interface contract matches the leaf's actual signature.
3. Run the verify command from the leaf's task table.
4. On gaps: re-dispatch with specific feedback, max 3 iterations, then report to the user.
5. Commit only the leaf's declared files: `git add <exact paths>` then `git commit -m "feat(llm): <leaf scope>"`.
6. Update the Completion Tracking Table.

### Integration

After every leaf is REVIEWED: run `make build`, `make test`, `make lint-ci`, `make graphs-check`, and the new suite (`make e2e-fast-area AREA=sovereign-parity`). Update `AGENTS.md` (package table + invariants) and `docs/workflows/` in the same commit as the last leaf, per the root maintenance rule.

## Coding Conventions

- **Config format is JSON5.** New `models.json5` keys are quoted, lower_snake_case, with an inline comment naming the reason - match the existing voice in `config/models.json5`.
- **Every new feature is opt-in with default false / zero.** Follow `docs/agents/opt-in-defaults.md`. A feature that changes behavior when unconfigured is a defect.
- **No `internal/llm` -> `internal/config` import.** Mirror config structs inside `internal/llm` (the `TierPolicy` / `FailurePolicyConfig` pattern).
- **Typed errors with sentinels, checked with `errors.Is`.** Terminal errors implement `NonRetryable`. New sentinels join the family in `internal/llm/errors_*.go`.
- **Pointer-identity for sentinels returned by pointer.** `errors.Is` also matches through a wrap, so exhaustion paths return the bare sentinel by pointer (contract in `internal/llm/AGENTS.md`).
- **No ignored errors and no bare `panic(err)`.** Log-and-continue with a `Warn` naming the operation; never `_ = f()`.
- **Never hold a mutex across I/O.** Snapshot under lock, act outside.
- **Two-value map assertions** everywhere.
- **No new bus topics.** Signal through response metadata keys, following `llm_tier*`.
- **UI text is lowercase** if any surface renders a new field.
- **Comments explain WHY, citing the incident or contract** - the repo's comment style names the failing case, not the mechanism.
- **Test placement:** e2e tier only for new features. Existing unit tests may be updated for bug fixes only.

## Completion Tracking Table

| Leaf | Requirement | Status | Commit | Notes |
|------|-------------|--------|--------|-------|
| 00T | gate | TESTS_VALIDATED | a130ce81 | 13 files/36 tests, compile-RED on C1-C11 symbols; golden captured on main subset; orchestrator-reviewed vs Test Gate matrix |
| 01 | R1 | REVIEWED | 01a16d2b | comparator = per-turn cost estimate (gateway.py estimate_cost shape); floor difficulty default 5 until leaf 07 threads per-turn value |
| 02 | R2 | REVIEWED | 01a16d2b | gate wired in all 3 ProviderManager rotation loops; typed verdict on all-refused, never pool-exhaustion shape |
| 03 | R8 | REVIEWED | d46fee26 | child interrupted post-implementation; wiring verified on disk (validateOfferedToolCalls, openai+anthropic+codex) |
| 04 | R4 | REVIEWED | 298c8ff5 | RLock-only proof; Posture type declared in shadow.go (leaf 05 consumes); CLI+RPC+doc landed |
| 05 | R3 | REVIEWED | 9a37d512 | import-cycle fallback (narrow interface); resolver-hook prompt seam; atomic prompt snapshot |
| 06 | R5 | REVIEWED | d7a6e166 | model-scope arming deduped into alias cooldown (ruling 18); T12/T13/T14 pass live |
| 07 | R7 | REVIEWED | 5a9a2659 | parameter-threaded difficulty (option unreachable at resolve-time); async lane via turn_difficulty metadata; single pin writer preserved |
| 08 | R6 | REVIEWED | 17fc00f8 + 2f34d16d | back-lane body + forward steer backfill; T7 WIRED (handoff ask 3 closed) |
| 09 | R9 | REVIEWED | 2dcf3045 | leaf-06 gap fill landed the full C10 surface; matches back lane's staged work (spillwayCostEpsilon rename applies); pricegroup gate tests in full-suite run |

Status values: PENDING, TESTS_IN_PROGRESS, TESTS_VALIDATED, IN_PROGRESS, IMPLEMENTED, REVIEWED, COMPLETE, BLOCKED.

## Integration Test Plan

1. **Flag-off control (blocking).** With no `quality`, no `data_posture`, no `budget.mode`, no `pool`, and no `price_group` config anywhere, run the existing routing suite and diff the `routing_decisions` rows against `main`. Must be identical. This is the single most important gate: it proves the nine features are additive.
2. **Contract compile check.** Each leaf's declared signature must exist verbatim. Run `go build ./...` after each wave.
3. **Precedence invariance.** Add a test that arms a quota block, an endpoint cooldown, and a sticky pin simultaneously and asserts the winner is unchanged from `main` (quota > endpoint > alias > sticky).
4. **Sentinel identity.** Assert `errors.Is(err, ErrContextTooSmall)` and direct pointer equality for `ErrToolCallInvalid` on the exhaustion path.
5. **e2e suite.** `make e2e-fast-area AREA=sovereign-parity` green; `e2e/manifest.json` carries the path_map entry for `internal/llm/` -> `sovereign-parity`.
6. **Static gates.** `make analyzers`, `make lint-ci`, `make graphs-check`.
7. **Docs.** `docs/workflows/llm-routing-quality.md` (new, maps `internal/llm/` selection) + AGENTS.md invariant summary in the same commit.

## Review Checklist

- [ ] Every leaf's declared interface contract matches the shipped signature exactly.
- [ ] Flag-off behavior is byte-identical to `main` (gate scenario ALL).
- [ ] Selection precedence (quota > endpoint > alias > sticky) is unchanged and pinned by a test.
- [ ] No new bus topic, no new WS event type.
- [ ] No `internal/llm` -> `internal/config` import introduced.
- [ ] Every new feature is opt-in, default false / zero value inert.
- [ ] No new ignored errors; no bare `panic(err)`; no `_ = f()`.
- [ ] No mutex held across I/O.
- [ ] Quality scores are seeded from measurement, not from vendor documentation (Sovereign Router rule: "leaderboards nominate, our own probes score").
- [ ] No provider API key or prompt text reaches a log line.
- [ ] `docs/workflows/` + root `AGENTS.md` updated in the same commit as the code.
- [ ] e2e suite added with a manifest entry; no new unit test files for new features.

## Open Questions

| # | Question | Recommendation | Impact if unresolved |
|---|----------|----------------|---------------------|
| Q1 | Does `config/models.json5` ship with seeded quality scores, or ship empty (feature inert until the operator measures)? | Ship EMPTY. Seeding invented scores violates the repo's "probes score, not leaderboards" rule and would silently reorder aliases on upgrade. | Leaf 01 blocks on the seed decision. |
| Q2 | Is `BudgetModeSteer` an acceptable product default for interactive chat, or must it stay `enforce` globally? | `enforce` default everywhere; `steer` is per-`[llm.budget]` opt-in. Sovereign Router's no-caps ruling is its owner's, not this repo's. | Leaf 08 ships the mode but nobody enables it. Harmless. |
| Q3 | Should `public_only` be the *default* posture for a provider with no explicit declaration (conservative) or `private` (backward-compatible)? | `private` - backward compatibility wins; the flag-off control gate requires it. | Choosing conservative breaks the flag-off gate and every existing install. |
| Q4 | Does the pool concept need a config surface (two providers naming one pool), or is one-pool-per-provider enough for v1? | One pool per provider in v1, with the `pool` field reserved. OpenRouter `:free` and Cloudflare are the only real multi-route pools and both are single-provider. | Leaf 06 grows a config surface and a migration. |
| Q5 | Should R2 admission control consult `llm.context_discovery` (opt-in, default off) or only the static `models.json5` `context_limit`? | Static only in v1. Discovery is off by default and async; an admission gate that depends on a background syncer is a gate that flaps. | Leaf 02 stays small; discovery integration is a follow-up. |
| Q6 | Should price groups (R9) be a ninth leaf, folded into leaf 01, or skipped? | Ninth leaf (`09-price-groups.md`). It is a distinct, independently-verifiable capability; folding it into 01 makes leaf 01 too large and couples schema to resolution. | A separate leaf lets 01 (schema) and 09 (resolution) advance on their own review cycles. |
| Q7 | What is the per-turn difficulty source for R7? | Derive from existing `Intent` signals (`SuggestedMode`/`RequiresPlanning`/input length) via the frozen `DeriveDifficulty` table (Contract C11) - zero extra inference, reuses what meept already computes. Never a second classifier call. | Locked in the master (Difficulty sourcing); leaf 07 implements the table. A caller may still override difficulty explicitly. |