package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// AgentReport represents the structured report from an agent's response.
//
//nolint:revive // stutter with package name is intentional for API clarity
type AgentReport struct {
	// Status indicates the completion state: completed, partial, failed, needs_input
	Status string `json:"status"`

	// Accomplished lists what the agent completed.
	Accomplished []string `json:"accomplished"`

	// NotDone lists what was not completed.
	NotDone []string `json:"not_done,omitempty"`

	// Issues lists problems encountered.
	Issues []string `json:"issues,omitempty"`

	// Observations provides context for follow-up work.
	Observations []string `json:"observations,omitempty"`

	// SuggestedNextAgent is the ID of the agent that should handle follow-up.
	SuggestedNextAgent string `json:"suggested_next_agent,omitempty"`

	// SuggestedNextHint is the finished agent's ADVISORY (free-form,
	// bounded at extraction) suggestion for who or what should pick up the
	// next step (agent-routing tree leaf 03). Purely observability — the
	// sanctioned routing channel remains SuggestedNextAgent +
	// request_handoff; nothing in this tree consumes the hint for routing.
	SuggestedNextHint string `json:"suggested_next_hint,omitempty"`

	// UserDecisionNeeded indicates if user input is required to proceed.
	UserDecisionNeeded bool `json:"user_decision_needed"`

	// DecisionContext explains what decision the user needs to make.
	DecisionContext string `json:"decision_context,omitempty"`
}

// ReportStatus constants
const (
	ReportStatusCompleted  = "completed"
	ReportStatusPartial    = "partial"
	ReportStatusFailed     = "failed"
	ReportStatusNeedsInput = "needs_input"
)

// IsComplete returns true if the status is completed.
func (r *AgentReport) IsComplete() bool {
	return r.Status == ReportStatusCompleted
}

// NeedsRouting returns true if the report suggests routing to another agent.
func (r *AgentReport) NeedsRouting() bool {
	return r.SuggestedNextAgent != "" && !r.UserDecisionNeeded
}

// NeedsUserInput returns true if user decision is required.
func (r *AgentReport) NeedsUserInput() bool {
	return r.UserDecisionNeeded
}

// HasIssues returns true if there were any issues reported.
func (r *AgentReport) HasIssues() bool {
	return len(r.Issues) > 0
}

// extractReportRegex matches a JSON code block containing a report.
var extractReportRegex = regexp.MustCompile("(?s)```json\\s*\\n(\\{[^`]*\"status\"[^`]*\\})\\s*\\n```")

// claimsBlockRegex matches a JSON code block containing claims/evidence.
var claimsBlockRegex = regexp.MustCompile("(?s)```json\\s*\\n\\{.*?\"claims\".*?\\}.*?\\n```")

// bareClaimsObjectRegex matches a BARE (unfenced) claims/evidence JSON object
// occupying a whole paragraph of the response. Task-step prompts MANDATE this
// envelope as the agent's final output (loop.go evidenceSection); the 8B often
// emits it without the fences, which StripReport's fenced-only regexes missed
// — the raw envelope then reached the reply guard, which replaced the ENTIRE
// reply with a canned line and the user never saw the agent's prose (e2e run
// 2026-09-23 EHpc4r: T1 created hello.txt but A4's "reply names the path"
// failed because the whole reply was replaced). The regex anchors on the
// leading "claims" key so ordinary prose or JSON data dumps are untouched.
// bareClaimsObjectRegex is retained for reference in tests only; envelope
// removal is brace-aware (findClaimsEnvelopeSpan) because the evidence array
// nests objects and a non-greedy regex cannot find the balancing brace.
var bareClaimsObjectRegex = regexp.MustCompile("(?s)\\s*\\{\\s*\"claims\"\\s*:.*?\"evidence\"\\s*:.*?\\}\\s*")

var _ = bareClaimsObjectRegex // referenced by comments/tests; keep linters quiet

// ExtractReport parses an AgentReport from the agent's response.
// It looks for a JSON code block containing a status field.
// Returns nil if no valid report is found.
func ExtractReport(response string) *AgentReport {
	// Try to find a JSON code block with a report
	matches := extractReportRegex.FindStringSubmatch(response)
	if len(matches) < 2 {
		// Try without code block fences (in case agent forgot them)
		return extractReportFallback(response)
	}

	jsonStr := strings.TrimSpace(matches[1])
	var report AgentReport
	if err := json.Unmarshal([]byte(jsonStr), &report); err != nil {
		return nil
	}

	// Validate status
	if !isValidStatus(report.Status) {
		return nil
	}

	return &report
}

// extractReportFallback tries to find a JSON report without code fences.
func extractReportFallback(response string) *AgentReport {
	// Look for JSON object with status field
	before, _, ok := strings.CutLast(response, `"status"`)
	if !ok {
		return nil
	}

	// Find the opening brace before status
	braceIdx := strings.LastIndex(before, "{")
	if braceIdx == -1 {
		return nil
	}

	// Find the closing brace
	rest := response[braceIdx:]
	depth := 0
	endIdx := -1
braceLoop:
	for i, ch := range rest {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				endIdx = i + 1
				break braceLoop
			}
		}
	}

	if endIdx == -1 {
		return nil
	}

	jsonStr := rest[:endIdx]
	var report AgentReport
	if err := json.Unmarshal([]byte(jsonStr), &report); err != nil {
		return nil
	}

	if !isValidStatus(report.Status) {
		return nil
	}

	return &report
}

