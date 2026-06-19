// Package queries provides tree-sitter query loading for repomap.
package queries

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"
)

// queriesFS embeds the query files.
//
//go:embed "go"
//go:embed "python"
//go:embed "typescript"
//go:embed "rust"
//go:embed "java"
//go:embed "generic"
var queriesFS embed.FS

// LoadQuery loads a tree-sitter query for a given language.
// It first tries language-specific queries, then falls back to generic.
func LoadQuery(lang, queryFile string) (string, error) {
	langName := strings.ToLower(lang)

	// Try language-specific query first
	query, err := loadFromFS(langName, queryFile)
	if err == nil && query != "" {
		return query, nil
	}

	// Fall back to generic queries
	query, err = loadFromFS("generic", queryFile)
	if err == nil && query != "" {
		return query, nil
	}

	// Return default query if nothing found
	return getDefaultQuery(langName, queryFile), nil
}

// loadFromFS attempts to load a query from the embedded filesystem.
func loadFromFS(langName, queryFile string) (string, error) {
	path := filepath.Join(langName, queryFile)
	data, err := queriesFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("query not found: %s", path)
	}
	return string(data), nil
}

// getDefaultQuery returns default query patterns when no query file is found.
func getDefaultQuery(langName, queryFile string) string {
	if queryFile == "definitions.scm" {
		switch langName {
		case "go":
			return `((function_declaration name: (identifier) @name.definition.function) @definition.function)
((method_declaration name: (identifier) @name.definition.method) @definition.method)
((type_declaration type_spec: (type_spec name: (type_identifier) @name.definition.type)) @definition.type)
((const_declaration (const_spec name: (identifier) @name.definition.constant)) @definition.constant)
((var_declaration (var_spec name: (identifier) @name.definition.variable)) @definition.variable)`
		case "python":
			return `((function_definition name: (identifier) @name.definition.function) @definition.function)
((class_definition name: (identifier) @name.definition.class) @definition.class)
((assignment left: (identifier) @name.definition.variable) @definition.variable)`
		case "typescript", "javascript":
			return `((function_declaration name: (identifier) @name.definition.function) @definition.function)
((method_definition name: (property_identifier) @name.definition.method) @definition.method)
((class_declaration name: (identifier) @name.definition.class) @definition.class)
((variable_declarator name: (identifier) @name.definition.variable) @definition.variable)`
		case "rust":
			return `((function_item name: (identifier) @name.definition.function) @definition.function)
((struct_item name: (type_identifier) @name.definition.type) @definition.type)
((enum_item name: (type_identifier) @name.definition.type) @definition.type)
((const_item name: (identifier) @name.definition.constant) @definition.constant)`
		default:
			return `((identifier) @name.definition)`
		}
	}

	// Default references query
	if queryFile == "references.scm" {
		switch langName {
		case "go", "python", "typescript", "javascript", "rust", "java":
			return `((identifier) @ref.variable)`
		default:
			return `((identifier) @ref)`
		}
	}

	return ""
}
