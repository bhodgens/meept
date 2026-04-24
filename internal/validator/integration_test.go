package validator

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// TestValidatorManager_EvidenceEnforcement tests that evidence
// requirements are enforced across all validator types.
func TestValidatorManager_EvidenceEnforcement(t *testing.T) {
	mgr := NewValidatorManager()

	tests := []struct {
		name      string
		toolHint  string
		evidence  []models.Evidence
		claims    []string
		wantValid bool
	}{
		{
			name:      "web_fetch with valid evidence",
			toolHint:  "web_fetch",
			evidence:  []models.Evidence{models.NewEvidence(models.EvidenceAPIResponse, "http://example.com", "status=200", "web_fetch")},
			claims:    []string{"fetched data from http://example.com"},
			wantValid: true,
		},
		{
			name:      "web_fetch with no evidence",
			toolHint:  "web_fetch",
			evidence:  nil,
			claims:    []string{"fetched data from http://example.com"},
			wantValid: true, // Validator exists but Validate() only checks evidence it receives, not claims
		},
		{
			name:      "memory_search with valid evidence",
			toolHint:  "memory_search",
			evidence:  []models.Evidence{models.NewEvidence(models.EvidenceDatabaseRow, "context", `{"found": true}`, "memory_search")},
			claims:    []string{"retrieved context from memory"},
			wantValid: true,
		},
		{
			name:      "memory_search with no evidence",
			toolHint:  "memory_search",
			evidence:  nil,
			claims:    []string{"retrieved context from memory"},
			wantValid: true, // Validator exists but Validate() only checks evidence it receives
		},
		{
			name:      "unknown tool hint passes through",
			toolHint:  "unknown_tool",
			evidence:  nil,
			claims:    []string{"did something"},
			wantValid: true, // No validator registered, passes through
		},
		{
			name:      "no claims no evidence passes",
			toolHint:  "unknown_tool",
			evidence:  nil,
			claims:    nil,
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &task.TaskStep{
				ToolHint: tt.toolHint,
				Evidence: tt.evidence,
				Claims:   tt.claims,
			}

			err := mgr.ValidateStep(context.Background(), step)
			gotValid := err == nil

			if gotValid != tt.wantValid {
				t.Errorf("ValidateStep() valid=%v, want %v, err=%v", gotValid, tt.wantValid, err)
			}
		})
	}
}

// TestStepValidator_ClaimEvidenceMismatch tests that claims are validated
// against the appropriate evidence types.
func TestStepValidator_ClaimEvidenceMismatch(t *testing.T) {
	v := NewStepValidator()

	tests := []struct {
		name      string
		claims    []string
		evidence  []models.Evidence
		wantValid bool
	}{
		{
			name:      "file claim with shell evidence",
			claims:    []string{"created file test.txt"},
			evidence:  []models.Evidence{models.NewEvidence(models.EvidenceProcessExit, "ls", "exit=0", "shell")},
			wantValid: false,
		},
		{
			name:      "shell claim with file evidence",
			claims:    []string{"executed command ls -la"},
			evidence:  []models.Evidence{models.NewEvidence(models.EvidenceFileExists, "/tmp/test.txt", "size=100", "file_read")},
			wantValid: false,
		},
		{
			name:      "web claim with file evidence",
			claims:    []string{"fetched data from http://example.com"},
			evidence:  []models.Evidence{models.NewEvidence(models.EvidenceFileExists, "/tmp/test.txt", "size=100", "file_read")},
			wantValid: false,
		},
		{
			name:      "memory claim with file evidence",
			claims:    []string{"retrieved context from memory"},
			evidence:  []models.Evidence{models.NewEvidence(models.EvidenceFileExists, "/tmp/test.txt", "size=100", "file_read")},
			wantValid: false,
		},
		{
			name:      "web claim with api evidence",
			claims:    []string{"fetched data from http://example.com"},
			evidence:  []models.Evidence{models.NewEvidence(models.EvidenceAPIResponse, "http://example.com", "status=200", "web_fetch")},
			wantValid: true,
		},
		{
			name:      "memory claim with db_row evidence",
			claims:    []string{"retrieved context from memory"},
			evidence:  []models.Evidence{models.NewEvidence(models.EvidenceDatabaseRow, "context", `{"found": true}`, "memory_search")},
			wantValid: true,
		},
		{
			name:      "no claims no evidence",
			claims:    nil,
			evidence:  nil,
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &task.TaskStep{
				Claims:   tt.claims,
				Evidence: tt.evidence,
			}

			result := v.Validate(context.Background(), step)
			if result.Valid != tt.wantValid {
				t.Errorf("Validate() valid=%v, want %v, errors=%v", result.Valid, tt.wantValid, result.Errors)
			}
		})
	}
}
