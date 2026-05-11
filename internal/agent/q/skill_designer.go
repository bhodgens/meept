package q

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SkillDesigner generates Claude Code compatible skill specifications.
type SkillDesigner struct {
	logger *slog.Logger
	config SkillDesignerConfig
}

// SkillDesignerConfig holds configuration for the SkillDesigner.
type SkillDesignerConfig struct {
	// SkillsDir is the directory where skills are stored (default: ~/.meept/skills)
	SkillsDir string
}

// NewSkillDesigner creates a new SkillDesigner.
func NewSkillDesigner(logger *slog.Logger, config SkillDesignerConfig) *SkillDesigner {
	return &SkillDesigner{
		logger: logger,
		config: config,
	}
}

// GenerateSkill generates a SkillDesign from a recommendation.
func (sd *SkillDesigner) GenerateSkill(rec Recommendation) (*SkillDesign, error) {
	if rec.Implementation.SkillSpec != nil {
		// Already has skill spec from research engine
		return sd.enrichSkill(rec.Implementation.SkillSpec, rec)
	}

	// Create skill from scratch based on recommendation
	skill := &SkillDesign{
		ID:              sd.generateSkillID(rec),
		Name:            sd.extractName(rec),
		Description:     rec.Description,
		TriggerKeywords: sd.extractTriggerKeywords(rec),
		ShellCommands:   sd.extractShellCommands(rec),
		SystemPrompt:    sd.generateSystemPrompt(rec),
	}

	return skill, nil
}

// enrichSkill enriches an existing skill spec with additional details.
func (sd *SkillDesigner) enrichSkill(skill *SkillDesign, rec Recommendation) (*SkillDesign, error) {
	skill.TriggerKeywords = append(skill.TriggerKeywords, sd.extractTriggerKeywords(rec)...)
	skill.ShellCommands = sd.extractShellCommands(rec)
	skill.SystemPrompt = sd.generateSystemPrompt(rec)
	return skill, nil
}

// WriteSkillFile writes a skill to disk in Claude Code compatible format.
func (sd *SkillDesigner) WriteSkillFile(skill *SkillDesign, outputPath string) error {
	// Ensure directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create skill directory: %w", err)
	}

	// Generate markdown content
	content := sd.GenerateFullSkillFile(skill)

	// Write file
	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write skill file: %w", err)
	}

	sd.logger.Info("saved skill", "path", outputPath, "name", skill.Name)
	return nil
}

// GenerateFullSkillFile generates the complete SKILL.md content.
func (sd *SkillDesigner) GenerateFullSkillFile(skill *SkillDesign) string {
	var buf strings.Builder

	// YAML frontmatter
	buf.WriteString("---\n")
	fmt.Fprintf(&buf, "name: %s\n", skill.ID)
	fmt.Fprintf(&buf, "description: >\n  %s\n", skill.Description)
	buf.WriteString("author: Q Agent (Quartermaster)\n")
	buf.WriteString("version: \"1.0\"\n")
	fmt.Fprintf(&buf, "date: %s\n", time.Now().Format("2006-01-02"))
	if len(skill.Tools) > 0 {
		buf.WriteString("allowed-tools:\n")
		for _, tool := range skill.Tools {
			fmt.Fprintf(&buf, "  - %s\n", tool)
		}
	}
	buf.WriteString("---\n\n")

	// Main content
	fmt.Fprintf(&buf, "# %s\n\n", skill.Name)

	buf.WriteString("## Overview\n")
	fmt.Fprintf(&buf, "%s\n\n", skill.Description)

	buf.WriteString("## When to Use\n")
	buf.WriteString("Use this skill when:\n")
	for _, keyword := range skill.TriggerKeywords {
		fmt.Fprintf(&buf, "- Handling %s-related tasks\n", keyword)
	}
	buf.WriteString("\n")

	if len(skill.ShellCommands) > 0 {
		buf.WriteString("## Shell Commands\n")
		buf.WriteString("This skill provides the following shell commands:\n\n")
		for _, cmd := range skill.ShellCommands {
			fmt.Fprintf(&buf, "```bash\n%s\n```\n\n", cmd)
		}
	}

	if len(skill.Tools) > 0 {
		buf.WriteString("## Required Tools\n")
		buf.WriteString("This skill requires access to:\n\n")
		for _, tool := range skill.Tools {
			fmt.Fprintf(&buf, "- `%s`\n", tool)
		}
		buf.WriteString("\n")
	}

	buf.WriteString("## The Pattern\n")
	buf.WriteString(sd.generatePatternSection(skill))

	buf.WriteString("## Examples\n\n")
	buf.WriteString(sd.generateExamples(skill))

	if skill.SystemPrompt != "" {
		buf.WriteString("## System Prompt\n")
		buf.WriteString("When this skill is invoked as an agent skill, use the following system prompt:\n\n")
		fmt.Fprintf(&buf, "```\n%s\n```\n\n", skill.SystemPrompt)
	}

	return buf.String()
}

