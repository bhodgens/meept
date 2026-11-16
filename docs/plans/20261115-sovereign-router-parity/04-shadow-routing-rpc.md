# 04 Shadow Routing (Read-Only Decision Preview) - Implementation Leaf

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
- **Scope:** Add `Resolver.ResolveShadow` (Contract C6) plus a read-only `llm.route.shadow` RPC: run the full selection pipeline (filters, quality, admission, cost estimate) and return the decision with NO upstream call, NO `llm_calls` row, NO budget consumption, NO alias-health mutation, NO sticky-pin creation.
- **Dependencies:** 00T gate tests committed (RED); 01-quality-dimension.md COMPLETE (needs `runSelectionFilters` and `Candidate`); 02-admission-context.md COMPLETE (shadow inherits the admission gate)
- **Estimated Context:** 60K
- **Concurrency Group:** D (wave 1 - file-disjoint from 01/02/03)
- **Source reference:** Sovereign Router `POST /v1/shadow` (`src/gateway.py:1121-1180`) - "decision-only, no upstream call, no ledger entry"

## Goal

There is no way today to ask meept "which model would you pick for this prompt, and what would it cost" without spending tokens. Tuning `model_aliases` order, seeding `quality`, or validating a new provider all require a real turn against a real model.

Sovereign Router solves this with `/v1/shadow`: same auth, same sensitivity scan, same classifier, same candidate selection, same cost estimate - and then it stops. No upstream call, no route-log side effects that matter for billing, no ledger row.

This leaf gives meept the same capability as a read-only RPC, which is the highest-leverage missing tool from the comparison (proposal P1).

## Context

- `internal/rpc/intake_status.go` - the read-only RPC pattern to copy: a `...View` struct whose JSON field names ARE the cross-surface contract (TUI/GUI/CLI), a handler in `internal/daemon/`, registration in the daemon's RPC wiring. Shadow follows this exactly.
- `internal/llm/resolver.go:528` - `ResolveForAlias`: the live path. Shadow calls the same selection logic but must not mutate. **Read this function carefully** - it mutates `health.CurrentIndex`, `health.CooldownUntil`, `health.StickyPins`, and `health.StickyPins[callerKey]`. Shadow must reach the same *decision* without those writes.
- `internal/llm/resolver.go:1014-1101` - `resolveStickyCaller`: creates a pin when absent. Shadow must compute what the pin *would* be without storing it.
- `internal/llm/resolver.go:1128-1142` - `recordStickyDecision`: writes the routing log. Shadow must not call it.
- `internal/llm/routing_log.go:91-105` - `routing_decisions` table. No cost/latency column exists; do not add one (the shadow estimate is returned in the RPC response, not persisted).
- `internal/llm/budget.go:225-262` - `RecordCost` / `RecordCostWithScope`. Shadow must not call either.
- `internal/llm/models.go:410-412` - `TotalCost()`. The shadow cost estimate is `(EstInput/1e6)*CostPerMillionInput + (EstOutput/1e6)*CostPerMillionOutput`, computed locally.
- `internal/llm/tier_policy.go:42-47` - `MetaKeyLLMTier*` - the "signal via metadata, never a new bus topic" precedent.

## Verified Anchors

- `internal/rpc/intake_status.go:1-30` - the read-only RPC shape to mirror
- `internal/daemon/intake_status_rpc.go` - the daemon-side registration to mirror
- `internal/llm/resolver.go:528` - `ResolveForAlias` (mutations to avoid)
- `internal/llm/resolver.go:793` - `RememberSessionEndpoint` (do not call)
- `internal/llm/resolver.go:1014` - `resolveStickyCaller` (pin creation to avoid)
- `internal/llm/resolver.go:1128` - `recordStickyDecision` (log write to avoid)
- `internal/llm/routing_log.go:91-105` - `routing_decisions` schema (no cost column)
- `internal/llm/budget.go:225` - `RecordCost` (do not call)
- `internal/llm/selection.go` - from leaf 01: `runSelectionFilters`, `Candidate`, `FilterCtx`
- `internal/llm/admission.go` - from leaf 02: `admissionFilter`
- `e2e/suites/sovereign-parity/shadow_test.go` - gate tests T10, T11

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/llm/shadow.go
type ShadowRequest struct {
    Alias      string   // alias name OR "provider/model" ref; empty -> default model
    Messages   []Message
    Tools      []ToolDef
    SessionID  string   // optional; used for sticky simulation, never written
    Difficulty int      // 0 = unclassified
}

