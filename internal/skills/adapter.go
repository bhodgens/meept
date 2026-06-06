// Package skills provides skill discovery, parsing, and execution for meept.
package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// ClaudeSkillAdapter adapts Claude Code skill format to Meept format.
//
// Claude Code skills use the same SKILL.md frontmatter+markdown format as
// Meept skills. The field names are nearly identical; the main differences
// are:
//   - Some Claude skills use camelCase field names (allowedTools, riskLevel,
//     maxIterations, maxTokens) instead of kebab-case or snake_case.
//   - Claude skills may include a "trigger" field that maps to Meept's Tags.
//   - Claude skills may include a "metadata" block which is ignored.
//
// The camelCase normalization is handled in the parser's parseMetadata
// function. The adapter provides path detection and any future
// normalizations needed for Claude-specific quirks.
type ClaudeSkillAdapter struct{}

// AdaptSkill applies Claude-specific normalization to a parsed skill.
// It is safe to call on any skill; non-Claude skills are returned unchanged.
//
// Normalizations applied:
//   - If the skill has no Tags and was loaded from a Claude path,
//     nothing extra is done (trigger-to-tags mapping is already handled
//     by the parser).
func (a *ClaudeSkillAdapter) AdaptSkill(skill *Skill) *Skill {
	if skill == nil {
		return nil
	}

	// Mark the skill as coming from the Claude tier.
	skill.Source = "claude"

	// If description is empty, derive it from the first non-heading,
	// non-empty line of the body.
	if skill.Description == "" && skill.Body != "" {
		for _, line := range strings.Split(skill.Body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if len(trimmed) > 200 {
				trimmed = trimmed[:200]
			}
			skill.Description = trimmed
			break
		}
	}

	// If Tags is empty and the skill was loaded from a directory-based
	// Claude skill path, derive a tag from the parent directory name.
	if len(skill.Tags) == 0 && skill.Path != "" {
		dir := filepath.Dir(skill.Path)
		parent := filepath.Base(dir)
		if parent != "." && parent != "skills" {
			skill.Tags = []string{parent}
		}
	}

	return skill
}

// IsClaudeSkillPath returns true if the path is under a ~/.claude/skills/
// directory (expanded or literal).
func IsClaudeSkillPath(path string) bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}

	candidates := []string{
		filepath.Join(homeDir, ".claude", "skills"),
	}

	for _, prefix := range candidates {
		if prefix != "" && strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}
