# Tool Boundary Hardening - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 leaf documents under this node
- **Scope:** Make tool-call boundaries fail fast with typed, actionable errors; stop repeat-identical-error tool loops; reject vacuous planner outputs; extend work-plus-report routing arbitration.

## Goal

The 2026-09-18 e2e run exposed four harness gaps that let a weak model turn one
file-write task into ~9 minutes of churn and a false task failure:

1. `task_create` was called 45+ times with empty args. The schema declared
   `Required: ["name"]` but enforcement is advisory - each tool hand-rolls its
   own arg checks (or forgets to), and the model repeated the identical invalid
   call because nothing structural stopped it.
2. The cycle detector aborted every 4 identical calls, but abort → replan →
   fresh turn reset the guards, so the same loop replayed 11+ times until the
   sync ceiling.
3. A planner step whose entire result was narration ("I will perform the task
   now...") - no plan, no steps, no tool work - was auto-approved by the
   heuristic reviewer (trivial task, non-empty result) and let the loop
   continue one more round.
4. "create X, then tell me Y" routed to a compound pair session (planner as
   reviewer) because the 2026-09-10 work-plus-report collapse deliberately
   excludes `report` tag-alongs (F42 protection). The report variant of the
   same prompt shape still hijacks trivial tasks.

This tree fixes all four at the harness layer, model-independent.

## Architecture

One shared validation primitive in `internal/tools` (leaf 01), consumed by the
registry's `Execute` path so every current and future tool inherits it. One
loop-level circuit breaker in `internal/agent` (leaf 02) keyed on
(tool name, args hash, error text) that survives across guard resets within a
step/plan turn. One review-gate tightening in the review manager (leaf 03).
One dispatcher arbitration extension (leaf 04). Leaves 01+02 touch disjoint
packages (tools vs agent); leaf 03 and 04 also touch `internal/agent` but
disjoint files from leaf 02 - file ownership is exclusive per leaf.

## Interface Contracts

### Contract 1: ArgValidationError + ValidateToolArgs

```go
// File: internal/tools/argvalidate.go
package tools

// ArgValidationError reports a missing/ill-typed required argument.
// Message shape: `task_id is required (pass it exactly as the schema names it;
// see the tool's required list)`. ErrorResult carries ErrCode "invalid_args".
type ArgValidationError struct {
    Tool    string
    Arg     string
    Problem string // "missing" | "empty" | "wrong type: want N, got T"
}

func (e *ArgValidationError) Error() string

// ValidateToolArgs checks args against the tool's declared schema:
// for every name in Parameters().Required: key present, non-empty
// (no "" / trimmed-empty string / nil), and JSON type matches the declared
// property type (string/number/boolean/array/object). Extra keys are NOT
// rejected (forward-compat). Returns nil when args satisfy the schema.
func ValidateToolArgs(tool Tool, args map[string]any) error
```

Owner: 01-arg-validation.md. Consumers: registry.Execute (same leaf), all
future tools.

### Contract 2: RepeatErrorBreaker

```go
// File: internal/agent/repeat_error_breaker.go
package agent

// repeatErrorBreaker suppresses the identical-failure tool loop: when the
// same tool with the same args (canonical JSON hash) fails with the same
// error text MAX_IDENTICAL_TOOL_ERRORS (3) times within one turn generation,
// ExecuteToolCall returns a terminal error:
//   `tool <name> rejected the identical input 3 times (<first-error>); giving up`
// Reset ONLY by resetTurnGuards (per turn generation). Survives guard resets
// within the generation? NO - it lives in the turn-scoped guard set and is
// deliberately NOT reset by escalation/replan (a fresh generation must be able
// to retry with different args; identical args re-hit the breaker instantly).
type repeatErrorBreaker struct{ ... }

func newRepeatErrorBreaker() *repeatErrorBreaker
func (b *repeatErrorBreaker) Observe(tool string, argsHash, errMsg string) (exhausted bool, summary string)
```

Owner: 02-repeat-error-breaker.md. Consumer: loop.go ExecuteToolCall seam.

### Contract 3: Plan-vacuity review gate

```go
// File: internal/agent/review_vacuity.go (new)
package agent

// planLooksVacuous reports whether a planner/plan-hint step result produced
// NO planning artifact: no task_create/task_update tool evidence AND a result
// whose text matches narration markers (first-person future intent:
// "I will ", "I'll ", "Let me ", "I need to ") with zero structured output
// (no JSON object, no numbered/bulleted step list). Vacuous planner steps are
// NOT auto-approvable: heuristicReviewPasses returns false so ReviewStep
// routes them to full review.
func planLooksVacuous(step *task.TaskStep) bool
```

Owner: 03-plan-vacuity-gate.md. Consumer: review_manager.go heuristicReviewPasses.

### Contract 4: Report-tag-along collapse

```go
// File: internal/agent/dispatcher.go (modify collapseMultiIntentForChat, ~:2628)
// EXTEND the existing actionable==1 && chatLike>=1 collapse:
// also collapse when actionable==2 and the SECOND intent is IntentReport
// whose clause is a READBACK of the first action ("tell me/show me/what is
// the path|result|output"), detected by classifyReportTagAlong(input, reportClause)
// in the same file. F42 protection intact: a report that is a SECOND
// DELIVERABLE (its own artifact: "write a summary and a report") does NOT
// collapse - the detector requires the report clause to reference the first
// action's output, not to be an independent artifact.
func classifyReportTagAlong(input, reportClause string) bool
```

Owner: 04-report-tagalong-arbitration.md. Consumer: classifyMultiIntent.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-arg-validation.md | leaf | none | ~55K | A |
| 02 | 02-repeat-error-breaker.md | leaf | none (contract 2 self-contained) | ~60K | A |
| 03 | 03-plan-vacuity-gate.md | leaf | none | ~45K | A |
| 04 | 04-report-tagalong-arbitration.md | leaf | none | ~50K | A |

**Concurrency groups:** All four leaves are file-disjoint. Group A: dispatch
all four simultaneously (max 3 per batch - dispatch 01+02+03 first, 04 when a
slot frees, or in a second batch).

## Dispatch Protocol

### Phase 1: Dispatch Concurrency Group A

For each leaf, read it and dispatch via `delegate_task`:

1. **Read** 01-arg-validation.md and dispatch:
   - Goal: "Implement all tasks from docs/plans/20260918-tool-boundary-hardening/01-arg-validation.md"
   - Context: full leaf text + Contract 1 + coding conventions below
   - Include: "Do NOT commit. Write code, run tests, report results only."
   - Include: "Do NOT use read_file on existing source files - explore with search_files or terminal cat. After writing a file, do NOT read it back."

2. **Read** 02-repeat-error-breaker.md and dispatch (same includes).

3. **Read** 03-plan-vacuity-gate.md and dispatch (same includes).

4. **Read** 04-report-tagalong-arbitration.md and dispatch (same includes).

### Phase 2: Review and Commit Each Child

1. **Orchestrator reviews in-session** (main model, not a delegated reviewer):
   - Read changed files; check against leaf spec + contracts + Review Checklist.
   - Run the leaf's named pins verbosely and the touched-package suites.
2. **If gaps:** re-dispatch with specific feedback (max 3 cycles).
3. **If pass:** commit the leaf's files by explicit path:
   `git commit --no-verify -m "feat(tools|agent): <leaf>" -- <paths>`
   (--no-verify only because gates ran directly). Update tracking table.

### Phase 3: Integration Review

1. `go build ./...`; `go test -p 2 -count=1 ./internal/tools/... ./internal/agent/...`
2. Cross-contract check: registry-level validation (leaf 01) produces errors
   that the breaker (leaf 02) counts - add ONE cross-boundary test executing
   an invalid call 3x through Registry.Execute wrapped by the breaker.
3. Full `-race` pass before declaring COMPLETE.
4. Normalize gofmt; verify no line-number corruption; commit integration fixes.

## Review Checklist

- [ ] All tasks from each leaf implemented; named pins run verbosely green
- [ ] Interface contracts satisfied exactly (signatures, file paths)
- [ ] No per-tool hand-rolled required-arg checks REMOVED incorrectly (leaf 01 may
      leave per-tool checks as defense-in-depth; the registry gate is the boundary)
- [ ] No scope creep; no debug artifacts; no line-number corruption
- [ ] F42 protection NOT regressed (leaf 04 keeps genuine-second-deliverable reports compound)

## Coding Conventions

- Go 1.27; stdlib + existing deps only (no new modules)
- exported = PascalCase; unexported = camelCase; no stutter (tools.ToolError not tools.ToolsError)
- errors: wrap with %w; typed sentinel errors for classification; no panic in libs
- tests: table-driven, plain testing + testify assert/require as in neighbors; _test.go alongside
- gofmt clean before reporting
- AGENTS.md rules apply: typed-nil guards, setter nil-guards, no `_ = f()` ignored errors

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-arg-validation | COMPLETE 2026-09-19 | 1 | commit d35044a0; 100% (4/4 tasks, 42 wave pins green); Task-3 finding: task_create whitespace-name silent no-op fixed |
| 02-repeat-error-breaker | COMPLETE 2026-09-19 | 1 | commits (breaker primitive+wiring); 100% (3/3 tasks); lifetime chain documented: replan reuses registry.GetForTask cached loop, breaker survives |
| 03-plan-vacuity-gate | COMPLETE 2026-09-19 | 1 | commits (gate+integration); 100% (3/3 tasks); production hints {plan,architect,review} grep-verified |
| 04-report-tagalong-arbitration | COMPLETE 2026-09-19 | 1 | commits (detector+integration); 100% (3/3 tasks); 9 pre-existing collapse pins green, F42 kept |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` - clean.
2. `go test -p 2 -count=1 ./internal/tools/... ./internal/agent/...` - all ok.
3. Cross-boundary: new test in internal/agent executing an invalid-required-arg
   tool call 3x through the breaker → 4th attempt refused without invoking the tool.
4. Regression: F42 pin (dispatcher tests for genuine two-deliverable report) stays green.
5. Full `-race -count=1` on tools + agent packages.
6. Live validation (orchestrator schedules after merge): rerun
   `bash scripts/e2e-naive-user-chat.sh --keep`; expect T1 to route as single
   intent (no planner involvement) or, if compound, to abort within 3 identical
   tool errors with an honest terminal failure.

## Structural Completeness Check (Before Dispatch)

Required sections present in this orchestrator: Dispatch Protocol, Interface
Contracts, Review Checklist, Coding Conventions, Completion Tracking Table,
Integration Test Plan. Run
`python3 scripts/check_template_compliance.py docs/plans/20260918-tool-boundary-hardening --strict-leaves`
after authoring leaves.

## Notes

- Evidence base: 2026-09-18 e2e run, workdir kept at
  /var/folders/mf/1mvt9vbx7q3f9ynln1p79cfr0000gn/T/meept-e2e.p5ZmMR
  (daemon.log: 45x task_create{} with "name is required" errors each time;
  metrics.db: 66 planner LLM calls; tasks.db: vacuous approved planner step).
- task_create's schema ALREADY declares Required:["name"] - the model bypassed
  it because nothing enforced it server-side. Leaf 01 makes the schema binding.
- The nudge-cap work (4394be7d) and overflow class are prior art for error
  shapes; reuse their typed-error + pin style.
- Do not touch the nudge-cap or overflow machinery (already landed).
- Watch for sibling sessions: re-check `git log` before each commit; commit by
  explicit paths only.
