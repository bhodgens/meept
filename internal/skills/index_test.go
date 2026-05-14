package skills

import (
	"testing"
)

func TestSkillIndexEntry_HasCapability(t *testing.T) {
	entry := &SkillIndexEntry{
		Name:     "test-skill",
		Requires: []string{"code", "reasoning"},
	}

	tests := []struct {
		cap  string
		want bool
	}{
		{"code", true},
		{"Code", true}, // case insensitive
		{"reasoning", true},
		{"tool_use", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.cap, func(t *testing.T) {
			if got := entry.HasCapability(tt.cap); got != tt.want {
				t.Errorf("HasCapability(%q) = %v, want %v", tt.cap, got, tt.want)
			}
		})
	}
}

func TestSkillIndexEntry_HasTag(t *testing.T) {
	entry := &SkillIndexEntry{
		Name: "test-skill",
		Tags: []string{"coding", "automation"},
	}

	tests := []struct {
		tag  string
		want bool
	}{
		{"coding", true},
		{"Coding", true}, // case insensitive
		{"automation", true},
		{"testing", false},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := entry.HasTag(tt.tag); got != tt.want {
				t.Errorf("HasTag(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestSkillIndex_Index(t *testing.T) {
	idx := NewSkillIndex()

	entry := &SkillIndexEntry{
		Name:        "test-skill",
		Description: "A test skill",
		Requires:    []string{"code"},
		Tags:        []string{"testing"},
		Path:        "/path/to/skill.md",
		Priority:    0,
	}

	idx.Index(entry)

	if idx.Count() != 1 {
		t.Errorf("Count() = %d, want 1", idx.Count())
	}

	got := idx.Get("test-skill")
	if got == nil {
		t.Fatal("Get(test-skill) returned nil")
	}
	if got.Name != "test-skill" {
		t.Errorf("Get(test-skill).Name = %q, want %q", got.Name, "test-skill")
	}
}

func TestSkillIndex_Get_CaseInsensitive(t *testing.T) {
	idx := NewSkillIndex()

	idx.Index(&SkillIndexEntry{
		Name: "Test-Skill",
		Path: "/path/to/skill.md",
	})

	tests := []string{
		"Test-Skill",
		"test-skill",
		"TEST-SKILL",
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			got := idx.Get(name)
			if got == nil {
				t.Errorf("Get(%q) returned nil", name)
			}
		})
	}
}

func TestSkillIndex_IndexAll(t *testing.T) {
	idx := NewSkillIndex()

	entries := []*SkillIndexEntry{
		{Name: "skill-1", Path: "/path/1.md"},
		{Name: "skill-2", Path: "/path/2.md"},
		{Name: "skill-3", Path: "/path/3.md"},
	}

	idx.IndexAll(entries)

	if idx.Count() != 3 {
		t.Errorf("Count() = %d, want 3", idx.Count())
	}
}

func TestSkillIndex_FindByTag(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "skill-1", Tags: []string{"coding", "automation"}},
		{Name: "skill-2", Tags: []string{"coding"}},
		{Name: "skill-3", Tags: []string{"testing"}},
	})

	// Find by coding tag
	coding := idx.FindByTag("coding")
	if len(coding) != 2 {
		t.Errorf("FindByTag(coding) returned %d entries, want 2", len(coding))
	}

	// Find by testing tag
	testingSkills := idx.FindByTag("testing")
	if len(testingSkills) != 1 {
		t.Errorf("FindByTag(testing) returned %d entries, want 1", len(testingSkills))
	}

	// Find non-existent tag
	none := idx.FindByTag("nonexistent")
	if len(none) != 0 {
		t.Errorf("FindByTag(nonexistent) returned %d entries, want 0", len(none))
	}
}

func TestSkillIndex_FindByCapability(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "skill-1", Requires: []string{"code", "reasoning"}},
		{Name: "skill-2", Requires: []string{"code"}},
		{Name: "skill-3", Requires: []string{"tool_use"}},
	})

	// Find by code capability
	code := idx.FindByCapability("code")
	if len(code) != 2 {
		t.Errorf("FindByCapability(code) returned %d entries, want 2", len(code))
	}

	// Find by reasoning capability
	reasoning := idx.FindByCapability("reasoning")
	if len(reasoning) != 1 {
		t.Errorf("FindByCapability(reasoning) returned %d entries, want 1", len(reasoning))
	}
}

func TestSkillIndex_FindByCapabilities(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "skill-1", Requires: []string{"code", "reasoning"}},
		{Name: "skill-2", Requires: []string{"code"}},
		{Name: "skill-3", Requires: []string{}}, // no requirements
	})

	// Find entries satisfiable by ["code"]
	codeOnly := idx.FindByCapabilities([]string{"code"})
	if len(codeOnly) != 2 { // skill-2 and skill-3 (no requirements)
		t.Errorf("FindByCapabilities([code]) returned %d entries, want 2", len(codeOnly))
	}

	// Find entries satisfiable by ["code", "reasoning"]
	both := idx.FindByCapabilities([]string{"code", "reasoning"})
	if len(both) != 3 { // all three
		t.Errorf("FindByCapabilities([code,reasoning]) returned %d entries, want 3", len(both))
	}
}

func TestSkillIndex_Match(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "code-review", Description: "Review code for quality"},
		{Name: "code-format", Description: "Format code files"},
		{Name: "test-runner", Description: "Run tests"},
	})

	tests := []struct {
		query    string
		wantName string
	}{
		{"code-review", "code-review"},   // exact match
		{"code", "code-review"},          // prefix match (or highest scoring)
		{"review", "code-review"},        // contains match
		{"format code", "code-format"},   // word match
		{"nonexistent", ""},              // no match
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := idx.Match(tt.query)
			if tt.wantName == "" {
				if got != nil {
					t.Errorf("Match(%q) = %v, want nil", tt.query, got.Name)
				}
			} else {
				if got == nil || got.Name != tt.wantName {
					name := ""
					if got != nil {
						name = got.Name
					}
					t.Errorf("Match(%q) = %v, want %v", tt.query, name, tt.wantName)
				}
			}
		})
	}
}

func TestSkillIndex_Clear(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "skill-1", Tags: []string{"test"}},
		{Name: "skill-2", Requires: []string{"code"}},
	})

	if idx.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", idx.Count())
	}

	idx.Clear()

	if idx.Count() != 0 {
		t.Errorf("Count() after Clear() = %d, want 0", idx.Count())
	}

	if len(idx.AllTags()) != 0 {
		t.Errorf("AllTags() after Clear() = %v, want empty", idx.AllTags())
	}

	if len(idx.AllCapabilities()) != 0 {
		t.Errorf("AllCapabilities() after Clear() = %v, want empty", idx.AllCapabilities())
	}
}

func TestSkillIndex_Names(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "charlie"},
		{Name: "alpha"},
		{Name: "bravo"},
	})

	names := idx.Names()
	if len(names) != 3 {
		t.Fatalf("Names() returned %d names, want 3", len(names))
	}

	// Should be sorted
	if names[0] != "alpha" || names[1] != "bravo" || names[2] != "charlie" {
		t.Errorf("Names() = %v, want [alpha bravo charlie]", names)
	}
}

func TestSkillIndex_MatchAll(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "code-review", Description: "Review code"},
		{Name: "code-format", Description: "Format code"},
		{Name: "test-runner", Description: "Run tests"},
	})

	matches := idx.MatchAll("code")
	if len(matches) != 2 {
		t.Errorf("MatchAll(code) returned %d matches, want 2", len(matches))
	}

	// Should be sorted by score
	if len(matches) >= 2 && matches[0].Score < matches[1].Score {
		t.Error("MatchAll results not sorted by score descending")
	}
}
