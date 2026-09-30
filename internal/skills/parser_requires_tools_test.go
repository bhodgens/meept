package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// Pins the L9 fix: SkillIndexEntry.RequiresTools must carry the frontmatter
// requires-tools metadata at BOTH construction sites (parser metadata-only
// path and the discovery fallback that strips bodies from full skills).
func TestParseSkillMetadataOnly_PopulatesRequiresTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := `---
name: requires-tools-fixture
description: fixture skill carrying requires-tools
requires-tools:
  - web_fetch
  - cua-driver.capture
---
Body text.
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	entry, err := ParseSkillMetadataOnly(path)
	if err != nil {
		t.Fatalf("ParseSkillMetadataOnly: %v", err)
	}
	if len(entry.RequiresTools) != 2 {
		t.Fatalf("RequiresTools = %v, want 2 entries", entry.RequiresTools)
	}
	if entry.RequiresTools[0] != "web_fetch" || entry.RequiresTools[1] != "cua-driver.capture" {
		t.Errorf("RequiresTools = %v, want [web_fetch cua-driver.capture]", entry.RequiresTools)
	}
}

func TestParseSkillText_EntryConstruction_RequiresTools(t *testing.T) {
	// Discovery's non-FileSource fallback strips bodies from full Skill
	// structs; verify the Skill itself carries RequiresTools through
	// ParseSkillText (the source of that copy).
	text := `---
name: full-parse-fixture
description: fixture
requires-tools: [alpha.one]
---
Body.
`
	skill, err := ParseSkillText(text)
	if err != nil {
		t.Fatalf("ParseSkillText: %v", err)
	}
	if len(skill.RequiresTools) != 1 || skill.RequiresTools[0] != "alpha.one" {
		t.Errorf("Skill.RequiresTools = %v, want [alpha.one]", skill.RequiresTools)
	}
}
