package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tailscale/hujson"
)

// durationToken matches Go-style duration literals like 30s, 2m, 5m, 100ms, 1h30m.
var durationToken = regexp.MustCompile(`(?m)(?:^|[:\s,])\s*(\d+(?:\.\d+)?(?:ns|us|ms|s|m|h|d))\s*`)

// LoadJSON5 reads a JSON5 file, expands environment variables, standardizes to JSON, and unmarshals into v.
func LoadJSON5(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s: %w", path, err)
		}
		return fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Expand env vars in raw content
	content := expandEnvVars(string(data))

	// Standardize JSON5 to JSON
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5 config %s: %w (JSON5 supports comments (// and /* */), trailing commas, and unquoted keys; check for syntax errors near the reported position)", path, err)
	}

	// Unmarshal with detailed error handling for type mismatches
	if err := json.Unmarshal(stdJSON, v); err != nil {
		return wrapJSONUnmarshalError(err, path)
	}
	return nil
}

// wrapJSONUnmarshalError provides detailed, user-friendly error messages for JSON unmarshaling failures.
func wrapJSONUnmarshalError(err error, configPath string) error {
	errMsg := err.Error()

	// Extract field information from error message
	var fieldInfo string
	if idx := strings.Index(errMsg, "into"); idx != -1 {
		// Error format: "json: cannot unmarshal X into Go struct field Y.Z of type T"
		remainder := errMsg[idx:]
		if strings.Contains(remainder, "field") {
			parts := strings.Split(remainder, " ")
			for i, part := range parts {
				if part == "field" && i+1 < len(parts) {
					fieldInfo = parts[i+1]
					break
				}
			}
		}
	}

	// Build context-aware error messages
	var detailMsg string
	var hintMsg string

	switch {
	case strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "type bool") && strings.Contains(errMsg, "array"):
		detailMsg = "expected a boolean value (true/false) but found an array [list]"
		hintMsg = "Hint: This field should be a single true/false value, not a list. Remove the square brackets [] or change to true/false."

	case strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "type bool") && strings.Contains(errMsg, "string"):
		detailMsg = "expected a boolean value (true/false) but found a string"
		hintMsg = "Hint: This field should be true or false (without quotes). If you're trying to set an enum value like 'ask', 'never', or 'always', check the config documentation for valid options."

	case strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "type int") && strings.Contains(errMsg, "string"):
		detailMsg = "expected an integer value but found a string"
		hintMsg = "Hint: Remove quotes around numeric values. For durations, use the raw number or a quoted duration string like \"30s\" if the field supports it."

	case strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "type []string") && strings.Contains(errMsg, "string"):
		detailMsg = "expected an array of strings but found a single string"
		hintMsg = "Hint: Wrap the value in square brackets: [\"value\"] or add more items: [\"value1\", \"value2\"]"

	case strings.Contains(errMsg, "cannot unmarshal"):
		// Generic type mismatch - extract as much info as possible
		detailMsg = fmt.Sprintf("type mismatch: %s", extractTypeMismatch(errMsg))
		hintMsg = "Hint: Check that the value type matches what the field expects (bool, int, string, array, or object)."

	case strings.Contains(errMsg, "unknown field"):
		detailMsg = "unknown configuration field"
		hintMsg = "Hint: This field name is not recognized. Check for typos or see if this feature requires a newer version."

	default:
		detailMsg = errMsg
		hintMsg = "Hint: Review the config file syntax and field values."
	}

	// Build the detailed error message
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("failed to parse config %s:\n", configPath))
	if fieldInfo != "" {
		sb.WriteString(fmt.Sprintf("  Field: %s\n", fieldInfo))
	}
	sb.WriteString(fmt.Sprintf("  Detail: %s\n", detailMsg))
	sb.WriteString(fmt.Sprintf("  %s", hintMsg))

	return fmt.Errorf("%s", sb.String())
}

// extractTypeMismatch extracts the core type information from a Go json.Unmarshal error.
func extractTypeMismatch(errMsg string) string {
	// Parse error like: "json: cannot unmarshal string into Go struct field Config.projects.enabled of type bool"
	parts := strings.Split(errMsg, "cannot unmarshal ")
	if len(parts) < 2 {
		return errMsg
	}

	remainder := parts[1]
	wordParts := strings.SplitN(remainder, " ", 2)
	if len(wordParts) < 1 {
		return errMsg
	}

	foundType := wordParts[0]

	// Find the target type
	if idx := strings.Index(remainder, " of type "); idx != -1 {
		targetType := remainder[idx+9:]
		return fmt.Sprintf("found %s, expected %s", foundType, targetType)
	}

	return fmt.Sprintf("found %s", foundType)
}

// LoadJSON5WithDefault loads JSON5 from path, or returns default if not found.
func LoadJSON5WithDefault(path string, v any) error {
	if err := LoadJSON5(path, v); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

// UnmarshalJSON5 parses JSON5-formatted bytes into a struct.
// Unlike LoadJSON5, this does NOT expand environment variables.
// It also handles Go-style duration literals (e.g. 30s, 2m) and
// Go duration string values (e.g. "30s") in JSON.
func UnmarshalJSON5(data []byte, v any) error {
	// Pre-process: convert bare duration tokens (unquoted) to quoted strings
	content := durationToken.ReplaceAllStringFunc(string(data), func(match string) string {
		// match starts with a delimiter (colon/space/comma), then a duration
		trimmed := strings.TrimLeft(match, " :,\t")
		return match[:len(match)-len(trimmed)] + `"` + trimmed + `"`
	})
	// Also convert quoted duration strings like "30s" to nanoseconds (int64)
	// so the standard json.Unmarshal can handle them as time.Duration values.
	content = quotedDurationToNanos(content)
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5: %w", err)
	}
	return json.Unmarshal(stdJSON, v)
}

// quotedDuration matches a quoted Go duration string like "30s", "2m", "100ms".
var quotedDuration = regexp.MustCompile(`"\d+(?:\.\d+)?(?:ns|us|ms|s|m|h|d)"`)

// quotedDurationToNanos converts quoted time.Duration strings to their nanosecond integer value.
func quotedDurationToNanos(data string) string {
	return quotedDuration.ReplaceAllStringFunc(data, func(match string) string {
		// match is like '"30s' — extract the duration value
		val := match[1 : len(match)-1] // "30s" -> 30s
		d, err := parseDuration(val)
		if err != nil {
			return match // leave unchanged on parse error
		}
		return fmt.Sprintf("%d", d)
	})
}

// parseDuration parses a Go duration string (e.g. "30s", "2m", "1h30m").
// Uses time.ParseDuration for standard Go durations and falls back to a
// custom parser only for the non-standard "d" (day) suffix.
func parseDuration(s string) (int64, error) {
	// Handle the non-standard "d" (day) suffix by converting to hours.
	if strings.HasSuffix(s, "d") {
		numStr := s[:len(s)-1]
		f, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration: %q", s)
		}
		return int64(f * 24 * float64(time.Hour.Nanoseconds())), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %q: %w", s, err)
	}
	return int64(d), nil
}
