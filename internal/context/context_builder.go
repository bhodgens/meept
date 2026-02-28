package context

import (
	"fmt"
	"strings"
)

// ContextBuilder builds context from artifacts for agent prompts
type ContextBuilder struct {
	artifacts *Artifacts
}

// NewContextBuilder creates a new context builder
func NewContextBuilder(artifacts *Artifacts) *ContextBuilder {
	return &ContextBuilder{
		artifacts: artifacts,
	}
}

// BuildContext represents the context built for a task
type BuildContext struct {
	TaskType        string
	TaskDescription string
	WorkingDir      string

	// Available context
	HasCLAUDEMD     bool
	HasClaudeDir    bool
	HasSkills       bool

	// Relevant sections
	RelevantSections []string

	// Extracted information
	Commands         []BuildCommand
	Architecture     *ArchitectureSection
	Components       []ComponentMapping
	Agents          []AgentDefinition
	Skills          []*Skill
	BuildInfo       string
	ArchitectureInfo string
	AgentInfo       string
	SkillInfo       string
	ProjectOverview string
}

// BuildForTask builds context for a specific task
func (cb *ContextBuilder) BuildForTask(task string) *BuildContext {
	ctx := &BuildContext{
		TaskType:        cb.classifyTask(task),
		TaskDescription: task,
		WorkingDir:      cb.artifacts.WorkingDir,
		HasCLAUDEMD:     cb.artifacts.HasCLAUDEMD(),
		HasClaudeDir:    cb.artifacts.HasClaudeDir(),
		HasSkills:       cb.artifacts.HasSkills(),
	}

	// Extract relevant information based on task type
	switch ctx.TaskType {
	case "build":
		cb.buildBuildContext(ctx)
	case "test":
		cb.buildTestContext(ctx)
	case "code":
		cb.buildCodeContext(ctx)
	case "architecture":
		cb.buildArchitectureContext(ctx)
	case "agent":
		cb.buildAgentContext(ctx)
	case "general":
		cb.buildGeneralContext(ctx)
	}

	return ctx
}

// classifyTask classifies the task type
func (cb *ContextBuilder) classifyTask(task string) string {
	taskLower := strings.ToLower(task)

	switch {
	case strings.Contains(taskLower, "build") || strings.Contains(taskLower, "compile"):
		return "build"
	case strings.Contains(taskLower, "test") || strings.Contains(taskLower, "spec"):
		return "test"
	case strings.Contains(taskLower, "code") || strings.Contains(taskLower, "implement") ||
	     strings.Contains(taskLower, "write") || strings.Contains(taskLower, "function"):
		return "code"
	case strings.Contains(taskLower, "architecture") || strings.Contains(taskLower, "design") ||
	     strings.Contains(taskLower, "structure"):
		return "architecture"
	case strings.Contains(taskLower, "agent") || strings.Contains(taskLower, "delegate"):
		return "agent"
	default:
		return "general"
	}
}

// buildBuildContext builds context for build tasks
func (cb *ContextBuilder) buildBuildContext(ctx *BuildContext) {
	ctx.RelevantSections = []string{"Build Commands", "Architecture Overview"}

	if cb.artifacts.HasCLAUDEMD() {
		// Get build commands
		ctx.Commands = cb.artifacts.CLAUDEMD.GetCommandsForContext("build")

		// Build build info string
		var buildInfo strings.Builder
		buildInfo.WriteString("## Build Commands\n\n")

		if len(ctx.Commands) > 0 {
			for _, cmd := range ctx.Commands {
				if cmd.Description != "" {
					buildInfo.WriteString(fmt.Sprintf("- %s: `%s`\n", cmd.Description, cmd.Command))
				} else {
					buildInfo.WriteString(fmt.Sprintf("- `%s`\n", cmd.Command))
				}
			}
		} else {
			buildInfo.WriteString("No build commands found in CLAUDE.md\n")
		}

		ctx.BuildInfo = buildInfo.String()
	}
}

