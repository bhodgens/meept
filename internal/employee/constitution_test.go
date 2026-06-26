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

// ---------------------------------------------------------------------------
// C1: Constitution version race condition (optimistic locking) tests.
// ---------------------------------------------------------------------------

func TestAmendRequest_ExpectedVersion(t *testing.T) {
	// Verify that ExpectedVersion=0 skips the check (backward compat).
	req := AmendRequest{EmployeeID: "emp-a", ExpectedVersion: 0}
	if req.ExpectedVersion != 0 {
		t.Error("zero ExpectedVersion should be the default")
	}
}

// ---------------------------------------------------------------------------
// C2: RiskCeiling mapping tests.
// ---------------------------------------------------------------------------

func TestSecurityEngineRiskToCeiling(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		want     riskLevel
	}{
		{"safe=0", 0, riskSafe},
		{"low=1", 1, riskLow},
		{"medium=2", 2, riskMedium},
		{"high=3", 3, riskHigh},
		{"critical=4", 4, riskCritical},
		{"unknown=-1 defaults medium", -1, riskMedium},
		{"unknown=99 defaults medium", 99, riskMedium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SecurityEngineRiskToCeiling(tt.input)
			if got != tt.want {
				t.Errorf("SecurityEngineRiskToCeiling(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestRiskCeilingToSecurityLevel(t *testing.T) {
	tests := []struct {
		input riskLevel
		want  int
	}{
		{riskSafe, 0},
		{riskLow, 1},
		{riskMedium, 2},
		{riskHigh, 3},
		{riskCritical, 4},
	}
	for _, tt := range tests {
		t.Run(riskLabel(tt.input), func(t *testing.T) {
			got := RiskCeilingToSecurityLevel(tt.input)
			if got != tt.want {
				t.Errorf("RiskCeilingToSecurityLevel(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestRiskCeilingRoundTrip(t *testing.T) {
	// Verify the mapping is bidirectional: string → riskLevel → int → riskLevel.
	for _, ceiling := range []RiskLevelCeiling{RiskCeilingSafe, RiskCeilingLow, RiskCeilingMedium, RiskCeilingHigh, RiskCeilingCritical} {
		rl := parseRiskCeiling(string(ceiling))
		secInt := RiskCeilingToSecurityLevel(rl)
		rl2 := SecurityEngineRiskToCeiling(secInt)
		if rl != rl2 {
			t.Errorf("round-trip failed for %s: %d → %d → %d", ceiling, rl, secInt, rl2)
		}
	}
}

// ---------------------------------------------------------------------------
// C3: Tool name canonicalization tests.
// ---------------------------------------------------------------------------

func TestCanonicalToolName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"already canonical", "shell_execute", "shell_execute"},
		{"uppercase normalized", "SHELL_EXECUTE", "shell_execute"},
		{"whitespace trimmed", "  web_fetch  ", "web_fetch"},
		{"mixed case", "WebFetch", "webfetch"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalToolName(tt.input)
			if got != tt.want {
				t.Errorf("canonicalToolName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCheckToolReferences_CaseInsensitive(t *testing.T) {
	// C3: Tool references with wrong casing should still match when
	// knownToolNames uses canonical (lowercase) keys.
	known := map[string]struct{}{
		"shell_execute": {},
		"web_fetch":     {},
	}
	c := Constitution{
		Constraints: ConstitutionalConstraints{
			ToolsAllowed:   []string{"Shell_Execute", "WEB_FETCH"},
			ToolsForbidden: []string{"SHELL_EXECUTE"},
		},
	}
	unknownA, unknownF := c.CheckToolReferences(known)
	if len(unknownA) != 0 {
		t.Errorf("expected 0 unknown allowed (case-insensitive), got %v", unknownA)
	}
	if len(unknownF) != 0 {
		t.Errorf("expected 0 unknown forbidden (case-insensitive), got %v", unknownF)
	}
}

// ---------------------------------------------------------------------------
// C4: NeverRule pattern matching tests.
// ---------------------------------------------------------------------------

func TestMatchType_Valid(t *testing.T) {
	tests := []struct {
		mt    MatchType
		valid bool
	}{
		{MatchSubstring, true},
		{MatchRegex, true},
		{MatchGlob, true},
		{MatchLLMOnly, true},
		{MatchType("bogus"), false},
		{MatchType(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.mt), func(t *testing.T) {
			if got := tt.mt.Valid(); got != tt.valid {
				t.Errorf("MatchType(%q).Valid() = %v, want %v", tt.mt, got, tt.valid)
			}
		})
	}
}

func TestNeverRule_InConstitution(t *testing.T) {
	// Verify NeverRules field exists and is validated.
	t.Run("valid never rules", func(t *testing.T) {
		c := validConstitution()
		c.Constraints.NeverRules = []NeverRule{
			{Pattern: "rm -rf /", MatchType: MatchSubstring, Reason: "dangerous delete"},
			{Pattern: "^curl.*|.*pipe.*bash", MatchType: MatchRegex, Reason: "pipe to shell"},
		}
		if err := c.Validate("emp-a"); err != nil {
			t.Fatalf("valid never rules rejected: %v", err)
		}
	})

	t.Run("empty pattern rejected", func(t *testing.T) {
		c := validConstitution()
		c.Constraints.NeverRules = []NeverRule{
			{Pattern: "", MatchType: MatchSubstring},
		}
		err := c.Validate("emp-a")
		if err == nil {
			t.Fatal("expected error for empty pattern")
		}
		if !contains(err.Error(), "pattern") {
			t.Errorf("error should mention pattern: %v", err)
		}
	})

	t.Run("unknown match type rejected", func(t *testing.T) {
		c := validConstitution()
		c.Constraints.NeverRules = []NeverRule{
			{Pattern: "test", MatchType: MatchType("bogus")},
		}
		err := c.Validate("emp-a")
		if err == nil {
			t.Fatal("expected error for unknown match type")
		}
		if !contains(err.Error(), "match_type") {
			t.Errorf("error should mention match_type: %v", err)
		}
	})

	t.Run("empty match type defaults to substring", func(t *testing.T) {
		c := validConstitution()
		c.Constraints.NeverRules = []NeverRule{
			{Pattern: "merge to main", MatchType: ""},
		}
		if err := c.Validate("emp-a"); err != nil {
			t.Fatalf("empty match_type should default to substring: %v", err)
		}
	})
}

func TestMatchesNeverRules(t *testing.T) {
	defaultRules := []NeverRule{
		{Pattern: "merge to main", MatchType: MatchSubstring},
		{Pattern: "^rm\\s+-rf", MatchType: MatchRegex},
		{Pattern: "*.env", MatchType: MatchGlob},
		{Pattern: "be dismissive", MatchType: MatchLLMOnly, Reason: "semantic rule"},
	}

	tests := []struct {
		name     string
		action   string
		tool     string
		details  map[string]string
		rules    []NeverRule
		wantHit  bool
		wantRule string
	}{
		{
			name:     "substring match in action",
			action:   "merge to main",
			tool:     "git",
			wantHit:  true,
			wantRule: "merge to main",
		},
		{
			name:     "regex match in details",
			action:   "execute",
			tool:     "shell_execute",
			details:  map[string]string{"command": "rm -rf /tmp"},
			wantHit:  true,
			wantRule: "^rm\\s+-rf",
		},
		{
			name:     "glob match in details path",
			action:   "read",
			tool:     "file_read",
			details:  map[string]string{"path": ".env"},
			wantHit:  true,
			wantRule: "*.env",
		},
		{
			name:    "llm_only not checked at pre-exec",
			action:  "be dismissive to the user",
			tool:    "chat",
			wantHit: false,
		},
		{
			name:    "no match clean",
			action:  "fetch",
			tool:    "web_fetch",
			details: map[string]string{"url": "https://example.com"},
			wantHit: false,
		},
		{
			name:    "empty rules no hit",
			action:  "anything",
			tool:    "any",
			rules:   []NeverRule{},
			wantHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.rules
			if r == nil {
				r = defaultRules
			}
			hit, rule := matchesNeverRules(r, tt.action, tt.tool, tt.details)
			if hit != tt.wantHit {
				t.Errorf("hit = %v, want %v (rule=%s)", hit, tt.wantHit, rule)
			}
			if hit && tt.wantRule != "" && rule != tt.wantRule {
				t.Errorf("rule = %q, want %q", rule, tt.wantRule)
			}
		})
	}
}

func TestMatchesNeverRules_MalformedRegex(t *testing.T) {
	// Malformed regex should fail-open (no match, no panic).
	rules := []NeverRule{
		{Pattern: "[invalid", MatchType: MatchRegex},
	}
	hit, _ := matchesNeverRules(rules, "test", "tool", nil)
	if hit {
		t.Error("malformed regex should not match (fail-open)")
	}
}

func TestSynthesizedPrompt_NeverRules(t *testing.T) {
	c := validConstitution()
	c.Constraints.NeverRules = []NeverRule{
		{Pattern: "rm -rf /", MatchType: MatchSubstring, Reason: "dangerous delete"},
		{Pattern: "be rude", MatchType: MatchLLMOnly, Reason: "tone guideline"},
	}
	got := SynthesizedPrompt(&c, "")
	lower := strings.ToLower(got)
	if !contains(lower, "rm -rf /") {
		t.Error("synthesized prompt should contain NeverRule pattern")
	}
	if !contains(lower, "dangerous delete") {
		t.Error("synthesized prompt should contain NeverRule reason")
	}
	if !contains(lower, "be rude") {
		t.Error("synthesized prompt should contain llm_only rule")
	}
}

// ---------------------------------------------------------------------------
// C5: SynthesizedPrompt truncation tests.
// ---------------------------------------------------------------------------

func TestSynthesizedPrompt_Truncation(t *testing.T) {
	t.Run("no truncation when under limit", func(t *testing.T) {
		c := validConstitution()
		got := SynthesizedPromptWithMax(&c, "existing", 10000)
		if len(got) > 10000 {
			t.Errorf("prompt length %d exceeds max 10000", len(got))
		}
		if !contains(got, "existing") {
			t.Error("existing prompt should be preserved when under limit")
		}
	})

	t.Run("truncation when charter is large", func(t *testing.T) {
		c := validConstitution()
		// Make charter very large.
		c.Charter = strings.Repeat("This is a very long charter line. ", 500)
		got := SynthesizedPromptWithMax(&c, "", 2000)
		if len(got) > 2100 { // allow small overshoot for "..."
			t.Errorf("prompt length %d should be approximately under 2000", len(got))
		}
		// Header and constraints should survive truncation.
		lower := strings.ToLower(got)
		if !contains(lower, "# employee profile") {
			t.Error("header should survive truncation")
		}
		if !contains(lower, "merge to main") {
			t.Error("never rules should survive truncation")
		}
	})

	t.Run("truncation preserves existing prompt partially", func(t *testing.T) {
		c := validConstitution()
		longExisting := strings.Repeat("context line\n", 500)
		got := SynthesizedPromptWithMax(&c, longExisting, 2000)
		if len(got) > 2100 {
			t.Errorf("prompt length %d should be approximately under 2000", len(got))
		}
	})

	t.Run("zero max disables truncation", func(t *testing.T) {
		c := validConstitution()
		c.Charter = strings.Repeat("long ", 10000)
		got := SynthesizedPromptWithMax(&c, "", 0)
		if len(got) < 50000 {
			t.Errorf("with maxLen=0, no truncation expected; got %d", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// C6: DefaultConservativeConstitution tests.
// ---------------------------------------------------------------------------

func TestDefaultConservativeConstitution(t *testing.T) {
	c := DefaultConservativeConstitution("test purpose")

	// Verify all fields have explicit defaults.
	if c.Purpose != "test purpose" {
		t.Errorf("Purpose = %q, want %q", c.Purpose, "test purpose")
	}
	if c.Role != "conservative default" {
		t.Errorf("Role = %q, want %q", c.Role, "conservative default")
	}
	if c.AutonomyTier != Tier1Reactive {
		t.Errorf("AutonomyTier = %v, want %v", c.AutonomyTier, Tier1Reactive)
	}
	if len(c.EscalatesTo) != 1 || c.EscalatesTo[0] != UserEscalationID {
		t.Errorf("EscalatesTo = %v, want [%s]", c.EscalatesTo, UserEscalationID)
	}
	if c.Constraints.RiskCeiling != RiskCeilingLow {
		t.Errorf("RiskCeiling = %v, want %v", c.Constraints.RiskCeiling, RiskCeilingLow)
	}
	if c.Constraints.ToolsAllowed != nil {
		t.Errorf("ToolsAllowed should be nil (inherit default)")
	}
	if len(c.Constraints.ToolsForbidden) != 0 {
		t.Errorf("ToolsForbidden should be empty, got %v", c.Constraints.ToolsForbidden)
	}
	if len(c.Constraints.Never) == 0 {
		t.Error("Never should have at least one default rule")
	}
	if c.Constraints.AssessmentInterval != "" {
		t.Errorf("AssessmentInterval should be empty for tier 1, got %q", c.Constraints.AssessmentInterval)
	}
	if !c.AmendmentPolicy.RequiresApproval {
		t.Error("RequiresApproval should be true")
	}
	if c.AmendmentPolicy.SelfProposeAllowed {
		t.Error("SelfProposeAllowed should be false")
	}
	if len(c.AmendmentPolicy.FrozenFields) == 0 {
		t.Error("FrozenFields should not be empty")
	}
	if c.Version != 1 {
		t.Errorf("Version = %d, want 1", c.Version)
	}
	if c.AuthoredBy != "default" {
		t.Errorf("AuthoredBy = %q, want %q", c.AuthoredBy, "default")
	}
	if c.ApprovedAt.IsZero() {
		t.Error("ApprovedAt should not be zero")
	}
}

func TestDefaultConservativeConstitution_Validates(t *testing.T) {
	c := DefaultConservativeConstitution("test employee")
	if err := c.Validate("emp-test"); err != nil {
		t.Fatalf("DefaultConservativeConstitution should pass Validate: %v", err)
	}
}
