package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// AgentReport represents the structured report from an agent's response.
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
	for i, ch := range rest {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				endIdx = i + 1
				break
			}
		}
		if endIdx > 0 {
			break
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

// StripReport removes the report JSON block from the response.
// This returns the response without the report for user display.
func StripReport(response string) string {
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
	Category    string  `json:"category"`    // "security", "performance", "maintainability", "follow-up"
	Priority    string  `json:"priority"`    // "critical", "high", "medium", "low"
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