// isValidStatus checks if a status string is valid.
func isValidStatus(status string) bool {
	switch status {
	case ReportStatusCompleted, ReportStatusPartial, ReportStatusFailed, ReportStatusNeedsInput:
		return true
	default:
		return false
	}
}

// StripClaimsEvidence removes a bare claims/evidence JSON envelope from the
// response and returns the remaining prose. Exported so the chat handler's
// bestStepResult can clean a task step's stored result before it becomes a
// user-facing reply: the envelope is machine-facing evidence mandated by the
// step prompt (loop.go evidenceSection), not user prose, and left in place it
// trips the reply guard's tool_result_json rule, which replaces the whole
// reply with a canned line.
//
// Brace-aware rather than regex-based: the envelope's "evidence" array nests
// objects, so a non-greedy `.*?\}` pattern stops at the first inner brace and
// leaves trailing `]}` debris. The scanner finds an object that starts with
// the "claims" key, walks to its BALANCING close brace (strings skipped), and
// removes exactly that span — fenced or bare (fenced removal matches
// StripReport's behavior for the same envelope).
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
// Tightened to match internal/task's envelope-boundary variant (audit
// 2026-11-22 L6, ported here bughunt wave2 finding 5 — the two files are
// documented byte-identical ports and had diverged): a brace-balanced span
// is stripped only when it is a fully-parsing JSON object with a
// claims/evidence key, so a literal claims-shaped fragment in ordinary
// prose no longer truncates the conversational reply.
func findClaimsEnvelopeSpan(s string) (int, int, bool) {
	for i := range len(s) {
		if s[i] != '{' {
			continue
		}
		// The object must open with the "claims" OR "evidence" key
		// (whitespace between): the model emits both variants of the
		// mandated envelope (2026-09-25 run NKZiEl — an evidence-only
		// envelope passed through the strip and the reply guard nuked the
		// whole answer).
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '	' || s[j] == '\n' || s[j] == '\r') {
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
					// claims/evidence object is ordinary prose. Keep
					// scanning instead of truncating the display text.
					break
				}
			}
		}
		// Unbalanced: no envelope here.
		return 0, 0, false
	}
	return 0, 0, false
}

// isClaimsEnvelopeJSON reports whether raw is a single, fully-parsing JSON
// object carrying at least one of the claims/evidence keys. The display
// strip is only allowed to remove a REAL machine envelope: a brace-balanced
// span that merely starts with the words "claims"/"evidence" in ordinary
// prose (or a truncated/malformed fragment) must survive intact.
//
// Byte-identical port of task.isClaimsEnvelopeJSON (bughunt wave2,
// finding 5: the task boundary gained this guard in cccf5b64 while the
// canonical conversational strip kept trusting the leading-key prefix).
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
func skipJSONString(s string, i int) int {
	i++
	for i < len(s) {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '"':
			return i + 1
		}
		i++
	}
	return i
}

// StripReport removes the report JSON block and claims/evidence blocks from the response.
// This returns the response without structured metadata for user display.
func StripReport(response string) string {
	// Remove claims/evidence JSON code block (FIX #0047), then the bare
	// (unfenced) envelope form.
	response = claimsBlockRegex.ReplaceAllString(response, "")
	response = StripClaimsEvidence(response)

	// Remove JSON code block with report
	matches := extractReportRegex.FindStringSubmatch(response)
	if len(matches) >= 1 {
		response = strings.Replace(response, matches[0], "", 1)
	}

	// Clean up trailing whitespace
	return strings.TrimSpace(response)
}

// RouteAction represents the action to take based on a report.
type RouteAction int

const (
	// RouteActionClose closes the task and notifies the user.
	RouteActionClose RouteAction = iota

	// RouteActionRoute routes to the suggested next agent.
	RouteActionRoute

	// RouteActionNotifyUser notifies the user and awaits input.
	RouteActionNotifyUser

	// RouteActionNotifyError notifies the user of failure.
	RouteActionNotifyError
)

// DetermineRouteAction determines what action to take based on a report.
// This implements the dispatcher feedback loop logic.
func DetermineRouteAction(report *AgentReport) RouteAction {
	if report == nil {
		// No report, just close
		return RouteActionClose
	}

	switch report.Status {
	case ReportStatusCompleted:
		if report.UserDecisionNeeded {
			return RouteActionNotifyUser
		}
		if report.SuggestedNextAgent != "" {
			return RouteActionRoute
		}
		return RouteActionClose

	case ReportStatusPartial:
		if report.UserDecisionNeeded {
			return RouteActionNotifyUser
		}
		if report.SuggestedNextAgent != "" {
			return RouteActionRoute
		}
		return RouteActionNotifyUser

	case ReportStatusNeedsInput:
		return RouteActionNotifyUser

	case ReportStatusFailed:
		return RouteActionNotifyError

	default:
		return RouteActionClose
	}
}

// String returns a human-readable description of the route action.
func (a RouteAction) String() string {
	switch a {
	case RouteActionClose:
		return "close"
	case RouteActionRoute:
		return "route"
	case RouteActionNotifyUser:
		return "notify_user"
	case RouteActionNotifyError:
		return "notify_error"
	default:
		return "unknown"
	}
}
