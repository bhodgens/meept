package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCLAUDEMD(t *testing.T) {
	// Create a temporary file
	tmpDir, err := os.MkdirTemp("", "test-claude-md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	claudeMDPath := filepath.Join(tmpDir, "CLAUDE.md")
	claudeMDContent := strings.Join([]string{
		"# CLAUDE.md",
		"",
		"## Build Commands",
		"",
		"```bash",
		"go build -o bin/app ./cmd/app",
		"go test ./... -v",
		"go run ./cmd/app",
		"```",
		"",
		"## Architecture Overview",
		"",
		"### Request Flow",
		"",
		"1. User Input",
		"2. Processing",
		"3. Response",
		"",
		"### Key Components",
		"",
		"| Layer | Packages |",
		"|-------|----------|",
		"| Core | internal/core, internal/utils |",
		"| API | internal/api, internal/routes |",
		"",
		"## Multi-Agent Architecture",
		"",
		"| Agent ID | Role | Purpose |",
		"|----------|------|---------|",
		"| coder | Coder | Writes code |",
		"| debugger | Debugger | Fixes bugs |",
		"",
		"## Code Conventions",
		"",
		"- Use Go 1.21+",
		"- Follow effective Go",
		"- Write tests for all code",
		"",
		"## Configuration",
		"",
		"Main config: ~/.app/config.toml",
	}, "\n")

	//nolint:gosec // test directory/file
	if err := os.WriteFile(claudeMDPath, []byte(claudeMDContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Parse file
	doc, err := ParseCLAUDEMD(claudeMDPath)
	if err != nil {
		t.Fatalf("ParseCLAUDEMD() error = %v", err)
	}

	// Check basic fields
	if doc.Path != claudeMDPath {
		t.Errorf("Path = %v, want %v", doc.Path, claudeMDPath)
	}
	if doc.RawContent != claudeMDContent {
		t.Error("RawContent not preserved")
	}
	if doc.WorkingDir != tmpDir {
		t.Errorf("WorkingDir = %v, want %v", doc.WorkingDir, tmpDir)
	}

	// Check build commands
	if len(doc.BuildCommands) == 0 {
		t.Error("No build commands extracted")
	}

	// Check architecture
	if doc.Architecture == nil {
		t.Error("Architecture section not extracted")
	}

	// Check components
	if len(doc.Components) == 0 {
		t.Error("No components extracted")
	}

	// Check agents
	if len(doc.Agents) == 0 {
		t.Error("No agents extracted")
	}

	// Check conventions
	if doc.Conventions == nil {
		t.Error("Conventions section not extracted")
	}
}

func TestExtractBuildCommands(t *testing.T) {
	content := strings.Join([]string{
		"## Build Commands",
		"",
		"```bash",
		"go build -o bin/app ./cmd/app",
		"go test ./... -v",
		"go run ./cmd/app",
		"```",
		"",
		"Build the application:",
		"```bash",
		"make build",
		"```",
	}, "\n")

	commands := extractBuildCommands(content)
	if len(commands) == 0 {
		t.Error("No commands extracted")
	}

	// Check categories
	buildFound := false
	testFound := false
	for _, cmd := range commands {
		if cmd.Category == "build" {
			buildFound = true
		}
		if cmd.Category == "test" {
			testFound = true
		}
	}

	if !buildFound {
		t.Error("No build commands found")
	}
	if !testFound {
		t.Error("No test commands found")
	}
}

func TestInferCommandCategory(t *testing.T) {
	tests := []struct {
		command  string
		expected string
	}{
		{"go build", "build"},
		{"go test", "test"},
		{"go run", "run"},
		{"make deploy", "deploy"},
		{"go install", "install"},
		{"make clean", "clean"},
		{"golangci-lint run", "lint"},
		{"go fmt ./...", "format"},
		{"echo hello", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := inferCommandCategory(tt.command); got != tt.expected {
				t.Errorf("inferCommandCategory() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFindSection(t *testing.T) {
	content := strings.Join([]string{
		"# Document",
		"",
		"## First Section",
		"",
		"Some content here.",
		"",
		"## Second Section",
		"",
		"More content here.",
		"",
		"### Subsection",
		"",
		"Subsection content.",
		"",
		"## Third Section",
		"",
		"Final content.",
	}, "\n")

	tests := []struct {
		name    string
		titles  []string
		wantLen int
	}{
		{
			name:    "find first section",
			titles:  []string{"First Section"},
			wantLen: 2, // "Some content here." + empty line
		},
		{
			name:    "find second section",
			titles:  []string{"Second Section"},
			wantLen: 4, // "More content here." + subsection + empty lines
		},
		{
			name:    "try multiple titles",
			titles:  []string{"NonExistent", "Third Section"},
			wantLen: 1, // "Final content." (last section, no trailing content)
		},
		{
			name:    "section not found",
			titles:  []string{"NonExistent"},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section := findSection(content, tt.titles...)
			lines := len(strings.Split(section, "\n"))
			if lines < tt.wantLen {
				t.Errorf("findSection() returned %d lines, want at least %d", lines, tt.wantLen)
			}
		})
	}
}

func TestExtractComponentsTable(t *testing.T) {
	content := strings.Join([]string{
		"### Key Components",
		"",
		"| Layer | Packages |",
		"|-------|----------|",
		"| Core | internal/core, internal/utils |",
		"| API | internal/api, internal/routes |",
		"| DB | internal/db |",
		"",
		"Other text here.",
	}, "\n")

	components := extractComponentsTable(content)
	if len(components) != 3 {
		t.Errorf("Expected 3 components, got %d", len(components))
	}

	// Check first component
	if components[0].Layer != "Core" {
		t.Errorf("First component layer = %v, want Core", components[0].Layer)
	}
	if len(components[0].Packages) != 2 {
		t.Errorf("First component has %d packages, want 2", len(components[0].Packages))
	}
}

func TestExtractAgents(t *testing.T) {
	content := strings.Join([]string{
		"## Multi-Agent Architecture",
		"",
		"| Agent ID | Role | Purpose |",
		"|----------|------|---------|",
		"| coder | Coder | Writes code |",
		"| debugger | Debugger | Fixes bugs |",
		"| planner | Planner | Plans tasks |",
		"",
		"Agents work together.",
	}, "\n")

	agents := extractAgents(content)
	if len(agents) != 3 {
		t.Errorf("Expected 3 agents, got %d", len(agents))
	}

	// Check first agent
	if agents[0].ID != "coder" {
		t.Errorf("First agent ID = %v, want coder", agents[0].ID)
	}
	if agents[0].Role != "Coder" {
		t.Errorf("First agent role = %v, want Coder", agents[0].Role)
	}
	if agents[0].Purpose != "Writes code" {
		t.Errorf("First agent purpose = %v, want 'Writes code'", agents[0].Purpose)
	}
}

func TestParseSkillFile(t *testing.T) {
	// Create a temporary skill file
	tmpDir, err := os.MkdirTemp("", "test-skill")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	skillDir := filepath.Join(tmpDir, "test-skill")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	skillPath := filepath.Join(skillDir, "SKILL.md")
	skillContent := strings.Join([]string{
		"---",
		"name: Test Skill",
		"description: A test skill for testing",
		"version: 1.0.0",
		"requires: [code, reasoning]",
		"---",
		"",
		"This is a test skill content.",
		"",
		"It provides useful capabilities.",
	}, "\n")

	//nolint:gosec // test directory/file
	if err := os.WriteFile(skillPath, []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Parse skill
	skill, err := ParseSkillFile(skillPath)
	if err != nil {
		t.Fatalf("ParseSkillFile() error = %v", err)
	}

	// Check fields
	if skill.Name != "Test Skill" {
		t.Errorf("Name = %v, want 'Test Skill'", skill.Name)
	}
	if skill.Description != "A test skill for testing" {
		t.Errorf("Description = %v, want 'A test skill for testing'", skill.Description)
	}
	if skill.Version != "1.0.0" {
		t.Errorf("Version = %v, want '1.0.0'", skill.Version)
	}
	if len(skill.Requires) != 2 {
		t.Errorf("Requires = %v, want 2 items", skill.Requires)
	}
	if skill.Slug != "test-skill" {
		t.Errorf("Slug = %v, want 'test-skill'", skill.Slug)
	}
}

func TestExtractYAMLFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantFront   string
		wantBody    string
		expectError bool
	}{
		{
			name: "valid frontmatter",
			content: strings.Join([]string{
				"---",
				"name: test",
				"version: 1.0",
				"---",
				"Body content here.",
			}, "\n"),
			wantFront:   "name: test\nversion: 1.0",
			wantBody:    "Body content here.",
			expectError: false,
		},
		{
			name:        "no frontmatter",
			content:     "Just body content",
			wantFront:   "",
			wantBody:    "",
			expectError: true,
		},
		{
			name:        "empty content",
			content:     "",
			wantFront:   "",
			wantBody:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			front, body, err := extractYAMLFrontmatter(tt.content)
			if (err != nil) != tt.expectError {
				t.Errorf("extractYAMLFrontmatter() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if !tt.expectError {
				if front != tt.wantFront {
					t.Errorf("Frontmatter = %v, want %v", front, tt.wantFront)
				}
				if body != tt.wantBody {
					t.Errorf("Body = %v, want %v", body, tt.wantBody)
				}
			}
		})
	}
}

func TestParseSkillFrontmatter_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		frontmatter   string
		wantName      string
		wantDesc      string
		wantVersion   string
		wantRequires  []string
		wantErr       bool
	}{
		{
			name: "basic fields",
			frontmatter: strings.Join([]string{
				"name: Test Skill",
				"description: A test skill",
				"version: 1.0.0",
				"requires: [code, reasoning]",
			}, "\n"),
			wantName:     "Test Skill",
			wantDesc:     "A test skill",
			wantVersion:  "1.0.0",
			wantRequires: []string{"code", "reasoning"},
		},
		{
			name: "description with colon inside quotes",
			frontmatter: strings.Join([]string{
				"name: Colon Skill",
				`description: "Use this skill when: foo, bar"`,
				"version: \"1.2.3\"",
			}, "\n"),
			wantName:    "Colon Skill",
			wantDesc:    "Use this skill when: foo, bar",
			wantVersion: "1.2.3",
		},
		{
			name: "list-style requires",
			frontmatter: strings.Join([]string{
				"name: List Skill",
				"description: A skill with list",
				"requires:",
				"  - code",
				"  - reasoning",
				"  - analysis",
			}, "\n"),
			wantName:     "List Skill",
			wantDesc:     "A skill with list",
			wantVersion:  "0.1.0",
			wantRequires: []string{"code", "reasoning", "analysis"},
		},
		{
			name: "empty version defaults",
			frontmatter: strings.Join([]string{
				"name: No Version",
				"description: No version here",
			}, "\n"),
			wantName:    "No Version",
			wantDesc:    "No version here",
			wantVersion: "0.1.0",
		},
		{
			name: "missing name errors",
			frontmatter: strings.Join([]string{
				"description: Missing name",
			}, "\n"),
			wantErr: true,
		},
		{
			name: "missing description errors",
			frontmatter: strings.Join([]string{
				"name: No Description",
			}, "\n"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skill := &Skill{Slug: "test"}
			err := parseSkillFrontmatter(tt.frontmatter, skill)
			if tt.wantErr {
				require.Error(t, err, "expected error")
				return
			}
			require.NoError(t, err, "unexpected error")
			assert.Equal(t, tt.wantName, skill.Name, "Name mismatch")
			assert.Equal(t, tt.wantDesc, skill.Description, "Description mismatch")
			assert.Equal(t, tt.wantVersion, skill.Version, "Version mismatch")
			assert.Equal(t, tt.wantRequires, skill.Requires, "Requires mismatch")
		})
	}
}

func TestInferSkillCategory(t *testing.T) {
	tests := []struct {
		slug     string
		path     string
		expected string
	}{
		{"agent-dev", "/path/to/agent-dev", "agent"},
		{"mermaid-diagrams", "/path/to/mermaid-diagrams", "visualization"},
		{"docx-parser", "/path/to/docx-parser", "document"},
		{"playwright-test", "/path/to/playwright-test", "testing"},
		{"react-best-practices", "/path/to/react-best-practices", "frontend"},
		{"architect-tools", "/path/to/architect-tools", "architecture"},
		{"devops-pipeline", "/path/to/devops-pipeline", "devops"},
		{"random-skill", "/path/to/random-skill", "general"},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			if got := inferSkillCategory(tt.slug, tt.path); got != tt.expected {
				t.Errorf("inferSkillCategory() = %v, want %v", got, tt.expected)
			}
		})
	}
}
