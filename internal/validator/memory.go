package validator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// MemoryValidator validates memory-related evidence (search results, stored contexts).
type MemoryValidator struct{}

// NewMemoryValidator creates a new MemoryValidator.
func NewMemoryValidator() *MemoryValidator {
	return &MemoryValidator{}
}

// Validate checks memory evidence against expected outcomes.
func (v *MemoryValidator) Validate(ctx context.Context, step *task.TaskStep) ValidationResult {
	var result ValidationResult

	for _, ev := range step.Evidence {
		switch ev.Type {
		case models.EvidenceDatabaseRow:
			// Validate database row was found/modified
			if err := v.validateDatabaseRow(ev.Subject, ev.Value); err != nil {
				result.Errors = append(result.Errors, err.Error())
			}
		default:
			// Not a memory evidence type, skip
			continue
		}
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// ValidateEvidence validates a single piece of memory evidence.
func (v *MemoryValidator) ValidateEvidence(ctx context.Context, ev models.Evidence) ValidationResult {
	var result ValidationResult

	switch ev.Type {
	case models.EvidenceDatabaseRow:
		if err := v.validateDatabaseRow(ev.Subject, ev.Value); err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
	default:
		// Not a memory evidence type - fail validation
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("unexpected evidence type for memory validator: %s", ev.Type))
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// validateDatabaseRow checks the database row evidence.
// Subject: query or context ID
// Value: JSON metadata about the operation (e.g., "rows_affected=1", "found=true")
func (v *MemoryValidator) validateDatabaseRow(subject, value string) error {
	// Try to parse as JSON first
	var metadata map[string]any
	if err := json.Unmarshal([]byte(value), &metadata); err == nil {
		// Check for expected fields
		if rowsAffected, ok := metadata["rows_affected"].(float64); ok {
			if rowsAffected < 0 {
				return fmt.Errorf("invalid rows_affected: %f", rowsAffected)
			}
		}
		return nil
	}

	// Fallback: simple key=value format
	// Expected format: "rows_affected=1" or "found=true"
	if value == "" {
		return fmt.Errorf("empty database row evidence for: %s", subject)
	}

	return nil
}
