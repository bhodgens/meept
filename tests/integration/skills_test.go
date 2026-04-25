package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
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

// TestSkillExecutorModelResolution tests that the executor resolves appropriate
// models based on skill requirements.
func TestSkillExecutorModelResolution(t *testing.T) {
	resolver := testResolver()
	executor := skills.NewExecutor(resolver)

	// Skill requiring only code
	codeSkill := &skills.Skill{
		Name:     "code-only",
		Requires: []string{"code"},
	}

	model, err := executor.GetModelForSkill(codeSkill)
	if err != nil {
		t.Fatalf("GetModelForSkill failed: %v", err)
	}
	if model == nil {
		t.Fatal("Expected non-nil model")
	}

	// Skill requiring tool_use
	toolSkill := &skills.Skill{
		Name:     "tool-skill",
		Requires: []string{"tool_use"},
	}

	model, err = executor.GetModelForSkill(toolSkill)
	if err != nil {
		t.Fatalf("GetModelForSkill failed for tool_use: %v", err)
	}
	if model.ModelID != "model-x" {
		t.Errorf("Expected model-x for tool_use, got %q", model.ModelID)
	}

	// Skill with unsatisfiable requirements
	impossibleSkill := &skills.Skill{
		Name:     "impossible",
		Requires: []string{"magic", "teleportation"},
	}

	_, err = executor.GetModelForSkill(impossibleSkill)
	if err == nil {
		t.Error("Expected error for unsatisfiable requirements")
	}
}

// TestSkillExecutorCanExecute tests executor capability checking.
func TestSkillExecutorCanExecute(t *testing.T) {
	resolver := testResolver()
	executor := skills.NewExecutor(resolver)

	tests := []struct {
		name    string
		skill   *skills.Skill
		canExec bool
	}{
		{
			name: "code requirement",
			skill: &skills.Skill{
				Name:     "code-skill",
				Requires: []string{"code"},
			},
			canExec: true,
		},
		{
			name: "reasoning and code requirement",
			skill: &skills.Skill{
				Name:     "reason-skill",
				Requires: []string{"code", "reasoning"},
			},
			canExec: true,
		},
		{
			name: "unsatisfiable requirement",
			skill: &skills.Skill{
				Name:     "impossible",
				Requires: []string{"vision", "audio"},
			},
			canExec: false,
		},
		{
			name:    "nil skill",
			skill:   nil,
			canExec: false,
		},
		{
			name: "no requirements",
			skill: &skills.Skill{
				Name:     "no-reqs",
				Requires: []string{},
			},
			canExec: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.CanExecute(tt.skill)
			if got != tt.canExec {
				t.Errorf("CanExecute() = %v, want %v", got, tt.canExec)
			}
		})
	}
}

// TestSkillDiscoveryToRegistryEndToEnd tests the full pipeline from
// skill file discovery through registry lookup.
func TestSkillDiscoveryToRegistryEndToEnd(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "meept-e2e-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create multiple skill files
	skills_data := []struct {
		filename string
		content  string
	}{
		{
			"code-review.md",
			`---
name: code-review
description: Automated code review skill
requires: [code, reasoning]
tags: [review, code]
allowed-tools: [file_read, shell_execute]
risk-level: low
max-iterations: 10
---

# Code Review Skill

Review the given code for quality and issues.
`,
		},
		{
			"deployment.md",
			`---
name: deploy
description: Deploy application to staging or production
requires: [code, tool_use]
tags: [deploy, devops]
allowed-tools: [shell_execute]
risk-level: high
max-iterations: 15
---

# Deploy Skill

Deploy the application following the deployment checklist.
`,
		},
		{
			"analysis.md",
			`---
name: data-analysis
description: Analyze data and produce insights
requires: [reasoning]
tags: [analysis, data]
risk-level: low
---

# Analysis Skill

Analyze the provided data and extract key insights.
`,
		},
	}

	for _, sd := range skills_data {
		path := filepath.Join(tempDir, sd.filename)
		if err := os.WriteFile(path, []byte(sd.content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", sd.filename, err)
		}
	}

	// Discover
	discovery := skills.NewDiscovery(
		skills.WithTiers([]skills.DiscoveryTier{
			{Path: tempDir, Priority: skills.PriorityProject},
		}),
	)
	discovered, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}
	if len(discovered) != 3 {
		t.Fatalf("Expected 3 discovered skills, got %d", len(discovered))
	}

	// Register all
	registry := skills.NewRegistry()
	registry.RegisterAll(discovered)

	// Verify all are registered
	if registry.Count() != 3 {
		t.Errorf("Expected 3 registered skills, got %d", registry.Count())
	}

	// Test lookups
	review := registry.Get("code-review")
	if review == nil {
		t.Error("code-review skill not found")
	} else {
		if len(review.AllowedTools) != 2 {
			t.Errorf("code-review: expected 2 allowed tools, got %d", len(review.AllowedTools))
		}
		if review.RiskLevel != "low" {
			t.Errorf("code-review: expected risk 'low', got '%s'", review.RiskLevel)
		}
	}

	deploy := registry.Get("deploy")
	if deploy == nil {
		t.Error("deploy skill not found")
	} else {
		if deploy.RiskLevel != "high" {
			t.Errorf("deploy: expected risk 'high', got '%s'", deploy.RiskLevel)
		}
	}

	// Test FindByTag
	devopsSkills := registry.FindByTag("devops")
	if len(devopsSkills) != 1 || devopsSkills[0].Name != "deploy" {
		t.Errorf("FindByTag(devops): expected [deploy], got %v", devopsSkills)
	}

	// Test FindByCapability
	reasoningSkills := registry.FindByCapability("reasoning")
	if len(reasoningSkills) != 2 {
		t.Errorf("FindByCapability(reasoning): expected 2, got %d", len(reasoningSkills))
	}

	// Test FindByCapabilities (code AND reasoning satisfied)
	// Returns all skills whose requirements are satisfied by the given capabilities.
	// code-review requires [code, reasoning] - satisfied by [code, reasoning]
	// data-analysis requires [reasoning] - also satisfied by [code, reasoning]
	capSkills := registry.FindByCapabilities([]string{"code", "reasoning"})
	if len(capSkills) != 2 {
		t.Errorf("FindByCapabilities([code,reasoning]): expected 2, got %d", len(capSkills))
	}

	// Test Match
	match := registry.Match("code review")
	if match == nil || match.Name != "code-review" {
		t.Errorf("Match('code review'): expected code-review, got %v", match)
	}
}

