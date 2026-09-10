# Allotment Math - Implementation Leaf

## DISPATCH INSTRUCTION

> **Implementing agent:** Implement ALL tasks below using TDD. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back
> to verify — write once and stop.

## Meta

- **Parent:** docs/plans/allotment/master.md
- **Scope:** Pure allotment math: token estimation, context-window
  conversion, and step batching — no scheduler wiring.
- **Dependencies:** none
- **Estimated Context:** ~40K
- **Concurrency Group:** A

## Goal

Provide the pure functions the tactical scheduler will use to convert a
model's context window into work allotments and partition ordered steps
into agent-sized batches. Zero side effects; fully table-testable.

## Context

meept plans execute as ordered TaskSteps (internal/task/step.go,
struct `TaskStep`: ID, Description, Sequence, DependsOn...). The
tactical scheduler currently caps phases by step COUNT. This leaf adds
the token math so batches respect the executor model's context window.

Key files:
- internal/task/step.go — TaskStep struct (Description field drives
  estimation)
- internal/agent/loop.go — DefaultConversationTokenBudget = 50000 (the
  budget that killed the E2E smoke; allotments must fit under it)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/allotment.go
package agent

type AllotmentConfig struct {
    UsableRatio   float64 // default 0.75
    ReserveTokens int     // default 4096
    CharsPerToken float64 // default 4
    MinStepTokens int     // default 512
    MaxBatchSteps int     // default 0 (no count cap)
}

func DefaultAllotmentConfig() AllotmentConfig
func EstimateStepTokens(desc string, cfg AllotmentConfig) int
func AllotmentTokens(contextLimit int, cfg AllotmentConfig) int
func SplitStepsByAllotment(steps []*task.TaskStep, allotmentTokens int,
    cfg AllotmentConfig) [][]*task.TaskStep
