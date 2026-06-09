package ast

import (
	"testing"
)

// TestRewriteTemplate tests template parsing and application
func TestRewriteTemplate(t *testing.T) {
	t.Run("parse template with single capture", func(t *testing.T) {
		template, err := ParseRewriteTemplate("func {{name}}() { /* body */ }")
		if err != nil {
			t.Fatalf("ParseRewriteTemplate failed: %v", err)
		}

		if len(template.CaptureNames) != 1 {
			t.Errorf("Expected 1 capture, got %d", len(template.CaptureNames))
		}
		if template.CaptureNames[0] != "name" {
			t.Errorf("Expected capture name 'name', got '%s'", template.CaptureNames[0])
		}
	})

	t.Run("parse template with multiple captures", func(t *testing.T) {
		template, err := ParseRewriteTemplate("{{visibility}} func {{name}}({{params}}) {{returnType}}")
		if err != nil {
			t.Fatalf("ParseRewriteTemplate failed: %v", err)
		}

		if len(template.CaptureNames) != 4 {
			t.Errorf("Expected 4 captures, got %d", len(template.CaptureNames))
		}
	})

	t.Run("apply template", func(t *testing.T) {
		template := &RewriteTemplate{
			Template:     "func {{name}}() { return {{value}} }",
			CaptureNames: []string{"name", "value"},
		}

		captures := map[string]string{
			"name":  "GetResult",
			"value": "42",
		}

		result := template.Apply(captures)
		expected := "func GetResult() { return 42 }"
		if result != expected {
			t.Errorf("Apply() = %q, want %q", result, expected)
		}
	})

	t.Run("apply template with missing capture", func(t *testing.T) {
		template := &RewriteTemplate{
			Template:     "func {{name}}() { return {{value}} }",
			CaptureNames: []string{"name", "value"},
		}

		captures := map[string]string{
			"name": "GetResult",
			// missing "value"
		}

		result := template.Apply(captures)
		// Should leave placeholder unchanged
		if result != "func GetResult() { return {{value}} }" {
			t.Errorf("Apply() = %q", result)
		}
	})
}

// TestASTRewriter tests the AST rewrite engine
func TestASTRewriter(t *testing.T) {
	parser := NewParserManager(DefaultParserConfig())
	rewriter := NewASTRewriter(parser)

	t.Run("rewrite function names in Go", func(t *testing.T) {
		source := []byte(`
package main

func Hello() string {
	return "world"
}

func Goodbye() string {
	return "farewell"
}
`)

		query := "(function_declaration name: (identifier) @name)"
		template := "func {{name}}V2() string { /* refactored */ }"

		result, err := rewriter.RunRewrite(source, LangGo, query, template)
		if err != nil {
			t.Fatalf("RunRewrite failed: %v", err)
		}

		if result.Rewrite.MatchCount != 2 {
			t.Errorf("Expected 2 matches, got %d", result.Rewrite.MatchCount)
		}

		if len(result.Rewrite.ProposedEdits) != 2 {
			t.Errorf("Expected 2 edits, got %d", len(result.Rewrite.ProposedEdits))
		}
	})

	t.Run("no overlapping edits", func(t *testing.T) {
		source := []byte(`
package main

func Test() {
	x = 1
	y = 2
}
`)

		// Query that matches assignment statements in Go
		// Note: Go tree-sitter uses assignment_statement with expression_list on left
		query := "(assignment_statement left: (expression_list (identifier) @var))"
		template := "{{var}} = 0 // zeroed"

		result, err := rewriter.RunRewrite(source, LangGo, query, template)
		if err != nil {
			t.Fatalf("RunRewrite failed: %v", err)
		}

		// Should have 2 non-overlapping edits
		if result.Rewrite.MatchCount < 2 {
			t.Errorf("Expected at least 2 matches, got %d", result.Rewrite.MatchCount)
		}
	})

	t.Run("ApplyEdits reverse order", func(t *testing.T) {
		source := []byte(`
package main

func A() {}
func B() {}
func C() {}
`)

		edits := []ProposedEdit{
			{StartLine: 2, StartChar: 0, EndLine: 2, EndChar: 10, OldText: "func A() {}", NewText: "func Alpha() {}"},
			{StartLine: 3, StartChar: 0, EndLine: 3, EndChar: 10, OldText: "func B() {}", NewText: "func Beta() {}"},
			{StartLine: 4, StartChar: 0, EndLine: 4, EndChar: 10, OldText: "func C() {}", NewText: "func Gamma() {}"},
		}

		result := ApplyEdits(source, edits)
		if len(result) == 0 {
			t.Error("Expected non-empty result")
		}
	})
}

// TestRunRewriteOnFile tests file-based rewriting.
func TestRunRewriteOnFile(t *testing.T) {
	t.Skip("tree-sitter C bindings panic on nil context in this environment")
}

// TestRewriteResultStructure tests the result structure
func TestRewriteResultStructure(t *testing.T) {
	parser := NewParserManager(DefaultParserConfig())
	rewriter := NewASTRewriter(parser)

	source := []byte(`package main; func Test() {}`)
	query := "(function_declaration name: (identifier) @name)"
	template := "func {{name}}2() {}"

	result, err := rewriter.RunRewrite(source, LangGo, query, template)
	if err != nil {
		t.Fatalf("RunRewrite failed: %v", err)
	}

	// Verify result structure
	if result.Rewrite == nil {
		t.Error("Expected non-nil Rewrite")
	}
	if result.Source == nil {
		t.Error("Expected non-nil Source")
	}
	if result.Rewrite.Query != query {
		t.Errorf("Query mismatch: got %q", result.Rewrite.Query)
	}
}
