// Package integration — employee_migration_test.go exercises the legacy
// bot → employee migration path. Spec line 643 requires: "drop a legacy
// bot JSON on disk, run migrate, assert valid constitution produced and
// employee starts."
//
// Manager.Migrate scans the configured bots directory, reads each
// *.json file, and synthesizes a conservative constitution per spec line
// 627 (tier_1_reactive, risk_ceiling: low, escalates_to: ["user"],
// never: [...]). Manager.ApplyMigration persists the proposed
// constitution and updates/creates the underlying bot definition.
package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/pkg/id"
)

// legacyBotJSON is the JSON shape of a pre-employee-era bot
// definition (matching internal/bot/types.go BotDefinition). The
// fixture intentionally uses a vague prompt to exercise the "conservative
// fallback constitution" path described in spec line 627.
const legacyBotJSON = `{
  "id": "legacy-greeter",
  "name": "Greeter Bot",
  "description": "Says hello when triggered",
  "prompt": "Be polite and helpful.",
  "model": "stub-model",
  "triggers": [{"type": "webhook", "enabled": true}],
  "tools": ["file_read"],
  "memory_scope": "private",
  "enabled": true
}`

// writeLegacyBot writes a single legacy bot JSON file to a bots/
// subdirectory of the test temp dir, mimicking the layout of
// ~/.meept/bots/*.json that Manager.Migrate scans. Returns the bots
// directory path (so the test can pass it to SetBotsDir).
func writeLegacyBot(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "greeter.json")
	//nolint:gosec // test fixture
	if err := os.WriteFile(path, []byte(legacyBotJSON), 0o644); err != nil {
		t.Fatalf("write legacy bot: %v", err)
	}
	return dir
}

// readLegacyBot decodes the legacy bot JSON on disk. Used by the
// manual-path subtest to verify the fixture round-trips cleanly.
func readLegacyBot(t *testing.T, botsDir string) bot.BotDefinition {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(botsDir, "greeter.json"))
	if err != nil {
		t.Fatalf("read legacy bot: %v", err)
	}
	var def bot.BotDefinition
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("unmarshal legacy bot: %v", err)
	}
	return def
}

// conservativeConstitutionFor is the minimal conservative constitution
// the spec (line 627) mandates for legacy bots whose prompts are too
// vague to synthesize richer constraints: tier_1_reactive,
// risk_ceiling: low, escalates_to: ["user"], never contains a sensible
// default. Used by the manual-path subtest.
func conservativeConstitutionFor(def bot.BotDefinition) map[string]any {
	return map[string]any{
		"purpose":       "migrated from legacy bot: " + def.Name,
		"role":          "migrated legacy bot",
		"charter":       def.Prompt,
		"autonomy_tier": "tier_1_reactive",
		"escalates_to":  []string{"user"},
		"amendment_policy": map[string]any{
			"requires_approval":    true,
			"self_propose_allowed": false,
		},
		"constraints": map[string]any{
			"risk_ceiling": "low",
			"never":        []string{"execute financial transactions"},
		},
	}
}

