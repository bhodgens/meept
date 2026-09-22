# Gate Wiring - Step-Completion Integration - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify - write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Wire the FilterChain into the step-completion path in tactical.go BEFORE the evidence-validation gate, with structured stage logging and an INDEPENDENT FilterRetryCount.
- **Dependencies:** 01-filter-interface.md (chain + interface)
- **Estimated Context:** 75K (exploration 25K + generation 20K + iteration 20K + overhead 10K)
- **Concurrency Group:** B (parallel with 02; both depend only on 01)

## Goal

Insert the filter stage at position 3 of the frozen post-step pipeline
(after claim-marking, before evidence validation), make every filter action
observably logged, and give filter rejections their own retry counter that
never touches the evidence-validation retry accounting.

## Context

The step-completion path lives in `internal/agent/tactical.go`
(`TaskService` / `ts`). The existing evidence-validation gate starts around
line 1186 (`ts.validatorManager.ValidateStep`); the claim-vs-evidence marker
precedes it around line 1160. The validation retry plumbing
(`validationRetryCount`, persisted `ValidationRetryCount` + in-memory
counter, `MaxValidationLoops` policy, requeue-via-stepStore.Update) is the
PATTERN to mirror. Do NOT modify validation-retry semantics.

Key files to understand before implementing:
- `internal/agent/tactical.go` (~1140-1260) - completion path, claim marker, validation gate, retry/requeue block
- `internal/agent/tactical.go` (~2319) - `hasMeaningfulEvidence`/validator checks near the wiring predicate
- `internal/task/step.go` - `Validated`, `ValidationError`, `ValidationRetryCount` fields (the model for the new fields)
- `internal/validator/filter.go` - the chain this leaf consumes (from leaf 01)
- `internal/agent/tactical_validation_gate_test.go` - existing gate test harness; extend, don't fork

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/task/step.go - NEW fields on TaskStep:
FilterRetryCount int    // filter rejections only; NEVER touched by the
                        // validation retry path
FilterError      string // last filter rejection Reason

// internal/agent/tactical.go - wiring:
// TaskService gains a chain holder + setter:
func (ts *TaskService) SetFilterChain(fc *validator.FilterChain)  // nil-guarded setter
// Completion path inserts, after claim marking and BEFORE the
// validatorManager.ValidateStep gate:
//   if ts.filterChain != nil && filtersEnabledFor(step) {
//       run chain -> log Actions verbatim (stage=output_filter ...)
//       FilterRewrite -> step.Result = chain.Output (persisted via stepStore.Update)
//       FilterFail -> independent retry path (below)
//   }
// filtersEnabledFor reads the config snapshot: output_filters.enabled &&
// len(filters) > 0. Default (key absent / enabled=false): stage skipped,
// zero behavior change, nothing logged.
```

### Frozen contracts this leaf implements (parent Contracts 3-4)

Gate order (never swaps):
```
1. step job result arrives
2. claim-vs-evidence marking
3. OUTPUT FILTER CHAIN            <- this leaf
4. evidence validation gate
5. ReviewStep policy/reviewer
6. adversarial verification
```

Retry-class separation (both directions, test-proven):
- Filter rejection -> `FilterRetryCount++` (persisted), requeue; cap =
  `max_filter_retries` (default 2, from config snapshot; field added by
  leaf 04 - read via a local interface so this leaf compiles first:
  `type filterRetryLimiter interface { MaxFilterRetries() int }` with
  default 2 when unset).
- On cap exhaustion: step finalizes FAILED, error text = the filter Reason,
  logged with `action=rejected_exhausted`.
- Validation retries untouched; `RevisionCount` untouched.
- Counter bump + requeue follow the EXACT persistence sequence of the
  existing validation retry block (Update step, then requeue; Warn-log the
  persistence failure and continue, as the existing block does).

Logging contract (every path, structured slog):
```
ts.logger.Info("output filter action",
    "stage", "output_filter",
    "filter", <name>, "pass", <n>, "action", "pass"|"rewrite"|"fail"|"rejected_exhausted",
    "step_id", step.ID, "reason", <reason-if-fail>)