```

### What This Leaf Consumes

```go
// internal/task/step.go (existing)
type TaskStep struct {
    ID          string
    Description string
    // ... existing fields
}
```

## Tasks

### Task 1: AllotmentConfig + defaults

**Objective:** Config struct with safe defaults; every field documents
its default.

**Files:**
- Create: `internal/agent/allotment.go`
- Test: `internal/agent/allotment_test.go`

**Step 1: Write failing test**

```go
func TestDefaultAllotmentConfig(t *testing.T) {
    cfg := DefaultAllotmentConfig()
    if cfg.UsableRatio != 0.75 {
        t.Errorf("UsableRatio = %v, want 0.75", cfg.UsableRatio)
    }
    if cfg.ReserveTokens != 4096 {
        t.Errorf("ReserveTokens = %v, want 4096", cfg.ReserveTokens)
    }
    if cfg.CharsPerToken != 4 {
        t.Errorf("CharsPerToken = %v, want 4", cfg.CharsPerToken)
    }
    if cfg.MinStepTokens != 512 {
        t.Errorf("MinStepTokens = %v, want 512", cfg.MinStepTokens)
    }
}
```

**Step 2:** Run: `go test ./internal/agent/ -run TestDefaultAllotmentConfig`
Expected: FAIL (undefined)

**Step 3: Implementation** — struct + DefaultAllotmentConfig() returning
the pinned defaults.

### Task 2: EstimateStepTokens

**Objective:** desc token estimate = ceil(len(desc)/CharsPerToken),
floored at MinStepTokens.

Failing test (table-driven):

```go
func TestEstimateStepTokens(t *testing.T) {
    cfg := DefaultAllotmentConfig()
    tests := []struct {
        name string
        desc string
        want int
    }{
        {"empty", "", cfg.MinStepTokens},
        {"short", "fix bug", cfg.MinStepTokens}, // 7 chars < 512 min
        {"exact min", strings.Repeat("a", 2048), 512}, // 2048/4 = 512
        {"large", strings.Repeat("a", 40000), 10000},  // 40000/4
    }
    for _, tt := range tests {
        if got := EstimateStepTokens(tt.desc, cfg); got != tt.want {
            t.Errorf("%s: EstimateStepTokens = %d, want %d", tt.name, got, tt.want)
        }
    }
}
```

Implementation: compute `len(desc)` as float, divide by
`cfg.CharsPerToken` (guard: if CharsPerToken <= 0 use 4), ceil, then
max with MinStepTokens.

### Task 3: AllotmentTokens

**Objective:** context window → work budget.
`(contextLimit - ReserveTokens) * UsableRatio`, floored at 0; returns 0
when contextLimit <= 0.

Failing test:

```go
func TestAllotmentTokens(t *testing.T) {
    cfg := DefaultAllotmentConfig()
    tests := []struct {
        name        string
        contextLim  int
        want        int
    }{
        {"unknown window", 0, 0},
        {"negative", -5, 0},
        {"8k model", 8192, int(float64(8192-4096) * 0.75)},   // 3072
        {"32k model", 32768, int(float64(32768-4096) * 0.75)}, // 21504
        {"128k model", 131072, int(float64(131072-4096) * 0.75)}, // 95232
        {"tiny window below reserve", 1000, 0},
    }
    for _, tt := range tests {
        if got := AllotmentTokens(tt.contextLim, cfg); got != tt.want {
            t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
        }
    }
}
```

Implementation: if contextLimit <= 0 return 0; budget = (contextLimit -
ReserveTokens) * UsableRatio; if budget < 0 return 0; return int(budget).

### Task 4: SplitStepsByAllotment

**Objective:** greedy batch fill. Steps in order; accumulate estimated
tokens; start a new batch when the next step would exceed
allotmentTokens OR MaxBatchSteps is reached. Single oversize step (its
own estimate > allotment) gets its own batch (never dropped, never
split). allotmentTokens <= 0 → one batch with everything (caller falls
back to count-based chunking).

Failing test:

```go
func TestSplitStepsByAllotment(t *testing.T) {
    mk := func(n int) []*task.TaskStep {
        var out []*task.TaskStep
        for i := 0; i < n; i++ {
            s := task.NewTaskStep("t1", strings.Repeat("a", 2048), i) // 512 tok each
            out = append(out, s)
        }
        return out
    }
    cfg := DefaultAllotmentConfig()

    t.Run("zero allotment single batch", func(t *testing.T) {
        batches := SplitStepsByAllotment(mk(5), 0, cfg)
        if len(batches) != 1 || len(batches[0]) != 5 {
            t.Errorf("want 1 batch of 5, got %d batches", len(batches))
        }
    })
    t.Run("greedy fill", func(t *testing.T) {
        // allotment 1536 fits three 512-token steps
        batches := SplitStepsByAllotment(mk(7), 1536, cfg)
        if len(batches) != 3 {
            t.Errorf("want 3 batches (3/3/1), got %d", len(batches))
        }
    })
    t.Run("oversize step own batch", func(t *testing.T) {
        big := task.NewTaskStep("t1", strings.Repeat("a", 40000), 0) // 10000 tok
        small := task.NewTaskStep("t1", "fix bug", 1)
        batches := SplitStepsByAllotment([]*task.TaskStep{big, small}, 2048, cfg)
        if len(batches) != 2 || len(batches[0]) != 1 || len(batches[1]) != 1 {
            t.Errorf("want 2 solo batches, got %d", len(batches))
        }
    })
    t.Run("empty input", func(t *testing.T) {
        batches := SplitStepsByAllotment(nil, 2048, cfg)
        if len(batches) != 0 {
            t.Errorf("want 0 batches, got %d", len(batches))
        }
    })
}
```

Implementation: iterate steps; accumulate EstimateStepTokens; flush the
batch before adding a step that would overflow (unless the batch is
empty — then take the oversize step alone); respect MaxBatchSteps > 0.

### Task 5: Continuation helpers (naming only, for leaf 02)

**Objective:** exported helper to describe continuation steps so leaf 02
labels them consistently.

```go
// ContinuationDescription prefixes desc with a [continuation k/N] marker.
func ContinuationDescription(desc string, k, n int) string {
    return fmt.Sprintf("[continuation %d/%d] %s", k, n, desc)
}

func TestContinuationDescription(t *testing.T) {
    got := ContinuationDescription("do the thing", 2, 3)
    want := "[continuation 2/3] do the thing"
    if got != want {
        t.Errorf("got %q, want %q", got, want)
    }
}
```

## Self-Verification Checklist

- [ ] All tasks implemented; table tests pass
- [ ] Edge cases: empty desc, empty input, oversize step, tiny window,
      zero/negative allotment
- [ ] gofmt clean; go vet clean; ASCII only
- [ ] No TODOs, no debug prints
- [ ] Signatures match Contract 1 exactly

**DO NOT COMMIT.** Orchestrator handles git operations.

**Deviations from spec:** none expected.

## Review Checklist (For Review Agent)

- [ ] All tasks implemented; tests present and passing
- [ ] Signatures match Contract 1 exactly
- [ ] Edge cases covered (empty, oversize, tiny window, zero allotment)
- [ ] Defaults match Contract 1 (0.75 / 4096 / 4 / 512 / 0)
- [ ] No scope creep

Output: APPROVED or list of specific gaps.

## Notes

- AllotmentTokens floors at 0 (never negative) so callers can use `> 0`
  as the "window known" check.
- The 0.75 ratio leaves headroom for the agent's own tool outputs within
  a turn; the 4096 reserve covers system prompt + tool defs.
- Greedy (not optimal) bin packing is deliberate: order encodes plan
  dependency semantics and must be preserved.
