# Output Filters (Milter-Style Post-Step Content Stage) - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 leaf documents under this node
- **Scope:** Add a pluggable milter-style output-filter stage (pass / rewrite / fail) between step completion and the existing evidence-validation gate, with a logged ordering contract, capped idempotent rewrite passes, and filter rejections accounted independently from evidence-validation retries.

## Goal

Meept today checks step results at two layers: deterministic evidence validation
(`internal/validator`, wired at `internal/agent/tactical.go` validation gate) and
LLM review (`ReviewManager.ReviewStep`). Neither inspects or repairs the step
RESULT CONTENT. Mechanical failures - invalid JSON, lint errors, wrong-language
output - currently cost either a full LLM review cycle or a rework turn.

This tree adds an output-filter chain: deterministic, cheap stages that can
PASS the result, REWRITE it (lint-autofix, JSON reformat), or FAIL it with a
machine-readable reason that feeds the existing retry/rework plumbing. This is
the "validated egress with a repair loop" shape; the failure reason supplies
the context, so the check stays binary and cheap.

## Architecture

New `OutputFilter` interface and `FilterChain` in `internal/validator` (beside
the existing evidence `Validator` - same package, shared manager). The chain is
wired into the step-completion path in `internal/agent/tactical.go` BEFORE the
existing evidence-validation gate. Filter rejections use their OWN retry
counter (`FilterRetryCount`) - they never consume `ValidationRetryCount`.
Rewrite passes are capped (`max_passes`, default 2) and every stage transition
is logged with a `stage` field so the ordering contract is observable.

## Interface Contracts

### Contract 1: OutputFilter interface and FilterResult (tri-state)

```
// File: internal/validator/filter.go
package validator

type FilterOutcome int

const (
    FilterPass    FilterOutcome = iota // result continues unchanged
    FilterRewrite                      // Output replaces the result
    FilterFail                         // result rejected; Reason feeds rework
)

type FilterResult struct {
    Outcome FilterOutcome
    Filter  string // stage name, e.g. "json_format" - ALWAYS set
    Output  string // rewritten result; set iff Outcome == FilterRewrite
    Reason  string // machine-readable failure context; set iff Outcome == FilterFail
}

type OutputFilter interface {
    Name() string
    Applies(step *task.TaskStep) bool
    Process(ctx context.Context, step *task.TaskStep, output string) FilterResult
}

// Owner: 01-filter-interface.md
// Consumers: 02-builtin-filters.md, 03-gate-wiring.md, 04-config-cli-docs.md
```

Rules bound to this contract:
- `Reason` must be machine-readable and self-sufficient for a repair prompt
  (e.g. `$.tools[2].name: expected string, got null`, `lang=de confidence=0.97`).
- A filter MUST NOT mutate the step; it receives and returns strings.
- `Process` must be idempotent: `Process(p(x)) == Process(x)` for rewrite stages.

### Contract 2: FilterChain execution semantics

```
// File: internal/validator/filter.go (same file)
package validator

type FilterChainConfig struct {
    Filters          []OutputFilter // ordered; order IS the contract
    MaxPasses        int            // rewrite sweeps before forced fail; default 2
}

type ChainResult struct {
    Output    string         // final output after rewrites
    Rejected  *FilterResult  // non-nil iff the chain failed
    Passes    int            // rewrite sweeps executed
    Actions   []string       // per-filter action log lines, in order
}

func NewFilterChain(filters []OutputFilter, maxPasses int) *FilterChain
func (fc *FilterChain) Run(ctx context.Context, step *task.TaskStep, output string) ChainResult

// Owner: 01-filter-interface.md
// Consumers: 03-gate-wiring.md
```

Chain semantics (FROZEN - this is the ordering contract):
1. One PASS = one sweep over all filters in declared order.
2. A `FilterFail` terminates the chain immediately; later filters do not run.
3. A `FilterRewrite` updates the output; the sweep re-runs from the first
   filter with the new output (rewrite can re-trigger earlier filters).
4. If `Passes` reaches `MaxPasses` and the output still changes, the chain
   returns `FilterFail` with reason `filter chain did not converge after N passes`
   - a non-converging rewrite pair must never loop forever and must never
   silently pass.
5. Filters where `Applies` returns false are skipped without counting as a pass.
6. `Actions` records one line per filter per sweep:
   `pass=<n> filter=<name> action=pass|rewrite|fail` - the orchestrator and
   the gate log this verbatim so the ordering contract is always observable.

### Contract 3: Step persistence fields and independent retry accounting

```
// File: internal/task/step.go - ADDED fields on TaskStep
FilterRetryCount int    // filter rejections only; NEVER touched by the
                        // evidence-validation retry path
FilterError      string // last filter rejection Reason, for audit/debug

// Owner: 03-gate-wiring.md
// Consumers: 04-config-cli-docs.md
```

