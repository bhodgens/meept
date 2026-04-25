# Dispatcher Enhancement Remediation Plan

**Status:** Proposed
**Priority:** High
**Depends on:** Phases 1-6 (all completed)
**Related:** `plan-dispatcher-enhancement-phase[1-6].md`

---

## Context

The dispatcher enhancement phases 1-6 implemented analytics, intent taxonomy, multi-intent detection, semantic matching, context-aware classification, and integration improvements. A comprehensive 10-agent review against the plan documents uncovered **2 critical bugs**, **10 important gaps**, **9 dead code items**, **3 performance issues**, and **zero test coverage** for 5 of the 6 phases. This plan rectifies all shortcomings in 5 sprints.

---

## Sprint 1: Critical Fixes

### 1A. Fix SessionTracker data race

**File:** `internal/agent/session_tracker.go`

Three race conditions:
1. `GetSession()` (line 48) calls `cleanupExpired()` (line 122) which does `delete(t.sessions, id)` while holding only `RLock`
2. `GetSession()` returns a `*SessionState` pointer; callers (`GetDominantIntent`, `GetLastIntent`, `GetLastAgent`, `GetIntentCounts`) read mutable fields after the lock is released
3. `RecordIntent()` (line 38) appends to `state.IntentHistory` while a concurrent reader iterates it

**Changes:**
- Remove `cleanupExpired()` call from `GetSession()` (line 51)
- Add exported `Cleanup()` method: acquires full `Lock()`, calls `cleanupExpired()`
- Rewrite `GetSession()`: pure read lookup under `RLock`, no cleanup
- Rewrite `GetDominantIntent`, `GetLastIntent`, `GetLastAgent`, `GetIntentCounts`: each acquires `RLock` independently, copies data, releases before computing
- `GetLastIntent` returns a copy of the intent (not pointer to shared state)
- Start a background cleanup goroutine in `NewDispatcher` calling `Cleanup()` every `maxAge/2`

### 1B. Wire LLM client to Dispatcher

**File:** `internal/daemon/components.go` (line 579-588)

The `Components` struct has `LLMClient *llm.Client` (line 44) but it's never passed to `DispatcherConfig`. The config has `MultiAgent.ClassifierModel` available.

**Changes:**
```go
c.Dispatcher = agent.NewDispatcher(agent.DispatcherConfig{
    // ... existing 7 fields ...
    LLMClient:       c.LLMClient,                         // ADD
    ClassifierModel: c.Config.MultiAgent.ClassifierModel,  // ADD
    SessionMaxAge:   30 * time.Minute,                     // ADD
})
```

**Note:** `EmbeddingClient` wiring is deferred — no embedding client exists on `Components` yet. Creating one requires config + API key plumbing from `MemoryConfig.Embeddings`. This is a separate effort.

### 1C. Remove custom `min()` function

**File:** `internal/agent/dispatcher.go` (lines 898-904)

Go 1.21+ has built-in `min`. The custom float64 version shadows it. Remove it; the call at line 859 works with the builtin.

### Verification
- `go test ./internal/agent/... -race -v` — no race conditions
- `go build ./cmd/meept-daemon` — compiles
- Debug log shows LLM classifier is active when LLM is configured

---

## Sprint 2: Compound Routing Fixes

### 2A. Add `IntentCompound` constant

**File:** `internal/agent/intent.go`

Add constant:
```go
IntentCompound IntentType = "compound"
```

Wire into all methods:
- `Category()` → `CategoryDefer`
- `DefaultAgent()` → `"orchestrator"`
- `RequiresPlanning()` → `true`
- `ShouldCreateTask()` → `true`
- `ShouldDispatchAsync()` → `true`
- `IsValidIntentType()` → include `IntentCompound`
- `Keywords()` → `[]string{"and also", "as well as", "plus"}`

### 2B. Fix routeCompound to store intent breakdown

**File:** `internal/agent/dispatcher.go` (lines 451-494)

Currently only stores count. Add individual intent types to task metadata:
```go
intentTypes := make([]string, 0, len(multi.Intents))
for _, intent := range multi.Intents {
    intentTypes = append(intentTypes, intent.Type)
}
```

Cap `multi.Intents` at 5 entries before processing (plan risk mitigation).

### 2C. Fix ShouldDispatchAsync for compound

**File:** `internal/agent/dispatcher.go` (lines 1107-1123)

With `IntentCompound.ShouldDispatchAsync()` returning `true`, compound requests will take the async path in `handler.go:338-349`, calling `publishPlanRequest`. This routes through the bus to the orchestrator — correct behavior.

