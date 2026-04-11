package agent

import (
	"testing"
)

func TestAgentCapabilities_HasCapability(t *testing.T) {
	caps := &AgentCapabilities{
		AgentID: "test",
		SkillCapabilities: map[string]bool{
			"code":      true,
			"reasoning": true,
		},
	}

	tests := []struct {
		cap  string
		want bool
	}{
		{"code", true},
		{"Code", true}, // case insensitive
		{"reasoning", true},
		{"tool_use", false},
	}

	for _, tt := range tests {
		t.Run(tt.cap, func(t *testing.T) {
			if got := caps.HasCapability(tt.cap); got != tt.want {
				t.Errorf("HasCapability(%q) = %v, want %v", tt.cap, got, tt.want)
			}
		})
	}
}

func TestAgentCapabilities_HasTag(t *testing.T) {
	caps := &AgentCapabilities{
		AgentID: "test",
		SkillTags: map[string]bool{
			"coding":     true,
			"automation": true,
		},
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
			if got := caps.HasTag(tt.tag); got != tt.want {
				t.Errorf("HasTag(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestAgentCapabilities_HasIntentType(t *testing.T) {
	caps := &AgentCapabilities{
		AgentID:     "test",
		IntentTypes: []string{"code", "review"},
	}

	tests := []struct {
		intent string
		want   bool
	}{
		{"code", true},
		{"Code", true}, // case insensitive
		{"review", true},
		{"debug", false},
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			if got := caps.HasIntentType(tt.intent); got != tt.want {
				t.Errorf("HasIntentType(%q) = %v, want %v", tt.intent, got, tt.want)
			}
		})
	}
}

func TestCapabilitiesMap_Add(t *testing.T) {
	cm := NewCapabilitiesMap()

	caps := &AgentCapabilities{
		AgentID:     "coder",
		Name:        "Coder Agent",
		Role:        RoleExecutor,
		IntentTypes: []string{"code", "review"},
		Keywords:    []string{"implement", "refactor"},
		SkillCapabilities: map[string]bool{
			"code": true,
		},
	}

	cm.Add(caps)

	if cm.Count() != 1 {
		t.Errorf("Count() = %d, want 1", cm.Count())
	}

	got := cm.Get("coder")
	if got == nil {
		t.Fatal("Get(coder) returned nil")
	}
	if got.Name != "Coder Agent" {
		t.Errorf("Get(coder).Name = %q, want %q", got.Name, "Coder Agent")
	}
}

func TestCapabilitiesMap_FindByIntentType(t *testing.T) {
	cm := NewCapabilitiesMap()

	cm.Add(&AgentCapabilities{
		AgentID:     "coder",
		IntentTypes: []string{"code", "review"},
	})
	cm.Add(&AgentCapabilities{
		AgentID:     "debugger",
		IntentTypes: []string{"debug"},
	})
	cm.Add(&AgentCapabilities{
		AgentID:     "analyst",
		IntentTypes: []string{"analyze", "review"}, // shared "review"
	})

	tests := []struct {
		intent    string
		wantCount int
	}{
		{"code", 1},
		{"review", 2}, // coder and analyst
		{"debug", 1},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			agents := cm.FindByIntentType(tt.intent)
			if len(agents) != tt.wantCount {
				t.Errorf("FindByIntentType(%q) returned %d agents, want %d", tt.intent, len(agents), tt.wantCount)
			}
		})
	}
}

func TestCapabilitiesMap_FindByKeyword(t *testing.T) {
	cm := NewCapabilitiesMap()

	cm.Add(&AgentCapabilities{
		AgentID:  "coder",
		Keywords: []string{"implement", "refactor", "write code"},
	})
	cm.Add(&AgentCapabilities{
		AgentID:  "debugger",
		Keywords: []string{"fix bug", "debug"},
	})

	tests := []struct {
		keyword   string
		wantCount int
	}{
		{"implement", 1},
		{"fix bug", 1},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.keyword, func(t *testing.T) {
			agents := cm.FindByKeyword(tt.keyword)
			if len(agents) != tt.wantCount {
				t.Errorf("FindByKeyword(%q) returned %d agents, want %d", tt.keyword, len(agents), tt.wantCount)
			}
		})
	}
}