Retry-class separation (FROZEN):
- Filter rejection -> `FilterRetryCount++`, requeue; capped by
  `max_filter_retries` (default 2). On exhaustion the step finalizes failed
  with the filter reason as the error text.
- Evidence-validation failure -> existing `ValidationRetryCount` path,
  unchanged. The two counters are independent in both directions: a filter
  rejection never consumes a validation retry and vice versa.
- Review revision cycles (`RevisionCount`) are likewise untouched.
- Every counter bump is persisted via `stepStore.Update` in the same
  transaction-style sequence the existing validation retry uses
  (`internal/agent/tactical.go` validation-gate block is the pattern).

### Contract 4: Gate ordering and logging contract

FROZEN post-step pipeline order in `internal/agent/tactical.go`:

```
1. step job result arrives
2. claim-vs-evidence marking            (existing, ~tactical.go:1160)
3. OUTPUT FILTER CHAIN                  (NEW - this tree)
4. evidence validation gate             (existing, ~tactical.go:1186)
5. ReviewStep policy/reviewer           (existing, internal/agent/review_manager.go)
6. adversarial verification             (existing hook)
```

- Filters run strictly before the evidence-validation gate. The evidence gate
  validates side-effects; the filter chain validates/repairs content. Neither
  substitutes for the other and the order never swaps.
- EVERY stage outcome is logged with structured slog keys:
  `stage=output_filter`, `filter=<name>`, `pass=<n>`,
  `action=pass|rewrite|fail|rejected_exhausted`, `step_id`, plus the reason on
  fail. A rejection that does not name its stage in the log is a bug.
- A rewrite MUST be logged with action=rewrite; a pass-through that rewrote
  nothing logs action=pass. Silent mutation is forbidden.

### Contract 5: Built-in filters shipped by this tree

```
// Files: internal/validator/filter_json.go, filter_language.go, filter_lint.go
json_format   - parses output as JSON (when the step expects JSON); on
                success reformats canonically (rewrite); parse failure = fail
                with the parser's error path/offset as Reason.
language_en   - script/stopword-based language detection; fails with
                lang=<code> confidence=<f> when the detected language is not
                the session's expected language (default en). Pure stdlib +
                bundled stopword lists; NO network, NO model call.
lint_go       - shells gofmt -l / go vet (where applicable) on code-bearing
                outputs; applies gofmt -w as rewrite in a temp dir. Subprocess
                timeout 10s; timeout = fail with reason timeout.
// Owner: 02-builtin-filters.md
```

### Contract 6: Configuration shape

