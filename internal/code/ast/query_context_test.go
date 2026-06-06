package ast

import (
	"context"
	"testing"
)

func TestRunQueryWithContext(t *testing.T) {
	source := []byte(`package main

func Hello() string {
	return "world"
}

func Goodbye() string {
	return "farewell"
}
`)

	pm := NewParserManager(DefaultParserConfig())
	executor := NewQueryExecutor(pm)

	matches, err := executor.RunQueryWithContext(context.Background(), source, LangGo,
		"(function_declaration name: (identifier) @name)", 2)
	if err != nil {
		t.Fatalf("RunQueryWithContext failed: %v", err)
	}

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	// Check first match has context
	m := matches[0]
	if len(m.BeforeContext) == 0 {
		t.Error("expected before context lines")
	}
	if len(m.MatchedLines) == 0 {
		t.Error("expected matched lines")
	}
	if len(m.AfterContext) == 0 {
		t.Error("expected after context lines")
	}

	// Check that the match includes the function name
	found := false
	for _, line := range m.MatchedLines {
		if line == "func Hello() string {" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected matched lines to contain Hello function, got: %v", m.MatchedLines)
	}
}

func TestRunQueryWithRule(t *testing.T) {
	source := []byte(`package main

func Hello() string {
	return "world"
}

func World() string {
	return "hello"
}
`)

	pm := NewParserManager(DefaultParserConfig())
	executor := NewQueryExecutor(pm)

	rule := &QueryRule{
		ID:      "find-hello",
		Pattern: "(function_declaration name: (identifier) @name)",
		Constraints: map[string]Constraint{
			"name": {Regex: "^Hello$"},
		},
	}

	result, err := executor.RunQueryWithRule(context.Background(), source, LangGo, rule)
	if err != nil {
		t.Fatalf("RunQueryWithRule failed: %v", err)
	}

	if result.Count != 1 {
		t.Fatalf("expected 1 match, got %d", result.Count)
	}
	if result.RuleID != "find-hello" {
		t.Errorf("expected RuleID 'find-hello', got %q", result.RuleID)
	}

	// Verify the correct function was matched
	capture := result.Matches[0].Captures[0]
	if capture.Node.Text != "Hello" {
		t.Errorf("expected capture text 'Hello', got %q", capture.Node.Text)
	}
}
