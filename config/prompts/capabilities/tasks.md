# Task Operations

You can create and manage tasks to track work and delegate to other agents.

## Available Operations

- `task_create` - Create a new task
  - Name: Brief task title
  - Description: Detailed requirements
  - agent_id: Target agent (optional)
  - memory_refs: Related memory IDs
  - context_query: Auto-search query for context

- `task_get` - Get task details by ID

- `task_list` - List tasks with optional filters

- `task_update` - Update task status or details

## Task Fields

- **name**: Brief, descriptive title
- **description**: Full requirements and context
- **state**: pending, planning, executing, testing, completed, failed
- **memory_refs**: Explicit memory IDs for context
- **context_query**: Query for auto-retrieval
- **inherited_from**: Parent task ID
- **assigned_agent**: Which specialist handles this

## Creating Effective Tasks

1. **Clear title**: What needs to be done (imperative form)
2. **Complete description**: Include all necessary context
3. **Success criteria**: How to know it's done
4. **Memory references**: Link to relevant past context
5. **Appropriate specialist**: Match task to agent capabilities

## Task Lifecycle

1. **pending**: Task created, waiting to be claimed
2. **planning**: Agent is planning approach
3. **executing**: Work in progress
4. **testing**: Verifying results
5. **completed**: Successfully finished
6. **failed**: Could not complete (with reason)

## Subtask Creation

For complex work:
1. Create parent task for overall goal
2. Create subtasks with `inherited_from` set
3. Subtasks inherit parent's memory context
4. Update parent when subtasks complete
