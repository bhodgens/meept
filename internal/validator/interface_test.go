package validator

import (
	"context"
	"testing"

	"github.com/caimlas/meept/pkg/models"
)

func TestStepValidator_ValidateEvidence_UnknownType(t *testing.T) {
	v := NewStepValidator()
	ev := models.Evidence{
		Type:    "unknown_type",
		Subject: "test",
		Value:   "test",
	}

	result := v.validateEvidence(context.Background(), ev)
	if result.Valid {
		t.Error("expected invalid for unknown evidence type")
	}
	if len(result.Errors) == 0 {
		t.Error("expected error message for unknown type")
	}
}

func TestStepValidator_ValidateEvidence_APIResponse(t *testing.T) {
	v := NewStepValidator()
	ev := models.NewEvidence(
		models.EvidenceAPIResponse,
		"https://api.example.com",
		"status=200",
		"web_fetch",
	)

	result := v.validateEvidence(context.Background(), ev)
	if !result.Valid {
		t.Errorf("expected valid for api_response, got errors: %v", result.Errors)
	}
}

func TestStepValidator_ValidateEvidence_DatabaseRow(t *testing.T) {
	v := NewStepValidator()
	ev := models.NewEvidence(
		models.EvidenceDatabaseRow,
		"context",
		`{"found": true}`,
		"memory_search",
	)

	result := v.validateEvidence(context.Background(), ev)
	if !result.Valid {
		t.Errorf("expected valid for db_row, got errors: %v", result.Errors)
	}
}

func TestStepValidator_ValidateEvidence_ShellOutput(t *testing.T) {
	v := NewStepValidator()
	ev := models.NewEvidence(
		models.EvidenceShellOutput,
		"ls -la",
		"total 48",
		"shell",
	)

	result := v.validateEvidence(context.Background(), ev)
	// Shell output validation passes with any non-empty output
	if !result.Valid {
		t.Errorf("expected valid for shell_output, got errors: %v", result.Errors)
	}
}
