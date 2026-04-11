package agents

import (
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantFM      string
		wantBody    string
		wantErr     bool
		errContains string
	}{
		{
			name: "basic frontmatter",
			input: `---
id: coder
name: Code Specialist
---

# Code Specialist

You write code.`,
			wantFM:   "id: coder\nname: Code Specialist",
			wantBody: "\n# Code Specialist\n\nYou write code.",
		},
		{
			name: "frontmatter with leading whitespace",
			input: `
---
id: test
---
Body here.`,
			wantFM:   "id: test",
			wantBody: "Body here.",
		},
		{
			name:        "no frontmatter",
			input:       "Just some text without frontmatter.",
			wantErr:     true,
			errContains: "no YAML frontmatter",
		},
		{
			name: "empty frontmatter",
			input: `---
---
Body only.`,
			wantFM:   "",
			wantBody: "Body only.",
		},
		{
			name: "complex frontmatter",
			input: `---
id: coder
name: Code Specialist
role: executor
additional_tools:
  - file_read
  - file_write
temperature: 0.3
---

# Code Specialist

## Principles
- Read before writing
`,
			wantFM: `id: coder
name: Code Specialist
role: executor
additional_tools:
  - file_read
  - file_write
temperature: 0.3`,
			wantBody: "\n# Code Specialist\n\n## Principles\n- Read before writing\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := splitFrontmatter(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if fm != tt.wantFM {
				t.Errorf("frontmatter mismatch:\ngot:  %q\nwant: %q", fm, tt.wantFM)
			}

			if body != tt.wantBody {
				t.Errorf("body mismatch:\ngot:  %q\nwant: %q", body, tt.wantBody)
			}
		})
	}
}

func TestParseAgentText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr bool
	}{
		{
			name: "basic agent",
			input: `---
id: coder
name: Code Specialist
role: executor
---

# Code Specialist

You implement code.`,
			wantID: "coder",
		},
		{
			name: "agent with all fields",
			input: `---
id: debugger
name: Debug Specialist
role: executor
model: smart
additional_tools:
  - file_read
  - shell_execute
capabilities:
  - code
  - reasoning
max_iterations: 20
timeout_seconds: 600
temperature: 0.2
---

# Debug Specialist

You debug code.`,
			wantID: "debugger",
		},
		{
			name: "no id",
			input: `---
name: Missing ID
---
Body`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, err := ParseAgentText(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if def.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", def.ID, tt.wantID)
			}
		})
	}
}

func TestParseAgentText_AllFields(t *testing.T) {
	input := `---
id: test-agent
name: Test Agent
role: executor
model: fast
additional_tools:
  - tool1
  - tool2
capabilities:
  - code
available_skills:
  - skill1
skill_triggers:
  keyword1: skill1
max_iterations: 15
timeout_seconds: 300
max_tokens_per_turn: 2048
max_memory_refs: 10
temperature: 0.5
top_p: 0.9
---

# Test Agent Instructions

Do testing.`

	def, err := ParseAgentText(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check all fields
	if def.ID != "test-agent" {
		t.Errorf("ID = %q, want %q", def.ID, "test-agent")
	}
	if def.Name != "Test Agent" {
		t.Errorf("Name = %q, want %q", def.Name, "Test Agent")
	}
	if def.Role != "executor" {
		t.Errorf("Role = %q, want %q", def.Role, "executor")
	}
	if def.Model != "fast" {
		t.Errorf("Model = %q, want %q", def.Model, "fast")
	}
	if len(def.AdditionalTools) != 2 {
		t.Errorf("AdditionalTools len = %d, want 2", len(def.AdditionalTools))
	}
	if len(def.Capabilities) != 1 || def.Capabilities[0] != "code" {
		t.Errorf("Capabilities = %v, want [code]", def.Capabilities)
	}
	if len(def.AvailableSkills) != 1 {
		t.Errorf("AvailableSkills len = %d, want 1", len(def.AvailableSkills))
	}
	if def.SkillTriggers["keyword1"] != "skill1" {
		t.Errorf("SkillTriggers[keyword1] = %q, want %q", def.SkillTriggers["keyword1"], "skill1")
	}
	if def.MaxIterations != 15 {
		t.Errorf("MaxIterations = %d, want 15", def.MaxIterations)
	}
	if def.TimeoutSeconds != 300 {
		t.Errorf("TimeoutSeconds = %d, want 300", def.TimeoutSeconds)
	}
	if def.MaxTokensPerTurn != 2048 {
		t.Errorf("MaxTokensPerTurn = %d, want 2048", def.MaxTokensPerTurn)
	}
	if def.MaxMemoryRefs != 10 {
		t.Errorf("MaxMemoryRefs = %d, want 10", def.MaxMemoryRefs)
	}
	if def.Temperature == nil || *def.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", def.Temperature)
	}
	if def.TopP == nil || *def.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", def.TopP)
	}
	if def.Body != "# Test Agent Instructions\n\nDo testing." {
		t.Errorf("Body = %q", def.Body)
	}
}
