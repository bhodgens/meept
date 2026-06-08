package ast

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// FindBlockSpan integration tests (real tree-sitter parser)
// ---------------------------------------------------------------------------

func TestFindBlockSpan_GoFunction(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	src := `package main

import "fmt"

func hello(name string) string {
	return fmt.Sprintf("hello, %s", name)
}

func main() {
	msg := hello("world")
	fmt.Println(msg)
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	ctx := context.Background()

	// Line 5 is the "func hello" line
	span, err := pm.FindBlockSpan(ctx, path, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.NodeType != "function_declaration" {
		t.Errorf("expected node type 'function_declaration', got %q", span.NodeType)
	}
	// The hello function spans lines 5-7 (func ... { ... })
	if span.StartLine != 5 {
		t.Errorf("expected start line 5, got %d", span.StartLine)
	}
	if span.EndLine != 7 {
		t.Errorf("expected end line 7, got %d", span.EndLine)
	}
}

func TestFindBlockSpan_NestedBlock(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	src := `package main

type Foo struct {
	name string
}

func (f *Foo) Greet() string {
	return "hi " + f.name
}

func (f *Foo) Farewell() string {
	return "bye " + f.name
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	ctx := context.Background()

	// Line 8 is inside the Greet method body ("return ...")
	span, err := pm.FindBlockSpan(ctx, path, 8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should find the innermost block — the method_declaration for Greet
	if span.NodeType != "method_declaration" {
		t.Errorf("expected node type 'method_declaration', got %q", span.NodeType)
	}
	// Greet method spans lines 7-9
	if span.StartLine != 7 {
		t.Errorf("expected start line 7, got %d", span.StartLine)
	}
	if span.EndLine != 9 {
		t.Errorf("expected end line 9, got %d", span.EndLine)
	}
}

func TestFindBlockSpan_NoBlock(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	src := `package main

// this is just a comment
var x = 42
`
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	ctx := context.Background()

	// Line 3 is a comment line — no block should be found
	_, err := pm.FindBlockSpan(ctx, path, 3)
	if err == nil {
		t.Error("expected error for line with no syntactic block")
	}
}

func TestFindBlockSpan_PythonFunction(t *testing.T) {
	skipIfNoGrammar(t, LangPython)

	src := `def greet(name):
    return f"hello, {name}"

def main():
    msg = greet("world")
    print(msg)
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.py")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	ctx := context.Background()

	// Line 1 is the "def greet" line
	span, err := pm.FindBlockSpan(ctx, path, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if span.NodeType != "function_definition" {
		t.Errorf("expected node type 'function_definition', got %q", span.NodeType)
	}
	// greet function spans lines 1-2
	if span.StartLine != 1 {
		t.Errorf("expected start line 1, got %d", span.StartLine)
	}
	if span.EndLine != 2 {
		t.Errorf("expected end line 2, got %d", span.EndLine)
	}
}

func TestFindBlockSpan_Fallback(t *testing.T) {
	skipIfNoGrammar(t, LangGo)

	// This file has a function but we query a line just outside the function body.
	// The line is within the 5-line fallback window of the function's start line.
	src := `package main

func hello() string {
	return "hello"
}

var y = 99
`
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	pm := NewParserManager(ParserConfig{CacheEnabled: false})
	ctx := context.Background()

	// Line 6 is "var y = 99" — not inside any function block.
	// However, the fallback should find the nearest function within 5 lines.
	// The "func hello" starts at line 3, distance to line 6 is 3, within fallback.
	// But actually, line 6 is not inside a block, so FindBlockSpan should
	// try the fallback. The nearest block start to line 6 (0-based 5) is
	// func hello at line 3 (0-based 2), distance = 3 <= 5, so it should return it.
	span, err := pm.FindBlockSpan(ctx, path, 6)
	if err != nil {
		t.Fatalf("unexpected error (fallback should have found a block): %v", err)
	}
	if span.NodeType != "function_declaration" {
		t.Errorf("expected fallback to find 'function_declaration', got %q", span.NodeType)
	}
	if span.StartLine != 3 {
		t.Errorf("expected fallback start line 3, got %d", span.StartLine)
	}
}
