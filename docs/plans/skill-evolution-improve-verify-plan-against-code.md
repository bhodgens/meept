# Plan: Skill evolution: improve verify-plan-against-code

## Meta

- plan_id: plan-20260831232108-0012
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.00 with 0 positive and 1 negative result from 9 injections, indicating the skill is not producing useful verifications. Need to strengthen it with clearer verification criteria, explicit checks against known failure modes, and structured output for downstream use.

Candidate content:
---
name: verify-plan-against-code
trigger: After any multi-step coding plan executes, or when explicitly asked to verify plan-to-code alignment.
description: >
  Verifies that the executed code changes faithfully implement the original plan/steps,
  checking for missing requirements, incorrect outputs, wrong file paths, and deviations
  from tool hints or constraints (e.g., direct:true, content exactness).
---

# Plan Verification Skill

## When to invoke

- After a multi-step plan completes execution
- When user asks "did the plan execute correctly?"
- Before declaring a task fully complete
- When a downstream consumer needs confidence the plan's claims are accurate

## Verification checklist

For each step in the original plan, verify:

1. **Step was executed**: Confirm the step has a non-empty `assistant_response` in the trace
2. **Output file exists**: If the step writes a file, check `file_exists` evidence
3. **Content matches**: Compare actual file content against the plan's declared content
4. **Path is correct**: Verify the file was written to the expected directory (repo root unless overridden)
5. **Constraints honored**: Check tool hints, flags (`direct:true`), and other constraints from the step
6. **No extra/missing files**: Ensure no unintended files were created or existing ones modified incorrectly
7. **SHA256 consistency**: If hashes are provided, verify they match actual file hashes

## Structured output format

Always return JSON:

```json
{
  "plan_verified": true | false,
  "verified_steps": [0, 1, 2],
  "failed_steps": [],
  "issues": [
    {
      "step_index": 0,
      "issue_type": "missing_file | content_mismatch | wrong_path | constraint_violated | incomplete",
      "expected": "...",
      "actual": "..."
    }
  ],
  "confidence": 0.0-1.0,
  "recommendation": "proceed | fix_step_N | re-run"
}
```

## Failure detection patterns

| Pattern | Detection | Action |
|---|---|---|
| File not written | `file_exists` false | Flag as missing, recommend re-write |
| Content mismatch | Hash or string diff | Flag with expected vs actual |
| Wrong path | Path differs from plan | Flag, recommend relocation |
| Missing tool constraint | Tool hint or flag not honored | Flag constraint violation |
| Incomplete trace | No `assistant_response` or summary | Flag as incomplete, recommend re-execution |
| Phantom files | Files exist but not in plan | Flag as unexpected side-effect |

## Usage notes

- Read the original plan from the task context or user prompt
- Read the execution traces for all steps
- Read the final summary and claims sections
- Cross-reference claims against actual file evidence
- Be strict on exact content matches (whitespace, newlines matter)
- If confidence < 0.8, always report the recommendation to re-run


## Notes

