# Model Alias Enhancements - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 2 leaf documents
- **Scope:** Add `default_model` and `balanced_sticky_requests` options to model aliases

## Goal

Model aliases currently support only round-robin rotation with cooldown-based failover. This feature adds two configuration options to `ModelAliasEntry`:

1. **`default_model`**: When set, after cooldown expires, revert to the specified model instead of continuing round-robin from the current position.
2. **`balanced_sticky_requests`**: When true, pin each caller (session/task) to a single model within the alias so concurrent callers distribute across models, while the pinned model is released when it enters cooldown.

## Architecture

Both features live in the `internal/llm` package. `ModelAliasEntry` in `providers.go` gains two fields. `AliasHealth` in `models.go` gains a `StickyPins` map keyed by caller ID. `Resolver.ResolveForAlias` gains a `callerKey` parameter and new logic for both features. Call sites in `internal/agent/loop.go` pass `l.sessionID`; classifier/analyst sites pass empty string (no sticky behavior).

## Interface Contracts

### Contract 1: ModelAliasEntry struct

```go
// File: internal/llm/providers.go
type ModelAliasEntry struct {
    Models                 []string `json:"models"`
    Timeout                int      `json:"timeout"`
    MaxFails               int      `json:"max_fails"`
    DefaultModel           string   `json:"default_model,omitempty"`              // optional: revert to this on cooldown expiry
    BalancedStickyRequests bool     `json:"balanced_sticky_requests,omitempty"`   // optional: pin callers to single model
}
```

Owner: 01-core-types.md
Consumers: 02-call-sites.md (read-only)

### Contract 2: AliasHealth struct

```go
// File: internal/llm/models.go
type AliasHealth struct {
    CurrentIndex     int
    ConsecutiveFails int
    LastFailure      time.Time
    CooldownUntil    time.Time
    StickyPins       map[string]int  // callerKey -> pinned model index (nil = no sticky)
}
```

Owner: 01-core-types.md
Consumers: 02-call-sites.md (read-only via Resolver methods)

### Contract 3: ResolveForAlias signature

```go
// File: internal/llm/resolver.go
func (r *Resolver) ResolveForAlias(aliasName string, callerKey string) (*ModelConfig, error)
```

Owner: 01-core-types.md
Consumers: All call sites updated in 02-call-sites.md

### Contract 4: ResolveForAlias behavior

When `BalancedStickyRequests=true` and `callerKey != ""`:
1. Check if caller has a sticky pin in `health.StickyPins[callerKey]`.
2. If pinned: return the pinned model (do NOT advance rotation).
3. If pinned model is in cooldown: release the pin, fall through to normal rotation.
4. If no pin: assign next available model, record pin, return it.

When `DefaultModel != ""`:
1. After cooldown expires and rotation occurs, check if `CurrentIndex` points to the default model.
2. If so, reset `CurrentIndex` back to the default model's position in the list.
3. Record routing decision with reason `"default_reversion"` instead of `"round_robin"`.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-core-types.md | leaf | none | 35K | A |
| 02 | 02-call-sites.md | leaf | 01 | 40K | B |

**Concurrency groups:** Group A (leaf 01) runs first. Group B (leaf 02) depends on 01.

## Dispatch Protocol

### Phase 1: Dispatch Leaf 01

1. **Read** `01-core-types.md` and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-core-types.md"
   - Context: Full leaf document text + interface contracts from this orchestrator + INLINED source code from `internal/llm/providers.go` (lines 92-97), `internal/llm/models.go` (lines 328-334), `internal/llm/resolver.go` (lines 312-361, 375-424)
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - Include: "Do NOT use read_file on existing source files — explore with search_files or terminal cat instead."

2. **Review** leaf 01 in-session:
   - Verify struct fields compile
   - Verify resolver logic handles both features
   - Run: `go test ./internal/llm/ -run TestResolver -v`

3. **Commit** if review passes:
   ```bash
   git add internal/llm/providers.go internal/llm/models.go internal/llm/resolver.go internal/llm/resolver_test.go
   git commit -m "feat(llm): add default_model and balanced_sticky_requests to aliases"
   ```

### Phase 2: Dispatch Leaf 02

1. **Read** `02-call-sites.md` and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 02-call-sites.md"
   - Context: Full leaf document text + interface contracts + updated signatures from leaf 01 + INLINED call site locations
   - Include: "Do NOT commit. Do NOT run git add."
   - Include: "After leaf 01 is complete, ResolveForAlias has a new callerKey parameter — use it."

2. **Review** leaf 02 in-session:
   - Verify all call sites updated
   - Verify docs updated
   - Run: `go build ./...`

3. **Commit** if review passes:
   ```bash
   git add internal/agent/loop.go internal/agent/intent_analyzer.go internal/agent/llm_classifier.go internal/daemon/components.go internal/tools/builtin/media_client.go docs/configuration/llm.md docs/feature-model-aliases.md
   git commit -m "feat(agent): wire sticky aliases and update docs"
   ```

### Phase 3: Integration Review

1. Run full test suite: `go test ./... -race`
2. Verify no compile errors
3. Check docs for accuracy

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues
- [ ] No debug artifacts: no print/stdout debugging, no TODOs, no placeholder values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source files

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language/Framework:** Go 1.22+
- **Naming:** exported = PascalCase, unexported = camelCase
- **Imports:** grouped stdlib/third-party/local, no unused imports
- **Error handling:** wrap with `%w`, return early, no panic in libs
- **Testing:** table-driven tests, testify require/assert, `_test.go` alongside
- **Formatting tool:** `gofmt` (run before reporting completion)
- **JSON tags:** use `omitempty` for optional fields

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-core-types.md | COMPLETE | 2 | Implemented: ModelAliasEntry fields, AliasHealth.StickyPins, ResolveForAlias with callerKey, sticky logic, default reversion. Tests pass. Committed 4d644e36. |
| 02-call-sites.md | COMPLETE | 1 | Implemented: wired sessionID through agent loops, updated docs. Committed bc4ffb2b. |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

After all children complete:
1. Run: `go test ./internal/llm/ -v -race`
2. Run: `go test ./internal/agent/ -v -race`
3. Run: `go build ./...`
4. Verify: `go vet ./...` passes
5. Check: no compile errors, all alias tests green

## Structural Completeness Check (Before Dispatch)

- `## Dispatch Protocol` ✓
- `## Interface Contracts` ✓
- `## Review Checklist` ✓
- `## Coding Conventions` ✓
- `## Completion Tracking Table` ✓
- `## Integration Test Plan` ✓

## Notes

- Backward compatible: zero-value `DefaultModel=""` and `BalancedStickyRequests=false` preserve current behavior
- Empty `callerKey` disables sticky behavior (used by classifiers/analysts that don't need it)
- Sticky pins are per-alias, keyed by caller string
