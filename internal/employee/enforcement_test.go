package employee

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// ---------------------------------------------------------------------------
// Test fixtures: Constitution + helpers.
// ---------------------------------------------------------------------------

func testConstitution() *Constitution {
	return &Constitution{
		Purpose:  "keep CI green",
		Role:     "CI Reliability Engineer",
		Charter:  "investigate failures, open issues, never merge code",
		AutonomyTier: Tier2Propose,
		EscalatesTo: []string{"user"},
		Constraints: ConstitutionalConstraints{
			ToolsAllowed:   []string{"web_fetch", "shell_execute", "file_read"},
			ToolsForbidden: []string{"git_push", "file_delete"},
			RiskCeiling:    "medium",
			DailyBudgetCents:     50,
			MaxInvocationsPerDay: 100,
			MaxTokensPerTurn:     8000,
			EscalationTriggers: []EscalationTrigger{
				{On: EscalateOnTool, Match: "shell_execute", Reason: "shell requires approval"},
				{On: EscalateOnRiskLevel, Match: "critical", Reason: "critical risk needs signoff"},
			},
			Never: []string{"merge to main", "force push", "delete branches"},
		},
	}
}

// budgetStub is a controllable BudgetChecker for tests.
type budgetStub struct {
	tokens      int
	cents       int
	invocations int
}

func (b budgetStub) SpentToday(string) (int, int, int) {
	return b.tokens, b.cents, b.invocations
}

// pauseTracker records auto-pause calls for assertion.
type pauseTracker struct {
	called  bool
	empID   string
	reason  string
}

func (p *pauseTracker) fn() AutoPauseFunc {
	return func(employeeID, reason string) error {
		p.called = true
		p.empID = employeeID
		p.reason = reason
		return nil
	}
}

// ---------------------------------------------------------------------------
// Truth-table tests for PreExecChecker.Check (Checkpoint 1).
// ---------------------------------------------------------------------------

