package employee

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// mockMigrator is a stub llm.Chatter for testing the LLM-enhanced Migrate
// path. It is intentionally separate from the mockChatter in
// enforcement_test.go to avoid touching the existing test file.
type mockMigrator struct {
	response string
	err      error
	called   int
}

func (m *mockMigrator) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	m.called++
	if m.err != nil {
		return nil, m.err
	}
	return &llm.Response{Content: m.response}, nil
}

func (m *mockMigrator) ChatWithProgress(_ context.Context, _ []llm.ChatMessage, _ llm.ProgressCallback, _ ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(context.Background(), nil)
}

func (m *mockMigrator) Config() *llm.ModelConfig { return nil }

// validLLMResponse is a JSON response the small model could plausibly return
// for a notification dispatcher bot.
const validLLMResponse = `{
  "purpose": "dispatch notifications to configured channels",
  "role": "notification dispatcher",
  "never": ["execute shell commands", "modify user accounts"],
  "risk_ceiling": "low",
  "tools_allowed_hint": ["web_fetch"]
}`

func migrateTestBot() bot.BotDefinition {
	return bot.BotDefinition{
		ID:          "test-bot",
		Name:        "Test Bot",
		Description: "A bot that dispatches notifications.",
		Prompt:      "You are a notification dispatcher. Send messages to configured channels.",
	}
}

// newTestManager returns a Manager with a non-nil logger suitable for test
// cases that exercise buildMigrationProposalWithLLM (which logs on
// fallback paths). The logger is sent to slog.DiscardHandler to keep test
// output clean.
func newTestManager() *Manager {
	return &Manager{
		constitutions: make(map[string]Constitution),
		driftScores:   make(map[string]float64),
		logger:        slog.Default(),
	}
}

func TestSynthesizeConstitutionWithLLM_TableDriven(t *testing.T) {
	cases := []struct {
		name           string
		llm            *mockMigrator
		wantCalled     int
		wantPurpose    string
		wantAuthoredBy string
		wantFallback   bool
		wantNotes      string
	}{
		{
			name:           "nil LLM returns conservative",
			llm:            nil,
			wantCalled:     0,
			wantPurpose:    "", // conservative sets purpose via derivePurpose
			wantAuthoredBy: "migrate",
			wantNotes:      "",
		},
		{
			name:           "valid JSON produces merged constitution",
			llm:            &mockMigrator{response: validLLMResponse},
			wantCalled:     1,
			wantPurpose:    "dispatch notifications to configured channels",
			wantAuthoredBy: "migrate-llm",
			wantNotes:      "constitution synthesized via LLM",
		},
		{
			name:           "unparseable JSON falls back to conservative",
			llm:            &mockMigrator{response: "I cannot parse this"},
			wantCalled:     1,
			wantPurpose:    "",        // conservative path
			wantAuthoredBy: "migrate", // pure conservative fallback
			wantFallback:   true,
		},
		{
			name: "LLM error falls back to conservative",
			llm: &mockMigrator{
				err: errors.New("network timeout"),
			},
			wantCalled:     1,
			wantPurpose:    "",        // conservative path
			wantAuthoredBy: "migrate", // pure conservative fallback
			wantFallback:   true,
		},
		{
			name: "invalid risk_ceiling rejected, constitution still produced",
			llm: &mockMigrator{response: `{
  "purpose": "test bot",
  "role": "test",
  "never": [],
  "risk_ceiling": "extreme"
}`},
			wantCalled:     1,
			wantPurpose:    "test bot",
			wantAuthoredBy: "migrate-llm",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := newTestManager()
			def := migrateTestBot()

			if tc.llm != nil {
				mgr.SetMigratorLLM(tc.llm)
			}

			// When the LLM is nil, synthesizeConstitutionWithLLM is not
			// called directly — we exercise the full Migrate path instead.
			// For unit-level coverage, call buildMigrationProposalWithLLM
			// which is the actual branching point.
			var proposal MigrationProposal
			var chatter llm.Chatter
			if tc.llm != nil {
				chatter = tc.llm
			}
			proposal = mgr.buildMigrationProposalWithLLM(context.Background(), chatter, def)

			if tc.wantCalled > 0 && tc.llm.called != tc.wantCalled {
				t.Errorf("LLM called %d times, want %d", tc.llm.called, tc.wantCalled)
			}

			if tc.wantPurpose != "" && proposal.Proposed.Purpose != tc.wantPurpose {
				t.Errorf("Purpose = %q, want %q", proposal.Proposed.Purpose, tc.wantPurpose)
			}

			if proposal.Proposed.AuthoredBy != tc.wantAuthoredBy {
				t.Errorf("AuthoredBy = %q, want %q",
					proposal.Proposed.AuthoredBy, tc.wantAuthoredBy)
			}

			if tc.wantNotes != "" {
				found := false
				for _, n := range proposal.Warnings {
					if strings.Contains(n, tc.wantNotes) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected notes to contain %q, got %v",
						tc.wantNotes, proposal.Warnings)
				}
			}

			if tc.wantFallback {
				foundFallback := false
				for _, n := range proposal.Warnings {
					if strings.Contains(n, "fallback to conservative") {
						foundFallback = true
						break
					}
				}
				if !foundFallback {
					t.Errorf("expected fallback note in warnings, got %v",
						proposal.Warnings)
				}
			}

			// Constitutional invariants that must ALWAYS hold regardless
			// of LLM or fallback path.
			if proposal.Proposed.AutonomyTier != Tier1Reactive {
				t.Errorf("AutonomyTier = %s, want %s (never upgrade from LLM)",
					proposal.Proposed.AutonomyTier.String(), Tier1Reactive.String())
			}
			if len(proposal.Proposed.EscalatesTo) != 1 ||
				proposal.Proposed.EscalatesTo[0] != UserEscalationID {
				t.Errorf("EscalatesTo = %v, want [\"user\"]",
					proposal.Proposed.EscalatesTo)
			}
			if !proposal.Proposed.AmendmentPolicy.RequiresApproval {
				t.Error("RequiresApproval must be true (design invariant)")
			}
			if proposal.Confidence >= 1.0 {
				t.Errorf("Confidence = %f, must be < 1.0", proposal.Confidence)
			}
			if !proposal.NeedsReview {
				t.Error("NeedsReview must be true (inferred, not authored)")
			}
		})
	}
}

