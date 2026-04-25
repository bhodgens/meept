package agent

import (
	"strings"
	"testing"
)

func TestLooksLikeCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "go function code",
			input:    "package main\n\nfunc hello() {\n\tfmt.Println(\"hello\")\n}\n",
			expected: true,
		},
		{
			name:     "python class code",
			input:    "class Foo:\n    def bar(self):\n        return 42\n",
			expected: true,
		},
		{
			name:     "rust code",
			input:    "fn main() {\n    let x = 42;\n    println!(\"{}\", x);\n}\n",
			expected: true,
		},
		{
			name:     "javascript code",
			input:    "const x = function() {\n  return 42;\n};\n",
			expected: true,
		},
		{
			name:     "plain text",
			input:    "This is just some regular text that does not contain any code at all.",
			expected: false,
		},
		{
			name:     "short string",
			input:    "func x()",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "json output",
			input:    `{"status": "ok", "data": {"items": [1, 2, 3]}, "message": "success"}`,
			expected: false,
		},
		{
			name:     "go struct with methods",
			input:    "type Server struct {\n\tAddr string\n}\n\nfunc (s *Server) Start() error {\n\treturn nil\n}\n",
			expected: true,
		},
		{
			name:     "html-like text",
			input:    "<html><body><h1>Hello</h1></body></html>",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeCode(tt.input)
			if result != tt.expected {
				t.Errorf("looksLikeCode(%q) = %v, want %v", tt.input[:min(50, len(tt.input))], result, tt.expected)
			}
		})
	}
}

func TestCompressCodeResult_GoCode(t *testing.T) {
	goCode := `package main

import "fmt"

// Hello says hello
func Hello(name string) string {
	result := "Hello, " + name
	for i := 0; i < 10; i++ {
		result += fmt.Sprintf(" line %d", i)
	}
	return result
}

// Goodbye says goodbye
func Goodbye(name string) string {
	result := "Goodbye, " + name
	for i := 0; i < 10; i++ {
		result += fmt.Sprintf(" line %d", i)
	}
	return result
}

type Server struct {
	Addr string
	Port int
}

func (s *Server) Start() error {
	fmt.Printf("Starting server on %s:%d\n", s.Addr, s.Port)
	return nil
}
`

	// Small budget should trigger compression
	result := compressCodeResult(goCode, 300)

	// Should be shorter than original
	if len(result) >= len(goCode) {
		t.Errorf("compressCodeResult should shorten code, got len %d >= original %d", len(result), len(goCode))
	}

	// Should preserve package declaration
	if !strings.Contains(result, "package main") {
		t.Error("compressed result should preserve package declaration")
	}

	// Should preserve import
	if !strings.Contains(result, "import") {
		t.Error("compressed result should preserve import")
	}

	// Should preserve function signatures
	if !strings.Contains(result, "Hello(") {
		t.Error("compressed result should preserve function signatures")
	}
	if !strings.Contains(result, "Goodbye(") {
		t.Error("compressed result should preserve function signatures")
	}

	// Should contain compression marker
	if !strings.Contains(result, "...[compressed]") {
		t.Error("compressed result should contain compression marker")
	}

	// Should preserve type definition
	if !strings.Contains(result, "Server") {
		t.Error("compressed result should preserve type definitions")
	}
}

func TestCompressCodeResult_ShortCode_NoCompression(t *testing.T) {
	code := "package main\n\nfunc hello() { return 42 }\n"
	result := compressCodeResult(code, 1000)

	// Should return original if it fits
	if result != code {
		t.Errorf("expected no compression for short code, got: %q", result)
	}
}

func TestCompressCodeResult_UnknownLanguage_Fallback(t *testing.T) {
	// Code with no recognizable language indicators
	unknownCode := strings.Repeat("x(y,z) { some very long body that repeats over and over ", 50)

	result := compressCodeResult(unknownCode, 200)

	// Should fall back to truncation
	if !strings.Contains(result, "...[truncated") {
		t.Error("expected truncation fallback for unknown language")
	}

	if len(result) > 250 { // Allow some slack for the marker
		t.Errorf("fallback truncation should keep output short, got len %d", len(result))
	}
}

