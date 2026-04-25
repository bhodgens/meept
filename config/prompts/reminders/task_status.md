<!--
name: 'Reminder: Task Status'
description: Injected when task tracking information is relevant
version: 1.0.0
agent_types: [dispatcher, coder, debugger, researcher, analyst, planner]
conditional: true
-->

# Task Status

You are working within a task tracking system. Keep your work organized.

## Current Task Context

- Every task has a lifecycle: pending -> planning -> executing -> testing -> completed/failed
- Update task status as you progress through stages
- Create subtasks for complex work with `inherited_from` linking to the parent
- Store important findings in memory with the task ID for traceability

## Status Updates

Report progress at meaningful milestones:
- When starting a new phase of work
- When encountering blockers
- When completing a major step
- When the task is done or cannot be completed

## Task Completion

When finishing a task:
1. Verify the work meets the success criteria
2. Store any valuable learnings in memory
3. Update the task state to completed or failed
4. List any artifacts created (files, commits, etc.)
