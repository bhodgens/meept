package ast

import (
	"strings"
	"testing"
)

// truncateStr is a test helper to limit error message output.
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// skipIfNoGrammar skips the test if the tree-sitter grammar for the given language
// is not available (e.g. missing .so files).
func skipIfNoGrammar(t *testing.T, lang Language) {
	t.Helper()
	if GetLanguageGrammar(lang) == nil {
		t.Skipf("grammar for %q not available", lang)
	}
}

// ---------------------------------------------------------------------------
// Go
// ---------------------------------------------------------------------------

func TestCompressCodeAtBoundaries_Go(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	tests := []struct {
		name           string
		source         string
		maxChars       int
		wantContains   []string
		wantNotContains []string
	}{
		{
			name: "basic compression preserves signatures",
			source: "package main\n\nimport \"fmt\"\n\nfunc Hello() {\n\t" +
				strings.Repeat("fmt.Println(\"hello\")\n", 50) +
				"}\n",
			maxChars:     200,
			wantContains: []string{"package main", "func Hello()", "...[compressed]"},
		},
		{
			name:           "short code returned as-is",
			source:         "package main\n\nfunc hello() { return 42 }\n",
			maxChars:       1000,
			wantNotContains: []string{"...[compressed]"},
		},
		{
			name: "nested function literal",
			source: "package main\n\nfunc outer() {\n\tfn := func() {\n\t\t" +
				strings.Repeat("x += 1\n", 100) +
				"\t}\n\tfn()\n}\n",
			maxChars:     200,
			wantContains: []string{"package main", "func outer()", "...[compressed]"},
		},
		{
			name: "interface with methods preserved",
			source: "package main\n\ntype Handler interface {\n\tHandle(msg string) error\n\tClose() error\n}\n\ntype Server struct {\n\tHandler Handler\n}\n\nfunc (s *Server) Start() error {\n\t" +
				strings.Repeat("fmt.Println(\"starting\")\n", 50) +
				"\treturn nil\n}\n",
			maxChars:     300,
			wantContains: []string{"Handler interface", "Handle(msg string)", "...[compressed]"},
		},
		{
			name: "multiple functions all compressed",
			source: "package main\n\nfunc a() { " + strings.Repeat("x", 200) + " }\nfunc b() { " + strings.Repeat("y", 200) + " }\nfunc c() { " + strings.Repeat("z", 200) + " }",
			maxChars:     200,
			wantContains: []string{"func a()", "func b()", "func c()"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompressCodeAtBoundaries([]byte(tt.source), LangGo, tt.maxChars)

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("result should contain %q\nGot: %s", want, truncateStr(result, 200))
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(result, notWant) {
					t.Errorf("result should NOT contain %q\nGot: %s", notWant, truncateStr(result, 200))
				}
			}

			// Allow some slack for compression markers and structural overhead
			if len(result) > tt.maxChars+100 {
				t.Errorf("result length %d exceeds maxChars %d significantly", len(result), tt.maxChars)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Python
// ---------------------------------------------------------------------------

func TestCompressCodeAtBoundaries_Python(t *testing.T) {
	skipIfNoGrammar(t, LangPython)

	t.Run("class with methods compressed", func(t *testing.T) {
		pythonCode := "class Processor:\n    def __init__(self):\n        " +
			strings.Repeat("self.data.append(i)\n        ", 100) +
			"\n    def process(self, items):\n        " +
			strings.Repeat("result.append(self.transform(item))\n        ", 100) +
			"\n    def transform(self, item):\n        return item * 2\n"

		result := CompressCodeAtBoundaries([]byte(pythonCode), LangPython, 300)

		if !strings.Contains(result, "class Processor") {
			t.Error("should preserve class definition")
		}
		if !strings.Contains(result, "...[compressed]") {
			t.Error("should compress method bodies")
		}
		if len(result) >= len(pythonCode) {
			t.Errorf("should be shorter, got %d >= %d", len(result), len(pythonCode))
		}
	})

	t.Run("short module fits in budget", func(t *testing.T) {
		source := "def hello():\n    return 42\n"
		result := CompressCodeAtBoundaries([]byte(source), LangPython, 1000)
		if strings.Contains(result, "...[compressed]") {
			t.Error("should not compress when source fits in budget")
		}
		if result != source {
			t.Errorf("should return source unchanged, got %q", result)
		}
	})
}

// ---------------------------------------------------------------------------
// TypeScript
// ---------------------------------------------------------------------------

func TestCompressCodeAtBoundaries_TypeScript(t *testing.T) {
	skipIfNoGrammar(t, LangTypeScript)

	t.Run("class methods compressed", func(t *testing.T) {
		tsCode := "class Handler {\n    constructor() {\n        " +
			strings.Repeat("this.data.push(i);\n        ", 100) +
			"}\n\n    process(input: string): void {\n        " +
			strings.Repeat("console.log(input);\n        ", 100) +
			"}\n}\n\nconst greet = function(name: string) {\n    " +
			strings.Repeat("return name;\n    ", 50) +
			"};\n"

		result := CompressCodeAtBoundaries([]byte(tsCode), LangTypeScript, 300)

		if !strings.Contains(result, "class Handler") {
			t.Error("should preserve class declaration")
		}
		if !strings.Contains(result, "...[compressed]") {
			t.Error("should compress method bodies")
		}
	})

	t.Run("arrow function compressed", func(t *testing.T) {
		source := "const add = (a: number, b: number): number => {\n  " +
			strings.Repeat("console.log(a + b);\n  ", 50) +
			"return a + b;\n};\n"

		result := CompressCodeAtBoundaries([]byte(source), LangTypeScript, 100)

		if !strings.Contains(result, "...[compressed]") {
			t.Error("should compress arrow function body")
		}
	})
}

// ---------------------------------------------------------------------------
// Rust
// ---------------------------------------------------------------------------

func TestCompressCodeAtBoundaries_Rust(t *testing.T) {
	skipIfNoGrammar(t, LangRust)

	t.Run("functions compressed", func(t *testing.T) {
		rustCode := "fn main() {\n    " +
			strings.Repeat("println!(\"hello\");\n    ", 100) +
			"}\n\nfn helper() -> i32 {\n    " +
			strings.Repeat("let x = 42;\n    ", 100) +
			"    42\n}\n"

		result := CompressCodeAtBoundaries([]byte(rustCode), LangRust, 200)

		if !strings.Contains(result, "fn main()") {
			t.Error("should preserve fn main signature")
		}
		if !strings.Contains(result, "...[compressed]") {
			t.Error("should compress function bodies")
		}
	})

	t.Run("impl block compressed", func(t *testing.T) {
		source := "struct Counter { count: i32 }\n\nimpl Counter {\n    fn new() -> Self {\n        " +
			strings.Repeat("println!(\"new\");\n        ", 50) +
			"Counter { count: 0 }\n    }\n\n    fn increment(&mut self) {\n        " +
			strings.Repeat("self.count += 1;\n        ", 50) +
			"}\n}\n"

		result := CompressCodeAtBoundaries([]byte(source), LangRust, 200)

		if !strings.Contains(result, "struct Counter") {
			t.Error("should preserve struct definition")
		}
		if !strings.Contains(result, "impl Counter") {
			t.Error("should preserve impl block")
		}
	})
}

// ---------------------------------------------------------------------------
// Edge Cases
// ---------------------------------------------------------------------------

func TestCompressCodeAtBoundaries_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		source   []byte
		lang     Language
		maxChars int
		check    func(t *testing.T, result string)
	}{
		{
			name:     "zero maxChars returns empty",
			source:   []byte("package main\n\nfunc hello() { return 42 }"),
			lang:     LangGo,
			maxChars: 0,
			check: func(t *testing.T, result string) {
				if result != "" {
					t.Errorf("expected empty string for maxChars=0, got %q", result)
				}
			},
		},
		{
			name:     "negative maxChars returns empty",
			source:   []byte("package main"),
			lang:     LangGo,
			maxChars: -1,
			check: func(t *testing.T, result string) {
				if result != "" {
					t.Errorf("expected empty string for negative maxChars, got %q", result)
				}
			},
		},
		{
			name:     "unknown language falls back to truncation",
			source:   []byte(strings.Repeat("x(y,z) { body } ", 100)),
			lang:     LangUnknown,
			maxChars: 100,
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, "...[truncated]") {
					t.Errorf("unknown language should fall back to truncation, got %q", result)
				}
			},
		},
		{
			name:     "syntax error falls back to truncation",
			source:   []byte("package main\n\nfunc (broken syntax { {{ }}}"),
			lang:     LangGo,
			maxChars: 50,
			check: func(t *testing.T, result string) {
				skipIfNoGrammar(t, LangGo)
				// Should not panic; may truncate or return what it can parse
				if len(result) > 100 {
					t.Errorf("result should be short for broken syntax, got len %d: %q", len(result), result)
				}
			},
		},
		{
			name:     "empty source",
			source:   []byte(""),
			lang:     LangGo,
			maxChars: 100,
			check: func(t *testing.T, result string) {
				if result != "" {
					t.Errorf("expected empty for empty source, got %q", result)
				}
			},
		},
		{
			name:     "source fits in budget no compression",
			source:   []byte("package main\n\nfunc hello() { return 42 }"),
			lang:     LangGo,
			maxChars: 1000,
			check: func(t *testing.T, result string) {
				if strings.Contains(result, "...[compressed]") {
					t.Error("should not compress when source fits in budget")
				}
			},
		},
		{
			name:     "nil source treated as empty",
			source:   nil,
			lang:     LangGo,
			maxChars: 100,
			check: func(t *testing.T, result string) {
				if result != "" {
					t.Errorf("expected empty for nil source, got %q", result)
				}
			},
		},
		{
			name:     "maxChars of 1 returns single byte",
			source:   []byte("package main\n\nfunc hello() { return 42 }"),
			lang:     LangGo,
			maxChars: 1,
			check: func(t *testing.T, result string) {
				// truncateByteFallback with maxChars=1 returns first byte (no room for marker)
				if len(result) > 1 {
					t.Errorf("expected at most 1 byte, got %q (len %d)", result, len(result))
				}
			},
		},
		{
			name:     "compressed result still too long falls back to truncation",
			source:   []byte(strings.Repeat("func f() { x } ", 200)),
			lang:     LangGo,
			maxChars: 50,
			check: func(t *testing.T, result string) {
				skipIfNoGrammar(t, LangGo)
				// After compression, if still too long, truncation is applied
				if len(result) > 100 {
					t.Errorf("result should be bounded, got len %d", len(result))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompressCodeAtBoundaries(tt.source, tt.lang, tt.maxChars)
			tt.check(t, result)
		})
	}
}

// ---------------------------------------------------------------------------
// truncateByteFallback (private helper)
// ---------------------------------------------------------------------------

func TestTruncateByteFallback(t *testing.T) {
	tests := []struct {
		name     string
		source   []byte
		maxChars int
		want     string
	}{
		{
			name:     "zero maxChars returns empty",
			source:   []byte("hello world"),
			maxChars: 0,
			want:     "",
		},
		{
			name:     "negative maxChars returns empty",
			source:   []byte("hello world"),
			maxChars: -5,
			want:     "",
		},
		{
			name:     "source shorter than maxChars returned as-is",
			source:   []byte("hi"),
			maxChars: 100,
			want:     "hi",
		},
		{
			name:     "source exactly maxChars returned as-is",
			source:   []byte("hello"),
			maxChars: 5,
			want:     "hello",
		},
		{
			name:     "truncation with marker",
			source:   []byte(strings.Repeat("abcdefghij", 5)), // 50 bytes
			maxChars: 20,
			want:     "abcde\n...[truncated]",
		},
		{
			name:     "maxChars smaller than marker length returns raw bytes",
			source:   []byte(strings.Repeat("abcdefghij", 5)), // 50 bytes
			maxChars: 8,
			want:     "abcdefgh",
		},
		{
			name:     "empty source",
			source:   []byte(""),
			maxChars: 100,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateByteFallback(tt.source, tt.maxChars)
			if got != tt.want {
				t.Errorf("truncateByteFallback(%q, %d) = %q, want %q", tt.source, tt.maxChars, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// truncateStrFallback (private helper)
// ---------------------------------------------------------------------------

func TestTruncateStrFallback(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		maxChars int
		want     string
	}{
		{
			name:     "zero maxChars returns empty",
			source:   "hello world",
			maxChars: 0,
			want:     "",
		},
		{
			name:     "negative maxChars returns empty",
			source:   "hello world",
			maxChars: -5,
			want:     "",
		},
		{
			name:     "source shorter than maxChars returned as-is",
			source:   "hi",
			maxChars: 100,
			want:     "hi",
		},
		{
			name:     "truncation with marker",
			source:   strings.Repeat("abcdefghij", 5), // 50 bytes
			maxChars: 20,
			want:     "abcde\n...[truncated]",
		},
		{
			name:     "maxChars smaller than marker length returns raw",
			source:   strings.Repeat("abcdefghij", 5), // 50 bytes
			maxChars: 8,
			want:     "abcdefgh",
		},
		{
			name:     "empty source",
			source:   "",
			maxChars: 100,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStrFallback(tt.source, tt.maxChars)
			if got != tt.want {
				t.Errorf("truncateStrFallback(%q, %d) = %q, want %q", tt.source, tt.maxChars, got, tt.want)
			}
		})
	}
}
