# Multi-Agent Orchestration

## Overview
Meept uses a multi-agent architecture where specialist agents handle different task types. The dispatcher agent routes incoming requests to appropriate specialists based on task requirements and agent capabilities.

## Problem
Single-agent systems struggle with complex tasks requiring different expertise. Multi-agent orchestration enables:
- Task decomposition into specialized subtasks
- Dynamic agent discovery and delegation
- Collaborative planning with review workflows
- Efficient resource allocation

## Behavior

### Agent Architecture
| Agent ID | Role | Purpose |
|----------|------|---------|
| `dispatcher` | Dispatcher | Intake, classify, route to specialists |
| `chat` | Executor | General conversation |
| `coder` | Executor | File ops, shell, coding tasks |
| `debugger` | Executor | Troubleshooting, bug fixing |
| `planner` | Executor | Task decomposition, planning |
| `analyst` | Executor | Research, data analysis |
| `committer` | Executor | Git operations |
| `scheduler` | Executor | Job scheduling |

### Task Flow
1. **Intake**: Dispatcher receives user request
2. **Classification**: Dispatcher analyzes task requirements
3. **Memory Search**: Relevant context retrieved from memory
4. **Agent Discovery**: Available specialists identified via `platform_agents`
5. **Delegation**: Task routed via `delegate_task`
6. **Execution**: Specialist agent performs work with evidence collection
7. **Validation**: Evidence verified against claims (Deterministic Execution)
8. **Review**: Optional collaborative review workflow
9. **Report Routing**: `ReportRouter` determines next action (close, handoff, notify user, or error)
10. **Completion**: Results returned to user

### Report Router (Multi-Agent Handoff)

When an agent completes, the `ReportRouter` examines its structured report and decides what to do next. This replaces the previous behavior where routing decisions were computed but never acted on.

**Route actions:**

| Action | Behavior |
|--------|----------|
| `RouteActionClose` | Agent finished. Format response from accomplishments and observations. |
| `RouteActionRoute` | Hand off to the next suggested agent. Context accumulates across handoffs. |
| `RouteActionNotifyUser` | User input needed. Force notification to all session participants. |
| `RouteActionNotifyError` | Agent failed. Force notification with error details. |

**Properties:**
- **Max depth: 5** — prevents infinite agent-to-agent loops. After 5 handoffs, forces user notification.
- **Context accumulation** — each handoff passes the previous agent's `Accomplished`, `Issues`, `Observations`, and `DecisionContext` to the next agent.
- **Single response** — the caller receives one final synthesized response, not N intermediate ones.

### Collaborative Planning
- **Review/Approval Workflow**: Tasks can require reviewer approval
- **Revision Cycles**: Agents can iterate based on feedback
- **Auto-Approve Patterns**: Simple tasks approved automatically

### Coworker Awareness
Agents discover each other dynamically:
- `platform_agents`: List available agents and capabilities
- `platform_tools`: List registered tools
- `delegate_task`: Route tasks to specific agents

## Configuration

```toml
[multiagent]
enabled = true
dispatcher_model = "claude-opus-4-5-20251101"
default_model = "claude-sonnet-4-5-20241022"
max_memory_refs = 20
context_search_limit = 10

[agents]
enabled = true
config_dirs = ["~/.meept/agents", "config/agents"]
prompts_dir = "config/prompts"
default_model = ""
dispatcher_id = "dispatcher"

[collaborative]
enabled = true
reviewer_mapping = {}
auto_approve_simple = true
max_revision_cycles = 3
```

## Observability

### Logging
- Agent delegation events
- Task routing decisions
- Memory context injection
- Review workflow state changes
- Report router decisions (action, agent, depth, has_report)
- Multi-agent handoff events (from/to/depth)

### Metrics
- Agent utilization rates
- Task completion times
- Memory hit rates
- Review approval rates
- Multi-agent handoff depth per conversation
- Route action distribution (close vs route vs notify vs error)

### Debug Info
- Current agent assignments
- Task queue status
- Memory context relevance scores
- Review workflow state
- Current routing depth per active handoff chain

## Edge Cases

### No Suitable Agent
- Dispatcher returns "no specialist available"
- Suggests manual agent selection
- Logs capability gap for monitoring

### Agent Unavailable
- Task queued for retry
- Alternative agents considered
- User notified of delay

### Memory Context Missing
- Dispatcher proceeds with limited context
- Logs missing context warning
- Subsequent tasks may benefit from current execution

### Review Cycle Limit
- Maximum revision cycles enforced
- Final decision forced after limit
- User notified of resolution

### Max Route Depth Exceeded
- `ReportRouter` forces `RouteActionNotifyUser` after 5 handoffs
- Accumulated response includes what each agent accomplished
- Warning logged with depth and max depth values

### Agent Reports No Suggested Next Agent
- `RouteActionRoute` requires `SuggestedNextAgent` in the report
- Falls back to `RouteActionClose` if missing