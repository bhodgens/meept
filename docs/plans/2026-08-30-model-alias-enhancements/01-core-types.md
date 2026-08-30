# Core Types and Resolver - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../../master.md
- **Scope:** Add default_model and balanced_sticky_requests to ModelAliasEntry, update AliasHealth and ResolveForAlias
- **Dependencies:** none
- **Estimated Context:** 35K (exploration + generation + iteration + overhead)
- **Concurrency Group:** A

## Goal

Extend the model alias system with two new configuration options:
1. `default_model` — revert to a specific model after cooldown expires
2. `balanced_sticky_requests` — pin each caller to a single model within the alias

This requires changes to three structs/types and the resolver logic.

## Context

Key files:
- `internal/llm/providers.go` — ModelAliasEntry struct definition
- `internal/llm/models.go` — AliasHealth struct definition  
- `internal/llm/resolver.go` — ResolveForAlias implementation

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/llm/providers.go:93
type ModelAliasEntry struct {
    Models                 []string `json:"models"`
    Timeout                int      `json:"timeout"`
    MaxFails               int      `json:"max_fails"`
    DefaultModel           string   `json:"default_model,omitempty"`
    BalancedStickyRequests bool     `json:"balanced_sticky_requests,omitempty"`
}

// File: internal/llm/models.go:329
type AliasHealth struct {
    CurrentIndex     int
    ConsecutiveFails int
    LastFailure      time.Time
    CooldownUntil    time.Time
    StickyPins       map[string]int
}

// File: internal/llm/resolver.go:314
func (r *Resolver) ResolveForAlias(aliasName string, callerKey string) (*ModelConfig, error)
```

### What This Leaf Consumes

```go
// From internal/llm/resolver.go
type Resolver struct {
    aliases       map[string]*AliasEntry
    health        map[string]*AliasHealth
    mu            sync.Mutex
}

func (r *Resolver) getOrCreateHealth(aliasName string) *AliasHealth
```

## Tasks

### Task 1: Extend ModelAliasEntry struct

**Objective:** Add DefaultModel and BalancedStickyRequests fields

**Files:**
- Modify: `internal/llm/providers.go:93-97`

**Step 1: Write failing test**

Create `internal/llm/providers_test.go`:
```go
func TestModelAliasEntry_DefaultModel(t *testing.T) {
    alias := ModelAliasEntry{
        Models:           []string{"zai/glm-4.7", "ollama/llama3.2"},
        Timeout:          30,
        MaxFails:         3,
        DefaultModel:     "zai/glm-4.7",
        BalancedStickyRequests: false,
    }
    assert.Equal(t, "zai/glm-4.7", alias.DefaultModel)
    assert.False(t, alias.BalancedStickyRequests)
}

