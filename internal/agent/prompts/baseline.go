// Package prompts provides system prompt templates for agents.
package prompts

import (
	"fmt"
	"strings"
)

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

// SkillInfo holds information about a skill for prompt building.
type SkillInfo struct {
	Name        string
	Description string
	Requires    []string
	Tags        []string
	Examples    []string
}

// BuildSkillsPromptSection builds a prompt section describing available skills.
func BuildSkillsPromptSection(skills []SkillInfo) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Available Skills\n\n")
	sb.WriteString("You can invoke the following skills using the /skill-name format:\n\n")

	for _, skill := range skills {
		sb.WriteString(fmt.Sprintf("## /%s\n", skill.Name))
		if skill.Description != "" {
			sb.WriteString(fmt.Sprintf("%s\n", skill.Description))
		}

		if len(skill.Requires) > 0 {
			sb.WriteString(fmt.Sprintf("Requires: %s\n", strings.Join(skill.Requires, ", ")))
		}

		if len(skill.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(skill.Tags, ", ")))
		}

		if len(skill.Examples) > 0 {
			sb.WriteString("\nExamples:\n")
			for _, ex := range skill.Examples {
				sb.WriteString(fmt.Sprintf("  - %s\n", ex))
			}
		}

		sb.WriteString("\n")
	}

	sb.WriteString("To invoke a skill, use: /<skill-name> <input>\n")
	sb.WriteString("Example: /code-review Check my Python function for bugs\n")

	return sb.String()
}

// SkillsInstructions provides instructions for skill usage.
const SkillsInstructions = `# Skill Usage

When a user invokes a skill with /skill-name:
1. The skill is executed with its specialized instructions
2. The skill may have restricted tool access
3. Results are returned directly to the user

You can suggest skills to users when appropriate for their task.
`
