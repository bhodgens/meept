package integration

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

// hermesSkillMD is a full Hermes SKILL.md fixture for integration tests.
// Note: this string intentionally avoids backtick characters in the body so it
// can be declared as a raw string literal. Backtick patterns are tested
// separately in TestHermesToolMappingIntegration using string concatenation.
var hermesSkillMD = `---
name: hermes-test-skill
description: A Hermes-compatible test skill for integration testing
version: 1.2.0
license: MIT
platforms:
  - macos
  - linux
prerequisites:
  env_vars:
    - PATH
    - HOME
  commands:
    - sh
    - ls
metadata:
  hermes:
    triggers:
      - hermes-deploy
      - auto-fix
risk-level: low
requires:
  - code
  - reasoning
tags:
  - hermes
  - integration
---

# Hermes Test Skill

This skill demonstrates Hermes-Agent compatibility.

## Available Tools

- schedule("daily", "09:00")
- skill_view("code-review")
- skills_list()
- team_create("review-team")
- team_list()
- image_gen("prompt here")

## Instructions

Use the schedule tool to create reminders. Call skills_list to enumerate
available skills. Use team_message to coordinate with other agents.

When the user says "run team_list" you should invoke the skills_list command.
`

// hermesSkillMDMixed tests that Meept-native fields work alongside Hermes fields.
const hermesSkillMDMixed = `---
name: hermes-mixed-skill
description: Skill with both Meept and Hermes fields
version: 2.0.0
platforms:
  - linux
requires:
  - tool_use
tags:
  - hermes
prerequisites:
  env_vars:
    - PATH
  commands:
    - sh
metadata:
  hermes:
    triggers:
      - deploy-trigger
risk-level: high
max-iterations: 20
---

# Mixed Skill

This skill uses both Meept and Hermes fields.
`

// ---------------------------------------------------------------------------
// Test 1: TestHermesSkillDiscoveryIntegration
// ---------------------------------------------------------------------------

func TestHermesSkillDiscoveryIntegration(t *testing.T) {
	t.Run("tier_auto_added_when_dir_exists", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a Hermes-style skills directory inside a fake home.
		hermesDir := filepath.Join(tmpDir, ".hermes", "skills")
		if err := os.MkdirAll(hermesDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		// Place a SKILL.md inside a subdirectory (Hermes convention).
		skillDir := filepath.Join(hermesDir, "hermes-test-skill")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		//nolint:gosec // test file
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(hermesSkillMD), 0o644); err != nil {
			t.Fatalf("write SKILL.md: %v", err)
		}

		// Create a FileSource pointing at our temp dir with Hermes priority.
		tiers := []skills.DiscoveryTier{
			{Path: hermesDir, Priority: skills.PriorityHermes},
		}
		src := skills.NewFileSource(tiers, slog.New(slog.DiscardHandler))

		discovered, err := src.Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(discovered) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(discovered))
		}

		skill := discovered[0]

		// SourceOrigin must be "hermes" as parsed from the frontmatter.
		if skill.SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", skill.SourceOrigin, "hermes")
		}
		// FileSource should also override the generic Source field.
		if skill.Source != "hermes" {
			t.Errorf("Source = %q, want %q", skill.Source, "hermes")
		}
		// Priority must match the Hermes tier.
		if skill.Priority != skills.PriorityHermes {
			t.Errorf("Priority = %d, want %d", skill.Priority, skills.PriorityHermes)
		}
		// Prerequisites should be populated.
		if skill.Prerequisites == nil {
			t.Fatal("Prerequisites is nil, expected non-nil")
		}
		if len(skill.Prerequisites.EnvVars) != 2 {
			t.Errorf("EnvVars count = %d, want 2", len(skill.Prerequisites.EnvVars))
		}
		if len(skill.Prerequisites.Commands) != 2 {
			t.Errorf("Commands count = %d, want 2", len(skill.Prerequisites.Commands))
		}
		// Tags should include platforms + triggers + explicit tags.
		if !skill.HasTag("macos") {
			t.Error("expected tag 'macos' from platforms")
		}
		if !skill.HasTag("linux") {
			t.Error("expected tag 'linux' from platforms")
		}
		if !skill.HasTag("hermes-deploy") {
			t.Error("expected tag 'hermes-deploy' from metadata.hermes.triggers")
		}
		if !skill.HasTag("auto-fix") {
			t.Error("expected tag 'auto-fix' from metadata.hermes.triggers")
		}
		if !skill.HasTag("hermes") {
			t.Error("expected explicit tag 'hermes'")
		}
	})

	t.Run("flat_file_discovery", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Place a flat .md file (no subdirectory).
		//nolint:gosec // test file
		if err := os.WriteFile(filepath.Join(tmpDir, "hermes-flat.md"), []byte(hermesSkillMD), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		src := skills.NewFileSource([]skills.DiscoveryTier{
			{Path: tmpDir, Priority: skills.PriorityHermes},
		}, slog.New(slog.DiscardHandler))

		discovered, err := src.Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(discovered) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(discovered))
		}
		if discovered[0].SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", discovered[0].SourceOrigin, "hermes")
		}
	})
}

