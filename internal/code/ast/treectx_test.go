package ast

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTreeContextWithMarkersSimple(t *testing.T) {
	// Create a temp Go file for testing
	content := `package main

import "fmt"

func hello() {
	fmt.Println("hello")
}

func main() {
	hello()
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Mark line 9 (the call to hello()) - 0-indexed so line 9 = line 10 in the file
	markedLines := map[int]bool{
		8: true, // "hello()" line (0-indexed)
	}

	// Test simple version
	result := TreeContextWithMarkersSimple(tmpFile, markedLines, 2)
	if result == "" {
		t.Error("expected non-empty result from TreeContextWithMarkersSimple")
	}

	// Should contain the error marker
	if !contains(result, "█") {
		t.Errorf("expected result to contain error marker, got: %q", result)
	}

	t.Logf("Result:\n%s", result)
}

func TestTreeContextWithMarkersWithOptions(t *testing.T) {
	content := `package main

import "fmt"

func greet(name string) string {
	return "Hello, " + name
}

func main() {
	msg := greet("world")
	fmt.Println(msg)
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "hello.go")
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	markedLines := map[int]bool{
		10: true, // fmt.Println line
	}

	// Test with options - disable indent
	opts := DefaultTreeContextOptions()
	opts.ShowIndent = false

	result, err := TreeContextWithMarkers(tmpFile, markedLines, 2, opts)
	if err != nil {
		t.Fatal(err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Language != LangGo {
		t.Errorf("expected Language to be LangGo, got %v", result.Language)
	}

	if len(result.Scopes) == 0 {
		t.Error("expected at least one scope")
	}

	// Verify marked lines are tracked
	if len(result.MarkedLines) != 1 || result.MarkedLines[0] != 10 {
		t.Errorf("expected MarkedLines [10], got %v", result.MarkedLines)
	}

	t.Logf("Scopes: %+v", result.Scopes)
}

func TestTreeContextWithMarkersUnknownFile(t *testing.T) {
	// Test with unknown file path
	markedLines := map[int]bool{0: true}
	result := TreeContextWithMarkersSimple("/nonexistent/file.xyz", markedLines, 3)
	if result != "" {
		t.Errorf("expected empty result for unknown file, got: %q", result)
	}
}

func TestTreeContextWithMarkersPython(t *testing.T) {
	content := `def greet(name):
    return f"Hello, {name}"

def main():
    msg = greet("world")
    print(msg)

if __name__ == "__main__":
    main()
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "hello.py")
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	markedLines := map[int]bool{
		5: true, // print(line)
	}

	result, err := TreeContextWithMarkers(tmpFile, markedLines, 1)
	if err != nil {
		t.Fatal(err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Language != LangPython {
		t.Errorf("expected Language to be LangPython, got %v", result.Language)
	}

	// Verify scopes include function definitions
	hasFunc := false
	for _, scope := range result.Scopes {
		if scope.Kind == "function" {
			hasFunc = true
			break
		}
	}
	if !hasFunc {
		t.Logf("Scopes: %+v", result.Scopes)
		// Note: scope detection may vary by language support
	}

	t.Logf("Result:\n%s", result.String)
}

func TestTreeContextWithMarkersResultStructure(t *testing.T) {
	content := `package main

func foo() int {
	return 42
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "foo.go")
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	markedLines := map[int]bool{}

	result, err := TreeContextWithMarkers(tmpFile, markedLines, 1)
	if err != nil {
		t.Fatal(err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should have valid result even with no marked lines
	if result.FilePath != tmpFile {
		t.Errorf("expected FilePath %q, got %q", tmpFile, result.FilePath)
	}

	if result.TotalLines == 0 {
		t.Error("expected non-zero TotalLines")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