```
Chain-level: on rejection also Warn "output filter rejected step" with the
same keys plus the full Actions slice. A rejection that does not name its
stage in the log is a bug.

### What This Leaf Consumes

```
// From 01-filter-interface.md:
type FilterChain; func (fc *FilterChain) Run(ctx, step, output) ChainResult
type ChainResult struct { Output string; Rejected *FilterResult; Passes int; Actions []string }
// From internal/task (existing): TaskStep, stepStore.Update / SetState
```

## Tasks

### Task 1: TaskStep new fields + persistence

**Objective:** Add `FilterRetryCount` and `FilterError` to TaskStep; verify
they round-trip through the step store.

**Files:**
- Modify: `internal/task/step.go` (add fields beside `ValidationRetryCount`)
- Test: `internal/task/step_test.go` (extend; follow existing table style)

**Step 1: Write failing test** - construct a TaskStep with both fields set,
persist via the store constructor used in existing step tests, reload, assert
round-trip. If the store drops unknown fields, extend its column/scan set the
same way `ValidationRetryCount` is handled (grep for it in the store files).

**Step 2:** `go test -p 2 ./internal/task/ -run TestStepFilterFields -v` -> FAIL
**Step 3:** implement (fields + store columns if required)
**Step 4:** PASS. Also run `go test -p 2 ./internal/task/ -v` full package.

### Task 2: SetFilterChain setter + disabled default

**Objective:** Nil-guarded setter; chain disabled by default; disabled path
byte-identical.

**Files:**
- Modify: `internal/agent/tactical.go` (TaskService struct near line 99,
  constructor near line 426, setter beside other Set* methods)
- Test: `internal/agent/tactical_filter_disabled_test.go` (new file)

**Step 1: Write failing test:**
- `SetFilterChain(nil)` does not panic and leaves the holder nil
  (typed-nil interface guard convention)
- with no chain set, a completing step's log contains NO "output filter
  action" events and Result is untouched

**Step 2:** FAIL **Step 3:** implement **Step 4:** PASS

### Task 3: Chain invocation + rewrite application

**Objective:** Insert stage 3; rewrite updates and persists step.Result.

**Files:**
- Modify: `internal/agent/tactical.go` (insert block between claim marking
  and the `validatorManager.ValidateStep` gate)
- Test: `internal/agent/tactical_filter_rewrite_test.go` (new file; reuse
  the harness pattern of `tactical_validation_gate_test.go`)

**Step 1: Write failing test:** with a stub chain (stub filters defined in
the test file implementing validator.OutputFilter) returning rewrite:
- step.Result equals the rewritten output after completion handling
- the rewritten Result is persisted (reload via store)
- Actions lines logged with stage=output_filter, action=rewrite
- the evidence-validation gate still ran AFTER the rewrite (order probe:
  validation stub records invocation sequence; filter runs first)

**Step 2:** FAIL **Step 3:** implement **Step 4:** PASS

### Task 4: Independent filter retry path

**Objective:** Filter rejection requeues on its own counter with its own cap.

**Files:**
- Modify: `internal/agent/tactical.go` (rejection branch mirroring the
  validation retry block)
- Test: `internal/agent/tactical_filter_retry_test.go` (new file)

**Step 1: Write failing test** (three scenarios):
1. rejection with retryCount < cap: step requeued (state transitions back to
   queued/pending as the validation retry does), `FilterRetryCount == 1`,
   `ValidationRetryCount == 0`, FilterError == Reason, rejection logged with
   action=fail
2. successive rejections reach the cap: step finalizes FAILED, error text
   equals the filter Reason, logged action=rejected_exhausted
3. THE independence probe: a step whose evidence ALSO fails validation,
   rejected first by the filter: assert filter retry increments while
   ValidationRetryCount stays 0 for that attempt; then a validation-only
   failure on a different step leaves FilterRetryCount at 0. Both directions.

**Step 2:** FAIL **Step 3:** implement **Step 4:** PASS

### Task 5: Ordering-contract regression guard

**Objective:** A test that fails if anyone reorders filter stage vs evidence gate.

**Files:**
- Test: `internal/agent/tactical_filter_ordering_test.go` (new file)

**Step 1: Write test:** stub both the chain and the evidence validator as
sequence recorders; run a completion; assert the recorded global sequence is
exactly [filter..., validation...]. Include a comment citing master.md
Contract 4 (gate ordering is FROZEN).

**Step 2-4:** this test passes immediately if Task 3 is correct - it exists
to break on future reordering. Verify it fails when the insertion point is
temporarily moved (manual check), then restore. Note the check result in
your report.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing:
      `go test -p 2 ./internal/task/ ./internal/agent/ -v`
- [ ] `-race` green on the new agent tests:
      `go test -p 2 -race ./internal/agent/ -run TestTaskServiceFilter -v`
- [ ] Disabled default proven byte-identical (Task 2 test)
- [ ] Retry independence proven in BOTH directions (Task 4.3)
- [ ] Setter nil-guarded
- [ ] gofmt clean
- [ ] No modifications to validation-retry semantics (diff review)
- [ ] No deviations from spec (or documented below)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing, including -race
- [ ] Gate order is filter -> validation, enforced by a regression test
- [ ] Filter retries NEVER touch ValidationRetryCount (both directions tested)
- [ ] Every filter action logs stage/filter/pass/action/reason
- [ ] Disabled default = zero behavior change, zero log lines
- [ ] Validation-retry code path unmodified (inspect the diff)
- [ ] No ignored errors, no bare panics, mutex never across I/O
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- tactical.go is 2900+ lines and heavily commented with finding IDs - place
  the new block with its own explanatory comment block in the house style
  (why the stage exists, the frozen order, where it sits).
- The requeue mechanics differ subtly between the validation retry (rebuilds
  the job payload from the persisted step) - copy that pattern exactly; a
  filter retry job must re-stamp identically to the first schedule.
- Do NOT wire config reading here beyond the local limiter interface - leaf
  04 owns the config schema; this leaf defines the seam.
