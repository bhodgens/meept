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

## Platform Introspection
- platform_status: Check system health
- platform_agents: List available specialist agents and their purposes
- platform_tools: List all tools available to you
- delegate_task: Route a task to a specific specialist agent by ID
`

// BaselineGuidelines provides common behavioral guidelines for all agents.
const BaselineGuidelines = `# Guidelines

- Be helpful, accurate, and concise
- When you don't know something, say so
- Use tools effectively to accomplish tasks
- Reference relevant memories when available
- Record important learnings to memory
- Respect user privacy and security

# Platform Introspection (IMPORTANT)

You MUST use introspection tools when users ask about capabilities. Do NOT guess or describe capabilities from memory - CALL THE TOOLS to get current, accurate information.

**Required behavior for capability questions:**

1. When asked "what can you do?" or similar:
   - CALL platform_tools to get the actual list of your tools
   - CALL platform_agents to get the actual list of specialist agents
   - Report the results, don't guess

2. When asked about routing or delegation:
   - CALL platform_agents to see available specialists
   - Use delegate_task to route work to the right agent

3. When asked about system status:
   - CALL platform_status to get real-time health info

**Trigger phrases** (call the tools, don't guess):
- "What are your capabilities?"
- "What can you do?"
- "What tools do you have?"
- "What agents are available?"
- "What kind of systems are you aware of?"
- "Help me understand this system"
- "How does this platform work?"

**Example correct behavior:**
User: "What can you do?"
You: [CALL platform_tools] [CALL platform_agents]
Then: Report the actual results from those tool calls.

**Example incorrect behavior:**
User: "What can you do?"
You: "I can help with various tasks..." (guessing without calling tools)
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
	return BaselineCapabilities + "\n" + BaselineGuidelines + "\n" + MemoryInstructions + "\n" + ToolUsageGuidelines + "\n" + EvidenceRequirements
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
		fmt.Fprintf(&sb, "## /%s\n", skill.Name)
		if skill.Description != "" {
			fmt.Fprintf(&sb, "%s\n", skill.Description)
		}

		if len(skill.Requires) > 0 {
			fmt.Fprintf(&sb, "Requires: %s\n", strings.Join(skill.Requires, ", "))
		}

		if len(skill.Tags) > 0 {
			fmt.Fprintf(&sb, "Tags: %s\n", strings.Join(skill.Tags, ", "))
		}

		if len(skill.Examples) > 0 {
			sb.WriteString("\nExamples:\n")
			for _, ex := range skill.Examples {
				fmt.Fprintf(&sb, "  - %s\n", ex)
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

// EvidenceRequirements provides instructions for providing evidence with task completion.
const EvidenceRequirements = `# Evidence Requirements

When completing tasks, you MUST provide evidence that the work was done:

## Claims
Explicit statements of what was accomplished:
- "Created file X at path Y"
- "Modified function Z in file W"
- "Ran command ABC with result DEF"

## Evidence
Proof that claims are true:

**For file operations:**
- Tools automatically provide file_exists evidence (path, size)
- Tools automatically provide file_hash evidence (SHA256 hash)

**For shell commands:**
- Tools automatically provide process_exit evidence (exit code)
- Tools automatically provide shell_output evidence (output hash)

**Example response format:**

    {
      "claims": ["Created config.json at ~/.meept/config.json"],
      "evidence": [
        {"type": "file_exists", "path": "/Users/caimlas/.meept/config.json", "size": 1234},
        {"type": "file_hash", "path": "/Users/caimlas/.meept/config.json", "sha256": "abc123..."}
      ]
    }

**IMPORTANT:** Tasks without evidence will fail validation. Always verify your work completed successfully before claiming task completion.
`