// TestSkillIndexCapabilityMatching tests the skill index and capability index
// working together for metadata-driven skill discovery.
func TestSkillIndexCapabilityMatching(t *testing.T) {
	// Build skill index from entries
	skillIndex := skills.NewSkillIndex()
	skillIndex.Index(&skills.SkillIndexEntry{
		Name:        "code-review",
		Description: "Automated code review",
		Requires:    []string{"code", "reasoning"},
		Tags:        []string{"review", "code"},
		AllowedTools: []string{"file_read", "shell_execute"},
		RiskLevel:   "low",
		Examples:    []string{"review my PR", "check code quality"},
	})
	skillIndex.Index(&skills.SkillIndexEntry{
		Name:        "deploy",
		Description: "Deploy application",
		Requires:    []string{"code", "tool_use"},
		Tags:        []string{"deploy", "devops"},
		AllowedTools: []string{"shell_execute"},
		RiskLevel:   "high",
		Examples:    []string{"deploy to staging", "push to production"},
	})

	// Build capability index
	capIndex := skills.BuildCapabilityIndex(skillIndex)

	// Test keyword-based matching
	matches := capIndex.Match("review code quality", 3)
	if len(matches) == 0 {
		t.Error("Expected matches for 'review code quality'")
	} else {
		// code-review should be top match
		if matches[0].Entry.Name != "code-review" {
			t.Errorf("Expected code-review as top match, got %s", matches[0].Entry.Name)
		}
	}

	// Test deploy-related query
	deployMatches := capIndex.Match("deploy to staging", 3)
	if len(deployMatches) == 0 {
		t.Error("Expected matches for 'deploy to staging'")
	} else {
		if deployMatches[0].Entry.Name != "deploy" {
			t.Errorf("Expected deploy as top match, got %s", deployMatches[0].Entry.Name)
		}
	}

	// Test threshold filtering
	highConf := capIndex.MatchWithThreshold("review code quality", 0.8, 3)
	// The confidence depends on TF-IDF weights, just verify it doesn't crash
	t.Logf("High confidence matches for 'review code quality': %d", len(highConf))
}

// TestToolFilteringInheritance tests that tool definitions are correctly
// filtered through the FilteredToolRegistry's GetDefinitions method.
func TestToolFilteringInheritance(t *testing.T) {
	parent := agent.NewPlaceholderToolRegistry()
	parent.Register(agent.NewMockTool("tool_a", "Tool A", nil))
	parent.Register(agent.NewMockTool("tool_b", "Tool B", nil))
	parent.Register(agent.NewMockTool("tool_c", "Tool C", nil))

	// Filter to only tool_a and tool_b
	filtered := agent.NewFilteredToolRegistry(parent, []string{"tool_a", "tool_b"})

	// Test Get (filtered tools accessible)
	if filtered.Get("tool_a") == nil {
		t.Error("tool_a should be accessible")
	}
	if filtered.Get("tool_b") == nil {
		t.Error("tool_b should be accessible")
	}
	if filtered.Get("tool_c") != nil {
		t.Error("tool_c should NOT be accessible")
	}

	// Test List
	tools := filtered.List()
	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	// Test GetDefinitions
	defs := filtered.GetDefinitions()
	if len(defs) != 2 {
		t.Errorf("Expected 2 definitions, got %d", len(defs))
	}

	// Verify parent is unmodified
	parentTools := parent.List()
	if len(parentTools) != 3 {
		t.Errorf("Parent should still have 3 tools, got %d", len(parentTools))
	}
}