// TestEmployee_Migration covers the legacy bot → employee migration
// scenarios described in spec lines 627 + 643. It exercises both the
// Migrate dry-run scan and the ApplyMigration write path, plus a
// manually-equivalent path through Manager.Hire to verify the
// conservative constitution satisfies Validate.
func TestEmployee_Migration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping employee migration integration test in short mode")
	}

	env := newEmployeeLifecycleEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// --- point Manager.Migrate at a temp bots directory ---
	botsDir := writeLegacyBot(t)
	env.empMgr.SetBotsDir(botsDir)

	// --- Migrate dry-run scan returns proposals ---
	t.Run("manager.Migrate returns proposals for legacy bots", func(t *testing.T) {
		proposals, err := env.empMgr.Migrate(ctx)
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if len(proposals) == 0 {
			t.Fatal("expected at least one proposal, got empty slice")
		}
		prop := proposals[0]
		if prop.BotID != "legacy-greeter" {
			t.Errorf("bot_id = %q, want 'legacy-greeter'", prop.BotID)
		}
		if prop.BotName != "Greeter Bot" {
			t.Errorf("bot_name = %q, want 'Greeter Bot'", prop.BotName)
		}
		// Conservative defaults (spec line 627).
		if err := prop.Proposed.Validate(prop.BotID); err != nil {
			t.Errorf("proposed constitution failed Validate: %v", err)
		}
		if prop.Proposed.AutonomyTier != employee.Tier1Reactive {
			t.Errorf("autonomy tier = %v, want Tier1Reactive", prop.Proposed.AutonomyTier)
		}
		if prop.Proposed.Constraints.RiskCeiling != employee.RiskCeilingLow {
			t.Errorf("risk_ceiling = %q, want %q",
				prop.Proposed.Constraints.RiskCeiling, employee.RiskCeilingLow)
		}
		if !prop.NeedsReview {
			t.Error("NeedsReview should be true for conservative proposals")
		}
		if prop.Confidence <= 0 || prop.Confidence >= 1.0 {
			t.Errorf("Confidence = %f, want (0, 1)", prop.Confidence)
		}
	})

	// --- ApplyMigration writes constitution and persists employee ---
	t.Run("manager.ApplyMigration writes constitution and persists bot", func(t *testing.T) {
		result, err := env.empMgr.ApplyMigration(ctx, "legacy-greeter")
		if err != nil {
			t.Fatalf("ApplyMigration: %v", err)
		}
		if !result.Applied {
			t.Error("result.Applied = false, want true")
		}

		// Constitution must now be persisted.
		persisted, err := env.constStore.Get(ctx, "legacy-greeter")
		if err != nil {
			t.Fatalf("constitution Get after ApplyMigration: %v", err)
		}
		if err := persisted.Validate("legacy-greeter"); err != nil {
			t.Errorf("persisted constitution failed Validate: %v", err)
		}
		if persisted.AutonomyTier.String() != "tier_1_reactive" {
			t.Errorf("autonomy tier = %q, want tier_1_reactive",
				persisted.AutonomyTier.String())
		}
		if persisted.Constraints.RiskCeiling != employee.RiskCeilingLow {
			t.Errorf("risk_ceiling = %q, want %q",
				persisted.Constraints.RiskCeiling, employee.RiskCeilingLow)
		}
		if persisted.AuthoredBy != "migrate" {
			t.Errorf("authored_by = %q, want 'migrate'", persisted.AuthoredBy)
		}
		// The bot definition should exist in the store now.
		def, err := env.botMgr.GetBot(ctx, "legacy-greeter")
		if err != nil {
			t.Fatalf("GetBot after ApplyMigration: %v", err)
		}
		if def.Name != "Greeter Bot" {
			t.Errorf("bot Name = %q, want 'Greeter Bot'", def.Name)
		}
	})

	// --- Migrate on empty dir returns empty slice ---
	t.Run("manager.Migrate on empty dir returns empty slice", func(t *testing.T) {
		emptyDir := filepath.Join(t.TempDir(), "empty-bots")
		if err := os.MkdirAll(emptyDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		env.empMgr.SetBotsDir(emptyDir)
		proposals, err := env.empMgr.Migrate(ctx)
		if err != nil {
			t.Fatalf("Migrate on empty dir: %v", err)
		}
		if len(proposals) != 0 {
			t.Errorf("expected 0 proposals, got %d", len(proposals))
		}
	})

	// --- Migrate on nonexistent dir returns empty slice ---
	t.Run("manager.Migrate on nonexistent dir returns empty slice", func(t *testing.T) {
		env.empMgr.SetBotsDir(filepath.Join(t.TempDir(), "does-not-exist"))
		proposals, err := env.empMgr.Migrate(ctx)
		if err != nil {
			t.Fatalf("Migrate on nonexistent dir: %v", err)
		}
		if len(proposals) != 0 {
			t.Errorf("expected 0 proposals, got %d", len(proposals))
		}
	})

	// --- legacy bot on disk can be read + decoded ---
	// Re-point at the populated bots dir for the manual-path subtest.
	env.empMgr.SetBotsDir(botsDir)

	// --- manually-equivalent migration: read JSON, synthesize,
	// validate, Hire, verify employee starts ---
	// This subtest uses the original bots dir to drive the manual path
	// (read JSON → synthesize → Hire) to verify the spec's conservative
	// constitution is valid through the Hire entry point.
	t.Run("manual migration path produces valid constitution", func(t *testing.T) {
		legacyDef := readLegacyBot(t, botsDir)
		if legacyDef.ID != "legacy-greeter" {
			t.Fatalf("legacy bot ID = %q, want 'legacy-greeter'", legacyDef.ID)
		}

		// Synthesize the conservative constitution the spec mandates
		// (line 627) for vague prompts.
		constitutionMap := conservativeConstitutionFor(legacyDef)

		// Hire the migrated employee. ID collision with the legacy bot
		// would be a problem in production; in the test we mint a fresh
		// employee ID so the migration target is unambiguous.
		migratedID := id.Generate("migrated_")
		emp, err := env.empMgr.Hire(ctx, employee.HireRequest{
			ID:           migratedID,
			Name:         legacyDef.Name,
			Description:  legacyDef.Description,
			Prompt:       legacyDef.Prompt,
			Model:        legacyDef.Model,
			Triggers:     legacyDef.Triggers,
			Tools:        legacyDef.Tools,
			MemoryScope:  legacyDef.MemoryScope,
			Enabled:      true,
			Constitution: constitutionMap,
		})
		if err != nil {
			t.Fatalf("Hire migrated employee: %v", err)
		}
		if !emp.HasConstitution() {
			t.Fatal("migrated employee should have a constitution")
		}

		// The synthesized constitution must satisfy Constitution.Validate
		// (this is the core assertion from spec line 643: "valid
		// constitution produced"). Re-fetch from the store to verify
		// the persisted shape round-trips cleanly.
		persisted, err := env.constStore.Get(ctx, migratedID)
		if err != nil {
			t.Fatalf("constitution Get after Hire: %v", err)
		}
		if err := persisted.Validate(migratedID); err != nil {
			t.Errorf("persisted constitution failed Validate: %v", err)
		}
		if persisted.AutonomyTier.String() != "tier_1_reactive" {
			t.Errorf("autonomy tier = %q, want tier_1_reactive",
				persisted.AutonomyTier.String())
		}
		if persisted.Constraints.RiskCeiling != employee.RiskCeilingLow {
			t.Errorf("risk_ceiling = %q, want %q",
				persisted.Constraints.RiskCeiling, employee.RiskCeilingLow)
		}

		// Spec: "and employee starts" — verify StartAll primes the
		// cache and the employee is visible via ListEmployees.
		if err := env.empMgr.StartAll(ctx); err != nil {
			t.Fatalf("StartAll after migration: %v", err)
		}
		listed, err := env.empMgr.ListEmployees(ctx, "")
		if err != nil {
			t.Fatalf("ListEmployees: %v", err)
		}
		found := false
		for _, e := range listed {
			if e.ID == migratedID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("migrated employee %q not in ListEmployees result (n=%d)",
				migratedID, len(listed))
		}
	})
}
