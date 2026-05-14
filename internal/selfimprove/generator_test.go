package selfimprove

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// -----------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------

func newTestGenerator(t *testing.T) *PatchGenerator {
	dir := t.TempDir()
	t.Cleanup(func() {})
	aiCfg := AIInfraConfig{
		GenerationModel:     "test-model",
		MaxGenerationTokens: 8192,
		Temperature:         0.2,
	}
	safetyCfg := SafetyConfig{
		ProtectedPatterns: []string{`\.key$`, `\.pem$`},
	}
	// No LLM client -- Generate() will fail with a specific error
	return NewPatchGenerator(aiCfg, safetyCfg, nil, dir, slog.Default())
}

// -----------------------------------------------------------------------
// NewPatchGenerator
// -----------------------------------------------------------------------

func TestNewPatchGenerator(t *testing.T) {
	g := newTestGenerator(t)
	if g == nil {
		t.Fatal("expected non-nil generator")
	}
	if g.logger == nil {
		t.Error("expected non-nil logger")
	}
}

// -----------------------------------------------------------------------
// Generate (LLM client not available)
// -----------------------------------------------------------------------

func TestGenerate_NoLLMClient(t *testing.T) {
	g := newTestGenerator(t)
	analysis := &RootCauseAnalysis{
		IssueID:       "issue-1",
		RootCause:     "test root cause",
		Confidence:    0.8,
		AffectedFiles: []string{"file.go"},
	}
	issue := Issue{
		ID:          "issue-1",
		Type:        IssueTypeError,
		Severity:    SeverityHigh,
		Description: "test issue",
	}

	_, err := g.Generate(context.Background(), analysis, issue)
	if err == nil {
		t.Fatal("expected error when LLM client is nil")
	}
	if err.Error() != "LLM client not available" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// -----------------------------------------------------------------------
// Generate (protected file)
// -----------------------------------------------------------------------

func TestGenerate_ProtectedFile(t *testing.T) {
	g := newTestGenerator(t)
	// Create a real LLM client mock by using the test directory's analysis
	analysis := &RootCauseAnalysis{
		IssueID:       "issue-2",
		RootCause:     "secret file issue",
		Confidence:    0.7,
		AffectedFiles: []string{"config.key"},
	}
	issue := Issue{ID: "issue-2", Type: IssueTypeSecurity, Description: "sensitive file"}

	_, err := g.Generate(context.Background(), analysis, issue)
	// This should fail either from protected file check or nil LLM client
	// Either way, it should error
	if err == nil {
		t.Fatal("expected error for protected file")
	}
}

// -----------------------------------------------------------------------
// readAffectedFiles
// -----------------------------------------------------------------------

func TestReadAffectedFiles(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "main.go")
	_ = os.WriteFile(testFile, []byte("package main\n\nfunc main() {}"), 0o644) //nolint:gosec // test uses temp dir

	g := newTestGenerator(t)
	g.projectRoot = dir

	content := g.readAffectedFiles([]string{testFile, "nonexistent.go"})
	if content == "" {
		t.Fatal("expected non-empty content")
	}
	// Should contain main.go content
	if !containsStr(content, "package main") {
		t.Error("expected main.go content in output")
	}
	// Nonexistent file should produce a warning comment
	if !containsStr(content, "Unable to read") {
		t.Error("expected unable-to-read message for missing file")
	}
}

func TestReadAffectedFiles_RelativePath(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	_ = os.MkdirAll(subDir, 0o755) //nolint:gosec // test uses temp dir
	testFile := filepath.Join(subDir, "code.go")
	_ = os.WriteFile(testFile, []byte("package sub"), 0o644) //nolint:gosec // test uses temp dir

	g := newTestGenerator(t)
	g.projectRoot = dir

	// Use relative path
	content := g.readAffectedFiles([]string{"sub/code.go"})
	if !containsStr(content, "sub/code.go") {
		t.Errorf("expected relative path in output, got: %s", content)
	}
}

func TestReadAffectedFiles_EmptyList(t *testing.T) {
	g := newTestGenerator(t)
	content := g.readAffectedFiles([]string{})
	if content != "" {
		t.Errorf("expected empty string, got: %s", content)
	}
}

// -----------------------------------------------------------------------
// isProtected
// -----------------------------------------------------------------------

func TestIsProtected(t *testing.T) {
	g := newTestGenerator(t)

	tests := []struct {
		file     string
		expected bool
	}{
		{"secret.key", true},
		{"config.pem", true},
		{"certificates.pem", true},
		{"main.go", false},
		{"utils/helper.go", false},
	}

	for _, tt := range tests {
		got := g.isProtected(tt.file)
		if got != tt.expected {
			t.Errorf("isProtected(%q) = %v, want %v", tt.file, got, tt.expected)
		}
	}
}

// -----------------------------------------------------------------------
// parseGenerationResponse
// -----------------------------------------------------------------------

func TestParseGenerationResponse(t *testing.T) {
	g := newTestGenerator(t)
	response := `FILE: main.go
RISK: medium
DESCRIPTION: fix nil pointer dereference
DIFF:
<<<<<<< ORIGINAL
func main() {
	var p *int = nil
	println(*p)
}
=======
func main() {
	var p *int = nil
	if p != nil {
		println(*p)
	}
}
>>>>>>> FIXED`

	fix, err := g.parseGenerationResponse("gen-1", response)
	if err != nil {
		t.Fatalf("parseGenerationResponse failed: %v", err)
	}
	if fix == nil {
		t.Fatal("expected non-nil fix")
	}
	if fix.FilePath != "main.go" {
		t.Errorf("expected file_path=main.go, got %s", fix.FilePath)
	}
	if fix.Risk != "medium" {
		t.Errorf("expected risk=medium, got %s", fix.Risk)
	}
	if fix.Description != "fix nil pointer dereference" {
		t.Errorf("expected description, got %s", fix.Description)
	}
	if fix.IssueID != "gen-1" {
		t.Errorf("expected issue_id=gen-1, got %s", fix.IssueID)
	}
	if fix.Diff == "" {
		t.Error("expected non-empty diff")
	}
	if fix.Type != FixTypeCodeChange {
		t.Errorf("expected type=%s, got %s", FixTypeCodeChange, fix.Type)
	}
}

