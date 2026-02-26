package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/skills"
)

// TestSkillDiscovery tests that skills are discovered from the filesystem.
func TestSkillDiscovery(t *testing.T) {
	// Create a temporary skill directory
	tempDir, err := os.MkdirTemp("", "meept-skills-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test skill file
	skillContent := `---
name: test-skill
description: A test skill for unit testing
requires:
  - code
tags:
  - test
  - example
allowed-tools:
  - file_read
  - shell_execute
risk-level: low
max-iterations: 5
---

# Test Skill Instructions

You are a test skill. Follow these instructions carefully.

1. Read the input
2. Process it
3. Return a response
`
	skillPath := filepath.Join(tempDir, "test-skill.md")
	if err := os.WriteFile(skillPath, []byte(skillContent), 0644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}

	// Create discovery with custom tier
	discovery := skills.NewDiscovery(
		skills.WithTiers([]skills.DiscoveryTier{
			{Path: tempDir, Priority: skills.PriorityProject},
		}),
	)

	// Discover skills
	discovered, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Failed to discover skills: %v", err)
	}

	if len(discovered) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(discovered))
	}

	skill := discovered[0]
	if skill.Name != "test-skill" {
		t.Errorf("Expected skill name 'test-skill', got '%s'", skill.Name)
	}
	if skill.Description != "A test skill for unit testing" {
		t.Errorf("Unexpected description: %s", skill.Description)
	}
	if len(skill.Requires) != 1 || skill.Requires[0] != "code" {
		t.Errorf("Unexpected requires: %v", skill.Requires)
	}
	if len(skill.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(skill.Tags))
	}
	if len(skill.AllowedTools) != 2 {
		t.Errorf("Expected 2 allowed tools, got %d", len(skill.AllowedTools))
	}
	if skill.RiskLevel != "low" {
		t.Errorf("Expected risk level 'low', got '%s'", skill.RiskLevel)
	}
	if skill.MaxIterations != 5 {
		t.Errorf("Expected max iterations 5, got %d", skill.MaxIterations)
	}
}

// TestSkillRegistry tests skill registry operations.
func TestSkillRegistry(t *testing.T) {
	registry := skills.NewRegistry()

	// Register a skill
	skill := &skills.Skill{
		Name:        "registry-test",
		Description: "A skill for registry testing",
		Tags:        []string{"test", "registry"},
		Requires:    []string{"code"},
	}
	registry.Register(skill)

	// Test Get
	retrieved := registry.Get("registry-test")
	if retrieved == nil {
		t.Fatal("Failed to retrieve registered skill")
	}
	if retrieved.Name != "registry-test" {
		t.Errorf("Unexpected skill name: %s", retrieved.Name)
	}

	// Test case-insensitive Get
	retrieved = registry.Get("Registry-Test")
	if retrieved == nil {
		t.Fatal("Failed to retrieve skill with different case")
	}

	// Test List
	allSkills := registry.List()
	if len(allSkills) != 1 {
		t.Errorf("Expected 1 skill in registry, got %d", len(allSkills))
	}

	// Test FindByTag
	taggedSkills := registry.FindByTag("test")
	if len(taggedSkills) != 1 {
		t.Errorf("Expected 1 skill with tag 'test', got %d", len(taggedSkills))
	}

	// Test FindByCapability
	capSkills := registry.FindByCapability("code")
	if len(capSkills) != 1 {
		t.Errorf("Expected 1 skill with capability 'code', got %d", len(capSkills))
	}

	// Test Unregister
	registry.Unregister("registry-test")
	if registry.Get("registry-test") != nil {
		t.Error("Skill should have been unregistered")
	}
}

// TestSkillParsing tests skill file parsing.
func TestSkillParsing(t *testing.T) {
	skillContent := `---
name: parse-test
description: Testing parsing
requires: [code, reasoning]
tags:
  - parsing
  - test
examples:
  - Example usage 1
  - Example usage 2
allowed-tools: [file_read, file_write]
risk-level: medium
max-iterations: 10
temperature: 0.7
max-tokens: 4096
---

# Parse Test Skill

This is the skill body with instructions.
`

	skill, err := skills.ParseSkillText(skillContent)
	if err != nil {
		t.Fatalf("Failed to parse skill: %v", err)
	}

	if skill.Name != "parse-test" {
		t.Errorf("Expected name 'parse-test', got '%s'", skill.Name)
	}
	if len(skill.Requires) != 2 {
		t.Errorf("Expected 2 requires, got %d", len(skill.Requires))
	}
	if len(skill.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(skill.Tags))
	}
	if len(skill.Examples) != 2 {
		t.Errorf("Expected 2 examples, got %d", len(skill.Examples))
	}
	if len(skill.AllowedTools) != 2 {
		t.Errorf("Expected 2 allowed tools, got %d", len(skill.AllowedTools))
	}
	if skill.Temperature == nil || *skill.Temperature != 0.7 {
		t.Errorf("Expected temperature 0.7, got %v", skill.Temperature)
	}
	if skill.MaxTokens == nil || *skill.MaxTokens != 4096 {
		t.Errorf("Expected max_tokens 4096, got %v", skill.MaxTokens)
	}
	if skill.Body == "" {
		t.Error("Expected non-empty body")
	}
}

