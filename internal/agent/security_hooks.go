package agent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
)

// SecurityBeforeToolCall implements BeforeToolCallHook.
// It runs security checks on all security-sensitive tools before execution.
type SecurityBeforeToolCall struct {
	orchestrator *intsecurity.Orchestrator
	logger       *slog.Logger
}

// NewSecurityBeforeToolCall creates a new security hook for tool call interception.
func NewSecurityBeforeToolCall(orch *intsecurity.Orchestrator, logger *slog.Logger) *SecurityBeforeToolCall {
	if orch == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SecurityBeforeToolCall{
		orchestrator: orch,
		logger:       logger.With("component", "security-before-tool"),
	}
}

func (s *SecurityBeforeToolCall) BeforeToolCall(ctx context.Context, toolCall llm.ToolCall) BlockResult {
	toolName := toolCall.Function.Name

	// Shell commands are the only tool type that needs pre-execution scanning
	// beyond what the tools already enforce themselves (FenceChecker at the
	// tool level handles file paths; WebFetchTool checks taint exfiltration).
	if toolName == "shell" {
		return s.scanShellCommand(ctx, toolCall)
	}
	return BlockResult{}
}

// scanShellCommand runs Tirith scan on shell commands
func (s *SecurityBeforeToolCall) scanShellCommand(ctx context.Context, toolCall llm.ToolCall) BlockResult {
	// Extract command from arguments
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		s.logger.Debug("could not parse shell command arguments", "error", err)
		return BlockResult{}
	}

	s.logger.Debug("scanning shell command", "command", truncateString(args.Command, 80))

	blocked, warning, reason := s.orchestrator.ScanShellCommand(ctx, args.Command)

	if blocked {
		s.logger.Info("shell command blocked by security scanner",
			"command", truncateString(args.Command, 80),
			"reason", reason,
		)
		return BlockResult{Block: true, Reason: reason}
	}

	if warning {
		s.logger.Info("shell command flagged with security warning",
			"command", truncateString(args.Command, 80),
			"reason", reason,
		)
		// Don't block on warning, but log it
	}

	return BlockResult{}
}

// SecurityTransformContext implements TransformContextHook.
// It sanitizes user messages through the security orchestrator.
type SecurityTransformContext struct {
	orchestrator *intsecurity.Orchestrator
	logger       *slog.Logger
}

// NewSecurityTransformContext creates a new security hook for context transformation.
func NewSecurityTransformContext(orch *intsecurity.Orchestrator, logger *slog.Logger) *SecurityTransformContext {
	if orch == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SecurityTransformContext{
		orchestrator: orch,
		logger:       logger.With("component", "security-transform"),
	}
}

func (s *SecurityTransformContext) TransformContext(_ context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform {
	modified := false
	threatsFound := false
	var detectedThreats []string

	for i, msg := range messages {
		if msg.Role == llm.RoleUser {
			cleaned, blocked, warnings := s.orchestrator.SanitizeInput(msg.Content)

			// Track and log threats
			if len(warnings) > 0 {
				threatsFound = true
				for _, w := range warnings {
					detectedThreats = append(detectedThreats, string(w.Type)+": "+w.Message)
				}
			}

			if blocked {
				s.logger.Info("user input blocked due to security threat",
					"threats", detectedThreats,
					"input_length", len(msg.Content),
				)
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

	// Log threats even if not blocked (transparency)
	if threatsFound && !modified {
		s.logger.Info("user input contained security threats but was sanitized",
			"threats", detectedThreats,
		)
	}

	if modified {
		s.logger.Debug("context transformed for security sanitization")
		return ContextTransform{
			Messages: messages,
			ToolDefs: toolDefs,
			Modified: true,
			Reason:   "security sanitization",
		}
	}
	return ContextTransform{}
}

// SecretObfuscationHook implements TransformContextHook.
// It obfuscates secrets in all message content before sending to the LLM.
type SecretObfuscationHook struct {
	obfuscator *intsecurity.SecretObfuscator
	logger     *slog.Logger
}

// NewSecretObfuscationHook creates a new hook that obfuscates secrets in messages.
// Returns nil if the obfuscator is nil.
func NewSecretObfuscationHook(obfuscator *intsecurity.SecretObfuscator, logger *slog.Logger) *SecretObfuscationHook {
	if obfuscator == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SecretObfuscationHook{
		obfuscator: obfuscator,
		logger:     logger.With("component", "secret-obfuscation"),
	}
}

func (h *SecretObfuscationHook) TransformContext(_ context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform {
	modified := false
	for i, msg := range messages {
		obfuscated := h.obfuscator.Obfuscate(msg.Content)
		if obfuscated != msg.Content {
			messages[i].Content = obfuscated
			modified = true
		}
	}

	if modified {
		h.logger.Debug("obfuscated secrets in context messages")
		return ContextTransform{
			Messages: messages,
			ToolDefs: toolDefs,
			Modified: true,
			Reason:   "secret obfuscation",
		}
	}
	return ContextTransform{}
}
