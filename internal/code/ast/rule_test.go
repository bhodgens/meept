package ast

import (
	"testing"
)

func TestParseQueryRule(t *testing.T) {
	yamlData := `
id: find-hello
language: go
pattern: |
  (function_declaration name: (identifier) @name)
constraints:
  name:
    regex: "Hello.*"
`
	rule, err := ParseQueryRule(yamlData)
	if err != nil {
		t.Fatalf("ParseQueryRule failed: %v", err)
	}
	if rule.ID != "find-hello" {
		t.Errorf("expected ID 'find-hello', got %q", rule.ID)
	}
	if rule.Language != "go" {
		t.Errorf("expected language 'go', got %q", rule.Language)
	}
	if rule.Pattern == "" {
		t.Error("expected non-empty pattern")
	}
	if len(rule.Constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(rule.Constraints))
	}
	c, ok := rule.Constraints["name"]
	if !ok {
		t.Fatal("expected constraint for 'name'")
	}
	if c.Regex != "Hello.*" {
		t.Errorf("expected regex 'Hello.*', got %q", c.Regex)
	}
}

func TestParseQueryRule_MissingPattern(t *testing.T) {
	_, err := ParseQueryRule("id: test")
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}

func TestQueryRule_ApplyConstraints_Regex(t *testing.T) {
	rule := &QueryRule{
		ID:      "test",
		Pattern: "(identifier) @id",
		Constraints: map[string]Constraint{
			"id": {Regex: "^Hello$"},
		},
	}

	// Match
	captures := []MatchCheck{{Name: "id", Text: "Hello"}}
	if !rule.ApplyConstraints(captures) {
		t.Error("expected match for 'Hello'")
	}

	// No match
	captures = []MatchCheck{{Name: "id", Text: "World"}}
	if rule.ApplyConstraints(captures) {
		t.Error("expected no match for 'World'")
	}
}

func TestQueryRule_ApplyConstraints_Eq(t *testing.T) {
	rule := &QueryRule{
		ID:      "test",
		Pattern: "(identifier) @id",
		Constraints: map[string]Constraint{
			"id": {Eq: "foo"},
		},
	}

	if !rule.ApplyConstraints([]MatchCheck{{Name: "id", Text: "foo"}}) {
		t.Error("expected match for 'foo'")
	}
	if rule.ApplyConstraints([]MatchCheck{{Name: "id", Text: "bar"}}) {
		t.Error("expected no match for 'bar'")
	}
}

func TestQueryRule_ApplyConstraints_NotEq(t *testing.T) {
	rule := &QueryRule{
		ID:      "test",
		Pattern: "(identifier) @id",
		Constraints: map[string]Constraint{
			"id": {NotEq: "private"},
		},
	}

	if !rule.ApplyConstraints([]MatchCheck{{Name: "id", Text: "public"}}) {
		t.Error("expected match for 'public'")
	}
	if rule.ApplyConstraints([]MatchCheck{{Name: "id", Text: "private"}}) {
		t.Error("expected no match for 'private'")
	}
}

func TestQueryRule_ApplyTransforms(t *testing.T) {
	rule := &QueryRule{
		ID:      "test",
		Pattern: "(identifier) @name",
		Transform: map[string]string{
			"name": "prefix_$name_suffix",
		},
	}

	captures := []MatchCheck{{Name: "name", Text: "foo"}}
	result := rule.ApplyTransforms(captures)
	if result["name"] != "prefix_foo_suffix" {
		t.Errorf("expected 'prefix_foo_suffix', got %q", result["name"])
	}
}
