package agent

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestNewIntentAnalyzer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := &llm.Client{} //nolint:ineffassign // Only tests construction

	ia := NewIntentAnalyzer(client, logger)
	if ia == nil {
		t.Fatal("NewIntentAnalyzer returned nil")
	}
	if ia.client != client {
		t.Error("client not set correctly")
	}
	if ia.ambiguityThreshold != defaultAmbiguityThreshold {
		t.Errorf("default threshold = %v, want %v", ia.ambiguityThreshold, defaultAmbiguityThreshold)
	}
	if ia.logger == nil {
		t.Error("logger should not be nil")
	}

	// Test nil logger fallback
	ia2 := NewIntentAnalyzer(client, nil)
	if ia2.logger == nil {
		t.Error("logger fallback to default failed")
	}

	// Test WithAmbiguityThreshold
	ia3 := NewIntentAnalyzer(client, logger).WithAmbiguityThreshold(0.8)
	if ia3.ambiguityThreshold != 0.8 {
		t.Errorf("custom threshold = %v, want 0.8", ia3.ambiguityThreshold)
	}
}

func TestIntentAnalyzer_ParseAnalysis(t *testing.T) {
	ia := NewIntentAnalyzer(nil, slog.Default())

	tests := []struct {
		name    string
		content string
		want    *TrueIntentAnalysis
		wantErr bool
	}{
		{
			name:    "valid full analysis",
			content: `{"goal":"implement a REST API","ambiguity":0.3,"scope":"narrow","category":"implementation","suggested_questions":[],"confidence":0.9}`,
			want: &TrueIntentAnalysis{
				Goal:               "implement a REST API",
				Ambiguity:          0.3,
				Scope:              "narrow",
				Category:           "implementation",
				SuggestedQuestions: []string{},
				Confidence:         0.9,
			},
			wantErr: false,
		},
		{
			name:    "valid ambiguous analysis with questions",
			content: `{"goal":"fix something","ambiguity":0.8,"scope":"broad","category":"fix","suggested_questions":["What is broken?","What are the symptoms?"],"confidence":0.7}`,
			want: &TrueIntentAnalysis{
				Goal:               "fix something",
				Ambiguity:          0.8,
				Scope:              "broad",
				Category:           "fix",
				SuggestedQuestions: []string{"What is broken?", "What are the symptoms?"},
				Confidence:         0.7,
			},
			wantErr: false,
		},
		{
			name:    "suggested_mode spec_plan parsed",
			content: `{"goal":"refactor multi-file module","ambiguity":0.2,"scope":"broad","category":"implementation","suggested_questions":[],"confidence":0.9,"suggested_mode":"spec_plan"}`,
			want: &TrueIntentAnalysis{
				Goal:               "refactor multi-file module",
				Ambiguity:          0.2,
				Scope:              "broad",
				Category:           "implementation",
				SuggestedQuestions: []string{},
				Confidence:         0.9,
				SuggestedMode:      "spec_plan",
			},
			wantErr: false,
		},
		{
			name:    "suggested_mode direct parsed",
			content: `{"goal":"what time is it","ambiguity":0.1,"scope":"narrow","category":"clarification","suggested_questions":[],"confidence":0.95,"suggested_mode":"direct"}`,
			want: &TrueIntentAnalysis{
				Goal:               "what time is it",
				Ambiguity:          0.1,
				Scope:              "narrow",
				Category:           "clarification",
				SuggestedQuestions: []string{},
				Confidence:         0.95,
				SuggestedMode:      "direct",
			},
			wantErr: false,
		},
		{
			name:    "invalid suggested_mode zeroed",
			content: `{"goal":"test","ambiguity":0.3,"scope":"narrow","category":"research","suggested_questions":[],"confidence":0.8,"suggested_mode":"ultra_mode"}`,
			want: &TrueIntentAnalysis{
				Goal:               "test",
				Ambiguity:          0.3,
				Scope:              "narrow",
				Category:           "research",
				SuggestedQuestions: []string{},
				Confidence:         0.8,
				SuggestedMode:      "",
			},
			wantErr: false,
		},
		{
			name:    "missing suggested_mode empty",
			content: `{"goal":"test","ambiguity":0.3,"scope":"narrow","category":"research","suggested_questions":[],"confidence":0.8}`,
			want: &TrueIntentAnalysis{
				Goal:               "test",
				Ambiguity:          0.3,
				Scope:              "narrow",
				Category:           "research",
				SuggestedQuestions: []string{},
				Confidence:         0.8,
				SuggestedMode:      "",
			},
			wantErr: false,
		},
		{
			name:    "JSON wrapped in markdown",
			content: "```json\n{\"goal\":\"research topic\",\"ambiguity\":0.5,\"scope\":\"medium\",\"category\":\"research\",\"suggested_questions\":[],\"confidence\":0.85}\n```",
			want: &TrueIntentAnalysis{
				Goal:               "research topic",
				Ambiguity:          0.5,
				Scope:              "medium",
				Category:           "research",
				SuggestedQuestions: []string{},
				Confidence:         0.85,
			},
			wantErr: false,
		},
		{
			name:    "invalid scope falls back to medium",
			content: `{"goal":"test","ambiguity":0.2,"scope":"invalid","category":"other","suggested_questions":[],"confidence":0.5}`,
			want: &TrueIntentAnalysis{
				Goal:               "test",
				Ambiguity:          0.2,
				Scope:              "medium",
				Category:           "other",
				SuggestedQuestions: []string{},
				Confidence:         0.5,
			},
			wantErr: false,
		},
		{
			name:    "invalid category falls back to other",
			content: `{"goal":"test","ambiguity":0.2,"scope":"narrow","category":"invalid_cat","suggested_questions":[],"confidence":0.5}`,
			want: &TrueIntentAnalysis{
				Goal:               "test",
				Ambiguity:          0.2,
				Scope:              "narrow",
				Category:           "other",
				SuggestedQuestions: []string{},
				Confidence:         0.5,
			},
			wantErr: false,
		},
		{
			name:    "ambiguity clamped to 1.0",
			content: `{"goal":"test","ambiguity":1.5,"scope":"narrow","category":"research","suggested_questions":[],"confidence":0.5}`,
			want: &TrueIntentAnalysis{
				Goal:               "test",
				Ambiguity:          1.0,
				Scope:              "narrow",
				Category:           "research",
				SuggestedQuestions: []string{},
				Confidence:         0.5,
			},
			wantErr: false,
		},
		{
			name:    "ambiguity clamped to 0.0",
			content: `{"goal":"test","ambiguity":-0.5,"scope":"narrow","category":"research","suggested_questions":[],"confidence":0.5}`,
			want: &TrueIntentAnalysis{
				Goal:               "test",
				Ambiguity:          0.0,
				Scope:              "narrow",
				Category:           "research",
				SuggestedQuestions: []string{},
				Confidence:         0.5,
			},
			wantErr: false,
		},
		{
			name:    "confidence clamped to 1.0",
			content: `{"goal":"test","ambiguity":0.5,"scope":"narrow","category":"research","suggested_questions":[],"confidence":2.0}`,
			want: &TrueIntentAnalysis{
				Goal:               "test",
				Ambiguity:          0.5,
				Scope:              "narrow",
				Category:           "research",
				SuggestedQuestions: []string{},
				Confidence:         1.0,
			},
			wantErr: false,
		},
		{
			name:    "empty JSON returns error",
			content: "",
			wantErr: true,
		},
		{
			name:    "no JSON in response returns error",
			content: "just some text without braces",
			wantErr: true,
		},
		{
			name:    "malformed JSON returns error",
			content: "{ invalid json }",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ia.parseAnalysis(tt.content)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseAnalysis(%q) expected error, got nil", tt.content)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAnalysis(%q) unexpected error: %v", tt.content, err)
			}

			if got.Goal != tt.want.Goal {
				t.Errorf("Goal = %q, want %q", got.Goal, tt.want.Goal)
			}
			if got.Ambiguity != tt.want.Ambiguity {
				t.Errorf("Ambiguity = %v, want %v", got.Ambiguity, tt.want.Ambiguity)
			}
			if got.Scope != tt.want.Scope {
				t.Errorf("Scope = %q, want %q", got.Scope, tt.want.Scope)
			}
			if got.Category != tt.want.Category {
				t.Errorf("Category = %q, want %q", got.Category, tt.want.Category)
			}
			if len(got.SuggestedQuestions) != len(tt.want.SuggestedQuestions) {
				t.Errorf("SuggestedQuestions length = %d, want %d", len(got.SuggestedQuestions), len(tt.want.SuggestedQuestions))
			} else {
				for i := range got.SuggestedQuestions {
					if got.SuggestedQuestions[i] != tt.want.SuggestedQuestions[i] {
						t.Errorf("SuggestedQuestions[%d] = %q, want %q", i, got.SuggestedQuestions[i], tt.want.SuggestedQuestions[i])
					}
				}
			}
			if got.Confidence != tt.want.Confidence {
				t.Errorf("Confidence = %v, want %v", got.Confidence, tt.want.Confidence)
			}
			if got.SuggestedMode != tt.want.SuggestedMode {
				t.Errorf("SuggestedMode = %q, want %q", got.SuggestedMode, tt.want.SuggestedMode)
			}
		})
	}
}

