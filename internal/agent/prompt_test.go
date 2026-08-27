package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
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

func TestPromptBuilderWithProjectInfo(t *testing.T) {
	prompt := NewPromptBuilder().
		WithProjectInfo("meept", "/Users/caimlas/git/meept", "main", "go", true).
		Build()

	if !strings.Contains(prompt, "# Current Project") {
		t.Error("prompt missing current project section")
	}
	if !strings.Contains(prompt, "Name: meept") {
		t.Error("prompt missing project name")
	}
	if !strings.Contains(prompt, "Path: /Users/caimlas/git/meept") {
		t.Error("prompt missing project path")
	}
	if !strings.Contains(prompt, "Branch: main (dirty)") {
		t.Error("prompt should include dirty branch")
	}
	if !strings.Contains(prompt, "Language: go") {
		t.Error("prompt missing language")
	}
}

func TestPromptBuilderWithProjectInfoClean(t *testing.T) {
	prompt := NewPromptBuilder().
		WithProjectInfo("meept", "/Users/caimlas/git/meept", "main", "", false).
		Build()

	if !strings.Contains(prompt, "Branch: main") {
		t.Error("prompt missing branch")
	}
	if strings.Contains(prompt, "(dirty)") {
		t.Error("clean project should not include dirty marker")
	}
}

