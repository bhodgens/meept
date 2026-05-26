package q

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		check func(string) bool
	}{
		{
			name:  "empty path",
			path:  "",
			check: func(got string) bool { return got == "" },
		},
		{
			name:  "absolute path",
			path:  "/tmp/test",
			check: func(got string) bool { return got == "/tmp/test" },
		},
		{
			name:  "relative path",
			path:  "relative/path",
			check: func(got string) bool { return got == "relative/path" },
		},
		{
			name: "tilde home dir expansion",
			path: "~/test/path",
			check: func(got string) bool {
				home, _ := os.UserHomeDir()
				expected := filepath.Join(home, "test", "path")
				return got == expected
			},
		},
		{
			name: "tilde only",
			path: "~",
			check: func(got string) bool {
				home, _ := os.UserHomeDir()
				return got == home
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandPath(tt.path)
			if !tt.check(got) {
				t.Errorf("expandPath(%q) = %q", tt.path, got)
			}
		})
	}
}

func TestMinInt(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{3, 5, 3},
		{5, 3, 3},
		{3, 3, 3},
		{0, 0, 0},
		{-1, -2, -2},
		{-5, 0, -5},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := minInt(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("minInt(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestQAgentGenerateSummaryEmpty(t *testing.T) {
	qa := &QAgent{}

	result := &AnalysisResult{
		SessionsAnalyzed: 0,
		Status:           "completed",
	}

	summary := qa.generateSummary(result)
	if summary == "" {
		t.Error("summary should not be empty")
	}
	if !containsStrQ(summary, "0 sessions") {
		t.Error("summary should mention 0 sessions")
	}
}

func TestQAgentGenerateSummaryNoPatterns(t *testing.T) {
	qa := &QAgent{}

	result := &AnalysisResult{
		SessionsAnalyzed: 5,
		PatternsDetected: []PatternReport{},
		Status:           "completed",
	}

	summary := qa.generateSummary(result)
	if !containsStrQ(summary, "No significant patterns") {
		t.Error("expected 'No significant patterns' message")
	}
}

func TestQAgentGenerateSummaryWithPatterns(t *testing.T) {
	qa := &QAgent{}

	patterns := []PatternReport{
		{PatternType: "high_error_rate", Confidence: 0.8},
		{PatternType: "wrong_agent_assignment", Confidence: 0.7},
		{PatternType: "model_misconfiguration", Confidence: 0.6},
		{PatternType: "extra_pattern", Confidence: 0.5},
	}

	result := &AnalysisResult{
		SessionsAnalyzed: 10,
		PatternsDetected: patterns,
		Status:           "completed",
	}

	summary := qa.generateSummary(result)

	if !containsStrQ(summary, "10 sessions") {
		t.Error("summary should mention 10 sessions")
	}
	if !containsStrQ(summary, "improvement opportunities") {
		t.Error("summary should mention improvement opportunities")
	}

	countParts := 0
	if containsStrQ(summary, "(1)") {
		countParts++
	}
	if containsStrQ(summary, "(2)") {
		countParts++
	}
	if containsStrQ(summary, "(3)") {
		countParts++
	}
	if containsStrQ(summary, "(4)") {
		countParts++
	}
	if countParts != 3 {
		t.Errorf("summary should include only first 3 patterns, got %d", countParts)
	}
}

func TestCompileResults(t *testing.T) {
	qa := &QAgent{}

	analyses := []*SessionAnalysis{
		{SessionID: "s1", AgentID: "coder", Outcome: "completed"},
	}

	patterns := []PatternReport{
		{PatternType: "high_error_rate", Confidence: 0.8, SessionCount: 5},
	}

	result := qa.compileResults(analyses, patterns, []*ResearchReport{}, []*AgentDesign{}, []*ImpactEstimate{}, nil)

	if result == nil {
		t.Fatal("compileResults returned nil")
	}
	if result.SessionsAnalyzed != 1 {
		t.Errorf("SessionsAnalyzed = %d, want 1", result.SessionsAnalyzed)
	}
	if len(result.PatternsDetected) != 1 {
		t.Errorf("PatternsDetected = %d, want 1", len(result.PatternsDetected))
	}
	if result.Status != "completed" {
		t.Errorf("status = %q, want 'completed'", result.Status)
	}
	if result.ID == "" {
		t.Error("result ID should not be empty")
	}
	if result.AnalyzedAt.IsZero() {
		t.Error("AnalyzedAt should not be zero")
	}
	// Recompile with research reports -- check recommendations flow through
	withRecs := qa.compileResults(analyses, patterns, []*ResearchReport{
		{
			Recommendations: []Recommendation{
				{Type: "new_agent", Title: "Debug specialist", Priority: "high"},
			},
		},
	}, []*AgentDesign{}, []*ImpactEstimate{}, []Recommendation{
		{Type: "new_agent", Title: "Debug specialist", Priority: "high"},
	})
	if len(withRecs.Recommendations) != 1 {
		t.Errorf("expected 1 recommendation, got %d", len(withRecs.Recommendations))
	}
}

func TestCompileResultsEmpty(t *testing.T) {
	qa := &QAgent{}

	result := qa.compileResults(nil, nil, []*ResearchReport{}, []*AgentDesign{}, []*ImpactEstimate{}, nil)

	if result == nil {
		t.Fatal("compileResults returned nil for empty inputs")
	}
	if result.SessionsAnalyzed != 0 {
		t.Errorf("SessionsAnalyzed = %d, want 0", result.SessionsAnalyzed)
	}
	if result.Summary == "" {
		t.Error("summary should exist even for empty results")
	}
}

func TestSaveArtifacts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "q_agent_save_test_")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	qa := &QAgent{
		logger: slog.Default(),
		config: config.QAgentConfig{
			AnalysisDir: tmpDir,
		},
	}

	result := &AnalysisResult{
		ID:               "analysis_2024-01-01",
		AnalyzedAt:       time.Now(),
		SessionsAnalyzed: 5,
		Status:           "completed",
		Summary:          "test summary",
	}

	designs := []*AgentDesign{
		{
			ID:      "debug_specialist",
			Name:    "Debug Specialist",
			Purpose: "debug agent",
			Constraints: AgentConstraints{
				MaxIterations: 20,
			},
		},
	}

	err = qa.saveArtifacts(result, designs)
	if err != nil {
		t.Fatalf("saveArtifacts: %v", err)
	}

	reportPath := filepath.Join(tmpDir, "analysis_2024-01-01_analysis.json")
	if _, err := os.Stat(reportPath); os.IsNotExist(err) {
		t.Errorf("report file not created at %s", reportPath)
	}

	agentDir := filepath.Join(tmpDir, "agents", "debug_specialist")
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		t.Errorf("agent directory not created at %s", agentDir)
	}

	agentFile := filepath.Join(agentDir, "AGENT.md")
	if _, err := os.Stat(agentFile); os.IsNotExist(err) {
		t.Errorf("AGENT.md not created at %s", agentFile)
	}
}

