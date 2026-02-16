package agent

import (
	"strings"
	"testing"
)

func TestDefaultPromptConfig(t *testing.T) {
	cfg := DefaultPromptConfig()

	if cfg.Constitution == "" {
		t.Error("expected non-empty default constitution")
	}

	if cfg.Restrictions == "" {
		t.Error("expected non-empty default restrictions")
	}

	if cfg.Purpose == "" {
		t.Error("expected non-empty default purpose")
	}
}

func TestNewPromptBuilder(t *testing.T) {
	builder := NewPromptBuilder()

	if builder == nil {
		t.Fatal("NewPromptBuilder returned nil")
	}

	prompt := builder.Build()
	if prompt == "" {
		t.Error("expected non-empty prompt from default builder")
	}
}

func TestPromptBuilderFluent(t *testing.T) {
	prompt := NewPromptBuilder().
		WithConstitution("Custom constitution").
		WithRestrictions("Custom restrictions").
		WithPurpose("Custom purpose").
		WithPersonality("Friendly").
		Build()

	if !strings.Contains(prompt, "Custom constitution") {
		t.Error("prompt missing custom constitution")
	}

	if !strings.Contains(prompt, "Custom restrictions") {
		t.Error("prompt missing custom restrictions")
	}

	if !strings.Contains(prompt, "Custom purpose") {
		t.Error("prompt missing custom purpose")
	}

	if !strings.Contains(prompt, "Friendly") {
		t.Error("prompt missing personality")
	}
}

func TestPromptBuilderWithTools(t *testing.T) {
	tools := []ToolDescription{
		{
			Name:        "read_file",
			Description: "Read a file from disk",
			Parameters: []ToolParameter{
				{Name: "path", Type: "string", Required: true},
			},
		},
		{
			Name:        "write_file",
			Description: "Write content to a file",
			Parameters: []ToolParameter{
				{Name: "path", Type: "string", Required: true},
				{Name: "content", Type: "string", Required: true},
				{Name: "append", Type: "boolean", Required: false},
			},
		},
	}

	prompt := NewPromptBuilder().
		WithTools(tools).
		Build()

	if !strings.Contains(prompt, "# Available Tools") {
		t.Error("prompt missing tools section")
	}

	if !strings.Contains(prompt, "read_file") {
		t.Error("prompt missing read_file tool")
	}

	if !strings.Contains(prompt, "write_file") {
		t.Error("prompt missing write_file tool")
	}

	if !strings.Contains(prompt, "(optional)") {
		t.Error("prompt should show optional parameter")
	}
}

func TestPromptBuilderAddTool(t *testing.T) {
	prompt := NewPromptBuilder().
		AddTool(ToolDescription{
			Name:        "tool1",
			Description: "First tool",
		}).
		AddTool(ToolDescription{
			Name:        "tool2",
			Description: "Second tool",
		}).
		Build()

	if !strings.Contains(prompt, "tool1") {
		t.Error("prompt missing tool1")
	}

	if !strings.Contains(prompt, "tool2") {
		t.Error("prompt missing tool2")
	}
}

func TestPromptBuilderWithMemoryContext(t *testing.T) {
	prompt := NewPromptBuilder().
		WithMemoryContext("User prefers dark mode").
		Build()

	if !strings.Contains(prompt, "# Relevant Context from Memory") {
		t.Error("prompt missing memory context section")
	}

	if !strings.Contains(prompt, "User prefers dark mode") {
		t.Error("prompt missing memory context content")
	}
}

func TestPromptBuilderWithUserPreferences(t *testing.T) {
	prefs := map[string]string{
		"timezone": "UTC",
		"language": "English",
	}

	prompt := NewPromptBuilder().
		WithUserPreferences(prefs).
		Build()

	if !strings.Contains(prompt, "# User Preferences") {
		t.Error("prompt missing user preferences section")
	}

	if !strings.Contains(prompt, "timezone: UTC") {
		t.Error("prompt missing timezone preference")
	}
}

func TestPromptBuilderAddUserPreference(t *testing.T) {
	prompt := NewPromptBuilder().
		AddUserPreference("theme", "dark").
		AddUserPreference("font", "monospace").
		Build()

	if !strings.Contains(prompt, "theme: dark") {
		t.Error("prompt missing theme preference")
	}

	if !strings.Contains(prompt, "font: monospace") {
		t.Error("prompt missing font preference")
	}
}

func TestPromptBuilderAddSection(t *testing.T) {
	prompt := NewPromptBuilder().
		AddSection("Custom Section", "Custom content here").
		Build()

	if !strings.Contains(prompt, "# Custom Section") {
		t.Error("prompt missing custom section title")
	}

	if !strings.Contains(prompt, "Custom content here") {
		t.Error("prompt missing custom section content")
	}
}

