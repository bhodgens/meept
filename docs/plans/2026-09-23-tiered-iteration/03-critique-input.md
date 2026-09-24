# Leaf 3 — PlanCritiqueInput assembly

## Meta

- parent: plan-20260923-tiered-iteration
- status: planning

## Objective

Assemble the evidence base the critic consumes. The planner never gathers
this itself (single responsibility: the CALLER composes evidence; the critic
judges; the planner writes).

## PlanCritiqueInput

```go
type PlanCritiqueInput struct {
    // Prior review verdicts for similar tasks in this session. Source:
    // the review pipeline's persisted verdicts (DISCOVERY below).
    PriorReviewVerdicts []ReviewVerdictSummary
    // This-is-a-replan evidence: the failure block from 2af298b1.
    FailureBlock string
    // Registry tool names, for coverage checks on draft tool hints.
    ValidTools map[string]bool
    // Prior draft, present from critique round 2 onward.
    PriorDraft *PlanDraft
}
```

`ReviewVerdictSummary` = `{task_name string, step_desc string, verdict
string, reason string}` — bounded (last 3 verdicts, 200 chars each).

## Discovery task (first work item)

Does the review pipeline (ReviewStep) PERSIST verdicts today, or are they
ephemeral in the tactical loop? Grep `ReviewStep`, `verdict`,
`review_` in internal/agent; check the step store schema for a review column.
Two outcomes:

- Persisted → write the read API (`ReviewVerdictsForSession(sessionID)`) on
  the review side, bounded query.
- Ephemeral → add persistence to the existing step-store write path (the
  verdict already lands in the step result; extract at review time). Keep the
  read API shape identical so leaf 2 does not care which.

## Assembly rules

- FailureBlock: reuse `buildFailureBlock` (2af298b1) output directly — no
  second format.
- ValidTools: `ToolRegistry.Names()` — same source the planner's
  tool-hint validation uses (2e80c238), so critic and parse-time checks can
  never disagree about what a valid tool is.
- Tool-coverage pre-check at assembly: draft hints not in ValidTools become
  a SYNTHETIC blocking objection (no critic call needed for that class).
- Session-scoped only: verdicts come from the current session; cross-session
  retrieval is out of scope (memory system's job, later).

## Pins

- Assembly with all sources → all sections rendered in the critic prompt.
- Empty/nil sources → sections omitted (prompt never contains "null" or
  empty-list noise).
- Synthetic blocking objection for unknown tool hints.
- Review API nil-safe (no review store → zero verdicts, no error).
- Bounded: 3 verdicts × 200 chars, failure block ≤2000 chars.