// buildTestContext builds context for test tasks
func (cb *ContextBuilder) buildTestContext(ctx *BuildContext) {
	ctx.RelevantSections = []string{"Build Commands"}

	if cb.artifacts.HasCLAUDEMD() {
		// Get test commands
		ctx.Commands = cb.artifacts.CLAUDEMD.GetCommandsForContext("test")

		// Build test info string
		var testInfo strings.Builder
		testInfo.WriteString("## Test Commands\n\n")

		if len(ctx.Commands) > 0 {
			for _, cmd := range ctx.Commands {
				if cmd.Description != "" {
					testInfo.WriteString(fmt.Sprintf("- %s: `%s`\n", cmd.Description, cmd.Command))
				} else {
					testInfo.WriteString(fmt.Sprintf("- `%s`\n", cmd.Command))
				}
			}
		} else {
			testInfo.WriteString("No test commands found in CLAUDE.md\n")
		}

		ctx.BuildInfo = testInfo.String()
	}
}

// buildCodeContext builds context for code generation/modification tasks
func (cb *ContextBuilder) buildCodeContext(ctx *BuildContext) {
	ctx.RelevantSections = []string{"Architecture Overview", "Key Components", "Code Conventions"}

	if cb.artifacts.HasCLAUDEMD() {
		doc := cb.artifacts.CLAUDEMD

		// Build architecture info
		if doc.Architecture != nil {
			var archInfo strings.Builder
			archInfo.WriteString("## Architecture Overview\n\n")

			if len(doc.Architecture.RequestFlow) > 0 {
				archInfo.WriteString("### Request Flow\n\n")
				for i, step := range doc.Architecture.RequestFlow {
					archInfo.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
				}
				archInfo.WriteString("\n")
			}

			if len(doc.Components) > 0 {
				archInfo.WriteString("### Key Components\n\n")
				for _, comp := range doc.Components {
					archInfo.WriteString(fmt.Sprintf("- **%s**: %s\n", comp.Layer, strings.Join(comp.Packages, ", ")))
				}
				archInfo.WriteString("\n")
			}

			ctx.ArchitectureInfo = archInfo.String()
			ctx.Architecture = doc.Architecture
		}

		// Build conventions info
		if doc.Conventions != nil {
			var convInfo strings.Builder
			convInfo.WriteString("## Code Conventions\n\n")

			if doc.Conventions.Language != "" {
				convInfo.WriteString(fmt.Sprintf("Language: %s\n\n", doc.Conventions.Language))
			}

			if len(doc.Conventions.Patterns) > 0 {
				convInfo.WriteString("Patterns:\n")
				for _, pattern := range doc.Conventions.Patterns {
					convInfo.WriteString(fmt.Sprintf("- %s\n", pattern))
				}
				convInfo.WriteString("\n")
			}

			ctx.ProjectOverview += convInfo.String()
		}
	}

	// Include available skills
	if cb.artifacts.HasSkills() {
		cb.buildSkillInfo(ctx, []string{"code", "development"})
	}
}