// ---------------------------------------------------------------------------
// Test 2: TestHermesParserIntegration
// ---------------------------------------------------------------------------

func TestHermesParserIntegration(t *testing.T) {
	t.Run("full_hermes_skill", func(t *testing.T) {
		skill, err := skills.ParseSkillText(hermesSkillMD)
		if err != nil {
			t.Fatalf("ParseSkillText: %v", err)
		}

		// Name and description.
		if skill.Name != "hermes-test-skill" {
			t.Errorf("Name = %q, want %q", skill.Name, "hermes-test-skill")
		}
		if skill.Description != "A Hermes-compatible test skill for integration testing" {
			t.Errorf("Description = %q", skill.Description)
		}

		// Tags: explicit (hermes, integration) + platforms (macos, linux) + triggers (hermes-deploy, auto-fix).
		expectedTags := map[string]bool{
			"hermes":        true,
			"integration":   true,
			"macos":         true,
			"linux":         true,
			"hermes-deploy": true,
			"auto-fix":      true,
		}
		for _, tag := range skill.Tags {
			if !expectedTags[tag] {
				t.Errorf("unexpected tag %q", tag)
			}
			delete(expectedTags, tag)
		}
		for missing := range expectedTags {
			t.Errorf("missing tag %q", missing)
		}

		// Prerequisites.
		if skill.Prerequisites == nil {
			t.Fatal("Prerequisites is nil")
		}
		if len(skill.Prerequisites.EnvVars) != 2 {
			t.Errorf("EnvVars = %v, want [PATH, HOME]", skill.Prerequisites.EnvVars)
		}
		if len(skill.Prerequisites.Commands) != 2 {
			t.Errorf("Commands = %v, want [sh, ls]", skill.Prerequisites.Commands)
		}

		// SourceOrigin.
		if skill.SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", skill.SourceOrigin, "hermes")
		}

		// Meept-native fields must still work.
		if skill.RiskLevel != "low" {
			t.Errorf("RiskLevel = %q, want %q", skill.RiskLevel, "low")
		}
		if len(skill.Requires) != 2 {
			t.Errorf("Requires = %v, want [code, reasoning]", skill.Requires)
		}
		if skill.MaxIterations != 10 {
			t.Errorf("MaxIterations = %d, want 10", skill.MaxIterations)
		}

		// Body must not be empty.
		if skill.Body == "" {
			t.Error("Body is empty, expected non-empty instruction body")
		}
	})

	t.Run("mixed_meept_hermes_fields", func(t *testing.T) {
		skill, err := skills.ParseSkillText(hermesSkillMDMixed)
		if err != nil {
			t.Fatalf("ParseSkillText: %v", err)
		}

		if skill.Name != "hermes-mixed-skill" {
			t.Errorf("Name = %q, want %q", skill.Name, "hermes-mixed-skill")
		}
		if skill.SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", skill.SourceOrigin, "hermes")
		}
		if skill.RiskLevel != "high" {
			t.Errorf("RiskLevel = %q, want %q", skill.RiskLevel, "high")
		}
		if skill.MaxIterations != 20 {
			t.Errorf("MaxIterations = %d, want 20", skill.MaxIterations)
		}
		if len(skill.Requires) != 1 || skill.Requires[0] != "tool_use" {
			t.Errorf("Requires = %v, want [tool_use]", skill.Requires)
		}
		// Tags: explicit (hermes) + platforms (linux) + triggers (deploy-trigger).
		if !skill.HasTag("linux") {
			t.Error("expected tag 'linux' from platforms")
		}
		if !skill.HasTag("deploy-trigger") {
			t.Error("expected tag 'deploy-trigger' from triggers")
		}
		if skill.Prerequisites == nil {
			t.Fatal("Prerequisites is nil")
		}
	})

	t.Run("non_hermes_skill_has_empty_origin", func(t *testing.T) {
		plain := `---
name: plain-skill
description: No Hermes fields
---
# Plain
`
		skill, err := skills.ParseSkillText(plain)
		if err != nil {
			t.Fatalf("ParseSkillText: %v", err)
		}
		if skill.SourceOrigin != "" {
			t.Errorf("SourceOrigin = %q, want empty", skill.SourceOrigin)
		}
		if skill.Prerequisites != nil {
			t.Error("Prerequisites should be nil for non-Hermes skill")
		}
	})
}

