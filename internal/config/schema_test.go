package config

import "testing"

// TestDefaultConfig_SkillsWikiState verifies the frozen defaults for the
// [skills.wiki] and [skills.state] sections (05-config-wiring.md §Contract 5):
// wiki defaults enabled (the store is inert until wired), state defaults
// disabled (opt-in by config; no default flips in this leaf).
func TestDefaultConfig_SkillsWikiState(t *testing.T) {
	c := DefaultConfig()
	if !c.Skills.Wiki.Enabled {
		t.Fatal("wiki default must be enabled")
	}
	if c.Skills.Wiki.Dir != "~/.meept/wiki" {
		t.Fatalf("dir: %q", c.Skills.Wiki.Dir)
	}
	if c.Skills.State.Enabled {
		t.Fatal("state default must be disabled")
	}
	if c.Skills.State.MaxStateChars != 2000 {
		t.Fatalf("max chars: %d", c.Skills.State.MaxStateChars)
	}
}
