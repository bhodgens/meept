package agent

import (
	"encoding/json"
	"strings"
)

// ExtractJSON extracts the first complete JSON object from text that may contain
// markdown fences, prose, or other wrapping. It uses balanced brace matching
// with string-literal awareness to correctly handle:
//   - JSON embedded in markdown code fences (```json ... ```)
//   - Multiple JSON objects (returns the first valid one)
//   - Braces inside string literals (e.g., {"desc": "use { for init"})
//   - Escaped quotes inside strings (e.g., {"desc": "say \"hi\""})
//   - Nested objects and arrays
//
// Returns empty string if no valid JSON object is found.
//
// Note: This function only looks for JSON objects (starting with '{'). A bare
// JSON array like '[1,2,3]' will not be matched; this matches the behavior of
// the legacy extractJSON function and is intentional since the strategic
// planner always emits object-shaped JSON.
func ExtractJSON(s string) string {
	// Strip markdown fences first. If the input contains a fenced block we
	// extract its inner content and scan that. We only strip the *first*
	// fenced region; if it doesn't yield valid JSON we fall through to a
	// full scan of the original text.
	if inner, ok := stripFirstFence(s); ok {
		if result := scanFirstJSONObject(inner); result != "" {
			return result
		}
	}
	return scanFirstJSONObject(s)
}

// stripFirstFence finds the first markdown code fence in s and returns the
// trimmed content between the opening and closing ```. The opening fence may
// include a language identifier (e.g., ```json). Returns ok=false if no fence
// is present or it is never closed.
func stripFirstFence(s string) (string, bool) {
	openIdx := strings.Index(s, "```")
	if openIdx < 0 {
		return "", false
	}
	// Start after the opening backticks.
	rest := s[openIdx+3:]
	// Skip an optional language identifier up to the next newline.
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:]
	} else {
		// One-line fence with no newline: "```{...}```" — unusual but handle it.
		// Nothing to skip; rest already points right after "```".
	}
	// Find the closing fence.
	closeIdx := strings.Index(rest, "```")
	if closeIdx < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[:closeIdx]), true
}

// scanFirstJSONObject scans text byte-by-byte looking for the first substring
// delimited by balanced '{' '}' braces (with string-literal awareness) that
// passes json.Valid. Returns "" if no such substring exists.
func scanFirstJSONObject(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		end := findBalancedEnd(s, i)
		if end < 0 {
			// Unbalanced from this point forward; no candidate can succeed.
			return ""
		}
		candidate := s[i : end+1]
		if json.Valid([]byte(candidate)) {
			return candidate
		}
		// Otherwise advance: the outer loop's i++ moves past this '{'.
	}
	return ""
}

// findBalancedEnd starts at openIdx (which must point at '{') and returns the
// index of the matching '}' that closes the object, accounting for nested
// braces, arrays, string literals, and escape sequences. Returns -1 if the
// string ends before the object is closed.
func findBalancedEnd(s string, openIdx int) int {
	if openIdx < 0 || openIdx >= len(s) || s[openIdx] != '{' {
		return -1
	}
	depth := 0
	inString := false
	escaped := false

	for i := openIdx; i < len(s); i++ {
		c := s[i]

		if escaped {
			// Current byte is consumed as part of an escape sequence.
			escaped = false
			continue
		}

		if inString {
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		// Outside a string.
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
			if depth < 0 {
				// More closing than opening — malformed; no match from openIdx.
				return -1
			}
		}
	}
	return -1
}
