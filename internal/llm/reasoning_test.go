package llm

import (
	"testing"
)

func TestReasoningConfig_IsZero(t *testing.T) {
	tests := []struct {
		name string
		rc   *ReasoningConfig
		want bool
	}{
		{"nil", nil, true},
		{"empty struct", &ReasoningConfig{}, true},
		{"effort only", &ReasoningConfig{Effort: "high"}, false},
		{"budget only", &ReasoningConfig{BudgetTokens: intPtr(8000)}, false},
		{"force only", &ReasoningConfig{Force: true}, false},
		{"enabled false", &ReasoningConfig{Enabled: boolPtr(false)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rc.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReasoningConfig_ResolveEnabled(t *testing.T) {
	tests := []struct {
		name string
		rc   *ReasoningConfig
		want bool
	}{
		{"nil", nil, false},
		{"empty", &ReasoningConfig{}, false},
		{"none", &ReasoningConfig{Effort: "none"}, false},
		{"low", &ReasoningConfig{Effort: "low"}, true},
		{"high", &ReasoningConfig{Effort: "high"}, true},
		{"max", &ReasoningConfig{Effort: "max"}, true},
		{"enabled true overrides effort none", &ReasoningConfig{Effort: "none", Enabled: boolPtr(true)}, true},
		{"enabled false overrides effort high", &ReasoningConfig{Effort: "high", Enabled: boolPtr(false)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rc.ResolveEnabled(); got != tt.want {
				t.Errorf("ResolveEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReasoningConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		rc      *ReasoningConfig
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", &ReasoningConfig{}, false},
		{"valid effort", &ReasoningConfig{Effort: "high"}, false},
		{"enabled false + effort none", &ReasoningConfig{Effort: "none", Enabled: boolPtr(false)}, false},
		{"enabled false + effort high (conflict)", &ReasoningConfig{Effort: "high", Enabled: boolPtr(false)}, true},
		{"invalid effort", &ReasoningConfig{Effort: "turbo"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rc.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAgentReasoningConfig_ClampEffort(t *testing.T) {
	tests := []struct {
		name     string
		agent    *AgentReasoningConfig
		effort   string
		want     string
	}{
		{"nil agent", nil, "high", "high"},
		{"no bounds", &AgentReasoningConfig{}, "high", "high"},
		{"empty effort, no bounds -> default medium", &AgentReasoningConfig{}, "", "medium"},
		{"clamped to min", &AgentReasoningConfig{MinEffort: "medium"}, "low", "medium"},
		{"clamped to max", &AgentReasoningConfig{MaxEffort: "medium"}, "xhigh", "medium"},
		{"within bounds", &AgentReasoningConfig{MinEffort: "low", MaxEffort: "high"}, "medium", "medium"},
		{"empty effort with min set", &AgentReasoningConfig{MinEffort: "high"}, "", "high"},
		{"empty effort with min and max", &AgentReasoningConfig{MinEffort: "low", MaxEffort: "high"}, "", "low"},
		{"effort at min boundary", &AgentReasoningConfig{MinEffort: "low"}, "low", "low"},
		{"effort at max boundary", &AgentReasoningConfig{MaxEffort: "high"}, "high", "high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.agent.ClampEffort(tt.effort)
			if got != tt.want {
				t.Errorf("ClampEffort(%q) = %q, want %q", tt.effort, got, tt.want)
			}
		})
	}
}

func TestAgentReasoningConfig_ToReasoningConfig(t *testing.T) {
	t.Run("nil agent + empty effort", func(t *testing.T) {
		var a *AgentReasoningConfig
		if rc := a.ToReasoningConfig(""); rc != nil {
			t.Errorf("expected nil, got %+v", rc)
		}
	})
	t.Run("nil agent + effort", func(t *testing.T) {
		var a *AgentReasoningConfig
		rc := a.ToReasoningConfig("high")
		if rc == nil || rc.Effort != "high" {
			t.Errorf("expected effort=high, got %+v", rc)
		}
	})
	t.Run("full agent config", func(t *testing.T) {
		budget := 12000
		a := &AgentReasoningConfig{
			Effort:       "medium",
			BudgetTokens: &budget,
			Force:        true,
		}
		rc := a.ToReasoningConfig("high")
		if rc == nil {
			t.Fatal("expected non-nil")
		}
		if rc.Effort != "high" {
			t.Errorf("Effort = %q, want high", rc.Effort)
		}
		if rc.BudgetTokens == nil || *rc.BudgetTokens != 12000 {
			t.Errorf("BudgetTokens = %v, want 12000", rc.BudgetTokens)
		}
		if !rc.Force {
			t.Error("Force should be true")
		}
		if rc.Enabled == nil || !*rc.Enabled {
			t.Error("Enabled should be true for non-none effort")
		}
	})
	t.Run("none effort produces enabled=false", func(t *testing.T) {
		a := &AgentReasoningConfig{Effort: "none"}
		rc := a.ToReasoningConfig("none")
		if rc == nil {
			t.Fatal("expected non-nil for explicit none")
		}
		if rc.Enabled != nil && *rc.Enabled {
			t.Error("Enabled should be false or nil for none")
		}
	})
}

func TestResolveReasoning(t *testing.T) {
	perRequest := &ReasoningConfig{Effort: "xhigh"}
	agentSpec := &ReasoningConfig{Effort: "medium"}
	modelDefault := &ReasoningConfig{Effort: "low"}

	t.Run("per-request wins", func(t *testing.T) {
		rc := ResolveReasoning(perRequest, agentSpec, modelDefault)
		if rc.Effort != "xhigh" {
			t.Errorf("got %q, want xhigh", rc.Effort)
		}
	})
	t.Run("agent wins without per-request", func(t *testing.T) {
		rc := ResolveReasoning(nil, agentSpec, modelDefault)
		if rc.Effort != "medium" {
			t.Errorf("got %q, want medium", rc.Effort)
		}
	})
	t.Run("model default when no agent", func(t *testing.T) {
		rc := ResolveReasoning(nil, nil, modelDefault)
		if rc.Effort != "low" {
			t.Errorf("got %q, want low", rc.Effort)
		}
	})
	t.Run("nil when all empty", func(t *testing.T) {
		rc := ResolveReasoning(nil, nil, nil)
		if rc != nil {
			t.Errorf("expected nil, got %+v", rc)
		}
	})
	t.Run("zero per-request skips to agent", func(t *testing.T) {
		rc := ResolveReasoning(&ReasoningConfig{}, agentSpec, nil)
		if rc.Effort != "medium" {
			t.Errorf("got %q, want medium", rc.Effort)
		}
	})
}

func TestResolveBudget(t *testing.T) {
	budget8k := 8000
	budget16k := 16000

	t.Run("nil rc returns nil", func(t *testing.T) {
		if b := ResolveBudget(nil, nil, nil, nil); b != nil {
			t.Errorf("expected nil")
		}
	})
	t.Run("per-request budget wins", func(t *testing.T) {
		rc := &ReasoningConfig{Effort: "high", BudgetTokens: &budget8k}
		agent := &AgentReasoningConfig{BudgetTokens: &budget16k}
		b := ResolveBudget(rc, agent, nil, nil)
		if b == nil || *b != 8000 {
			t.Errorf("got %v, want 8000", b)
		}
	})
	t.Run("agent budget when no per-request budget", func(t *testing.T) {
		rc := &ReasoningConfig{Effort: "high"}
		agent := &AgentReasoningConfig{BudgetTokens: &budget16k}
		b := ResolveBudget(rc, agent, nil, nil)
		if b == nil || *b != 16000 {
			t.Errorf("got %v, want 16000", b)
		}
	})
	t.Run("global budgets map", func(t *testing.T) {
		rc := &ReasoningConfig{Effort: "high"}
		global := map[string]int{"high": 20000}
		b := ResolveBudget(rc, nil, nil, global)
		if b == nil || *b != 20000 {
			t.Errorf("got %v, want 20000", b)
		}
	})
	t.Run("hardcoded default when no config", func(t *testing.T) {
		rc := &ReasoningConfig{Effort: "high"}
		b := ResolveBudget(rc, nil, nil, nil)
		if b == nil || *b != 16000 {
			t.Errorf("got %v, want 16000", b)
		}
	})
	t.Run("none effort returns nil budget", func(t *testing.T) {
		rc := &ReasoningConfig{Effort: "none"}
		b := ResolveBudget(rc, nil, nil, nil)
		if b != nil {
			t.Errorf("expected nil for none effort")
		}
	})
}

func TestIsValidEffort(t *testing.T) {
	valid := []string{"", "none", "low", "medium", "high", "xhigh", "max"}
	invalid := []string{"turbo", "OFF", "Medium", "high ", "1"}

	for _, e := range valid {
		if !IsValidEffort(e) {
			t.Errorf("IsValidEffort(%q) = false, want true", e)
		}
	}
	for _, e := range invalid {
		if IsValidEffort(e) {
			t.Errorf("IsValidEffort(%q) = true, want false", e)
		}
	}
}

func TestDefaultBudgetTable(t *testing.T) {
	t1 := DefaultBudgetTable()
	t2 := DefaultBudgetTable()
	t1["low"] = 999
	if t2["low"] == 999 {
		t.Error("DefaultBudgetTable() should return independent copies")
	}
}

// Helpers
func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }
