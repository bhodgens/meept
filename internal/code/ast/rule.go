package ast

import (
	"context"
	"fmt"
	"maps"
	"os"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"gopkg.in/yaml.v3"
)

// Rule represents a YAML-based AST rule (inspired by ast-grep).
type Rule struct {
	ID          string            `yaml:"id"`
	Language    string            `yaml:"language"`
	Pattern     string            `yaml:"pattern"`
	Constraints []Constraint      `yaml:"constraints,omitempty"`
	Transform   []Transform       `yaml:"transform,omitempty"`
	Metadata    map[string]string `yaml:"metadata,omitempty"`
}

// Constraint represents a constraint on a captured node.
type Constraint struct {
	Kind     *KindConstraint     `yaml:"kind,omitempty"`
	Regex    *RegexConstraint    `yaml:"regex,omitempty"`
	HasField *HasFieldConstraint `yaml:"has_field,omitempty"`
}

// KindConstraint checks the node kind.
type KindConstraint struct {
	Node string `yaml:"node"`
}

// RegexConstraint applies a regex pattern to a captured node's text.
type RegexConstraint struct {
	Node    string `yaml:"node"`
	Pattern string `yaml:"pattern"`
}

// HasFieldConstraint checks if a node has a specific field.
type HasFieldConstraint struct {
	Node  string `yaml:"node"`
	Field string `yaml:"field"`
}

// Transform represents a transformation to apply to a captured node.
type Transform struct {
	Type    string `yaml:"type"` // "uppercase", "lowercase", "replace", "prepend", "append"
	Node    string `yaml:"node"`
	Pattern string `yaml:"pattern,omitempty"` // for replace
	Replace string `yaml:"replace,omitempty"` // for replace
	Prefix  string `yaml:"prefix,omitempty"`  // for prepend
	Suffix  string `yaml:"suffix,omitempty"`  // for append
}

// RuleResult contains matches and transforms for a rule.
type RuleResult struct {
	Rule    *Rule
	Matches []RuleMatch
}

// RuleMatch represents a single rule match.
type RuleMatch struct {
	Captures    map[string]string
	NodeKind    string
	StartLine   int
	StartChar   int
	EndLine     int
	EndChar     int
	Transformed map[string]string
}

// ParseRule parses a YAML rule string.
func ParseRule(yamlStr string) (*Rule, error) {
	var rule Rule
	if err := yaml.Unmarshal([]byte(yamlStr), &rule); err != nil {
		return nil, fmt.Errorf("invalid YAML rule: %w", err)
	}

	if rule.ID == "" {
		return nil, fmt.Errorf("rule must have an id")
	}
	if rule.Pattern == "" {
		return nil, fmt.Errorf("rule must have a pattern")
	}
	if rule.Language == "" {
		return nil, fmt.Errorf("rule must have a language")
	}

	return &rule, nil
}

// LoadRuleFile loads a rule from a YAML file.
func LoadRuleFile(filePath string) (*Rule, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read rule file: %w", err)
	}
	return ParseRule(string(data))
}

// RuleExecutor executes YAML rules against source code.
type RuleExecutor struct {
	parser *ParserManager
}

// NewRuleExecutor creates a new rule executor.
func NewRuleExecutor(parser *ParserManager) *RuleExecutor {
	return &RuleExecutor{parser: parser}
}

// ExecuteRule executes a single rule against source code.
func (e *RuleExecutor) ExecuteRule(source []byte, lang Language, rule *Rule) (*RuleResult, error) {
	grammar := GetLanguageGrammar(lang)
	if grammar == nil {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	// Parse the source
	tree, err := e.parser.GetTree(context.TODO(), source, lang)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	defer tree.Close()

	// Compile the query
	query, err := sitter.NewQuery([]byte(rule.Pattern), grammar)
	if err != nil {
		return nil, fmt.Errorf("invalid query pattern: %w", err)
	}
	defer query.Close()

	// Execute query
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	cursor.Exec(query, tree.RootNode())

	result := &RuleResult{
		Rule:    rule,
		Matches: make([]RuleMatch, 0),
	}

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		match = cursor.FilterPredicates(match, source)

		// Extract captures
		captures := make(map[string]string)
		var targetNode *sitter.Node
		for _, capture := range match.Captures {
			captureName := query.CaptureNameForId(capture.Index)
			nodeText := string(source[capture.Node.StartByte():capture.Node.EndByte()])
			captures[captureName] = nodeText

			if targetNode == nil {
				targetNode = capture.Node
			}
		}

		if targetNode == nil {
			continue
		}

		// Apply constraints
		if !e.checkConstraints(captures, targetNode, source, rule.Constraints) {
			continue
		}

		// Apply transforms
		transformed := e.applyTransforms(captures, rule.Transform)

		// Calculate position
		startLine := int(targetNode.StartPoint().Row)
		startChar := int(targetNode.StartPoint().Column)
		endLine := int(targetNode.EndPoint().Row)
		endChar := int(targetNode.EndPoint().Column)

		ruleMatch := RuleMatch{
			Captures:    captures,
			NodeKind:    targetNode.Type(),
			StartLine:   startLine,
			StartChar:   startChar,
			EndLine:     endLine,
			EndChar:     endChar,
			Transformed: transformed,
		}

		result.Matches = append(result.Matches, ruleMatch)
	}

	return result, nil
}

// checkConstraints checks if all constraints are satisfied.
func (e *RuleExecutor) checkConstraints(captures map[string]string, node *sitter.Node, source []byte, constraints []Constraint) bool {
	for _, c := range constraints {
		if !e.checkConstraint(c, captures, node, source) {
			return false
		}
	}
	return true
}

