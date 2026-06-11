package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/taint"
)

// TaintBeforeToolCall implements BeforeToolCallHook.
// It checks tool arguments for taint violations before execution.
type TaintBeforeToolCall struct {
	tracker *taint.ExtendedTracker
	logger  *slog.Logger
	config  TaintHookConfig
}

// TaintHookConfig holds configuration for taint tracking hooks.
type TaintHookConfig struct {
	BlockUserInputShell   bool
	BlockSecretNetwork    bool
	BlockUntrustedAgent   bool
	BlockExternalShell    bool
}

// NewTaintBeforeToolCall creates a new taint tracking hook for tool call interception.
func NewTaintBeforeToolCall(tracker *taint.ExtendedTracker, logger *slog.Logger, config TaintHookConfig) *TaintBeforeToolCall {
	if tracker == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TaintBeforeToolCall{
		tracker: tracker,
		logger:  logger.With("component", "taint-before-tool"),
		config:  config,
	}
}

func (t *TaintBeforeToolCall) BeforeToolCall(ctx context.Context, toolCall llm.ToolCall) BlockResult {
	toolName := toolCall.Function.Name

	// Route to appropriate taint check based on tool type
	switch toolName {
	case "shell":
		return t.checkShellTaint(ctx, toolCall)
	case "web_fetch", "web_search":
		return t.checkNetworkTaint(ctx, toolCall)
	case "send_message":
		return t.checkMessageTaint(ctx, toolCall)
	default:
		// No taint check needed for this tool
		return BlockResult{}
	}
}

// checkShellTaint checks if shell command contains tainted user input or external data
func (t *TaintBeforeToolCall) checkShellTaint(ctx context.Context, toolCall llm.ToolCall) BlockResult {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		t.logger.Debug("could not parse shell command arguments", "error", err)
		return BlockResult{}
	}

	// Check if command contains suspicious patterns that suggest external taint
	for _, pattern := range taint.SuspiciousPatterns {
		if strings.Contains(args.Command, pattern) {
			// Create a tainted value and check if it should be blocked
			tv := taint.NewTaintedValue(args.Command, []taint.TaintLabel{taint.TaintExternal}, "shell_command_pattern")
			sink := taint.ShellExecSink()

			if violation := t.tracker.CheckSink(tv, sink); violation != nil {
				if t.config.BlockExternalShell {
					t.logger.Warn("shell command blocked: contains potentially injected external data",
						"command", truncateString(args.Command, 80),
						"pattern", pattern,
						"violation", violation.Error(),
					)
					return BlockResult{
						Block:  true,
						Reason: "command contains potentially injected external data: " + pattern,
					}
				}
				t.logger.Debug("shell command contains external data but blocking disabled",
					"command", truncateString(args.Command, 80),
					"pattern", pattern,
				)
			}
		}
	}

	t.logger.Debug("shell command taint check passed",
		"command", truncateString(args.Command, 80),
	)
	return BlockResult{}
}

// checkNetworkTaint checks if URL contains tainted secrets
func (t *TaintBeforeToolCall) checkNetworkTaint(ctx context.Context, toolCall llm.ToolCall) BlockResult {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		t.logger.Debug("could not parse URL arguments", "error", err)
		return BlockResult{}
	}

	// Check for exfiltration patterns in URL
	lowerURL := strings.ToLower(args.URL)
	exfilPatterns := []string{
		"api_key=", "apikey=", "token=", "secret=",
		"password=", "authorization:", "bearer ",
	}

	for _, pattern := range exfilPatterns {
		if strings.Contains(lowerURL, pattern) {
			if t.config.BlockSecretNetwork {
				// Create a tainted value representing the secret
				tv := taint.NewTaintedValue(args.URL, []taint.TaintLabel{taint.TaintSecret}, "web_fetch_url")
				sink := taint.NetFetchSink()

				if violation := t.tracker.CheckSink(tv, sink); violation != nil {
					t.logger.Warn("URL blocked: potential secret exfiltration",
						"url", truncateString(args.URL, 100),
						"pattern", pattern,
						"violation", violation.Error(),
					)
					return BlockResult{
						Block:  true,
						Reason: "URL may contain sensitive data: " + pattern,
					}
				}
			}
			t.logger.Debug("URL contains sensitive pattern but blocking disabled",
				"url", truncateString(args.URL, 100),
				"pattern", pattern,
			)
		}
	}

	t.logger.Debug("network taint check passed",
		"url", truncateString(args.URL, 100),
	)
	return BlockResult{}
}

// checkMessageTaint checks if message contains tainted untrusted data
func (t *TaintBeforeToolCall) checkMessageTaint(ctx context.Context, toolCall llm.ToolCall) BlockResult {
	var args struct {
		Recipient string `json:"recipient"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		t.logger.Debug("could not parse message arguments", "error", err)
		return BlockResult{}
	}

	// For cross-agent messages, check for untrusted taint
	if strings.HasPrefix(args.Recipient, "agent:") && t.config.BlockUntrustedAgent {
		// Check if content might be from untrusted source
		tv := taint.NewTaintedValue(args.Content, []taint.TaintLabel{taint.TaintUntrusted}, "agent_message")
		sink := taint.AgentMessageSink()

		if violation := t.tracker.CheckSink(tv, sink); violation != nil {
			t.logger.Warn("agent message blocked: contains untrusted data",
				"recipient", args.Recipient,
				"violation", violation.Error(),
			)
			return BlockResult{
				Block:  true,
				Reason: "message contains untrusted data",
			}
		}
	}

	t.logger.Debug("message taint check passed",
		"recipient", args.Recipient,
	)
	return BlockResult{}
}
