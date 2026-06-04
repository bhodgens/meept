// Package markdown provides helpers for extracting structured data from markdown.
package markdown

import (
	"encoding/json"
	"regexp"
	"strings"
)

// jsonFenceRe matches a fenced JSON block (```json...```).
var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\n(.*?)\\n```")

// ExtractJSON extracts the first valid JSON object or array found within
// markdown content. It tries fenced code blocks first, then falls back to
// top-level JSON.
func ExtractJSON(content string) []byte {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	// Try each fenced JSON block.
	matches := jsonFenceRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if len(m) > 1 {
			candidate := strings.TrimSpace(m[1])
			if isValidJSON(candidate) {
				return []byte(candidate)
			}
		}
	}

	// No fenced block worked — try the full content as raw JSON.
	if isValidJSON(content) {
		return []byte(content)
	}

	// Final fallback: scan for object/array literals.
	for _, candidate := range []string{
		extractBracketed(content, "{"),
		extractBracketed(content, "["),
	} {
		if candidate != "" && isValidJSON(candidate) {
			return []byte(candidate)
		}
	}

	return nil
}

// extractBracketed extracts the outermost balanced bracketed text starting
// with the given open bracket ('{' or '[').
func extractBracketed(s string, open string) string {
	close := "}"
	if open == "[" {
		close = "]"
	}

	start := strings.Index(s, open)
	if start == -1 {
		return ""
	}

	depth := 0
	for i := start; i < len(s); i++ {
		ch := string(s[i])
		if ch == open {
			depth++
		} else if ch == close {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// ExtractJSONArray works like ExtractJSON but specifically for JSON arrays.
func ExtractJSONArray(content string) []byte {
	data := ExtractJSON(content)
	if data == nil {
		return nil
	}
	// Must start with '[' for it to be an array.
	if len(data) > 0 && data[0] == '[' {
		return data
	}
	return nil
}

func isValidJSON(s string) bool {
	if s == "" {
		return false
	}
	var v any
	return json.Unmarshal([]byte(s), &v) == nil
}