```
// ~/.meept/meept.json5 and per-agent AGENT.md frontmatter
"output_filters": {
    "enabled":            true,
    "max_passes":         2,   // rewrite sweeps before chain fail
    "max_filter_retries": 2,   // independent of validation retries
    "filters":            ["json_format", "language_en"]
}
// Per-agent override mirrors the verification-config override chain:
// daemon defaults -> agent front matter -> runtime metadata.
// Default when the key is absent: enabled=false (zero behavior change
// for existing installs until opted in).
// Owner: 04-config-cli-docs.md
// Consumers: 03-gate-wiring.md reads via the config snapshot.
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-filter-interface.md | leaf | none | 45K | A |
| 02 | 02-builtin-filters.md | leaf | 01 | 65K | B |
| 03 | 03-gate-wiring.md | leaf | 01 | 75K | B |
| 04 | 04-config-cli-docs.md | leaf | 01, 03 | 55K | C |

**Concurrency groups:** Documents in the same letter group have no
inter-dependencies and can be dispatched simultaneously (max 3 per batch).

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group [A]

1. **Read** 01-filter-interface.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-filter-interface.md"
   - Context: Full leaf document text + Contracts 1-2 from this orchestrator
     + coding conventions block + internal/validator/interface.go and
     internal/validator/manager.go INLINED
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - Include: "Do NOT use read_file on existing source files - explore with search_files
     or terminal cat instead. If you read a file, never feed its output into write_file."

### Phase 2: Dispatch Concurrency Group [B] (after 01 reaches REVIEWED)

1. **Read** 02-builtin-filters.md and dispatch as in Phase 1, with Contracts
   1, 2, 5 and the completed internal/validator/filter.go INLINED.
2. **Read** 03-gate-wiring.md and dispatch as in Phase 1, with Contracts
   1-4 and the completed internal/validator/filter.go INLINED.

### Phase 3: Dispatch Concurrency Group [C] (after 03 reaches REVIEWED)

1. **Read** 04-config-cli-docs.md and dispatch as in Phase 1, with Contracts
   3-6 INLINED.

### Phase 4: Review and Commit Each Child

After each implementation agent returns, the orchestrator reviews in-session
(the main model reviews directly, NOT a delegated subagent):

1. **Orchestrator reviews in-session:**
   - Read the changed files (from the implementer's file list)
   - Check against leaf spec + interface contracts + Review Checklist below
   - Run tests and build verification: `go test -p 2 ./internal/validator/ ./internal/task/ ./internal/agent/ -v`
2. **If review finds gaps:** re-dispatch with specific feedback, max 3 cycles, then escalate.
3. **If review passes:** commit the leaf's files with explicit paths
   `git add <exact paths> && git commit -m "feat(validator): implement [leaf name]"`,
   update the tracking table to REVIEWED.

### Phase 5: Integration Review

After ALL children reach REVIEWED:

1. Run the full Integration Test Plan below in-session.
2. Verify the ordering contract end-to-end with the logging assertions.
3. Normalize with gofmt; verify no line-number corruption
   (`grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/` returns zero).
4. Commit integration changes with explicit paths; mark all children COMPLETE.
5. Update the GitHub issue referenced in Meta with completion status.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied exactly
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder values
- [ ] No line-number corruption: no `     N|` prefixes baked into source files
- [ ] Contract-specific: filter rejections NEVER touch ValidationRetryCount
      and validation retries NEVER touch FilterRetryCount (test proves it)
- [ ] Contract-specific: every filter action logs stage/filter/pass/action
- [ ] Contract-specific: rewrite stages are idempotent and pass-capped

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language/Framework:** Go (module github.com/caimlas/meept), stdlib-first
- **Naming:** exported = PascalCase, unexported = camelCase; error vars `ErrX`
- **Error handling:** wrap with `%w`, return early; NO `_ = expr` ignored
  errors (pre-commit gate blocks); NO bare `panic(err)`; two-value type
  assertions on map payloads
- **Mutexes:** never across I/O; collect-under-lock then operate
  (mutexio analyzer enforced)
- **Setters:** every `Set*` gets a nil guard
- **IDs:** never `time.Now().UnixNano()` or `math/rand` (predid analyzer)
- **Testing:** table-driven, stdlib `testing` + testify where the package
  already uses it; `go test -p 2` (macOS ephemeral-port exhaustion)
- **Logging:** `log/slog` structured keys, lowercase values
- **Formatting tool:** gofmt before reporting completion
- **UI text:** lowercase (TUI/GUI strings, if any)

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-filter-interface | PENDING | 0 | |
| 02-builtin-filters | PENDING | 0 | |
| 03-gate-wiring | PENDING | 0 | |
| 04-config-cli-docs | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. **Unit:** `go test -p 2 ./internal/validator/ ./internal/task/ ./internal/agent/ -v` - all pass.
2. **Ordering contract test:** a step whose output fails a filter AND whose
   evidence is missing shows the filter rejection FIRST in the log sequence;
   the evidence gate never ran for that attempt (assert via the Actions log
   and the step's `FilterError` being set while `ValidationError` is empty).
3. **Retry-independence test:** exhaust `max_filter_retries` while the
   evidence validator would also fail; assert `ValidationRetryCount == 0`
   throughout and failure text names the filter reason.
4. **Convergence test:** two filters that each rewrite the other's output
   (constructed in-test) reach the non-convergence `FilterFail` at
   `MaxPasses`, never loop, never pass.
5. **Idempotency test:** running each builtin filter twice on its own output
   yields action=pass on the second run.
6. **Zero-change default:** with `output_filters.enabled=false` (default),
   `go test -p 2 ./...` passes and step-completion behavior is byte-identical
   (no filter stage in logs).
7. **Full sweep:** `go build ./...` and `go test -p 2 ./...` green.

## Open Questions

- Which agent classes get filters on by default once enabled globally?
  (Proposal: coder/researcher first; chat last. Deferred to issue #55
  discussion - this tree ships the mechanism + opt-in config only.)
- Should `applyReplyGuard` (internal/agent, loop-layer reply shaping) later
  migrate onto FilterChain? Deliberately OUT of scope: different lifecycle
  (reply vs step result). Record as a follow-up issue at integration time.

## Notes

- GitHub tracking issue: https://github.com/bhodgens/meept/issues/55 (the
  orchestrator updates it at integration completion).
- The existing evidence-validation retry plumbing
  (`internal/agent/tactical.go` validation gate, `validationRetryCount`
  in-memory + persisted hybrid, `GetValidationPolicy().MaxValidationLoops`)
  is the PATTERN to mirror for filter retries, not the thing to modify.
  Do not change validation-retry semantics in this tree.
- `internal/validator` currently validates tool-execution EVIDENCE against
  ground truth. The new OutputFilter validates/repairs RESULT CONTENT. Keep
  the two concepts in separate files and separate doc comments; the manager
  may share the package but must not conflate the interfaces.
- README.md gained a "Post-Step Validation Pipeline" section (authored
  separately) documenting layers 2/4/5 as they exist BEFORE this tree; leaf
  04 amends it to insert the filter stage at position 3.
