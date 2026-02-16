package skills

import (
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	skill := &Skill{
		Name:        "test-skill",
		Description: "A test skill",
		Requires:    []string{"code"},
		Tags:        []string{"test"},
	}

	reg.Register(skill)

	got := reg.Get("test-skill")
	if got == nil {
		t.Fatal("Get returned nil")
	}

	if got.Name != "test-skill" {
		t.Errorf("Name = %q, want test-skill", got.Name)
	}
}

func TestRegistry_GetCaseInsensitive(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{
		Name:        "MixedCase-Skill",
		Description: "Test",
	})

	tests := []string{
		"MixedCase-Skill",
		"mixedcase-skill",
		"MIXEDCASE-SKILL",
	}

	for _, name := range tests {
		got := reg.Get(name)
		if got == nil {
			t.Errorf("Get(%q) = nil, want non-nil", name)
		}
	}
}

func TestRegistry_RegisterAll(t *testing.T) {
	reg := NewRegistry()

	skills := []*Skill{
		{Name: "skill-a", Description: "A"},
		{Name: "skill-b", Description: "B"},
		{Name: "skill-c", Description: "C"},
	}

	reg.RegisterAll(skills)

	if reg.Count() != 3 {
		t.Errorf("Count = %d, want 3", reg.Count())
	}

	for _, s := range skills {
		if reg.Get(s.Name) == nil {
			t.Errorf("Skill %q not found", s.Name)
		}
	}
}

func TestRegistry_Unregister(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "to-remove"})

	if reg.Get("to-remove") == nil {
		t.Fatal("Skill not registered")
	}

	removed := reg.Unregister("to-remove")
	if !removed {
		t.Error("Unregister returned false")
	}

	if reg.Get("to-remove") != nil {
		t.Error("Skill still exists after unregister")
	}

	// Unregistering non-existent should return false
	if reg.Unregister("nonexistent") {
		t.Error("Unregister nonexistent should return false")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "gamma"})
	reg.Register(&Skill{Name: "alpha"})
	reg.Register(&Skill{Name: "beta"})

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("List length = %d, want 3", len(list))
	}

	// Should be sorted by name
	if list[0].Name != "alpha" {
		t.Errorf("First skill = %q, want alpha (should be sorted)", list[0].Name)
	}
}

func TestRegistry_Names(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "gamma"})
	reg.Register(&Skill{Name: "alpha"})
	reg.Register(&Skill{Name: "beta"})

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("Names length = %d, want 3", len(names))
	}

	// Should be sorted
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Errorf("Names = %v, want [alpha, beta, gamma]", names)
	}
}

func TestRegistry_FindByTag(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "skill-1", Tags: []string{"code", "review"}})
	reg.Register(&Skill{Name: "skill-2", Tags: []string{"code", "debug"}})
	reg.Register(&Skill{Name: "skill-3", Tags: []string{"docs"}})

	// Find by "code" tag
	found := reg.FindByTag("code")
	if len(found) != 2 {
		t.Errorf("FindByTag(code) = %d skills, want 2", len(found))
	}

	// Find by "docs" tag
	found = reg.FindByTag("docs")
	if len(found) != 1 {
		t.Errorf("FindByTag(docs) = %d skills, want 1", len(found))
	}

	// Find nonexistent tag
	found = reg.FindByTag("nonexistent")
	if len(found) != 0 {
		t.Errorf("FindByTag(nonexistent) = %d skills, want 0", len(found))
	}

	// Case insensitive
	found = reg.FindByTag("CODE")
	if len(found) != 2 {
		t.Errorf("FindByTag(CODE) = %d skills, want 2 (case insensitive)", len(found))
	}
}

func TestRegistry_FindByTags(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "skill-1", Tags: []string{"code", "review"}})
	reg.Register(&Skill{Name: "skill-2", Tags: []string{"code", "debug"}})
	reg.Register(&Skill{Name: "skill-3", Tags: []string{"code", "review", "debug"}})

	// Find by multiple tags
	found := reg.FindByTags([]string{"code", "review"})
	if len(found) != 2 {
		t.Errorf("FindByTags([code, review]) = %d skills, want 2", len(found))
	}

	// All three tags
	found = reg.FindByTags([]string{"code", "review", "debug"})
	if len(found) != 1 {
		t.Errorf("FindByTags([code, review, debug]) = %d skills, want 1", len(found))
	}
}

func TestRegistry_FindByCapability(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "skill-1", Requires: []string{"code", "reasoning"}})
	reg.Register(&Skill{Name: "skill-2", Requires: []string{"code"}})
	reg.Register(&Skill{Name: "skill-3", Requires: []string{"tool_use"}})

	// Find skills requiring "code"
	found := reg.FindByCapability("code")
	if len(found) != 2 {
		t.Errorf("FindByCapability(code) = %d skills, want 2", len(found))
	}

	// Find skills requiring "reasoning"
	found = reg.FindByCapability("reasoning")
	if len(found) != 1 {
		t.Errorf("FindByCapability(reasoning) = %d skills, want 1", len(found))
	}
}

