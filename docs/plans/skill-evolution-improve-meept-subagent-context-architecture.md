# Plan: Skill evolution: improve meept-subagent-context-architecture

## Meta

- plan_id: plan-20260831224728-0041
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is critically low (0.17) with 5 negative outcomes out of 6 injections, indicating the skill's current guidance is mismatched to agent needs. The skill likely lacks clear decision criteria for when to invoke it and what outputs to produce.

Candidate content:
# meept-subagent-context-architecture

## Purpose
Design and orchestrate the context-passing architecture between a Meept agent and its subagents to maximize task completion accuracy while minimizing unnecessary token usage.

## When to Use
Invoke this skill ONLY when:
1. A task requires decomposing into multiple subagent calls
2. Subagents need shared context (project state, prior results, tool outputs)
3. The agent must coordinate subagent results into a coherent final output

**Do NOT invoke** for single-turn, self-contained queries that don't require subagent delegation.

## Context Architecture Patterns

### Pattern 1: Cascading Context
Use when subagents depend on each other's outputs sequentially.
- Subagent 1 receives full task brief + any relevant shared context
- Subagent 2 receives task brief + Subagent 1's results + any other relevant context
- Each subagent gets only the context it needs, not everything

### Pattern 2: Parallel Fan-out
Use when subagents are independent and can run concurrently.
- All subagents receive the same core task brief
- Each subagent gets only the context relevant to its specific subtask
- Results are aggregated by the parent agent

### Pattern 3: Router + Workers
Use when the task scope is unclear and needs triage first.
- A router subagent classifies the request and assigns subtasks
- Worker subagents execute assigned subtasks with appropriate context
- Results are synthesized by the parent agent

## Subagent Prompt Template
```
You are a subagent specializing in: [subtask description]

Your parent task: [brief description of overall goal]

Context you need to know:
[relevant shared context, prior results, tool outputs]

What you must produce:
[specific deliverable format]

Guidelines:
[Any constraints, style, or quality requirements]
```

## Output Format
When producing context architecture recommendations, return:
1. **Pattern selected** and why
2. **Context map**: which subagent gets which pieces of context
3. **Prompt template** for each subagent invocation
4. **Aggregation strategy** for combining subagent outputs

## Common Pitfalls to Avoid
- Don't dump all available context into every subagent prompt
- Don't create unnecessary subagents for trivial subtasks
- Don't have subagents re-derive context the parent already has
- Always specify the output format a subagent must return

## Examples

### Good Use Case
Task: "Migrate our monolith to microservices"
- Router subagent: decompose into service boundaries
- Worker subagent 1: analyze database schema for splitting
- Worker subagent 2: review existing API surface
- Parent: synthesize migration plan from all results

### Bad Use Case
Task: "What's 2+2?"
- No subagent needed; answer directly.


## Notes