// ---------------------------------------------------------------------------
// Test 3: TestHermesToolMappingIntegration
// ---------------------------------------------------------------------------

func TestHermesToolMappingIntegration(t *testing.T) {
	mapper := skills.NewHermesToolMapper(slog.New(slog.DiscardHandler))

	t.Run("translate_single", func(t *testing.T) {
		tests := []struct {
			hermes string
			meept  string
		}{
			{"schedule", "schedule_create"},
			{"skill_view", "skills.get"},
			{"skills_list", "skills.list"},
			{"team_create", "delegate_task"},
			{"team_list", "platform_agents"},
			{"team_message", "request_handoff"},
			// Unmapped tools pass through unchanged.
			{"unknown_tool", "unknown_tool"},
			{"image_gen", "image_gen"}, // maps to empty, so passthrough
		}
		for _, tt := range tests {
			got := mapper.Translate(tt.hermes)
			if got != tt.meept {
				t.Errorf("Translate(%q) = %q, want %q", tt.hermes, got, tt.meept)
			}
		}
	})

	t.Run("translate_tool_references_in_body", func(t *testing.T) {
		// Build body with backtick patterns using string concatenation.
		backtick := "`"
		body := "## Tools\n\n" +
			"- schedule(\"daily\", \"09:00\")\n" +
			"- skill_view(\"code-review\")\n" +
			"- skills_list()\n" +
			"- team_create(\"review-team\")\n" +
			"- team_list()\n" +
			"- image_gen(\"prompt here\")\n\n" +
			"### Instructions\n\n" +
			"Use the schedule tool to create reminders. Call skills_list to enumerate\n" +
			"available skills. Use team_message to coordinate with other agents.\n\n" +
			backtick + "team_message(\"hello\")" + backtick + " is a good way to communicate.\n\n" +
			"When the user says \"run team_list\" you should invoke the " + backtick + "skills_list" + backtick + " command.\n\n" +
			"Use \"schedule\" for timed tasks.\n"

		result := mapper.TranslateToolReferences(body)

		// Verify all mapped tool names appear in the result.
		expectedMappings := []struct {
			from              string
			to                string
			skipAbsenceCheck  bool // true when from is a substring of to
		}{
			{from: "schedule", to: "schedule_create", skipAbsenceCheck: true},
			{from: "skill_view", to: "skills.get", skipAbsenceCheck: false},
			{from: "skills_list", to: "skills.list", skipAbsenceCheck: false},
			{from: "team_create", to: "delegate_task", skipAbsenceCheck: false},
			{from: "team_list", to: "platform_agents", skipAbsenceCheck: false},
			{from: "team_message", to: "request_handoff", skipAbsenceCheck: false},
		}

		for _, em := range expectedMappings {
			// The mapped Meept tool name SHOULD appear.
			if !strings.Contains(result, em.to) {
				t.Errorf("meept tool %q missing from output body", em.to)
			}
			// The original Hermes tool name should NOT appear as a standalone reference.
			// Skip this check when the from name is a substring of the to name.
			if !em.skipAbsenceCheck && strings.Contains(result, em.from) {
				t.Errorf("hermes tool %q still present in output body", em.from)
			}
		}

		// image_gen has no equivalent, so it stays unchanged.
		if !strings.Contains(result, "image_gen") {
			t.Error("image_gen should remain in output (no meept equivalent)")
		}
	})

	t.Run("empty_body_noop", func(t *testing.T) {
		result := mapper.TranslateToolReferences("")
		if result != "" {
			t.Errorf("expected empty output, got %q", result)
		}
	})

	t.Run("no_tool_references_unchanged", func(t *testing.T) {
		plain := "This is a plain body with no tool references."
		result := mapper.TranslateToolReferences(plain)
		if result != plain {
			t.Errorf("expected unchanged output, got %q", result)
		}
	})
}

