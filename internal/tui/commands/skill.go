// Package commands provides slash command handlers for the TUI.
package commands

import (
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/skills"
)

// SkillResult represents the result of executing a skill command.
type SkillResult struct {
	Output  string
	IsError bool
}

// SkillCommand handles /skill slash command execution.
type SkillCommand struct {
	registry *skills.Registry
}

// NewSkillCommand creates a new skill command handler.
func NewSkillCommand(registry *skills.Registry) *SkillCommand {
	return &SkillCommand{
		registry: registry,
	}
}

// Execute executes the /skill command with the given arguments.
//
// Usage:
//
//	/skill              - list all available skills
//	/skill <name>       - show skill details
//	/skill search <q>   - search skills by name/description
func (c *SkillCommand) Execute(args []string) *SkillResult {
	if len(args) == 0 {
		return c.executeList()
	}

	switch args[0] {
	case "search":
		if len(args) < 2 {
			return &SkillResult{
				Output:  "usage: /skill search <query>",
				IsError: true,
			}
		}
		return c.executeSearch(args[1])
	default:
		// Show specific skill details
		return c.executeShow(args[0])
	}
}

func (c *SkillCommand) executeList() *SkillResult {
	skillList := c.registry.List()
	if len(skillList) == 0 {
		return &SkillResult{Output: "no skills installed"}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("installed skills (%d):\n\n", len(skillList)))

	for _, skill := range skillList {
		sb.WriteString(fmt.Sprintf("  /%-20s %s\n", skill.Name, skill.Description))
	}

	sb.WriteString("\nusage: /skill <name> to view details")

	return &SkillResult{Output: sb.String()}
}

func (c *SkillCommand) executeShow(name string) *SkillResult {
	skill := c.registry.Get(name)
	if skill == nil {
		return &SkillResult{
			Output:  fmt.Sprintf("skill not found: %s", name),
			IsError: true,
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("skill: %s\n", skill.Name))
	sb.WriteString(fmt.Sprintf("description: %s\n", skill.Description))

	if len(skill.Requires) > 0 {
		sb.WriteString(fmt.Sprintf("requires: %s\n", strings.Join(skill.Requires, ", ")))
	}

	if len(skill.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("tags: %s\n", strings.Join(skill.Tags, ", ")))
	}

	if skill.RiskLevel != "" {
		sb.WriteString(fmt.Sprintf("risk: %s\n", skill.RiskLevel))
	}

	return &SkillResult{Output: sb.String()}
}

func (c *SkillCommand) executeSearch(query string) *SkillResult {
	match := c.registry.Match(query)
	if match == nil {
		return &SkillResult{
			Output:  fmt.Sprintf("no skills matching: %s", query),
			IsError: true,
		}
	}

	return c.executeShow(match.Name)
}

// GetSkillNames returns all skill names for autocomplete.
func (c *SkillCommand) GetSkillNames() []string {
	return c.registry.Names()
}