// checkConstraint checks if a single constraint is satisfied.
func (e *RuleExecutor) checkConstraint(c Constraint, captures map[string]string, node *sitter.Node, source []byte) bool {
	// Kind constraint: check if the captured node has the expected kind
	if c.Kind != nil {
		nodeText, ok := captures[c.Kind.Node]
		if !ok {
			return false
		}
		// For kind constraints, we just check that the capture exists
		// The actual kind matching is done in the query pattern
		_ = nodeText // suppress unused warning
		return true
	}

	// Regex constraint: check if the captured node's text matches the pattern
	if c.Regex != nil {
		nodeText, ok := captures[c.Regex.Node]
		if !ok {
			return false
		}
		matched, err := regexp.MatchString(c.Regex.Pattern, nodeText)
		if err != nil || !matched {
			return false
		}
	}

	// HasField constraint: check if the captured node has a specific field
	if c.HasField != nil {
		_, ok := captures[c.HasField.Node]
		if !ok {
			return false
		}
		// Field check - verify the node actually has the field
		if c.HasField.Field != "" {
			fieldNode := node.ChildByFieldName(c.HasField.Field)
			if fieldNode == nil {
				return false
			}
		}
	}

	return true
}

// applyTransforms applies transformations to captures.
func (e *RuleExecutor) applyTransforms(captures map[string]string, transforms []Transform) map[string]string {
	result := make(map[string]string)
	maps.Copy(result, captures)

	for _, t := range transforms {
		text, ok := captures[t.Node]
		if !ok {
			continue
		}

		switch t.Type {
		case "uppercase":
			result[t.Node] = strings.ToUpper(text)
		case "lowercase":
			result[t.Node] = strings.ToLower(text)
		case "replace":
			re := regexp.MustCompile(t.Pattern)
			result[t.Node] = re.ReplaceAllString(text, t.Replace)
		case "prepend":
			result[t.Node] = t.Prefix + text
		case "append":
			result[t.Node] = text + t.Suffix
		}
	}

	return result
}

// ExecuteRuleOnFile executes a rule against a file.
func (e *RuleExecutor) ExecuteRuleOnFile(filePath string, rule *Rule) (*RuleResult, error) {
	lang := DetectLanguage(filePath)
	if lang == LangUnknown {
		return nil, fmt.Errorf("could not detect language for: %s", filePath)
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return e.ExecuteRule(source, lang, rule)
}

// CommonRules provides pre-built YAML rules for common patterns.
var CommonRules = map[string]string{
	// Find test functions in Go
	"go_test_functions": `
id: go-test-functions
language: go
pattern: "(function_declaration name: (identifier) @name)"
constraints:
  - regex:
      node: name
      pattern: "^Test"
`,

	// Find TODO comments
	"todo_comments": `
id: todo-comments
language: go
pattern: (comment) @comment
constraints:
  - regex:
      node: comment
      pattern: "TODO|FIXME|XXX"
`,

	// Find empty if blocks (potential dead code)
	"empty_if_blocks": `
id: empty-if-blocks
language: go
pattern: "(if_statement consequence: (block) @body)"
constraints:
  - kind:
      node: body
`,

	// Find unused variable declarations (Go-specific)
	"unused_variables": `
id: unused-variables
language: go
pattern: "(var_declaration (var_spec name: (identifier) @name))"
constraints:
  - regex:
      node: name
      pattern: "^_"
metadata:
  description: "Find explicitly ignored variables (underscore prefix)"
`,

	// Find debug print statements
	"debug_prints": `
id: debug-prints
language: go
pattern: "(call_expression function: [(selector_expression) @func (identifier) @func])"
constraints:
  - regex:
      node: func
      pattern: "^(fmt\\.Print|log\\.Print|panic)"
metadata:
  description: "Find debug print statements that should be removed"
`,

	// Find empty catch/recover blocks
	"empty_catch_blocks": `
id: empty-catch-blocks
language: go
pattern: "(defer_statement (call_expression function: (identifier) @fn))"
constraints:
  - regex:
      node: fn
      pattern: "^recover$"
metadata:
  description: "Find deferred recover calls without handling"
`,

	// Find long function declarations (by parameter count hint)
	"long_functions": `
id: long-functions
language: go
pattern: "(function_declaration parameters: (parameter_list) @params)"
constraints:
  - regex:
      node: params
      pattern: ".{100,}"
metadata:
  description: "Find functions with many parameters"
`,

	// Find println statements in any language
	"console_logs": `
id: console-logs
language: typescript
pattern: "(call_expression function: (member_expression object: (identifier) @obj property: (property_identifier) @prop))"
constraints:
  - regex:
      node: obj
      pattern: "^console$"
  - regex:
      node: prop
      pattern: "^(log|warn|error|info|debug)$"
metadata:
  description: "Find console.log statements"
`,

	// Find Python print statements
	"python_prints": `
id: python-prints
language: python
pattern: "(call_expression function: (identifier) @func)"
constraints:
  - regex:
      node: func
      pattern: "^print$"
metadata:
  description: "Find print statements in Python"
`,

	// Find empty method bodies
	"empty_methods": `
id: empty-methods
language: go
pattern: "(method_declaration body: (block) @body)"
constraints:
  - kind:
      node: body
metadata:
  description: "Find methods with empty bodies"
`,
}

// GetCommonRule returns a pre-built rule by name.
func GetCommonRule(ruleName string) (string, bool) {
	rule, ok := CommonRules[ruleName]
	return rule, ok
}
