# 04-model-selection - Per-Child Model / Provider / Reasoning-Effort Selection

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests for this branch
> (00T-tests.md) ALREADY EXIST and are committed — your job is to make them
> pass; do NOT delete, skip, weaken, or rewrite any gate test (report
> suspected-wrong gate tests to the orchestrator instead). Do NOT commit —
> the orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files — explore with search_files or terminal
> cat. After writing a file, do NOT read it back — write once and stop.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Per-child LLM route selection — `provider`/`model`/`reasoning_effort`
  on `DelegateRequest`, validated against a route allowlist and preflighted
  through meept's resolver before the child spawns. Implements B4.
- **Dependencies:** 01-spawn-async (B1 `ChildOptions`, `SetRouteResolver`),
  00T-tests (gate F4)
- **Estimated Context:** 55K
- **Concurrency Group:** D (parallel with 05, after 01+03 REVIEWED)
- **Audit references:** dsh-audit GAP 4 (per-child model), model-selection.ts

## Goal

meept's `delegate_task` exposes no per-child model/provider/reasoning
override — `modelRefForAgent` (registry.go:484-504) binds the child to its
spec's model at loop construction, and a delegation is stuck with that.
dsh's subagent tool threads `provider`/`model`/`reasoning_effort` per call,
merges them over parent/configured defaults in a defined precedence, enforces
a Session route allowlist, and preflights the effective route through the live
LLM adapter before any child exists (model-selection.ts:99-128,139-159,182-202).
This leaf ports the pure selection logic into
`internal/agent/modelselect`: `ResolveChildRoute` (merge + allowlist assert)
and `PreflightRoute` (resolver reachability), wired into 01's
`DelegateManager.SetRouteResolver` so every spawn path validates the child's
route before the goroutine runs.

## Context

- `internal/agent/delegate.go` (01) — `ChildOptions{Provider,Model,ReasoningEffort}`, `SetRouteResolver(func(ChildOptions, DelegateRequest) (ChildOptions, error))`.
- `internal/agent/registry.go:484-504` — `modelRefForAgent` precedence (model: > model_alias: > same-id alias > global default). The child's chosen route must still reconcile with this: an explicit `Provider`/`Model` on the request is the delegation-time override.
- `internal/llm` — resolver surface: `HasAlias`, `ResolveForAlias`, provider registration. `PreflightRoute` needs the route to be resolvable (alias exists OR provider/model reachable) before spawn.
- `internal/tools/builtin/platform.go:284-307` — the delegate_task Parameters that gain model/provider/reasoning_effort (already added by 01 T5).
- dsh reference: `/tmp/deepseek-harness/packages/subagent/tool-subagent/src/model-selection.ts` (the whole file: route key, merge, assert, preflight; `ReasoningEffortId`).

## Verified Anchors (grep-confirmed at authoring time)

- internal/agent/registry.go:484-504 — `modelRefForAgent` precedence chain
- internal/tools/builtin/platform.go:284-307 — tool Parameters (new model/provider/reasoning_effort fields)
- internal/llm/resolver — `HasAlias(alias) bool` (grep verified in registry.go use); `PreflightRoute` resolves via this
- internal/agent/delegate.go (01) — `ChildOptions` + `SetRouteResolver` seam
- dsh model-selection.ts:99-128 — `requestedAgentOptions` merge precedence (parent < configured < request; provider+model together; route-change-clears-effort)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/modelselect/modelselect.go  (NEW package)
type AllowedModelRoute struct{ Provider, Model string } // provider+model exact pair
type ModelSelectionPolicy struct{ Routes []AllowedModelRoute }
func ResolveChildRoute(parent ChildOptions, configured *ChildOptions, req DelegateRequest,
        policy *ModelSelectionPolicy, enabled bool) (*ChildOptions, error)
    // mirrors dsh requestedAgentOptions + assertAllowedModelSelection:
    //   - no selection requested -> configured (may be nil -> parent route)
    //   - !enabled && selection requested -> error
    //   - provider+model must be supplied together; empty string rejected
    //   - policy nil -> only the parent route allowed
    //   - policy set -> route must be an allowed member
    //   - route changed + no effort -> cleared per configured route-owned effort
