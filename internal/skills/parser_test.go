package skills

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestSkillMetadata_CommaSeparatedListFields verifies that list-typed
// frontmatter fields (allowed-tools, triggers, etc.) decode from BOTH real
// YAML lists and comma-separated string scalars (Claude-format skills).
func TestSkillMetadata_CommaSeparatedListFields(t *testing.T) {
	fm := "name: security-audit\n" +
		"description: audit\n" +
		"allowed-tools: Read, Grep, Glob, Bash, Agent\n" +
		"triggers:\n  - \"/security-audit\"\n  - \"security scan\"\n"
	var meta SkillMetadata
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"Read", "Grep", "Glob", "Bash", "Agent"}
	if !reflect.DeepEqual([]string(meta.AllowedTools), want) {
		t.Errorf("AllowedTools = %v, want %v", meta.AllowedTools, want)
	}
	if !reflect.DeepEqual([]string(meta.Triggers), []string{"/security-audit", "security scan"}) {
		t.Errorf("Triggers (list form) = %v", meta.Triggers)
	}
}

// TestSkillMetadata_AllListFieldsAcceptScalarsAndLists covers every
// tolerant list field in both forms, including empty-segment dropping,
// whitespace trimming for the scalar form, and the alt-name parse passes.
func TestSkillMetadata_AllListFieldsAcceptScalarsAndLists(t *testing.T) {
	fm := "requires: alpha, beta\n" +
		"tags: [x, y]\n" +
		"examples: one, two , ,three\n" +
		"trigger: /run-it\n"
	var meta SkillMetadata
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual([]string(meta.Requires), []string{"alpha", "beta"}) {
		t.Errorf("Requires (scalar) = %v", meta.Requires)
	}
	if !reflect.DeepEqual([]string(meta.Tags), []string{"x", "y"}) {
		t.Errorf("Tags (list) = %v", meta.Tags)
	}
	if !reflect.DeepEqual([]string(meta.Examples), []string{"one", "two", "three"}) {
		t.Errorf("Examples (scalar w/ empties) = %v", meta.Examples)
	}

	// parseMetadata-level behavior: trigger merges into Tags after decoding.
	mergedMeta, err := parseMetadata("name: m\ndescription: m\ntags: [x]\ntrigger: /run-it\n")
	if err != nil {
		t.Fatalf("parseMetadata trigger merge: %v", err)
	}
	if !reflect.DeepEqual([]string(mergedMeta.Tags), []string{"x", "/run-it"}) {
		t.Errorf("Tags (trigger merge via parseMetadata) = %v", mergedMeta.Tags)
	}

	// Alt-name passes: scalar allowed_tools / allowedTools must also decode
	// and merge when the hyphenated key is absent.
	altFM := "name: alt\n" +
		"description: alt\n" +
		"allowed_tools: A, B , C\n"
	altMeta, err := parseMetadata(altFM)
	if err != nil {
		t.Fatalf("parseMetadata alt-name: %v", err)
	}
	if !reflect.DeepEqual([]string(altMeta.AllowedTools), []string{"A", "B", "C"}) {
		t.Errorf("AllowedTools (allowed_tools scalar via parseMetadata) = %v", altMeta.AllowedTools)
	}

	// List-typed fields stay nil (not zero-length non-nil) when absent,
	// preserving "not set" semantics for the alt-name merge passes.
	var absent SkillMetadata
	if err := yaml.Unmarshal([]byte("name: n\ndescription: d\n"), &absent); err != nil {
		t.Fatalf("unmarshal absent: %v", err)
	}
	if absent.Requires != nil || absent.AllowedTools != nil || absent.Triggers != nil {
		t.Errorf("absent list fields should be nil, got requires=%v allowed=%v triggers=%v",
			absent.Requires, absent.AllowedTools, absent.Triggers)
	}
}