// buildArchitectureContext builds context for architecture questions
func (cb *ContextBuilder) buildArchitectureContext(ctx *BuildContext) {
	ctx.RelevantSections = []string{"Architecture Overview", "Key Components", "Project Structure"}

	if cb.artifacts.HasCLAUDEMD() {
		doc := cb.artifacts.CLAUDEMD

		var archInfo strings.Builder
		archInfo.WriteString("## Architecture\n\n")

		// Request flow
		if doc.Architecture != nil && len(doc.Architecture.RequestFlow) > 0 {
			archInfo.WriteString("### Request Flow\n\n")
			for i, step := range doc.Architecture.RequestFlow {
				archInfo.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
			}
			archInfo.WriteString("\n")
		}

		// Components
		if len(doc.Components) > 0 {
			archInfo.WriteString("### Component Mapping\n\n")
			for _, comp := range doc.Components {
				archInfo.WriteString(fmt.Sprintf("**%s**\n- Packages: %s\n\n", comp.Layer, strings.Join(comp.Packages, ", ")))
			}
		}

		// Data flow
		if doc.Architecture != nil && len(doc.Architecture.DataFlow) > 0 {
			archInfo.WriteString("### Data Flow\n\n")
			for _, step := range doc.Architecture.DataFlow {
				archInfo.WriteString(fmt.Sprintf("- %s\n", step.Action))
			}
			archInfo.WriteString("\n")
		}

		// Security layers
		if len(doc.SecurityLayers) > 0 {
			archInfo.WriteString("### Security Layers\n\n")
			for _, layer := range doc.SecurityLayers {
				archInfo.WriteString(fmt.Sprintf("- **%s**: %s\n", layer.Name, layer.Description))
			}
			archInfo.WriteString("\n")
		}

		ctx.ArchitectureInfo = archInfo.String()
		ctx.Architecture = doc.Architecture
		ctx.Components = doc.Components
	}
}

// buildAgentContext builds context for agent-related tasks
func (cb *ContextBuilder) buildAgentContext(ctx *BuildContext) {
	ctx.RelevantSections = []string{"Multi-Agent Architecture", "Coworker Awareness"}

	if cb.artifacts.HasCLAUDEMD() {
		doc := cb.artifacts.CLAUDEMD

		if len(doc.Agents) > 0 {
			var agentInfo strings.Builder
			agentInfo.WriteString("## Available Agents\n\n")

			for _, agent := range doc.Agents {
				agentInfo.WriteString(fmt.Sprintf("- **%s** (%s)\n", agent.ID, agent.Role))
				if agent.Purpose != "" {
					agentInfo.WriteString(fmt.Sprintf("  Purpose: %s\n", agent.Purpose))
				}
				if len(agent.Capabilities) > 0 {
					agentInfo.WriteString(fmt.Sprintf("  Capabilities: %s\n", strings.Join(agent.Capabilities, ", ")))
				}
				agentInfo.WriteString("\n")
			}

			ctx.AgentInfo = agentInfo.String()
			ctx.Agents = doc.Agents
		}
	}

	// Include agent development skills
	if cb.artifacts.HasSkills() {
		cb.buildSkillInfo(ctx, []string{"agent"})
	}
}

// buildGeneralContext builds context for general tasks
func (cb *ContextBuilder) buildGeneralContext(ctx *BuildContext) {
	ctx.RelevantSections = []string{"Architecture Overview", "Project Structure", "Code Conventions"}

	if cb.artifacts.HasCLAUDEMD() {
		doc := cb.artifacts.CLAUDEMD

		var overview strings.Builder
		overview.WriteString("## Project Overview\n\n")

		// Working directory
		overview.WriteString(fmt.Sprintf("Working Directory: `%s`\n\n", cb.artifacts.WorkingDir))

		// Architecture summary
		if doc.Architecture != nil && len(doc.Architecture.RequestFlow) > 0 {
			overview.WriteString("### Architecture\n\n")
			for _, step := range doc.Architecture.RequestFlow {
				overview.WriteString(fmt.Sprintf("- %s\n", step))
			}
			overview.WriteString("\n")
		}

		// Conventions
		if doc.Conventions != nil {
			overview.WriteString("### Conventions\n\n")
			if doc.Conventions.Language != "" {
				overview.WriteString(fmt.Sprintf("Language: %s\n", doc.Conventions.Language))
			}
			if len(doc.Conventions.Patterns) > 0 {
				overview.WriteString("\nPatterns:\n")
				for _, pattern := range doc.Conventions.Patterns {
					overview.WriteString(fmt.Sprintf("- %s\n", pattern))
				}
			}
			overview.WriteString("\n")
		}

		ctx.ProjectOverview = overview.String()
	}

	// Include all available skills
	if cb.artifacts.HasSkills() {
		cb.buildSkillInfo(ctx, nil) // nil means all categories
	}
}

