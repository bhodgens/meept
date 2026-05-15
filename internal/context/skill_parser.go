package context

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// parseSkillFrontmatter parses the YAML frontmatter into a Skill
func parseSkillFrontmatter(frontmatter string, skill *Skill) error {
	// Parse name
	if name := parseYAMLField(frontmatter, "name"); name != "" {
		skill.Name = name
	} else {
		return fmt.Errorf("missing required field: name")
	}

	// Parse description
	if desc := parseYAMLField(frontmatter, "description"); desc != "" {
		skill.Description = desc
	} else {
		return fmt.Errorf("missing required field: description")
	}

	// Parse version (optional)
	if version := parseYAMLField(frontmatter, "version"); version != "" {
		skill.Version = version
	} else {
		skill.Version = "0.1.0"
	}

	// Parse requires (optional)
	if requires := parseYAMLArray(frontmatter, "requires"); len(requires) > 0 {
		skill.Requires = requires
	}

	// Extract triggers from description
	skill.Triggers = extractTriggersFromDescription(skill.Description)

	// Infer category from slug or path
	skill.Category = inferSkillCategory(skill.Slug, skill.Path)

	return nil
}

// parseYAMLField extracts a simple YAML field value
func parseYAMLField(yaml, field string) string {
	// Pattern for key: value
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(field) + `:\s*(.+)$`)
	match := pattern.FindStringSubmatch(yaml)
	if len(match) > 1 {
		// Remove quotes if present
		value := strings.TrimSpace(match[1])
		value = strings.Trim(value, `"`)
		value = strings.Trim(value, `'`)
		return value
	}
	return ""
}

// parseYAMLArray extracts a YAML array
func parseYAMLArray(yaml, field string) []string {
	var items []string

	// Pattern for key: [item1, item2, ...] or key: - item1
	inlinePattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(field) + `:\s*\[(.+)\]`)
	inlineMatch := inlinePattern.FindStringSubmatch(yaml)
	if len(inlineMatch) > 1 {
		// Parse inline array
		values := strings.SplitSeq(inlineMatch[1], ",")
		for v := range values {
			item := strings.TrimSpace(v)
			item = strings.Trim(item, `"`)
			item = strings.Trim(item, `'`)
			if item != "" {
				items = append(items, item)
			}
		}
		return items
	}

	// Parse list items
	lines := strings.Split(yaml, "\n")
	inField := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, field+":") {
			inField = true
			continue
		}

		if inField {
			if strings.HasPrefix(trimmed, "- ") {
				item := strings.TrimSpace(trimmed[2:])
				item = strings.Trim(item, `"`)
				item = strings.Trim(item, `'`)
				if item != "" {
					items = append(items, item)
				}
			} else if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				// New field or end of list
				break
			}
		}
	}

	return items
}

// extractTriggersFromDescription extracts trigger phrases from description
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

// parseAgentFrontmatter parses agent frontmatter
func parseAgentFrontmatter(frontmatter, body string) (*AgentDefinition, error) {
	agent := &AgentDefinition{}

	// Parse name (required)
	if name := parseYAMLField(frontmatter, "name"); name != "" {
		agent.ID = name
		agent.Name = name
	} else {
		return nil, fmt.Errorf("missing required field: name")
	}

	// Parse description to extract role and purpose
	if description := parseYAMLField(frontmatter, "description"); description != "" {
		agent.Purpose = description
		agent.Role = inferAgentRole(description, body)
	}

	// Parse model (optional)
	if model := parseYAMLField(frontmatter, "model"); model != "" {
		agent.Model = model
	} else {
		agent.Model = "inherit"
	}

	// Parse color (optional)
	if color := parseYAMLField(frontmatter, "color"); color != "" {
		agent.Color = color
	} else {
		agent.Color = "blue"
	}

	// Parse tools (optional)
	if tools := parseYAMLArray(frontmatter, "tools"); len(tools) > 0 {
		agent.Capabilities = tools
	}

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
