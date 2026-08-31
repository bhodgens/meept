# Plan: Skill evolution: improve meept-subagent-context-architecture

## Meta

- plan_id: plan-20260831232028-0008
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.17 (1 positive out of 6 uses). The skill appears to be used as a task decomposition/planning tool but its current wiki entry describes it as a 'two-turn turn-taking pattern for interactive code problems' — a mismatch. The execution traces show successful task decomposition, suggesting the skill content needs to be realigned with its actual use case: decomposing requests into discrete, executable steps with tool hints and dependency ordering.

Candidate content:
# Task Decomposition Planner

Decompose user requests into discrete, executable steps that can be assigned to specialist agents.

## When to Use

Use this skill when a request needs to be broken down into a plan before execution. Common scenarios:
- Multi-step tasks requiring coordination between specialists
- Requests that involve both planning and execution phases
- Ambiguous or complex requests that benefit from upfront decomposition

## Step Format

Each step must include:
- **description**: A single unit of work, specific and actionable
- **tool_hint**: The agent type best suited for the step:
  - `code` or `refactor` → coding specialist
  - `debug` or `fix` → debugging specialist
  - `analyze` or `research` → analysis specialist
  - `git` or `commit` → git operations specialist
  - `plan` → further planning/decomposition
  - `chat` → general conversation
- **depends_on**: 0-based indices of prerequisite steps (empty array for parallel-start steps)

## Constraints

- Keep plans to **10 steps maximum**
- Steps with empty `depends_on` can run in parallel
- Be specific and actionable — avoid vague descriptions
- Output ONLY valid JSON, no markdown, no explanation

## Output Format

```json
{
  "steps": [
    {"description": "step description", "tool_hint": "code", "depends_on": []},
    {"description": "step description", "tool_hint": "code", "depends_on": [0]},
    {"description": "step description", "tool_hint": "git", "depends_on": [0, 1]}
  ]
}
```

## Example

Request: "Write a file named summary.txt in the repository root containing the text: meept is an AI agent daemon."

Plan:
```json
{
  "steps": [
    {"description": "Get project info to confirm the repository root directory", "tool_hint": "plan", "depends_on": []},
    {"description": "Write summary.txt to the repository root with content 'meept is an AI agent daemon.' using file_write with direct:true", "tool_hint": "code", "depends_on": [0]}
  ]
}
```


## Notes

