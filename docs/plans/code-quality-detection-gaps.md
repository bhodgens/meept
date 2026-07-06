# Code Quality Detection Gaps — Implementation Plan

**Problem:** The codebase review identified 9 bug classes, but only ~40% are detectable by syntactic hooks. The remainder require test discipline, runtime instrumentation, documentation standards, and LLM-assisted verification.

**Goal:** Build a layered defense that catches each bug class *before* merge, with decreasing reliance on human vigilance.

---

## Bug Classes and Recommended Detection

| # | Bug Class | Primary Detection | Secondary Detection | Tertiary |
|---|-----------|-------------------|---------------------|----------|
| 1 | SQLite missing WAL/busy_timeout | ✅ Pre-commit hook | ❌ | ❌ |
| 2 | Channel close-then-nil | ✅ Pre-commit hook | ❌ | ❌ |
| 3 | Predictable ID generation | ✅ Pre-commit hook | ❌ | ❌ |
| 4 | Ignored errors (`_ =`) | ✅ Pre-commit hook | ❌ | ❌ |
| 5 | Mutex + I/O | ✅ `mutexio` analyzer | ⚠️ Race detector | LLM review |
| 6 | TOCTOU / missing serialization | ⚠️ Ownership linter | ✅ Race detector | LLM review |
| 7 | Dead subscriptions | ✅ Runtime instrumentation | ⚠️ Liveness analyzer | Code review |
| 8 | Wrong variable (semantic) | ✅ Unit test expansion | Mutation testing | Contract review |
| 9 | Domain timestamp mismatch | ✅ Spec-to-code verify | Type separation | Adapter pattern |

**Status:** #1, #2, #3, #4 have hooks. #5 has `mutexio`. #6–#9 need implementation.

---

## Implementation Phases

### Phase 1: Runtime Instrumentation (Dead Subscriptions)
**Timeline:** 1–2 days
**Owner:** Cluster/gossip team

#### 1.1: Bus Subscription Tracking
**File:** `internal/bus/bus.go`

Add subscription tracking with optional runtime validation:

```go
type MessageBus struct {
    subs map[string][]chan *BusMessage
    // New: track subscription metadata for debugging
    subMeta map[uintptr]SubMetadata  // key = channel address
}

type SubMetadata struct {
    Topic     string
    CreatedAt time.Time
    Caller    string  // runtime.Caller(2)
}
```

#### 1.2: Publish-Side Warning
**File:** `internal/bus/bus.go`

```go
func (mb *MessageBus) Publish(topic string, msg *BusMessage) {
    mb.mu.RLock()
    subs := mb.subs[topic]

    // Warn if no subscribers
    if len(subs) == 0 {
        slog.Warn("bus: Publish with no subscribers", "topic", topic)
    }

    // Check for full buffers (early warning before "dropped message" logs)
    for _, ch := range subs {
        if len(ch) > cap(ch)*9/10 {
            slog.Warn("bus: subscriber buffer near full",
                "topic", topic, "utilization", float64(len(ch))/float64(cap(ch)))
        }
    }
    // ... rest of publish logic
}
```

#### 1.3: Test-Only Panic Mode
**File:** `internal/bus/bus.go`

```go
var panicOnUndrainedSubscription = false  // set via tests

func SetPanicOnUndrainedSubscription(enabled bool) {
    panicOnUndrainedSubscription = enabled
}

// In Publish():
if panicOnUndrainedSubscription && len(subs) == 0 {
    panic(fmt.Sprintf("bus: Publish(%q) with no subscribers", topic))
}
```

#### 1.4: Test Integration
**File:** `internal/cluster/gossip_test.go`

```go
func TestGossip_SubscriptionDrained(t *testing.T) {
    bus.SetPanicOnUndrainedSubscription(true)
    t.Cleanup(func() { bus.SetPanicOnUndrainedSubscription(false) })

    // Start gossip engine, publish messages
    // If subscription exists but isn't drained, test panics
}
```

**Acceptance criteria:**
- [ ] Bus warns on zero-subscriber publish
- [ ] Bus warns on near-full subscriber buffers
- [ ] Tests can enable panic mode
- [ ] Existing gossip test verifies subscription is drained

---

### Phase 2: Ownership Annotations (TOCTOU / Missing Serialization)
**Timeline:** 2–3 days
**Owner:** Concurrency/cluster team

#### 2.1: Documentation Standard
**File:** `docs/concepts/concurrency.md` (new)

```markdown
# Concurrency and Field Ownership

## Annotation Format

All shared struct fields MUST be annotated with their guarding mechanism:

```go
type GitSync struct {
    mu      sync.Mutex  // guards: running, runCtx, runCancel
    gitMu   sync.Mutex  // guards: all git CLI operations
    cfg     *Config     // immutable after construction
    localCfg *Config    // immutable after construction
}
```

## Access Patterns

### Guarded Fields
```go
// WRONG: accessing guarded field without lock
running := g.running

