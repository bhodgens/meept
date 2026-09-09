# Leaf 01 — Intent Type: quickplan

## DISPATCH INSTRUCTION

> **Implementing agent:** implement ALL tasks below via TDD. Do NOT
> commit. Do NOT run git add. Write code, run tests, report results only.
> Do NOT use read_file on existing source files — explore with
> search_files or `terminal cat` instead; never feed read_file output
> into write_file. After writing a file, do NOT read it back to verify.

- **Parent:** docs/plans/quickplan-mode/master.md
- **Scope:** add the `quickplan` intent type constant and wire it into
  every intent-method switch in internal/agent/intent.go
- **Dependencies:** none (first leaf)
- **Estimated context:** ~45K

## Interface Contract (exposed to siblings)

```go
// internal/agent/intent.go
IntentQuickPlan IntentType = "quickplan"
// SuggestedMode() -> "quick_plan"
// DefaultAgent()  -> "orchestrator"
// Category()      -> CategoryDefer
// RequiresPlanning() -> true
// ShouldCreateTask() -> true
```

Everything downstream (dispatcher, prefilter, docs) consumes ONLY these
method results — no sibling touches the constant directly except through
`IntentQuickPlan`.

## Tasks

### Task 1: Add the constant

File: `internal/agent/intent.go`

Add to the Execution intent group (near `IntentPlan`):

```go
// Execution (async to orchestrator)
IntentCode     IntentType = "code"
IntentDebug    IntentType = "debug"
IntentReview   IntentType = "review"
IntentPlan     IntentType = "plan"
IntentQuickPlan IntentType = "quickplan" // plan+clarify+execute autonomously (adjudication record: docs/plans/classifier-iteration)
IntentGit      IntentType = "git"
IntentSchedule IntentType = "schedule"
```

### Task 2: Wire the five intent-method switches

Same file, each switch gains a case:

1. `SuggestedMode()`:
```go
case IntentQuickPlan:
    return "quick_plan"
```
2. `DefaultAgent()`:
```go
case IntentQuickPlan:
    return "orchestrator"
```
3. `Category()`: add `IntentQuickPlan` to the existing
   `IntentCode, IntentDebug, IntentReview, IntentPlan, IntentGit,
   IntentSchedule, ...` defer case.
4. `RequiresPlanning()`:
```go
case IntentCode, IntentPlan, IntentCompound, IntentQuickPlan:
    return true
```
5. `ShouldCreateTask()`: add `IntentQuickPlan` to the tracked-task case
   (with IntentCode, IntentDebug, ...).

### Task 3: Tests

File: `internal/agent/intent_quickplan_test.go`

Table-driven additions + a dedicated test:

```go
func TestIntentQuickPlan(t *testing.T) {
    it := IntentQuickPlan
    if got := it.SuggestedMode(); got != "quick_plan" {
        t.Errorf("SuggestedMode() = %q, want quick_plan", got)
    }
    if got := it.DefaultAgent(); got != "orchestrator" {
        t.Errorf("DefaultAgent() = %q, want orchestrator", got)
    }
    if got := it.Category(); got != CategoryDefer {
        t.Errorf("Category() = %v, want defer", got)
    }
    if !it.RequiresPlanning() {
        t.Error("RequiresPlanning() = false, want true")
    }
    if !it.ShouldCreateTask() {
        t.Error("ShouldCreateTask() = false, want true")
    }
}
```

Also update the existing DefaultAgent table test (internal/agent/
intent_test.go) with `{IntentQuickPlan, "orchestrator"}`.

### Task 4: Update every exhaustive intent table in the package

`grep -rn "IntentReview" internal/agent/ --include="*.go" | grep -v _test`
— any switch/table treating intents exhaustively (e.g. ambiguity
allowlists, defer-category tables) must gain `IntentQuickPlan` with a
comment referencing the adjudication record. If a table's semantics are
unclear, prefer EXCLUDING quickplan and leave a `// TODO(quickplan)`
note rather than guessing.

## Self-Verification Checklist

- [ ] `go build ./internal/agent/...` passes
- [ ] `go test ./internal/agent/ -run TestIntentQuickPlan -count=1` passes
- [ ] `go test ./internal/agent/ -short -count=1` passes (no table test broke)
- [ ] gofmt clean
- [ ] grep confirms quickplan present in all five method switches
- [ ] No TODOs, no debug prints

## Review Checklist (orchestrator)

- [ ] Constant + 5 switches wired exactly per contract
- [ ] No existing intent behavior changed
- [ ] Test compiles and passes
- [ ] ASCII only, gofmt clean