func TestProjectInfoBeforeMemoryContext(t *testing.T) {
	prompt := NewPromptBuilder().
		WithProjectInfo("meept", "/path", "main", "", false).
		WithMemoryContext("some memory").
		Build()

	projPos := strings.Index(prompt, "# Current Project")
	memPos := strings.Index(prompt, "# Relevant Context")
	if projPos == -1 || memPos == -1 {
		t.Fatalf("project or memory section missing (proj=%d mem=%d)", projPos, memPos)
	}
	if projPos > memPos {
		t.Errorf("current project section should appear before memory context (proj=%d mem=%d)", projPos, memPos)
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

// --- Stable-prefix prompt assembly tests (loop-economics leaf 01) ---

// sha256HexOf returns the hex-encoded sha256 of s (test helper).
func sha256HexOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestAssembleOrderedStableBeforeUnstable(t *testing.T) {
	sections := []PromptSection{
		{Name: "memory", Stable: false, Body: "memory body"},
		{Name: "constitution", Stable: true, Body: "constitution body"},
		{Name: "tools", Stable: false, Body: "tools body"},
		{Name: "rules", Stable: true, Body: "rules body"},
	}

	prompt, _ := AssembleOrdered(sections)

	constPos := strings.Index(prompt, "constitution body")
	rulesPos := strings.Index(prompt, "rules body")
	memPos := strings.Index(prompt, "memory body")
	toolsPos := strings.Index(prompt, "tools body")
	for name, pos := range map[string]int{"constitution": constPos, "rules": rulesPos, "memory": memPos, "tools": toolsPos} {
		if pos == -1 {
			t.Fatalf("section %s missing from prompt", name)
		}
	}
	// Stable sections first, in given order; then unstable, in given order.
	if !(constPos < rulesPos && rulesPos < memPos && memPos < toolsPos) {
		t.Errorf("wrong order: constitution=%d rules=%d memory=%d tools=%d", constPos, rulesPos, memPos, toolsPos)
	}
}

func TestAssembleOrderedByteStability(t *testing.T) {
	sections := []PromptSection{
		{Name: "identity", Stable: true, Body: "you are meept"},
		{Name: "memory", Stable: false, Body: "user prefers dark mode"},
	}

	p1, h1 := AssembleOrdered(sections)
	p2, h2 := AssembleOrdered(sections)

	if p1 != p2 {
		t.Error("two calls with identical inputs must produce byte-identical prompts")
	}
	if h1 != h2 {
		t.Error("two calls with identical inputs must produce identical hashes")
	}
	wantHash := sha256HexOf("you are meept")
	if h1 != wantHash {
		t.Errorf("hash mismatch: got %s want %s (sha256 over stable prefix bytes)", h1, wantHash)
	}
}

func TestAssembleOrderedVolatilityIsolation(t *testing.T) {
	stable := []PromptSection{
		{Name: "identity", Stable: true, Body: "stable identity"},
		{Name: "rules", Stable: true, Body: "stable rules"},
	}

	_, h1 := AssembleOrdered(append(append([]PromptSection{}, stable...),
		PromptSection{Name: "memory", Stable: false, Body: "turn one memory"}))
	_, h2 := AssembleOrdered(append(append([]PromptSection{}, stable...),
		PromptSection{Name: "memory", Stable: false, Body: "turn two memory, totally different"}))

	if h1 != h2 {
		t.Errorf("changing an unstable section must not change the stable prefix hash: %s vs %s", h1, h2)
	}
}

func TestAssembleOrderedEmptyStableHash(t *testing.T) {
	prompt, hash := AssembleOrdered([]PromptSection{
		{Name: "memory", Stable: false, Body: "only unstable content"},
	})

	want := sha256HexOf("")
	if hash != want {
		t.Errorf("empty stable set must hash to sha256(\"\"), got %s want %s", hash, want)
	}
	if !strings.Contains(prompt, "only unstable content") {
		t.Error("unstable content must still appear in the assembled prompt")
	}
}

func TestAssembleOrderedEmptyInput(t *testing.T) {
	prompt, hash := AssembleOrdered(nil)
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
	if hash != sha256HexOf("") {
		t.Errorf("expected sha256 of empty string, got %s", hash)
	}
}

func TestAssembleOrderedEmptyBodiesSkipped(t *testing.T) {
	prompt, hash := AssembleOrdered([]PromptSection{
		{Name: "empty-stable", Stable: true, Body: ""},
		{Name: "identity", Stable: true, Body: "real content"},
		{Name: "empty-unstable", Stable: false, Body: ""},
	})
	if hash != sha256HexOf("real content") {
		t.Errorf("empty stable bodies must not contribute to the hash, got %s", hash)
	}
	if strings.Contains(prompt, "empty-stable") || strings.Contains(prompt, "empty-unstable") {
		t.Error("empty sections must not appear in the prompt")
	}
}

func TestBuildSystemPromptStablePrefixDefault(t *testing.T) {
	// Default path (cache_stable_prefix default true): volatile sections must
	// appear after all stable sections regardless of builder call order.
	b := NewPromptBuilder()
	b.WithMemoryContext("volatile memory content") // added early, but unstable
	b.AddSection("Volatile Custom", "volatile custom content")
	b.WithTools([]ToolDescription{{Name: "tool_a", Description: "a tool"}})

	prompt, hash := b.BuildSystemPrompt()

	memPos := strings.Index(prompt, "# Relevant Context from Memory")
	toolsPos := strings.Index(prompt, "# Available Tools")
	constPos := strings.Index(prompt, "# Constitution")
	if constPos == -1 || memPos == -1 || toolsPos == -1 {
		t.Fatalf("missing sections: constitution=%d memory=%d tools=%d", constPos, memPos, toolsPos)
	}
	if !(constPos < memPos && constPos < toolsPos) {
		t.Errorf("stable constitution must precede unstable sections: constitution=%d memory=%d tools=%d", constPos, memPos, toolsPos)
	}
	if hash == "" {
		t.Error("expected non-empty stable prefix hash")
	}
	if hash != sha256HexOf("") && !strings.Contains(prompt, "# Constitution") {
		t.Error("hash should cover the stable prefix")
	}
}

func TestBuildSystemPromptLegacyFlagOff(t *testing.T) {
	// Flag false: output must match the legacy Build() byte-for-byte.
	b := NewPromptBuilder()
	b.WithMemoryContext("some memory")
	b.WithTools([]ToolDescription{{Name: "tool_a", Description: "a tool"}})

	legacy := b.Build()
	ordered, _ := b.BuildSystemPromptOrdered(false)
	if ordered != legacy {
		t.Errorf("cache_stable_prefix=false must preserve legacy ordering byte-for-byte")
	}
}

func TestBuildSystemPromptTwoTurnDrift(t *testing.T) {
	// The actual point of this leaf: two turns where ONLY the conversation
	// tail changes (messages live outside the system prompt) must produce an
	// identical stable prefix hash. The memory snapshot is frozen per session
	// (Hermes pattern), so it does not disturb the prefix either.
	conv := NewConversation()
	conv.SetMemoryContext("frozen session memory")
	if err := conv.FreezeMemorySnapshot(context.Background()); err != nil {
		t.Fatalf("freeze snapshot: %v", err)
	}

	loop := NewAgentLoop("drift-session", "", WithAgentConfig(DefaultAgentConfig()))

	build := func(userMsg string) string {
		conv.AddMessage(llm.ChatMessage{Role: llm.RoleUser, Content: userMsg})
		loop.buildSystemPromptWithContextAndSkills(context.Background(), conv, nil)
		return loop.LastStablePrefixHash()
	}

	h1 := build("turn one question")
	h2 := build("turn two question, completely different")

	if h1 == "" || h2 == "" {
		t.Fatalf("expected non-empty hashes, got %q and %q", h1, h2)
	}
	if h1 != h2 {
		t.Errorf("stable prefix hash must not drift when only the conversation tail changes: %s vs %s", h1, h2)
	}
}

func TestBuildSystemPromptWithOverrideUntouched(t *testing.T) {
	// Override paths must remain byte-identical to the legacy behavior.
	override := "Complete custom override prompt"
	tools := []ToolDescription{{Name: "tool1", Description: "Tool 1"}}

	got := BuildSystemPromptWithOverride(override, tools)
	if !strings.HasPrefix(got, override) {
		t.Error("override must lead the prompt")
	}
	if !strings.Contains(got, "# Available Tools") || !strings.Contains(got, "tool1") {
		t.Error("override path must still append tools")
	}

	loop := NewAgentLoop("override-session", "", WithAgentConfig(AgentConfig{
		SystemPromptOveride: override,
	}))
	loopPrompt := loop.buildSystemPrompt()
	if loopPrompt != override {
		t.Errorf("loop override path must return the bare override, got %q", loopPrompt)
	}
}

func TestFormatToolDescriptionNoPerTurnValues(t *testing.T) {
	// formatToolDescription must embed only static schema facts (name,
	// description, parameter names/types) — no timestamps, no memory refs.
	tool := ToolDescription{
		Name:        "static_tool",
		Description: "A tool with static metadata",
		Parameters: []ToolParameter{
			{Name: "path", Type: "string", Required: true},
		},
	}
	first := formatToolDescription(tool)
	second := formatToolDescription(tool)
	if first != second {
		t.Error("formatToolDescription must be deterministic across calls")
	}
	if strings.Contains(first, "memory-context") || strings.Contains(first, "System note") {
		t.Error("tool descriptions must not embed memory references")
	}
}

func TestLastStablePrefixHash(t *testing.T) {
	loop := NewAgentLoop("hash-session", "", WithAgentConfig(DefaultAgentConfig()))

	if got := loop.LastStablePrefixHash(); got != "" {
		t.Errorf("expected empty hash before any build, got %q", got)
	}

	conv := NewConversation()
	loop.buildSystemPromptWithContextAndSkills(context.Background(), conv, nil)
	hash := loop.LastStablePrefixHash()
	if hash == "" {
		t.Fatal("expected non-empty hash after build")
	}
	if got := loop.LastStablePrefixHash(); got != hash {
		t.Errorf("LastStablePrefixHash() = %s, want %s", got, hash)
	}
}
