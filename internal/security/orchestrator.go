package security

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
)

// OrchestratorConfig holds configuration for the security orchestrator.
type OrchestratorConfig struct {
	// Input sanitization settings
	SanitizeInputs     bool
	SanitizeStrictness StrictnessLevel

	// Output monitoring settings
	MonitorOutput bool
	RedactOutput  bool

	// Shell command scanning settings
	ScanShellCommands bool
	TirithBinary      string

	// Audit logging settings
	EnableAuditLog bool
	AuditDBPath    string
}

// DefaultOrchestratorConfig returns a configuration with sensible defaults.
func DefaultOrchestratorConfig() OrchestratorConfig {
	return OrchestratorConfig{
		SanitizeInputs:     true,
		SanitizeStrictness: StrictnessStandard,
		MonitorOutput:      true,
		RedactOutput:       true,
		ScanShellCommands:  true,
		TirithBinary:       "tirith",
		EnableAuditLog:     false,
		AuditDBPath:        "",
	}
}

// Orchestrator coordinates all security components.
// It provides a unified interface for input sanitization, output monitoring,
// and shell command scanning.
type Orchestrator struct {
	config        OrchestratorConfig
	sanitizer     *InputSanitizer
	outputMonitor *OutputMonitor
	promptGuard   *PromptGuard
	tirithScanner *TirithScanner
	logger        *slog.Logger

	// Metrics tracking (atomic for thread safety)
	inputsProcessed       atomic.Int64
	inputsSanitized       atomic.Int64
	inputsBlocked         atomic.Int64
	outputsScanned        atomic.Int64
	outputsWithCreds      atomic.Int64
	outputsRedacted       atomic.Int64
	commandsScanned       atomic.Int64
	commandsBlocked       atomic.Int64
	commandsWarned        atomic.Int64

	// Mutex for complex operations
	mu sync.RWMutex
}

// NewOrchestrator creates a new security orchestrator with the given configuration.
func NewOrchestrator(cfg OrchestratorConfig, logger *slog.Logger) *Orchestrator {
	if logger == nil {
		logger = slog.Default()
	}

	o := &Orchestrator{
		config: cfg,
		logger: logger.With("component", "security-orchestrator"),
	}

	// Initialize components based on configuration
	if cfg.SanitizeInputs {
		o.sanitizer = NewInputSanitizer(cfg.SanitizeStrictness)
		o.promptGuard = NewPromptGuard()
		logger.Info("Input sanitization enabled",
			"strictness", cfg.SanitizeStrictness.String(),
		)
	}

	if cfg.MonitorOutput {
		o.outputMonitor = NewOutputMonitor()
		logger.Info("Output monitoring enabled",
			"redact", cfg.RedactOutput,
		)
	}

	if cfg.ScanShellCommands {
		o.tirithScanner = NewTirithScanner(cfg.TirithBinary)
		logger.Info("Shell command scanning enabled",
			"binary", cfg.TirithBinary,
		)
	}

	return o
}

// SanitizeInput processes user input through the security pipeline.
// Returns the (possibly modified) text, whether it was blocked, and any warnings.
func (o *Orchestrator) SanitizeInput(text string) (string, bool, []Warning) {
	o.inputsProcessed.Add(1)

	if !o.config.SanitizeInputs || o.sanitizer == nil {
		// Sanitization disabled - pass through unchanged
		return text, false, nil
	}

	// Run through input sanitizer
	result := o.sanitizer.Sanitize(text)

	// Check for severe threats that should block the input
	blocked := false
	for _, threat := range result.ThreatsDetected {
		// Block on critical injection patterns
		if isCriticalThreat(threat.Type) {
			blocked = true
			o.inputsBlocked.Add(1)
			o.logger.Warn("Input blocked due to critical threat",
				"threat_type", threat.Type,
				"threat_message", threat.Message,
			)
			break
		}
	}

	if result.WasModified {
		o.inputsSanitized.Add(1)
		o.logger.Debug("Input was sanitized",
			"threats_detected", len(result.ThreatsDetected),
			"was_modified", result.WasModified,
		)
	}

	// Also run prompt guard detection for additional warnings
	if o.promptGuard != nil {
		hasInjection, matches := o.promptGuard.DetectInjection(text)
		if hasInjection && len(matches) > 0 {
			for _, match := range matches {
				result.ThreatsDetected = append(result.ThreatsDetected, Warning{
					Type:    match.Type,
					Message: "Detected injection pattern: " + match.Pattern,
				})
			}
		}
	}

	return result.CleanText, blocked, result.ThreatsDetected
}