func TestPreExecChecker_Check(t *testing.T) {
	base := testConstitution()
	pause := &pauseTracker{}

	type tc struct {
		name      string
		action    string
		tool      string
		details   map[string]string
		constitution *Constitution
		budget    BudgetChecker
		wantAllow bool
		wantSev   string
		wantPlan  bool
		wantPause bool
		wantReason string // substring check
	}

	tests := []tc{
		// --- 1. tools_forbidden ---
		{
			name:        "forbidden tool denied",
			action:      "execute",
			tool:        "git_push",
			constitution: base,
			wantAllow:   false,
			wantSev:     "warning",
			wantReason:  "forbidden",
		},
		{
			name:        "forbidden tool file_delete denied",
			action:      "delete",
			tool:        "file_delete",
			constitution: base,
			wantAllow:   false,
			wantSev:     "warning",
		},

		// --- 2. tools_allowed ---
		{
			name:        "allowed tool web_fetch",
			action:      "fetch",
			tool:        "web_fetch",
			constitution: base,
			wantAllow:   true,
			wantSev:     "info",
		},
		{
			name:        "allowed tool shell_execute (but triggers escalation)",
			action:      "run",
			tool:        "shell_execute",
			constitution: base,
			wantAllow:   false,
			wantSev:     "",
			wantPlan:    true,
			wantReason:  "escalation",
		},
		{
			name:        "tool not in allowed list denied",
			action:      "search",
			tool:        "web_search",
			constitution: base,
			wantAllow:   false,
			wantSev:     "warning",
			wantReason:  "not in tools_allowed",
		},

		// --- 3. risk_ceiling ---
		{
			name:        "risk within ceiling allowed",
			action:      "read",
			tool:        "file_read",
			details:     map[string]string{"risk_level": "low"},
			constitution: base,
			wantAllow:   true,
			wantSev:     "info",
		},
		{
			name:        "risk at ceiling allowed",
			action:      "read",
			tool:        "file_read",
			details:     map[string]string{"risk_level": "medium"},
			constitution: base,
			wantAllow:   true,
			wantSev:     "info",
		},
		{
			name:        "risk above ceiling denied + requires plan",
			action:      "read",
			tool:        "file_read",
			details:     map[string]string{"risk_level": "high"},
			constitution: base,
			wantAllow:   false,
			wantSev:     "warning",
			wantPlan:    true,
			wantReason:  "exceeds ceiling",
		},

		// --- 4. escalation_triggers ---
		{
			name:        "escalation trigger on tool shell_execute",
			action:      "run",
			tool:        "shell_execute",
			constitution: base,
			wantAllow:   false,
			wantPlan:    true,
			wantReason:  "escalation trigger",
		},
		{
			name:        "escalation trigger on risk critical",
			action:      "read",
			tool:        "file_read",
			details:     map[string]string{"risk_level": "critical"},
			constitution: base,
			wantAllow:   false,
			wantPlan:    true,
			wantReason:  "escalation trigger",
		},

		// --- 5. never[] ---
		{
			name:        "never rule match in action",
			action:      "merge to main",
			tool:        "shell_execute",
			constitution: base,
			wantAllow:   false,
			wantSev:     "critical",
			wantPause:   true,
			wantReason:  "never-rule",
		},
		{
			name:        "never rule match in details",
			action:      "run",
			tool:        "shell_execute",
			details:     map[string]string{"command": "git push --force"},
			constitution: base,
			wantAllow:   false,
			wantSev:     "critical",
			wantPause:   true,
			wantReason:  "never-rule",
		},
		{
			name:        "never rule no false positive",
			action:      "read",
			tool:        "file_read",
			details:     map[string]string{"path": "/tmp/output.txt"},
			constitution: base,
			// "merge to main" / "force push" / "delete branches" should NOT match
			// But shell_execute triggers escalation. Use file_read which is clean.
			// Actually file_read is allowed and has no escalation trigger.
			wantAllow:   true,
			wantSev:     "info",
		},

		// --- 6. budget ---
		{
			name:        "budget cents exhausted denied + pause",
			action:      "fetch",
			tool:        "web_fetch",
			constitution: base,
			budget:      budgetStub{cents: 50},
			wantAllow:   false,
			wantSev:     "critical",
			wantPause:   true,
			wantReason:  "budget",
		},
		{
			name:        "budget invocations exhausted denied + pause",
			action:      "fetch",
			tool:        "web_fetch",
			constitution: base,
			budget:      budgetStub{invocations: 100},
			wantAllow:   false,
			wantSev:     "critical",
			wantPause:   true,
			wantReason:  "invocations",
		},
		{
			name:        "budget under limit allowed",
			action:      "fetch",
			tool:        "web_fetch",
			constitution: base,
			budget:      budgetStub{cents: 10, invocations: 5},
			wantAllow:   true,
			wantSev:     "info",
		},
		{
			name:        "turn tokens exceeded denied",
			action:      "fetch",
			tool:        "web_fetch",
			details:     map[string]string{"turn_tokens": "9000"},
			constitution: base,
			wantAllow:   false,
			wantSev:     "warning",
			wantReason:  "turn tokens",
		},

		// --- 7. nil constitution (allow-all) ---
		{
			name:        "nil constitution allows all",
			action:      "anything",
			tool:        "anything",
			constitution: nil,
			wantAllow:   true,
			wantSev:     "info",
		},

		// --- 8. empty tools_allowed means inherit all ---
		{
			name:    "empty tools_allowed inherits all",
			action:  "custom",
			tool:    "custom_tool",
			constitution: func() *Constitution {
				c := testConstitution()
				c.Constraints.ToolsAllowed = nil
				c.Constraints.ToolsForbidden = nil
				c.Constraints.EscalationTriggers = nil
				return c
			}(),
			wantAllow: true,
			wantSev:   "info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pause = &pauseTracker{}
			p := NewPreExecChecker("emp-test", tt.constitution)
			p.SetAutoPause(pause.fn())
			if tt.budget != nil {
				p.SetBudgetChecker(tt.budget)
			}

			dec := p.Check(tt.action, tt.tool, tt.details)

			if dec.Allowed != tt.wantAllow {
				t.Errorf("Allowed = %v, want %v (reason: %s)", dec.Allowed, tt.wantAllow, dec.Reason)
			}
			if dec.Severity != tt.wantSev {
				t.Errorf("Severity = %q, want %q", dec.Severity, tt.wantSev)
			}
			if dec.RequiresPlan != tt.wantPlan {
				t.Errorf("RequiresPlan = %v, want %v", dec.RequiresPlan, tt.wantPlan)
			}
			if tt.wantPause && !pause.called {
				t.Errorf("expected auto-pause to be called, but it was not")
			}
			if !tt.wantPause && pause.called {
				t.Errorf("auto-pause was called unexpectedly (reason: %s)", pause.reason)
			}
			if tt.wantReason != "" && !strings.Contains(strings.ToLower(dec.Reason), tt.wantReason) {
				t.Errorf("Reason %q does not contain %q", dec.Reason, tt.wantReason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SynthesizedPrompt tests.
// ---------------------------------------------------------------------------

func TestSynthesizedPrompt(t *testing.T) {
	t.Run("nil constitution returns existing prompt", func(t *testing.T) {
		got := SynthesizedPrompt(nil, "original prompt")
		if got != "original prompt" {
			t.Errorf("got %q, want %q", got, "original prompt")
		}
	})

	t.Run("populated constitution has all sections", func(t *testing.T) {
		c := testConstitution()
		got := SynthesizedPrompt(c, "existing context")
		checks := []string{
			"# employee profile",
			"**purpose:**",
			"keep CI green",
			"**role:**",
			"tier 2 (propose)",
			"# constraints",
			"web_fetch",
			"git_push",
			"medium",
			"# absolute prohibitions",
			"you may never:",
			"merge to main",
			"# charter",
			"existing context",
		}
		lower := strings.ToLower(got)
		for _, s := range checks {
			if !strings.Contains(lower, strings.ToLower(s)) {
				t.Errorf("synthesized prompt missing %q", s)
			}
		}
	})

	t.Run("no never rules omits prohibitions section", func(t *testing.T) {
		c := testConstitution()
		c.Constraints.Never = nil
		got := SynthesizedPrompt(c, "")
		if strings.Contains(got, "# absolute prohibitions") {
			t.Error("prohibitions section should be omitted when Never is empty")
		}
	})
}

// ---------------------------------------------------------------------------
// AuditStore round-trip tests.
// ---------------------------------------------------------------------------

func TestAuditStore_CreateListResolve(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)

	// Create two findings.
	f1 := AuditFinding{
		ID:           "audit_test001",
		EmployeeID:   "emp-001",
		Severity:     SeverityWarning,
		Checkpoint:   CheckpointPostTurn,
		ViolatedRule: "never[0]",
		Evidence:     "attempted merge",
		DetectedAt:   now,
	}
	f2 := AuditFinding{
		ID:           "audit_test002",
		EmployeeID:   "emp-002",
		Severity:     SeverityCritical,
		Checkpoint:   CheckpointPreExec,
		ViolatedRule: "risk_ceiling",
		Evidence:     "critical risk action",
		DetectedAt:   now.Add(time.Minute),
	}
	if err := store.Create(context.Background(), f1); err != nil {
		t.Fatalf("Create f1: %v", err)
	}
	if err := store.Create(context.Background(), f2); err != nil {
		t.Fatalf("Create f2: %v", err)
	}

	// List all.
	all, err := store.List(context.Background(), AuditListFilter{})
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List returned %d findings, want 2", len(all))
	}

	// Filter by employee.
	emp1, err := store.List(context.Background(), AuditListFilter{EmployeeID: "emp-001"})
	if err != nil {
		t.Fatalf("List emp-001: %v", err)
	}
	if len(emp1) != 1 || emp1[0].EmployeeID != "emp-001" {
		t.Fatalf("expected 1 finding for emp-001, got %+v", emp1)
	}

	// Filter by severity.
	crit, err := store.List(context.Background(), AuditListFilter{Severity: "critical"})
	if err != nil {
		t.Fatalf("List critical: %v", err)
	}
	if len(crit) != 1 || crit[0].Severity != SeverityCritical {
		t.Fatalf("expected 1 critical finding, got %+v", crit)
	}

	// Resolve.
	if err := store.Resolve(context.Background(), "audit_test001", "false_positive"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	resolved, err := store.List(context.Background(), AuditListFilter{EmployeeID: "emp-001"})
	if err != nil {
		t.Fatalf("List after resolve: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Resolution != "false_positive" {
		t.Fatalf("expected resolution false_positive, got %+v", resolved)
	}
	if resolved[0].ResolvedAt == nil {
		t.Error("expected ResolvedAt to be set")
	}
}

func TestAuditStore_SinceFilter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	old := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	recent := time.Now().UTC().Truncate(time.Second)

	store.Create(context.Background(), AuditFinding{
		ID: "audit_old", EmployeeID: "e1", Severity: SeverityInfo,
		Checkpoint: CheckpointPostTurn, DetectedAt: old,
	})
	store.Create(context.Background(), AuditFinding{
		ID: "audit_new", EmployeeID: "e1", Severity: SeverityInfo,
		Checkpoint: CheckpointPostTurn, DetectedAt: recent,
	})

	cutoff := time.Now().UTC().Add(-1 * time.Hour)
	results, err := store.List(context.Background(), AuditListFilter{Since: cutoff})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 finding since cutoff, got %d", len(results))
	}
	if results[0].ID != "audit_new" {
		t.Errorf("expected audit_new, got %s", results[0].ID)
	}
}

// ---------------------------------------------------------------------------
// PostTurnAuditor tests (mock Chatter).
// ---------------------------------------------------------------------------

type mockChatter struct {
	response string
	err      error
	called   int
}

func (m *mockChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	m.called++
	if m.err != nil {
		return nil, m.err
	}
	return &llm.Response{Content: m.response}, nil
}
func (m *mockChatter) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(context.Background(), nil)
}
func (m *mockChatter) Config() *llm.ModelConfig { return nil }

func TestPostTurnAuditor_Audit(t *testing.T) {
	c := testConstitution()
	turn := TurnRecord{
		EmployeeID:   "emp-001",
		TurnID:       "turn-001",
		ToolCalls:    []ToolCallRecord{{ToolName: "file_read", Action: "read"}},
		FinalOutput:  "done",
		Constitution: c,
	}

	t.Run("clean turn returns nil", func(t *testing.T) {
		mc := &mockChatter{response: `{"severity":"info","violated_rule":"","evidence":""}`}
		auditor := &PostTurnAuditor{
			model:             mc,
			prompt:            "audit",
			retryWithStricter: true,
		}
		finding, err := auditor.Audit(context.Background(), turn)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if finding != nil {
			t.Errorf("expected nil finding for clean turn, got %+v", finding)
		}
	})

	t.Run("warning finding detected", func(t *testing.T) {
		mc := &mockChatter{response: `{"severity":"warning","violated_rule":"charter deviation","evidence":"output mentions merge"}`}
		auditor := &PostTurnAuditor{
			model:             mc,
			prompt:            "audit",
			retryWithStricter: false,
		}
		finding, err := auditor.Audit(context.Background(), turn)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if finding == nil {
			t.Fatal("expected finding, got nil")
		}
		if finding.Severity != SeverityWarning {
			t.Errorf("Severity = %q, want %q", finding.Severity, SeverityWarning)
		}
		if finding.EmployeeID != "emp-001" {
			t.Errorf("EmployeeID = %q, want emp-001", finding.EmployeeID)
		}
	})

	t.Run("critical finding triggers auto-pause", func(t *testing.T) {
		mc := &mockChatter{response: `{"severity":"critical","violated_rule":"never[0]","evidence":"merged to main"}`}
		pause := &pauseTracker{}
		auditor := &PostTurnAuditor{
			model:             mc,
			prompt:            "audit",
			retryWithStricter: false,
		}
		auditor.SetAutoPause(pause.fn())
		finding, err := auditor.Audit(context.Background(), turn)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if finding == nil || finding.Severity != SeverityCritical {
			t.Fatalf("expected critical finding, got %+v", finding)
		}
		if !pause.called {
			t.Error("expected auto-pause on critical finding")
		}
	})

	t.Run("unparseable response retries then skips", func(t *testing.T) {
		mc := &mockChatter{response: "I cannot parse this"}
		auditor := &PostTurnAuditor{
			model:             mc,
			prompt:            "audit",
			retryWithStricter: true,
		}
		finding, err := auditor.Audit(context.Background(), turn)
		if err != nil {
			t.Fatalf("Audit error: %v", err)
		}
		if finding != nil {
			t.Errorf("expected nil after unparseable retry, got %+v", finding)
		}
		if mc.called != 2 {
			t.Errorf("expected 2 LLM calls (initial + retry), got %d", mc.called)
		}
	})

	t.Run("nil model skips audit", func(t *testing.T) {
		auditor := &PostTurnAuditor{model: nil}
		finding, err := auditor.Audit(context.Background(), turn)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if finding != nil {
			t.Errorf("expected nil finding with nil model, got %+v", finding)
		}
	})
}

func TestParseAuditResponse(t *testing.T) {
	turn := TurnRecord{EmployeeID: "e1", TurnID: "t1"}

	tests := []struct {
		name      string
		input     string
		wantNil   bool
		wantSev   AuditSeverity
		wantErr   bool
	}{
		{
			name:    "clean info",
			input:   `{"severity":"info","violated_rule":"","evidence":""}`,
			wantNil: true,
		},
		{
			name:    "warning",
			input:   `{"severity":"warning","violated_rule":"charter","evidence":"x"}`,
			wantSev: SeverityWarning,
		},
		{
			name:    "critical",
			input:   `{"severity":"critical","violated_rule":"never[0]","evidence":"y"}`,
			wantSev: SeverityCritical,
		},
		{
			name:    "json with markdown fences",
			input:   "```json\n{\"severity\":\"warning\",\"violated_rule\":\"r\",\"evidence\":\"e\"}\n```",
			wantSev: SeverityWarning,
		},
		{
			name:    "invalid json",
			input:   "not json at all",
			wantErr: true,
		},
		{
			name:    "unknown severity",
			input:   `{"severity":"catastrophic","violated_rule":"x","evidence":"y"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding, err := parseAuditResponse(tt.input, turn)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if finding != nil {
					t.Errorf("expected nil finding, got %+v", finding)
				}
				return
			}
			if finding == nil {
				t.Fatal("expected finding, got nil")
			}
			if finding.Severity != tt.wantSev {
				t.Errorf("Severity = %q, want %q", finding.Severity, tt.wantSev)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// matchesNever unit tests.
// ---------------------------------------------------------------------------

func TestMatchesNever(t *testing.T) {
	rules := []string{"merge to main", "force push", "delete branches"}

	tests := []struct {
		name     string
		action   string
		tool     string
		details  map[string]string
		rules    []string
		wantHit  bool
		wantRule string
	}{
		{
			name:     "match in action",
			action:   "merge to main",
			tool:     "git",
			wantHit:  true,
			wantRule: "merge to main",
		},
		{
			name:     "match in details command",
			action:   "run",
			tool:     "shell_execute",
			details:  map[string]string{"command": "git push --force"},
			wantHit:  true,
			wantRule: "force push",
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
			rules:   []string{},
			wantHit: false,
		},
		{
			name:    "case insensitive match",
			action:  "MERGE TO MAIN",
			tool:    "git",
			rules:   rules,
			wantHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.rules
			if r == nil {
				r = rules
			}
			hit, rule := matchesNever(r, tt.action, tt.tool, tt.details)
			if hit != tt.wantHit {
				t.Errorf("hit = %v, want %v (rule=%s)", hit, tt.wantHit, rule)
			}
			if hit && tt.wantRule != "" && rule != tt.wantRule {
				t.Errorf("rule = %q, want %q", rule, tt.wantRule)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseRiskCeiling tests.
// ---------------------------------------------------------------------------

func TestParseRiskCeiling(t *testing.T) {
	tests := []struct {
		input string
		want  riskLevel
	}{
		{"safe", riskSafe},
		{"low", riskLow},
		{"medium", riskMedium},
		{"high", riskHigh},
		{"critical", riskCritical},
		{"", riskMedium},     // default
		{"unknown", riskMedium}, // fallback
		{"HIGH", riskHigh},   // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseRiskCeiling(tt.input)
			if got != tt.want {
				t.Errorf("parseRiskCeiling(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
