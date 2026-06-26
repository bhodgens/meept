package agent

import (
	"context"
	"log/slog"
	"os"
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

	_, err := ia.AnalyzeTrueIntent(ctx, "test input")
	if err == nil {
		t.Error("Expected error when client is nil, got nil")
	}
}
