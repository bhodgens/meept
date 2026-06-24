package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/security/taint"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
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
		TirithBinary:       BinaryTirith,
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
	taintTracker  *TaintTracker // optional: information flow tracking
	logger        *slog.Logger
	auditDB       *sql.DB // SQLite database for audit logging (nil when disabled)

	// Metrics tracking (atomic for thread safety)
	inputsProcessed  atomic.Int64
	inputsSanitized  atomic.Int64
	inputsBlocked    atomic.Int64
	outputsScanned   atomic.Int64
	outputsWithCreds atomic.Int64
	outputsRedacted  atomic.Int64
	commandsScanned  atomic.Int64
	commandsBlocked  atomic.Int64
	commandsWarned   atomic.Int64

	// Mutex for complex operations
	mu sync.RWMutex
}

// TaintTracker is the taint tracking interface used by the orchestrator.
type TaintTracker = taint.ExtendedTracker

// SetTaintTracker sets the taint tracker for information flow security.
func (o *Orchestrator) SetTaintTracker(tt *TaintTracker) {
	if tt != nil {
		o.taintTracker = tt
		o.logger.Info("Taint tracking enabled")
	}
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

	// Initialize audit logging if enabled
	if cfg.EnableAuditLog && cfg.AuditDBPath != "" {
		if err := o.initAuditDB(cfg.AuditDBPath); err != nil {
			logger.Error("Failed to initialize audit log database",
				"path", cfg.AuditDBPath,
				"error", err,
			)
		} else {
			logger.Info("Audit logging enabled",
				"path", cfg.AuditDBPath,
			)
		}
	}

	return o
}

// SanitizeInput processes user input through the security pipeline.
// Returns the (possibly modified) text, whether it was blocked, and any warnings.
func (o *Orchestrator) SanitizeInput(text string) (sanitized string, ok bool, warnings []Warning) {
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
		if !isCriticalThreat(threat.Type) {
			continue
		}
		blocked = true
		o.inputsBlocked.Add(1)
		o.logger.Warn("Input blocked due to critical threat",
			"threat_type", threat.Type,
			"threat_message", threat.Message,
		)
		o.logAuditEvent("input_blocked", "critical", map[string]any{
			"threat_type": threat.Type,
			"message":     threat.Message,
			"text_length": len(text),
		}, "sanitizer")
		break
	}

	if result.WasModified {
		o.inputsSanitized.Add(1)
		o.logger.Debug("Input was sanitized",
			"threats_detected", len(result.ThreatsDetected),
			"was_modified", result.WasModified,
		)
		o.logAuditEvent("input_sanitized", "warning", map[string]any{
			"threats":     result.ThreatsDetected,
			"text_length": len(text),
		}, "sanitizer")
	}

	// FIX #SECURITY: Log warnings even if input wasn't blocked or modified
	// This ensures sanitizer warnings are in the audit trail
	if !blocked && !result.WasModified && len(result.ThreatsDetected) > 0 {
		o.logger.Debug("Input triggered warnings (not blocked)",
			"warnings", len(result.ThreatsDetected),
		)
		o.logAuditEvent("input_warning", "info", map[string]any{
			"threats":     result.ThreatsDetected,
			"text_length": len(text),
		}, "sanitizer")
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
func (o *Orchestrator) ScanOutput(text string) (sanitized string, ok bool, warnings []Warning) {
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

		o.logAuditEvent("output_credentials", "warning", map[string]any{
			"warnings":     result.Warnings,
			"text_length":  len(text),
			"was_redacted": o.config.RedactOutput,
		}, "output_monitor")

		if o.config.RedactOutput {
			o.outputsRedacted.Add(1)
			o.logger.Debug("Output was redacted")
			return result.RedactedText, true, result.Warnings
		}
	}

	return text, result.HasCredentials, result.Warnings
}

