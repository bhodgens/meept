# Contest Path - Implementation Leaf

> For the implementing agent: Implement ALL tasks below. Do NOT commit.
> Do NOT git add. Do NOT edit C10 files.
> Re-activate only e2e/suites/plan-critic/contest_test.go.staged.

## Meta

- Parent: master.md
- Scope: Let the planner rebut a critic objection with quoted evidence. A wish to contest is not evidence.
- Dependencies: 03 REVIEWED (revise cycle shape is stable)
- Estimated Context: 45K
- Concurrency Group: D

## Goal

reviseDraft today tells the planner to address every objection (plan_critique_loop.go:612). Add an optional rebuttal block. A contest counts only when Evidence quotes a section that PlanCritiqueInput.Render() actually contained.

Contests that fail that check are tagged contested-unproven and appended to Known Risks. They are not applied as fixes and not dropped.

## Context

This is the EGM contest rule, narrowed to evidence the critic could have read. Team lore and "this was intentional" are not quotes of PlanCritiqueInput, so they fail the check.

Do not add a second critic adjudication round. One checkable quote rule is the whole leaf. A later tree can add re-presentation to the hostile child.

## Verified Anchors

- internal/agent/plan_critique_loop.go:608 - reviseDraft
- internal/agent/plan_critique_loop.go:612 - "address every objection"
- internal/agent/plan_critique_input.go:177 - Render() is the evidence text a quote must come from
- internal/agent/plan_critique_loop.go:386 - Known Risks append on exhaustion

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract C6 criticRebuttal and the quote check.

### What This Leaf Consumes

- C6 from master.md
- PlanCritiqueInput.Render from leaf 03's caller (already built before revise)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Parse rebuttals from planner revise output and accept a contest only when Evidence is a substring of the rendered critique input | Modify: internal/agent/plan_critique_loop.go | `go test -p 2 ./internal/agent/ -count=1 -run 'TestContest\|Critique'` | Any contest is applied, or none parse | A contest whose evidence is absent from Render() is not applied; one whose evidence is a verbatim substring is recorded as contested and not re-opened as blocking in the same cycle | Given an objection and a rebuttal whose evidence is not in Render(); When revise output is parsed; Then the objection remains blocking or moves to Known Risks as contested-unproven, and the draft is not treated as having answered it |
| T2 | Re-activate contest_test.go.staged and make TestContestWithoutEvidenceNotApplied pass without weakening it | e2e/suites/plan-critic/contest_test.go | `go test -p 2 -tags e2e ./e2e/suites/plan-critic/ -count=1 -run TestContestWithoutEvidenceNotApplied` | Test fails or does not compile | Test passes | Given verdict contest and an evidence string that is not in the input render; When the check runs; Then the fix is not applied |

Prompt line to add to reviseDraft, after the existing objection list:

"You may contest an objection only by quoting a sentence from the evidence sections. A wish to contest is not evidence. You wrote this draft."

Keep the flag-off and no-rebuttal path: if the planner returns only markdown, behavior stays "address every objection."

## Self-Verification Checklist

- [ ] Contest without a quote is not applied
- [ ] Contest with a verbatim quote is recorded, not silently deleted
- [ ] No C10 file in the diff
- [ ] No second model call added

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] C6 fields match exactly
- [ ] Quote check is a substring of Render(), not a model judgment
- [ ] Gate test assertion was not weakened

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not edit verifier files or AGENT.md files listed in C10.