// ScanOutput processes LLM output for credential leakage.
// Returns the (possibly redacted) text, whether credentials were found, and any warnings.
func (o *Orchestrator) ScanOutput(text string) (string, bool, []Warning) {
	o.outputsScanned.Add(1)

	if !o.config.MonitorOutput || o.outputMonitor == nil {
		// Output monitoring disabled - pass through unchanged
		return text, false, nil
	}

	result := o.outputMonitor.Scan(text)

	if result.HasCredentials {
		o.outputsWithCreds.Add(1)
		o.logger.Warn("Credentials detected in output",
			"warning_count", len(result.Warnings),
		)

		if o.config.RedactOutput {
			o.outputsRedacted.Add(1)
			o.logger.Debug("Output was redacted")
			return result.RedactedText, true, result.Warnings
		}
	}

	return text, result.HasCredentials, result.Warnings
}

// ScanShellCommand scans a shell command before execution using Tirith.
// Returns whether the command should be blocked, whether there's a warning, and the reason.
func (o *Orchestrator) ScanShellCommand(ctx context.Context, command string) (blocked bool, warning bool, reason string) {
	o.commandsScanned.Add(1)

	if !o.config.ScanShellCommands || o.tirithScanner == nil {
		// Shell command scanning disabled - allow execution
		return false, false, ""
	}

	// Check if Tirith is available (graceful degradation)
	if !o.tirithScanner.IsAvailable(ctx) {
		o.logger.Debug("Tirith not available, allowing command execution")
		return false, false, "tirith scanner not available"
	}

	// Scan the command
	result := o.tirithScanner.Scan(ctx, command)
	if result == nil {
		// Scan failed - allow execution (graceful degradation)
		return false, false, ""
	}

	if result.Blocked {
		o.commandsBlocked.Add(1)
		reason := "command blocked by security scanner"
		if result.Message != nil {
			reason = *result.Message
		}
		o.logger.Warn("Shell command blocked",
			"command", truncateCommand(command),
			"severity", ptrStringValue(result.Severity),
			"rule_id", ptrStringValue(result.RuleID),
			"reason", reason,
		)
		return true, false, reason
	}

	if result.Warning {
		o.commandsWarned.Add(1)
		reason := "command flagged with warning"
		if result.Message != nil {
			reason = *result.Message
		}
		o.logger.Info("Shell command warning",
			"command", truncateCommand(command),
			"severity", ptrStringValue(result.Severity),
			"rule_id", ptrStringValue(result.RuleID),
			"reason", reason,
		)
		return false, true, reason
	}

	return false, false, ""
}

// WrapUserInput wraps text in user-input boundary markers for prompt injection defense.
func (o *Orchestrator) WrapUserInput(text string) string {
	if o.promptGuard == nil {
		return text
	}
	return o.promptGuard.WrapUserInput(text)
}

// WrapToolOutput wraps output from a tool in tool-output boundary markers.
func (o *Orchestrator) WrapToolOutput(toolName, output string) string {
	if o.promptGuard == nil {
		return output
	}
	return o.promptGuard.WrapToolOutput(toolName, output)
}

// Stats returns security metrics as a map.
func (o *Orchestrator) Stats() map[string]int64 {
	return map[string]int64{
		"inputs_processed":   o.inputsProcessed.Load(),
		"inputs_sanitized":   o.inputsSanitized.Load(),
		"inputs_blocked":     o.inputsBlocked.Load(),
		"outputs_scanned":    o.outputsScanned.Load(),
		"outputs_with_creds": o.outputsWithCreds.Load(),
		"outputs_redacted":   o.outputsRedacted.Load(),
		"commands_scanned":   o.commandsScanned.Load(),
		"commands_blocked":   o.commandsBlocked.Load(),
		"commands_warned":    o.commandsWarned.Load(),
	}
}

// Config returns the current configuration.
func (o *Orchestrator) Config() OrchestratorConfig {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.config
}

// IsEnabled returns whether the orchestrator has any security features enabled.
func (o *Orchestrator) IsEnabled() bool {
	return o.config.SanitizeInputs || o.config.MonitorOutput || o.config.ScanShellCommands
}

// isCriticalThreat determines if a threat type should cause input blocking.
func isCriticalThreat(threatType string) bool {
	// These threat types indicate active prompt injection attempts
	criticalTypes := map[string]bool{
		"instruction_override":      true,
		"role_switch_attempt":       true,
		"instruction_injection":     true,
		"role_marker_system":        true,
		"role_marker_assistant":     true,
		"markdown_role_injection":   true,
		"special_token_chatml":      true,
		"special_token_llama":       true,
		"special_token_llama_sys":   true,
		"special_token_phi":         true,
		"special_token_eos":         true,
	}
	return criticalTypes[threatType]
}

// truncateCommand truncates a command for logging purposes.
func truncateCommand(cmd string) string {
	const maxLen = 100
	if len(cmd) <= maxLen {
		return cmd
	}
	return cmd[:maxLen] + "..."
}

// ptrStringValue safely dereferences a string pointer for logging.
func ptrStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ParseStrictnessLevel parses a strictness level from a string.
func ParseStrictnessLevel(s string) StrictnessLevel {
	switch strings.ToLower(s) {
	case "permissive":
		return StrictnessPermissive
	case "standard":
		return StrictnessStandard
	case "strict":
		return StrictnessStrict
	default:
		return StrictnessStandard
	}
}