// ScanShellCommand scans a shell command before execution using taint tracking and Tirith.
// Returns whether the command should be blocked, whether there's a warning, and the reason.
func (o *Orchestrator) ScanShellCommand(ctx context.Context, command string) (blocked, warning bool, reason string) {
	o.commandsScanned.Add(1)

	// Check taint tracking first (if enabled)
	if o.taintTracker != nil {
		if violation := o.taintTracker.CheckShellCommand(command); violation != nil {
			o.commandsBlocked.Add(1)
			o.logger.Warn("Shell command blocked by taint tracking",
				"command", truncateCommand(command),
				"violation", violation.Error(),
			)
			o.logAuditEvent("taint_blocked", "critical", map[string]any{
				"command":   truncateCommand(command),
				"violation": violation.Error(),
			}, "taint")
			return true, false, violation.Error()
		}
	}

	if !o.config.ScanShellCommands || o.tirithScanner == nil {
		// Shell command scanning disabled - allow execution
		return false, false, ""
	}

	// Check if Tirith is available (graceful degradation)
	if !o.tirithScanner.IsAvailable(ctx) {
		o.logger.Warn("Tirith not available, allowing command execution without security scan",
			"command", truncateCommand(command),
		)
		o.logAuditEvent("tirith_unavailable", "warning", map[string]any{
			"command": truncateCommand(command),
		}, "tirith")
		return false, false, "tirith scanner not available"
	}

	// Scan the command
	result := o.tirithScanner.Scan(ctx, command)
	if result == nil {
		// Scan failed - allow execution (graceful degradation)
		o.logger.Warn("Tirith scan returned nil, allowing command execution",
			"command", truncateCommand(command),
		)
		o.logAuditEvent("scan_failed", "warning", map[string]any{
			"command": truncateCommand(command),
		}, "tirith")
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
		o.logAuditEvent("command_blocked", "critical", map[string]any{
			"command":  truncateCommand(command),
			"severity": ptrStringValue(result.Severity),
			"rule_id":  ptrStringValue(result.RuleID),
			"reason":   reason,
		}, "tirith")
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
		o.logAuditEvent("command_warning", "warning", map[string]any{
			"command":  truncateCommand(command),
			"severity": ptrStringValue(result.Severity),
			"rule_id":  ptrStringValue(result.RuleID),
			"reason":   reason,
		}, "tirith")
		return false, true, reason
	}

	// Command was clean - log at DEBUG level for audit trail
	o.logger.Debug("Shell command scanned and allowed",
		"command", truncateCommand(command),
	)
	return false, false, ""
}

// CheckWebFetch checks a URL for taint policy violations (e.g., secret exfiltration).
// Returns blocked=true and a reason if the URL should be denied.
func (o *Orchestrator) CheckWebFetch(url string) (blocked bool, reason string) {
	if o.taintTracker == nil {
		return false, ""
	}
	if violation := o.taintTracker.CheckWebFetch(url); violation != nil {
		o.logger.Warn("Web fetch blocked by taint tracking",
			"url", url,
			"violation", violation.Error(),
		)
		o.logAuditEvent("taint_web_blocked", "critical", map[string]any{
			"url":       url,
			"violation": violation.Error(),
		}, "taint")
		return true, violation.Error()
	}
	return false, ""
}

// RecordToolTaint stores a tainted tool result value in the taint tracker so
// that subsequent policy checks (e.g., shell_exec sink) can detect tainted
// data flowing through the agent loop. The call is a no-op when taint
// tracking is disabled (no tracker configured) or when the label is empty.
func (o *Orchestrator) RecordToolTaint(toolCallID, toolName string, value string, label taint.TaintLabel) {
	if o.taintTracker == nil || label == taint.TaintNone {
		return
	}
	source := toolName
	if toolCallID != "" {
		source = toolName + ":" + toolCallID
	}
	tv := taint.NewTaintedValue(value, []taint.TaintLabel{label}, source)
	o.taintTracker.Store(toolCallID, tv)
	o.logger.Debug("recorded tool taint",
		"tool", toolName,
		"tool_call_id", toolCallID,
		"label", label.String(),
	)
}

