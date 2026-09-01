package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

// writeTierFixtureSkill creates <tierDir>/<name>/SKILL.md with valid
// frontmatter and returns the content written.
func writeTierFixtureSkill(t *testing.T, tierDir, name string) string {
	t.Helper()
	dir := filepath.Join(tierDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fixture skill dir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: fixture skill in " + filepath.Base(tierDir) + "\n---\n\nbody of " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture SKILL.md: %v", err)
	}
	return content
}

// TestWriterTier_ArchiveFromClaudeTier verifies ArchiveSkill archives a skill
// living in a NON-default tier (fixture ~/.claude/skills), landing the archive
// adjacent to that tier, and never creating or consulting the fixed
// ~/.meept/skills root. Uses the Writer's default resolver (env-driven
// discovery) — the same path the daemon's DataDir-rooted construction takes.
func TestWriterTier_ArchiveFromClaudeTier(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("HOME", fixtureHome)

	dataDir := t.TempDir()
	skillsDir := filepath.Join(dataDir, "skills")
	claudeTier := filepath.Join(fixtureHome, ".claude", "skills")
	writeTierFixtureSkill(t, claudeTier, "tier-skill")

	w := NewWriter(skillsDir, nil)
	if err := w.ArchiveSkill("tier-skill"); err != nil {
		t.Fatalf("ArchiveSkill: %v", err)
	}

	// Archive lands per Writer's existing naming/location semantics:
	// <skillsDir>.archived/<name>/SKILL.md under the Writer root.
	archivedFile := filepath.Join(dataDir, "skills.archived", "tier-skill", "SKILL.md")
	if _, err := os.Stat(archivedFile); err != nil {
		t.Errorf("archived skill missing at legacy archive path %s: %v", archivedFile, err)
	}

	// The skill is gone from its tier.
	if _, err := os.Stat(filepath.Join(claudeTier, "tier-skill")); !os.IsNotExist(err) {
		t.Errorf("skill still present in tier after archive (err=%v)", err)
	}

	// The fixed default tier root was never created under HOME.
	if _, err := os.Stat(filepath.Join(fixtureHome, ".meept")); !os.IsNotExist(err) {
		t.Errorf("~/.meept was created or consulted under fixture HOME (err=%v)", err)
	}
}

// TestWriterTier_ArchiveNoTier verifies ArchiveSkill returns a clean error
// naming the skill when it exists in no discovery tier.
func TestWriterTier_ArchiveNoTier(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("HOME", fixtureHome)

	dataDir := t.TempDir()
	w := NewWriter(filepath.Join(dataDir, "skills"), nil)

	err := w.ArchiveSkill("ghost-skill")
	if err == nil {
		t.Fatal("expected error archiving a skill that exists in no tier")
	}
	if !errors.Is(err, skills.ErrSkillNotFound) {
		t.Errorf("error should wrap skills.ErrSkillNotFound, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ghost-skill") {
		t.Errorf("error should name the skill, got: %v", err)
	}
}

// TestWriterTier_WriteLandsInTier verifies the refine/edit path: WriteSkill on
// a skill that lives in a non-default tier edits the resolved tier file, not
// a path under the Writer's DataDir root. ReadSkill follows the same
// resolution.
func TestWriterTier_WriteLandsInTier(t *testing.T) {
	fixtureHome := t.TempDir()
	t.Setenv("HOME", fixtureHome)

	dataDir := t.TempDir()
	skillsDir := filepath.Join(dataDir, "skills")
	claudeTier := filepath.Join(fixtureHome, ".claude", "skills")
	writeTierFixtureSkill(t, claudeTier, "edit-skill")

	w := NewWriter(skillsDir, nil)

	edited := "---\nname: edit-skill\ndescription: refined\n---\n\nrefined body\n"
	if err := w.WriteSkill("edit-skill", edited); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	// The edit landed in the resolved tier file.
	tierFile := filepath.Join(claudeTier, "edit-skill", "SKILL.md")
	data, err := os.ReadFile(tierFile)
	if err != nil {
		t.Fatalf("edited tier file missing at %s: %v", tierFile, err)
	}
	if string(data) != edited {
		t.Errorf("tier file content mismatch:\ngot:  %q\nwant: %q", string(data), edited)
	}

	// Nothing was written under the DataDir root.
	if _, err := os.Stat(filepath.Join(skillsDir, "edit-skill")); !os.IsNotExist(err) {
		t.Errorf("skill dir created under DataDir root (err=%v)", err)
	}

	// ReadSkill resolves the same way.
	got, err := w.ReadSkill("edit-skill")
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	if got != edited {
		t.Errorf("ReadSkill content mismatch:\ngot:  %q\nwant: %q", got, edited)
	}
}

// TestWriterTier_SetTierResolverOverrides verifies the nil-guarded setter:
// an explicitly injected resolver drives resolution (fixture tiers built
// without touching HOME at all), proving the daemon can wire a custom
// discovery if it ever needs to.
func TestWriterTier_SetTierResolverOverrides(t *testing.T) {
	fixtureRoot := t.TempDir()
	tierDir := filepath.Join(fixtureRoot, "custom-tier")
	writeTierFixtureSkill(t, tierDir, "injected-skill")

	discovery := skills.NewDiscovery(
		skills.WithSources(skills.NewClaudeSourceWithPath(tierDir, nil)),
	)

	dataDir := t.TempDir()
	w := NewWriter(filepath.Join(dataDir, "skills"), nil)
	w.SetTierResolver(discovery)
	w.SetTierResolver(nil) // nil guard: must be a no-op, not a wipe

	if err := w.ArchiveSkill("injected-skill"); err != nil {
		t.Fatalf("ArchiveSkill with injected resolver: %v", err)
	}
	archivedFile := filepath.Join(dataDir, "skills.archived", "injected-skill", "SKILL.md")
	if _, err := os.Stat(archivedFile); err != nil {
		t.Errorf("archived skill missing at %s: %v", archivedFile, err)
	}
}
