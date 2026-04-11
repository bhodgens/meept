# Global Rules

These rules apply to all agents in the meept platform.

## Post-Execution Report (REQUIRED)

At the end of every response where you performed work, include a structured report in a JSON code block. This enables the dispatcher to route follow-ups or notify the user appropriately.

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

### Status Values

- **completed**: All requested work is done
- **partial**: Some work done, more needed
- **failed**: Could not complete the task
- **needs_input**: Blocked waiting for user decision

### When to Set Fields

- **accomplished**: Always list concrete things you did
- **not_done**: List if status is partial or needs_input
- **issues**: List any errors, blockers, or concerns
- **observations**: Include context that would help a follow-up agent
- **suggested_next_agent**: Set when another specialist should continue
- **user_decision_needed**: Set true when you need user input to proceed
- **decision_context**: Explain what decision is needed

### Examples

**Completed task:**
```json
{
  "status": "completed",
  "accomplished": ["Fixed the null pointer exception in user.go", "Added test coverage"],
  "user_decision_needed": false
}
```

**Needs follow-up:**
```json
{
  "status": "partial",
  "accomplished": ["Identified root cause of bug"],
  "not_done": ["Implement fix", "Update tests"],
  "suggested_next_agent": "coder",
  "user_decision_needed": false
}
```

**Needs user input:**
```json
{
  "status": "needs_input",
  "accomplished": ["Found two possible approaches"],
  "user_decision_needed": true,
  "decision_context": "Should we prioritize performance (option A) or simplicity (option B)?"
}
```

## Quality Guidelines

1. **Read before writing** - Always examine existing code/content before modifying
2. **Minimal changes** - Make the smallest change that accomplishes the goal
3. **Verify your work** - Check that changes work as intended
4. **Follow conventions** - Respect existing project patterns and style
5. **Document decisions** - Note why you chose a particular approach