// RecordUserInput records direct user input as carrying TaintUserInput so that
// downstream policy checks (e.g., shell_exec sink) can distinguish user-originated
// data from trusted system messages. The call is a no-op when taint tracking is
// disabled (no tracker configured).
func (o *Orchestrator) RecordUserInput(conversationID, input string) {
	if o.taintTracker == nil {
		return
	}
	key := fmt.Sprintf("user_input:%s", conversationID)
	tv := taint.NewTaintedValue(input, []taint.TaintLabel{taint.TaintUserInput}, "user_input")
	o.taintTracker.Store(key, tv)
	o.logger.Debug("recorded user input taint",
		"conversation", conversationID,
		"label", taint.TaintUserInput.String(),
	)
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

// WrapSkillOutput wraps output from a skill execution in boundary markers.
// Skill results use the same boundary marker scheme as tool outputs so that
// existing injection-detection (IsWithinBoundary, safety reminders) covers
// them automatically. The skill name is prefixed with "skill:" to distinguish
// skill-sourced output from tool-sourced output in logs and boundary scans.
func (o *Orchestrator) WrapSkillOutput(skillName, output string) string {
	if o.promptGuard == nil {
		return output
	}
	return o.promptGuard.WrapSkillOutput(skillName, output)
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

// InputSanitizer returns the input sanitizer component.
// Returns nil if input sanitization is not enabled.
func (o *Orchestrator) InputSanitizer() *InputSanitizer {
	return o.sanitizer
}

// IsEnabled returns whether the orchestrator has any security features enabled.
func (o *Orchestrator) IsEnabled() bool {
	return o.config.SanitizeInputs || o.config.MonitorOutput || o.config.ScanShellCommands
}

// Close cleans up resources held by the orchestrator (audit DB connection).
func (o *Orchestrator) Close() {
	if o.auditDB != nil {
		if err := o.auditDB.Close(); err != nil {
			o.logger.Warn("Failed to close audit database", "error", err)
		}
	}
}

// AuditEvent represents a single row in the orchestrator audit log.
type AuditEvent struct {
	ID        int64           `json:"id"`
	Timestamp time.Time       `json:"timestamp"`
	EventType string          `json:"event_type"` // e.g. "input_sanitized", "input_blocked", "output_scan", "command_blocked", "command_warning"
	Severity  string          `json:"severity"`   // e.g. "info", "warning", "critical"
	Details   json.RawMessage `json:"details"`    // event-specific details as JSON
	Source    string          `json:"source"`     // e.g. "sanitizer", "output_monitor", "tirith"
}

// initAuditDB opens (or creates) the SQLite audit database and ensures the
// audit_log table exists.
func (o *Orchestrator) initAuditDB(dbPath string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create audit db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("open audit db: %w", err)
	}

	// Create the audit_log table if it does not exist.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_log (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp  TEXT    NOT NULL DEFAULT (datetime('now')),
			event_type TEXT    NOT NULL,
			severity   TEXT    NOT NULL DEFAULT 'info',
			details    TEXT    NOT NULL DEFAULT '{}',
			source     TEXT    NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_audit_log_event_type ON audit_log(event_type);
		CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp);
	`)
	if err != nil {
		db.Close()
		return fmt.Errorf("create audit_log table: %w", err)
	}

	o.auditDB = db
	return nil
}

// AuditDB returns the underlying audit database handle, or nil if audit
// logging is disabled. The caller should not close the returned DB.
func (o *Orchestrator) AuditDB() *sql.DB {
	return o.auditDB
}

// logAuditEvent inserts an audit event row. It is safe to call when audit
// logging is disabled (no-op) and silently drops errors so that audit
// failures never block security operations.
func (o *Orchestrator) logAuditEvent(eventType, severity string, details any, source string) {
	if o.auditDB == nil {
		return
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		detailsJSON = fmt.Appendf(nil, `{"marshal_error": %q}`, err)
	}

	_, err = o.auditDB.Exec(
		`INSERT INTO audit_log (timestamp, event_type, severity, details, source) VALUES (?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano),
		eventType,
		severity,
		string(detailsJSON),
		source,
	)
	if err != nil {
		o.logger.Warn("Failed to write audit event",
			"event_type", eventType,
			"error", err,
		)
	}
}

// isCriticalThreat determines if a threat type should cause input blocking.
func isCriticalThreat(threatType string) bool {
	// These threat types indicate active prompt injection attempts
	criticalTypes := map[string]bool{
		"instruction_override":    true,
		"role_switch_attempt":     true,
		"instruction_injection":   true,
		"role_marker_system":      true,
		"role_marker_assistant":   true,
		"markdown_role_injection": true,
		"special_token_chatml":    true,
		"special_token_llama":     true,
		"special_token_llama_sys": true,
		"special_token_phi":       true,
		"special_token_eos":       true,
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