// RIGHT: snapshot under lock
g.mu.Lock()
running := g.running
g.mu.Unlock()
```

### Per-Entity Serialization
```go
// Pattern: map[K]*sync.Mutex for per-key serialization
type Manager struct {
    invokeMuMap      map[string]*sync.Mutex
    invokeMuMapGuard sync.Mutex
}

func (m *Manager) getMutex(key string) *sync.Mutex {
    m.invokeMuMapGuard.Lock()
    defer m.invokeMuMapGuard.Unlock()

    if m.invokeMuMap == nil {
        m.invokeMuMap = make(map[string]*sync.Mutex)
    }
    mu, ok := m.invokeMuMap[key]
    if !ok {
        mu = &sync.Mutex{}
        m.invokeMuMap[key] = mu
    }
    return mu
}
```
```

#### 2.2: Custom Linter — `fieldguard`
**File:** `tools/analyzers/fieldguard/` (new)

```go
// fieldguard analyzes struct field access and verifies:
// 1. Fields annotated with "// guarded by <mutex>" are only accessed while holding that mutex
// 2. Fields annotated with "// immutable" are only written in constructors
// 3. Unannotated shared fields are flagged for review

package fieldguard

import "golang.org/x/tools/go/analysis"

var Analyzer = &analysis.Analyzer{
    Name: "fieldguard",
    Doc:  "verify struct field access follows ownership annotations",
    Run:  run,
}

// Checks:
// - g.mu.Lock() / g.mu.Unlock() pairs
// - g.mu.RLock() / g.mu.RUnlock() pairs
// - defer mu.Unlock() scope analysis
// - Field access within lock scope
```

**Implementation approach:**
- Parse `// guarded by <mutex>` comments on struct fields
- Build SSA to track lock state at each program point
- Flag field access outside lock scope

#### 2.3: Code Review Checklist
**File:** `.githooks/pre-commit-concurrency` (new hook)

```bash
# Check for "// guarded by" annotations on shared structs
# Flag new struct definitions without annotations
# Not blocking — informational only
```

**Acceptance criteria:**
- [ ] `docs/concepts/concurrency.md` published
- [ ] All `internal/cluster/` structs annotated
- [ ] `fieldguard` analyzer runs in CI (non-blocking initially)
- [ ] Pre-commit hook added (informational)

---

### Phase 3: Test Expansion (Wrong Variable / Semantic Bugs)
**Timeline:** 1–2 days
**Owner:** Agent/tooling team

#### 3.1: AST Rule Engine Tests
**File:** `internal/code/ast/rule_test.go`

Add multi-capture test cases:

```go
func TestRuleExecutor_HasField_MultiCapture(t *testing.T) {
    // Rule with 2 captures: @def:=(function_definition) @name:(identifier)
    // Constraint: has_field on @name (not @def)
    rule := `
    - pattern: "(function_definition name: (identifier) @name)"
      constraints:
        - has_field:
            node: name  # must check 'name' capture, not 'def'
            field: text
    `

    source := `func hello() {}`  // should match
    source2 := `func 123invalid() {}`  // should not match (no text field)

    // Verify constraint checks the right capture
}
```

#### 3.2: Mutation Testing Harness
**File:** `tools/mutation/mutate.go` (new)

```go
// mutation testing: intentionally break code, verify tests fail
package mutation

// Mutators:
// - SwapConstraintNode: changes c.HasField.Node to wrong capture
// - InvertCondition: changes if ok { } to if !ok { }
// - ZeroReturn: changes return value to zero value

func RunMutationTest(t *testing.T, mutateFn func(), testFn func() error) {
    // 1. Run original test — must pass
    // 2. Apply mutation
    // 3. Run test — must fail
    // 4. If test passes with mutation, test is insufficient
}
```

#### 3.3: Pre-Commit Mutation Check
**File:** `.githooks/pre-commit-mutation` (new)

```bash
# For files with *_test.go changes, run mutation testing on affected functions
# Flag any mutations that don't cause test failures
# Not blocking initially — generates report
```

**Acceptance criteria:**
- [ ] Multi-capture AST tests added
- [ ] Mutation harness functional
- [ ] At least 5 mutation variants tested
- [ ] Mutation test report generated in CI

---

### Phase 4: Spec-to-Code Verification (Domain Timestamp Mismatch)
**Timeline:** Ongoing (per-feature)
**Owner:** Feature teams + LLM subagents

#### 4.1: Spec Annotation Standard
**File:** `docs/workflows/` (existing — update templates)

All feature specs MUST include:

```markdown
## Data Model

| Field | Type | Meaning | Source |
|-------|------|---------|--------|
| `LastAssessed` | `time.Time` | When goal health was last computed | `goal.go:173` |
| `Plan.CreatedAt` | `time.Time` | When plan was submitted for approval | `plan.go:51` |

## Lifecycle States

1. Plan created → State: `draft`
2. Plan submitted → State: `pending_approval` (timestamp: `CreatedAt`)
3. Plan approved → State: `approved`
4. Plan rejected → State: `rejected`

## Timeout Semantics

**Approval timeout (spec line 591):** Plan pending > N hours → auto-reject
- Measurement: time since `Plan.CreatedAt` (submission), NOT `LastAssessed`
- Rationale: `LastAssessed` is goal health, unrelated to plan submission
```

#### 4.2: LLM Verification Skill
**File:**技能: `verify-plan-against-code` (existing — enhance)

Add timestamp semantic verification:

```markdown
## Verification Checklist

For each timeout/deadline in the spec:
1. Identify the timestamp field mentioned (or implied)
2. Search codebase for timeout implementation
3. Verify code uses the CORRECT timestamp (semantic match, not just type match)
4. Flag any_proxy timestamps (e.g., using `LastAssessed` for plan age)

## Output Format

✅ PASS: `scheduler_jobs.go:350` uses `PlanCreatedAt` via `PlanLookup`
❌ FAIL: `scheduler_jobs.go:350` uses `g.LastAssessed` (goal health, not plan age)
```

#### 4.3: Adapter Pattern Template
**File:** `internal/daemon/employee_service_adapter.go` (existing pattern)

Document the pattern for future use:

```go
// Pattern: When a sweeper needs field X from struct Y, but
// sweeper is in package A and Y is in package B (import cycle risk):
//
// 1. Define interface in package A:
//    type YLookup interface { YField(ctx, id) (T, error) }
//
// 2. Implement adapter in package C (no cycle):
//    type yLookupAdapter struct{ y *B.YManager }
//
// 3. Inject via setter:
//    manager.SetYLookup(&yLookupAdapter{pm: c.YManager})
```

**Acceptance criteria:**
- [ ] Spec template updated with timestamp semantics table
- [ ] `verify-plan-against-code` skill enhanced for timestamp checks
- [ ] Adapter pattern documented
- [ ] At least 1 new feature uses the full workflow

---

### Phase 5: CI/CD Integration
**Timeline:** 1 day
**Owner:** DevOps team

#### 5.1: GitHub Actions Workflow
**File:** `.github/workflows/code-quality.yml` (new)

```yaml
name: Code Quality Detection

on: [push, pull_request]

jobs:
  hooks:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run pre-commit hooks
        run: |
          git config core.hooksPath .githooks
          .githooks/pre-commit

  analyzers:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install analyzers
        run: |
          go install honnef.co/go/tools/cmd/staticcheck@latest
          go install github.com/securego/gosec/v2/cmd/gosec@latest
          go build -o bin/mutexio ./tools/analyzers/mutexio
          go build -o bin/fieldguard ./tools/analyzers/fieldguard
      - name: Run mutexio
        run: ./bin/mutexio ./...
      - name: Run fieldguard (non-blocking)
        run: ./bin/fieldguard ./... || true

  race-detector:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Test with race detector
        run: go test -race -count=1 ./internal/cluster/ ./internal/queue/ ./internal/bus/

  runtime-instrumentation:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Test with bus panic mode
        run: go test -run TestGossip_SubscriptionDrained ./internal/cluster/
```

#### 5.2: Pre-Commit Hook Updates
**File:** `.githooks/pre-commit` (update numbering + add new hooks)

Already updated with hooks #9 (sqlite-pragmas) and #10 (channel-nilafterclose).

Add Phase 2-3 hooks:
- `pre-commit-concurrency` (informational)
- `pre-commit-mutation` (report-only)

**Acceptance criteria:**
- [ ] GitHub Actions workflow runs all checks
- [ ] Pre-commit hooks updated
- [ ] CI status visible on PRs

---

## Summary Timeline

| Phase | Deliverables | Duration |
|-------|--------------|----------|
| 1: Runtime Instrumentation | Bus tracking, warn/panic modes, tests | 1–2 days |
| 2: Ownership Annotations | Concurrency doc, `fieldguard` analyzer, checklist | 2–3 days |
| 3: Test Expansion | Multi-capture tests, mutation harness | 1–2 days |
| 4: Spec-to-Code | Spec templates, LLM verification, adapters | Ongoing (per-feature) |
| 5: CI/CD Integration | GitHub Actions, hook updates | 1 day |

**Total:** 5–8 days initial implementation, then ongoing per-feature verification.

---

## Success Metrics

| Metric | Baseline | Target |
|--------|----------|--------|
| Bugs caught by hooks | 4/9 classes | 6/9 classes |
| Bugs caught by analyzers | 1/9 classes | 3/9 classes |
| Bugs caught by tests | 2/9 classes | 5/9 classes |
| Bugs caught by LLM review | 4/9 classes | 6/9 classes |
| False positive rate | N/A | <5% |

**Measurement:** Track bug class detection source for 90 days post-implementation. Adjust hooks/analyzers based on false positives and misses.
