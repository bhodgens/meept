package agent

import (
	"strings"
	"testing"
	"time"
)

func TestGetTeamPreset(t *testing.T) {
	tests := []struct {
		name    string
		preset  string
		wantErr bool
	}{
		{"hyperplan preset found", "hyperplan", false},
		{"security_research preset found", "security_research", false},
		{"unknown preset returns error", "nonexistent", true},
		{"empty preset returns error", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preset, err := GetTeamPreset(tc.preset)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for preset %q, got nil", tc.preset)
				}
				if !strings.Contains(err.Error(), tc.preset) && tc.preset != "" {
					t.Errorf("error message should contain preset name %q", tc.preset)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if preset.Name != tc.preset {
				t.Errorf("Name() = %q, want %q", preset.Name, tc.preset)
			}
			if preset.LeadAgent == "" {
				t.Error("LeadAgent should not be empty")
			}
			if len(preset.Roster) == 0 {
				t.Error("Roster should not be empty")
			}
			if preset.MaxConcurrent <= 0 {
				t.Error("MaxConcurrent should be > 0")
			}
			if preset.PromptTemplate == "" {
				t.Error("PromptTemplate should not be empty")
			}
		})
	}
}

func TestListTeamPresets(t *testing.T) {
	presets := ListTeamPresets()

	if len(presets) != 2 {
		t.Errorf("ListTeamPresets() returned %d presets, want 2", len(presets))
	}

	names := make(map[string]bool)
	for _, p := range presets {
		names[p.Name] = true
		if p.Description == "" {
			t.Errorf("preset %q has empty description", p.Name)
		}
	}

	for _, expected := range []string{"hyperplan", "security_research"} {
		if !names[expected] {
			t.Errorf("expected preset %q not found in ListTeamPresets()", expected)
		}
	}
}

func TestApplyPreset_Hyperplan(t *testing.T) {
	cfg, err := ApplyPreset("hyperplan", "Review the authentication module design")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LeadAgent != "planner" {
		t.Errorf("LeadAgent = %q, want %q", cfg.LeadAgent, "planner")
	}

	if len(cfg.Roster) != 5 {
		t.Errorf("Roster len = %d, want 5", len(cfg.Roster))
	}

	expectedRoster := []string{"analyst", "coder", "debugger", "planner", "analyst"}
	for i, agentID := range expectedRoster {
		if cfg.Roster[i] != agentID {
			t.Errorf("Roster[%d] = %q, want %q", i, cfg.Roster[i], agentID)
		}
	}

	if cfg.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want 5", cfg.MaxConcurrent)
	}

	if cfg.MemberTimeout <= 0 {
		t.Error("MemberTimeout should be > 0")
	}

	// Verify roster is a copy (not shared reference)
	original, _ := GetTeamPreset("hyperplan")
	cfg.Roster[0] = "modified"
	if original.Roster[0] == "modified" {
		t.Error("ApplyPreset should return a copy of the roster, not a reference to the original")
	}
}

func TestApplyPreset_SecurityResearch(t *testing.T) {
	cfg, err := ApplyPreset("security_research", "Audit the login API endpoint")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LeadAgent != "analyst" {
		t.Errorf("LeadAgent = %q, want %q", cfg.LeadAgent, "analyst")
	}

	if len(cfg.Roster) != 5 {
		t.Errorf("Roster len = %d, want 5", len(cfg.Roster))
	}

	// 3 hunters (coder) + 2 PoC engineers (debugger)
	coderCount := 0
	debuggerCount := 0
	for _, agentID := range cfg.Roster {
		switch agentID {
		case "coder":
			coderCount++
		case "debugger":
			debuggerCount++
		}
	}
	if coderCount != 3 {
		t.Errorf("expected 3 coders (hunters), got %d", coderCount)
	}
	if debuggerCount != 2 {
		t.Errorf("expected 2 debuggers (PoC engineers), got %d", debuggerCount)
	}

	if cfg.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want 5", cfg.MaxConcurrent)
	}
}

func TestApplyPreset_UnknownPreset(t *testing.T) {
	_, err := ApplyPreset("nonexistent", "some task")
	if err == nil {
		t.Fatal("expected error for unknown preset")
	}
}

func TestApplyPreset_PreservesDefaults(t *testing.T) {
	cfg, err := ApplyPreset("hyperplan", "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MemberTimeout should be set to default 5 minutes
	if cfg.MemberTimeout != 5*time.Minute {
		t.Errorf("MemberTimeout = %v, want %v", cfg.MemberTimeout, 5*time.Minute)
	}
}

func TestPresetPrompt(t *testing.T) {
	tests := []struct {
		name     string
		preset   string
		task     string
		wantErr  bool
		contains []string
	}{
		{
			name:     "hyperplan prompt contains task",
			preset:   "hyperplan",
			task:     "Review auth module",
			wantErr:  false,
			contains: []string{"Review auth module", "Hyperplan", "Multi-Perspective Plan Review"},
		},
		{
			name:     "security_research prompt contains task",
			preset:   "security_research",
			task:     "Audit login API",
			wantErr:  false,
			contains: []string{"Audit login API", "Security Research Team", "Hunter", "PoC Engineer"},
		},
		{
			name:    "unknown preset",
			preset:  "nonexistent",
			task:    "some task",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prompt, err := PresetPrompt(tc.preset, tc.task)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, substr := range tc.contains {
				if !strings.Contains(prompt, substr) {
					t.Errorf("prompt should contain %q", substr)
				}
			}
		})
	}
}