func TestTrueIntentAnalysis_IsAmbiguous(t *testing.T) {
	tests := []struct {
		name      string
		ambiguity float64
		threshold float64
		want      bool
	}{
		{"below threshold", 0.5, 0.6, false},
		{"at threshold", 0.6, 0.6, true},
		{"above threshold", 0.8, 0.6, true},
		{"zero ambiguity", 0.0, 0.6, false},
		{"max ambiguity", 1.0, 0.6, true},
		{"high threshold not met", 0.7, 0.8, false},
		{"high threshold met", 0.9, 0.8, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := &TrueIntentAnalysis{Ambiguity: tt.ambiguity}
			got := analysis.IsAmbiguous(tt.threshold)
			if got != tt.want {
				t.Errorf("IsAmbiguous(%v) with ambiguity=%v = %v, want %v", tt.threshold, tt.ambiguity, got, tt.want)
			}
		})
	}
}

func TestIntentAnalyzer_AnalyzeTrueIntent_NoClient(t *testing.T) {
	ia := NewIntentAnalyzer(nil, slog.Default())
	ctx := context.Background()

	_, err := ia.AnalyzeTrueIntent(ctx, "test input", nil)
	if err == nil {
		t.Error("Expected error when client is nil, got nil")
	}
}

// --- Session-aware analysis (leaf 02 of session-aware-intent-gate) ---
//
// IntentAnalyzer stores a concrete *llm.Client, which cannot be backed by a
// stub, so message construction is factored into the unexported
// buildAnalysisMessages helper and unit-tested directly here. End-to-end
// capture of the exact messages sent through a real *llm.Client is covered
// by the httptest-backed tests in intent_session_rules_test.go.

