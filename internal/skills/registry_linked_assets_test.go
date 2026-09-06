package skills

import "testing"

// TestRegistry_PreservesLinkedAssets verifies that Register/Get pass the
// Dir and LinkedAssets fields through intact (Registry stores *Skill
// pointers, so no field-by-field copying happens).
func TestRegistry_PreservesLinkedAssets(t *testing.T) {
	reg := NewRegistry()

	skill := &Skill{
		Name:        "linked-skill",
		Description: "Skill with linked assets",
		Dir:         "/tmp/skills/linked-skill",
		LinkedAssets: []LinkedAsset{
			{Name: "x.py", RelPath: "scripts/x.py", Kind: "script"},
			{Name: "notes.md", RelPath: "references/notes.md", Kind: "reference"},
		},
	}

	reg.Register(skill)

	got := reg.Get("linked-skill")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Dir != skill.Dir {
		t.Errorf("Dir = %q, want %q", got.Dir, skill.Dir)
	}
	if len(got.LinkedAssets) != len(skill.LinkedAssets) {
		t.Fatalf("LinkedAssets has %d entries, want %d", len(got.LinkedAssets), len(skill.LinkedAssets))
	}
	for i, want := range skill.LinkedAssets {
		if got.LinkedAssets[i] != want {
			t.Errorf("LinkedAssets[%d] = %+v, want %+v", i, got.LinkedAssets[i], want)
		}
	}
}
