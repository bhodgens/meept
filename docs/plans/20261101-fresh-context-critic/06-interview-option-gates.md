# Interview Option Gates - Implementation Leaf

> For the implementing agent: Implement ALL tasks below. Do NOT commit.
> Do NOT git add. Do NOT edit C10 files.
> Re-activate only e2e/suites/plan-critic/interview_gates_test.go.staged.

## Meta

- Parent: master.md
- Scope: When interview_option_gates is on, interview questions may carry up to three unranked options. Flag off stays the current string list.
- Dependencies: 00T TESTS_VALIDATED
- Estimated Context: 40K
- Concurrency Group: A

## Goal

ConductInterview (strategic.go, question generation near line 414, parse at line 495) today returns a string list capped at 4. This leaf adds contract C8 behind a flag. The planner must not mark a recommended option. A `recommended` key is a parse error for that question, and the question is dropped, not defaulted.

This is the only leaf that changes planner-facing interview output, and only when the flag is on.

## Context

Flag off must keep parseInterviewQuestions behavior. Existing callers that read InterviewQuestions []string must still compile. Add a parallel field for option questions. Do not replace the string slice.

Suggested shape on the existing interview result struct (confirm the field name in strategic.go before writing; do not invent a second result type):

```go
OptionQuestions []InterviewOptionQuestion
```

```go
type InterviewOptionQuestion struct {
    Question string
    Options  []InterviewOption // len 1..3 when flag on
}
type InterviewOption struct {
    Mechanism        string
    TradeOff         string
    FailureCondition string
}
```

No Recommended field. If the model emits one, reject that question.

## Verified Anchors

- internal/agent/strategic.go:414 - question generation
- internal/agent/strategic.go:462 - parseInterviewQuestions call
- internal/agent/strategic.go:472 - cap of 4 string questions
- internal/agent/strategic.go:495 - parseInterviewQuestions

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C8 JSON shape
- SetInterviewOptionGates (C2)

### What This Leaf Consumes

- C8 from master.md
- C1 flag name interview_option_gates (read via the setter; do not edit schema.go)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add the setter and the flag-off byte-identical parse | Modify: internal/agent/strategic.go only | `go test -p 2 ./internal/agent/ -count=1 -run 'Interview'` | Flag off parse changes | Flag off still returns the string slice and leaves OptionQuestions empty | Given the flag false and a questions JSON array of strings; When parsed; Then the string slice matches today and OptionQuestions is empty |
| T2 | Parse C8 when the flag is on; reject recommended; cap options at 3 | Modify: internal/agent/strategic.go | re-activated TestInterviewFlagOffKeepsStringQuestions plus a flag-on case in the existing interview test file | recommended key becomes a default | A question with a recommended key is dropped; a question with 3 options is kept; a 4th option is truncated | Given flag on and one question with a recommended key; When parsed; Then that question is absent from OptionQuestions |

Prompt addition, flag on only: "When a design choice has more than one defensible answer, emit up to three options. Do not rank them. Do not mark a recommended option. Each option names mechanism, trade_off, and failure_condition."

Do not change the ambiguity threshold (0.6, strategic.go:160).

## Self-Verification Checklist

- [ ] Flag off string-question path unchanged
- [ ] No Recommended field on the struct
- [ ] Option cap is 3
- [ ] schema.go not edited
- [ ] No C10 file in the diff
- [ ] Only interview_gates_test.go was re-activated

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] C8 keys match exactly
- [ ] Flag off test passes
- [ ] recommended key does not select an option
- [ ] No AGENT.md edit

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not edit verifier files or AGENT.md files listed in C10. Do not edit plan_critique_loop.go.