// TestToolFiltering tests tool filtering for skills.
func TestToolFiltering(t *testing.T) {
	// Create a mock parent registry
	parent := agent.NewPlaceholderToolRegistry()

	// Add some mock tools
	parent.Register(agent.NewMockTool("file_read", "Read files", nil))
	parent.Register(agent.NewMockTool("file_write", "Write files", nil))
	parent.Register(agent.NewMockTool("shell_execute", "Execute shell commands", nil))
	parent.Register(agent.NewMockTool("web_fetch", "Fetch web content", nil))

	// Create filtered registry with only file operations
	filtered := agent.NewFilteredToolRegistry(parent, []string{"file_read", "file_write"})

	// Test that allowed tools are available
	if filtered.Get("file_read") == nil {
		t.Error("file_read should be available in filtered registry")
	}
	if filtered.Get("file_write") == nil {
		t.Error("file_write should be available in filtered registry")
	}

	// Test that non-allowed tools are not available
	if filtered.Get("shell_execute") != nil {
		t.Error("shell_execute should NOT be available in filtered registry")
	}
	if filtered.Get("web_fetch") != nil {
		t.Error("web_fetch should NOT be available in filtered registry")
	}

	// Test List returns only allowed tools
	listedTools := filtered.List()
	if len(listedTools) != 2 {
		t.Errorf("Expected 2 tools in filtered list, got %d", len(listedTools))
	}
}

// TestFilterToolsForSkill tests the FilterToolsForSkill helper.
func TestFilterToolsForSkill(t *testing.T) {
	parent := agent.NewPlaceholderToolRegistry()
	parent.Register(agent.NewMockTool("tool1", "Tool 1", nil))
	parent.Register(agent.NewMockTool("tool2", "Tool 2", nil))
	parent.Register(agent.NewMockTool("tool3", "Tool 3", nil))

	// Test with empty allowed list (should return parent unchanged)
	result := agent.FilterToolsForSkill(parent, []string{})
	if len(result.List()) != 3 {
		t.Error("Empty allowed list should return all tools")
	}

	// Test with specific allowed list
	result = agent.FilterToolsForSkill(parent, []string{"tool1", "tool3"})
	if len(result.List()) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(result.List()))
	}
}

// TestDispatcherSkillInvocation tests skill invocation through the dispatcher.
func TestDispatcherSkillInvocation(t *testing.T) {
	// Create a skill registry with a test skill
	registry := skills.NewRegistry()
	registry.Register(&skills.Skill{
		Name:        "test-dispatch",
		Description: "Test skill for dispatcher",
	})

	// Create dispatcher with skill registry (no executor - we'll test the routing logic)
	dispatcher := agent.NewDispatcher(agent.DispatcherConfig{
		SkillRegistry: registry,
		// SkillExecutor is nil - execution will fail but routing should work
	})

	// Test that skill registry is accessible
	if dispatcher.GetSkillRegistry() == nil {
		t.Error("Skill registry should be accessible from dispatcher")
	}

	// Test that the skill can be found
	if dispatcher.GetSkillRegistry().Get("test-dispatch") == nil {
		t.Error("Test skill should be in dispatcher's registry")
	}

	// Test ClassifyAndRoute with skill invocation - should attempt skill execution
	ctx := context.Background()
	result, err := dispatcher.ClassifyAndRoute(ctx, "/test-dispatch some input", "test-session")

	// Without an executor, this will fail
	if err == nil {
		t.Error("Expected error when executor is nil")
	}

	// Test non-skill invocation falls through to normal routing
	result, err = dispatcher.ClassifyAndRoute(ctx, "hello world", "test-session")
	if err != nil {
		t.Errorf("Non-skill invocation should not error: %v", err)
	}
	if result == nil {
		t.Error("Expected dispatch result for non-skill input")
	}
}

// TestAgentSpecSkillAwareness tests skill-related methods on AgentSpec.
func TestAgentSpecSkillAwareness(t *testing.T) {
	spec := &agent.AgentSpec{
		ID:              "test-agent",
		AvailableSkills: []string{"skill1", "skill2", "skill3"},
		SkillTriggers: map[string]string{
			"review":  "code-review",
			"analyze": "analysis",
		},
	}

	// Test HasSkill
	if !spec.HasSkill("skill1") {
		t.Error("HasSkill should return true for skill1")
	}
	if !spec.HasSkill("skill2") {
		t.Error("HasSkill should return true for skill2")
	}
	if spec.HasSkill("skill4") {
		t.Error("HasSkill should return false for skill4")
	}

	// Test GetSkillForTrigger
	if skill := spec.GetSkillForTrigger("review"); skill != "code-review" {
		t.Errorf("Expected 'code-review' for trigger 'review', got '%s'", skill)
	}
	if skill := spec.GetSkillForTrigger("analyze"); skill != "analysis" {
		t.Errorf("Expected 'analysis' for trigger 'analyze', got '%s'", skill)
	}
	if skill := spec.GetSkillForTrigger("unknown"); skill != "" {
		t.Errorf("Expected empty string for unknown trigger, got '%s'", skill)
	}

	// Test with nil SkillTriggers
	spec2 := &agent.AgentSpec{ID: "test-agent-2"}
	if skill := spec2.GetSkillForTrigger("review"); skill != "" {
		t.Errorf("Expected empty string when SkillTriggers is nil, got '%s'", skill)
	}
}