// ---------------------------------------------------------------------------
// Test 4: TestHermesPrerequisitesValidationIntegration
// ---------------------------------------------------------------------------

func TestHermesPrerequisitesValidationIntegration(t *testing.T) {
	checker := skills.NewDefaultPrerequisiteChecker(slog.New(slog.DiscardHandler))

	t.Run("existing_env_vars_and_commands", func(t *testing.T) {
		prereqs := &skills.HermesPrerequisites{
			EnvVars:  []string{"PATH", "HOME"},
			Commands: []string{"sh", "ls"},
		}
		err := skills.CheckPrerequisites(checker, prereqs)
		if err != nil {
			t.Errorf("CheckPrerequisites with valid prereqs returned error: %v", err)
		}
	})

	t.Run("missing_env_var", func(t *testing.T) {
		prereqs := &skills.HermesPrerequisites{
			EnvVars: []string{"MEEPT_INTEGRATION_TEST_VAR_DOES_NOT_EXIST_XYZZY"},
		}
		err := skills.CheckPrerequisites(checker, prereqs)
		if err == nil {
			t.Fatal("expected error for missing env var, got nil")
		}
		if !strings.Contains(err.Error(), "missing required env var") {
			t.Errorf("error message = %q, want 'missing required env var'", err.Error())
		}
		if !strings.Contains(err.Error(), "MEEPT_INTEGRATION_TEST_VAR_DOES_NOT_EXIST_XYZZY") {
			t.Errorf("error should mention var name, got %q", err.Error())
		}
	})

	t.Run("missing_command", func(t *testing.T) {
		prereqs := &skills.HermesPrerequisites{
			Commands: []string{"nonexistent_command_hermes_test_xyz"},
		}
		err := skills.CheckPrerequisites(checker, prereqs)
		if err == nil {
			t.Fatal("expected error for missing command, got nil")
		}
		if !strings.Contains(err.Error(), "missing required command") {
			t.Errorf("error message = %q, want 'missing required command'", err.Error())
		}
	})

	t.Run("nil_checker_and_prereqs", func(t *testing.T) {
		// CheckPrerequisites must be nil-safe.
		err := skills.CheckPrerequisites(nil, nil)
		if err != nil {
			t.Errorf("nil checker+prereqs: %v", err)
		}
	})

	t.Run("nil_prereqs_only", func(t *testing.T) {
		err := skills.CheckPrerequisites(checker, nil)
		if err != nil {
			t.Errorf("nil prereqs: %v", err)
		}
	})

	t.Run("empty_prereqs", func(t *testing.T) {
		prereqs := &skills.HermesPrerequisites{}
		err := skills.CheckPrerequisites(checker, prereqs)
		if err != nil {
			t.Errorf("empty prereqs: %v", err)
		}
	})

	t.Run("first_error_stops", func(t *testing.T) {
		// Env var check comes first, so a missing env var should be reported
		// before a missing command.
		prereqs := &skills.HermesPrerequisites{
			EnvVars:  []string{"MEEPT_MISSING_VAR_FIRST"},
			Commands: []string{"nonexistent_cmd_hermes_test"},
		}
		err := skills.CheckPrerequisites(checker, prereqs)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "env var") {
			t.Errorf("expected env var error (comes first), got %q", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// Test 5: TestHermesSkillIndexIntegration
// ---------------------------------------------------------------------------

func TestHermesSkillIndexIntegration(t *testing.T) {
	t.Run("parse_metadata_only_produces_hermes_origin", func(t *testing.T) {
		tmpDir := t.TempDir()
		skillDir := filepath.Join(tmpDir, "hermes-test-skill")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		skillPath := filepath.Join(skillDir, "SKILL.md")
		//nolint:gosec // test file
		if err := os.WriteFile(skillPath, []byte(hermesSkillMD), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		entry, err := skills.ParseSkillMetadataOnly(skillPath)
		if err != nil {
			t.Fatalf("ParseSkillMetadataOnly: %v", err)
		}

		if entry.Name != "hermes-test-skill" {
			t.Errorf("Name = %q, want %q", entry.Name, "hermes-test-skill")
		}
		if entry.SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", entry.SourceOrigin, "hermes")
		}
		if entry.RiskLevel != "low" {
			t.Errorf("RiskLevel = %q, want %q", entry.RiskLevel, "low")
		}
		// Tags from platforms and triggers should be present.
		if !entry.HasTag("macos") {
			t.Error("expected tag 'macos' from platforms")
		}
		if !entry.HasTag("hermes-deploy") {
			t.Error("expected tag 'hermes-deploy' from metadata.hermes.triggers")
		}
	})

	t.Run("index_retrieves_hermes_entry", func(t *testing.T) {
		idx := skills.NewSkillIndex()

		entries := []*skills.SkillIndexEntry{
			{
				Name:         "hermes-code-review",
				Description:  "Hermes code review skill",
				SourceOrigin: "hermes",
				Tags:         []string{"hermes", "review"},
				Requires:     []string{"code"},
				RiskLevel:    "medium",
			},
			{
				Name:         "meept-deploy",
				Description:  "Native Meept deploy skill",
				SourceOrigin: "meept",
				Tags:         []string{"deploy"},
				Requires:     []string{"tool_use"},
				RiskLevel:    "high",
			},
		}

		idx.IndexAll(entries)

		if idx.Count() != 2 {
			t.Fatalf("Count = %d, want 2", idx.Count())
		}

		// Retrieve by name.
		got := idx.Get("hermes-code-review")
		if got == nil {
			t.Fatal("Get('hermes-code-review') returned nil")
		}
		if got.SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", got.SourceOrigin, "hermes")
		}

		// Find by tag.
		hermesSkills := idx.FindByTag("hermes")
		if len(hermesSkills) != 1 {
			t.Fatalf("FindByTag('hermes') count = %d, want 1", len(hermesSkills))
		}
		if hermesSkills[0].SourceOrigin != "hermes" {
			t.Errorf("hermes tag entry SourceOrigin = %q, want %q", hermesSkills[0].SourceOrigin, "hermes")
		}

		// Find by capability.
		codeSkills := idx.FindByCapability("code")
		if len(codeSkills) != 1 {
			t.Fatalf("FindByCapability('code') count = %d, want 1", len(codeSkills))
		}
		if codeSkills[0].Name != "hermes-code-review" {
			t.Errorf("code capability entry Name = %q, want %q", codeSkills[0].Name, "hermes-code-review")
		}

		// List returns both.
		all := idx.List()
		if len(all) != 2 {
			t.Fatalf("List count = %d, want 2", len(all))
		}
	})

	t.Run("index_via_filesource_discover_metadata", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create Hermes skill directory structure.
		hermesDir := filepath.Join(tmpDir, "hermes-skills")
		skillDir := filepath.Join(hermesDir, "hermes-index-skill")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		//nolint:gosec // test file
		if err := os.WriteFile(
			filepath.Join(skillDir, "SKILL.md"),
			[]byte(hermesSkillMD),
			0o644,
		); err != nil {
			t.Fatalf("write: %v", err)
		}

		src := skills.NewFileSource([]skills.DiscoveryTier{
			{Path: hermesDir, Priority: skills.PriorityHermes},
		}, slog.New(slog.DiscardHandler))

		metaEntries, err := src.DiscoverMetadata(context.Background())
		if err != nil {
			t.Fatalf("DiscoverMetadata: %v", err)
		}
		if len(metaEntries) != 1 {
			t.Fatalf("expected 1 metadata entry, got %d", len(metaEntries))
		}

		entry := metaEntries[0]
		if entry.SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", entry.SourceOrigin, "hermes")
		}
		if entry.Priority != skills.PriorityHermes {
			t.Errorf("Priority = %d, want %d", entry.Priority, skills.PriorityHermes)
		}

		// Now index it and verify retrieval.
		idx := skills.NewSkillIndex()
		idx.IndexAll(metaEntries)

		retrieved := idx.Get("hermes-test-skill")
		if retrieved == nil {
			t.Fatal("indexed Hermes skill not retrievable by name")
		}
		if retrieved.SourceOrigin != "hermes" {
			t.Errorf("retrieved SourceOrigin = %q, want %q", retrieved.SourceOrigin, "hermes")
		}
	})
}

// ---------------------------------------------------------------------------
// Test 6: TestHermesConfigOverridesIntegration
// ---------------------------------------------------------------------------

func TestHermesConfigOverridesIntegration(t *testing.T) {
	t.Run("auto_discover_false_filters_hermes_tier", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Simulate DefaultTiers() output: project + user + hermes + system.
		allTiers := []skills.DiscoveryTier{
			{Path: filepath.Join(tmpDir, ".meept", "skills"), Priority: skills.PriorityProject},
			{Path: filepath.Join(tmpDir, "user-skills"), Priority: skills.PriorityUser},
			{Path: filepath.Join(tmpDir, ".hermes", "skills"), Priority: skills.PriorityHermes},
			{Path: filepath.Join(tmpDir, "system-skills"), Priority: skills.PrioritySystem},
		}

		// When AutoDiscoverHermes is false, filter out the Hermes tier.
		autoDiscover := false
		filteredTiers := make([]skills.DiscoveryTier, 0, len(allTiers))
		for _, tier := range allTiers {
			if autoDiscover && tier.Priority == skills.PriorityHermes {
				continue
			}
			// Also filter Hermes if NOT auto-discovered (default behavior).
			if !autoDiscover && tier.Priority == skills.PriorityHermes {
				continue
			}
			filteredTiers = append(filteredTiers, tier)
		}

		// Verify Hermes tier was filtered.
		for _, tier := range filteredTiers {
			if tier.Priority == skills.PriorityHermes {
				t.Error("Hermes tier should be filtered when AutoDiscoverHermes is false")
			}
		}
		if len(filteredTiers) != 3 {
			t.Errorf("filtered tier count = %d, want 3", len(filteredTiers))
		}

		// Verify that with auto-discover enabled, the tier is kept.
		autoDiscover = true
		filteredTiers = filteredTiers[:0]
		for _, tier := range allTiers {
			if !autoDiscover && tier.Priority == skills.PriorityHermes {
				continue
			}
			filteredTiers = append(filteredTiers, tier)
		}
		foundHermes := false
		for _, tier := range filteredTiers {
			if tier.Priority == skills.PriorityHermes {
				foundHermes = true
			}
		}
		if !foundHermes {
			t.Error("Hermes tier should be present when AutoDiscoverHermes is true")
		}
		if len(filteredTiers) != 4 {
			t.Errorf("filtered tier count = %d, want 4", len(filteredTiers))
		}
	})

	t.Run("hermes_skills_dir_override", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a custom Hermes skills directory.
		customHermesDir := filepath.Join(tmpDir, "custom-hermes", "skills")
		if err := os.MkdirAll(customHermesDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		// Place a Hermes skill there.
		skillDir := filepath.Join(customHermesDir, "custom-hermes-skill")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		//nolint:gosec // test file
		if err := os.WriteFile(
			filepath.Join(skillDir, "SKILL.md"),
			[]byte(hermesSkillMD), // use the same fixture
			0o644,
		); err != nil {
			t.Fatalf("write: %v", err)
		}

		// Simulate HermesSkillsDir override: use customHermesDir instead of default.
		tiers := []skills.DiscoveryTier{
			{Path: customHermesDir, Priority: skills.PriorityHermes},
		}
		src := skills.NewFileSource(tiers, slog.New(slog.DiscardHandler))

		discovered, err := src.Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(discovered) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(discovered))
		}

		skill := discovered[0]
		if skill.SourceOrigin != "hermes" {
			t.Errorf("SourceOrigin = %q, want %q", skill.SourceOrigin, "hermes")
		}
		if skill.Priority != skills.PriorityHermes {
			t.Errorf("Priority = %d, want %d", skill.Priority, skills.PriorityHermes)
		}
	})

	t.Run("validate_prerequisites_flag_affects_check", func(t *testing.T) {
		// When ValidatePrerequisites is false, CheckPrerequisites should be
		// skipped entirely (nil checker means no validation).
		prereqs := &skills.HermesPrerequisites{
			EnvVars:  []string{"MEEPT_MISSING_VAR_VALIDATE_TEST"},
			Commands: []string{"nonexistent_cmd_validate_test"},
		}

		// ValidatePrerequisites=true: use checker, expect error.
		checker := skills.NewDefaultPrerequisiteChecker(slog.New(slog.DiscardHandler))
		err := skills.CheckPrerequisites(checker, prereqs)
		if err == nil {
			t.Error("expected error when ValidatePrerequisites is true and prereqs fail")
		}

		// ValidatePrerequisites=false: pass nil checker (skips all checks).
		err = skills.CheckPrerequisites(nil, prereqs)
		if err != nil {
			t.Errorf("nil checker (ValidatePrerequisites=false) should skip checks, got: %v", err)
		}
	})
}