type ShadowCandidate struct {
    ModelID    string
    ProviderID string
    Quality    int
    EstCostUSD float64
    Selected   bool
    SkipReason string   // empty when Selected
}

type ShadowDecision struct {
    ModelID    string
    ProviderID string
    Alias      string
    Reason     string   // "shadow_alias" | "shadow_direct" | "shadow_default"
    EstInput   int
    EstOutput  int
    EstCostUSD float64
    Quality    int
    Candidates []ShadowCandidate
}

func (r *Resolver) ResolveShadow(ctx context.Context, req ShadowRequest) (*ShadowDecision, error)
```

```
// internal/rpc/shadow_route.go - the wire contract. Field names are FROZEN
// (TUI/GUI/CLI parity, mirroring the intake.status precedent).
type ShadowRouteRequest struct {
    Alias    string `json:"alias,omitempty"`
    Prompt   string `json:"prompt"`
    ModelRef string `json:"model_ref,omitempty"`
}
type ShadowRouteView struct {
    ModelID    string           `json:"model_id"`
    ProviderID string           `json:"provider_id"`
    Alias      string           `json:"alias"`
    Reason     string           `json:"reason"`
    EstInput   int              `json:"est_input_tokens"`
    EstOutput  int              `json:"est_output_tokens"`
    EstCostUSD float64          `json:"est_cost_usd"`
    Quality    int              `json:"quality"`
    Candidates []ShadowCandidateView `json:"candidates"`
}
type ShadowCandidateView struct {
    ModelID    string  `json:"model_id"`
    ProviderID string  `json:"provider_id"`
    Quality    int     `json:"quality"`
    EstCostUSD float64 `json:"est_cost_usd"`
    Selected   bool    `json:"selected"`
    SkipReason string  `json:"skip_reason,omitempty"`
}
```

RPC method name: `llm.route.shadow`. Registered read-only, owner-trusted (Unix socket, no token auth - per the repo's Unix-RPC posture in AGENTS.md).

### What This Leaf Consumes

- `runSelectionFilters` (leaf 01) and `admissionFilter` (leaf 02).
- `ModelConfig.CostPerMillionInput/Output` for the estimate.
- `Budget` - read-only consultation only (`Advisory()` from leaf 08 is NOT required; this leaf must not block on it).

### Frozen invariants (the whole point of the leaf)

```
ResolveShadow MUST NOT:
  - issue any HTTP request to an upstream provider
  - INSERT into llm_calls, provider_requests, or routing_decisions
  - call Budget.RecordCost / RecordCostWithScope / RecordUsage
  - mutate AliasHealth (CurrentIndex, CooldownUntil, StickyPins, ConsecutiveFails)
  - create or release a sticky pin
  - mutate Resolver.endpointBlocks / entryBlocks / credentialBlock
  - increment any counter

ResolveShadow MUST:
  - acquire r.mu.RLock (never Lock)
  - return the SAME model the live path would pick, given identical state
  - return every candidate with a skip reason for the ones it rejected
  - never return an error for a legitimately-unroutable request; instead
    return a decision with Selected=false and per-candidate reasons