### 2D. Populate compound fields in publishPlanRequest

**File:** `internal/agent/handler.go` (lines 394-422)

After creating `PlanRequest`, check if compound:
```go
if result.Intent.Type == string(agent.IntentCompound) {
    req.IsCompound = true
    if ct, ok := result.Task.Metadata["compound_type"]; ok {
        req.CompoundType = string(ct)
    }
}
```

### 2E. Fix formatAsyncTaskAck for compound

**File:** `internal/agent/handler.go` (lines 663-679)

Replace the `"simple"` check (never produced by any code path) with compound handling:
```go
status := "planning steps..."
if result.Intent.Type == string(agent.IntentCompound) {
    status = "coordinating multiple tasks..."
}
```

### Verification
- `IntentCompound` is recognized by `IsValidIntentType`
- Compound requests route through async bus path (not `RouteToAgent`)
- Task metadata includes individual intent types
- `PlanRequest` has `IsCompound=true` for compound requests

---

## Sprint 3: Code Quality

### 3A. Dead code removal

| Item | File | Lines | Action |
|------|------|-------|--------|
| `classifiers []IntentClassifier` field | `dispatcher.go` | 67, 116, 125 | Remove field and append lines |
| `GetSkillExecutor()` | `dispatcher.go` | 1175 | Remove |
| `GetCapabilityMatcher()` | `dispatcher.go` | 1180 | Remove |
| `SetCapabilityMatcher()` | `dispatcher.go` | 1185 | Remove |
| `multiIntentResponse` struct | `llm_classifier.go` | 131-137 | Remove |
| `CategoryTrackable` constant | `intent.go` | 37 | Remove |

Keep `handleStatsQuery` (wired in Sprint 4) and `GetFallbackDetails` (used by stats RPC).

### 3B. ClassifyAll uses IntentType constants

**File:** `internal/agent/dispatcher.go` (lines 768-809)

Replace raw string literals (`"platform"`, `"report"`, etc.) with `string(IntentPlatform)`, `string(IntentReport)`, etc. to match `Classify()`.

### 3C. Pre-compile anaphora regex

**File:** `internal/agent/dispatcher.go` (line 889)

Add package-level:
```go
var anaphoraForRegex = regexp.MustCompile(`do the same for (.+)`)
```
Replace `regexp.MustCompile` inside `resolveAnaphora` with `anaphoraForRegex`.

### 3D. Fix keyword classifier confidence

**File:** `internal/agent/dispatcher.go` (lines 738-764)

`bestScore` is computed but never used — `p.confidence` is always returned. Use the computed score (clamped to [0, 1]):
```go
adjustedConfidence := math.Min(score, 1.0)
bestMatch = &Intent{
    Confidence: adjustedConfidence,
    // ...
}
```

### 3E. Add `recordCompoundDispatch` to stats

**File:** `internal/agent/dispatcher.go`

Add method and call from `routeCompound`:
```go
func (d *Dispatcher) recordCompoundDispatch(intentCount int) {
    d.recordClassificationMethod("compound")
    d.recordAgent("orchestrator")
    d.recordIntentType(string(IntentCompound))
}
```

### Verification
- `go vet ./internal/agent/...` clean
- `go build ./...` compiles
- No unused imports or variables

---

## Sprint 4: Stats Accessibility

### 4A. Wire dispatcher stats into platform query

**File:** `internal/agent/dispatcher.go`

Wire `handleStatsQuery()` (line 612) into `handlePlatformIntrospection` so platform queries like "show me dispatcher stats" route to it:
```go
if strings.Contains(lower, "dispatcher stats") || strings.Contains(lower, "routing stats") {
    return d.handleStatsQuery(ctx)
}
```

### 4B. Expose stats via bus/RPC

**File:** `internal/daemon/components.go`

Register a bus handler responding to `dispatcher.stats` requests using `c.Dispatcher.GetStats()`. Wire into the daemon's bus handler registration phase.

### Verification
- Platform query "dispatcher stats" returns JSON with classification counts
- Fallback details are accessible

---

## Sprint 5: Test Coverage

### 5A. `internal/agent/intent_test.go` (NEW)

Table-driven tests for all IntentType methods:
- `TestIntentCategory` — verify each constant returns expected category
- `TestIntentDefaultAgent` — verify each constant returns expected agent
- `TestIntentRequiresPlanning` — verify true for Code/Plan/Compound
- `TestIntentShouldCreateTask` — verify correct intents
- `TestIntentShouldDispatchAsync` — verify including schedule conditional
- `TestIsValidIntentType` — all known types true, unknowns false
- `TestIntentKeywords` — verify non-nil for all types

