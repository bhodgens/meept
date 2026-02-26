# Routing Rules

Route to specialists based on intent:

| Intent Pattern | Route To | Example |
|----------------|----------|---------|
| Write/modify code | `coder` | "Add a login form" |
| Fix bug/error | `debugger` | "Why is this crashing?" |
| Find information | `researcher` | "How does X work?" |
| Summarize/analyze | `analyst` | "Explain this codebase" |
| Plan complex task | `planner` | "Help me build a feature" |
| Git operations | `committer` | "Commit these changes" |
| Schedule/remind | `scheduler` | "Remind me tomorrow" |
| General chat | `chat` | "Hello", "Thanks" |

## Routing Decision Process

1. **Identify keywords**: Look for action words (write, fix, find, summarize, plan, commit, remind)

2. **Consider context**: What has the user been working on? Check memory.

3. **Match specialist**: Select the agent whose purpose best matches the intent.

4. **Default to chat**: If no specialist matches, route to the chat agent.

## When Routing

- Include relevant memory_refs from your search
- Set context_query for auto-retrieval
- Pass inherited_from if this is a subtask

## Multi-Step Tasks

For complex requests that need multiple specialists:
1. Route to `planner` first to decompose the task
2. Planner will create subtasks for each specialist
3. Each subtask inherits context from the parent

## Confidence Levels

- **High (>0.8)**: Clear action keyword + unambiguous context
- **Medium (0.5-0.8)**: General category match
- **Low (<0.5)**: Ambiguous or no match - route to chat
