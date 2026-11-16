# 07 Sticky Pin Invalidation on Difficulty Increase - Implementation Leaf

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
- **Scope:** Extend sticky pins with a `Difficulty` field (Contract C8) and add the difficulty **source** (Contract C11): `DeriveDifficulty`, a pure projection of the dispatcher's existing `Intent` signals into a 1-5 level, threaded into the loop where `ResolveForAlias` runs. A pin created for an easy turn is released when the derived difficulty rises past the level the pin was created for, and the caller re-pins at the rotation head. Same-or-lower difficulty keeps the pin.
- **Dependencies:** 00T gate tests committed (RED); 06-pool-scoped-failure.md COMPLETE (serializes behind 06; both edit `resolver.go`)
- **Estimated Context:** 55K
- **Concurrency Group:** A4 (wave 4 - serialized behind 06)
- **Source reference:** Sovereign Router `src/sticky.py:1-60` - "a new *user* message arrives whose classified level is higher than the level the sticky model was chosen for." Plus the difficulty-source analysis (Q7): meept has no per-turn difficulty today, and `Intent` is computed at dispatch, which is a different clock from per-turn model resolution (`loop.go:5309`) - so this leaf adds the derivation seam.

## Goal

meept's sticky pins (`internal/llm/resolver.go:1014-1101`) exist to preserve prompt-cache pricing inside an agent's tool loop. That is the same reason Sovereign Router has them. The difference is the invalidation trigger.

meept releases a pin only when the model **fails**: quota-blocked, endpoint-blocked, or in a cooldown (`releaseQuotaBlockedPin`, `isEndpointBlocked`, `failedModelMatches`). Sovereign Router adds one non-failure trigger: the user raised the difficulty.

Without it, a conversation that starts with "rename this variable" pins the cheap model, and when the user follows with "now redesign the auth layer" the pin keeps the cheap model serving a hard turn. The pin is doing exactly what it was told - the problem is that nobody told it the task got harder.

## Context

- `internal/llm/resolver.go:1014-1101` - `resolveStickyCaller`: the function to modify. Read it in full before editing. Structure:
  - `health.StickyPins` is `map[string]int` (callerKey -> alias member index).
  - `releaseQuotaBlockedPin`, the `pinnedIdx >= len(...)` stale case, `isEndpointBlocked`, and `failedModelMatches` each release the pin and fall through to re-pin.
  - The re-pin block starts at `health.CurrentIndex`, skips quota-blocked and endpoint-blocked candidates with a one-full-lap cap, then writes `health.StickyPins[callerKey] = nextIdx`.
- `internal/llm/resolver.go:1128-1142` - `recordStickyDecision(alias, aliasName, idx, reason)`: writes `RoutingDecision{Reason: reason}`. Existing reason strings are `sticky_request` and `sticky_request_new`. This leaf adds `sticky_difficulty_increase` - the `reason` column is `TEXT` (`internal/llm/routing_log.go:98`), so no schema change.
- `internal/llm/resolver.go:543-545` - the call site: `if alias.BalancedStickyRequests && sessionID != "" { return r.resolveStickyCaller(alias, aliasName, health, sessionID, now), nil }`.
- `internal/llm/selection.go` (leaf 01) - `FilterCtx.Difficulty` already carries the classified difficulty. This leaf threads it into the sticky path.
- `internal/agent/dispatcher.go:101-140` - `Intent`. The difficulty must ride alongside the intent, not replace it. **Survey first**: find where the agent loop knows the current turn's classification and how it reaches the resolver. If no such channel exists, add the narrowest one (a request-scoped option, like `llm.WithResolvedModel`), never a shared mutable field.

## Verified Anchors

