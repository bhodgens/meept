# Filter Interface + Chain - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify - write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Build the `OutputFilter` interface, tri-state `FilterResult`, and the capped, logged, idempotent `FilterChain` in `internal/validator`.
- **Dependencies:** none (first leaf in the tree)
- **Estimated Context:** 45K (exploration 8K + generation 15K + iteration 10K + overhead 12K)
- **Concurrency Group:** A

## Goal

Create the type foundation every other leaf builds on: the tri-state filter
result (pass / rewrite / fail), the `OutputFilter` interface, and the
`FilterChain` executor with frozen sweep/convergence/logging semantics. No
concrete filters and no daemon wiring in this leaf - types and executor only.

## Context

`internal/validator` today validates tool-execution EVIDENCE (did the claimed
side-effect happen) via the `Validator` interface and `ValidatorManager`
(`internal/validator/interface.go`, `manager.go`). This leaf adds a parallel
concept: validation/repair of step RESULT CONTENT. Same package, separate
files, separate interfaces - the manager may coexist but the concepts must not
conflate.

Key files to understand before implementing:
- `internal/validator/interface.go` - existing Validator pattern; follow its doc-comment style and the `//nolint:revive` convention for stuttering names
- `internal/validator/manager.go` - RWMutex collect-under-lock pattern; the existing ValidateStep error-joining style
- `internal/task/step.go` - TaskStep shape (only read for the `Applies`/`Process` signatures; do NOT modify in this leaf)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/validator/filter.go
package validator

type FilterOutcome int

const (
    FilterPass    FilterOutcome = iota
    FilterRewrite
    FilterFail
)

type FilterResult struct {
    Outcome FilterOutcome
    Filter  string // stage name, ALWAYS set
    Output  string // set iff Outcome == FilterRewrite
    Reason  string // machine-readable, set iff Outcome == FilterFail
}

type OutputFilter interface {
    Name() string
    Applies(step *task.TaskStep) bool
    Process(ctx context.Context, step *task.TaskStep, output string) FilterResult
}

type FilterChainConfig struct {
    Filters   []OutputFilter
    MaxPasses int
}

type ChainResult struct {
    Output   string
    Rejected *FilterResult
    Passes   int
    Actions  []string // "pass=<n> filter=<name> action=pass|rewrite|fail"
}

