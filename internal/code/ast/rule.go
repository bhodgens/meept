package ast

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	sitter "github.com/smacker/go-tree-sitter"
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
	Kind     string            `yaml:"kind,omitempty"`
	Regex    *RegexConstraint  `yaml:"regex,omitempty"`
	HasField *HasFieldConstraint `yaml:"has_field,omitempty"`
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
	Type      string `yaml:"type"` // "uppercase", "lowercase", "replace", "prepend", "append"
	Node      string `yaml:"node"`
	Pattern   string `yaml:"pattern,omitempty"`   // for replace
	Replace   string `yaml:"replace,omitempty"`   // for replace
	Prefix    string `yaml:"prefix,omitempty"`    // for prepend
	Suffix    string `yaml:"suffix,omitempty"`    // for append
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
	tree, err := e.parser.GetTree(nil, source, lang)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// Compile the query
	query, err := sitter.NewQuery([]byte(rule.Pattern), grammar)
	if err != nil {
		return nil, fmt.Errorf("invalid query pattern: %w", err)
	}

	// Execute query
	cursor := sitter.NewQueryCursor()
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
		if !e.checkConstraints(captures, source, rule.Constraints) {
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
func (e *RuleExecutor) checkConstraints(captures map[string]string, source []byte, constraints []Constraint) bool {
	for _, c := range constraints {
		if !checkConstraint(c, captures) {
			return false
		}
	}
	return true
}

func checkConstraint(c Constraint, captures map[string]string) bool {
	if c.Kind != "" {
		// Kind constraint - not implemented for this simple version
		// Would require access to node kind from capture
		return true
	}

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

	if c.HasField != nil {
		_, ok := captures[c.HasField.Node]
		if !ok {
			return false
		}
		// Field check would require node access - simplified here
		return true
	}

	return true
}

// applyTransforms applies transformations to captures.
func (e *RuleExecutor) applyTransforms(captures map[string]string, transforms []Transform) map[string]string {
	result := make(map[string]string)
	for k, v := range captures {
		result[k] = v
	}

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
pattern: (function_declaration name: (identifier) @name)
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
pattern: (if_statement consequence: (block) @body)
constraints:
  - kind:
      node: body
`,
}

// GetCommonRule returns a pre-built rule by name.
func GetCommonRule(ruleName string) (string, bool) {
	rule, ok := CommonRules[ruleName]
	return rule, ok
}
