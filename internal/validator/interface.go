// Package validator provides validation for tool execution evidence.
// It validates that tool side-effects occurred as claimed by verifying
// evidence against ground truth (filesystem, API responses, etc.)
package validator

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
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
type Validator interface {
	// Validate checks evidence against ground truth.
	// Returns a ValidationResult indicating success or failure.
	Validate(ctx context.Context, step *task.TaskStep) ValidationResult
}

// StepValidator validates a step's evidence against its claims.
// It orchestrates multiple validators (filesystem, shell, etc.) based on evidence type.
type StepValidator struct {
	fsValidator     *FilesystemValidator
	shellValidator  *ShellValidator
	webValidator    *WebValidator
	memoryValidator *MemoryValidator
}

// NewStepValidator creates a new StepValidator with default sub-validators.
func NewStepValidator() *StepValidator {
	return &StepValidator{
		fsValidator:     NewFilesystemValidator(),
		shellValidator:  NewShellValidator(),
		webValidator:    NewWebValidator(),
		memoryValidator: NewMemoryValidator(),
	}
}

// Validate validates a step's evidence against its claims.
func (v *StepValidator) Validate(ctx context.Context, step *task.TaskStep) ValidationResult {
	var result ValidationResult

	// Validate each piece of evidence with the appropriate validator
	for _, ev := range step.Evidence {
		evResult := v.validateEvidence(ctx, ev)
		if !evResult.Valid {
			result.Errors = append(result.Errors, evResult.Errors...)
		}
		result.Warnings = append(result.Warnings, evResult.Warnings...)
	}

	// Validate claims against evidence
	for _, claim := range step.Claims {
		claimResult := v.validateClaim(ctx, claim, step.Evidence)
		if !claimResult.Valid {
			result.Errors = append(result.Errors, claimResult.Errors...)
		}
	}

	// If no evidence but claims exist, that's an error
	if len(step.Claims) > 0 && len(step.Evidence) == 0 {
		result.Errors = append(result.Errors, "claims made without supporting evidence")
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// validateEvidence validates a single piece of evidence.
func (v *StepValidator) validateEvidence(ctx context.Context, ev models.Evidence) ValidationResult {
	// Route to appropriate validator based on evidence type
	switch ev.Type {
	case models.EvidenceFileExists, models.EvidenceFileHash:
		return v.fsValidator.ValidateEvidence(ctx, ev)
	case models.EvidenceProcessExit, models.EvidenceShellOutput:
		return v.shellValidator.ValidateEvidence(ctx, ev)
	case models.EvidenceAPIResponse:
		return v.webValidator.ValidateEvidence(ctx, ev)
	case models.EvidenceDatabaseRow:
		return v.memoryValidator.ValidateEvidence(ctx, ev)
	default:
		// Unknown evidence type - fail validation
		return ValidationResult{
			Valid:  false,
			Errors: []string{fmt.Sprintf("unknown evidence type: %s", ev.Type)},
		}
	}
}

// validateClaim checks if a claim is supported by evidence.
func (v *StepValidator) validateClaim(ctx context.Context, claim string, evidence []models.Evidence) ValidationResult {
	// Basic check: ensure evidence exists for the claim
	if len(evidence) == 0 {
		return ValidationResult{
			Valid:  false,
			Errors: []string{"no evidence to support claim: " + claim},
		}
	}

	lowerClaim := strings.ToLower(claim)

	// File creation/modification claims require file_exists or file_hash evidence
	if strings.Contains(lowerClaim, "created") || strings.Contains(lowerClaim, "wrote") ||
		strings.Contains(lowerClaim, "modified") || strings.Contains(lowerClaim, "updated") {
		hasFileEvidence := false
		for _, ev := range evidence {
			if ev.Type == models.EvidenceFileExists || ev.Type == models.EvidenceFileHash {
				hasFileEvidence = true
				break
			}
		}
		if !hasFileEvidence {
			return ValidationResult{
				Valid:  false,
				Errors: []string{"file operation claim without file_exists/file_hash evidence"},
			}
		}
	}

	// Shell command claims require process_exit evidence
	if strings.Contains(lowerClaim, "executed") || strings.Contains(lowerClaim, "ran") ||
		strings.Contains(lowerClaim, "command") || strings.Contains(lowerClaim, "shell") {
		hasExitEvidence := false
		for _, ev := range evidence {
			if ev.Type == models.EvidenceProcessExit {
				hasExitEvidence = true
				break
			}
		}
		if !hasExitEvidence {
			return ValidationResult{
				Valid:  false,
				Errors: []string{"shell command claim without process_exit evidence"},
			}
		}
	}

	// Web/API claims require api_response evidence
	if strings.Contains(lowerClaim, "fetch") || strings.Contains(lowerClaim, "api") ||
		strings.Contains(lowerClaim, "http") || strings.Contains(lowerClaim, "web") {
		hasWebEvidence := false
		for _, ev := range evidence {
			if ev.Type == models.EvidenceAPIResponse {
				hasWebEvidence = true
				break
			}
		}
		if !hasWebEvidence {
			return ValidationResult{
				Valid:  false,
				Errors: []string{"web/api claim without api_response evidence"},
			}
		}
	}

	// Memory claims require database_row evidence
	if strings.Contains(lowerClaim, "memory") || strings.Contains(lowerClaim, "stored") ||
		strings.Contains(lowerClaim, "retrieved") || strings.Contains(lowerClaim, "context") {
		hasMemoryEvidence := false
		for _, ev := range evidence {
			if ev.Type == models.EvidenceDatabaseRow {
				hasMemoryEvidence = true
				break
			}
		}
		if !hasMemoryEvidence {
			return ValidationResult{
				Valid:  false,
				Errors: []string{"memory operation claim without db_row evidence"},
			}
		}
	}

	return ValidationResult{Valid: true}
}

// validateClaimSimple checks if a simple file operation claim is supported.
func validateClaimSimple(claim string, evidence []models.Evidence) bool {
	// Simple heuristic: if claim mentions "created" or "wrote" and we have file evidence, pass
	if strings.Contains(strings.ToLower(claim), "created") ||
		strings.Contains(strings.ToLower(claim), "wrote") {
		for _, ev := range evidence {
			if ev.Type == models.EvidenceFileExists {
				return true
			}
		}
	}
	return len(evidence) > 0
}
