package ast

import (
	"testing"
)

// TestParseRule tests YAML rule parsing
func TestParseRule(t *testing.T) {
	t.Run("parse valid rule", func(t *testing.T) {
		yamlStr := `
id: test-rule
language: go
pattern: "(function_declaration name: (identifier) @name)"
constraints:
  - regex:
      node: name
      pattern: "^Test"
transform:
  - type: uppercase
    node: name
`
		rule, err := ParseRule(yamlStr)
		if err != nil {
			t.Fatalf("ParseRule failed: %v", err)
		}

		if rule.ID != "test-rule" {
			t.Errorf("Expected ID 'test-rule', got '%s'", rule.ID)
		}
		if rule.Language != "go" {
			t.Errorf("Expected language 'go', got '%s'", rule.Language)
		}
		if rule.Pattern == "" {
			t.Error("Expected non-empty pattern")
		}
		if len(rule.Constraints) != 1 {
			t.Errorf("Expected 1 constraint, got %d", len(rule.Constraints))
		}
		if len(rule.Transform) != 1 {
			t.Errorf("Expected 1 transform, got %d", len(rule.Transform))
		}
	})

	t.Run("parse rule without constraints", func(t *testing.T) {
		yamlStr := `
id: simple-rule
language: python
pattern: "(function_definition name: (identifier) @name)"
`
		rule, err := ParseRule(yamlStr)
		if err != nil {
			t.Fatalf("ParseRule failed: %v", err)
		}

		if rule.ID != "simple-rule" {
			t.Errorf("Expected ID 'simple-rule', got '%s'", rule.ID)
		}
		if len(rule.Constraints) != 0 {
			t.Errorf("Expected 0 constraints, got %d", len(rule.Constraints))
		}
	})

	t.Run("parse rule missing id", func(t *testing.T) {
		yamlStr := `
language: go
pattern: "(function_declaration)"
`
		_, err := ParseRule(yamlStr)
		if err == nil {
			t.Error("Expected error for missing id")
		}
	})

	t.Run("parse rule missing pattern", func(t *testing.T) {
		yamlStr := `
id: test-rule
language: go
`
		_, err := ParseRule(yamlStr)
		if err == nil {
			t.Error("Expected error for missing pattern")
		}
	})

	t.Run("parse rule missing language", func(t *testing.T) {
		yamlStr := `
id: test-rule
pattern: "(function_declaration)"
`
		_, err := ParseRule(yamlStr)
		if err == nil {
			t.Error("Expected error for missing language")
		}
	})
}

// TestRuleExecutor tests rule execution
func TestRuleExecutor(t *testing.T) {
	parser := NewParserManager(DefaultParserConfig())
	executor := NewRuleExecutor(parser)

	t.Run("execute go_test_functions rule", func(t *testing.T) {
		source := []byte(`
package main

func TestSomething(t *testing.T) {
	// test code
}

func TestAnotherThing(t *testing.T) {
	// test code
}

func NotATest() {
	// not a test
}
`)

		rule, err := ParseRule(`
id: go-test-functions
language: go
pattern: "(function_declaration name: (identifier) @name)"
constraints:
  - regex:
      node: name
      pattern: "^Test"
`)
		if err != nil {
			t.Fatalf("ParseRule failed: %v", err)
		}

		result, err := executor.ExecuteRule(source, LangGo, rule)
		if err != nil {
			t.Fatalf("ExecuteRule failed: %v", err)
		}

		if result.Rule.ID != "go-test-functions" {
			t.Errorf("Expected rule ID 'go-test-functions', got '%s'", result.Rule.ID)
		}

		if len(result.Matches) != 2 {
			t.Errorf("Expected 2 matches, got %d", len(result.Matches))
		}

		// Verify captures
		for _, match := range result.Matches {
			name := match.Captures["name"]
			if name != "TestSomething" && name != "TestAnotherThing" {
				t.Errorf("Unexpected captured name: %s", name)
			}
		}
	})

	t.Run("execute todo_comments rule", func(t *testing.T) {
		source := []byte(`
package main

// TODO: implement this
func Foo() {}

// FIXME: bug here
func Bar() {}

// Normal comment
func Baz() {}
`)

		rule, err := ParseRule(`
id: todo-comments
language: go
pattern: (comment) @comment
constraints:
  - regex:
      node: comment
      pattern: "TODO|FIXME|XXX"
`)
		if err != nil {
			t.Fatalf("ParseRule failed: %v", err)
		}

		result, err := executor.ExecuteRule(source, LangGo, rule)
		if err != nil {
			t.Fatalf("ExecuteRule failed: %v", err)
		}

		if len(result.Matches) != 2 {
			t.Errorf("Expected 2 matches (TODO and FIXME), got %d", len(result.Matches))
		}
	})

	t.Run("execute rule with transform", func(t *testing.T) {
		source := []byte(`
package main

func TestFoo() {}
`)

		rule, err := ParseRule(`
id: uppercase-names
language: go
pattern: "(function_declaration name: (identifier) @name)"
transform:
  - type: uppercase
    node: name
`)
		if err != nil {
			t.Fatalf("ParseRule failed: %v", err)
		}

		result, err := executor.ExecuteRule(source, LangGo, rule)
		if err != nil {
			t.Fatalf("ExecuteRule failed: %v", err)
		}

		if len(result.Matches) != 1 {
			t.Errorf("Expected 1 match, got %d", len(result.Matches))
		}

		match := result.Matches[0]
		if match.Transformed["name"] != "TESTFOO" {
			t.Errorf("Expected uppercase 'TESTFOO', got '%s'", match.Transformed["name"])
		}
	})
}

