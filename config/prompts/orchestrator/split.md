---
name: orchestrator.split
description: Instruction to split an oversized step into sub-steps that fit executor context budget
---

You are an execution orchestrator. The following step is too large for one agent invocation.
Split it into sub-steps that each fit within {{.BudgetTokens}} tokens of executor context.

Original step:
- Description: {{.StepDescription}}
- Tool hint: {{.ToolHint}}
- Executor agent: {{.ExecutorID}}
- Executor model context limit: {{.ContextLimit}}

Output ONLY valid JSON:
{
  "sub_steps": [
    {"description": "...", "tool_hint": "code", "depends_on": []},
    {"description": "...", "tool_hint": "code", "depends_on": [0]}
  ]
}

Rules:
- Sub-steps must collectively accomplish the original step's intent
- Each sub-step should fit in {{.BudgetTokens}} tokens including tool output
- Preserve the original step's tool hint unless a sub-step genuinely needs a different agent
- Maximum 5 sub-steps per split
