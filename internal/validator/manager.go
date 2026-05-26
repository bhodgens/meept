package validator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/task"
)

// ValidatorManager orchestrates validators for different tool types.
//
//nolint:revive // stutter with package name is intentional for API clarity
type ValidatorManager struct {
	validators map[string]Validator // tool_hint -> validator
	logger     *slog.Logger
}

// NewValidatorManager creates a new ValidatorManager with default validators.
func NewValidatorManager() *ValidatorManager {
	return &ValidatorManager{
		validators: map[string]Validator{
			"code":               NewFilesystemValidator(),
			"refactor":           NewFilesystemValidator(),
			"file_write":         NewFilesystemValidator(),
			"file_read":          NewFilesystemValidator(),
			"shell":              NewShellValidator(),
			"list_dir":           NewFilesystemValidator(),
			"file_delete":        NewFilesystemValidator(),
			"web_fetch":          NewWebValidator(),
			"web_search":         NewWebValidator(),
			"memory_search":      NewMemoryValidator(),
			"memory_get_context": NewMemoryValidator(),
			"memory_store":       NewMemoryValidator(),
			"memory_delete":      NewMemoryValidator(),
		},
		logger: slog.Default(),
	}
}

// NewValidatorManagerWithLogger creates a new ValidatorManager with a custom logger.
func NewValidatorManagerWithLogger(logger *slog.Logger) *ValidatorManager {
	mgr := NewValidatorManager()
	if logger != nil {
		mgr.logger = logger
	}
	return mgr
}

// RegisterValidator registers a validator for a specific tool hint.
func (m *ValidatorManager) RegisterValidator(toolHint string, validator Validator) {
	m.validators[toolHint] = validator
}

// ValidateStep validates a step using the appropriate validator.
// Returns nil if validation passes or no validator exists for the tool hint.
// Returns an error if validation fails.
func (m *ValidatorManager) ValidateStep(ctx context.Context, step *task.TaskStep) error {
	if step == nil {
		return nil
	}

	validator, ok := m.validators[step.ToolHint]
	if !ok {
		m.logger.Debug("No validator for tool hint", "hint", step.ToolHint)
		return nil // No validator registered, pass through
	}

	result := validator.Validate(ctx, step)
	if !result.Valid {
		m.logger.Error("Validation failed", "step_id", step.ID, "errors", result.Errors)
		return fmt.Errorf("validation failed: %s", strings.Join(result.Errors, ", "))
	}

	// Log warnings if any
	if len(result.Warnings) > 0 {
		m.logger.Warn("Validation warnings", "step_id", step.ID, "warnings", result.Warnings)
	}

	return nil
}

// HasValidator returns true if a validator is registered for the given tool hint.
func (m *ValidatorManager) HasValidator(toolHint string) bool {
	_, ok := m.validators[toolHint]
	return ok
}
