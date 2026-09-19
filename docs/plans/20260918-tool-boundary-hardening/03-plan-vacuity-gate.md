# Plan-Vacuity Review Gate - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat. After
> writing a file, do NOT read it back to verify. Report what you built, files
> touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A planner-hint step whose result is pure narration (intent to act, no plan artifact) is NOT auto-approvable - it routes to full review.
- **Dependencies:** none
- **Estimated Context:** ~45K
- **Concurrency Group:** A
- **Audit references:** 2026-09-18 e2e (planner step APPROVED with result: "I need to create the file 'hello.txt'... I will perform the task now... Let me execute the necessary commands" - zero planning artifact; heuristic auto-approve: "trivial task, non-empty result")

## Goal

The heuristic reviewer approves a step when the task is trivial and the
result non-empty. For planner/plan-hint steps, "non-empty" is not enough: a
result that only ANNOUNCES future work ("I will...", "Let me...") with no
planning artifact (no created tasks, no step list, no structured plan output)
is vacuous - approving it lets a doomed loop continue. This leaf makes
`heuristicReviewPasses` refuse auto-approval for vacuous planner steps so
`ReviewStep` routes them to full review (the LLM reviewer, which may still
approve with justification - the gate is against SILENT auto-approval).

## Context

- `internal/agent/review_manager.go`: `ReviewStep` (~:86) →
  `policy.NeedsReview` shortcut (~:126, guard-ordered per ce8360d1) →
  `policy.ShouldAutoApprove` (~:136) → heuristic guards in
  `heuristicReviewPasses` (~:1000-1068).
- Existing guards to mirror: artifact-claims-without-evidence (~:1040),
  tool-execution-claims guard (~:1052-1066, added by 3fc9d50a). The
  claim-vs-evidence evidence check is `hasMeaningfulEvidence(step.Evidence)`.
- Tool evidence lives in `step.Evidence` (tool-issued, from executor
  tr.Evidence); narration is `step.Result` text.
- Step tool hints: `step.ToolHint` — planner steps carry hint `plan` (verify
  in internal/agent/strategic.go / createFallbackSteps what hint planner
  steps actually carry; also pair:reviewer steps).

Key files:
- `internal/agent/review_manager.go` - heuristicReviewPasses
- `internal/agent/review.go` - DefaultReviewPolicy (SkipReview includes chat/report/recall/search/analyze)
- `internal/agent/strategic.go` - what tool_hint planner steps carry

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/review_vacuity.go (new)
package agent

