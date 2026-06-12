package agent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/lint"
)

func TestReflectionConfig_Defaults(t *testing.T) {
	config := DefaultReflectionConfig()

	if config.MaxReflections != 3 {
		t.Errorf("expected MaxReflections=3, got %d", config.MaxReflections)
	}
	if !config.AutoLint {
		t.Error("expected AutoLint=true")
	}
	if !config.AutoTest {
		t.Error("expected AutoTest=true")
	}
}

func TestReflectionEngine_NewReflectionEngine(t *testing.T) {
	logger := slog.Default()
	linter := lint.NewRegistry()
	testRunner := lint.NewTestRunner(logger)

	// Test with nil LLM client (should still work, just won't fix)
	engine := NewReflectionEngine(logger, linter, testRunner, nil)

	if engine.config.MaxReflections != 3 {
		t.Errorf("expected MaxReflections=3, got %d", engine.config.MaxReflections)
	}
	if engine.linter == nil {
		t.Error("expected linter to be set")
	}
	if engine.testRunner == nil {
		t.Error("expected testRunner to be set")
	}
	if engine.llmClient != nil {
		t.Error("expected llmClient to be nil")
	}
}

func TestReflectionEngine_NewReflectionEngineWithConfig(t *testing.T) {
	logger := slog.Default()
	config := ReflectionConfig{
		MaxReflections: 5,
		AutoLint:       false,
		AutoTest:       true,
		WorkDir:        "/tmp/test",
	}

	engine := NewReflectionEngineWithConfig(logger, nil, nil, nil, config)

	if engine.config.MaxReflections != 5 {
		t.Errorf("expected MaxReflections=5, got %d", engine.config.MaxReflections)
	}
	if engine.config.AutoLint != false {
		t.Error("expected AutoLint=false")
	}
	if engine.config.AutoTest != true {
		t.Error("expected AutoTest=true")
	}
	if engine.config.WorkDir != "/tmp/test" {
		t.Errorf("expected WorkDir=/tmp/test, got %s", engine.config.WorkDir)
	}
}

func TestReflectionEngine_SetEditAvailability(t *testing.T) {
	engine := NewReflectionEngine(slog.Default(), nil, nil, nil)

	engine.SetEditAvailability(true)
	if !engine.editAvail {
		t.Error("expected editAvail=true")
	}

	engine.SetEditAvailability(false)
	if engine.editAvail {
		t.Error("expected editAvail=false")
	}
}

func TestReflectionResult_Defaults(t *testing.T) {
	result := &ReflectionResult{}

	if result.Fixed {
		t.Error("expected Fixed=false by default")
	}
	if result.Iterations != 0 {
		t.Errorf("expected Iterations=0, got %d", result.Iterations)
	}
	if result.GaveUp {
		t.Error("expected GaveUp=false by default")
	}
}

