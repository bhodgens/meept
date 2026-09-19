package agent

// Plan-vacuity predicate (leaf 03, docs/plans/20260918-tool-boundary-hardening/03-plan-vacuity-gate.md).
//
// Regression context (2026-09-18 e2e): a planner step whose result was pure
// first-person future-intent narration — "I need to create the file
// 'hello.txt'... I will perform the task now... Let me execute the necessary
// commands" — was heuristic-approved as "trivial task, non-empty result".
// Narrating intent to act is not evidence of planning work; approving it
// lets a doomed loop continue. This predicate detects the shape; the
// heuristicReviewPasses guard in review_manager.go routes such steps to
// full review.

import (
	"encoding/json"
	"strings"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// vacuityScanWindow bounds the narration-marker scan to the first 400
// characters of the result. Slicing is boundary-safe (mirror of the
// guard-first-slice-inside rule): a shorter result scans whole.
const vacuityScanWindow = 400

// planVacuityNarrationMarkers are first-person future-intent narration
// markers. Matched case-insensitively as substrings within the scan window.
var planVacuityNarrationMarkers = []string{
	"i will ",
	"i'll ",
	"i need to ",
	"let me ",
	"i am going to ",
}

// planVacuityHints is the planner-hint set: step tool hints whose product is
// planning work. Sourced from the production hint values (grep evidence):
//   - internal/agent/intent.go:23  IntentPlan      = "plan"
//   - internal/agent/intent.go:64  IntentArchitect = "architect"
//   - internal/agent/intent.go:32  IntentReview    = "review" (pair:reviewer
//     steps, strategic.go:1250: reviewerStep.ToolHint = string(IntentReview))
//   - internal/agent/strategic.go:1211 fallback steps carry req.Intent
//   - internal/agent/planner_template.go:208-210 the planner prompt
//     advertises "plan" as a valid step tool_hint
var planVacuityHints = map[string]bool{
	"plan":      true,
	"architect": true,
	"review":    true,
}

// planLooksVacuous reports whether a step whose TOOL HINT denotes planning
// work produced no planning artifact:
//   - zero task-management tool evidence (task_create/task_update/task_list)
//     in step.Evidence, AND
//   - step.Result text matches first-person future-intent narration
//     ("I will ", "I'll ", "I need to ", "Let me ", "I am going to "),
//     case-insensitive, within the first 400 chars (safe slicing), AND
//   - contains no structured artifact: no JSON object/array start, no
//     markdown numbered list (^\s*\d+\.) and no bullet plan (- / * at line
//     start with 2+ lines).
//
// Non-planner hints are unaffected (returns false). The predicate is
// deliberately conservative: a false positive costs one full review, while
// a false negative re-opens the approved-vacuous doom-loop hole.
func planLooksVacuous(step *task.TaskStep) bool {
	if step == nil {
		return false
	}
	hint := strings.ToLower(strings.TrimSpace(step.ToolHint))
	if !planVacuityHints[hint] {
		return false
	}
	if hasTaskManagementEvidence(step.Evidence) {
		return false
	}
	result := strings.TrimSpace(step.Result)
	if result == "" {
		// Empty is a different failure mode (handled by the non-empty
		// heuristic check) — not vacuity.
		return false
	}
	// Structured step-job envelopes: scan the response narration only,
	// mirroring heuristicReviewPasses scan scope. The envelope itself is a
	// JSON object, which would otherwise always defeat the artifact check.
	scanText := result
	if response, ok := extractEnvelopeResponse(result); ok {
		scanText = strings.TrimSpace(response)
		if scanText == "" {
			// Envelope with an empty response: nothing narrated, nothing
			// structured in the response — not vacuity by narration.
			return false
		}
	}
	if hasStructuredPlanArtifact(scanText) {
		return false
	}
	return hasVacuityNarrationMarker(scanText)
}

// hasTaskManagementEvidence reports whether any evidence record was produced
// by a task-management tool (task_create / task_update / task_list) or any
// evidence exists at all. The leaf contract requires ZERO task-management
// tool evidence for vacuity; hasMeaningfulEvidence (review_manager.go)
// provides the broader zero-evidence base case.
func hasTaskManagementEvidence(evs []models.Evidence) bool {
	for _, e := range evs {
		if e.Type == "" && e.Subject == "" && e.Value == "" {
			continue // zero-value record: no real evidence (F9 shape)
		}
		return true
	}
	return false
}

// extractEnvelopeResponse decodes a structured step-job envelope and
// returns the model's response narration (same scope heuristicReviewPasses
// scans). ok is true when result parsed as an envelope with a response field.
func extractEnvelopeResponse(result string) (string, bool) {
	var envelope struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err == nil && envelope.Response != "" {
		return envelope.Response, true
	}
	return "", false
}

// hasVacuityNarrationMarker reports whether any narration marker appears
// case-insensitively within the first 400 chars of text (safe slicing).
func hasVacuityNarrationMarker(text string) bool {
	window := text
	if len(window) > vacuityScanWindow {
		window = window[:vacuityScanWindow]
	}
	lower := strings.ToLower(window)
	for _, marker := range planVacuityNarrationMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// hasStructuredPlanArtifact reports whether the text carries a structured
// plan artifact: a JSON object/array start, a markdown numbered list line
// (^\s*\d+\.), or 2+ bullet lines (- / * at line start). A single bullet
// does NOT defeat detection — narration + one bullet stays vacuous.
func hasStructuredPlanArtifact(text string) bool {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	bulletLines := 0
	for _, line := range strings.Split(text, "\n") {
		stripped := strings.TrimLeft(line, " \t")
		if isNumberedListItem(stripped) {
			return true
		}
		if strings.HasPrefix(stripped, "- ") || strings.HasPrefix(stripped, "* ") {
			bulletLines++
			if bulletLines >= 2 {
				return true
			}
		}
	}
	return false
}

// isNumberedListItem reports whether a line starts a markdown numbered list
// item (e.g. "1. Create hello.txt").
func isNumberedListItem(line string) bool {
	dot := strings.Index(line, ".")
	if dot <= 0 || dot > 9 { // at most 9 digits keeps this bounded
		return false
	}
	for _, r := range line[:dot] {
		if r < '0' || r > '9' {
			return false
		}
	}
	rest := line[dot+1:]
	return strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")
}
