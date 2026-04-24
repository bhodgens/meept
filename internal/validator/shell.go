package validator

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// ShellValidator validates shell command evidence.
type ShellValidator struct{}

// NewShellValidator creates a new ShellValidator.
func NewShellValidator() *ShellValidator {
	return &ShellValidator{}
}

// Validate checks shell evidence against expected outcomes.
func (v *ShellValidator) Validate(ctx context.Context, step *task.TaskStep) ValidationResult {
	var result ValidationResult

	for _, ev := range step.Evidence {
		switch ev.Type {
		case models.EvidenceProcessExit:
			// Exit code should be "0" for success
			if ev.Value != "0" {
				result.Errors = append(result.Errors,
					fmt.Sprintf("process exited with non-zero code: %s", ev.Value))
			}
		case models.EvidenceShellOutput:
			// Verify output contains expected patterns if subject is provided
			// Subject contains expected pattern, Value contains hashed output
			// For now, we just verify the evidence exists
			if ev.Value == "" {
				result.Errors = append(result.Errors,
					"shell output evidence missing value")
			}
		default:
			// Not a shell evidence type, skip
			continue
		}
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// ValidateEvidence validates a single piece of shell evidence.
func (v *ShellValidator) ValidateEvidence(ctx context.Context, ev models.Evidence) ValidationResult {
	var result ValidationResult

	switch ev.Type {
	case models.EvidenceProcessExit:
		if ev.Value != "0" {
			result.Errors = append(result.Errors,
				fmt.Sprintf("process exited with non-zero code: %s", ev.Value))
		}
	case models.EvidenceShellOutput:
		if ev.Value == "" {
			result.Errors = append(result.Errors,
				"shell output evidence missing value")
		}
	default:
		// Not a shell evidence type, pass through
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// ValidateOutputPattern checks if shell output contains an expected pattern.
// This is a more advanced validation that can be used for specific patterns.
func (v *ShellValidator) ValidateOutputPattern(output, expectedPattern string) bool {
	return strings.Contains(output, expectedPattern)
}

// ValidateExitCode checks if an exit code indicates success.
func (v *ShellValidator) ValidateExitCode(exitCode string) bool {
	return exitCode == "0"
}