func NewFilterChain(filters []OutputFilter, maxPasses int) *FilterChain
func (fc *FilterChain) Run(ctx context.Context, step *task.TaskStep, output string) ChainResult
```

Frozen semantics (parent Contracts 1-2, verbatim binding):
1. One PASS = one sweep over all filters in declared order.
2. `FilterFail` terminates the chain immediately.
3. `FilterRewrite` updates the output; the sweep re-runs from the FIRST
   filter with the new output.
4. Non-convergence: if `Passes` reaches `MaxPasses` and output still changes,
   return `FilterFail` with reason `filter chain did not converge after N passes`.
5. Filters where `Applies` is false are skipped, do not count as a pass, and
   do not appear in Actions.
6. Every applied filter appends exactly one Actions line per sweep.
7. A filter MUST NOT mutate the step. `Process` must be idempotent for
   rewrite stages.

### What This Leaf Consumes

```
// From internal/task (existing, read-only in this leaf):
type TaskStep struct { ... }  // step identity + Result fields
```

## Tasks

### Task 1: FilterResult and OutputFilter types

**Objective:** Define the tri-state types with validation of invariants.

**Files:**
- Create: `internal/validator/filter.go`
- Test: `internal/validator/filter_test.go`

**Step 1: Write failing test**

```go
func TestFilterResultInvariants(t *testing.T) {
    // Rewrite must carry Output; Fail must carry Reason; Pass carries neither.
    r := FilterResult{Outcome: FilterRewrite, Filter: "json_format", Output: "{}"}
    if r.Output == "" || r.Reason != "" { t.Fatal("rewrite must set Output only") }
    f := FilterResult{Outcome: FilterFail, Filter: "language_en", Reason: "lang=de confidence=0.97"}
    if f.Reason == "" || f.Output != "" { t.Fatal("fail must set Reason only") }
    p := FilterResult{Outcome: FilterPass, Filter: "lint_go"}
    if p.Output != "" || p.Reason != "" { t.Fatal("pass must set neither") }
}
```

**Step 2: Run test to verify failure**

Run: `go test -p 2 ./internal/validator/ -run TestFilterResultInvariants -v`
Expected: FAIL - FilterOutcome undefined

**Step 3: Write minimal implementation** (types above; also add a
`(r FilterResult) Valid() bool` helper that checks the invariants)

**Step 4: Run test to verify pass** - same command, expected PASS

### Task 2: FilterChain sweep + fail-fast

**Objective:** Ordered sweep, immediate termination on fail, Applies skipping.

**Files:**
- Modify: `internal/validator/filter.go`
- Test: `internal/validator/filter_test.go`

**Step 1: Write failing test** - table-driven: a fake filter set
(stub filters defined in the test file) proving:
- order is declaration order (record invocation sequence)
- fail on filter 2 of 3 stops filter 3 (invocation log proves it)
- Applies=false filters never run and never appear in Actions

**Step 2: Run to verify failure** (`-run TestFilterChainSweep`)
**Step 3: Implement** the sweep loop per frozen semantics 1, 2, 5, 6.
**Step 4: Run to verify pass.**

### Task 3: Rewrite re-sweep from first filter + pass cap

**Objective:** Rewrites restart the sweep; MaxPasses bounds total sweeps;
non-convergence fails.

**Files:**
- Modify: `internal/validator/filter.go`
- Test: `internal/validator/filter_test.go`

**Step 1: Write failing test** proving:
- a rewrite from filter B causes filter A to re-run with the new output
- a chain that converges (second sweep, no rewrite) returns Output with
  Rejected == nil and the correct Passes count
- a constructed ping-pong pair (A appends "x", B strips "x" - each sweep
  changes output) hits the non-convergence FilterFail at MaxPasses with
  reason containing "did not converge", and total filter invocations are
  bounded (no infinite loop)
- MaxPasses <= 0 passed to NewFilterChain defaults to 2

**Step 2-4:** failing test -> implement -> pass
(`-run TestFilterChainRewrite`)

### Task 4: Actions log lines

**Objective:** Machine-checkable per-action log lines.

**Files:**
- Modify: `internal/validator/filter.go`
- Test: `internal/validator/filter_test.go`

**Step 1: Write failing test** asserting the exact Actions format
`pass=<n> filter=<name> action=pass|rewrite|fail` for a mixed
pass/rewrite/fail scenario, in execution order.

**Step 2-4:** failing test -> implement -> pass (`-run TestFilterChainActions`)

### Task 5: FilterChain concurrency safety

**Objective:** Chain state never crosses calls (chains are per-run; no shared
mutable state), verified under `-race`.

**Files:**
- Test: `internal/validator/filter_race_test.go`

**Step 1: Write test** running `Run` concurrently from 8 goroutines with
shared filter instances whose Process is pure; assert all results identical.

**Step 2-4:** failing (if state leaks) -> fix -> pass.
Run with: `go test -p 2 -race ./internal/validator/ -run Race -v`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing (`go test -p 2 ./internal/validator/ -v`)
- [ ] Interface contracts (above) satisfied exactly - signatures match verbatim
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - no concrete filters, no daemon wiring, no step.go changes
- [ ] gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, file paths)
- [ ] Frozen semantics 1-7 hold (sweep order, fail-fast, re-sweep, cap,
      skip, logging, no step mutation)
- [ ] Code follows project conventions (naming, error handling, structure)
- [ ] No bugs, no security issues
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The ping-pong non-convergence test is the most important test in the tree -
  it proves a rewrite pair can never loop forever and can never silently pass.
- Keep `filter.go` free of imports from `internal/agent` (import cycle risk:
  agent imports validator). Only `internal/task` and stdlib.
- Existing package uses `//nolint:revive` for stuttering names - follow suit.
