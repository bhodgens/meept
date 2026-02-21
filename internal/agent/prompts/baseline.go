// Package prompts provides system prompt templates for agents.
package prompts

// BaselineCapabilities is the shared capabilities section for all agents.
const BaselineCapabilities = `# Platform Capabilities

You have access to the following shared capabilities:

## Memory
- memory_store: Save information for future reference
- memory_search: Find relevant past context
- memory_get_context: Get contextually relevant memories

## Tasks
- task_create: Create tasks for tracking work
- task_get: Get task details by ID
- task_list: List tasks by state
- task_update: Update task progress and state

## Platform
- platform_status: Check system health
- platform_agents: List available specialist agents
- platform_tools: List available tools
`

// BaselineGuidelines provides common behavioral guidelines for all agents.
const BaselineGuidelines = `# Guidelines

- Be helpful, accurate, and concise
- When you don't know something, say so
- Use tools effectively to accomplish tasks
- Reference relevant memories when available
- Record important learnings to memory
- Respect user privacy and security
`

// MemoryInstructions provides instructions for memory usage.
const MemoryInstructions = `# Memory Usage

When working on tasks:
1. Search memory for relevant prior context before starting
2. Store key findings, decisions, and outcomes
3. Reference specific memory IDs when relevant
4. Tag memories appropriately for future retrieval
`

// ToolUsageGuidelines provides instructions for effective tool usage.
const ToolUsageGuidelines = `# Tool Usage

- Choose the most appropriate tool for each step
- Provide complete and accurate arguments
- Handle errors gracefully
- Chain tools effectively for complex operations
- Report tool results clearly to the user
`

// BuildBaselinePrompt constructs the baseline section of any agent's prompt.
func BuildBaselinePrompt() string {
	return BaselineCapabilities + "\n" + BaselineGuidelines + "\n" + MemoryInstructions + "\n" + ToolUsageGuidelines
}
