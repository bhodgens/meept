package agent

import (
	"encoding/json"
	"strings"
)

// envelopeProse extracts the human-readable prose from a claims/evidence
// envelope: concatenates the "claims" strings and the "evidence" entry
// descriptions. Returns "" when nothing prose-shaped exists. Best-effort and
// non-panicking on any shape.
func envelopeProse(raw string) string {
	var v struct {
		Claims   []string          `json:"claims"`
		Evidence []json.RawMessage `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range v.Claims {
		c = strings.TrimSpace(c)
		if c != "" {
			sb.WriteString("- ")
			sb.WriteString(c)
			sb.WriteString("\n")
		}
	}
	for _, e := range v.Evidence {
		// Evidence entries are either objects ({type, path, ...}) or
		// plain prose strings (the 8B's observed shape) — handle both.
		var s string
		if json.Unmarshal(e, &s) == nil && strings.TrimSpace(s) != "" {
			sb.WriteString("- ")
			sb.WriteString(strings.TrimSpace(s))
			sb.WriteString("\n")
			continue
		}
		var probe map[string]any
		if json.Unmarshal(e, &probe) != nil {
			continue
		}
		for _, key := range []string{"description", "detail", "note"} {
			if s, ok := probe[key].(string); ok && strings.TrimSpace(s) != "" {
				sb.WriteString("- ")
				sb.WriteString(strings.TrimSpace(s))
				sb.WriteString("\n")
				break
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// StripClaimsEvidenceOrProse removes the envelope; when the ENTIRE input was
// the envelope, returns the envelope's own prose (claims + evidence
// descriptions) so the user still sees what was done instead of an empty or
// machine-shaped reply (2026-09-25 run NKZiEl: the model emitted an
// evidence-only envelope whose inner strings held the full user answer).
func StripClaimsEvidenceOrProse(raw string) string {
	if stripped := StripClaimsEvidence(raw); stripped != "" {
		return stripped
	}
	// Whole input was (or contained only) the envelope: recover its prose.
	spanStart, spanEnd, ok := findClaimsEnvelopeSpan(raw)
	if !ok {
		return strings.TrimSpace(raw)
	}
	return envelopeProse(raw[spanStart:spanEnd])
}