func TestCompressCodeResult_PythonCode(t *testing.T) {
	pythonCode := `class DataProcessor:
    """Process some data."""

    def __init__(self, name):
        self.name = name
        self.data = []
        for i in range(100):
            self.data.append(i * 2)

    def process(self, items):
        result = []
        for item in items:
            processed = self.transform(item)
            result.append(processed)
        return result

    def transform(self, item):
        return item * 2 + 1

def main():
    proc = DataProcessor("test")
    items = list(range(50))
    print(proc.process(items))

if __name__ == "__main__":
    main()
`

	result := compressCodeResult(pythonCode, 300)

	if len(result) >= len(pythonCode) {
		t.Errorf("compressCodeResult should shorten Python code, got len %d >= original %d", len(result), len(pythonCode))
	}

	// Should preserve class definition
	if !strings.Contains(result, "class DataProcessor") {
		t.Error("compressed result should preserve class definition")
	}

	// Should contain compression markers for bodies
	if !strings.Contains(result, "...[compressed]") {
		t.Error("compressed result should contain compression markers")
	}
}

func TestDetectLanguageFromContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "go with package and func",
			input:    "package main\n\nfunc main() {}",
			expected: "go",
		},
		{
			name:     "go with method receiver",
			input:    "func (s *Server) Start() error {\n\ttype X struct{}\n\treturn nil\n}",
			expected: "go",
		},
		{
			name:     "python def",
			input:    "def hello():\n    pass\n",
			expected: "python",
		},
		{
			name:     "python class with self",
			input:    "class Foo:\n    def bar(self):\n        pass\n",
			expected: "python",
		},
		{
			name:     "rust fn with let",
			input:    "fn main() {\n    let x = 42;\n}\n",
			expected: "rust",
		},
		{
			name:     "rust pub fn",
			input:    "pub fn hello() -> i32 {\n    42\n}\n",
			expected: "rust",
		},
		{
			name:     "javascript const function",
			input:    "const greet = function(name) {\n  return name;\n};\n",
			expected: "javascript",
		},
		{
			name:     "unknown plain text",
			input:    "just some regular text here",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectLanguageFromContent(tt.input)
			if string(result) != tt.expected {
				t.Errorf("detectLanguageFromContent() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestToCompressedJSON_CodeAware(t *testing.T) {
	// Build a large Go code result
	goCode := "package main\n\nimport \"fmt\"\n\nfunc Hello() {\n\t" +
		strings.Repeat("fmt.Println(\"hello world this is a long line\")\n\t", 50) +
		"}\n"

	r := &ExecutionResult{
		ToolCallID: "test-123",
		Success:    true,
		Result:     goCode,
	}

	// Use a small token budget to trigger compression (100 tokens * 3 chars = 300 chars)
	compressed := r.ToCompressedJSON(100)

	// Should be valid JSON
	if !strings.HasPrefix(compressed, "{") {
		t.Errorf("expected JSON output, got: %q", compressed[:min(50, len(compressed))])
	}

	// Should be shorter than uncompressed
	full := r.ToJSON()
	if len(compressed) >= len(full) {
		t.Errorf("compressed JSON (%d) should be shorter than full (%d)", len(compressed), len(full))
	}

	// Should contain the tool call ID
	if !strings.Contains(compressed, "test-123") {
		t.Error("compressed JSON should contain tool_call_id")
	}
}

func TestToCompressedJSON_NonCodeString(t *testing.T) {
	// Non-code string should use regular truncation
	longText := strings.Repeat("This is just regular text that is not code at all. ", 100)

	r := &ExecutionResult{
		ToolCallID: "test-456",
		Success:    true,
		Result:     longText,
	}

	compressed := r.ToCompressedJSON(50)

	// Should contain truncation marker, not compression marker
	if strings.Contains(compressed, "...[compressed]") && !strings.Contains(compressed, "...[truncated") {
		t.Error("non-code strings should use truncation, not code compression")
	}

	// Should be valid JSON
	if !strings.HasPrefix(compressed, "{") {
		t.Errorf("expected JSON output, got: %q", compressed[:min(50, len(compressed))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
