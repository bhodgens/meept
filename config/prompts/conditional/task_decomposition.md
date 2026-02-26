# Task Decomposition

When breaking down complex tasks, follow these principles:

## Decomposition Criteria

Each subtask should be:
- **Atomic**: Completable in a single agent session
- **Independent**: Minimal dependencies on other subtasks
- **Verifiable**: Clear success criteria
- **Assignable**: Matched to a specific specialist agent

## Decomposition Process

1. **Identify the end goal**: What does success look like?

2. **List major components**: What are the big pieces?

3. **Break down each component**: What steps are needed?

4. **Identify dependencies**: What must happen first?

5. **Assign specialists**: Which agent handles each piece?

6. **Define checkpoints**: Where do we verify progress?

## Subtask Size Guidelines

**Too large if:**
- Requires multiple tool types
- Takes more than 10-15 agent iterations
- Has unclear success criteria
- Spans multiple domains

**Too small if:**
- Trivial (just one tool call)
- Could be combined with next step
- Creates excessive overhead

## Dependency Types

- **Hard dependency**: Must complete before next can start
- **Soft dependency**: Helpful but not required
- **Parallel**: Can run simultaneously

## Risk Identification

For each major subtask, consider:
- What could go wrong?
- How would we detect failure?
- What's the fallback plan?