// buildSkillInfo builds skill information string
func (cb *ContextBuilder) buildSkillInfo(ctx *BuildContext, categories []string) {
	if !cb.artifacts.HasSkills() {
		return
	}

	var skillInfo strings.Builder
	skillInfo.WriteString("## Available Skills\n\n")

	for _, skill := range cb.artifacts.ClaudeDir.Skills {
		// Filter by category if specified
		if categories != nil {
			matched := false
			for _, cat := range categories {
				if strings.Contains(strings.ToLower(skill.Category), strings.ToLower(cat)) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		skillInfo.WriteString(fmt.Sprintf("- **%s** (%s)\n", skill.Name, skill.Category))
		if skill.Description != "" {
			skillInfo.WriteString(fmt.Sprintf("  %s\n", skill.Description))
		}
		if skill.Version != "" {
			skillInfo.WriteString(fmt.Sprintf("  Version: %s\n", skill.Version))
		}
		skillInfo.WriteString("\n")

		ctx.Skills = append(ctx.Skills, skill)
	}

	if skillInfo.Len() > len("## Available Skills\n\n") {
		ctx.SkillInfo = skillInfo.String()
	}
}

// FormatForPrompt formats the context for use in an agent prompt
func (cb *ContextBuilder) FormatForPrompt(context *BuildContext) string {
	var prompt strings.Builder

	prompt.WriteString("---\n")
	prompt.WriteString("Context from Claude Artifacts\n")
	prompt.WriteString("---\n\n")

	// Project overview
	if context.ProjectOverview != "" {
		prompt.WriteString(context.ProjectOverview)
		prompt.WriteString("\n")
	}

	// Build/test commands
	if context.BuildInfo != "" {
		prompt.WriteString(context.BuildInfo)
		prompt.WriteString("\n")
	}

	// Architecture info
	if context.ArchitectureInfo != "" {
		prompt.WriteString(context.ArchitectureInfo)
		prompt.WriteString("\n")
	}

	// Agent info
	if context.AgentInfo != "" {
		prompt.WriteString(context.AgentInfo)
		prompt.WriteString("\n")
	}

	// Skill info
	if context.SkillInfo != "" {
		prompt.WriteString(context.SkillInfo)
		prompt.WriteString("\n")
	}

	prompt.WriteString("---\n")
	prompt.WriteString("End of Claude Artifact Context\n")
	prompt.WriteString("---\n")

	return prompt.String()
}

// ShouldInjectContext determines if context should be injected for a task
func (cb *ContextBuilder) ShouldInjectContext(task string) bool {
	// Always inject if artifacts are available
	return cb.artifacts.Available
}

// GetRelevantCommands returns commands relevant to a task
func (cb *ContextBuilder) GetRelevantCommands(task string) []BuildCommand {
	if !cb.artifacts.HasCLAUDEMD() {
		return nil
	}

	return cb.artifacts.CLAUDEMD.GetCommandsForContext(task)
}

// FindSkillForTask finds a skill relevant to a task
func (cb *ContextBuilder) FindSkillForTask(task string) *Skill {
	if !cb.artifacts.HasSkills() {
		return nil
	}

	taskLower := strings.ToLower(task)

	for _, skill := range cb.artifacts.ClaudeDir.Skills {
		// Check triggers
		for _, trigger := range skill.Triggers {
			if strings.Contains(taskLower, strings.ToLower(trigger)) {
				return skill
			}
		}

		// Check description
		if strings.Contains(taskLower, strings.ToLower(skill.Description)) {
			return skill
		}

		// Check name
		if strings.Contains(taskLower, strings.ToLower(skill.Name)) {
			return skill
		}
	}

	return nil
}

// FindAgentForTask finds an agent suitable for a task
func (cb *ContextBuilder) FindAgentForTask(task string) *AgentDefinition {
	if !cb.artifacts.HasCLAUDEMD() {
		return nil
	}

	return cb.artifacts.CLAUDEMD.GetAgentForTask(task)
}
