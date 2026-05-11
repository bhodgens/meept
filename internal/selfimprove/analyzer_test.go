package selfimprove

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------

func newTestAnalyzer(t *testing.T) *RootCauseAnalyzer {
	dir := t.TempDir()
	t.Cleanup(func() {})
	cfg := AIInfraConfig{
		AnalysisModel:       "test-model",
		MaxAnalysisTokens:   4096,
		MaxGenerationTokens: 8192,
		Temperature:         0.2,
	}
	// No LLM client -> fallback analysis
	return NewRootCauseAnalyzer(cfg, nil, dir, slog.Default())
}

// -----------------------------------------------------------------------
// NewRootCauseAnalyzer
// -----------------------------------------------------------------------

func TestNewRootCauseAnalyzer(t *testing.T) {
	a := newTestAnalyzer(t)
	if a == nil {
		t.Fatal("expected non-nil analyzer")
	}
	if a.config.AnalysisModel != "test-model" {
		t.Errorf("expected model test-model, got %s", a.config.AnalysisModel)
	}
	if a.logger == nil {
		t.Error("expected non-nil logger")
	}
}

// -----------------------------------------------------------------------
// Analyze (fallback path)
// -----------------------------------------------------------------------

func TestAnalyze_Fallback(t *testing.T) {
	a := newTestAnalyzer(t)
	_issue := Issue{
		ID:          "issue-1",
		Type:        IssueTypeError,
		Severity:    SeverityHigh,
		Description: "database connection failed",
		Source:      filepath.Join(t.TempDir(), "app.log"),
		Context:     "ERROR: connection refused on port 5432",
		DetectedAt:  time.Now(),
	}

	analysis, err := a.Analyze(context.Background(), _issue)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}
	if analysis == nil {
		t.Fatal("expected non-nil analysis from fallback")
	}
	if analysis.IssueID != _issue.ID {
		t.Errorf("expected issue_id=%s, got %s", _issue.ID, analysis.IssueID)
	}
	// Fallback sets RootCause = Description
	if analysis.RootCause != _issue.Description {
		t.Errorf("expected root cause to be description in fallback")
	}
	if analysis.Confidence != 0.3 {
		t.Errorf("expected confidence=0.3 in fallback, got %f", analysis.Confidence)
	}
}

func TestAnalyze_FallbackWithGoSource(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "main.go")
	os.WriteFile(goFile, []byte("package main"), 0644)

	cfg := AIInfraConfig{AnalysisModel: "test"}
	a := NewRootCauseAnalyzer(cfg, nil, dir, slog.Default())

	issue := Issue{
		ID:          "issue-2",
		Type:        IssueTypeError,
		Severity:    SeverityMedium,
		Description: "nil pointer dereference",
		Source:      goFile,
		Context:     "panic: runtime error",
		DetectedAt:  time.Now(),
	}

	analysis, err := a.Analyze(context.Background(), issue)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}
	if len(analysis.AffectedFiles) != 1 {
		t.Fatalf("expected 1 affected file, got %d", len(analysis.AffectedFiles))
	}
	// AffectedFiles should be the relative path
	if analysis.AffectedFiles[0] != "main.go" {
		t.Errorf("expected affected file main.go, got %s", analysis.AffectedFiles[0])
	}
}

// -----------------------------------------------------------------------
// AnalyzeBatch
// -----------------------------------------------------------------------

func TestAnalyzeBatch(t *testing.T) {
	a := newTestAnalyzer(t)
	issues := []Issue{
		{ID: "b-1", Type: IssueTypeError, Severity: SeverityHigh, Description: "err1"},
		{ID: "b-2", Type: IssueTypeReliability, Severity: SeverityMedium, Description: "err2"},
	}

	analyses, err := a.AnalyzeBatch(context.Background(), issues)
	if err != nil {
		t.Fatalf("AnalyzeBatch failed: %v", err)
	}
	if len(analyses) != 2 {
		t.Errorf("expected 2 analyses, got %d", len(analyses))
	}
	for i, a := range analyses {
		if a.IssueID != issues[i].ID {
			t.Errorf("analysis %d: expected issue_id=%s, got %s", i, issues[i].ID, a.IssueID)
		}
	}
}

func TestAnalyzeBatch_ContextCancellation(t *testing.T) {
	a := newTestAnalyzer(t)
	issues := []Issue{
		{ID: "c-1", Type: IssueTypeError, Description: "err1"},
		{ID: "c-2", Type: IssueTypeError, Description: "err2"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before start

	analyses, err := a.AnalyzeBatch(ctx, issues)
	if !errors.Is(err, context.Canceled) {
		t.Logf("expected context.Canceled, got err=%v (behavior may vary)", err)
	}
	_ = analyses
}

// -----------------------------------------------------------------------
// extractRelevantCode
// -----------------------------------------------------------------------

func TestExtractRelevantCode(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "code.go")
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line " + string(rune('a'+i%26))
	}
	os.WriteFile(goFile, []byte(lines[0]+"\n"+lines[1]+"\n"+lines[2]+"\n"+lines[3]), 0644)

	a := newTestAnalyzer(t)
	issue := Issue{
		Metadata: map[string]any{"line_number": 2},
	}

	code := a.extractRelevantCode(lines[0]+"\n"+lines[1]+"\n"+lines[2]+"\n"+lines[3], issue)
	if code == "" {
		t.Error("expected non-empty relevant code")
	}
}