### 5B. `internal/agent/embedding_test.go` (NEW)

- `TestCosineSimilarity` — identical vectors (1.0), orthogonal (0.0), opposite (-1.0), mismatched lengths (0), empty (0)

### 5C. `internal/agent/intent_index_test.go` (NEW)

- `TestBuildIndex` — mock EmbeddingClient, verify entries populated
- `TestMatch` — verify best match selected above threshold
- `TestMatchBelowThreshold` — verify nil below minConfidence
- `TestMatchUnready` — verify nil when index not built

### 5D. `internal/agent/session_tracker_test.go` (NEW)

- `TestRecordIntent` — history grows, caps at 20
- `TestGetDominantIntent` — most frequent wins
- `TestGetLastIntent` — returns last intent copy
- `TestGetLastAgent` — returns last agent type
- `TestGetIntentCounts` — correct counts
- `TestCleanup` — expired sessions removed
- `TestConcurrentAccess` — 50 goroutines calling RecordIntent + read methods simultaneously

### 5E. Extend `internal/agent/dispatcher_test.go`

- `TestRecordMethods` — verify ByMethod, ByAgent, ByIntent increment correctly
- `TestGetStats` — verify returned copy, nil safety
- `TestGetFallbackDetails` — verify limit and truncation
- `TestDetectCompound` — single (false), multi-sequential, multi-parallel
- `TestDeduplicateIntents` — overlapping types, all unique, empty
- `TestClassifyAll` — multi-keyword input returns multiple matches

### 5F. `internal/agent/context_test.go` (NEW)

- `TestApplyContextWeighting` — same-intent +0.15, same-agent +0.1, frequency +0.05*count, anaphora +0.2, cap at 0.3
- `TestHasAnaphora` — positive and negative examples
- `TestResolveAnaphora` — "do the same for X" expansion, no-match passthrough

### Verification
- `go test ./internal/agent/... -v` — all pass
- `go test ./internal/agent/... -race` — no races
- `go test ./internal/agent/... -cover` — target >70% on dispatcher files

---

## Dependency Graph

```
Sprint 1 (Critical) — no dependencies
  1A: session_tracker.go
  1B: components.go
  1C: dispatcher.go (min removal)

Sprint 2 (Compound) — depends on Sprint 1
  2A: intent.go (IntentCompound)
  2B: dispatcher.go (routeCompound metadata + cap)
  2C: dispatcher.go (ShouldDispatchAsync — auto-fixed by 2A)
  2D: handler.go (publishPlanRequest)
  2E: handler.go (formatAsyncTaskAck)

Sprint 3 (Quality) — depends on Sprint 2 for IntentCompound
  3A: dead code removal
  3B: ClassifyAll constants
  3C: regex precompile
  3D: confidence fix
  3E: recordCompoundDispatch

Sprint 4 (Stats) — depends on Sprint 1
  4A: platform query wiring
  4B: bus/RPC exposure

Sprint 5 (Tests) — depends on all prior sprints
  5A-5F: all test files
```

## Files Modified Summary

| File | Sprints | Changes |
|------|---------|---------|
| `internal/agent/session_tracker.go` | 1A | Fix race: separate cleanup, copy-on-read |
| `internal/daemon/components.go` | 1B, 4B | Wire LLMClient/ClassifierModel, stats bus |
| `internal/agent/dispatcher.go` | 1C, 2B, 2C, 3A-3E | Remove min(), fix compound, dead code, regex, confidence |
| `internal/agent/intent.go` | 2A, 3A | Add IntentCompound, remove CategoryTrackable |
| `internal/agent/handler.go` | 2D, 2E | Populate compound fields, fix formatAsyncTaskAck |
| `internal/agent/llm_classifier.go` | 3A | Remove multiIntentResponse |
| `internal/agent/intent_test.go` | 5A | NEW |
| `internal/agent/embedding_test.go` | 5B | NEW |
| `internal/agent/intent_index_test.go` | 5C | NEW |
| `internal/agent/session_tracker_test.go` | 5D | NEW |
| `internal/agent/dispatcher_test.go` | 5E | EXTEND |
| `internal/agent/context_test.go` | 5F | NEW |

## End-to-End Verification

```bash
# Build
go build ./cmd/meept-daemon
go build ./cmd/meept

# Tests (no races)
go test ./internal/agent/... -race -v

# Coverage
go test ./internal/agent/... -coverprofile=coverage.out
go tool cover -func=coverage.out | grep -E "dispatcher|intent|session_tracker|embedding|intent_index"

# Vet
go vet ./...

# Full suite
go test ./... -v
```
