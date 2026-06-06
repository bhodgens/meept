package ast

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// QueryRule represents an ast-grep style YAML rule.
type QueryRule struct {
	ID          string                `yaml:"id"`
	Language    string                `yaml:"language,omitempty"`
	Pattern     string                `yaml:"pattern"`
	Constraints map[string]Constraint `yaml:"constraints,omitempty"`
	Transform   map[string]string     `yaml:"transform,omitempty"`
	Fix         string                `yaml:"fix,omitempty"`
}

// Constraint defines a filter on a captured node.
type Constraint struct {
	Regex    string `yaml:"regex,omitempty"`
	Eq       string `yaml:"eq,omitempty"`
	NotEq    string `yaml:"not_eq,omitempty"`
	HasChild string `yaml:"has_child,omitempty"`
}

// ParseQueryRule parses a YAML string into a QueryRule.
func ParseQueryRule(yamlData string) (*QueryRule, error) {
	var rule QueryRule
	if err := yaml.Unmarshal([]byte(yamlData), &rule); err != nil {
		return nil, fmt.Errorf("invalid YAML rule: %w", err)
	}
	if rule.Pattern == "" {
		return nil, fmt.Errorf("rule must have a 'pattern' field")
	}
	return &rule, nil
}

// MatchCheck holds a captured node's text and type for constraint checking.
type MatchCheck struct {
	Name     string
	Text     string
	NodeType string
}

// ApplyConstraints filters matches by checking constraints on captured nodes.
func (r *QueryRule) ApplyConstraints(captures []MatchCheck) bool {
	if len(r.Constraints) == 0 {
		return true
	}

	for _, capture := range captures {
		constraint, ok := r.Constraints[capture.Name]
		if !ok {
			continue
		}

		if constraint.Regex != "" {
			re, err := regexp.Compile(constraint.Regex)
			if err != nil {
				return false
			}
			if !re.MatchString(capture.Text) {
				return false
			}
		}

		if constraint.Eq != "" && capture.Text != constraint.Eq {
			return false
		}

		if constraint.NotEq != "" && capture.Text == constraint.NotEq {
			return false
		}

		if constraint.HasChild != "" && !strings.Contains(capture.NodeType, constraint.HasChild) {
			return false
		}
	}

	return true
}

// ApplyTransforms transforms captured text using simple string substitution.
func (r *QueryRule) ApplyTransforms(captures []MatchCheck) map[string]string {
	result := make(map[string]string)
	if len(r.Transform) == 0 {
		return result
	}

	for _, capture := range captures {
		if tmpl, ok := r.Transform[capture.Name]; ok {
			// Simple substitution: replace $NAME with captured text
			result[capture.Name] = strings.ReplaceAll(tmpl, "$"+capture.Name, capture.Text)
		}
	}

	return result
}
