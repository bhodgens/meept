// Package validator provides validation for tool execution evidence.
// It validates that tool side-effects occurred as claimed by verifying
// evidence against ground truth (filesystem, API responses, etc.)
package validator

import (
	"context"

	"github.com/caimlas/meept/internal/task"
)

// ValidationResult contains the outcome of a validation check.
type ValidationResult struct {
	// Valid indicates whether validation passed.
	Valid bool `json:"valid"`
	// Errors contains validation error messages.
	Errors []string `json:"errors,omitempty"`
	// Warnings contains non-blocking warning messages.
	Warnings []string `json:"warnings,omitempty"`
}

// Validator is the interface for validating tool execution evidence.
//
//nolint:revive // stutter with package name is intentional for API clarity
type Validator interface {
	// Validate checks evidence against ground truth.
	// Returns a ValidationResult indicating success or failure.
	Validate(ctx context.Context, step *task.TaskStep) ValidationResult
}
