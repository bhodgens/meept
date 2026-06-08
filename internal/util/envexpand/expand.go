// Package envexpand provides environment variable expansion for config files.
//
// This package exists because config files (TOML, JSON5) use both $VAR and
// ${VAR} syntax, while Go's os.ExpandEnv only supports the former.
// The regex-based approach matches both forms and returns an empty string for
// undefined variables, matching the behavior of the original config loading code.
//
// The providers variant (WithPlaceholders) accepts a set of placeholder variable
// names that should be preserved unexpanded -- these are resolved later by
// runtime configuration validation.
package envexpand

import (
	"os"
	"regexp"
	"strings"
)

// envVarPattern matches ${VAR_NAME} or $VAR_NAME patterns.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// Expand expands environment variables in s using both ${VAR} and $VAR syntax.
// Undefined variables are replaced with an empty string.
func Expand(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		var varName string
		if strings.HasPrefix(match, "${") {
			varName = match[2 : len(match)-1]
		} else {
			varName = match[1:]
		}
		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return ""
	})
}

// PlaceholderVars is a map of variable names that should not be expanded.
// This is used for values like MODEL_PATH that are resolved later by runtime
// configuration. To expand with placeholders, pass them via WithPlaceholders.
type PlaceholderVars map[string]bool

// ExpandWithPlaceholders expands environment variables in s, but skips variable
// names present in placeholders. Undefined variables (not in placeholders) are
// replaced with an empty string.
func ExpandWithPlaceholders(s string, placeholders PlaceholderVars) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		var varName string
		if strings.HasPrefix(match, "${") {
			varName = match[2 : len(match)-1]
		} else {
			varName = match[1:]
		}

		// Skip placeholder variables -- they will be expanded later
		if placeholders != nil && placeholders[varName] {
			return match
		}

		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return ""
	})
}
