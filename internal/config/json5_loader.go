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

// durationTokenExact matches a WHOLE value that is one duration literal
// ("1h", "250ms", "1h30m"). Used by preprocessDurations' line-based pass so
// only true duration VALUES are rewritten — never duration-like text inside
// quoted strings. The old content-wide regex (durationToken) rewrote
// duration-like text inside string values ("runs about 1h"), corrupting the
// JSON; the line-anchored exact match replaces it.
var durationTokenExact = regexp.MustCompile(`^\d+(?:\.\d+)?(?:ns|us|ms|s|m|h|d)$`)

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
	content, err := ExpandEnvVars(string(data))
	if err != nil {
		return fmt.Errorf("failed to expand env vars in config %s: %w", path, err)
	}

	// Convert Go-style duration values ("30s", "1h", quoted or bare) to
	// nanosecond integers so time.Duration fields unmarshal. Same
	// preprocessing as UnmarshalJSON5 — without it, any config loaded
	// through this path rejects duration strings that the TOML path accepts
	// (found via [skills.evolver] interval in a -c config, 2026-08-29).
	content = preprocessDurations(content)

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

// preprocessDurations applies the shared duration preprocessing used by both
// config load paths: bare duration tokens get quoted, then quoted duration
// values become nanosecond integers.
//
// The bare-token pass runs LINE-BY-LINE with a colon-anchored check: a bare
// token qualifies only when it follows "key: " on the same line (a JSON
// value position). This avoids the string-mangling bug where duration-like
// text INSIDE a quoted string value ("runs about 1h total") got rewritten.
// stringDurationKeys are config keys declared as STRING fields whose
// values happen to look like Go durations (e.g. queue.interactive_window
// holds "5m" and must stay a string). quotedDurationToNanos must not
// rewrite their values to nanosecond integers — the schema validator
// rejects the number (configui save/load roundtrip regression, tree 04
// leaf 01 follow-up).
var stringDurationKeys = map[string]bool{
	"interactive_window": true,
}

func preprocessDurations(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		value := strings.TrimSpace(line[colon+1:])
		if value == "" || strings.HasPrefix(value, `"`) {
			// Quoted values are handled by quotedDurationToNanos below
			// (which only rewrites true duration-shaped quoted values);
			// free text inside strings must pass through untouched.
			continue
		}
		// Strip a trailing comma (JSON5 trailing-comma style) before the
		// exact-match test, preserving it in the rewritten line.
		token := strings.TrimSuffix(value, ",")
		if durationTokenExact.MatchString(token) {
			lines[i] = line[:colon+1] + ` "` + token + `"` + strings.TrimPrefix(value, token)
		}
	}
	return quotedDurationToNanos(strings.Join(lines, "\n"))
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
	content := preprocessDurations(string(data))
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5: %w", err)
	}
	return json.Unmarshal(stdJSON, v)
}

// quotedDuration matches a quoted Go duration string used as a JSON value
// (preceded by `: ` to avoid matching duration-like strings embedded in
// descriptive text). Examples: "30s", "2m", "100ms".
var quotedDuration = regexp.MustCompile(`:\s*"(\d+(?:\.\d+)?(?:ns|us|ms|s|m|h|d))"`)

// quotedDurationToNanos converts quoted time.Duration strings to their nanosecond integer value.
// Only matches quoted durations that appear as JSON values (after `: `) to
// avoid corrupting non-duration string fields.
func quotedDurationToNanos(data string) string {
	// Key-aware pass: exempt string-declared duration-like fields by
	// checking the key that anchors each quoted value on its line.
	lines := strings.Split(data, "\n")
	for i, line := range lines {
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}
		key := strings.TrimRight(strings.TrimSpace(line[:colon]), `"`)
		if idx := strings.LastIndexAny(key, " \t{,"); idx >= 0 {
			key = strings.TrimSpace(key[idx+1:])
		}
		key = strings.Trim(key, `"`)
		if stringDurationKeys[strings.TrimSpace(key)] {
			continue
		}
		lines[i] = quotedDuration.ReplaceAllStringFunc(line, func(match string) string {
			sub := quotedDuration.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			d, err := parseDuration(sub[1])
			if err != nil {
				return match
			}
			colonIdx := strings.Index(match, ":")
			prefix := match[:colonIdx+1]
			return prefix + fmt.Sprintf(" %d", d)
		})
	}
	return strings.Join(lines, "\n")
}

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