func PreflightRoute(ctx context.Context, resolver *llm.Resolver, opts ChildOptions) error
    // route must resolve via the resolver (alias present / provider reachable)
    // BEFORE the child spawns; errors visibly at the tool boundary.
func SetRoute(on *DelegateRequest, route *ChildOptions) // helper mapping route onto the request for 01
```

### What This Leaf Consumes

- B1 (01) — `ChildOptions`, `DelegateRequest.Model/Provider/ReasoningEffort`, `DelegateManager.SetRouteResolver`
- B4 (self) — the two functions above
- llm resolver — `HasAlias` / provider reachability for preflight

## Tasks

### T1: ResolveChildRoute — merge + allowlist (pure)

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T1 | Port `ResolveChildRoute` (merge precedence + allowlist assert) into modelselect | Create: internal/agent/modelselect/modelselect.go; Test: internal/agent/modelselect/modelselect_test.go (gate) | `go test -p 2 ./internal/agent/modelselect/ -count=1` | package/symbols undefined | merge honors parent<configured<request; provider+model together; empty rejected; policy enforced | Given parent route X and a request for Z when a policy allows Z; When ResolveChildRoute runs; Then it returns Z; a request for W outside the policy errors; a provider-only request errors |

T1 (expand):
1. Confirm the gate test REDs.
2. Port the exact dsh semantics (model-selection.ts:99-159) into Go:

```go
type AllowedModelRoute struct{ Provider, Model string }
type ModelSelectionPolicy struct{ Routes []AllowedModelRoute }

func ResolveChildRoute(parent ChildOptions, configured *ChildOptions,
        req DelegateRequest, policy *ModelSelectionPolicy, enabled bool) (*ChildOptions, error) {
    // hasSelection = any of req.Model/Provider/ReasoningEffort != ""
    // if !hasSelection: return configured (may be nil navigated back to parent)
    // if !enabled: return error
    // provider+model must be supplied together (one set -> error); empty string -> error
    // baseline = configured?.Provider ?? parent.Provider, etc.
    // routeChanged = provider supplied && (provider != baseline || model != baseline)
    // effort: suppress configured route-owned effort when routeChanged && no explicit effort
    // allowed = policy==nil ? (route == parent route) : member(policy.Routes)
    //   if !allowed: return error
    // return &ChildOptions{merged...}
}
```

Return a COPY (never aliasing `req`/`configured`). Engine the allowlist nil
rule exactly per dsh (policy nil => only the parent route; set policy => route
must be a member; never a no-policy free-for-all).
3. Verify pass via `go test -p 2 ./internal/agent/modelselect/ -count=1`.

### T2: PreflightRoute — resolver reachability

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T2 | `PreflightRoute` validates the effective route resolves through meept's resolver before spawn | Create: internal/agent/modelselect/modelselect.go (continue); Test: (gate, modelselect_test.go) | `go test -p 2 ./internal/agent/modelselect/ -count=1` | preflight not enforced | a route absent from the resolver errors before the child runs | Given `resolver` that lacks the child's alias; When PreflightRoute runs; Then it returns an error naming the unresolvable route and no child spawns |

T2 (expand): implement `PreflightRoute(ctx, resolver, opts)`:
- If `opts.Provider == "" || opts.Model == ""`, reuse the parent resolution path only when it leads to a resolvable route; a genuinely empty route is a preflight error.
- Resolve via `resolver.HasAlias(opts.Model)` / provider reachability (mirror `modelRefForAgent`'s resolver usage at registry.go:492-503). If the model is an alias the resolver has, pass; if a direct provider/model the resolver doesn't admit, error with the route named.
- Signature `func PreflightRoute(ctx context.Context, resolver *llm.Resolver, opts ChildOptions) error` — if `*llm.Resolver` import cycles (modelselect must not import internal/agent), use a narrow interface `type RouteResolver interface { HasAlias(string) bool }` and note it; prefer the concrete resolver.
3. Verify via the gate preflight tests.

### T3: Wire into DelegateManager route resolution

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T3 | `SetRouteResolver` wires ResolveChildRoute+PreflightRoute into spawn | Modify: internal/agent/delegate.go (set the route resolver func); Test: e2e delegate-seam (`TestDelegate_ModelRoute`) | `make e2e-fast-area AREA=delegate-seam` | per-child model ignored (gate e2e RED) | a spawn with an allowed route pins the child's model; a disallowed route errors and spawns nothing | Given a caller that sets model/provider on delegate_task (an allowed route); When the child spawns; Then it runs on that route; a disallowed route returns an error and no child is created |

T3 (expand):
1. In 01's manager construction, call `mgr.SetRouteResolver(func(route ChildOptions, req DelegateRequest) (ChildOptions, error) { ... })` that runs `ResolveChildRoute(parent, configured, req, policy, enabled)` + `PreflightRoute`. The `configured`/`policy`/`enabled` come from the manager's app config (a `ModelSelectionPolicy` built from models.json5 allowlist + a tool-instance `enabled` flag). Where the manager doesn't yet carry them, add fields via 01's config seam — do NOT fork the manager; the app config is available at daemon wiring.
2. Preflight runs BEFORE the goroutine in 01's `Spawn` (which already calls routeResolver and errors the envelope) — confirm 01 did that; if it only resolves post-spawn, move the preflight before the goroutine and error out of `Spawn`. This is the "validate before any child exists" contract (audit 1a).
3. Verify via `make e2e-fast-area AREA=delegate-seam`.

### T4: Surface the policy + document

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T4 | A durable `allowed_model_routes` (allowlist) accessible to the user + doc | Modify: internal/config (model selection policy), docs/workflows/delegation.md (routes section) | `scripts/config-drift-check.py` (if config touched) | allowlist has no CLI/config surface | a user can add/remove an allowed provider/model route via config; doc explains precedence | Given a config `delegation.allowed_routes`; When a user adds a route; Then spawns using that route pass preflight, and other routes error |

T4 (expand): read an `allowed_routes` allowlist from the app config
(`delegation.allowed_routes`, JSON5) into the `ModelSelectionPolicy` the
daemon hands the manager. Document precedence (parent < configured < request;
policy nil => parent route only) in `docs/workflows/delegation.md`. If the
config schema needs a `// drift:` annotation (scripts/config-drift-check.py),
add it and regenerate config docs.