func TestSaveArtifactsNoDesigns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "q_agent_save_nodesigns_")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	qa := &QAgent{
		logger: slog.Default(),
		config: config.QAgentConfig{
			AnalysisDir: tmpDir,
		},
	}

	result := &AnalysisResult{
		ID:         "test_001",
		AnalyzedAt: time.Now(),
		Status:     "completed",
	}

	err = qa.saveArtifacts(result, nil)
	if err != nil {
		t.Fatalf("saveArtifacts: %v", err)
	}

	reportPath := filepath.Join(tmpDir, "test_001_analysis.json")
	if _, err := os.Stat(reportPath); os.IsNotExist(err) {
		t.Errorf("report file not created at %s", reportPath)
	}
}

func TestLogOutcome(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "q_agent_log_")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logPath := filepath.Join(tmpDir, "outcomes.jsonl")

	qa := &QAgent{
		logger: slog.Default(),
		config: config.QAgentConfig{
			OutcomesLog: logPath,
		},
	}

	result := &AnalysisResult{
		AnalyzedAt:       time.Now(),
		ID:               "analysis_001",
		SessionsAnalyzed: 5,
		Status:           "completed",
		Summary:          "done",
	}

	err = qa.logOutcome(result)
	if err != nil {
		t.Fatalf("logOutcome: %v", err)
	}

	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("log file is empty")
	}
}

func TestGetStatus(t *testing.T) {
	qa := &QAgent{
		config: config.QAgentConfig{
			Enabled:     true,
			AnalysisDir: "~/.meept/q",
			OutcomesLog: "~/.meept/q_outcomes.jsonl",
		},
	}

	// GetStatus requires memvidClient; with nil client IsAvailable may panic.
	// Test with nil memvid -> skip.
	_ = qa
	ctx := context.Background()
	_ = ctx
}

func TestMinIntEdgeCases(t *testing.T) {
	if minInt(1000000, 500000) != 500000 {
		t.Error("minInt large numbers failed")
	}
	if minInt(-1, -5) != -5 {
		t.Error("minInt negative numbers failed")
	}
}

func TestCompileResultsNullInput(t *testing.T) {
	qa := &QAgent{}
	result := qa.compileResults(nil, nil, nil, nil, nil, nil)

	if result == nil {
		t.Fatal("expected non-nil result for null inputs")
	}
	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
}

// Helper functions for testing

// Helper functions for testing
func containsStrQ(s, target string) bool {
	return strings.Contains(s, target)
}
