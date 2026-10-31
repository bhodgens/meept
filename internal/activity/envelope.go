package activity

import (
	"encoding/json"
	"strings"
)

// stripEnvelopeProse removes machine-facing claims/evidence JSON envelopes
// from step results and message content, returning user-safe prose. This is
// a local re-implementation of agent.StripClaimsEvidenceOrProse: importing
// internal/agent from internal/activity risks an import cycle through the
// daemon wiring graph (approved deviation, dispatch addendum 2026-10-30 #4).
func stripEnvelopeProse(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, `"claims"`) && !strings.Contains(trimmed, `"evidence"`) {
		return trimmed
	}
	out := trimmed
	for {
		start, end, ok := envelopeSpan(out)
		if !ok {
			break
		}
		out = strings.TrimSpace(out[:start] + " " + out[end:])
	}
	if out != "" {
		return out
	}
	// The whole input was (or contained only) an envelope: recover its prose.
	return envelopeProse(trimmed)
}

// envelopeSpan locates one claims/evidence JSON object in s and returns
// (start, end, true) with end exclusive; ok=false when none is present.
// Brace-aware with strings skipped so braces inside strings cannot
// miscount; an unterminated envelope drops the tail from its opening brace.
func envelopeSpan(s string) (int, int, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
			j++
		}
		if !strings.HasPrefix(s[j:], `"claims"`) && !strings.HasPrefix(s[j:], `"evidence"`) {
			continue
		}
		depth := 0
		inStr := false
		esc := false
		for k := i; k < len(s); k++ {
			c := s[k]
			if inStr {
				switch {
				case esc:
					esc = false
				case c == '\\':
					esc = true
				case c == '"':
					inStr = false
				}
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return i, k + 1, true
				}
			}
		}
		return i, len(s), true
	}
	return 0, 0, false
}

// envelopeProse extracts the human-readable prose from a claims/evidence
// envelope: the claims strings plus evidence entry descriptions. Returns ""
// when nothing prose-shaped exists. Best-effort and non-panicking.
func envelopeProse(raw string) string {
	var v struct {
		Claims   []string          `json:"claims"`
		Evidence []json.RawMessage `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return ""
	}
	var parts []string
	for _, c := range v.Claims {
		if s := strings.TrimSpace(c); s != "" {
			parts = append(parts, s)
		}
	}
	for _, e := range v.Evidence {
		var s string
		if json.Unmarshal(e, &s) == nil {
			if s = strings.TrimSpace(s); s != "" {
				parts = append(parts, s)
			}
			continue
		}
		var obj struct {
			Description string `json:"description"`
			Detail      string `json:"detail"`
			Note        string `json:"note"`
		}
		if json.Unmarshal(e, &obj) == nil {
			for _, cand := range []string{obj.Description, obj.Detail, obj.Note} {
				if s := strings.TrimSpace(cand); s != "" {
					parts = append(parts, s)
					break
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

// oneLine collapses all whitespace runs to single spaces so a Summary is a
// single line of lowercase-friendly prose.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncateSummary trims, collapses to one line, and bounds s to at most
// maxSummaryRunes runes (the trailing "…" counts within the limit).
func truncateSummary(s string) string {
	s = oneLine(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxSummaryRunes {
		return s
	}
	return string(r[:maxSummaryRunes-1]) + "…"
}

// firstNonEmpty returns the first value with non-blank content.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