```

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add `ShadowRequest`, `ShadowCandidate`, `ShadowDecision` with JSON tags | Create: `internal/llm/shadow.go` | `go build ./internal/llm/` | RED (compile) | GREEN | Given the structs; When marshalled; Then the field names match the frozen wire contract |
| T2 | Implement the cost estimator `(EstInput/1e6)*in + (EstOutput/1e6)*out` | Modify: `internal/llm/shadow.go` | `go test -p 2 ./internal/llm/ -run TestShadowCost` | RED (undefined) | GREEN | Given known prices and token counts; When estimated; Then the arithmetic matches to 1e-9 |
| T3 | Implement `ResolveShadow` read-only core: RLock, resolve alias/default/direct ref, build the candidate list, run the filters, mark the winner | Modify: `internal/llm/shadow.go` | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run 'TestShadow_'` | RED (undefined) | GREEN | Given a configured alias; When shadow-resolved; Then `ModelID` equals the live `ResolveForAlias` result |
| T4 | Candidate list completeness: every candidate appears exactly once with either `Selected:true` or a non-empty `SkipReason` | Modify: `internal/llm/shadow.go` | `-run TestShadow_CandidatesComplete'` | RED | GREEN | Given a 3-member alias; When shadow-resolved; Then 3 candidates, 1 selected, 2 with reasons |
| T5 | No-write proof: zero rows in `llm_calls`, `provider_requests`, `routing_decisions` after a shadow call | Modify: `internal/llm/shadow.go` (verify only) | `-run TestShadow_NoSideEffects'` | RED | GREEN | Given a fresh daemon; When shadow resolves twice; Then all three tables are empty |
| T6 | No-budget proof: `Budget` counters and `hourlyCostWindow` are unchanged | same | `-run TestShadow_NoBudgetConsumption'` | RED | GREEN | Given a non-zero budget baseline; When shadow resolves; Then the counters are identical |
| T7 | No-sticky proof: a shadow resolve creates no pin; a subsequent live resolve behaves as if shadow never ran | same | `-run TestShadow_NoStickyPin'` | RED | GREEN | Given an alias with sticky on; When shadow then live resolve; Then the live pick equals a clean live pick |
| T8 | No-health-mutation proof: `CurrentIndex` and `CooldownUntil` are unchanged | same | `-run TestShadow_NoHealthMutation'` | RED | GREEN | Given a mid-rotation alias; When shadow resolves; Then the health snapshot matches |
| T9 | Admission inheritance: an over-context shadow request reports the admission skip, not a bogus selection | same | `-run TestShadow_InheritsAdmission'` | RED | GREEN | Given a 100k prompt and a 64k model; When shadow-resolved; Then the skip reason is `context_too_small` |
| T10 | Add the `llm.route.shadow` RPC handler + `ShadowRouteView` (frozen field names) | Create: `internal/rpc/shadow_route.go`; Modify: `internal/daemon/shadow_rpc_wiring.go` | `go build ./...` | RED (compile) | GREEN | Given a request; When the RPC is called; Then the view's JSON matches the frozen contract |
| T11 | CLI surface: `meept route shadow --alias <name> --prompt <text>` prints the decision as JSON | Create: `cmd/meept/route_shadow.go` (or the nearest existing route command file); Modify: `cmd/meept/main.go` command registration | `./bin/meept route shadow --help` | RED | GREEN | Given the built binary; When `--help`; Then the subcommand is listed with its flags |
| T12 | Docs: `docs/workflows/llm-shadow-routing.md` mapping the RPC + resolver seam | Create: `docs/workflows/llm-shadow-routing.md` | file exists, cross-references resolve | n/a | present | Given the doc; When read; Then it names `internal/llm/shadow.go`, `internal/rpc/shadow_route.go`, and the no-side-effect invariants |

## Self-Verification Checklist

- [ ] T1-T12 complete; gate tests T10, T11 GREEN.
- [ ] `ResolveShadow` takes `RLock` only - grep the function body for `r.mu.Lock(` and confirm zero hits.
- [ ] No call to `RecordCost`, `RecordUsage`, `recordStickyDecision`, `RememberSessionEndpoint`, or any `INSERT`.
- [ ] The winner matches `ResolveForAlias` for the same state (proven by T3).
- [ ] Every rejected candidate carries a reason; no silent drops.
- [ ] The RPC field names match the frozen contract exactly (they are a TUI/GUI/CLI surface).
- [ ] Read-only RPC, owner-trusted Unix socket, no token auth (matches `intake.status`).
- [ ] No new bus topic, no new WS type.
- [ ] `go build ./...` and `go test -p 2 ./internal/llm/` green.
- [ ] No `_ = f()`, no bare `panic`, no ignored error.

## Review Checklist

- [ ] Each MUST NOT in the frozen invariants has a corresponding passing test (T5-T9).
- [ ] `git diff` on `resolver.go` shows no logic change to `ResolveForAlias` (shadow is a NEW function, not a refactor of the live path).
- [ ] The RPC is registered read-only and cannot mutate state.
- [ ] Wire field names are frozen and documented in `internal/rpc/`.
- [ ] The CLI subcommand follows the repo's cobra conventions and lowercase UI text rule.
- [ ] Docs exist and map the seam.

## Do NOT commit

The orchestrator commits this leaf after review.