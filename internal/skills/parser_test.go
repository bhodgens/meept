package skills

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillText_Valid(t *testing.T) {
	text := `---
name: code-review
description: Review code for bugs and improvements
requires: [code, reasoning]
tags: [development, review]
examples:
  - "Review this function for bugs"
  - "What improvements can be made to this code?"
---

You are a code reviewer. Analyze the provided code for:
1. Bugs and logic errors
2. Performance issues
3. Style and readability
4. Security vulnerabilities

Provide constructive feedback with specific suggestions.
`

	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText failed: %v", err)
	}

	if skill.Name != "code-review" {
		t.Errorf("Name = %q, want code-review", skill.Name)
	}

	if skill.Description != "Review code for bugs and improvements" {
		t.Errorf("Description = %q, want 'Review code for bugs and improvements'", skill.Description)
	}

	if len(skill.Requires) != 2 {
		t.Errorf("Requires length = %d, want 2", len(skill.Requires))
	} else {
		if skill.Requires[0] != "code" || skill.Requires[1] != "reasoning" {
			t.Errorf("Requires = %v, want [code, reasoning]", skill.Requires)
		}
	}

	if len(skill.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2", len(skill.Tags))
	}

	if len(skill.Examples) != 2 {
		t.Errorf("Examples length = %d, want 2", len(skill.Examples))
	}

	if skill.Body == "" {
		t.Error("Body should not be empty")
	}

	if skill.RiskLevel != "medium" {
		t.Errorf("RiskLevel = %q, want medium (default)", skill.RiskLevel)
	}

	if skill.MaxIterations != 10 {
		t.Errorf("MaxIterations = %d, want 10 (default)", skill.MaxIterations)
	}
}

func TestParseSkillText_WithOptionalFields(t *testing.T) {
	text := `---
name: advanced-skill
description: An advanced skill
requires: [tool_use]
risk-level: high
max-iterations: 5
temperature: 0.3
max-tokens: 2000
allowed-tools: [shell, file]
---

Instructions here.
`

	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText failed: %v", err)
	}

	if skill.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want high", skill.RiskLevel)
	}

	if skill.MaxIterations != 5 {
		t.Errorf("MaxIterations = %d, want 5", skill.MaxIterations)
	}

	if skill.Temperature == nil || *skill.Temperature != 0.3 {
		t.Errorf("Temperature = %v, want 0.3", skill.Temperature)
	}

	if skill.MaxTokens == nil || *skill.MaxTokens != 2000 {
		t.Errorf("MaxTokens = %v, want 2000", skill.MaxTokens)
	}

	if len(skill.AllowedTools) != 2 {
		t.Errorf("AllowedTools length = %d, want 2", len(skill.AllowedTools))
	}
}

func TestParseSkillText_AlternativeFieldNames(t *testing.T) {
	text := `---
name: alt-skill
description: Skill with underscore field names
risk_level: low
max_iterations: 20
max_tokens: 1000
allowed_tools: [api]
---

Body here.
`

	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText failed: %v", err)
	}

	if skill.RiskLevel != "low" {
		t.Errorf("RiskLevel = %q, want low", skill.RiskLevel)
	}

	if skill.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d, want 20", skill.MaxIterations)
	}

	if skill.MaxTokens == nil || *skill.MaxTokens != 1000 {
		t.Errorf("MaxTokens = %v, want 1000", skill.MaxTokens)
	}
}

func TestParseSkillText_NoFrontmatter(t *testing.T) {
	text := `# Just a regular markdown file

No frontmatter here.
`

	_, err := ParseSkillText(text)
	if !errors.Is(err, ErrNoFrontmatter) {
		t.Errorf("Expected ErrNoFrontmatter, got %v", err)
	}
}

func TestParseSkillText_InvalidYAML(t *testing.T) {
	text := `---
name: [invalid yaml structure
requires: broken
---

Body.
`

	_, err := ParseSkillText(text)
	if !errors.Is(err, ErrInvalidYAML) {
		t.Errorf("Expected ErrInvalidYAML, got %v", err)
	}
}

func TestParseSkillText_NoName(t *testing.T) {
	text := `---
description: Skill without a name
requires: [code]
---

Body here.
`

	_, err := ParseSkillText(text)
	if !errors.Is(err, ErrNoName) {
		t.Errorf("Expected ErrNoName, got %v", err)
	}
}

func TestParseSkillText_EmptyBody(t *testing.T) {
	text := `---
name: minimal-skill
description: A minimal skill
---
`

	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText failed: %v", err)
	}

	if skill.Name != "minimal-skill" {
		t.Errorf("Name = %q, want minimal-skill", skill.Name)
	}

	// Empty body is valid
	if skill.Body != "" {
		t.Errorf("Body = %q, want empty", skill.Body)
	}
}

func TestParseSkillText_LeadingWhitespace(t *testing.T) {
	text := `

   ---
name: whitespace-skill
description: Skill with leading whitespace
---

Body.
`

	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText failed: %v", err)
	}

	if skill.Name != "whitespace-skill" {
		t.Errorf("Name = %q, want whitespace-skill", skill.Name)
	}
}

func TestParseSkillFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "skills-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write test skill file
	skillContent := `---
name: file-skill
description: Skill loaded from file
requires: [code]
tags: [test]
---

This skill was loaded from a file.
`
	skillPath := filepath.Join(tmpDir, "test-skill.md")
	if err := os.WriteFile(skillPath, []byte(skillContent), 0644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}

	skill, err := ParseSkillFile(skillPath)
	if err != nil {
		t.Fatalf("ParseSkillFile failed: %v", err)
	}

	if skill.Name != "file-skill" {
		t.Errorf("Name = %q, want file-skill", skill.Name)
	}

	if skill.Path != skillPath {
		t.Errorf("Path = %q, want %q", skill.Path, skillPath)
	}
}

func TestParseSkillFile_NotFound(t *testing.T) {
	_, err := ParseSkillFile("/nonexistent/path/skill.md")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}

	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Errorf("Expected ParseError, got %T", err)
	}
}

func TestSplitFrontmatter_Various(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantFront     string
		wantBody      string
		wantErr       error
	}{
		{
			name: "standard",
			input: `---
key: value
---

body content`,
			wantFront: "key: value",
			wantBody:  "body content",
		},
		{
			name: "no closing marker",
			input: `---
key: value

no closing`,
			wantErr: ErrNoFrontmatter,
		},
		{
			name: "no opening marker",
			input: `key: value
---

body`,
			wantErr: ErrNoFrontmatter,
		},
		{
			name: "empty frontmatter",
			input: `---
---

body`,
			wantFront: "",
			wantBody:  "body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			front, body, err := splitFrontmatter(tt.input)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			frontTrimmed := trimWhitespace(front)
			wantFrontTrimmed := trimWhitespace(tt.wantFront)
			if frontTrimmed != wantFrontTrimmed {
				t.Errorf("frontmatter = %q, want %q", frontTrimmed, wantFrontTrimmed)
			}

			bodyTrimmed := trimWhitespace(body)
			wantBodyTrimmed := trimWhitespace(tt.wantBody)
			if bodyTrimmed != wantBodyTrimmed {
				t.Errorf("body = %q, want %q", bodyTrimmed, wantBodyTrimmed)
			}
		})
	}
}

func trimWhitespace(s string) string {
	// Trim leading/trailing whitespace for comparison
	result := ""
	for _, line := range splitLines(s) {
		trimmed := trimLine(line)
		if trimmed != "" {
			if result != "" {
				result += "\n"
			}
			result += trimmed
		}
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimLine(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
