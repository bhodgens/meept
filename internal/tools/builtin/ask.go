package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ResponseFunc is the callback type that the agent loop injects into AskTool.
// It should block until the user provides a response, then return it.
// The function receives the question and optional multiple-choice options, and
// returns the user's free-text reply. If the callback is nil, AskTool returns
// an error indicating the ask mechanism is not available.
type ResponseFunc func(ctx context.Context, question string, options []string) (string, error)

// AskTool allows an agent to ask the user a follow-up question during execution.
// The agent loop injects a ResponseFunc callback that blocks until the user
// replies. If options are provided, the user may select from them; otherwise
// a free-text response is expected.
//
// This tool is the agent-level counterpart to the dispatcher's ClarificationReply.
// While the dispatcher asks clarification questions at routing time, AskTool lets
// specialist agents (coder, planner, analyst) ask mid-execution questions.
type AskTool struct {
	askUser ResponseFunc
}

// NewAskTool creates a new ask tool. The responseFunc should be set later via
// SetResponseFunc, or provided here. A nil responseFunc is safe but will cause
// Execute to return an error.
func NewAskTool(responseFunc ResponseFunc) *AskTool {
	return &AskTool{askUser: responseFunc}
}

// SetResponseFunc injects the callback used to solicit user input.
// This is typically called by the agent loop during setup.
// Follows the typed-nil interface guard pattern: a nil func is not stored.
func (t *AskTool) SetResponseFunc(fn ResponseFunc) {
	if fn != nil {
		t.askUser = fn
	}
}

func (t *AskTool) Name() string { return "ask" }

func (t *AskTool) Category() string { return "agent" }

func (t *AskTool) Description() string {
	return "Ask the user a follow-up question and wait for their response. Use this when you need clarification, confirmation, or a decision from the user before proceeding. Returns the user's reply as free text."
}

func (t *AskTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"question": {
				Type:        schemaTypeString,
				Description: "The question to ask the user. Be specific and concise.",
			},
			"options": {
				Type:        schemaTypeArray,
				Description: "Optional list of choices for the user to pick from. When provided, the response will be one of these values (or a free-text reply if the user types something else).",
				Items: &llm.ParameterProperty{
					Type: schemaTypeString,
				},
			},
		},
		Required: []string{"question"},
	}
}

// AskResult is the structured result returned by the ask tool.
type AskResult struct {
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Options  []string `json:"options,omitempty"`
}

// TerminateHint implements tools.TerminatingTool. The ask tool does NOT
// terminate: the user's reply must be fed back to the LLM so it can
// incorporate the answer into its next action. Returning true here would
// suppress the LLM follow-up and silently drop the user's response.
func (t *AskTool) TerminateHint(args map[string]any) bool {
	return false
}

func (t *AskTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// Extract required parameter
	question, _ := args["question"].(string)
	if question == "" {
		return tools.NewErrorResult("question is required and must be a non-empty string"), nil
	}

	// Extract optional options
	var options []string
	if optsRaw, ok := args["options"]; ok {
		switch v := optsRaw.(type) {
		case []string:
			options = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					options = append(options, s)
				}
			}
		}
	}

	// Check callback availability
	if t.askUser == nil {
		return tools.NewErrorResult("ask tool is not available: no user response callback configured"), nil
	}

	// Invoke the callback to get the user's response
	answer, err := t.askUser(ctx, question, options)
	if err != nil {
		// Distinguish context cancellation from other errors
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ask failed: %w", err)
	}

	result := AskResult{
		Question: question,
		Answer:   answer,
	}

	// Only include options in the result if they were originally provided
	if len(options) > 0 {
		result.Options = options
	}

	return tools.NewSuccessResult(result), nil
}

// AskQuestion builds a human-readable clarification string from a question
// and optional choices. This is a utility function that the agent loop (or
// transport layer) can use when presenting the question to the user.
func AskQuestion(question string, options []string) string {
	if len(options) == 0 {
		return question
	}
	var b strings.Builder
	b.WriteString(question)
	b.WriteString("\n\nOptions:\n")
	for i, opt := range options {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, opt)
	}
	return b.String()
}

// Ensure AskTool implements the Tool interface.
var _ tools.Tool = (*AskTool)(nil)

// Ensure AskTool implements TerminatingTool.
var _ tools.TerminatingTool = (*AskTool)(nil)
