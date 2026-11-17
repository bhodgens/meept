# Sovereign-router parity: the LLM routing selection chain

Plan tree: docs/plans/20261115-sovereign-router-parity/ (R1-R9, landed
2026-11-16/17). This doc maps the new selection surfaces in
`internal/llm/` and the invariants that bind them.

## The chain (Contract C1, selection.go)

Three pre-dispatch filters, FIXED registration order in NewResolver:

1. postureFilter (R3, posture.go) - a sensitive turn never reaches a
   non-cleared model. Sensitive = secret_scanner.Scan hits on the prompt
   (match text is never logged, only the count).
2. qualityFilter (R1, quality.go) - difficulty -> floor table
   (QualityFloorFor: 1-2 -> 1, 3 -> 2, 4 -> 3, 5 -> 4, 0 -> inert).
   Absent `quality` block = zero = never binds.
3. admissionFilter (R2, admission.go) - EstInput + min(MaxTokens,64) >
   ContextLimit -> skip("context_too_small"); ContextLimit <= 0 = unknown
   = allow. Wired at all three ProviderManager rotation loops; a
   reject-everything request surfaces *ContextTooSmallError (NonRetryable,
   never parked, never retried).

runSelectionFilters returns the FIRST non-Allow verdict. There is NO
fourth (budget) filter: advisory budget binds at the client budget gate
and the provider-manager tier steer (see llm-advisory-budget.md) —
drift-audit ruling 4.

Frozen precedence stays: quota blocks > endpoint cooldowns > alias blocks
> stickiness. The chain filters candidates AFTER alias resolution; they
never reorder it.

## Per-turn difficulty (C11, difficulty.go; leaf 07)

DeriveDifficulty(mode, requiresPlanning, inputLen) projects the
dispatcher Intent to 1-5, no second classifier call. It threads to
ResolveForAlias (sync + async lanes via turn_difficulty metadata); a
sticky pin created at difficulty D releases only when a later turn
exceeds D strictly (reason sticky_difficulty_increase in the routing
log). Difficulty 0 = inert.

## Pool-scoped failure memory (C5, pool_failure.go; leaf 06)

ClassifyFailure(provider, model, status, body) -> (class, scope).
401/402/403 = account scope (sibling models on the provider skip), 404 =
model, 400/413/422 = request (blocks nothing), 429-with-pool-phrase =
pool, 5xx = transient/model. Block keys widen to (scope, target); legacy
host|credential keys keep working.

## Shadow routing (C6, shadow.go; leaf 04)

Resolver.ResolveShadow = full decision, ZERO side effects: no upstream
HTTP, no llm_calls / metrics rows, no budget consumption, no health /
pin / block mutation, RLock only. RPC llm.route.shadow (read-only,
intake.status precedent) + `meept route shadow`. See
llm-shadow-routing.md.

## Streamed tool-call validation (C9, stream_toolcheck.go; leaf 03)

ValidateToolCalls(resp, offered): unknown function name or non-object /
unparseable arguments -> ErrToolCallInvalid (BARE sentinel, pointer
identity). Runs after successful parse, before the executor, on all
three clients, streaming validated once at stream end. Empty
completions still classify ErrEmptyResponse first - ordering untouched.
Offered-list nil = no contract = pass (no-tool turns are a different
defect class, deliberately not owned here).

## Config surface (all opt-in, absent = byte-identical)

models.json5 per-model: `quality`, `quality_source`,
`quality_verified_at`, `data_posture` (default private). Per-alias:
`price_group` ("" | free | paid | premium; see llm-price-groups.md).
meept.json5: `llm.budget.mode` (enforce default | steer; see
llm-advisory-budget.md).

## Hard rules

- No new bus topics, no new WS event types. Signals ride response
  metadata (llm_tier* precedent) and the existing agent.quota_wait topic
  (class budget_advisory).
- No internal/llm -> internal/config import; config mirrors as plain
  structs.
- Quality scores come from measurement, never vendor docs (probes score,
  leaderboards nominate).
- Gate: e2e/suites/sovereign-parity/ (36 tests) + flag-off golden control.
