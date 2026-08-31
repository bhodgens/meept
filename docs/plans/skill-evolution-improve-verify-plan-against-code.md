# Plan: Skill evolution: improve verify-plan-against-code

## Meta

- plan_id: plan-20260831224809-0044
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.00 with a negative feedback signal — the skill likely lacks clear verification criteria, structured output, and concrete checking steps. Improving it with explicit methodology and expected output format should increase usefulness.

Candidate content:
# Verify Plan Against Code

## Purpose
Verify that an implementation plan accurately maps to the actual codebase, ensuring no gaps, mismatches, or hallucinated code locations/references.

## When to Use
- After receiving an implementation plan before starting work
- When a developer suspects a plan may not align with existing code
- Before approving a plan for execution

## Verification Steps
1. **Locate Relevant Files**: Identify all files referenced in the plan using search or direct path lookup.
2. **Check File Existence**: Confirm each referenced file exists at the stated path.
3. **Verify Plan References**:
   - Are class/function names accurate and present?
   - Do the described behaviors match actual implementations?
   - Are line numbers or sections approximately correct?
4. **Identify Gaps**: Flag any plan elements that reference non-existent code or suggest changes to non-existent structures.
5. **Validate Assumptions**: Check if the plan assumes code patterns, dependencies, or configurations not present in the repo.

## Output Format
Return a structured report:

```
Plan Verification Report
========================

Status: PASS / FAIL / NEEDS_REVISION

Verified Items:
- [file/path]: Accurate / Inaccurate / Not Found

Gaps Identified:
- [description of missing or incorrect references]

Recommendations:
- [specific suggestions to correct the plan]
```

## Tips
- Use `grep`, `rg` (ripgrep), or IDE search to locate references efficiently.
- Do not assume the plan is wrong — verify objectively before reporting issues.
- If uncertainty exists, flag it rather than declaring a definitive failure.


## Notes

