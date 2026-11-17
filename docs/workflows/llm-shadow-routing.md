# llm shadow routing (`llm.route.shadow`) — Contract C6

Sovereign-router-parity tree, leaf 04 (R4). Source reference: Sovereign
Router `POST /v1/shadow` (`src/gateway.py:1121-1180`) — "decision-only, no
upstream call, no ledger entry."

Shadow routing answers "which model would you pick for this prompt, and what
would it cost" **without spending tokens**. Tuning `model_aliases` order,
seeding `quality`, or validating a new provider no longer requires a real
turn against a real model.

## Where the seam lives

- `internal/llm/shadow.go` — `Resolver.ResolveShadow(ctx, ShadowRequest)
  (*ShadowDecision, error)`: the read-only mirror of `ResolveForAlias`
  (internal/llm/resolver.go). It is a NEW function, not a refactor of the
  live path; `ResolveForAlias` is untouched.
- `internal/rpc/shadow_route.go` — the `llm.route.shadow` RPC handler and
  the frozen wire view (`ShadowRouteView` / `ShadowCandidateView`).
- `internal/daemon/shadow_rpc_wiring.go` — `registerShadowRouteRPC`, the
  nil-safe registration helper called from daemon boot (pattern:
  `registerIntakeStatusRPC`).
- CLI: `meept routing shadow --alias <name> --prompt <text> [--json]`
  (cmd/meept/route_shadow.go, subcommand of the existing `routing` tree).

## The no-side-effect invariants (the whole point)

`ResolveShadow` MUST NOT:

- issue any HTTP request to an upstream provider
- INSERT into `llm_calls`, `provider_requests`, or `routing_decisions`
- call `Budget.RecordCost` / `RecordCostWithScope` / `RecordUsage`
- mutate `AliasHealth` (`CurrentIndex`, `CooldownUntil`, `StickyPins`,
  `ConsecutiveFails`, `RevertAt`)
- create or release a sticky pin
- mutate `Resolver.endpointBlocks` / `entryBlocks` / `credentialBlock`
- increment any counter, call the tier gate, or consume rotation depth

`ResolveShadow` MUST:

- acquire `r.mu.RLock` only (never `Lock`) — the `Resolver.mu` field was
  widened from `sync.Mutex` to `sync.RWMutex` for this reader; every
  existing writer keeps `Lock()`/`Unlock()` with identical semantics
- return the SAME model the live path would pick, given identical state
- return every candidate exactly once, with a skip reason for the ones it
  rejected (no silent drops)
- never return an error for a legitimately-unroutable request — instead a
  decision with `Selected=false` on every candidate plus per-candidate
  reasons. The ONLY error is a genuinely unknown alias/ref name (a caller
  mistake, surfaced by the RPC as reason `shadow_unknown_alias`).

Verified by the e2e gate (`e2e/suites/sovereign-parity/shadow_test.go`):
`TestShadow_ParityWithLivePath` (same pick as the live routing log) and
`TestShadow_NoSideEffects` (zero `llm_calls` / `routing_decisions` rows, no
sticky pin created, rotation head untouched).

## What the decision mirrors

For an alias, the read-only ladder matches the live path step for step:
default-model reversion, alias-level cooldown advance, quota-block rotation,
endpoint-block rotation, sticky-pin simulation (a balanced-sticky alias with
a `SessionID` computes what pin the caller WOULD get — inspecting existing
pins read-only, storing none), the session-endpoint preference hook, the
final joint re-validation, and the selection-chain hook (quality floor;
`VerdictFloor` vetoes the winner exactly as the live chain refuses the turn).

Skip-reason vocabulary on candidates: `quota_blocked`,
`endpoint_blocked`, `alias_cooldown`, `not_rotation_head`, plus the
selection-chain reasons (e.g. `quality_below_required`, and
`context_too_small` once leaf 02's admission filter registers). The decision
`Reason` is one of the frozen values `shadow_alias` | `shadow_direct` |
`shadow_default` (plus `shadow_unknown_alias` / `shadow_unavailable` from
the RPC envelope).

## Cost + token estimate

The estimate is computed locally and returned IN the response — it is never
persisted (the `routing_decisions` table gains NO columns):

- `est_input_tokens`: the package heuristic tokenizer (3 chars/token,
  internal/llm/tokenizer.go) over message content plus per-tool schema
  overhead.
- `est_output_tokens`: the model's configured `max_output`; 0 when
  unconfigured (never fabricate a bound).
- `est_cost_usd`: `(EstInput/1e6)*CostPerMillionInput +
  (EstOutput/1e6)*CostPerMillionOutput` — the `ModelConfig.TotalCost`
  arithmetic computed from the request's estimates. Per-candidate
  `est_cost_usd` uses the same formula with the shared input estimate.

The estimator is pure arithmetic: it never calls the tier gate and consumes
no depth.

## Wire contract (frozen)

`llm.route.shadow` request:

```json
{"alias": "general", "prompt": "summarize this", "model_ref": ""}
```

`model_ref` (a "provider/model" ref) takes precedence over `alias`. Empty
both previews the default model. Response (`ShadowRouteView`; field names
ARE the cross-surface contract — TUI/GUI/CLI parity, mirroring the
`intake.status` precedent):

```json
{
  "model_id": "model-s1",
  "provider_id": "provsh1",
  "alias": "general",
  "reason": "shadow_alias",
  "est_input_tokens": 12,
  "est_output_tokens": 4096,
  "est_cost_usd": 0.004108,
  "quality": 0,
  "candidates": [
    {"model_id": "model-s1", "provider_id": "provsh1", "quality": 0,
     "est_cost_usd": 0.004108, "selected": true},
    {"model_id": "model-s2", "provider_id": "provsh2", "quality": 0,
     "est_cost_usd": 0.008216, "selected": false,
     "skip_reason": "not_rotation_head"}
  ]
}
```

Transport: owner-trusted Unix socket, no token auth — identical to
`intake.status`. Registered unconditionally: an unwired resolver answers the
disabled envelope (`reason: "shadow_unavailable"`), never "method not
found".

## Signals ride the response, never the bus

No new bus topic, no new WS event type — the decision IS the response
payload (same posture as the `llm_tier*` response-metadata precedent,
internal/llm/tier_policy.go).

## Leaf-05 coordination note

`ShadowDecision.Posture` (the C6 union shape, drift-audit ruling #3) uses
the `Posture` type, which leaf 05 owns. Because wave 1 dispatches leaf 04
ahead of leaf 05, the type + the three frozen constants
(`private` / `public_only` / `non_sensitive`) are declared in
internal/llm/shadow.go. `posture.go` (leaf 05) owns the full surface —
`ParsePosture`, `AllowsSensitive`, `postureFilter` — and must NOT redeclare
the type/constants. Until leaf 05 wires the real value, `Posture` on the
decision carries the backward-compatible default `PosturePrivate` (absent
key = private, master Q3).
