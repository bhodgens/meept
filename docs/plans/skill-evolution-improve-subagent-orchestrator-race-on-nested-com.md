# Plan: Skill evolution: improve subagent-orchestrator-race-on-nested-completion

## Meta

- plan_id: plan-20260831232046-0010
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is low (0.30) with more negatives than positives; the skill description likely lacks clear guidance on when to use parallel versus sequential steps and how to assign tool hints, leading to suboptimal decompositions.

Candidate content:
# subagent-orchestrator-race-on-nested-completion

Decompose a request into discrete, executable steps that can be assigned to specialist agents.

## Steps
1. Identify the primary goal and break it into independent units of work.
2. For each step, assign a tool hint that matches the specialist type:
   - `code` or `refactor` → coding specialist
   - `debug` or `fix` → debugging specialist
   - `analyze` or `research` → analysis specialist
   - `git` or `commit` → git operations specialist
   - `plan` → further planning/decomposition
   - `chat` → general conversation
3. Determine dependencies between steps. Steps with no dependencies can run in parallel.
4. Keep the plan to a maximum of 10 steps. Be specific and actionable.
5. Output ONLY valid JSON in the exact format below.

## Output Format
```json
{
  "steps": [
    {"description": "step description", "tool_hint": "code", "depends_on": []},
    {"description": "step description", "tool_hint": "git", "depends_on": [0]}
  ]
}
```

## Notes
- Use parallel steps when tasks are independent to reduce total time.
- Only introduce a dependency when a step explicitly requires output from a previous step.
- If the request is simple, a single step may suffice.
- Always confirm the repository root or project context if needed before file operations.

## Notes