func TestPromptBuilderFromConfig(t *testing.T) {
	cfg := PromptConfig{
		Constitution: "Config constitution",
		Restrictions: "Config restrictions",
		Purpose:      "Config purpose",
		Personality:  "Config personality",
	}

	prompt := NewPromptBuilderFromConfig(cfg).Build()

	if !strings.Contains(prompt, "Config constitution") {
		t.Error("prompt missing config constitution")
	}

	if !strings.Contains(prompt, "Config restrictions") {
		t.Error("prompt missing config restrictions")
	}

	if !strings.Contains(prompt, "Config purpose") {
		t.Error("prompt missing config purpose")
	}

	if !strings.Contains(prompt, "Config personality") {
		t.Error("prompt missing config personality")
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	cfg := DefaultPromptConfig()
	tools := []ToolDescription{
		{Name: "test_tool", Description: "A test tool"},
	}

	prompt := BuildSystemPrompt(cfg, tools, "Memory context")

	if !strings.Contains(prompt, DefaultConstitution) {
		t.Error("prompt missing default constitution")
	}

	if !strings.Contains(prompt, "test_tool") {
		t.Error("prompt missing tool")
	}

	if !strings.Contains(prompt, "Memory context") {
		t.Error("prompt missing memory context")
	}
}

func TestBuildSystemPromptWithOverride(t *testing.T) {
	override := "This is a complete custom prompt"
	tools := []ToolDescription{
		{Name: "tool1", Description: "Tool 1"},
	}

	prompt := BuildSystemPromptWithOverride(override, tools)

	if !strings.HasPrefix(prompt, override) {
		t.Error("prompt should start with override")
	}

	if !strings.Contains(prompt, "# Available Tools") {
		t.Error("prompt should include tools section")
	}

	if !strings.Contains(prompt, "tool1") {
		t.Error("prompt should include tool")
	}
}

func TestBuildSystemPromptWithOverrideNoTools(t *testing.T) {
	override := "Complete custom prompt"

	prompt := BuildSystemPromptWithOverride(override, nil)

	if prompt != override {
		t.Errorf("expected exact override, got '%s'", prompt)
	}
}

func TestBuildSystemPromptWithEmptyOverride(t *testing.T) {
	tools := []ToolDescription{
		{Name: "tool1", Description: "Tool 1"},
	}

	prompt := BuildSystemPromptWithOverride("", tools)

	// Should fall back to default prompt
	if !strings.Contains(prompt, DefaultConstitution) {
		t.Error("should use default prompt when override is empty")
	}
}

func TestToolsFromDefinitions(t *testing.T) {
	definitions := []ToolDefinitionInfo{
		{
			Name:        "file_read",
			Description: "Read a file",
			Parameters: []ToolParameterInfo{
				{Name: "path", Type: "string", Required: true},
			},
		},
		{
			Name:        "file_write",
			Description: "Write a file",
			Parameters: []ToolParameterInfo{
				{Name: "path", Type: "string", Required: true},
				{Name: "content", Type: "string", Required: true},
				{Name: "append", Type: "boolean", Required: false},
			},
		},
	}

	tools := ToolsFromDefinitions(definitions)

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	if tools[0].Name != "file_read" {
		t.Errorf("expected 'file_read', got '%s'", tools[0].Name)
	}

	if len(tools[0].Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(tools[0].Parameters))
	}

	if len(tools[1].Parameters) != 3 {
		t.Errorf("expected 3 parameters, got %d", len(tools[1].Parameters))
	}

	// Check optional parameter
	found := false
	for _, p := range tools[1].Parameters {
		if p.Name == "append" && !p.Required {
			found = true
			break
		}
	}
	if !found {
		t.Error("append parameter should be optional")
	}
}

func TestFormatToolDescription(t *testing.T) {
	tool := ToolDescription{
		Name:        "example_tool",
		Description: "An example tool",
		Parameters: []ToolParameter{
			{Name: "required_param", Type: "string", Required: true},
			{Name: "optional_param", Type: "int", Required: false},
		},
	}

	formatted := formatToolDescription(tool)

	if !strings.Contains(formatted, "**example_tool**") {
		t.Error("should contain bold tool name")
	}

	if !strings.Contains(formatted, "An example tool") {
		t.Error("should contain description")
	}

	if !strings.Contains(formatted, "required_param: string") {
		t.Error("should contain required parameter")
	}

	if !strings.Contains(formatted, "optional_param: int (optional)") {
		t.Error("should mark optional parameter")
	}
}

func TestPromptSectionOrder(t *testing.T) {
	prompt := NewPromptBuilder().
		WithConstitution("Constitution").
		WithRestrictions("Restrictions").
		WithPurpose("Purpose").
		WithPersonality("Personality").
		WithUserPreferences(map[string]string{"pref": "value"}).
		WithMemoryContext("Memory").
		WithTools([]ToolDescription{{Name: "tool", Description: "desc"}}).
		AddSection("Custom", "Content").
		Build()

	// Check order by finding positions
	sections := []string{
		"# Constitution",
		"# Safety Restrictions",
		"# Purpose",
		"# Personality",
		"# User Preferences",
		"# Relevant Context",
		"# Available Tools",
		"# Custom",
	}

	lastPos := -1
	for _, section := range sections {
		pos := strings.Index(prompt, section)
		if pos == -1 {
			t.Errorf("missing section: %s", section)
			continue
		}
		if pos < lastPos {
			t.Errorf("section %s is out of order", section)
		}
		lastPos = pos
	}
}

func TestEmptyPromptBuilder(t *testing.T) {
	// Test with all empty values
	builder := &PromptBuilder{
		constitution: "",
		restrictions: "",
		purpose:      "",
		personality:  "",
	}

	prompt := builder.Build()

	// Should produce an empty or minimal prompt
	if strings.Contains(prompt, "# Constitution") {
		t.Error("should not have constitution section when empty")
	}
}
