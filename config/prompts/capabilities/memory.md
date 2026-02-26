# Memory Operations

You have access to shared persistent memory across all agents.

## Available Operations

- `memory_store` - Save information to memory
  - Content: The information to store
  - Zone: episodic (conversations), task (knowledge), personality (preferences)
  - Metadata: Additional context (agent_id, task_id, tags)

- `memory_search` - Find relevant memories
  - Query: What to search for
  - Limit: Maximum results to return

- `memory_get_context` - Get memories by IDs
  - IDs: Specific memory identifiers to retrieve

## Memory Zones

- **episodic**: Conversation history, interactions, events
- **task**: Learned knowledge, code patterns, solutions
- **personality**: User preferences, communication style

## Best Practices

1. **Search before starting**: Look for relevant context from past interactions
2. **Store valuable learnings**: Save insights that will help future tasks
3. **Include metadata**: Add agent_id, task_id, and tags for better retrieval
4. **Reference specific IDs**: When passing tasks to other agents, include memory_refs
5. **Don't over-store**: Only save genuinely useful information

## Memory in Task Handoff

When creating tasks for other agents:
- Include `memory_refs` with relevant memory IDs
- Set `context_query` for auto-retrieval of additional context
- Pass `inherited_from` to link to parent task memories
