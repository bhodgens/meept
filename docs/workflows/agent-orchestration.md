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
7. **Dynamic Handoff**: Agent may call `request_handoff` to inject a new step for another specialist mid-task
8. **Validation**: Evidence verified against claims (Deterministic Execution)
9. **Review**: Optional collaborative review workflow
10. **Report Routing**: `ReportRouter` determines next action (close, handoff, notify user, or error)
11. **Completion**: Results returned to user

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
- `delegate_task`: Route tasks to specific agents (synchronous, blocking)
- `request_handoff`: Dynamically inject a new step and route to another agent (async, non-blocking)

### Dynamic Agent Handoff

When an agent discovers mid-execution that it needs expertise from another specialist, it can use `request_handoff` to dynamically inject a new step into the running task's DAG without going through the dispatcher.

**How it works:**
1. Agent calls `request_handoff` with target agent ID, description, reason, and partial results
2. Tool publishes `orchestrator.handoff` bus event via `models.NewBusMessage()`
3. `Orchestrator.handleHandoff()` receives the event and delegates to `TacticalScheduler.HandleHandoff()`
4. `HandleHandoff` creates a new `TaskStep` with dependency on the originating step
5. Downstream steps are rewired to depend on the injected step (when `inject_after` is true)
6. New step is promoted and scheduled via the existing step lifecycle

**Amendment integration:**
When `HandoffUseAmendment` is enabled and an `AmendmentSubmitter` is configured, handoff requests route through the amendment system for review/approval before step creation. If the amendment is rejected, the handoff fails. If `HandoffUseAmendment` is true but no `AmendmentSubmitter` is available, it falls through to direct creation.

**Rate limiting:**
`MaxHandoffSteps` (default 5) limits the number of handoff steps per task to prevent runaway handoff chains.

**Implementation:**
- Tool: `internal/tools/builtin/handoff.go` — `RequestHandoffTool` struct
- Handler: `internal/agent/tactical.go` — `HandleHandoff()`, `rewireDownstreamDeps()`, `agentIDToToolHint()`
- Wiring: `internal/agent/orchestrator.go` — subscribes to `orchestrator.handoff`
- Registration: `internal/daemon/components.go` — registers tool with bus and agent existence check

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