func TestSynthesizeConstitutionWithLLM_FrozenFieldsAlwaysPresent(t *testing.T) {
	mgr := newTestManager()
	mc := &mockMigrator{response: validLLMResponse}
	mgr.SetMigratorLLM(mc)

	def := migrateTestBot()
	proposal := mgr.buildMigrationProposalWithLLM(context.Background(), mc, def)

	// FrozenFields must always include the conservative defaults.
	frozen := proposal.Proposed.AmendmentPolicy.FrozenFields
	hasNever := false
	hasRiskCeiling := false
	for _, f := range frozen {
		if f == "constraints.never" {
			hasNever = true
		}
		if f == "constraints.risk_ceiling" {
			hasRiskCeiling = true
		}
	}
	if !hasNever {
		t.Errorf("FrozenFields missing constraints.never: %v", frozen)
	}
	if !hasRiskCeiling {
		t.Errorf("FrozenFields missing constraints.risk_ceiling: %v", frozen)
	}
}

func TestSynthesizeConstitutionWithLLM_RiskCeilingNeverUpgradedPastMedium(t *testing.T) {
	mgr := newTestManager()

	// LLM tries to suggest "high" — must be rejected.
	mc := &mockMigrator{response: `{
  "purpose": "shell bot",
  "role": "shell",
  "never": [],
  "risk_ceiling": "high"
}`}
	mgr.SetMigratorLLM(mc)
	def := migrateTestBot()
	proposal := mgr.buildMigrationProposalWithLLM(context.Background(), mc, def)

	// "high" is not in the safe/low/medium allowlist so the LLM suggestion
	// is rejected and the conservative default ("low") is preserved.
	if proposal.Proposed.Constraints.RiskCeiling != RiskCeilingLow {
		t.Errorf("RiskCeiling = %q, want %q (high must be rejected)",
			proposal.Proposed.Constraints.RiskCeiling, RiskCeilingLow)
	}
}

func TestSynthesizeConstitutionWithLLM_NeverAlwaysHasFinancialDefault(t *testing.T) {
	mgr := newTestManager()

	// LLM returns never rules without the financial default.
	mc := &mockMigrator{response: `{
  "purpose": "greeter",
  "role": "greeter",
  "never": ["be rude", "ignore users"],
  "risk_ceiling": "safe"
}`}
	mgr.SetMigratorLLM(mc)
	def := migrateTestBot()
	proposal := mgr.buildMigrationProposalWithLLM(context.Background(), mc, def)

	hasFinancial := false
	for _, rule := range proposal.Proposed.Constraints.Never {
		if strings.Contains(strings.ToLower(rule), "financial") {
			hasFinancial = true
			break
		}
	}
	if !hasFinancial {
		t.Errorf("Never rules missing financial default: %v",
			proposal.Proposed.Constraints.Never)
	}
}