func TestModelAliasEntry_StickyRequests(t *testing.T) {
    alias := ModelAliasEntry{
        Models:                 []string{"zai/glm-4.7", "ollama/llama3.2"},
        Timeout:                30,
        MaxFails:               3,
        BalancedStickyRequests: true,
    }
    assert.True(t, alias.BalancedStickyRequests)
    assert.Empty(t, alias.DefaultModel)
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestModelAliasEntry -v`
Expected: FAIL - field not found

**Step 3: Update struct definition**

In `internal/llm/providers.go`, change lines 93-97 to:
```go
type ModelAliasEntry struct {
    Models                 []string `json:"models"`
    Timeout                int      `json:"timeout"`
    MaxFails               int      `json:"max_fails"`
    DefaultModel           string   `json:"default_model,omitempty"`
    BalancedStickyRequests bool     `json:"balanced_sticky_requests,omitempty"`
}
```

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestModelAliasEntry -v`
Expected: PASS

### Task 2: Extend AliasHealth struct

**Objective:** Add StickyPins map for tracking per-caller pins

**Files:**
- Modify: `internal/llm/models.go:329-334`

**Step 1: Write failing test**

Create `internal/llm/models_test.go`:
```go
func TestAliasHealth_StickyPins(t *testing.T) {
    health := &AliasHealth{
        CurrentIndex: 0,
        StickyPins:   make(map[string]int),
    }
    health.StickyPins["session-abc"] = 1
    assert.Equal(t, 1, health.StickyPins["session-abc"])
}

func TestAliasHealth_StickyPinsNilByDefault(t *testing.T) {
    health := &AliasHealth{CurrentIndex: 0}
    assert.Nil(t, health.StickyPins)
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestAliasHealth -v`
Expected: FAIL - field not found

**Step 3: Update struct definition**

In `internal/llm/models.go`, change lines 329-334 to:
```go
type AliasHealth struct {
    CurrentIndex     int
    ConsecutiveFails int
    LastFailure      time.Time
    CooldownUntil    time.Time
    StickyPins       map[string]int
}
```

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestAliasHealth -v`
Expected: PASS

### Task 3: Update ResolveForAlias signature

**Objective:** Add callerKey parameter to ResolveForAlias

**Files:**
- Modify: `internal/llm/resolver.go:314`

**Step 1: Write failing test**

Create `internal/llm/resolver_caller_test.go`:
```go
func TestResolveForAlias_WithCallerKey(t *testing.T) {
    cfg := createTestConfig()
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
    resolver := NewResolver(cfg, logger)
    
    // Should accept caller key without error
    _, err := resolver.ResolveForAlias("classifier", "session-1")
    assert.NoError(t, err)
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestResolveForAlias_WithCallerKey -v`
Expected: FAIL - too many arguments

**Step 3: Update function signature**

In `internal/llm/resolver.go:314`, change:
```go
func (r *Resolver) ResolveForAlias(aliasName string) (*ModelConfig, error) {
```
to:
```go
func (r *Resolver) ResolveForAlias(aliasName string, callerKey string) (*ModelConfig, error) {
```

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestResolveForAlias_WithCallerKey -v`
Expected: PASS (but other tests will fail — that's expected, they'll be fixed in leaf 2)

### Task 4: Implement sticky request logic

**Objective:** Add caller pinning when BalancedStickyRequests is true

**Files:**
- Modify: `internal/llm/resolver.go:314-361`

**Step 1: Write failing test**

In `internal/llm/resolver_test.go`, add:
```go
func TestResolveForAlias_StickyRequests(t *testing.T) {
    cfg := createTestConfig()
    // Add sticky alias
    cfg.ModelAliases["sticky"] = ModelAliasEntry{
        Models:                 []string{"a", "b", "c"},
        Timeout:                30,
        MaxFails:               3,
        BalancedStickyRequests: true,
    }
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
    resolver := NewResolver(cfg, logger)
    
    // First call for session-1 should pin to model 0
    m1, err := resolver.ResolveForAlias("sticky", "session-1")
    assert.NoError(t, err)
    
    // Second call for session-1 should return SAME model (sticky)
    m2, err := resolver.ResolveForAlias("sticky", "session-1")
    assert.NoError(t, err)
    assert.Equal(t, m1.ModelID, m2.ModelID)
    
    // Call for session-2 should get DIFFERENT model
    m3, err := resolver.ResolveForAlias("sticky", "session-2")
    assert.NoError(t, err)
    assert.NotEqual(t, m1.ModelID, m3.ModelID)
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestResolveForAlias_StickyRequests -v`
Expected: FAIL

**Step 3: Implement sticky logic**

In `internal/llm/resolver.go`, after the cooldown check (around line 341), add sticky logic:
```go
// Handle sticky requests
if alias.BalancedStickyRequests && callerKey != "" {
    if health.StickyPins == nil {
        health.StickyPins = make(map[string]int)
    }
    
    // Check if caller has a pin
    if pinnedIdx, ok := health.StickyPins[callerKey]; ok {
        // Verify pinned model is still available
        if pinnedIdx < len(alias.Models) {
            // Check if pinned model is in cooldown
            if !health.CooldownUntil.IsZero() && now.Before(health.CooldownUntil) {
                // Pinned model in cooldown, release pin
                delete(health.StickyPins, callerKey)
            } else {
                // Return pinned model
                chosen := alias.Models[pinnedIdx]
                // Log decision
                if r.routingLogger != nil {
                    decision := RoutingDecision{
                        ChosenModelID:    chosen.ModelID,
                        ChosenProviderID: chosen.ProviderID,
                        Alias:            aliasName,
                        Reason:           "sticky_request",
                    }
                    _ = r.routingLogger.Record(context.Background(), decision)
                }
                return chosen, nil
            }
        }
    }
    
    // No pin or pin released, assign new pin
    nextIdx := health.CurrentIndex
    if nextIdx < len(alias.Models) {
        health.StickyPins[callerKey] = nextIdx
        health.CurrentIndex = (nextIdx + 1) % len(alias.Models)
        
        chosen := alias.Models[nextIdx]
        if r.routingLogger != nil {
            decision := RoutingDecision{
                ChosenModelID:    chosen.ModelID,
                ChosenProviderID: chosen.ProviderID,
                Alias:            aliasName,
                Reason:           "sticky_request_new",
            }
            _ = r.routingLogger.Record(context.Background(), decision)
        }
        return chosen, nil
    }
}
```

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestResolveForAlias_StickyRequests -v`
Expected: PASS

### Task 5: Implement default model reversion

**Objective:** Revert to default model after cooldown expires

**Files:**
- Modify: `internal/llm/resolver.go:314-361`

**Step 1: Write failing test**

In `internal/llm/resolver_test.go`, add:
```go
func TestResolveForAlias_DefaultModelReversion(t *testing.T) {
    cfg := createTestConfig()
    // Add alias with default model
    cfg.ModelAliases["with-default"] = ModelAliasEntry{
        Models:   []string{"b", "a", "c"},
        Timeout:  1, // 1 second for quick test
        MaxFails: 3,
        DefaultModel: "a",
    }
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
    resolver := NewResolver(cfg, logger)
    
    // Simulate failure to trigger cooldown
    resolver.RecordAliasFailure("with-default", nil)
    
    // Wait for cooldown to expire
    time.Sleep(100 * time.Millisecond)
    
    // After cooldown, should revert to default model "a"
    m, err := resolver.ResolveForAlias("with-default", "")
    assert.NoError(t, err)
    assert.Equal(t, "a", m.ModelID)
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestResolveForAlias_DefaultModelReversion -v`
Expected: FAIL

**Step 3: Implement default model reversion**

In `internal/llm/resolver.go`, after the rotation logic (around line 341), add:
```go
// Handle default model reversion
if alias.DefaultModel != "" && !health.CooldownUntil.IsZero() && now.After(health.CooldownUntil) {
    // Find default model index
    defaultIdx := -1
    for i, m := range alias.Models {
        if m == alias.DefaultModel {
            defaultIdx = i
            break
        }
    }
    
    if defaultIdx >= 0 && health.CurrentIndex != defaultIdx {
        // Revert to default model
        health.CurrentIndex = defaultIdx
        health.ConsecutiveFails = 0
        health.CooldownUntil = time.Time{}
    }
}
```

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestResolveForAlias_DefaultModelReversion -v`
Expected: PASS

### Task 6: Update existing tests

**Objective:** Fix all existing test calls to ResolveForAlias to pass empty callerKey

**Files:**
- Modify: `internal/llm/resolver_test.go` (all calls)
- Modify: `internal/llm/resolver.go` (any internal calls)

**Step 1: Find all ResolveForAlias calls**

Run: `grep -n "ResolveForAlias" internal/llm/resolver_test.go`

**Step 2: Update all test calls**

Replace all `resolver.ResolveForAlias("alias")` with `resolver.ResolveForAlias("alias", "")`

**Step 3: Run all tests**

Run: `go test ./internal/llm/ -v`
Expected: All tests PASS

### Task 7: Run full build and vet

**Objective:** Verify code compiles and passes static analysis

**Step 1: Build**

Run: `go build ./...`
Expected: No errors

**Step 2: Run vet**

Run: `go vet ./internal/llm/...`
Expected: No issues

**Step 3: Run race detector**

Run: `go test ./internal/llm/ -race -v`
Expected: All tests pass

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] `go test ./internal/llm/ -v` passes
- [ ] `go test ./internal/llm/ -race -v` passes
- [ ] `go build ./...` succeeds

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, file paths)
- [ ] Code follows project conventions (naming, error handling, structure)
- [ ] No bugs, no security issues
- [ ] No scope creep beyond specified tasks
- [ ] `go test ./internal/llm/ -race` passes

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Empty `callerKey` disables sticky behavior
- `DefaultModel` should match one of the models in the `Models` slice
- Sticky pins are per-alias, keyed by caller string
- When a pinned model enters cooldown, the pin is automatically released
