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
	statusIdx := strings.LastIndex(response, `"status"`)
	if statusIdx == -1 {
		return nil
	}

	// Find the opening brace before status
	braceIdx := strings.LastIndex(response[:statusIdx], "{")
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
func findClaimsEnvelopeSpan(s string) (int, int, bool) {
	for i := 0; i < len(s); i++ {
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
					return i, k + 1, true
				}
			}
		}
		// Unbalanced: no envelope here.
		return 0, 0, false
	}
	return 0, 0, false
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

// CategorizedRecommendation represents a recommendation from an agent.
type CategorizedRecommendation struct {
	Category    string  `json:"category"` // "security", "performance", "maintainability", "follow-up"
	Priority    string  `json:"priority"` // "critical", "high", "medium", "low"
	Description string  `json:"description"`
	AgentID     string  `json:"agent_id"`
	Confidence  float64 `json:"confidence"`
	// Optional fields
	CodeSnippet     string   `json:"code_snippet,omitempty"`
	RelatedFiles    []string `json:"related_files,omitempty"`
	EstimatedEffort string   `json:"estimated_effort,omitempty"` // "small", "medium", "large"
}

// AggregatedTaskReport represents the final report for a completed task.
type AggregatedTaskReport struct {
	// Summary is a brief overview of what was accomplished
	Summary string `json:"summary"`
	// StepsCompleted is the number of steps that completed successfully
	StepsCompleted int `json:"steps_completed"`
	// StepsTotal is the total number of steps
	StepsTotal int `json:"steps_total"`
	// Recommendations are categorized recommendations from all agents
	Recommendations []CategorizedRecommendation `json:"recommendations"`
	// ExecutionTime is the total execution time
	ExecutionTime string `json:"execution_time"`
}

// TaskReportAggregator aggregates recommendations and results from all sub-agents
// into a structured final report.
type TaskReportAggregator struct {
	// recommendations collects recommendations from all steps
	recommendations []CategorizedRecommendation
}

// NewTaskReportAggregator creates a new report aggregator.
func NewTaskReportAggregator() *TaskReportAggregator {
	return &TaskReportAggregator{
		recommendations: make([]CategorizedRecommendation, 0),
	}
}

// ExtractRecommendations extracts recommendations from a step's result text.
// It looks for structured recommendation blocks in the agent output.
func (a *TaskReportAggregator) ExtractRecommendations(stepResult, agentID string) []CategorizedRecommendation {
	if stepResult == "" {
		return nil
	}

	// Try to find recommendation blocks in the result
	// Format: ```recommendations\n[{...}]\n```
	var recs []CategorizedRecommendation

	// Try JSON extraction from recommendation blocks
	codeBlockPattern := regexp.MustCompile("(?s)```recommendations\\s*\\n(.*?)\\n```")
	matches := codeBlockPattern.FindAllStringSubmatch(stepResult, -1)
	for _, match := range matches {
		if len(match) > 1 {
			var parsed []CategorizedRecommendation
			if err := json.Unmarshal([]byte(strings.TrimSpace(match[1])), &parsed); err == nil {
				// Fill in agent ID if not set
				for i := range parsed {
					if parsed[i].AgentID == "" {
						parsed[i].AgentID = agentID
					}
				}
				recs = append(recs, parsed...)
			}
		}
	}

	// Also try extracting from standard JSON blocks that contain "category" and "priority"
	if len(recs) == 0 {
		jsonPattern := regexp.MustCompile("(?s)```json\\s*\\n(.*?)\\n```")
		jsonMatches := jsonPattern.FindAllStringSubmatch(stepResult, -1)
		for _, match := range jsonMatches {
			if len(match) > 1 {
				var parsed []CategorizedRecommendation
				candidate := strings.TrimSpace(match[1])
				// Only parse if it looks like recommendations (array with category/priority)
				if strings.Contains(candidate, `"category"`) && strings.Contains(candidate, `"priority"`) {
					if err := json.Unmarshal([]byte(candidate), &parsed); err == nil {
						for i := range parsed {
							if parsed[i].AgentID == "" {
								parsed[i].AgentID = agentID
							}
						}
						recs = append(recs, parsed...)
					}
				}
			}
		}
	}

	return recs
}

// AddRecommendations adds extracted recommendations to the aggregator.
func (a *TaskReportAggregator) AddRecommendations(recs []CategorizedRecommendation) {
	a.recommendations = append(a.recommendations, recs...)
}

// BuildReport creates the final aggregated task report.
func (a *TaskReportAggregator) BuildReport(summary string, stepsCompleted, stepsTotal int, executionTime string) *AggregatedTaskReport {
	return &AggregatedTaskReport{
		Summary:         summary,
		StepsCompleted:  stepsCompleted,
		StepsTotal:      stepsTotal,
		Recommendations: a.DeduplicateRecommendations(),
		ExecutionTime:   executionTime,
	}
}

// DeduplicateRecommendations removes duplicate recommendations based on description.
func (a *TaskReportAggregator) DeduplicateRecommendations() []CategorizedRecommendation {
	seen := make(map[string]bool)
	var deduped []CategorizedRecommendation

	// Sort by priority: critical > high > medium > low
	priorityOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}

	// Sort recommendations by priority
	sorted := make([]CategorizedRecommendation, len(a.recommendations))
	copy(sorted, a.recommendations)
	for i := range len(sorted) - 1 {
		for j := i + 1; j < len(sorted); j++ {
			pi, ok1 := priorityOrder[sorted[i].Priority]
			pj, ok2 := priorityOrder[sorted[j].Priority]
			if !ok1 {
				pi = 99
			}
			if !ok2 {
				pj = 99
			}
			if pi > pj {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	for _, rec := range sorted {
		key := strings.ToLower(strings.TrimSpace(rec.Description))
		if !seen[key] {
			seen[key] = true
			deduped = append(deduped, rec)
		}
	}

	return deduped
}

// GetRecommendationsByCategory returns recommendations filtered by category.
func (a *TaskReportAggregator) GetRecommendationsByCategory(category string) []CategorizedRecommendation {
	var filtered []CategorizedRecommendation
	for _, rec := range a.recommendations {
		if rec.Category == category {
			filtered = append(filtered, rec)
		}
	}
	return filtered
}

// GetRecommendationsByPriority returns recommendations filtered by priority.
func (a *TaskReportAggregator) GetRecommendationsByPriority(priority string) []CategorizedRecommendation {
	var filtered []CategorizedRecommendation
	for _, rec := range a.recommendations {
		if rec.Priority == priority {
			filtered = append(filtered, rec)
		}
	}
	return filtered
}

// RecommendationCount returns the total number of collected recommendations.
func (a *TaskReportAggregator) RecommendationCount() int {
	return len(a.recommendations)
}
