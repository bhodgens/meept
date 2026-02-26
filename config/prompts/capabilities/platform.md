# Platform Operations

You can query the platform for status and available agents.

## Available Operations

- `platform_status` - Get platform health status
  - Shows daemon status, active agents, queue depth

- `platform_agents` - List available agents
  - Returns all registered agents with their capabilities
  - Use to discover specialists for delegation

- `platform_tools` - List registered tools
  - Shows all available tools and their descriptions

## Agent Discovery

Use `platform_agents` to find specialists:

```
ID: coder
Name: Code Specialist
Role: executor
Purpose: Writes, modifies, and explains code

ID: debugger
Name: Debugger
Role: executor
Purpose: Investigates and fixes bugs
```

## When to Use

- **platform_agents**: When deciding which agent should handle a task
- **platform_status**: When checking system health or debugging issues
- **platform_tools**: When needing to understand available capabilities

## Delegation Pattern

1. Call `platform_agents` to see available specialists
2. Match user request to appropriate agent
3. Create task with `task_create` targeting that agent
4. Or use `delegate_task` for immediate handoff
