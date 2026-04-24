package validator

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

func TestMemoryValidator_ValidateEvidence_DBRow_Success(t *testing.T) {
	m := NewMemoryValidator()
	ev := models.NewEvidence(
		models.EvidenceDatabaseRow,
		"session_context",
		`{"rows_affected": 1, "found": true}`,
		"memory_search",
	)

	result := m.ValidateEvidence(context.Background(), ev)
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}

func TestMemoryValidator_ValidateEvidence_DBRow_ValidJSON(t *testing.T) {
	m := NewMemoryValidator()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"valid rows affected", `{"rows_affected": 1}`, true},
		{"valid found flag", `{"found": true}`, true},
		{"empty object", `{}`, true},
		{"multiple fields", `{"rows_affected": 5, "found": true, "id": 123}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := models.NewEvidence(
				models.EvidenceDatabaseRow,
				"test_query",
				tt.value,
				"memory_search",
			)
			result := m.ValidateEvidence(context.Background(), ev)
			if result.Valid != tt.want {
				t.Errorf("value %s: valid=%v, want %v, errors=%v", tt.value, result.Valid, tt.want, result.Errors)
			}
		})
	}
}

func TestMemoryValidator_ValidateEvidence_DBRow_InvalidJSON(t *testing.T) {
	m := NewMemoryValidator()
	ev := models.NewEvidence(
		models.EvidenceDatabaseRow,
		"session_context",
		"not-json",
		"memory_search",
	)

	result := m.ValidateEvidence(context.Background(), ev)
	// Should handle gracefully - JSON parse fails but fallback accepts non-empty string
	// This documents current behavior - non-empty string values pass
	if !result.Valid {
		t.Errorf("expected valid for non-empty string (fallback), got errors: %v", result.Errors)
	}
}

func TestMemoryValidator_ValidateEvidence_DBRow_EmptyValue(t *testing.T) {
	m := NewMemoryValidator()
	ev := models.NewEvidence(
		models.EvidenceDatabaseRow,
		"session_context",
		"",
		"memory_search",
	)

	result := m.ValidateEvidence(context.Background(), ev)
	if result.Valid {
		t.Error("expected invalid for empty value")
	}
}

func TestMemoryValidator_ValidateEvidence_WrongType(t *testing.T) {
	m := NewMemoryValidator()
	ev := models.NewEvidence(
		models.EvidenceFileExists,
		"/tmp/test.txt",
		"size=100",
		"file_read",
	)

	result := m.ValidateEvidence(context.Background(), ev)
	if result.Valid {
		t.Error("expected invalid for wrong evidence type")
	}
	if len(result.Errors) == 0 {
		t.Error("expected error message for wrong type")
	}
}

func TestMemoryValidator_Validate_StepIntegration(t *testing.T) {
	m := NewMemoryValidator()

	step := &task.TaskStep{
		ToolHint: "memory_search",
		Evidence: []models.Evidence{
			models.NewEvidence(models.EvidenceDatabaseRow, "context", `{"found": true}`, "memory_search"),
		},
	}

	result := m.Validate(context.Background(), step)
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}