func TestCapabilitiesMap_MatchKeywords(t *testing.T) {
	cm := NewCapabilitiesMap()

	cm.Add(&AgentCapabilities{
		AgentID:  "coder",
		Keywords: []string{"implement", "refactor", "write code"},
	})
	cm.Add(&AgentCapabilities{
		AgentID:  "debugger",
		Keywords: []string{"fix bug", "debug", "error"},
	})

	// Test matching multiple keywords
	matches := cm.MatchKeywords("please implement this and refactor")
	if matches["coder"] != 2 {
		t.Errorf("MatchKeywords(coder) = %d, want 2", matches["coder"])
	}

	// Test matching debugger keywords
	matches = cm.MatchKeywords("fix bug in the code")
	if matches["debugger"] != 1 {
		t.Errorf("MatchKeywords(debugger) = %d, want 1", matches["debugger"])
	}
}

func TestCapabilitiesMap_FindByCapability(t *testing.T) {
	cm := NewCapabilitiesMap()

	cm.Add(&AgentCapabilities{
		AgentID: "coder",
		SkillCapabilities: map[string]bool{
			"code":      true,
			"reasoning": true,
		},
	})
	cm.Add(&AgentCapabilities{
		AgentID: "analyst",
		SkillCapabilities: map[string]bool{
			"reasoning": true,
		},
	})

	tests := []struct {
		cap       string
		wantCount int
	}{
		{"code", 1},
		{"reasoning", 2},
		{"nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.cap, func(t *testing.T) {
			agents := cm.FindByCapability(tt.cap)
			if len(agents) != tt.wantCount {
				t.Errorf("FindByCapability(%q) returned %d agents, want %d", tt.cap, len(agents), tt.wantCount)
			}
		})
	}
}

func TestCapabilitiesMap_Clear(t *testing.T) {
	cm := NewCapabilitiesMap()

	cm.Add(&AgentCapabilities{
		AgentID:     "coder",
		IntentTypes: []string{"code"},
		Keywords:    []string{"implement"},
	})
	cm.Add(&AgentCapabilities{
		AgentID:     "debugger",
		IntentTypes: []string{"debug"},
	})

	if cm.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", cm.Count())
	}

	cm.Clear()

	if cm.Count() != 0 {
		t.Errorf("Count() after Clear() = %d, want 0", cm.Count())
	}
}

func TestCapabilitiesMap_AgentIDs(t *testing.T) {
	cm := NewCapabilitiesMap()

	cm.Add(&AgentCapabilities{AgentID: "charlie"})
	cm.Add(&AgentCapabilities{AgentID: "alpha"})
	cm.Add(&AgentCapabilities{AgentID: "bravo"})

	ids := cm.AgentIDs()
	if len(ids) != 3 {
		t.Fatalf("AgentIDs() returned %d IDs, want 3", len(ids))
	}

	// Should be sorted
	if ids[0] != "alpha" || ids[1] != "bravo" || ids[2] != "charlie" {
		t.Errorf("AgentIDs() = %v, want [alpha bravo charlie]", ids)
	}
}

func TestCapabilitiesMap_List(t *testing.T) {
	cm := NewCapabilitiesMap()

	cm.Add(&AgentCapabilities{AgentID: "charlie", Name: "Charlie"})
	cm.Add(&AgentCapabilities{AgentID: "alpha", Name: "Alpha"})

	list := cm.List()
	if len(list) != 2 {
		t.Fatalf("List() returned %d items, want 2", len(list))
	}

	// Should be sorted by AgentID
	if list[0].AgentID != "alpha" {
		t.Errorf("List()[0].AgentID = %q, want %q", list[0].AgentID, "alpha")
	}
}

func TestCapabilitiesMap_AllMethods(t *testing.T) {
	cm := NewCapabilitiesMap()

	cm.Add(&AgentCapabilities{
		AgentID:     "coder",
		IntentTypes: []string{"code", "review"},
		Keywords:    []string{"implement"},
		SkillCapabilities: map[string]bool{
			"code": true,
		},
	})

	// AllIntentTypes
	intents := cm.AllIntentTypes()
	if len(intents) != 2 {
		t.Errorf("AllIntentTypes() returned %d, want 2", len(intents))
	}

	// AllKeywords
	keywords := cm.AllKeywords()
	if len(keywords) != 1 {
		t.Errorf("AllKeywords() returned %d, want 1", len(keywords))
	}

	// AllCapabilities
	caps := cm.AllCapabilities()
	if len(caps) != 1 {
		t.Errorf("AllCapabilities() returned %d, want 1", len(caps))
	}
}