- `internal/llm/resolver.go:543-545` - the sticky call site in `ResolveForAlias`
- `internal/llm/resolver.go:1014` - `resolveStickyCaller` signature and body
- `internal/llm/resolver.go:1025-1047` - the existing pin-release cases (quota, stale, endpoint, failed)
- `internal/llm/resolver.go:1049-1101` - the re-pin block with its one-full-lap caps
- `internal/llm/resolver.go:1128-1142` - `recordStickyDecision` and the reason strings
- `internal/llm/routing_log.go:98` - `Reason` column (free text, no migration)
- `internal/llm/selection.go` - `FilterCtx.Difficulty` from leaf 01
- `internal/agent/dispatcher.go:101-140` - `Intent`: `SuggestedMode` (:123), `RequiresPlanning` (:112). The difficulty source inputs.
- `internal/agent/dispatcher.go:2013-2044` - `suggestMode`: the short-input downgrade (<50 chars -> "direct") and quickplan/compound/spec_pair mode synthesis this leaf mirrors in `DeriveDifficulty`.
- `internal/agent/loop.go:5309`, `:6943` - `ResolveForAlias(l.modelRef, l.sessionID)`: the per-turn resolution site that must receive the derived difficulty.
- `e2e/suites/sovereign-parity/sticky_test.go` - gate tests T17, T18

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/llm/resolver.go (or selection.go if cleaner - but the pin map
// lives on AliasHealth in resolver.go, so keep it there)
type StickyPin struct {
    Index      int
    Difficulty int   // the classified difficulty the pin was created for
    CreatedAt  time.Time
}
// health.StickyPins changes from map[string]int to map[string]StickyPin.
// A single writer (resolveStickyCaller) and the existing release paths are
// the only mutators.

// New option, request-scoped (never shared state):
func WithDifficulty(d int) ChatOption
// Read inside resolveStickyCaller as fc.Difficulty. Zero = unclassified =
// the feature is inert (a pin created at 0 is never released for difficulty).

// internal/llm/difficulty.go (Contract C11) - the difficulty SOURCE.
// DeriveDifficulty projects an Intent's routing signals into a 1-5 level.
// Pure function: no classifier call, no I/O. Values come from the values the
// dispatcher already computed, so there is zero extra inference cost.
// A caller with no Intent or no derivable signal passes 0 -> R7 inert.
//
// The frozen table (locked by Q7):
//   requiresPlanning==true              -> level 4
//   mode=="quick_plan"                  -> level 5   (explicit execution phrasing)
//   mode=="spec_plan" && requiresPlan   -> level 4
//   mode=="spec_pair" || mode=="compound"-> level 3
//   otherwise, inputLen<50              -> level 1   (short-input downgrade,
//                                                     mirrors suggestMode)
//   otherwise                           -> level 2
func DeriveDifficulty(mode string, requiresPlanning bool, inputLen int) int
```

### Frozen semantics

```
fc.Difficulty <= 0            -> pin NEVER released for difficulty (inert)
fc.Difficulty > pin.Difficulty -> release the pin, re-pin at the rotation
                                  head, reason "sticky_difficulty_increase"
fc.Difficulty == pin.Difficulty -> keep the pin (reason unchanged)
fc.Difficulty <  pin.Difficulty -> keep the pin (reason unchanged)
```

The release must reuse the EXISTING re-pin block (the one with the quota-block and endpoint-block one-full-lap caps) rather than writing a second pin-assignment path. Two pin writers is how a sticky bug becomes a heisenbug.

### Non-goals

- **Do NOT change sticky TTL or make it sliding.** meept's pin lives for the process lifetime of the health map (LRU 1024 via `SessionEndpoints`; the pin map itself is unbounded but process-scoped). Sovereign Router uses a sliding 30-minute TTL. meept's design is deliberate - "sticky state is a cache-affinity optimisation, not correctness state" - and changing it is out of scope.
- **Do NOT classify inside the resolver.** The resolver receives a difficulty; it never calls a classifier. Classification stays in `internal/agent`.
- **Do NOT re-pin on a lower difficulty.** Only an increase releases.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T0 | Implement `DeriveDifficulty(mode, requiresPlanning, inputLen) int` as the frozen table (Contract C11). Pure, no I/O, no classifier call. Create `internal/llm/difficulty.go`. | Create: `internal/llm/difficulty.go` | `go test -p 2 ./internal/llm/ -run TestDeriveDifficulty` | RED (undefined) | GREEN | Given each row of the frozen table; When derived; Then the level matches |
| T1 | Add `StickyPin` struct; change `AliasHealth.StickyPins` to `map[string]StickyPin`; update every reader/writer (`resolveStickyCaller`, `releasePinsForModel`, `ForgetSessionEndpoint*`) | Modify: `internal/llm/resolver.go` (pin map + all uses) | `go build ./internal/llm/` | RED (compile) | GREEN | Given the new type; When built; Then every pin site compiles with Index/Date/Difficulty |
| T2 | Add `WithDifficulty(d int) ChatOption` and, when no explicit difficulty is given, derive it from the loop's pending intent via `DeriveDifficulty` before the sticky call site | Modify: `internal/llm/resolver.go:543-545`, the `ChatOption` plumbing, `internal/agent/loop.go` | `go test -p 2 ./internal/llm/ -run TestWithDifficulty` | RED (undefined) | GREEN | Given an option; When applied; Then the resolver's `FilterCtx.Difficulty` carries the value |
| T3 | Implement the difficulty release: `fc.Difficulty > pin.Difficulty` deletes the pin and falls through to the existing re-pin block with reason `sticky_difficulty_increase` | Modify: `internal/llm/resolver.go:1025-1047` | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run 'TestSticky_'` | RED | GREEN | Given a pin at difficulty 2; When a difficulty-4 turn resolves; Then the pin moves and the routing-log reason is `sticky_difficulty_increase` |
| T4 | Same-or-lower difficulty keeps the pin (both `==` and `<` cases) | Modify: `internal/llm/resolver.go` | `-run TestSticky_SameDifficultyKeepsPin` | RED | GREEN | Given a pin at 3; When difficulty 2 or 3; Then the same model serves |
| T5 | Inertness: difficulty 0 (unclassified) never releases a pin | same | `-run TestSticky_ZeroDifficultyInert'` | RED | GREEN | Given a pin at 2; When difficulty 0; Then the pin holds |
| T6 | Release reuse: the difficulty release falls through to the SAME re-pin block as the quota/endpoint releases (no second assignment path) | Modify: `internal/llm/resolver.go:1049-1101` | `go build ./internal/llm/` + review | n/a | single writer confirmed | Given a release; When re-pinning; Then the one-full-lap caps apply identically |
| T7 | Routing-log reason is queryable: `sticky_difficulty_increase` appears in `routing_decisions.reason` | - | `-run TestSticky_RoutingLogReason'` | RED | GREEN | Given a difficulty release; When read back; Then the row carries the new reason string |
| T8 | Regression: all existing sticky tests pass unmodified | - | `go test -p 2 ./internal/llm/ -run 'TestSticky|TestResolveForAlias'` | n/a | GREEN, zero edits | Given the suite; When run; Then no assertion changed |

## Self-Verification Checklist

- [ ] T0-T8 complete; gate tests T17, T18 GREEN.
- [ ] `DeriveDifficulty` is pure (no classifier call, no I/O) and matches the frozen table in this leaf verbatim.
- [ ] `StickyPins` has exactly ONE writer path (the existing re-pin block); grep confirms no second assignment.
- [ ] `WithDifficulty` is request-scoped (a `ChatOption`), never a field on the shared `ProviderManager` or `Resolver`.
- [ ] Difficulty 0 is inert.
- [ ] The resolver never calls a classifier.
- [ ] TTL is unchanged; no sliding window added.
- [ ] `releasePinsForModel`, `ForgetSessionEndpoint`, and `ForgetSessionEndpointIf` all updated for the new value type.
- [ ] `go build ./...` and `go test -p 2 ./internal/llm/` green.
- [ ] No `_ = f()`, no bare `panic`, no ignored error.

## Review Checklist

- [ ] Frozen table matches master.md C8 (`> releases`, `==`/`<` keep, `0` inert).
- [ ] The release reuses the existing re-pin block, not a new one.
- [ ] `WithDifficulty` cannot leak into shared state (it is an option, not a field).
- [ ] The new reason string is a free-text column value - no migration was added.
- [ ] Sticky TTL semantics are untouched.
- [ ] Existing sticky tests are unmodified.

## Do NOT commit

The orchestrator commits this leaf after review.