// generateSkillID generates a skill ID from the recommendation.
func (sd *SkillDesigner) generateSkillID(rec Recommendation) string {
	// Convert title to kebab-case
	id := strings.ToLower(rec.Title)
	id = strings.ReplaceAll(id, " ", "_")
	id = strings.ReplaceAll(id, "-", "_")
	id = strings.ReplaceAll(id, ":", "")
	id = strings.ReplaceAll(id, "create ", "")
	id = strings.ReplaceAll(id, "skill", "")
	id = strings.TrimSpace(id)
	return fmt.Sprintf("skill_%s", strings.ReplaceAll(id, "__", "_"))
}

// extractName extracts a human-readable name from the recommendation.
func (sd *SkillDesigner) extractName(rec Recommendation) string {
	return strings.TrimPrefix(rec.Title, "Create ")
}

// extractTriggerKeywords extracts trigger keywords from the recommendation.
func (sd *SkillDesigner) extractTriggerKeywords(rec Recommendation) []string {
	keywords := []string{}

	// Extract from description
	words := strings.Fields(rec.Description)
	seen := make(map[string]bool)
	for _, word := range words {
		word = strings.Trim(strings.ToLower(word), ".,;:!?")
		if len(word) > 3 && !seen[word] && !isStopWord(word) {
			keywords = append(keywords, word)
			seen[word] = true
		}
	}

	// Also use intent from recommendation if available
	if rec.Title != "" {
		parts := strings.FieldsSeq(rec.Title)
		for part := range parts {
			clean := strings.Trim(part, ".,;:!?")
			if len(clean) > 3 && !seen[strings.ToLower(clean)] {
				keywords = append(keywords, strings.ToLower(clean))
			}
		}
	}

	return keywords
}

// extractShellCommands extracts shell commands from the recommendation.
func (sd *SkillDesigner) extractShellCommands(rec Recommendation) []string {
	commands := make([]string, 0)

	// Check implementation details for commands
	for _, cmd := range rec.Implementation.Commands {
		if cmd != "" {
			commands = append(commands, cmd)
		}
	}

	// Check files to create for script content
	for _, file := range rec.Implementation.FilesToCreate {
		if strings.HasSuffix(file.Path, ".sh") || strings.HasSuffix(file.Path, ".bash") {
			commands = append(commands, file.Content)
		}
	}

	return commands
}

// generateSystemPrompt generates a system prompt for the skill.
func (sd *SkillDesigner) generateSystemPrompt(rec Recommendation) string {
	var buf strings.Builder

	fmt.Fprintf(&buf, "You are a %s skill.\n", rec.Title)
	fmt.Fprintf(&buf, "Your purpose: %s\n\n", rec.Description)

	buf.WriteString("When invoked:\n")
	buf.WriteString("1. Verify the task matches your specialty\n")
	buf.WriteString("2. Execute the appropriate command from your repertoire\n")
	buf.WriteString("3. Report results concisely\n\n")

	buf.WriteString("Constraints:\n")
	buf.WriteString("- Only handle tasks within your specialty\n")
	buf.WriteString("- Escalate if the task requires LLM reasoning\n")
	buf.WriteString("- Log all operations for audit\n")

	return buf.String()
}

// generatePatternSection generates the pattern section content.
func (sd *SkillDesigner) generatePatternSection(skill *SkillDesign) string {
	return fmt.Sprintf(`This skill automates %s tasks.

**Workflow:**
1. Receive task request
2. Match against trigger keywords
3. Execute appropriate shell command
4. Return result

**Benefits:**
- Deterministic execution (no LLM variance)
- Fast response time
- Consistent output format
- Audit trail of all executions

`, skill.Description)
}

// generateExamples generates example usage.
func (sd *SkillDesigner) generateExamples(skill *SkillDesign) string {
	if len(skill.TriggerKeywords) == 0 {
		return "No examples available.\n\n"
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "Example 1: Using %s skill\n", skill.Name)
	buf.WriteString("```\n")
	fmt.Fprintf(&buf, "User: Handle %s\n", skill.TriggerKeywords[0])
	fmt.Fprintf(&buf, "Assistant: [invokes %s skill]\n", skill.ID)
	buf.WriteString("Result: Task completed successfully.\n")
	buf.WriteString("```\n\n")

	return buf.String()
}

// isStopWord checks if a word is a common stop word.
func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "with": true,
		"this": true, "that": true, "from": true, "have": true,
		"been": true, "were": true, "would": true, "could": true,
		"should": true, "might": true, "just": true, "into": true,
	}
	return stopWords[word]
}
