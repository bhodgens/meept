package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTierTestDiscovery builds a Discovery over temp-dir fixture tiers ONLY:
// a user tier (~/.meept/skills) and a Claude tier (~/.claude/skills), both
// under the fixture homeDir. HOME is pointed at the fixture so any
// DefaultTiers()/os.UserHomeDir() leakage can never reach the developer's
// real home (which holds 189 live Claude skill dirs).
func newTierTestDiscovery(t *testing.T, homeDir string) *Discovery {
	t.Helper()
	t.Setenv("HOME", homeDir)

	userTier := filepath.Join(homeDir, ".meept", "skills")
	claudeTier := filepath.Join(homeDir, ".claude", "skills")
	if err := os.MkdirAll(userTier, 0o755); err != nil {
		t.Fatalf("create user tier: %v", err)
	}
	if err := os.MkdirAll(claudeTier, 0o755); err != nil {
		t.Fatalf("create claude tier: %v", err)
	}

	return NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: userTier, Priority: PriorityUser},
		}),
		WithSources(NewClaudeSourceWithPath(claudeTier, nil)),
	)
}

// writeFixtureSkill creates <tierDir>/<name>/SKILL.md with valid frontmatter.
func writeFixtureSkill(t *testing.T, tierDir, name string) {
	t.Helper()
	dir := filepath.Join(tierDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create skill dir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: fixture skill\n---\n\nbody of " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// TestResolveTierPath verifies tier resolution is consistent with discovery
// precedence: project > user > claude > hermes > system.
func TestResolveTierPath(t *testing.T) {
	t.Run("claude tier only", func(t *testing.T) {
		home := t.TempDir()
		d := newTierTestDiscovery(t, home)
		claudeTier := filepath.Join(home, ".claude", "skills")
		writeFixtureSkill(t, claudeTier, "solo-skill")

		tierRoot, skillPath, source, err := d.ResolveTierPath("solo-skill")
		if err != nil {
			t.Fatalf("ResolveTierPath: %v", err)
		}
		if tierRoot != claudeTier {
			t.Errorf("tierRoot = %q, want %q", tierRoot, claudeTier)
		}
		wantPath := filepath.Join(claudeTier, "solo-skill", "SKILL.md")
		if skillPath != wantPath {
			t.Errorf("skillPath = %q, want %q", skillPath, wantPath)
		}
		if source != "claude" {
			t.Errorf("source = %q, want %q", source, "claude")
		}
	})

	t.Run("user tier shadows claude per discovery precedence", func(t *testing.T) {
		home := t.TempDir()
		d := newTierTestDiscovery(t, home)
		userTier := filepath.Join(home, ".meept", "skills")
		claudeTier := filepath.Join(home, ".claude", "skills")
		writeFixtureSkill(t, userTier, "dupe-skill")
		writeFixtureSkill(t, claudeTier, "dupe-skill")

		tierRoot, _, source, err := d.ResolveTierPath("dupe-skill")
		if err != nil {
			t.Fatalf("ResolveTierPath: %v", err)
		}
		// PriorityUser (1) outranks PriorityClaude (2) — same order discovery
		// itself applies when merging sources.
		if tierRoot != userTier {
			t.Errorf("tierRoot = %q, want user tier %q (discovery precedence)", tierRoot, userTier)
		}
		if source != "meept" {
			t.Errorf("source = %q, want %q", source, "meept")
		}
	})

	t.Run("not found wraps sentinel with skill name", func(t *testing.T) {
		home := t.TempDir()
		d := newTierTestDiscovery(t, home)

		_, _, _, err := d.ResolveTierPath("absent-skill")
		if err == nil {
			t.Fatal("expected error for skill absent from all tiers")
		}
		if !errors.Is(err, ErrSkillNotFound) {
			t.Fatalf("error should wrap ErrSkillNotFound, got: %v", err)
		}
		if !strings.Contains(err.Error(), "absent-skill") {
			t.Errorf("error should name the skill, got: %v", err)
		}
	})

	t.Run("empty name wraps sentinel", func(t *testing.T) {
		home := t.TempDir()
		d := newTierTestDiscovery(t, home)

		_, _, _, err := d.ResolveTierPath("   ")
		if !errors.Is(err, ErrSkillNotFound) {
			t.Fatalf("error should wrap ErrSkillNotFound for empty name, got: %v", err)
		}
	})

	t.Run("stable across repeated calls", func(t *testing.T) {
		home := t.TempDir()
		d := newTierTestDiscovery(t, home)
		claudeTier := filepath.Join(home, ".claude", "skills")
		writeFixtureSkill(t, claudeTier, "stable-skill")

		root1, path1, src1, err1 := d.ResolveTierPath("stable-skill")
		root2, path2, src2, err2 := d.ResolveTierPath("stable-skill")
		if err1 != nil || err2 != nil {
			t.Fatalf("ResolveTierPath failed: %v / %v", err1, err2)
		}
		if root1 != root2 || path1 != path2 || src1 != src2 {
			t.Errorf("resolution not stable across calls:\nfirst:  %q %q %q\nsecond: %q %q %q",
				root1, path1, src1, root2, path2, src2)
		}
	})

	t.Run("lookup is case-insensitive like discovery", func(t *testing.T) {
		home := t.TempDir()
		d := newTierTestDiscovery(t, home)
		claudeTier := filepath.Join(home, ".claude", "skills")
		writeFixtureSkill(t, claudeTier, "cased-skill")

		_, skillPath, _, err := d.ResolveTierPath("Cased-Skill")
		if err != nil {
			t.Fatalf("ResolveTierPath: %v", err)
		}
		wantPath := filepath.Join(claudeTier, "cased-skill", "SKILL.md")
		if skillPath != wantPath {
			t.Errorf("skillPath = %q, want %q", skillPath, wantPath)
		}
	})
}