// planLooksVacuous reports whether a step whose TOOL HINT denotes planning
// work produced no planning artifact:
//   - zero task_create/task_update/task_list task-management tool evidence in
//     step.Evidence, AND
//   - step.Result text matches first-person future-intent narration
//     ("I will ", "I'll ", "I need to ", "Let me ", "I am going to ")
//     case-insensitive, within the first 400 chars, AND
//   - contains no structured artifact: no JSON object/array start, no
//     markdown numbered list (^\s*\d+\.) and no bullet plan (- / * at line
//     start with 2+ lines).
// Non-planner hints are unaffected (returns false).
func planLooksVacuous(step *task.TaskStep) bool
```

Integration point: in `heuristicReviewPasses`, after the existing guards:

```go
if planLooksVacuous(step) {
    rm.logger.Warn("Heuristic review: vacuous plan result (narration only); refusing auto-approve",
        "step_id", step.ID, "tool_hint", step.ToolHint)
    return false
}
```

(false from heuristicReviewPasses = route to full review, the established
semantic.)

### What This Leaf Consumes

- `task.TaskStep` (.ToolHint, .Result, .Evidence), `hasMeaningfulEvidence`
  (existing helper), the `reviewHintIsConversational` helper for the
  planner-hint set.

## Tasks

### Task 1: planLooksVacuous primitive

**Objective:** Pure predicate with table-driven pins.

**Files:**
- Create: `internal/agent/review_vacuity.go`
- Test: `internal/agent/review_vacuity_test.go`

**Step 1: Failing tests:**

```go
func TestPlanLooksVacuous_NarrationOnly(t *testing.T) {
    // hint=plan, result "I need to create the file 'hello.txt'... I will perform the task now..."
    // evidence: none -> TRUE
}
func TestPlanLooksVacuous_WithTaskCreateEvidence(t *testing.T) { // same text but Evidence carries a task_create record -> FALSE }
func TestPlanLooksVacuous_StructuredPlanText(t *testing.T) {
    // result "1. Create hello.txt\n2. Report the path" (numbered list), no evidence -> FALSE
}
func TestPlanLooksVacuous_NonPlannerHintUnaffected(t *testing.T) { // hint=code with narration text -> FALSE }
func TestPlanLooksVacuous_EmptyResultNotVacuous(t *testing.T) { // empty result -> FALSE (empty is a different failure, already handled elsewhere) }
func TestPlanLooksVacuous_ReviewHintPlannerPair(t *testing.T) { // whatever hint pair:reviewer planner steps carry -> covered }
```

Use the REAL hint value planner steps carry (grep strategic.go) - the fixture
MUST use production values, not plausible ones.

**Step 2:** FAIL. **Step 3:** Implement. Narration markers as a
case-insensitive prefix/contains scan over the first 400 chars (boundary:
slice safely, mirror the guard-first-slice-inside rule). JSON detection:
first non-space char `{` or `[`. **Step 4:** PASS.

### Task 2: Wire into heuristicReviewPasses

**Objective:** Vacuous planner steps refuse auto-approval.

**Files:**
- Modify: `internal/agent/review_manager.go` (heuristicReviewPasses, after the tool-execution-claims guard)
- Test: `internal/agent/review_vacuity_gate_test.go`

**Step 1: Failing test:**

```go
func TestReviewStep_VacuousPlanNotAutoApproved(t *testing.T) {
    // planner-hint step, narration-only result, no evidence, trivial-task policy
    // ReviewStep must NOT return ReviewApproved from the heuristic path
    // (either routes to full review per the existing heuristicRefusal semantics)
}
func TestReviewStep_SubstantivePlanStillAutoApproves(t *testing.T) {
    // planner-hint step with a real step-list result -> auto-approved (no regression)
}
```

**Step 2:** FAIL. **Step 3:** Insert the guard (Contract snippet). **Step 4:** PASS.

### Task 3: No-regression sweep

**Objective:** Existing approvals stay green.

**Files:**
- Run: existing `review_*_test.go` suites + `internal/agent` package suite

**Step 1:** Run `go test -p 2 -count=1 ./internal/agent -run 'TestReview|TestHeuristic|TestClaimsToolExecution' -v`. **Step 2:** Any regression = the predicate is too broad - narrow per the failing fixture (likely the narration-marker set or the hint set). **Step 3:** iterate until green. Pins from Task 1+2 stay green.

## Self-Verification Checklist

- [ ] Pins green verbosely; package suite green
- [ ] Fixture hint values are production-accurate (document the grep evidence)
- [ ] Predicate is conservative: false-positives cost one full review, not a wrong approval
- [ ] go build ./... clean; gofmt clean

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Narration markers match the e2e offender text exactly (pin uses it verbatim)
- [ ] Non-planner hints provably unaffected (pin)
- [ ] Structured-artifact detection (numbered list / JSON / bullets) cannot be
      defeated by a narration + one bullet
- [ ] Existing review pins unregressed

Output: APPROVED or specific gaps with file:line.

## Notes

- The e2e approved result text (use it verbatim in the pin): "I need to create
  the file 'hello.txt' in the current directory containing the word 'hello'.
  Since I have no evidence of this being done yet, I will perform the task now
  and then provide the full path. Let me execute the necessary commands to
  create the file and verify its contents."
- Deliberately NOT in scope: changing ShouldAutoApprove patterns, touching the
  LLM reviewer, or gating non-plan hints.
