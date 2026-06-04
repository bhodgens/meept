package context

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrNoFrontmatter is returned (alongside a valid Skill) when a SKILL.md
// file has no YAML frontmatter. Callers should log a warning but still use
// the returned skill.
var ErrNoFrontmatter = fmt.Errorf("no frontmatter found")

// ParseSkillFile parses a SKILL.md file.
// YAML frontmatter is optional: when present it populates Name/Description/etc.
// When absent, the slug (directory name) is used as the name, the entire file
// content becomes the body, and ErrNoFrontmatter is returned alongside the
// valid skill so callers can emit a warning.
func ParseSkillFile(path string) (*Skill, error) {
	// Read the file
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill file: %w", err)
	}

	skill := &Skill{
		Path:    path,
		Content: string(content),
	}

	// Extract skill slug from path
	skill.Slug = extractSkillSlug(path)

	// Parse YAML frontmatter (optional)
	frontmatter, body, err := extractYAMLFrontmatter(string(content))
	if err != nil {
		// No frontmatter — use slug as name, full content as body.
		skill.Name = skill.Slug
		skill.Content = strings.TrimSpace(string(content))
		skill.Version = "0.1.0"
		skill.Category = inferSkillCategory(skill.Slug, skill.Path)
		return skill, ErrNoFrontmatter
	}

	// Parse frontmatter fields
	if err := parseSkillFrontmatter(frontmatter, skill); err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter fields: %w", err)
	}

	// Store body content
	skill.Content = strings.TrimSpace(body)

	return skill, nil
}

// extractSkillSlug extracts the skill slug from the path
func extractSkillSlug(path string) string {
	// Get the parent directory name - this is the skill directory
	skillDir := filepath.Dir(path)
	if skillDir != "" && skillDir != "." {
		return filepath.Base(skillDir)
	}

	// Fallback: extract from path
	if idx := strings.LastIndex(path, "/"); idx != -1 {
		slug := path[idx+1:]
		// Remove "/SKILL.md" suffix if present
		if strings.HasSuffix(slug, "/SKILL.md") {
			return slug[:len(slug)-9]
		}
		return slug
	}

	return "unknown"
}

// extractYAMLFrontmatter extracts YAML frontmatter from markdown
func extractYAMLFrontmatter(content string) (frontmatter, body string, err error) {
	lines := strings.Split(content, "\n")

	if len(lines) < 3 {
		return "", "", fmt.Errorf("content too short for frontmatter")
	}

	// Check for opening ---
	if !strings.HasPrefix(lines[0], "---") {
		return "", "", fmt.Errorf("no frontmatter found")
	}

	// Find closing ---
	var frontmatterLines []string
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "---") {
			// Found closing, rest is body
			body = strings.Join(lines[i+1:], "\n")
			frontmatter = strings.Join(frontmatterLines, "\n")
			return frontmatter, body, nil
		}
		frontmatterLines = append(frontmatterLines, lines[i])
	}

	return "", "", fmt.Errorf("no closing --- found in frontmatter")
}

// skillFrontmatter represents the YAML frontmatter for a skill.
type skillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Requires    []string `yaml:"requires"`
}

// parseSkillFrontmatter parses the YAML frontmatter into a Skill.
func parseSkillFrontmatter(frontmatter string, skill *Skill) error {
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		return fmt.Errorf("failed to parse frontmatter: %w", err)
	}
	if fm.Name == "" {
		return fmt.Errorf("missing required field: name")
	}
	if fm.Description == "" {
		return fmt.Errorf("missing required field: description")
	}
	skill.Name = fm.Name
	skill.Description = fm.Description
	if fm.Version != "" {
		skill.Version = fm.Version
	} else {
		skill.Version = "0.1.0"
	}
	skill.Requires = fm.Requires
	skill.Triggers = extractTriggersFromDescription(skill.Description)
	skill.Category = inferSkillCategory(skill.Slug, skill.Path)
	return nil
}

func extractTriggersFromDescription(description string) []string {
	var triggers []string

	// Look for "Use this agent when..." or "Use this skill when..." patterns
	pattern := regexp.MustCompile(`(?i)use this (agent|skill) when (.+?)\.`)
	match := pattern.FindAllStringSubmatch(description, -1)

	for _, m := range match {
		if len(m) > 2 {
			trigger := strings.TrimSpace(m[2])
			triggers = append(triggers, trigger)
		}
	}

	return triggers
}

