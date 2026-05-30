package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// RequestReviewTool invokes the appropriate reviewer agent inline during
// an actor agent's execution. It uses the same delegateRegistry mechanism as
// DelegateTaskTool so that the reviewer's structured feedback returns as a
// tool result in the actor's conversation.
//
// The caller specifies the work to review. If reviewer_id is not provided,
// the tool auto-selects a reviewer based on the caller's agent type using the
// default reviewer mapping (coder -> code-reviewer, debugger -> debug-reviewer, etc.).
type RequestReviewTool struct {
	registry      delegateRegistry
	reviewMapping map[string]string // caller agent ID -> default reviewer ID
}

// NewRequestReviewTool creates a new request review tool.
// The registry is the same AgentRegistry used by DelegateTaskTool.
// reviewMapping maps caller agent IDs to default reviewer agent IDs
// (e.g., "coder" -> "code-reviewer"). If nil, DefaultReviewerMapping is used.
func NewRequestReviewTool(registry delegateRegistry, reviewMapping map[string]string) *RequestReviewTool {
	if reviewMapping == nil {
		reviewMapping = DefaultReviewerMapping()
	}
	return &RequestReviewTool{
		registry:      registry,
		reviewMapping: reviewMapping,
	}
}

// DefaultReviewerMapping returns the standard mapping from actor agent IDs to
// their paired reviewer agent IDs.
func DefaultReviewerMapping() map[string]string {
	return map[string]string{
		"coder":     "code-reviewer",
		"debugger":  "debug-reviewer",
		"planner":   "planner-reviewer",
		"analyst":   "analyst-reviewer",
		"committer": "code-reviewer",
	}
}

func (t *RequestReviewTool) Name() string { return "request_review" }

func (t *RequestReviewTool) Category() string { return "platform" }

func (t *RequestReviewTool) Description() string {
	return "Request an inline code review from a reviewer agent. Call this after completing a logical unit of work (e.g., after writing a file, after a set of changes). Returns structured feedback: approved/rejected with specific issues. If rejected, address the feedback and continue."
}

func (t *RequestReviewTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropMessage: {
				Type:        schemaTypeString,
				Description: "Description of the work to review. Include what was done, what files were changed, and what the intended outcome is.",
			},
			"work_content": {
				Type:        schemaTypeString,
				Description: "Optional: the actual code or content to review. Include this for best results.",
			},
			"reviewer_id": {
				Type:        schemaTypeString,
				Description: "Optional: specific reviewer agent ID (e.g., 'code-reviewer'). If omitted, the default reviewer for this agent type is used.",
			},
			"caller_agent_id": {
				Type:        schemaTypeString,
				Description: "The ID of the agent calling this tool (e.g., 'coder'). Used to select the default reviewer.",
			},
		},
		Required: []string{schemaPropMessage},
	}
}

// InlineReviewResult is the structured result returned by RequestReviewTool.
type InlineReviewResult struct {
	ReviewerID string   `json:"reviewer_id"`
	Status     string   `json:"status"` // "approved", "rejected", "needs_info"
	Feedback   string   `json:"feedback"`
	Issues     []string `json:"issues,omitempty"`
	Approved   bool     `json:"approved"`
}

func (t *RequestReviewTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.registry == nil {
		return tools.NewErrorResult("agent registry not available"), nil
	}

	message, _ := args[schemaPropMessage].(string)
	if message == "" {
		return tools.NewErrorResult("message is required: describe the work to review"), nil
	}

	workContent, _ := args["work_content"].(string)
	reviewerID, _ := args["reviewer_id"].(string)
	callerAgentID, _ := args["caller_agent_id"].(string)

	// Resolve reviewer: explicit > mapping > fallback
	if reviewerID == "" {
		reviewerID = t.resolveReviewer(callerAgentID)
	}

	// Verify the reviewer agent exists
	spec, ok := t.registry.GetSpec(reviewerID)
	if !ok {
		available := t.availableReviewers()
		return tools.NewErrorResult(fmt.Sprintf(
			"reviewer agent not found: %s. Available reviewers: %s",
			reviewerID, joinStrings(available),
		)), nil
	}

	// Build the review request prompt
	reviewPrompt := t.buildReviewPrompt(message, workContent, callerAgentID)

	// Generate an isolated conversation ID for this review
	conversationID := "review-" + spec.ID + "-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Invoke the reviewer synchronously (same mechanism as delegate_task)
	response, err := t.registry.RunAgent(ctx, spec.ID, reviewPrompt, conversationID)
	if err != nil {
		return tools.NewErrorResult(fmt.Sprintf("reviewer execution failed: %v", err)), nil
	}

	// Parse the reviewer's structured JSON response
	return t.parseResponse(reviewerID, response)
}

// resolveReviewer returns the reviewer ID for a given caller agent.
// Falls back to "code-reviewer" if no mapping exists.
func (t *RequestReviewTool) resolveReviewer(callerAgentID string) string {
	if callerAgentID != "" {
		if mapped, ok := t.reviewMapping[callerAgentID]; ok {
			return mapped
		}
	}
	return "code-reviewer"
}

// availableReviewers returns a list of agent IDs that look like reviewers.
func (t *RequestReviewTool) availableReviewers() []string {
	specs := t.registry.ListSpecs()
	var reviewers []string
	for _, s := range specs {
		if s.Role == agent.RoleReviewer {
			reviewers = append(reviewers, s.ID)
		}
	}
	if len(reviewers) == 0 {
		return []string{"(none registered)"}
	}
	return reviewers
}

// buildReviewPrompt constructs the message sent to the reviewer agent.
func (t *RequestReviewTool) buildReviewPrompt(message, workContent, callerAgentID string) string {
	prompt := "## Inline Review Request\n\n"
	if callerAgentID != "" {
		prompt += fmt.Sprintf("**Requesting agent:** %s\n\n", callerAgentID)
	}
	prompt += fmt.Sprintf("## Work Description\n%s\n\n", message)
	if workContent != "" {
		prompt += fmt.Sprintf("## Content to Review\n```\n%s\n```\n\n", workContent)
	}
	prompt += "Review this work for correctness, style, security, and completeness. " +
		"Respond with JSON: {\"status\": \"approved\"|\"rejected\"|\"needs_info\", " +
		"\"feedback\": \"...\", \"issues\": [...]}"
	return prompt
}

// parseResponse extracts the structured review result from the reviewer's response.
func (t *RequestReviewTool) parseResponse(reviewerID, response string) (any, error) {
	// Try to extract JSON from the response (may be wrapped in code block)
	data, extractErr := ExtractJSONFromText(response)
	if extractErr != nil {
		// If the reviewer didn't return structured JSON, wrap the raw text
		return InlineReviewResult{
			ReviewerID: reviewerID,
			Status:     "needs_info",
			Feedback:   response,
			Approved:   false,
		}, nil
	}

	status, _ := data["status"].(string)
	feedback, _ := data["feedback"].(string)
	approved := status == "approved"

	var issues []string
	if rawIssues, ok := data["issues"]; ok {
		if issueSlice, ok := rawIssues.([]any); ok {
			for _, iss := range issueSlice {
				if s, ok := iss.(string); ok {
					issues = append(issues, s)
				}
			}
		}
	}

	return InlineReviewResult{
		ReviewerID: reviewerID,
		Status:     status,
		Feedback:   feedback,
		Issues:     issues,
		Approved:   approved,
	}, nil
}

// Ensure RequestReviewTool implements the Tool interface.
var _ tools.Tool = (*RequestReviewTool)(nil)
