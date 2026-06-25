package employee

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

func TestAutonomyTier_String(t *testing.T) {
	tests := []struct {
		name string
		tier AutonomyTier
		want string
	}{
		{"tier_1_reactive", Tier1Reactive, "tier_1_reactive"},
		{"tier_2_propose", Tier2Propose, "tier_2_propose"},
		{"tier_3_autonomous", Tier3Autonomous, "tier_3_autonomous"},
		{"unknown tier clamps", AutonomyTier(99), "tier_unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tier.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAutonomyTier_JSONRoundTrip(t *testing.T) {
	tests := []AutonomyTier{Tier1Reactive, Tier2Propose, Tier3Autonomous}
	for _, tier := range tests {
		t.Run(tier.String(), func(t *testing.T) {
			b, err := tier.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}
			var got AutonomyTier
			if err := got.UnmarshalJSON(b); err != nil {
				t.Fatalf("UnmarshalJSON: %v", err)
			}
			if got != tier {
				t.Errorf("round-trip mismatch: got %v, want %v", got, tier)
			}
		})
	}
}

func TestAutonomyTier_UnmarshalJSON_Unknown(t *testing.T) {
	var tier AutonomyTier
	err := tier.UnmarshalJSON([]byte(`"tier_9_nonexistent"`))
	if err == nil {
		t.Fatal("expected error for unknown tier, got nil")
	}
	// Unknown tier falls back to Tier1Reactive (conservative default).
	if tier != Tier1Reactive {
		t.Errorf("expected fallback to Tier1Reactive, got %v", tier)
	}
}

func TestConstitution_Validate(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*Constitution)
		selfID      string
		wantErr     bool
		errContains string
	}{
		{
			name:   "valid constitution",
			mutate: func(*Constitution) {},
			selfID: "emp-a",
			wantErr: false,
		},
		{
			name: "direct self-escalation",
			mutate: func(c *Constitution) {
				c.EscalatesTo = []string{"emp-a", "user"}
			},
			selfID:      "emp-a",
			wantErr:     true,
			errContains: "direct self-escalation",
		},
		{
			name: "self-escalation skipped when selfID empty",
			mutate: func(c *Constitution) {
				c.EscalatesTo = []string{"anyone"}
			},
			selfID:  "",
			wantErr: false,
		},
		{
			name: "unknown risk ceiling",
			mutate: func(c *Constitution) {
				c.Constraints.RiskCeiling = RiskLevelCeiling("extreme")
			},
			selfID:      "emp-a",
			wantErr:     true,
			errContains: "risk_ceiling",
		},
		{
			name: "unknown escalation trigger on",
			mutate: func(c *Constitution) {
				c.Constraints.EscalationTriggers = []EscalationTrigger{
					{On: EscalationOn("bogus"), Match: "x"},
				}
			},
			selfID:      "emp-a",
			wantErr:     true,
			errContains: "escalation_triggers[0].on",
		},
		{
			name: "escalation trigger missing match",
			mutate: func(c *Constitution) {
				c.Constraints.EscalationTriggers = []EscalationTrigger{
					{On: EscalateOnRiskLevel, Match: ""},
				}
			},
			selfID:      "emp-a",
			wantErr:     true,
			errContains: "escalation_triggers[0].match",
		},
		{
			name: "requires_approval false rejected",
			mutate: func(c *Constitution) {
				c.AmendmentPolicy.RequiresApproval = false
			},
			selfID:      "emp-a",
			wantErr:     true,
			errContains: "requires_approval",
		},
		{
			name: "unknown frozen field top-level",
			mutate: func(c *Constitution) {
				c.AmendmentPolicy.FrozenFields = []string{"nonexistent_field"}
			},
			selfID:      "emp-a",
			wantErr:     true,
			errContains: "frozen_fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConstitution()
			tt.mutate(&c)
			err := c.Validate(tt.selfID)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errContains)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if tt.wantErr && tt.errContains != "" && !contains(err.Error(), tt.errContains) {
				t.Errorf("error %q does not contain expected substring %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestConstitution_ValidateFrozenFields(t *testing.T) {
	tests := []struct {
		name    string
		fields  []string
		wantBad int
	}{
		{"empty", nil, 0},
		{"valid top-level", []string{"purpose", "autonomy_tier", "escalates_to"}, 0},
		{"valid constraints subfield", []string{"constraints.risk_ceiling", "constraints.never"}, 0},
		{"case-insensitive", []string{"Purpose", "CONSTRAINTS.Risk_Ceiling"}, 0},
		{"duplicates deduped", []string{"purpose", "purpose", "Purpose"}, 0},
		{"whitespace trimmed", []string{"  purpose  "}, 0},
		{"unknown top-level", []string{"nonexistent"}, 1},
		{"unknown constraints subfield", []string{"constraints.bogus"}, 1},
		{"empty entry", []string{""}, 1},
		{"mixed valid and invalid", []string{"purpose", "bogus", "constraints.never", "constraints.bogus"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Constitution{
				AmendmentPolicy: AmendmentPolicy{FrozenFields: tt.fields},
			}
			bad := c.ValidateFrozenFields()
			if len(bad) != tt.wantBad {
				t.Errorf("got %d bad entries %v, want %d", len(bad), bad, tt.wantBad)
			}
		})
	}
}

func TestConstitution_CheckEscalationReferences(t *testing.T) {
	tests := []struct {
		name          string
		escalatesTo   []string
		knownIDs      map[string]struct{}
		wantUnknown   int
		wantErr       bool
	}{
		{
			name:        "all known",
			escalatesTo: []string{"emp-b", "user"},
			knownIDs:    map[string]struct{}{"emp-b": {}},
			wantUnknown: 0,
			wantErr:     false,
		},
		{
			name:        "legacy user accepted",
			escalatesTo: []string{"user"},
			knownIDs:    map[string]struct{}{},
			wantUnknown: 0,
			wantErr:     false,
		},
		{
			name:        "role-prefixed user accepted",
			escalatesTo: []string{"role:user"},
			knownIDs:    map[string]struct{}{},
			wantUnknown: 0,
			wantErr:     false,
		},
		{
			name:        "role-prefixed oncall accepted",
			escalatesTo: []string{"role:oncall"},
			knownIDs:    map[string]struct{}{},
			wantUnknown: 0,
			wantErr:     false,
		},
		{
			name:        "legacy system accepted",
			escalatesTo: []string{"system"},
			knownIDs:    map[string]struct{}{},
			wantUnknown: 0,
			wantErr:     false,
		},
		{
			name:        "one unknown",
			escalatesTo: []string{"emp-b", "emp-ghost"},
			knownIDs:    map[string]struct{}{"emp-b": {}},
			wantUnknown: 1,
			wantErr:     true,
		},
		{
			name:        "multiple unknown sorted",
			escalatesTo: []string{"z-bad", "a-bad", "user"},
			knownIDs:    map[string]struct{}{},
			wantUnknown: 2,
			wantErr:     true,
		},
		{
			name:        "empty escalates_to accepted",
			escalatesTo: nil,
			knownIDs:    map[string]struct{}{"emp-b": {}},
			wantUnknown: 0,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Constitution{EscalatesTo: tt.escalatesTo}
			unknown, err := c.CheckEscalationReferences(tt.knownIDs)
			if len(unknown) != tt.wantUnknown {
				t.Errorf("got %d unknown IDs %v, want %d", len(unknown), unknown, tt.wantUnknown)
			}
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestConstitution_CheckToolReferences(t *testing.T) {
	known := map[string]struct{}{
		"web_fetch":     {},
		"shell_execute": {},
		"memory_store":  {},
	}

	tests := []struct {
		name             string
		allowed          []string
		forbidden        []string
		wantUnknownA     int
		wantUnknownF     int
	}{
		{
			name:         "all known",
			allowed:      []string{"web_fetch"},
			forbidden:    []string{"shell_execute"},
			wantUnknownA: 0,
			wantUnknownF: 0,
		},
		{
			name:         "unknown in allowed",
			allowed:      []string{"web_fetch", "ghost_tool"},
			forbidden:    nil,
			wantUnknownA: 1,
			wantUnknownF: 0,
		},
		{
			name:         "unknown in forbidden",
			allowed:      nil,
			forbidden:    []string{"ghost_tool"},
			wantUnknownA: 0,
			wantUnknownF: 1,
		},
		{
			name:         "empty lists",
			allowed:      nil,
			forbidden:    nil,
			wantUnknownA: 0,
			wantUnknownF: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Constitution{
				Constraints: ConstitutionalConstraints{
					ToolsAllowed:   tt.allowed,
					ToolsForbidden: tt.forbidden,
				},
			}
			unknownA, unknownF := c.CheckToolReferences(known)
			if len(unknownA) != tt.wantUnknownA {
				t.Errorf("unknown allowed: got %d %v, want %d", len(unknownA), unknownA, tt.wantUnknownA)
			}
			if len(unknownF) != tt.wantUnknownF {
				t.Errorf("unknown forbidden: got %d %v, want %d", len(unknownF), unknownF, tt.wantUnknownF)
			}
		})
	}
}

func TestConstitution_CheckToolReferences_NilMap(t *testing.T) {
	c := Constitution{
		Constraints: ConstitutionalConstraints{
			ToolsAllowed: []string{"anything"},
		},
	}
	unknownA, unknownF := c.CheckToolReferences(nil)
	if unknownA != nil || unknownF != nil {
		t.Errorf("nil knownTools should produce empty results, got %v / %v", unknownA, unknownF)
	}
}

func TestEmployee_Validate(t *testing.T) {
	// Employee.Validate combines BotDefinition.Validate and
	// Constitution.Validate; we test the integration here, not the
	// individual validators (covered above).
	t.Run("valid employee", func(t *testing.T) {
		e := Employee{
			BotDefinition: validBotDefinition(),
			Constitution:  validConstitution(),
		}
		if err := e.Validate(); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("invalid bot definition surfaces", func(t *testing.T) {
		e := Employee{
			BotDefinition: validBotDefinition(),
			Constitution:  validConstitution(),
		}
		e.ID = "" // invalid bot
		err := e.Validate()
		if err == nil {
			t.Fatal("expected error for invalid bot definition")
		}
		if !contains(err.Error(), "bot definition") {
			t.Errorf("error should mention bot definition: %v", err)
		}
	})

	t.Run("invalid constitution surfaces", func(t *testing.T) {
		e := Employee{
			BotDefinition: validBotDefinition(),
			Constitution:  validConstitution(),
		}
		e.Constitution.AmendmentPolicy.RequiresApproval = false
		err := e.Validate()
		if err == nil {
			t.Fatal("expected error for invalid constitution")
		}
		if !contains(err.Error(), "constitution") {
			t.Errorf("error should mention constitution: %v", err)
		}
	})
}

func TestEmployee_HasConstitution(t *testing.T) {
	t.Run("populated constitution", func(t *testing.T) {
		e := Employee{
			BotDefinition: validBotDefinition(),
			Constitution:  validConstitution(),
		}
		if !e.HasConstitution() {
			t.Error("expected HasConstitution=true for populated constitution")
		}
	})
	t.Run("empty constitution", func(t *testing.T) {
		e := Employee{BotDefinition: validBotDefinition()}
		if e.HasConstitution() {
			t.Error("expected HasConstitution=false for empty constitution")
		}
	})
}

// --- helpers ---

// validConstitution returns a Constitution that passes Validate().
func validConstitution() Constitution {
	return Constitution{
		Purpose:      "monitor CI status and surface failures",
		Role:         "CI Reliability Engineer",
		Charter:      "Keep CI green. Never merge to main.",
		AutonomyTier: Tier2Propose,
		EscalatesTo:  []string{"user"},
		Constraints: ConstitutionalConstraints{
			ToolsAllowed:   []string{"web_fetch", "shell_execute"},
			ToolsForbidden: []string{"git_push"},
			RiskCeiling:    RiskCeilingMedium,
			Never:          []string{"merge to main", "force push"},
		},
		AmendmentPolicy: AmendmentPolicy{
			SelfProposeAllowed: false,
			RequiresApproval:   true,
			FrozenFields:       []string{"constraints.risk_ceiling", "constraints.never"},
		},
		Version:    1,
		AuthoredBy: "user",
		ApprovedAt: time.Now(),
	}
}

// validBotDefinition returns a bot.BotDefinition that passes its
// Validate(). Kept minimal — the employee package tests care about
// the Constitution layer, not the bot layer's full validation matrix.
func validBotDefinition() bot.BotDefinition {
	return bot.BotDefinition{
		ID:      "ci-monitor",
		Name:    "CI Monitor",
		Prompt:  "check CI status",
		Enabled: true,
		Triggers: []bot.BotTrigger{
			{Type: bot.TriggerTypeWebhook, Enabled: true},
		},
	}
}

// contains is a small alias for strings.Contains so test cases read
// as "does this error mention this substring" without repeated
// strings.Contains boilerplate.
func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