func TestRegistry_FindByCapabilities(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "no-reqs", Requires: nil})
	reg.Register(&Skill{Name: "code-only", Requires: []string{"code"}})
	reg.Register(&Skill{Name: "code-reasoning", Requires: []string{"code", "reasoning"}})
	reg.Register(&Skill{Name: "tool-use", Requires: []string{"tool_use"}})

	// Model has [code] - should match no-reqs and code-only
	found := reg.FindByCapabilities([]string{"code"})
	if len(found) != 2 {
		t.Errorf("FindByCapabilities([code]) = %d skills, want 2", len(found))
	}

	// Model has [code, reasoning] - should match no-reqs, code-only, code-reasoning
	found = reg.FindByCapabilities([]string{"code", "reasoning"})
	if len(found) != 3 {
		t.Errorf("FindByCapabilities([code, reasoning]) = %d skills, want 3", len(found))
	}

	// Model has everything
	found = reg.FindByCapabilities([]string{"code", "reasoning", "tool_use"})
	if len(found) != 4 {
		t.Errorf("FindByCapabilities([code, reasoning, tool_use]) = %d skills, want 4", len(found))
	}

	// Empty capabilities - only no-reqs skill matches
	found = reg.FindByCapabilities([]string{})
	if len(found) != 1 {
		t.Errorf("FindByCapabilities([]) = %d skills, want 1", len(found))
	}
}

func TestRegistry_Match(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "code-review", Description: "Review code for bugs", Tags: []string{"code"}})
	reg.Register(&Skill{Name: "code-debug", Description: "Debug code issues", Tags: []string{"debug"}})
	reg.Register(&Skill{Name: "doc-writer", Description: "Write documentation"})

	// Exact match
	found := reg.Match("code-review")
	if found == nil || found.Name != "code-review" {
		t.Errorf("Match(code-review) = %v, want code-review", found)
	}

	// Prefix match
	found = reg.Match("code")
	if found == nil {
		t.Error("Match(code) = nil, want non-nil")
	}

	// Description match
	found = reg.Match("bugs")
	if found == nil || found.Name != "code-review" {
		t.Errorf("Match(bugs) = %v, want code-review", found)
	}

	// No match
	found = reg.Match("nonexistent-unique-query")
	if found != nil {
		t.Errorf("Match(nonexistent) = %v, want nil", found)
	}

	// Empty query
	found = reg.Match("")
	if found != nil {
		t.Errorf("Match('') = %v, want nil", found)
	}
}

func TestRegistry_MatchAll(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "code-review", Description: "Review code", Tags: []string{"code"}})
	reg.Register(&Skill{Name: "code-debug", Description: "Debug code", Tags: []string{"code"}})
	reg.Register(&Skill{Name: "doc-writer", Description: "Write docs"})

	matches := reg.MatchAll("code")
	if len(matches) != 2 {
		t.Errorf("MatchAll(code) = %d matches, want 2", len(matches))
	}

	// Should be sorted by score
	if len(matches) >= 2 && matches[0].Score < matches[1].Score {
		t.Error("Matches should be sorted by score descending")
	}
}

func TestRegistry_Clear(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "skill-1"})
	reg.Register(&Skill{Name: "skill-2"})

	if reg.Count() != 2 {
		t.Fatalf("Count = %d, want 2", reg.Count())
	}

	reg.Clear()

	if reg.Count() != 0 {
		t.Errorf("Count after Clear = %d, want 0", reg.Count())
	}

	if reg.Get("skill-1") != nil {
		t.Error("Skill still exists after Clear")
	}
}

func TestRegistry_GetRequirements(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&Skill{Name: "skill-with-reqs", Requires: []string{"code", "reasoning"}})
	reg.Register(&Skill{Name: "skill-no-reqs"})

	reqs := reg.GetRequirements("skill-with-reqs")
	if len(reqs) != 2 {
		t.Errorf("GetRequirements = %v, want [code, reasoning]", reqs)
	}

	reqs = reg.GetRequirements("skill-no-reqs")
	if len(reqs) != 0 {
		t.Errorf("GetRequirements(no-reqs) = %v, want empty", reqs)
	}

	reqs = reg.GetRequirements("nonexistent")
	if reqs != nil {
		t.Errorf("GetRequirements(nonexistent) = %v, want nil", reqs)
	}
}

func TestSkill_HasCapability(t *testing.T) {
	skill := &Skill{
		Requires: []string{"code", "reasoning"},
	}

	if !skill.HasCapability("code") {
		t.Error("HasCapability(code) = false, want true")
	}

	if skill.HasCapability("tool_use") {
		t.Error("HasCapability(tool_use) = true, want false")
	}
}

func TestSkill_HasTag(t *testing.T) {
	skill := &Skill{
		Tags: []string{"development", "review"},
	}

	if !skill.HasTag("development") {
		t.Error("HasTag(development) = false, want true")
	}

	if skill.HasTag("testing") {
		t.Error("HasTag(testing) = true, want false")
	}
}

func TestSkill_MatchesTags(t *testing.T) {
	skill := &Skill{
		Tags: []string{"code", "review", "quality"},
	}

	if !skill.MatchesTags([]string{"code", "review"}) {
		t.Error("MatchesTags([code, review]) = false, want true")
	}

	if skill.MatchesTags([]string{"code", "debug"}) {
		t.Error("MatchesTags([code, debug]) = true, want false")
	}

	if !skill.MatchesTags([]string{}) {
		t.Error("MatchesTags([]) = false, want true (empty should match)")
	}
}

func TestMatchScore(t *testing.T) {
	skill := &Skill{
		Name:        "code-review",
		Description: "Review code for bugs and issues",
		Tags:        []string{"code", "quality"},
	}

	// Exact name match should have high score
	exactScore := matchScore(skill, "code-review")
	prefixScore := matchScore(skill, "code")
	descScore := matchScore(skill, "bugs")
	noMatchScore := matchScore(skill, "zzzzz")

	if exactScore <= prefixScore {
		t.Errorf("Exact match score (%d) should be > prefix score (%d)", exactScore, prefixScore)
	}

	if prefixScore <= descScore {
		t.Errorf("Prefix score (%d) should be > description score (%d)", prefixScore, descScore)
	}

	if noMatchScore != 0 {
		t.Errorf("No match score should be 0, got %d", noMatchScore)
	}
}