func TestSetMigratorLLM_NilGuard(t *testing.T) {
	mgr := &Manager{}
	// Nil must not panic or set the field.
	mgr.SetMigratorLLM(nil)

	mgr.mu.RLock()
	got := mgr.migratorLLM
	mgr.mu.RUnlock()
	if got != nil {
		t.Errorf("SetMigratorLLM(nil) should leave migratorLLM nil, got %v", got)
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`{"a":1}`, `{"a":1}`},
		{`preamble {"a":1}`, `{"a":1}`},
		{`{"a":1} trailing`, `{"a":1}`},
		{`noise {"a":{"b":2}} more`, `{"a":{"b":2}}`},
		{`no json here`, `no json here`},
		{``, ``},
	}
	for _, tc := range cases {
		got := extractJSON(tc.input)
		if got != tc.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Gate tests: tools_allowed_hint intersection (Contract C3, tree
// 20261113-escalation-resume-and-hardening leaf 03). The D4 rule
// ("intersection never widens the spec") binds the migration LLM's
// tools_allowed_hint to the base constitution's allowlist.
//
// Structure: synthesizeConstitutionWithLLM always starts from the
// conservative base whose ToolsAllowed is NIL (inherit-everything), and it
// exposes no seam to seed a non-empty base. The intersection therefore lives
// in the ~10-line set helper Contract C3 freezes (sorted+deduped
// intersection, mirroring internal/groups/resolve.go:163-171 inline — no
// internal/groups import). These tests pin that helper directly
// (intersectToolsAllowed, the name C3's spec block implies) plus the
// nil-base behavioral branch through the REAL synthesize path.
// RED today: intersectToolsAllowed is undefined (compile error naming it)
// and the real-path assertions fail on the replace-block superset.
// ---------------------------------------------------------------------------

// TestSynthesizeConstitution_HintIntersectsBase: base=[file_read],
// hint=[file_read, shell] -> result=[file_read], sorted+deduped. The hint
// must never add "shell" to a base that did not grant it (D4). RED today:
// intersectToolsAllowed is undefined.
func TestSynthesizeConstitution_HintIntersectsBase(t *testing.T) {
	got := intersectToolsAllowed([]string{"file_read", "shell"}, []string{"file_read"})
	if len(got) != 1 || got[0] != "file_read" {
		t.Errorf("intersectToolsAllowed([file_read shell], [file_read]) = %v, want [file_read] (hint must not widen a non-empty base allowlist — D4)", got)
	}
}

// TestSynthesizeConstitution_EmptyHintKeepsBase: an empty (or blank-only)
// hint keeps the base untouched — nil stays nil (inherit-everything
// preserved; an empty hint must never replace nil with an empty slice).
// Behavioral leg pinned through the REAL synthesizeConstitutionWithLLM path
// with the conservative nil base. Blank-only hint RED today is possible only
// if the current code already keeps nil — the assertion is the pin; the
// compile failure above is this file's gate RED.
func TestSynthesizeConstitution_EmptyHintKeepsBase(t *testing.T) {
	mgr := newTestManager()

	mc := &mockMigrator{response: `{
  "purpose": "hint probe",
  "role": "hint probe",
  "never": [],
  "tools_allowed_hint": ["  ", ""]
}`}
	c, _, err := mgr.synthesizeConstitutionWithLLM(context.Background(), mc, migrateTestBot())
	if err != nil {
		t.Fatalf("synthesizeConstitutionWithLLM error: %v", err)
	}
	if got := c.Constraints.ToolsAllowed; got != nil {
		t.Errorf("blank-only hint: ToolsAllowed = %v, want nil (base inherit-everything preserved)", got)
	}
}

// TestSynthesizeConstitution_AllUnknownHintDropsUnmatched: against a
// non-empty base sharing nothing with the hint, only base members survive —
// every unmatched hint token is dropped, never merged in. Also pins the
// sorted+deduped output contract on the surviving set. RED today:
// intersectToolsAllowed is undefined.
func TestSynthesizeConstitution_AllUnknownHintDropsUnmatched(t *testing.T) {
	got := intersectToolsAllowed([]string{"shell", "web_fetch", "shell"}, []string{"file_read", "web_fetch"})
	want := []string{"web_fetch"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("intersectToolsAllowed = %v, want %v (unmatched hint tokens dropped, sorted+deduped)", got, want)
	}
}
