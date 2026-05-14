package agent

import (
	"context"
	"encoding/json"

	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/llm"
)

// SecurityBeforeToolCall implements BeforeToolCallHook.
// It runs Tirith scan on shell commands before execution.
type SecurityBeforeToolCall struct {
	orchestrator *intsecurity.Orchestrator
}

// NewSecurityBeforeToolCall creates a new security hook for tool call interception.
func NewSecurityBeforeToolCall(orch *intsecurity.Orchestrator) *SecurityBeforeToolCall {
	if orch == nil {
		return nil
	}
	return &SecurityBeforeToolCall{orchestrator: orch}
}

func (s *SecurityBeforeToolCall) BeforeToolCall(ctx context.Context, toolCall llm.ToolCall) BlockResult {
	// Only scan shell tool calls
	if toolCall.Function.Name != "shell" {
		return BlockResult{}
	}

	// Extract command from arguments
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		// Can't parse args, allow through
		return BlockResult{}
	}

	blocked, _, reason := s.orchestrator.ScanShellCommand(ctx, args.Command)
	if blocked {
		return BlockResult{Block: true, Reason: reason}
	}
	return BlockResult{}
}

// SecurityTransformContext implements TransformContextHook.
// It sanitizes user messages through the security orchestrator.
type SecurityTransformContext struct {
	orchestrator *intsecurity.Orchestrator
}

// NewSecurityTransformContext creates a new security hook for context transformation.
func NewSecurityTransformContext(orch *intsecurity.Orchestrator) *SecurityTransformContext {
	if orch == nil {
		return nil
	}
	return &SecurityTransformContext{orchestrator: orch}
}

func (s *SecurityTransformContext) TransformContext(_ context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform {
	modified := false
	for i, msg := range messages {
		if msg.Role == llm.RoleUser {
			cleaned, blocked, _ := s.orchestrator.SanitizeInput(msg.Content)
			if blocked {
				return ContextTransform{
					Messages: []llm.ChatMessage{{
						Role:    llm.RoleAssistant,
						Content: "I cannot process that request due to security concerns.",
					}},
					Modified: true,
					Reason:   "input blocked by security",
				}
			}
			if cleaned != messages[i].Content {
				messages[i].Content = cleaned
				modified = true
			}
		}
	}

	if modified {
		return ContextTransform{
			Messages: messages,
			ToolDefs: toolDefs,
			Modified: true,
			Reason:   "security sanitization",
		}
	}
	return ContextTransform{}
}
