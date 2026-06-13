package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"github.com/tailscale/hujson"
)

// durationToken matches Go-style duration literals like 30s, 2m, 5m, 100ms, 1h30m.
var durationToken = regexp.MustCompile(`(?m)(?:^|[:\s,])\s*(\d+(?:\.\d+)?(?:ns|us|ms|s|m|h|d))\s*`)

// LoadJSON5 reads a JSON5 file, expands environment variables, standardizes to JSON, and unmarshals into v.
func LoadJSON5(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Expand env vars in raw content
	content := expandEnvVars(string(data))
	// Standardize JSON5 to JSON
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5: %w", err)
	}

	// Unmarshal with detailed error handling for type mismatches
	if err := json.Unmarshal(stdJSON, v); err != nil {
		// Provide more specific error messages for common type mismatches
		errMsg := err.Error()
		if strings.Contains(errMsg, "cannot unmarshal") {
			if strings.Contains(errMsg, "type bool") && strings.Contains(errMsg, "array") {
				return fmt.Errorf("config type mismatch: expected a boolean value but found an array - check the field for a list that should be a single true/false value")
			}
			if strings.Contains(errMsg, "cannot unmarshal string") && strings.Contains(errMsg, "type bool") {
				return fmt.Errorf("config type mismatch: expected a boolean value but found a string - check for quotes around true/false or enum values like 'ask'/'never'/'always'")
			}
			if strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "type") {
				return fmt.Errorf("config type mismatch: %s - verify the field type matches the expected value", errMsg)
			}
		}
		return fmt.Errorf("failed to parse config JSON: %w", err)
	}
	return nil
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
func parseDuration(s string) (d int64, err error) {
	var raw string
	// Try to find the suffix
	for _, suffix := range []string{"ns", "us", "ms", "s", "m", "h", "d"} {
		if strings.HasSuffix(s, suffix) {
			raw = s[:len(s)-len(suffix)]
			break
		}
	}
	if raw == "" {
		// Try composite format like "1h30m" or "1h30m15s"
		raw = s
		d, err = parseCompositeDuration(s)
		return
	}
	var f float64
	_, err = fmt.Sscanf(raw, "%f", &f)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	switch s[len(raw):] {
	case "ns":
		return int64(f), nil
	case "us", "µs":
		return int64(f * 1e3), nil
	case "ms":
		return int64(f * 1e6), nil
	case "s":
		return int64(f * 1e9), nil
	case "m":
		return int64(f * 60 * 1e9), nil
	case "h":
		return int64(f * 3600 * 1e9), nil
	case "d":
		return int64(f * 86400 * 1e9), nil
	default:
		return 0, fmt.Errorf("unknown duration suffix: %q", s[len(raw):])
	}
}

// parseCompositeDuration parses composite duration strings like "1h30m15s".
func parseCompositeDuration(s string) (int64, error) {
	var d int64
	raw := s
	units := map[string]int64{"d": 86400e9, "h": 3600e9, "m": 60e9, "s": 1e9, "ms": 1e6, "us": 1e3, "ns": 1}
	for _, suffix := range []string{"d", "h", "m", "s", "ms", "us", "ns"} {
		for strings.HasSuffix(raw, suffix) {
			prefix := raw[:len(raw)-len(suffix)]
			if prefix == "" {
				return 0, fmt.Errorf("invalid duration: %q", s)
			}
			var f float64
			if _, err := fmt.Sscanf(prefix, "%f", &f); err != nil {
				// Check for another unit: e.g. "1h" -> try next char as unit start
				break
			}
			d += int64(float64(f) * float64(units[suffix]))
			raw = prefix
			if raw == "" {
				return 0, fmt.Errorf("invalid duration: %q", s)
			}
		}
	}
	if raw != "" {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	return d, nil
}