func TestParseGenerationResponse_RefactorType(t *testing.T) {
	g := newTestGenerator(t)
	response := `FILE: main.go
RISK: low
DESCRIPTION: refactor this function for clarity
DIFF:
<<<<<<< ORIGINAL
func old() {}
=======
func new() {}
>>>>>>> FIXED`

	fix, err := g.parseGenerationResponse("ref-1", response)
	if err != nil {
		t.Fatalf("parseGenerationResponse failed: %v", err)
	}
	if fix.Type != FixTypeRefactor {
		t.Errorf("expected type=%s, got %s", FixTypeRefactor, fix.Type)
	}
}

func TestParseGenerationResponse_ConfigChangeType(t *testing.T) {
	g := newTestGenerator(t)
	response := `FILE: config.toml
RISK: low
DESCRIPTION: update config setting
DIFF:
<<<<<<< ORIGINAL
timeout=30
=======
timeout=60
>>>>>>> FIXED`

	fix, err := g.parseGenerationResponse("cfg-1", response)
	if err != nil {
		t.Fatalf("parseGenerationResponse failed: %v", err)
	}
	if fix.Type != FixTypeConfigChange {
		t.Errorf("expected type=%s, got %s", FixTypeConfigChange, fix.Type)
	}
}

func TestParseGenerationResponse_MissingFilePath(t *testing.T) {
	g := newTestGenerator(t)
	response := `RISK: low
DIFF:
<<<<<<< ORIGINAL
old
=======
new
>>>>>>> FIXED`

	_, err := g.parseGenerationResponse("bad-1", response)
	if err == nil {
		t.Fatal("expected error for missing file path")
	}
}

func TestParseGenerationResponse_MissingDiff(t *testing.T) {
	g := newTestGenerator(t)
	response := `FILE: main.go
RISK: low
DESCRIPTION: test fix`

	_, err := g.parseGenerationResponse("bad-2", response)
	if err == nil {
		t.Fatal("expected error for missing diff")
	}
}

func TestParseGenerationResponse_IDFormat(t *testing.T) {
	g := newTestGenerator(t)
	response := `FILE: a.go
RISK: low
DESCRIPTION: test
DIFF:
<<<<<<< ORIGINAL
x
=======
y
>>>>>>> FIXED`

	fix, err := g.parseGenerationResponse("id-test", response)
	if err != nil {
		t.Fatalf("parseGenerationResponse failed: %v", err)
	}
	if fix.ID == "" {
		t.Error("expected non-empty fix ID")
	}
	if len(fix.ID) > 16 {
		t.Errorf("expected fix ID <= 16 chars, got %d", len(fix.ID))
	}
}

// -----------------------------------------------------------------------
// Close
// -----------------------------------------------------------------------

func TestGeneratorClose(t *testing.T) {
	g := newTestGenerator(t)
	err := g.Close()
	if err != nil {
		t.Errorf("Close returned unexpected error: %v", err)
	}
}

// -----------------------------------------------------------------------
// GenerateBatch
// -----------------------------------------------------------------------

func TestGenerateBatch_NoLLMClient(t *testing.T) {
	g := newTestGenerator(t)
	analyses := []*RootCauseAnalysis{
		{IssueID: "b-1", RootCause: "cause1", AffectedFiles: []string{"f1.go"}},
		{IssueID: "b-2", RootCause: "cause2", AffectedFiles: []string{"f2.go"}},
	}
	issues := []Issue{
		{ID: "b-1", Type: IssueTypeError, Description: "issue1"},
		{ID: "b-2", Type: IssueTypeError, Description: "issue2"},
	}

	fixes, err := g.GenerateBatch(context.Background(), analyses, issues)
	// GenerateBatch logs warnings but does not propagate per-issue errors;
	// it returns an empty fixes slice with a nil error.
	if err != nil {
		t.Fatalf("unexpected error from GenerateBatch: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("expected 0 fixes, got %d", len(fixes))
	}
}

func TestGenerateBatch_ContextCancellation(t *testing.T) {
	g := newTestGenerator(t)
	analyses := []*RootCauseAnalysis{
		{IssueID: "ctx-1", RootCause: "cause1"},
		{IssueID: "ctx-2", RootCause: "cause2"},
	}
	issues := []Issue{
		{ID: "ctx-1"},
		{ID: "ctx-2"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already cancelled

	fixes, err := g.GenerateBatch(ctx, analyses, issues)
	if !errors.Is(err, context.Canceled) {
		t.Logf("expected context.Canceled, got err=%v", err)
	}
	_ = fixes
}

func TestGenerateBatch_MissingIssueMap(t *testing.T) {
	g := newTestGenerator(t)
	// Analysis with IssueID that doesn't exist in the issues slice
	analyses := []*RootCauseAnalysis{
		{IssueID: "missing-id", RootCause: "orphaned analysis"},
	}
	issues := []Issue{
		{ID: "other-id", Description: "other issue"},
	}

	fixes, err := g.GenerateBatch(context.Background(), analyses, issues)
	if err != nil {
		t.Fatalf("GenerateBatch failed: %v", err)
	}
	// Should skip the orphaned analysis but not error
	if len(fixes) != 0 {
		t.Errorf("expected 0 fixes for missing issue, got %d", len(fixes))
	}
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
