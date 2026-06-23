// Package commands provides slash command handlers for the TUI.
package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// SkillResult represents the result of executing a skill command.
type SkillResult struct {
	Output  string
	IsError bool
}

// SkillInfo holds the subset of skill metadata needed by the TUI. It is
// populated from the daemon's RPC response (skills.list) so the TUI never
// depends on the daemon's concrete skills.Registry type.
type SkillInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Requires    []string `json:"requires,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	RiskLevel   string   `json:"risk_level,omitempty"`
}

// SkillLister fetches skills via RPC (decoupled from the RPC client concrete
// type and from the daemon's skills.Registry). Implementations must be safe
// for concurrent use.
type SkillLister interface {
	ListSkills(ctx context.Context) ([]SkillInfo, error)
}

// SkillCommand handles /skill slash command execution.
type SkillCommand struct {
	lister SkillLister
}

// NewSkillCommand creates a new skill command handler backed by the given
// lister. If lister is nil, Execute returns an error at runtime (the command
// is registered but reports it is unavailable).
func NewSkillCommand(lister SkillLister) *SkillCommand {
	return &SkillCommand{
		lister: lister,
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
	if c.lister == nil {
		return &SkillResult{
			Output:  "skill system not available (no RPC connection)",
			IsError: true,
		}
	}

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
	skills, err := c.lister.ListSkills(context.Background())
	if err != nil {
		return &SkillResult{
			Output:  fmt.Sprintf("failed to fetch skills: %v", err),
			IsError: true,
		}
	}

	if len(skills) == 0 {
		return &SkillResult{Output: "no skills installed"}
	}

	// Sort by name for stable output.
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("installed skills (%d):\n\n", len(skills)))

	for _, skill := range skills {
		sb.WriteString(fmt.Sprintf("  /%-20s %s\n", skill.Name, skill.Description))
	}

	sb.WriteString("\nusage: /skill <name> to view details")

	return &SkillResult{Output: sb.String()}
}

func (c *SkillCommand) executeShow(name string) *SkillResult {
	skills, err := c.lister.ListSkills(context.Background())
	if err != nil {
		return &SkillResult{
			Output:  fmt.Sprintf("failed to fetch skills: %v", err),
			IsError: true,
		}
	}

	for _, s := range skills {
		if s.Name == name {
			return c.formatSkillDetail(s)
		}
	}

	return &SkillResult{
		Output:  fmt.Sprintf("skill not found: %s", name),
		IsError: true,
	}
}

func (c *SkillCommand) formatSkillDetail(s SkillInfo) *SkillResult {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("skill: %s\n", s.Name))
	sb.WriteString(fmt.Sprintf("description: %s\n", s.Description))

	if len(s.Requires) > 0 {
		sb.WriteString(fmt.Sprintf("requires: %s\n", strings.Join(s.Requires, ", ")))
	}

	if len(s.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("tags: %s\n", strings.Join(s.Tags, ", ")))
	}

	if s.RiskLevel != "" {
		sb.WriteString(fmt.Sprintf("risk: %s\n", s.RiskLevel))
	}

	return &SkillResult{Output: sb.String()}
}

func (c *SkillCommand) executeSearch(query string) *SkillResult {
	skills, err := c.lister.ListSkills(context.Background())
	if err != nil {
		return &SkillResult{
			Output:  fmt.Sprintf("failed to fetch skills: %v", err),
			IsError: true,
		}
	}

	q := strings.ToLower(query)
	for _, s := range skills {
		if strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Description), q) {
			return c.formatSkillDetail(s)
		}
	}

	return &SkillResult{
		Output:  fmt.Sprintf("no skills matching: %s", query),
		IsError: true,
	}
}

// GetSkillNames returns all skill names for autocomplete. On error returns nil.
// This performs an RPC fetch on each call; callers should cache results if
// invoking in a hot path.
func (c *SkillCommand) GetSkillNames() []string {
	if c.lister == nil {
		return nil
	}
	skills, err := c.lister.ListSkills(context.Background())
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}
