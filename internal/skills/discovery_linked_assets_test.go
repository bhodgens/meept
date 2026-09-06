package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiscovery_LinkedAssets verifies that directory-layout skills record
// their skill directory and linked assets, while flat-layout skills keep
// Dir empty and LinkedAssets nil.
func TestDiscovery_LinkedAssets(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills-linked-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skillsDir := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("Failed to create skills dir: %v", err)
	}

	// Directory-layout skill with linked assets.
	subDir := filepath.Join(skillsDir, "linked-skill")
	for _, sub := range []string{"scripts", "references", "assets", "random", "templates/nested"} {
		if err := os.MkdirAll(filepath.Join(subDir, sub), 0o755); err != nil {
			t.Fatalf("Failed to create skill subdir %s: %v", sub, err)
		}
	}

	skillContent := "---\nname: linked-skill\ndescription: Skill with linked assets\n---\n\nUse $SKILL_DIR/scripts/helper.py.\n"
	if err := os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}

	assets := map[string]string{
		"scripts/a.py":            "print('a')",
		"scripts/b.sh":            "#!/bin/sh\necho b",
		"references/notes.md":     "# notes",
		"assets/logo.png":         "fakepng",
		"random/ignored.txt":      "should be invisible",
		"templates/nested/x.tmpl": "nested — one level deep only",
	}
	for rel, content := range assets {
		if err := os.WriteFile(filepath.Join(subDir, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to write asset %s: %v", rel, err)
		}
	}

	// Flat-layout skill.
	flatContent := "---\nname: flat-skill\ndescription: A flat skill\n---\n\nFlat instructions.\n"
	if err := os.WriteFile(filepath.Join(skillsDir, "flat-skill.md"), []byte(flatContent), 0o644); err != nil {
		t.Fatalf("Failed to write flat skill: %v", err)
	}

	discovery := NewDiscovery(
		WithTiers([]DiscoveryTier{
			{Path: skillsDir, Priority: PriorityProject},
		}),
	)

	if _, err := discovery.Discover(); err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	linked := discovery.GetSkill("linked-skill")
	if linked == nil {
		t.Fatal("linked-skill not found")
	}

	if linked.Dir != subDir {
		t.Errorf("Dir = %q, want %q", linked.Dir, subDir)
	}

	wantRelPaths := []string{"assets/logo.png", "references/notes.md", "scripts/a.py", "scripts/b.sh"}
	if len(linked.LinkedAssets) != len(wantRelPaths) {
		t.Fatalf("LinkedAssets has %d entries, want %d: %+v", len(linked.LinkedAssets), len(wantRelPaths), linked.LinkedAssets)
	}
	for i, want := range wantRelPaths {
		got := linked.LinkedAssets[i]
		if got.RelPath != want {
			t.Errorf("LinkedAssets[%d].RelPath = %q, want %q (list must be sorted by RelPath)", i, got.RelPath, want)
		}
		if wantName := filepath.Base(want); got.Name != wantName {
			t.Errorf("LinkedAssets[%d].Name = %q, want %q", i, got.Name, wantName)
		}
	}
	// random/ignored.txt must be absent.
	for _, a := range linked.LinkedAssets {
		if a.RelPath == "random/ignored.txt" {
			t.Errorf("random/ignored.txt should not be a linked asset: %+v", a)
		}
	}
	// Kind classification spot-checks.
	wantKinds := map[string]string{
		"scripts/a.py":        "script",
		"scripts/b.sh":        "script",
		"references/notes.md": "reference",
		"assets/logo.png":     "asset",
	}
	for _, a := range linked.LinkedAssets {
		wantKind := wantKinds[a.RelPath]
		if a.Kind != wantKind {
			t.Errorf("LinkedAssets[%s].Kind = %q, want %q", a.RelPath, a.Kind, wantKind)
		}
	}

	flat := discovery.GetSkill("flat-skill")
	if flat == nil {
		t.Fatal("flat-skill not found")
	}
	if flat.Dir != "" {
		t.Errorf("flat skill Dir = %q, want empty", flat.Dir)
	}
	if flat.LinkedAssets != nil {
		t.Errorf("flat skill LinkedAssets = %+v, want nil", flat.LinkedAssets)
	}
}