func TestReflectionEngine_RunReflection_Empty(t *testing.T) {
	logger := slog.Default()

	// Create engine with defaults - no linter/test runner means no issues
	engine := NewReflectionEngine(logger, nil, nil, nil)
	engine.config.AutoLint = false
	engine.config.AutoTest = false

	result, err := engine.RunReflection(context.Background(), []string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// With no linters/tests enabled, it should succeed immediately
	if !result.Fixed {
		t.Error("expected Fixed=true when all checks are disabled")
	}
	if result.Iterations != 1 {
		t.Errorf("expected Iterations=1, got %d", result.Iterations)
	}
}

func TestReflectionEngine_RunReflection_LintOnly(t *testing.T) {
	logger := slog.Default()

	// Create engine with linter but no test runner
	linter := lint.NewRegistry()
	engine := NewReflectionEngine(logger, linter, nil, nil)
	engine.config.AutoLint = true
	engine.config.AutoTest = false

	// Should succeed since there are no files to lint
	result, err := engine.RunReflection(context.Background(), []string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !result.Fixed {
		t.Error("expected Fixed=true with no files")
	}
}

func TestDetectLanguageFromExt(t *testing.T) {
	tests := []struct {
		filePath string
		expected string
	}{
		{"test.go", "go"},
		{"test.py", "python"},
		{"test.js", "javascript"},
		{"test.ts", "typescript"},
		{"test.tsx", "typescript"},
		{"test.jsx", "javascript"},
		{"test.txt", ""},
		{"test", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := detectLanguageFromExt(tt.filePath)
		if result != tt.expected {
			t.Errorf("detectLanguageFromExt(%q) = %q, expected %q", tt.filePath, result, tt.expected)
		}
	}
}

func TestUniqueFilesFromErrors(t *testing.T) {
	errors := []lint.LinterResult{
		{File: "file1.go"},
		{File: "file2.go"},
		{File: "file1.go"}, // duplicate
		{File: "file3.go"},
	}

	files := uniqueFilesFromErrors(errors)

	if len(files) != 3 {
		t.Errorf("expected 3 unique files, got %d", len(files))
	}

	// Check that we have the expected files
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}

	if !fileSet["file1.go"] {
		t.Error("expected file1.go in result")
	}
	if !fileSet["file2.go"] {
		t.Error("expected file2.go in result")
	}
	if !fileSet["file3.go"] {
		t.Error("expected file3.go in result")
	}
}

func TestFilterErrorsForFile(t *testing.T) {
	errors := []lint.LinterResult{
		{File: "file1.go", Line: 10},
		{File: "file1.go", Line: 20},
		{File: "file2.go", Line: 5},
	}

	filtered := filterErrorsForFile(errors, "file1.go")

	if len(filtered) != 2 {
		t.Errorf("expected 2 errors for file1.go, got %d", len(filtered))
	}

	filtered = filterErrorsForFile(errors, "file3.go")
	if len(filtered) != 0 {
		t.Errorf("expected 0 errors for file3.go, got %d", len(filtered))
	}
}

func TestReflectionTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel... (truncated)"},
		{"", 10, ""},
		{"hello", 0, "hello"},
		{"hi", 2, "hi"},
		{"long string here", 10, "long strin... (truncated)"},
	}

	for _, tt := range tests {
		result := reflectionTruncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("reflectionTruncate(%q, %d) = %q, expected %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestDetectProjectLanguage(t *testing.T) {
	// Test with Go files
	lang := detectProjectLanguage([]string{"main.go", "utils.go"}, ".")
	if lang != "go" {
		t.Errorf("expected go, got %s", lang)
	}

	// Test with Python files
	lang = detectProjectLanguage([]string{"main.py", "utils.py"}, ".")
	if lang != "python" {
		t.Errorf("expected python, got %s", lang)
	}

	// Test with JS files
	lang = detectProjectLanguage([]string{"index.js", "app.js"}, ".")
	if lang != "javascript" {
		t.Errorf("expected javascript, got %s", lang)
	}

	// Test with unknown files
	lang = detectProjectLanguage([]string{"README.txt"}, ".")
	if lang != "" {
		t.Errorf("expected empty, got %s", lang)
	}
}

// TestParseFixResponse_MultiFile_ParsesPerFileCodeBlocks tests that the function
// correctly parses markdown code blocks with file path annotations and returns
// only the files that are actually referenced in the LLM response.
func TestParseFixResponse_MultiFile_ParsesPerFileCodeBlocks(t *testing.T) {
	logger := slog.Default()
	engine := NewReflectionEngine(logger, nil, nil, nil)

	llmResponse := "// File: handler.go\n```go\nfunc handle() {\n\tfmt.Println(\"handler\")\n}\n```\n\n// File: utils.go\n```go\nfunc util() {\n\tfmt.Println(\"utils\")\n}\n```"

	originalFiles := []string{"handler.go", "utils.go"}
	attempt := engine.parseFixResponse(llmResponse, originalFiles)

	if attempt == nil {
		t.Fatal("expected non-nil FixAttempt")
	}

	if len(attempt.Files) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(attempt.Files), attempt.Files)
	}
}

// TestParseFixResponse_FiltersUnreferencedFiles tests that files not mentioned
// in the LLM response are excluded from the FixAttempt.
func TestParseFixResponse_FiltersUnreferencedFiles(t *testing.T) {
	logger := slog.Default()
	engine := NewReflectionEngine(logger, nil, nil, nil)

	llmResponse := "Here's the fix for handler.go:\n```go\nfunc handle() {}\n```"

	originalFiles := []string{"handler.go", "unused.go"}
	attempt := engine.parseFixResponse(llmResponse, originalFiles)

	if attempt == nil {
		t.Fatal("expected non-nil FixAttempt")
	}

	if len(attempt.Files) != 1 {
		t.Errorf("expected 1 file (handler.go), got %d: %v", len(attempt.Files), attempt.Files)
	}
}

// TestParseFixResponse_ToolCallJSON tests parsing of file_edit tool call JSON blocks.
func TestParseFixResponse_ToolCallJSON(t *testing.T) {
	logger := slog.Default()
	engine := NewReflectionEngine(logger, nil, nil, nil)

	llmResponse := "I'll fix the file:\n```tool_call\n{\"name\":\"file_edit\",\"arguments\":{\"filepath\":\"handler.go\",\"content\":\"func handle() {}\"}}\n```"

	originalFiles := []string{"handler.go", "other.go"}
	attempt := engine.parseFixResponse(llmResponse, originalFiles)

	if attempt == nil {
		t.Fatal("expected non-nil FixAttempt")
	}

	if len(attempt.Files) != 1 {
		t.Errorf("expected 1 file, got %d: %v", len(attempt.Files), attempt.Files)
	}
}
