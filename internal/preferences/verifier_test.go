package preferences

import (
	"testing"
)

func TestVerifier_ValidInstruction(t *testing.T) {
	v := NewInstructionVerifier(nil)

	instr := &ParsedInstruction{
		Trigger: TriggerConfig{
			Type:    "cron",
			Pattern: "0 9 * * *",
		},
		Action: ActionConfig{
			Tool: "memory_retain",
		},
		Scope:    "global",
		Priority: "normal",
	}

	result := v.Verify(instr)
	if !result.Valid {
		t.Errorf("Verify() valid = false, want true. Errors: %v", result.Errors)
	}
}

func TestVerifier_NilInstruction(t *testing.T) {
	v := NewInstructionVerifier(nil)

	result := v.Verify(nil)
	if result.Valid {
		t.Error("Verify(nil) valid = true, want false")
	}
	if len(result.Errors) == 0 {
		t.Error("Verify(nil) errors empty, want error message")
	}
}

func TestVerifier_EmptyAction(t *testing.T) {
	v := NewInstructionVerifier(nil)

	instr := &ParsedInstruction{
		Trigger: TriggerConfig{Type: "cron"},
		Action:  ActionConfig{Tool: ""},
	}

	result := v.Verify(instr)
	if result.Valid {
		t.Error("Verify() with empty action valid = true, want false")
	}
}

func TestVerifier_ToolRiskLevels(t *testing.T) {
	v := NewInstructionVerifier(nil)

	tests := []struct {
		name     string
		tool     string
		wantRisk string
	}{
		{"memory", "memory_retain", "low"},
		{"agent trigger", "agent_trigger", "medium"},
		{"notification", "notification", "low"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instr := &ParsedInstruction{
				Trigger: TriggerConfig{Type: "post_hook"},
				Action:  ActionConfig{Tool: tt.tool},
			}
			result := v.Verify(instr)
			if result.RiskLevel != tt.wantRisk {
				t.Errorf("Verify(%q) risk = %v, want %v", tt.tool, result.RiskLevel, tt.wantRisk)
			}
		})
	}
}