// inferSkillCategory infers a category from the skill slug/path
func inferSkillCategory(slug, _ string) string {
	lower := strings.ToLower(slug)

	switch {
	case strings.Contains(lower, "agent"):
		return SectionAgent
	case strings.Contains(lower, "diagram") || strings.Contains(lower, "mermaid"):
		return "visualization"
	case strings.Contains(lower, "docx") || strings.Contains(lower, "document"):
		return "document"
	case strings.Contains(lower, "test") || strings.Contains(lower, "playwright"):
		return "testing"
	case strings.Contains(lower, "react") || strings.Contains(lower, "frontend"):
		return "frontend"
	case strings.Contains(lower, "architect") || strings.Contains(lower, "design"):
		return "architecture"
	case strings.Contains(lower, "devops") || strings.Contains(lower, "deploy"):
		return "devops"
	default:
		return "general"
	}
}

// ParseAgentFile parses an agent definition file
func ParseAgentFile(path string) ([]*AgentDefinition, error) {
	// Read the file
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent file: %w", err)
	}

	var agents []*AgentDefinition

	// Extract YAML frontmatter
	frontmatter, body, err := extractYAMLFrontmatter(string(content))
	if err != nil {
		// No frontmatter, try to parse body
		body = string(content)
	}

	if frontmatter != "" {
		// Single agent definition in frontmatter
		agent, err := parseAgentFrontmatter(frontmatter, body)
		if err != nil {
			return nil, fmt.Errorf("failed to parse agent frontmatter: %w", err)
		}
		agents = append(agents, agent)
	} else {
		// Look for multiple agents in the body
		bodyAgents := parseAgentsFromBody(body, path)
		agents = append(agents, bodyAgents...)
	}

	return agents, nil
}

// agentFrontmatter represents the YAML frontmatter for an agent.
type agentFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Model       string   `yaml:"model"`
	Color       string   `yaml:"color"`
	Tools       []string `yaml:"tools"`
}

// parseAgentFrontmatter parses agent frontmatter.
func parseAgentFrontmatter(frontmatter, body string) (*AgentDefinition, error) {
	var fm agentFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		return nil, fmt.Errorf("failed to parse agent frontmatter: %w", err)
	}
	if fm.Name == "" {
		return nil, fmt.Errorf("missing required field: name")
	}
	agent := &AgentDefinition{
		ID:   fm.Name,
		Name: fm.Name,
	}
	if fm.Description != "" {
		agent.Purpose = fm.Description
		agent.Role = inferAgentRole(fm.Description, body)
	}
	if fm.Model != "" {
		agent.Model = fm.Model
	} else {
		agent.Model = "inherit"
	}
	if fm.Color != "" {
		agent.Color = fm.Color
	} else {
		agent.Color = "blue"
	}
	agent.Capabilities = fm.Tools
	return agent, nil
}

// inferAgentRole infers the agent role from description and body
func inferAgentRole(description, _ string) string {
	descLower := strings.ToLower(description)

	switch {
	case strings.Contains(descLower, "code") || strings.Contains(descLower, "program"):
		return "Coder"
	case strings.Contains(descLower, "debug"):
		return "Debugger"
	case strings.Contains(descLower, "plan"):
		return "Planner"
	case strings.Contains(descLower, "analyze") || strings.Contains(descLower, "analyst"):
		return "Analyst"
	case strings.Contains(descLower, "chat") || strings.Contains(descLower, "conversation"):
		return "Chat"
	case strings.Contains(descLower, "commit") || strings.Contains(descLower, "git"):
		return "Committer"
	case strings.Contains(descLower, "schedule") || strings.Contains(descLower, "job"):
		return "Scheduler"
	case strings.Contains(descLower, "dispatch") || strings.Contains(descLower, "route"):
		return "Dispatcher"
	default:
		return "Executor"
	}
}

// parseAgentsFromBody parses multiple agents from the body
func parseAgentsFromBody(_, path string) []*AgentDefinition {
	agents := make([]*AgentDefinition, 0, 1)

	// This is a simplified implementation
	// In a full implementation, this would parse complex agent definitions
	// from the body content, potentially including multiple agents

	// For now, create a single generic agent if we can't parse properly
	agent := &AgentDefinition{
		ID:   extractFileName(path),
		Name: extractFileName(path),
		Role: "Executor",
	}

	agents = append(agents, agent)

	return agents
}

// extractFileName extracts the base filename without extension
func extractFileName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx != -1 {
		filename := path[idx+1:]
		if extIdx := strings.LastIndex(filename, "."); extIdx != -1 {
			return filename[:extIdx]
		}
		return filename
	}
	return path
}