// TestCommonRules tests pre-built rules
func TestCommonRules(t *testing.T) {
	t.Run("get common rule", func(t *testing.T) {
		rule, ok := GetCommonRule("go_test_functions")
		if !ok {
			t.Error("Expected to find go_test_functions rule")
		}
		if rule == "" {
			t.Error("Expected non-empty rule")
		}
	})

	t.Run("get non-existent rule", func(t *testing.T) {
		_, ok := GetCommonRule("non_existent_rule")
		if ok {
			t.Error("Expected false for non-existent rule")
		}
	})

	t.Run("validate all common rules parse correctly", func(t *testing.T) {
		parser := NewParserManager(DefaultParserConfig())
		executor := NewRuleExecutor(parser)

		testSource := []byte(`
package main

// TODO: fix this
func TestExample(t *testing.T) {
	fmt.Println("debug")
}

func unused_function() {}

if false {
	// empty if
}
`)

		for ruleName := range CommonRules {
			rule, err := ParseRule(CommonRules[ruleName])
			if err != nil {
				t.Errorf("Failed to parse rule '%s': %v", ruleName, err)
				continue
			}

			// Try to execute (may fail due to language mismatch, that's ok)
			_, err = executor.ExecuteRule(testSource, LangGo, rule)
			// We don't fail on execute errors since some rules are for other languages
		}
	})
}

// TestConstraintTypes tests different constraint types
func TestConstraintTypes(t *testing.T) {
	parser := NewParserManager(DefaultParserConfig())
	executor := NewRuleExecutor(parser)

	t.Run("regex constraint", func(t *testing.T) {
		source := []byte(`package main; func TestFoo() {}`)
		rule, _ := ParseRule(`
id: regex-test
language: go
pattern: "(function_declaration name: (identifier) @name)"
constraints:
  - regex:
      node: name
      pattern: "^Test"
`)
		result, err := executor.ExecuteRule(source, LangGo, rule)
		if err != nil {
			t.Fatalf("ExecuteRule failed: %v", err)
		}
		if len(result.Matches) != 1 {
			t.Errorf("Expected 1 match, got %d", len(result.Matches))
		}
	})

	t.Run("kind constraint", func(t *testing.T) {
		source := []byte(`package main; func Foo() {}`)
		rule, _ := ParseRule(`
id: kind-test
language: go
pattern: "(function_declaration body: (block) @body)"
constraints:
  - kind:
      node: body
`)
		result, err := executor.ExecuteRule(source, LangGo, rule)
		if err != nil {
			t.Fatalf("ExecuteRule failed: %v", err)
		}
		if len(result.Matches) < 1 {
			t.Errorf("Expected at least 1 match, got %d", len(result.Matches))
		}
	})
}

// TestTransformTypes tests different transform types
func TestTransformTypes(t *testing.T) {
	t.Run("uppercase transform", func(t *testing.T) {
		executor := &RuleExecutor{}
		captures := map[string]string{"name": "testValue"}
		transforms := []Transform{
			{Type: "uppercase", Node: "name"},
		}
		result := executor.applyTransforms(captures, transforms)
		if result["name"] != "TESTVALUE" {
			t.Errorf("Expected 'TESTVALUE', got '%s'", result["name"])
		}
	})

	t.Run("lowercase transform", func(t *testing.T) {
		executor := &RuleExecutor{}
		captures := map[string]string{"name": "TESTVALUE"}
		transforms := []Transform{
			{Type: "lowercase", Node: "name"},
		}
		result := executor.applyTransforms(captures, transforms)
		if result["name"] != "testvalue" {
			t.Errorf("Expected 'testvalue', got '%s'", result["name"])
		}
	})

	t.Run("replace transform", func(t *testing.T) {
		executor := &RuleExecutor{}
		captures := map[string]string{"name": "TestFoo"}
		transforms := []Transform{
			{Type: "replace", Node: "name", Pattern: "^Test", Replace: "Get"},
		}
		result := executor.applyTransforms(captures, transforms)
		if result["name"] != "GetFoo" {
			t.Errorf("Expected 'GetFoo', got '%s'", result["name"])
		}
	})

	t.Run("prepend transform", func(t *testing.T) {
		executor := &RuleExecutor{}
		captures := map[string]string{"name": "value"}
		transforms := []Transform{
			{Type: "prepend", Node: "name", Prefix: "new"},
		}
		result := executor.applyTransforms(captures, transforms)
		if result["name"] != "newvalue" {
			t.Errorf("Expected 'newvalue', got '%s'", result["name"])
		}
	})

	t.Run("append transform", func(t *testing.T) {
		executor := &RuleExecutor{}
		captures := map[string]string{"name": "value"}
		transforms := []Transform{
			{Type: "append", Node: "name", Suffix: "123"},
		}
		result := executor.applyTransforms(captures, transforms)
		if result["name"] != "value123" {
			t.Errorf("Expected 'value123', got '%s'", result["name"])
		}
	})
}