// TestParseSkillText_ClaudeStyleSkill is the end-to-end check: a skill file
// mirroring ~/.claude/skills/security-audit/SKILL.md (block-scalar
// description, triggers list, comma-separated allowed-tools) parses without
// error.
func TestParseSkillText_ClaudeStyleSkill(t *testing.T) {
	text := "---\n" +
		"name: security-audit\n" +
		"description: |\n" +
		"  Run a security audit of the current project.\n" +
		"  Checks dependencies and common vulnerability patterns.\n" +
		"triggers:\n" +
		"  - \"/security-audit\"\n" +
		"  - \"security scan\"\n" +
		"allowed-tools: Read, Grep, Glob, Bash, Agent\n" +
		"---\n" +
		"\n" +
		"Audit the project for security issues.\n"

	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText: %v", err)
	}
	if skill.Name != "security-audit" {
		t.Errorf("Name = %q, want security-audit", skill.Name)
	}
	if got, want := len(skill.AllowedTools), 5; got != want {
		t.Errorf("len(AllowedTools) = %d (%v), want %d", got, skill.AllowedTools, want)
	}
	wantTools := []string{"Read", "Grep", "Glob", "Bash", "Agent"}
	if !reflect.DeepEqual(skill.AllowedTools, wantTools) {
		t.Errorf("AllowedTools = %v, want %v", skill.AllowedTools, wantTools)
	}
	// triggers merge into Tags during parseMetadata.
	if !slices.Contains(skill.Tags, "/security-audit") || !slices.Contains(skill.Tags, "security scan") {
		t.Errorf("Tags missing triggers = %v", skill.Tags)
	}
}

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
	} else if skill.Requires[0] != "code" || skill.Requires[1] != "reasoning" {
		t.Errorf("Requires = %v, want [code, reasoning]", skill.Requires)
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

func TestParseSkillText_CamelCaseFieldNames(t *testing.T) {
	text := `---
name: claude-skill
description: Skill with camelCase field names
allowedTools: [shell, file]
riskLevel: high
maxIterations: 3
maxTokens: 500
---

Instructions for a Claude-format skill.
`

	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText failed: %v", err)
	}

	if skill.Name != "claude-skill" {
		t.Errorf("Name = %q, want claude-skill", skill.Name)
	}

	if skill.RiskLevel != "high" {
		t.Errorf("RiskLevel = %q, want high", skill.RiskLevel)
	}

	if skill.MaxIterations != 3 {
		t.Errorf("MaxIterations = %d, want 3", skill.MaxIterations)
	}

	if skill.MaxTokens == nil || *skill.MaxTokens != 500 {
		t.Errorf("MaxTokens = %v, want 500", skill.MaxTokens)
	}

	if len(skill.AllowedTools) != 2 {
		t.Errorf("AllowedTools length = %d, want 2", len(skill.AllowedTools))
	} else if skill.AllowedTools[0] != "shell" || skill.AllowedTools[1] != "file" {
		t.Errorf("AllowedTools = %v, want [shell, file]", skill.AllowedTools)
	}
}

func TestParseSkillText_TriggerFieldMappedToTags(t *testing.T) {
	text := `---
name: triggered-skill
description: Skill with a trigger
trigger: /graphify
---

When triggered, do graph things.
`

	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText failed: %v", err)
	}

	if skill.Name != "triggered-skill" {
		t.Errorf("Name = %q, want triggered-skill", skill.Name)
	}

	if !skill.HasTag("/graphify") {
		t.Error("Trigger field should be mapped to Tags; expected tag '/graphify'")
	}
}

func TestParseSkillText_TriggerFieldNotDuplicated(t *testing.T) {
	text := `---
name: triggered-skill
description: Skill with trigger already in tags
trigger: /graphify
tags: [/graphify, another-tag]
---

Instructions.
`

	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText failed: %v", err)
	}

	// Should have 2 tags: /graphify and another-tag (trigger not duplicated).
	if len(skill.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2 (trigger should not be duplicated)", len(skill.Tags))
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

// TestParseSkillText_StateFlag covers the `state: true` frontmatter flag
// used by SKILL.state mode (arXiv:2608.26263): present ⇒ Skill.State is true,
// absent ⇒ defaults to false.
func TestParseSkillText_StateFlag(t *testing.T) {
	s, err := ParseSkillText("---\nname: t\ndescription: d\nstate: true\n---\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if !s.State {
		t.Fatal("state flag not parsed")
	}

	s2, err := ParseSkillText("---\nname: t\ndescription: d\n---\nbody")
	if err != nil {
		t.Fatal(err)
	}
	if s2.State {
		t.Fatal("state must default false")
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
	//nolint:gosec // test directory/file
	if err := os.WriteFile(skillPath, []byte(skillContent), 0o644); err != nil {
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
		name      string
		input     string
		wantFront string
		wantBody  string
		wantErr   error
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
