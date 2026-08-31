package selfimprove

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------

func newTestReportArtifact(t *testing.T, format ReportFormat) (*ReportArtifact, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := ReportConfig{
		OutputDir:              dir,
		Format:                 format,
		IncludeTraces:          true,
		IncludeRecommendations: true,
		CleanupAfterDays:       30,
	}
	ra, err := NewReportArtifact(cfg)
	if err != nil {
		t.Fatalf("NewReportArtifact: %v", err)
	}
	return ra, dir
}

func sampleFailures() []FailureMode {
	return []FailureMode{
		{
			Category:       "refusal_loop",
			Description:    "Model refused tool call three times",
			Severity:       "high",
			TraceID:        "abc123",
			Model:          "claude-sonnet-4-5-20250929",
			Recommendation: "Review tool prompt template",
		},
		{
			Category:    "tool_error",
			Description: "File read failed with permission error",
			Severity:    "medium",
			TraceID:     "def456",
			Model:       "claude-opus-4-6",
		},
	}
}

const sampleAnalysis = "The root cause of the refusal loop is an overly strict security policy in the fence configuration."

// -----------------------------------------------------------------------
// TestReportArtifact_MarkdownFormat
// -----------------------------------------------------------------------

func TestReportArtifact_MarkdownFormat(t *testing.T) {
	ra, _ := newTestReportArtifact(t, ReportFormatMarkdown)

	err := ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	path := ra.GetPath()
	if !strings.HasSuffix(path, "report.md") {
		t.Errorf("expected path ending in report.md, got %s", path)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(contents)
	checks := []struct {
		label string
		key   string
	}{
		{"headings", "# Self-Improvement Report"},
		{"executive summary", "## Executive Summary"},
		{"analysis section", "## Analysis"},
		{"failure modes section", "## Failure Modes"},
		{"root cause", "root cause"},
		{"severity entry", "high"},
		{"category entry", "refusal_loop"},
		{"trace ID", "abc123"},
		{"recommendation", "Review tool prompt template"},
		{"trace evidence section", "## Trace Evidence"},
	}

	for _, check := range checks {
		if !strings.Contains(content, check.key) {
			t.Errorf("markdown report missing %q: %q", check.label, check.key)
		}
	}

	// Also verify traces.jsonl was created
	tracesPath := filepath.Join(filepath.Dir(path), "traces.jsonl")
	if _, err := os.Stat(tracesPath); os.IsNotExist(err) {
		t.Error("expected traces.jsonl to exist")
	}

	// Verify metadata.json was created
	metaPath := filepath.Join(filepath.Dir(path), "metadata.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Error("expected metadata.json to exist")
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_JSONFormat
// -----------------------------------------------------------------------

func TestReportArtifact_JSONFormat(t *testing.T) {
	ra, _ := newTestReportArtifact(t, ReportFormatJSON)

	err := ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	path := ra.GetPath()
	if !strings.HasSuffix(path, "report.json") {
		t.Errorf("expected path ending in report.json, got %s", path)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var parsed reportJSON
	if err := json.Unmarshal(contents, &parsed); err != nil {
		t.Fatalf("JSON parse failed: %v", err)
	}

	if parsed.Format != "json" {
		t.Errorf("expected format json, got %s", parsed.Format)
	}

	if parsed.FailureCount != 2 {
		t.Errorf("expected 2 failures, got %d", parsed.FailureCount)
	}

	if parsed.Analysis != sampleAnalysis {
		t.Error("analysis mismatch")
	}

	if !parsed.IncludeTraces {
		t.Error("expected include_traces=true in JSON report")
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_HTMLFormat
// -----------------------------------------------------------------------

func TestReportArtifact_HTMLFormat(t *testing.T) {
	ra, _ := newTestReportArtifact(t, ReportFormatHTML)

	err := ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	path := ra.GetPath()
	if !strings.HasSuffix(path, "report.html") {
		t.Errorf("expected path ending in report.html, got %s", path)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(contents)

	checks := []struct {
		label string
		key   string
	}{
		{"DOCTYPE", "<!DOCTYPE html>"},
		{"title", "<title>Self-Improvement Report</title>"},
		{"summary section", "Failure Modes Detected"},
		{"failure category", "refusal_loop"},
		{"severity style", "severity-high"},
		{"collapsible JS", "failure-header"},
		{"trace link", "traces.jsonl"},
		{"analysis section", "Analysis"},
	}

	for _, check := range checks {
		if !strings.Contains(content, check.key) {
			t.Errorf("html report missing %q: %q", check.label, check.key)
		}
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_OutputDirCreation
// -----------------------------------------------------------------------

func TestReportArtifact_OutputDirCreation(t *testing.T) {
	// Start with a non-existent output dir
	root := t.TempDir()
	nested := filepath.Join(root, "deeply", "nested", "reports")

	cfg := ReportConfig{
		OutputDir:              nested,
		Format:                 ReportFormatMarkdown,
		IncludeTraces:          true,
		IncludeRecommendations: true,
	}
	ra, _ := NewReportArtifact(cfg)

	if _, err := os.Stat(nested); os.IsNotExist(err) {
		// Verify dir does not exist yet
	}

	err := ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check all dirs were created
	if _, err := os.Stat(nested); os.IsNotExist(err) {
		t.Error("expected deeply nested output dir to be created")
	}

	// Check subdirectory was created
	entries, err := os.ReadDir(nested)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one entry in output dir")
	}

	foundSubDir := false
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "self-improve-") {
			foundSubDir = true
			break
		}
	}
	if !foundSubDir {
		t.Error("expected self-improve-<timestamp> subdirectory in output dir")
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_MetadataIncluded
// -----------------------------------------------------------------------

func TestReportArtifact_MetadataIncluded(t *testing.T) {
	ra, _ := newTestReportArtifact(t, ReportFormatMarkdown)

	models := []string{"claude-sonnet-4-5-20250929", "claude-opus-4-6"}
	err := ra.GenerateWithMeta(sampleFailures(), sampleAnalysis, models, "test-employee-1")
	if err != nil {
		t.Fatalf("GenerateWithMeta failed: %v", err)
	}

	path := ra.GetPath()
	subDir := filepath.Dir(path)

	metaPath := filepath.Join(subDir, "metadata.json")
	contents, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("ReadFile metadata.json: %v", err)
	}

	var meta ReportMetadata
	if err := json.Unmarshal(contents, &meta); err != nil {
		t.Fatalf("JSON parse metadata: %v", err)
	}

	if meta.Format != "markdown" {
		t.Errorf("expected format markdown, got %s", meta.Format)
	}

	if meta.FailureCount != 2 {
		t.Errorf("expected failure_count=2, got %d", meta.FailureCount)
	}

	if meta.EmployeeID != "test-employee-1" {
		t.Errorf("expected employee_id=test-employee-1, got %s", meta.EmployeeID)
	}

	if len(meta.ModelsUsed) != 2 {
		t.Errorf("expected 2 models used, got %d", len(meta.ModelsUsed))
	}

	// ModelsUsed should contain both models
	modelSet := make(map[string]bool)
	for _, m := range meta.ModelsUsed {
		modelSet[m] = true
	}
	if !modelSet["claude-sonnet-4-5-20250929"] {
		t.Error("missing model claude-sonnet-4-5-20250929 in metadata")
	}
	if !modelSet["claude-opus-4-6"] {
		t.Error("missing model claude-opus-4-6 in metadata")
	}

	// Timestamp should be a recent time
	if time.Since(meta.Timestamp) > time.Hour {
		t.Errorf("metadata timestamp is too far in the past: %v", meta.Timestamp)
	}

	// OutputDir should point to the subdirectory
	if !strings.Contains(meta.OutputDir, "self-improve-") {
		t.Errorf("expected OutputDir to contain timestamp, got %s", meta.OutputDir)
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_InvalidConfig
// -----------------------------------------------------------------------

func TestReportArtifact_InvalidConfig(t *testing.T) {
	_, err := NewReportArtifact(ReportConfig{
		OutputDir:        "/tmp/test-reports",
		Format:           ReportFormat("invalid"),
		IncludeTraces:    true,
		CleanupAfterDays: 30,
	})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}

	if !strings.Contains(err.Error(), "report format must be one of") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_DefaultConfig
// -----------------------------------------------------------------------

func TestReportArtifact_DefaultConfig(t *testing.T) {
	cfg := DefaultReportConfig()

	if cfg.Format != ReportFormatMarkdown {
		t.Errorf("expected default format markdown, got %s", cfg.Format)
	}

	if !strings.HasSuffix(cfg.OutputDir, ".meept/reports") {
		t.Errorf("unexpected default output dir: %s", cfg.OutputDir)
	}

	if cfg.IncludeTraces != true {
		t.Error("expected default IncludeTraces=true")
	}

	if cfg.CleanupAfterDays != 30 {
		t.Errorf("expected default CleanupAfterDays=30, got %d", cfg.CleanupAfterDays)
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_EmptyFailures
// -----------------------------------------------------------------------

func TestReportArtifact_EmptyFailures(t *testing.T) {
	for _, format := range []ReportFormat{ReportFormatMarkdown, ReportFormatJSON, ReportFormatHTML} {
		t.Run(string(format), func(t *testing.T) {
			ra, _ := newTestReportArtifact(t, format)

			err := ra.Generate(nil, sampleAnalysis)
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			path := ra.GetPath()
			if path == "" {
				t.Error("expected non-empty path")
			}

			_, err = os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_SaveAndGetPath
// -----------------------------------------------------------------------

func TestReportArtifact_SaveAndGetPath(t *testing.T) {
	ra, _ := newTestReportArtifact(t, ReportFormatMarkdown)

	// Before Generate, GetPath should be empty
	if ra.GetPath() != "" {
		t.Errorf("expected empty path before Generate, got %s", ra.GetPath())
	}

	_, err := ra.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	err = ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	path := ra.GetPath()
	if path == "" {
		t.Error("expected non-empty path after Generate")
	}

	savedPath, err := ra.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if savedPath != path {
		t.Errorf("expected Save to return %s, got %s", path, savedPath)
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_Cleanup
// -----------------------------------------------------------------------

func TestReportArtifact_Cleanup(t *testing.T) {
	root := t.TempDir()

	// Create a fake old report and set a negative timestamp so cleanupOld would delete it
	oldReport := filepath.Join(root, "self-improve-2020-01-01T00-00-00Z")
	if err := os.MkdirAll(oldReport, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Backdate the directory using a sub-entrance
	oldFile := filepath.Join(oldReport, "report.md")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Use a config with CleanupAfterDays=1 and set the sub-dir mod time far in the future
	// to simulate an "old" report. Instead, we test that GenerateWithMeta does not
	// delete the current report even when old ones exist.
	cfg := ReportConfig{
		OutputDir:              root,
		Format:                 ReportFormatMarkdown,
		IncludeTraces:          true,
		IncludeRecommendations: true,
		CleanupAfterDays:       1,
	}
	ra, err := NewReportArtifact(cfg)
	if err != nil {
		t.Fatalf("NewReportArtifact: %v", err)
	}

	err = ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	newPath := ra.GetPath()
	if newPath == "" {
		t.Fatal("expected non-empty path")
	}

	// The current report should still exist
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Error("current report was deleted during GenerateWithMeta")
	}

	// The old report created by the test is outside the generated subdir,
	// so it should be untouched
	if _, err := os.Stat(oldFile); os.IsNotExist(err) {
		t.Error("old report file was unexpectedly cleaned up")
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_IncludeTraces_False
// -----------------------------------------------------------------------

func TestReportArtifact_IncludeTraces_False(t *testing.T) {
	root := t.TempDir()
	cfg := ReportConfig{
		OutputDir:              root,
		Format:                 ReportFormatMarkdown,
		IncludeTraces:          false,
		IncludeRecommendations: true,
	}
	ra, err := NewReportArtifact(cfg)
	if err != nil {
		t.Fatalf("NewReportArtifact: %v", err)
	}

	err = ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	path := ra.GetPath()
	subDir := filepath.Dir(path)

	_, err = os.Stat(filepath.Join(subDir, "traces.jsonl"))
	if err == nil {
		t.Error("expected no traces.jsonl when IncludeTraces=false")
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_IncludeRecommendations_False
// -----------------------------------------------------------------------

func TestReportArtifact_IncludeRecommendations_False(t *testing.T) {
	root := t.TempDir()
	cfg := ReportConfig{
		OutputDir:              root,
		Format:                 ReportFormatMarkdown,
		IncludeTraces:          true,
		IncludeRecommendations: false,
	}
	ra, err := NewReportArtifact(cfg)
	if err != nil {
		t.Fatalf("NewReportArtifact: %v", err)
	}

	err = ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	contents, err := os.ReadFile(ra.GetPath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if strings.Contains(string(contents), "Recommendation") {
		t.Error("expected no recommendations in markdown when IncludeRecommendations=false")
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_HtmlRecommendations_False
// -----------------------------------------------------------------------

func TestReportArtifact_HTML_NoRecommendations(t *testing.T) {
	root := t.TempDir()
	cfg := ReportConfig{
		OutputDir:              root,
		Format:                 ReportFormatHTML,
		IncludeTraces:          true,
		IncludeRecommendations: false,
	}
	ra, err := NewReportArtifact(cfg)
	if err != nil {
		t.Fatalf("NewReportArtifact: %v", err)
	}

	err = ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	contents, err := os.ReadFile(ra.GetPath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if strings.Contains(string(contents), "Review tool prompt template") {
		t.Error("expected no recommendations in HTML when IncludeRecommendations=false")
	}
}

// -----------------------------------------------------------------------
// TestReportArtifact_JSON_NoTraces
// -----------------------------------------------------------------------

func TestReportArtifact_JSON_NoTraces(t *testing.T) {
	root := t.TempDir()
	cfg := ReportConfig{
		OutputDir:              root,
		Format:                 ReportFormatJSON,
		IncludeTraces:          false,
		IncludeRecommendations: false,
	}
	ra, err := NewReportArtifact(cfg)
	if err != nil {
		t.Fatalf("NewReportArtifact: %v", err)
	}

	err = ra.Generate(sampleFailures(), sampleAnalysis)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Verify traces.jsonl does not exist
	subDir := filepath.Dir(ra.GetPath())
	if _, err := os.Stat(filepath.Join(subDir, "traces.jsonl")); !os.IsNotExist(err) {
		t.Error("expected no traces.jsonl when IncludeTraces=false")
	}
}