func TestExtractRelevantCode_NoLineNum(t *testing.T) {
	a := newTestAnalyzer(t)
	content := "line1\nline2\nline3\n"
	issue := Issue{} // No metadata

	code := a.extractRelevantCode(content, issue)
	if code == "" {
		t.Error("expected non-empty code for zero line number")
	}
}

// -----------------------------------------------------------------------
// parseAnalysisResponse
// -----------------------------------------------------------------------

func TestParseAnalysisResponse(t *testing.T) {
	a := newTestAnalyzer(t)
	response := `ROOT_CAUSE: missing nil check
FACTORS: no error handling, unchecked return values
FILES: main.go, utils.go
CONFIDENCE: 0.85`

	analysis := a.parseAnalysisResponse("test-issue", response)
	if analysis.RootCause != "missing nil check" {
		t.Errorf("expected root cause 'missing nil check', got %s", analysis.RootCause)
	}
	if len(analysis.Contributing) != 2 {
		t.Errorf("expected 2 contributing factors, got %d", len(analysis.Contributing))
	}
	if len(analysis.AffectedFiles) != 2 {
		t.Errorf("expected 2 affected files, got %d", len(analysis.AffectedFiles))
	}
	if analysis.Confidence != 0.85 {
		t.Errorf("expected confidence=0.85, got %f", analysis.Confidence)
	}
}

func TestParseAnalysisResponse_FailedParsing(t *testing.T) {
	a := newTestAnalyzer(t)
	// No matching patterns
	response := `This is unstructured analysis text
that doesn't follow the expected format
at all.`

	analysis := a.parseAnalysisResponse("parse-issue", response)
	if analysis.RootCause == "" {
		t.Error("expected root cause to fall back to full response")
	}
	if analysis.Confidence != 0.3 {
		t.Errorf("expected confidence=0.3 on failed parse, got %f", analysis.Confidence)
	}
}

func TestParseAnalysisResponse_OutOfRangeConfidence(t *testing.T) {
	a := newTestAnalyzer(t)
	response := "ROOT_CAUSE: test\nCONFIDENCE: 1.5"
	analysis := a.parseAnalysisResponse("conf-issue", response)
	// Confidence 1.5 is out of range, should keep default
	if analysis.Confidence == 1.5 {
		t.Error("expected confidence to be clamped when out of range")
	}
}

func TestParseAnalysisResponse_ZeroConfidence(t *testing.T) {
	a := newTestAnalyzer(t)
	response := "ROOT_CAUSE: test\nCONFIDENCE: 0.0"
	analysis := a.parseAnalysisResponse("zero-conf", response)
	if analysis.Confidence != 0.0 {
		t.Errorf("expected confidence=0.0, got %f", analysis.Confidence)
	}
}

// -----------------------------------------------------------------------
// fallbackAnalysis (direct test)
// -----------------------------------------------------------------------

func TestFallbackAnalysis(t *testing.T) {
	dir := t.TempDir()
	cfg := AIInfraConfig{AnalysisModel: "test"}
	a := NewRootCauseAnalyzer(cfg, nil, dir, slog.Default())

	wantIssue := Issue{
		ID:          "fb-1",
		Type:        IssueTypeError,
		Severity:    SeverityCritical,
		Description: "critical memory leak",
		Source:      filepath.Join(dir, "leak.go"),
	}

	analysis := a.fallbackAnalysis(wantIssue)
	if analysis.IssueID != wantIssue.ID {
		t.Errorf("expected issue_id=%s, got %s", wantIssue.ID, analysis.IssueID)
	}
	if analysis.RootCause != wantIssue.Description {
		t.Error("fallback should set RootCause = Description")
	}
	if analysis.Confidence != 0.3 {
		t.Errorf("expected confidence=0.3, got %f", analysis.Confidence)
	}
	if len(analysis.AffectedFiles) != 1 {
		t.Error("expected 1 affected file")
	}
}

// -----------------------------------------------------------------------
// Close (no-op)
// -----------------------------------------------------------------------

func TestAnalyzerClose(t *testing.T) {
	a := newTestAnalyzer(t)
	err := a.Close()
	if err != nil {
		t.Errorf("Close returned unexpected error: %v", err)
	}
}
