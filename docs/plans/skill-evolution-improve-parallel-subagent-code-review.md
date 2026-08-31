# Plan: Skill evolution: improve parallel-subagent-code-review

## Meta

- plan_id: plan-20260831232037-0009
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is only 0.17 (11 positive vs 35 negative out of 65 uses). The skill is being invoked far more often than it succeeds, suggesting the trigger conditions are too broad and the output instructions need tighter specificity. I'll narrow the trigger criteria and add clearer structural guidance to reduce false positives.

Candidate content:
---
name: parallel-subagent-code-review
description: >
  Identify opportunities to run multiple independent code-review subagents in
  parallel across distinct files or subsystems. Only trigger when the input
  contains multiple clearly separable review targets (e.g., different files,
  modules, or unrelated concerns) that can be reviewed independently without
  cross-dependencies.
triggers:
  - "review", "code-review", "check", "analyze" combined with multiple distinct targets
  - Multi-file changesets where each file/module can be evaluated separately
  - Explicit request for parallel or distributed review
non-triggers:
  - Single-file changes
  - Sequentially dependent review tasks
  - General code questions or non-review requests
  - Refactoring, debugging, or feature-implementation requests
output_format:
  format: json
  schema:
    review_tasks:
      type: array
      description: List of parallel review tasks, one per independently reviewable unit
      items:
        target: string
          description: File path, module name, or subsystem identifier
        focus_area: string
          description: What aspect to review (bugs, style, performance, security, logic)
        instructions: string
          description: Specific review instructions for this target
        depends_on: []
          description: Empty array — all tasks are independent and parallel
  constraints:
    max_tasks: 5
    minimum_separability: tasks must have no shared mutable state or sequential dependency
examples:
  - input: "Review these three files: auth.go, payment.go, and search.go"
    output:
      review_tasks:
        - {target: "auth.go", focus_area: "security and authentication logic", instructions: "Check for auth bypasses, token handling, and session management", depends_on: []}
        - {target: "payment.go", focus_area: "transaction correctness", instructions: "Verify idempotency, error handling, and refund logic", depends_on: []}
        - {target: "search.go", focus_area: "performance and correctness", instructions: "Check query handling, edge cases, and pagination", depends_on: []}
  - input: "Fix the bug in user-service"
    output: skip
behavior:
  - If fewer than 2 independent review targets exist, return skip
  - Each task must be truly parallelizable — no cross-task dependencies
  - Keep instructions specific and actionable for a coding specialist agent
  - Do not review the same file twice in parallel
usage:
  inject_count: 65
  positive: 11
  negative: 35
  neutral: 3
  effectiveness: 0.17
---


## Notes

