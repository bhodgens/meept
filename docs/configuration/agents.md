# Agent Configuration

Meept uses a multi-agent system where specialist agents handle different types of tasks. Agents are configured through TOML-based definitions and YAML frontmatter.

## Agent System Configuration

Enable and configure the multi-agent system in `~/.meept/meept.toml`:

```toml
[agents]
enabled = false
config_dirs = ["~/.meept/agents", "config/agents"]
prompts_dir = "config/prompts"
default_model = ""
dispatcher_id = "dispatcher"
```

### Configuration Options

- **enabled**: Enable/disable the multi-agent system
- **config_dirs**: Directories to search for agent definition TOML files (searched in order)
- **prompts_dir**: Base directory for prompt components
- **default_model**: Fallback model for agents without specific model configuration
- **dispatcher_id**: Agent ID that handles intake and routing

## Agent Definition Format

Agents are defined using `AGENT.md` files with YAML frontmatter:

```yaml
---
id: coder
name: Code Specialist
role: executor
additional_tools:
  - file_read
  - file_write
  - file_delete
  - list_directory
  - shell_execute
capabilities:
  - code
  - reasoning
max_iterations: 15
timeout_seconds: 600
max_tokens_per_turn: 4096
max_memory_refs: 20
temperature: 0.3
---
```

### Agent Properties

- **id**: Unique identifier for the agent
- **name**: Human-readable display name
- **role**: Agent role (`executor`, `dispatcher`, etc.)
- **additional_tools**: List of tools this agent can access
- **capabilities**: Required model capabilities (`code`, `reasoning`, `tool_use`, etc.)
- **max_iterations**: Maximum agent loop iterations
- **timeout_seconds**: Maximum execution time
- **max_tokens_per_turn**: Maximum tokens per conversation turn
- **max_memory_refs**: Maximum memory references per turn
- **temperature**: LLM temperature setting

## Discovery Hierarchy

Agents are discovered through a 4-tier priority system:

1. **Project-local** (highest priority): `.meept/skills/`
2. **User-global**: `~/.meept/skills/`
3. **System-wide**: `~/.config/meept/skills/`
4. **Third-party**: `~/.meept/clawskills/` (claw: prefix)

Later directories override earlier ones in case of conflicts.

## Global Rules

All agents follow the global rules defined in `config/RULES.md`:

### Post-Execution Report

Every agent response must include a structured JSON report:

```json
{
  "status": "completed|partial|failed|needs_input",
  "accomplished": ["list of what you completed"],
  "not_done": ["list of what you did not complete"],
  "issues": ["problems encountered"],
  "observations": ["context for follow-up work"],
  "suggested_next_agent": "agent-id or empty",
  "user_decision_needed": true,
  "decision_context": "what the user needs to decide"
}
```

### Quality Guidelines

1. **Read before writing** - Always examine existing code/content first
2. **Minimal changes** - Make the smallest change that works
3. **Verify your work** - Check that changes work as intended
4. **Follow conventions** - Respect project patterns and style
5. **Document decisions** - Explain non-obvious choices

## Agent Constraints

Each agent can be configured with operational constraints:

- **max_iterations**: Limits agent loop iterations (prevents infinite loops)
- **timeout_seconds**: Maximum execution time before timeout
- **temperature**: Controls creativity vs. determinism
- **max_memory_refs**: Limits memory context size

## Reviewer Mapping

The review system can map agents to specific reviewers:

```toml
[review]
reviewer_mapping = {
  coder = "code-reviewer",
  debugger = "debug-reviewer",
  planner = "planner-reviewer",
  analyst = "analyst-reviewer",
  committer = "code-reviewer"
}
```

## Available Agents

Meept includes several built-in specialist agents:

- **dispatcher**: Intake, classification, and routing
- **chat**: General conversation
- **coder**: File operations, shell, coding tasks
- **debugger**: Troubleshooting, bug fixing
- **planner**: Task decomposition, planning
- **analyst**: Research, data analysis
- **committer**: Git operations
- **scheduler**: Job scheduling

## Example Agent Definition

```yaml
---
id: analyst
name: Research Specialist
role: executor
additional_tools:
  - memory_search
  - web_search
capabilities:
  - reasoning
max_iterations: 10
timeout_seconds: 300
max_tokens_per_turn: 8192
max_memory_refs: 15
temperature: 0.7
---

# Research Specialist

You analyze information, conduct research, and provide insights.

## Core Principles

1. **Thorough investigation** - Explore multiple sources
2. **Critical thinking** - Evaluate information credibility
3. **Clear communication** - Present findings clearly
4. **Context awareness** - Consider broader implications
```