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
			sb.WriteString(renderEvidenceProse(s))
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
				sb.WriteString(renderEvidenceProse(s))
				sb.WriteString("\n")
				break
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// renderEvidenceProse turns one evidence-entry string into user prose.
// Plain prose passes through byte-identically (TrimSpace only). A
// job-relay entry that embeds the machine executor-report envelope
// ("job <id> completed by agent <x>: {\"status\":...}") has its JSON
// recovered to prose via the gate-T2 stripper, so raw JSON keys never
// render in a user reply (F-ORCH-21, output-filters-06: the queue relay
// evidence from components.go carries exactly this shape).
func renderEvidenceProse(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "job ") || !strings.Contains(s, " completed by agent ") {
		return s
	}
	if recovered := StripClaimsEvidenceOrProse(s); recovered != "" {
		return recovered
	}
	return s
}

// StripClaimsEvidenceOrProse removes the envelope; when the ENTIRE input was
// the envelope, returns the envelope's own prose (claims + evidence
// descriptions) so the user still sees what was done instead of an empty or
// machine-shaped reply (2026-09-25 run NKZiEl: the model emitted an
// evidence-only envelope whose inner strings held the full user answer).
//
// Gate T2 (04-clutter/01, F-ORCH-21): the executor's step-result shape
// {"status","accomplished","not_done","issues","observations",...} is the
// SAME machine envelope class and is handled HERE, in place (no parallel
// stripper): prose is recovered even from the live truncated job-prefixed
// form ("job <id> completed by agent <x>: {...<truncated>"), and no raw
// JSON keys may render in a user reply.
func StripClaimsEvidenceOrProse(raw string) string {
	// Only a REAL removal counts: StripClaimsEvidence returns the input
	// trimmed when no claims/evidence span exists, so comparing against
	// the trimmed raw is what distinguishes "legacy envelope removed"
	// from "no legacy envelope" (gate T2: an executor-report envelope
	// used to ride the no-op return and leak its raw JSON keys).
	if stripped := StripClaimsEvidence(raw); stripped != "" && stripped != strings.TrimSpace(raw) {
		return stripped
	}
	// Whole input was (or contained only) the envelope: recover its prose.
	spanStart, spanEnd, ok := findClaimsEnvelopeSpan(raw)
	if ok {
		// Legacy contract preserved exactly: a claims/evidence envelope
		// returns its own prose, even when that prose is empty.
		return envelopeProse(raw[spanStart:spanEnd])
	}
	// Executor-report envelope shape (gate T2). Checked after the legacy
	// lane so claims/evidence semantics keep their exact old behavior.
	if start, end, truncOK := findExecutorReportSpan(raw); truncOK {
		prose := executorReportProse(raw[start:end])
		prefix := strings.TrimSpace(raw[:start])
		if isJobCompletionPrefix(prefix) {
			prefix = ""
		}
		if prose == "" {
			return strings.TrimSpace(raw)
		}
		if prefix == "" {
			return prose
		}
		return prefix + "\n" + prose
	}
	return strings.TrimSpace(raw)
}

// executorReportKeys are the leading keys of the executor's structured
// step-result report (planner-mandated, machine-facing). An object opening
// with one of these AND carrying a report list is never user prose.
var executorReportKeys = []string{`"status"`, `"accomplished"`, `"not_done"`, `"issues"`, `"observations"`}

// executorReportListKeys are the keys whose presence proves the object is
// an executor report rather than arbitrary status JSON.
var executorReportListKeys = []string{`"accomplished"`, `"not_done"`, `"issues"`, `"observations"`}

// findExecutorReportSpan locates the executor-report JSON object in s.
// Like findClaimsEnvelopeSpan it walks to the balancing close brace, but
// TRUNCATED input (the live job-preview shape ends mid-string) is still a
// report: when the walk hits EOF with open containers, the span runs to
// the end of the string. Returns (start, end, true) with end exclusive.
func findExecutorReportSpan(s string) (int, int, bool) {
	for i := range len(s) {
		if s[i] != '{' {
			continue
		}
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '	' || s[j] == '\n' || s[j] == '\r') {
			j++
		}
		leadOK := false
		for _, key := range executorReportKeys {
			if strings.HasPrefix(s[j:], key) {
				leadOK = true
				break
			}
		}
		if !leadOK {
			continue
		}
		// Must carry at least one report list key to qualify (a bare
		// {"status": ...} of unknown origin is not an executor report).
		listOK := false
		for _, key := range executorReportListKeys {
			if strings.Contains(s[i:], key+":") || strings.Contains(s[i:], key+" :") {
				listOK = true
				break
			}
		}
		if !listOK {
			continue
		}
		depth := 0
		for k := i; k < len(s); k++ {
			switch s[k] {
			case '"':
				k = skipJSONString(s, k) - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i, k + 1, true
				}
			}
		}
		// Unbalanced at EOF: the live truncated-preview shape. The span
		// is whatever remains; the parser repairs the cut below.
		if depth > 0 {
			return i, len(s), true
		}
		return 0, 0, false
	}
	return 0, 0, false
}

// isJobCompletionPrefix reports whether text is the queue relay's glue in
// front of a step result ("job job-... completed by agent coder:") - the
// live p6 shape. That prefix is relay machinery, never user prose.
func isJobCompletionPrefix(prefix string) bool {
	if prefix == "" {
		return false
	}
	if !strings.HasPrefix(prefix, "job ") || !strings.Contains(prefix, " completed by agent ") {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(prefix), ":")
}

// repairTruncatedJSON closes unterminated strings/containers so the
// truncated executor report (cut mid-value by the upstream preview cap)
// parses. Well-formed input is returned unchanged. Containers close in
// stack order (] for arrays, } for objects).
func repairTruncatedJSON(s string) string {
	var stack []byte
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, c)
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if !inString && len(stack) == 0 {
		return s
	}
	var sb strings.Builder
	sb.WriteString(s)
	if inString {
		sb.WriteString(`"`)
	}
	// Best-effort closer: drop a trailing dangling comma/colon (or a cut
	// "key:" with no value) so the repaired JSON parses.
	out := strings.TrimRight(sb.String(), " 	\n\r")
	out = strings.TrimRight(out, ",:")
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '[' {
			out += "]"
		} else {
			out += "}"
		}
	}
	return out
}

// executorReportProse renders the user-facing prose of an executor report
// object: accomplished items first, then observations, then anything the
// step explicitly flagged (not_done/issues) - as bullets, no JSON keys.
func executorReportProse(raw string) string {
	parsed := repairTruncatedJSON(strings.TrimSpace(raw))
	var v struct {
		Accomplished []string `json:"accomplished"`
		NotDone      []string `json:"not_done"`
		Issues       []string `json:"issues"`
		Observations []string `json:"observations"`
	}
	if err := json.Unmarshal([]byte(parsed), &v); err != nil {
		return ""
	}
	var sb strings.Builder
	writeItems := func(prefix string, items []string) {
		for _, it := range items {
			it = strings.TrimSpace(it)
			if it == "" {
				continue
			}
			sb.WriteString("- ")
			sb.WriteString(prefix)
			sb.WriteString(it)
			sb.WriteString("\n")
		}
	}
	writeItems("", v.Accomplished)
	writeItems("", v.Observations)
	writeItems("not done: ", v.NotDone)
	writeItems("issue: ", v.Issues)
	return strings.TrimSpace(sb.String())
}
