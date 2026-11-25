package task

import (
	"encoding/json"
	"strings"
)

// envelopeProse extracts the human-readable prose from a claims/evidence
// envelope: concatenates the "claims" strings and the "evidence" entry
// descriptions. Returns "" when nothing prose-shaped exists. Best-effort and
// non-panicking on any shape.
//
// Byte-identical port of agent.envelopeProse (internal/agent/envelope_prose.go)
// so the task payload boundary can prose-recover an envelope WITHOUT
// importing internal/agent (the same no-fork constraint that put
// StripPromptScaffolding here, gate T4b 04-clutter/01): internal/agent
// imports internal/task, so the dependency only points one way. The
// canonical conversational semantics stay in internal/agent - callers with
// a loop/handler context keep using agent.StripClaimsEvidenceOrProse;
// payload-boundary callers (task/step RPC + HTTP response builders, the
// task.progress composer) use StripClaimsEvidenceOrProse here.
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
		// plain prose strings (the 8B's observed shape) - handle both.
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
//
// Byte-identical port of agent.findExecutorReportSpan (see envelopeProse
// above for the no-fork rationale).
func findExecutorReportSpan(s string) (int, int, bool) {
	for i := range len(s) {
		if s[i] != '{' {
			continue
		}
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
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
//
// Byte-identical port of agent.isJobCompletionPrefix.
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
//
// Byte-identical port of agent.repairTruncatedJSON.
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
	out := strings.TrimRight(sb.String(), " \t\n\r")
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
//
// Byte-identical port of agent.executorReportProse.
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

// StripClaimsEvidenceOrProse removes a claims/evidence envelope (or the
// executor's structured step-result report) from a step result and returns
// user prose. Legacy contract: when the ENTIRE input was the envelope, the
// envelope's own prose (claims + evidence descriptions) is returned so the
// user still sees what was done instead of an empty or machine-shaped reply.
// Input without an envelope is returned trimmed and unchanged.
//
// Byte-identical port of agent.StripClaimsEvidenceOrProse (gate T2,
// 04-clutter/01, F-ORCH-21) - see envelopeProse above for the no-fork
// rationale. The shared strip helper calls this on every step result that
// crosses a task/step payload boundary; the STORE keeps the raw result
// (display-only, same posture as StripPromptScaffolding).
func StripClaimsEvidenceOrProse(raw string) string {
	if raw == "" {
		return raw
	}
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

// StripClaimsEvidence removes a bare claims/evidence JSON envelope from the
// response and returns the remaining prose.
//
// Brace-aware rather than regex-based: the envelope's "evidence" array nests
// objects, so a non-greedy `.*?\}` pattern stops at the first inner brace and
// leaves trailing `]}` debris. The scanner finds an object that starts with
// the "claims" key, walks to its BALANCING close brace (strings skipped), and
// removes exactly that span - fenced or bare (fenced removal matches
// StripReport's behavior for the same envelope).
//
// Byte-identical port of agent.StripClaimsEvidence.
func StripClaimsEvidence(response string) string {
	out := response
	for {
		start, end, ok := findClaimsEnvelopeSpan(out)
		if !ok {
			break
		}
		out = out[:start] + out[end:]
	}
	return strings.TrimSpace(out)
}

// findClaimsEnvelopeSpan locates one claims/evidence JSON object in s and
// returns (start, end, true) with end exclusive. ok=false when no envelope is
// present. start is the index of the opening '{'.
//
// Tightened relative to agent.findClaimsEnvelopeSpan (audit 2026-11-22 L6):
// a brace-balanced span is stripped only when it is a fully-parsing JSON
// object with a claims/evidence key, so a literal claims-shaped fragment in
// ordinary prose no longer truncates the display payload. The canonical
// conversational strip stays in internal/agent; this is the payload-boundary
// variant it mirrors in semantics for REAL envelopes.
func findClaimsEnvelopeSpan(s string) (int, int, bool) {
	for i := range len(s) {
		if s[i] != '{' {
			continue
		}
		// The object must open with the "claims" OR "evidence" key
		// (whitespace between): the model emits both variants of the
		// mandated envelope (2026-09-25 run NKZiEl - an evidence-only
		// envelope passed through the strip and the reply guard nuked the
		// whole answer).
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
			j++
		}
		if !strings.HasPrefix(s[j:], `"claims"`) && !strings.HasPrefix(s[j:], `"evidence"`) {
			continue
		}
		// Walk to the balancing close brace; strings are skipped so braces
		// inside strings cannot miscount. The k-- compensates for the loop
		// post-increment: skipJSONString returns the index of the closing
		// quote's NEXT byte, and that byte still needs its own switch pass.
		depth := 0
		for k := i; k < len(s); k++ {
			switch s[k] {
			case '"':
				k = skipJSONString(s, k) - 1
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					span := s[i : k+1]
					if isClaimsEnvelopeJSON(span) {
						return i, k + 1, true
					}
					// A brace-balanced fragment that is not a REAL
					// claims/evidence object is ordinary prose:
					// abandon this candidate and re-anchor the scan
					// from i+1 (bughunt M16: falling out of the
					// whole scan here hid a REAL envelope later in
					// the same reply — d1f0cfe1's `break` broke the
					// switch, not the loop, and 3c1c9ae4 deleted it).
					k = len(s) - 1
				}
			}
		}
		// Unbalanced or a failed candidate: no envelope starts inside
		// this fragment; keep scanning from i+1.
		continue
	}
	return 0, 0, false
}

// isClaimsEnvelopeJSON reports whether raw is a single, fully-parsing JSON
// object carrying at least one of the claims/evidence keys. The display
// strip is only allowed to remove a REAL machine envelope: a brace-balanced
// span that merely starts with the words "claims"/"evidence" in ordinary
// prose (or a truncated/malformed fragment) must survive intact.
func isClaimsEnvelopeJSON(raw string) bool {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return false
	}
	_, hasClaims := obj["claims"]
	_, hasEvidence := obj["evidence"]
	return hasClaims || hasEvidence
}

// skipJSONString returns the index just past the string whose opening quote is
// at s[i]. Best-effort on malformed input: returns len(s) on unterminated
// strings.
//
// Byte-identical port of agent.skipJSONString.
func skipJSONString(s string, i int) int {
	i++
	for i < len(s) {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '"':
			return i + 1
		default:
			i++
		}
	}
	return len(s)
}

// StripStepForDisplay returns a step row safe for client consumption: the
// description goes through StripPromptScaffolding and the result through
// StripClaimsEvidenceOrProse. It NEVER mutates the passed row's stored
// callers beyond the caller's own copy semantics: callers pass a freshly
// scanned row (the step store returns new rows per call), so mutating it
// here cannot reach the database - the strip is display-only and the store
// keeps the raw text for the planner.
func StripStepForDisplay(step *TaskStep) *TaskStep {
	if step == nil {
		return nil
	}
	step.Description = StripPromptScaffolding(step.Description)
	step.Result = StripClaimsEvidenceOrProse(step.Result)
	return step
}