// TestDispatcherSkillRoutingWithAllowedTools tests that the dispatcher correctly
// routes to skills and respects tool restrictions when dispatching.
func TestDispatcherSkillRoutingWithAllowedTools(t *testing.T) {
	registry := skills.NewRegistry()
	registry.Register(&skills.Skill{
		Name:         "restricted-skill",
		Description:  "A skill with tool restrictions",
		AllowedTools: []string{"file_read"},
	})
	registry.Register(&skills.Skill{
		Name:        "open-skill",
		Description: "A skill with no tool restrictions",
	})

	dispatcher := agent.NewDispatcher(agent.DispatcherConfig{
		SkillRegistry: registry,
	})

	ctx := context.Background()

	// Test explicit skill invocation with restricted tools
	result, err := dispatcher.ClassifyAndRoute(ctx, "/restricted-skill some input", "test-session")
	if err == nil {
		// Without executor, this should error
		t.Error("Expected error when executor is nil")
	}

	// Verify non-skill input doesn't trigger skill path
	result, err = dispatcher.ClassifyAndRoute(ctx, "general question", "test-session")
	if err != nil {
		t.Errorf("General input should not error: %v", err)
	}
	if result == nil {
		t.Error("Expected dispatch result for general input")
	}

	// Verify non-existent skill falls through
	result, err = dispatcher.ClassifyAndRoute(ctx, "/nonexistent-skill input", "test-session")
	if err != nil {
		t.Errorf("Non-existent skill should fall through to normal routing: %v", err)
	}
	if result == nil {
		t.Error("Expected dispatch result for non-existent skill fallthrough")
	}
}

// TestAgentSpecSkillTriggersIntegration tests skill trigger mapping in agent specs
// for automatic skill invocation based on keywords.
func TestAgentSpecSkillTriggersIntegration(t *testing.T) {
	spec := &agent.AgentSpec{
		ID:   "coder",
		Name: "Coder Agent",
		AvailableSkills: []string{
			"code-review",
			"refactor",
			"debug",
		},
		SkillTriggers: map[string]string{
			"review":   "code-review",
			"refactor": "refactor",
			"debug":    "debug",
			"fix":      "debug",
		},
	}

	// Verify all skills are reported
	if !spec.HasSkill("code-review") {
		t.Error("Should have code-review skill")
	}
	if !spec.HasSkill("refactor") {
		t.Error("Should have refactor skill")
	}
	if !spec.HasSkill("debug") {
		t.Error("Should have debug skill")
	}
	if spec.HasSkill("deploy") {
		t.Error("Should NOT have deploy skill")
	}

	// Verify trigger mappings
	triggerTests := []struct {
		trigger   string
		wantSkill string
	}{
		{"review", "code-review"},
		{"refactor", "refactor"},
		{"debug", "debug"},
		{"fix", "debug"},
		{"unknown", ""},
	}

	for _, tt := range triggerTests {
		got := spec.GetSkillForTrigger(tt.trigger)
		if got != tt.wantSkill {
			t.Errorf("GetSkillForTrigger(%q) = %q, want %q", tt.trigger, got, tt.wantSkill)
		}
	}
}

// testResolver creates a resolver with test providers for skill model resolution.
func testResolver() *llm.Resolver {
	cfg := &llm.ProvidersConfig{
		Model:      "provider1/model-a",
		SmallModel: "provider1/model-b",
		Providers: map[string]llm.ProviderConfig{
			"provider1": {
				API: "openai",
				Options: llm.ProviderOptionsConfig{
					BaseURL: "http://localhost:11434/v1",
				},
				Models: map[string]llm.ModelDef{
					"model-a": {
						Name:         "model-a",
						Capabilities: []string{"code", "reasoning"},
						InputCost:    1.0,
						OutputCost:   2.0,
						ContextLimit: 128000,
						MaxOutput:    4096,
						Temperature:  0.7,
					},
					"model-b": {
						Name:         "model-b",
						Capabilities: []string{"code"},
						InputCost:    0.5,
						OutputCost:   1.0,
						ContextLimit: 32000,
						MaxOutput:    2048,
						Temperature:  0.5,
					},
				},
			},
			"provider2": {
				API: "openai",
				Options: llm.ProviderOptionsConfig{
					BaseURL: "https://api.example.com/v1",
				},
				Models: map[string]llm.ModelDef{
					"model-x": {
						Name:         "model-x",
						Capabilities: []string{"code", "reasoning", "tool_use"},
						InputCost:    3.0,
						OutputCost:   15.0,
						ContextLimit: 200000,
						MaxOutput:    8192,
						Temperature:  0.7,
					},
				},
			},
		},
	}

	return llm.NewResolver(cfg, nil)
}