const (
	testRule1Substr    = "Recent session activity may be provided with the input"
	testRule2Substr    = "If the input is a short follow-up question about the recent activity"
	testActivitySubstr = "[Recent session activity]"
)

// requireSessionRules asserts both context-conditioned ambiguity rules are
// present in the system prompt (they must appear in EVERY call).
func requireSessionRules(t *testing.T, systemPrompt string) {
	t.Helper()
	if !strings.Contains(systemPrompt, testRule1Substr) {
		t.Errorf("system prompt missing session-activity pronoun rule; got:\n%s", systemPrompt)
	}
	if !strings.Contains(systemPrompt, testRule2Substr) {
		t.Errorf("system prompt missing follow-up LOW-ambiguity rule; got:\n%s", systemPrompt)
	}
}

func TestIntentAnalyzer_BuildAnalysisMessages_Contextless(t *testing.T) {
	ia := NewIntentAnalyzer(nil, digestTestLogger())
	const input = "did the change get made?"

	cases := map[string]*SessionContextDigest{
		"nil digest":   nil,
		"empty digest": {},
	}

	for name, digest := range cases {
		t.Run(name, func(t *testing.T) {
			got := ia.buildAnalysisMessages(input, digest)
			if len(got) != 2 {
				t.Fatalf("len(messages) = %d, want 2", len(got))
			}
			if got[0].Role != llm.RoleSystem {
				t.Errorf("messages[0].Role = %q, want system", got[0].Role)
			}
			if got[1].Role != llm.RoleUser {
				t.Errorf("messages[1].Role = %q, want user", got[1].Role)
			}
			// Byte-identical contextless behavior: user message is the raw
			// input alone, no activity block.
			if got[1].Content != input {
				t.Errorf("user message not byte-identical to input:\n got: %q\nwant: %q", got[1].Content, input)
			}
			if strings.Contains(got[1].Content, testActivitySubstr) {
				t.Errorf("contextless user message contains activity block: %q", got[1].Content)
			}
			requireSessionRules(t, got[0].Content)
		})
	}
}