## Self-Verification Checklist

- [ ] ResolveChildRoute ports dsh merge/assert (provider+model together, empty rejected, parent-only fallback, allowlist); gate T1 GREEN
- [ ] PreflightRoute resolves before any child spawns; gate RED->GREEN
- [ ] Wired into DelegateManager.SetRouteResolver; e2e model-route scenario GREEN
- [ ] No scope creep — no workflow engine, no spawning logic beyond the route seam
- [ ] modelselect package has no import cycle with internal/agent (use the RouteResolver interface if needed); gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path (modelselect/modelselect.go, delegate.go wiring, config, docs).
2. **Verify command + output:** `go test -p 2 ./internal/agent/modelselect/ -count=1`; `make e2e-fast-area AREA=delegate-seam`; `go build ./...`; last ~20 lines.
3. **Deviations:** none / list (esp. the resolver interface if `*llm.Resolver` cycled, and the policy source).

## Review Checklist (For Review Agent)

- [ ] dsh merge/assert semantics exact (provider+model coupled, empty rejected, allowlist nil-parent-only, route-change-clears-effort)
- [ ] Preflight happens before the child goroutine; errors are envelope results, not panics
- [ ] Wired via B1 SetRouteResolver; no second manager; modelselect has no import cycle
- [ ] Conventions; typed-nil guards on resolver/policy wiring; no debug artifacts / line-number corruption

Output: APPROVED or specific gaps (file + line).

## Notes

- Per-child model pinning (Orchestrator Open Question 1): `modelRefForAgent`
  binds at loop construction. This leaf resolves + preflights the route;
  leaf 01's `runChild` applies it (request-scoped override preferred over
  mutating the shared spec). If 01 cannot apply a per-child model without
  widening, the orchestrator reconciles both contracts at review — do not
  paper over it with a comment.
- Root AGENTS.md quota/refusal invariants: `PreflightRoute` must treat
  `HasAlias` as the reachability test, NOT enforce quota/slot gating (that is
  the resolver's job at call time). Never short-retry or surface a quota
  error here.
