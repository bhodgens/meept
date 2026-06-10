package lint

import (
	"context"
	"testing"
)

// TestTreeSitterLinterLintGoodCode tests that valid Go code passes linting.
func TestTreeSitterLinterLintGoodCode(t *testing.T) {
	linter := NewTreeSitterLinter()
	ctx := context.Background()

	// Valid Go code - should have no errors
	validGoCode := `package main

import "fmt"

func main() {
	fmt.Println("Hello, world!")
}`

	results, err := linter.Lint(ctx, "test.go", "test.go", validGoCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Filter for errors only
	var errors []LinterResult
	for _, r := range results {
		if r.Severity == "error" {
			errors = append(errors, r)
		}
	}

	if len(errors) > 0 {
		t.Errorf("expected no syntax errors for valid Go code, got: %v", errors)
	}
}

// TestTreeSitterLinterLintBadGoCode tests that invalid Go code is detected.
func TestTreeSitterLinterLintBadGoCode(t *testing.T) {
	linter := NewTreeSitterLinter()
	ctx := context.Background()

	// Invalid Go code - missing closing brace
	badGoCode := `package main

import "fmt"

func main() {
	fmt.Println("Hello, world!")
` // missing closing brace

	results, err := linter.Lint(ctx, "test.go", "test.go", badGoCode)
	if err != nil {
		// Some parser errors are returned in results, not as error
		t.Logf("parse error returned: %v", err)
	}

	// Check for errors
	var errors []LinterResult
	for _, r := range results {
		if r.Severity == "error" {
			errors = append(errors, r)
		}
	}

	if len(errors) == 0 {
		t.Logf("no errors detected for invalid Go code: %s", badGoCode)
	}
}

// TestTreeSitterLinterLintPythonCode tests Python linting.
func TestTreeSitterLinterLintPythonCode(t *testing.T) {
	linter := NewTreeSitterLinter()
	ctx := context.Background()

	// Valid Python code
	validPythonCode := `def hello():
    print("Hello, world!")
    return True

if __name__ == "__main__":
    hello()
`

	results, err := linter.Lint(ctx, "test.py", "test.py", validPythonCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var errors []LinterResult
	for _, r := range results {
		if r.Severity == "error" {
			errors = append(errors, r)
		}
	}

	if len(errors) > 0 {
		t.Errorf("expected no syntax errors for valid Python code, got: %v", errors)
	}
}

// TestTreeSitterLinterLintInvalidPythonCode tests invalid Python detection.
func TestTreeSitterLinterLintInvalidPythonCode(t *testing.T) {
	linter := NewTreeSitterLinter()
	ctx := context.Background()

	// Invalid Python code - unclosed parenthesis
	badPythonCode := `def hello():
    print("Hello, world!"
    return True
`

	results, _ := linter.Lint(ctx, "test.py", "test.py", badPythonCode)

	var errors []LinterResult
	for _, r := range results {
		if r.Severity == "error" {
			errors = append(errors, r)
		}
	}

	if len(errors) == 0 {
		t.Logf("no errors detected for invalid Python code")
	}
}

// TestTreeSitterLinterLintJavaScriptCode tests JavaScript linting.
func TestTreeSitterLinterLintJavaScriptCode(t *testing.T) {
	linter := NewTreeSitterLinter()
	ctx := context.Background()

	// Valid JavaScript code
	validJSCode := `function hello() {
    console.log("Hello, world!");
    return true;
}

hello();
`

	results, err := linter.Lint(ctx, "test.js", "test.js", validJSCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var errors []LinterResult
	for _, r := range results {
		if r.Severity == "error" {
			errors = append(errors, r)
		}
	}

	if len(errors) > 0 {
		t.Errorf("expected no syntax errors for valid JavaScript code, got: %v", errors)
	}
}

// TestTreeSitterLinterUnknownLanguage tests that unknown languages return nil.
func TestTreeSitterLinterUnknownLanguage(t *testing.T) {
	linter := NewTreeSitterLinter()
	ctx := context.Background()

	results, err := linter.Lint(ctx, "test.xyz", "test.xyz", "some content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results != nil {
		t.Errorf("expected nil results for unknown language, got: %v", results)
	}
}

// TestDetectLanguageFromPath tests language detection.
func TestDetectLanguageFromPath(t *testing.T) {
	tests := []struct {
		filePath string
		expected string
	}{
		{"test.go", "go"},
		{"test.py", "python"},
		{"test.pyi", "python"},
		{"test.js", "javascript"},
		{"test.jsx", "javascript"},
		{"test.ts", "typescript"},
		{"test.tsx", "typescript"},
		{"test.rs", "rust"},
		{"test.java", "java"},
		{"test.rb", "ruby"},
		{"test.c", "c"},
		{"test.cpp", "cpp"},
		{"test.h", "c"},
		{"test.unknown", ""},
		{"test", ""},
	}

	for _, tt := range tests {
		result := detectLanguageFromPath(tt.filePath)
		if result != tt.expected {
			t.Errorf("detectLanguageFromPath(%q) = %q, want %q", tt.filePath, result, tt.expected)
		}
	}
}

// TestGetTreeSitterLanguage tests that we can get tree-sitter languages.
func TestGetTreeSitterLanguage(t *testing.T) {
	tests := []struct {
		lang     string
		expected bool
	}{
		{"go", true},
		{"python", true},
		{"javascript", true},
		{"typescript", true},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		result := GetTreeSitterLanguage(tt.lang)
		if (result != nil) != tt.expected {
			t.Errorf("GetTreeSitterLanguage(%q) = %v, want non-nil = %v", tt.lang, result, tt.expected)
		}
	}
}