func TestIntentAnalyzer_BuildAnalysisMessages_WithDigest(t *testing.T) {
	ia := NewIntentAnalyzer(nil, digestTestLogger())

	digest := &SessionContextDigest{
		LastTaskName:      "Fix the login bug",
		LastTaskState:     "completed",
		LastTaskAgent:     "coder",
		LastResultSummary: "Fixed the login bug.",
	}

	got := ia.buildAnalysisMessages("did the change get made?", digest)
	if len(got) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(got))
	}

	wantUser := "did the change get made?" +
		"\n\n[Recent session activity]\n" +
		"Last task: Fix the login bug (state: completed, agent: coder)\n" +
		"Result summary: Fixed the login bug."
	if got[1].Content != wantUser {
		t.Errorf("user message:\n got: %q\nwant: %q", got[1].Content, wantUser)
	}
	requireSessionRules(t, got[0].Content)
}

func TestIntentAnalyzer_BuildAnalysisMessages_PartialDigest(t *testing.T) {
	ia := NewIntentAnalyzer(nil, digestTestLogger())

	// Name present; state, agent, and summary empty.
	digest := &SessionContextDigest{LastTaskName: "Pending work item"}

	got := ia.buildAnalysisMessages("what about the change?", digest)
	if len(got) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(got))
	}

	wantUser := "what about the change?\n\n[Recent session activity]\nLast task: Pending work item"
	if got[1].Content != wantUser {
		t.Errorf("user message:\n got: %q\nwant: %q", got[1].Content, wantUser)
	}
	if strings.Contains(got[1].Content, "Result summary:") {
		t.Errorf("user message contains Result summary line despite empty summary: %q", got[1].Content)
	}
	if strings.Contains(got[1].Content, "state:") {
		t.Errorf("user message contains state segment despite empty state: %q", got[1].Content)
	}
	requireSessionRules(t, got[0].Content)
}

// TestIntentAnalyzer_ResolvedModel verifies the provenance accessor returns
// the resolved "provider/model" that actually served the analysis (leaf 01
// of classifier-observability). The primary alias candidate fails with an
// empty response, forcing the documented rotation to the secondary, so the
// resolved model must be p2/m2 — not the initially-configured p1/m1.
func TestIntentAnalyzer_ResolvedModel(t *testing.T) {
	_, _, primaryCfg, secondaryCfg := failoverTestServers(
		t, emptyContentResponse(), validAnalysisResponse())
	resolver := newFailoverResolver(t, primaryCfg, secondaryCfg)

	ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{
		ModelConfig: primaryCfg,
		Resolver:    resolver,
		AliasName:   testClassifierAlias,
	}, llm.NewClient(primaryCfg), slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if got := ia.ResolvedModel(); got != "" {
		t.Fatalf("ResolvedModel() before any analysis = %q, want empty", got)
	}

	if _, err := ia.AnalyzeTrueIntent(context.Background(), "fix this bug please", nil); err != nil {
		t.Fatalf("AnalyzeTrueIntent failed: %v", err)
	}

	if got := ia.ResolvedModel(); got != "p2/m2" {
		t.Errorf("ResolvedModel() after failover = %q, want %q", got, "p2/m2")
	}
}

// TestIntentAnalyzer_ResolvedModel_NoRotation covers the no-failover path:
// the analyzer reports the model it was constructed with.
func TestIntentAnalyzer_ResolvedModel_NoRotation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validAnalysisResponse()))
	}))
	defer srv.Close()

	cfg := &llm.ModelConfig{BaseURL: srv.URL, ModelID: "primary", ProviderID: "p1"}
	ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{
		ModelConfig: cfg,
	}, llm.NewClient(cfg), slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if _, err := ia.AnalyzeTrueIntent(context.Background(), "fix this bug please", nil); err != nil {
		t.Fatalf("AnalyzeTrueIntent failed: %v", err)
	}

	if got := ia.ResolvedModel(); got != "p1/primary" {
		t.Errorf("ResolvedModel() = %q, want %q", got, "p1/primary")
	}
}
