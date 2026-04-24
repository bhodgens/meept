package validator

import (
	"context"
	"strconv"
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

func TestWebValidator_ValidateEvidence_APIResponse_Success(t *testing.T) {
	v := NewWebValidator()
	ev := models.NewEvidence(
		models.EvidenceAPIResponse,
		"https://api.example.com/data",
		"status=200,size=1234",
		"web_fetch",
	)

	result := v.ValidateEvidence(context.Background(), ev)
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}

func TestWebValidator_ValidateEvidence_APIResponse_SuccessCodes(t *testing.T) {
	v := NewWebValidator()

	tests := []struct {
		name   string
		status int
		want   bool
	}{
		{"200 OK", 200, true},
		{"201 Created", 201, true},
		{"204 No Content", 204, true},
		{"301 Moved", 301, true},
		{"302 Found", 302, true},
		{"400 Bad Request", 400, false},
		{"404 Not Found", 404, false},
		{"500 Internal", 500, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := models.NewEvidence(
				models.EvidenceAPIResponse,
				"https://api.example.com/data",
				"status="+strconv.Itoa(tt.status),
				"web_fetch",
			)
			result := v.ValidateEvidence(context.Background(), ev)
			if result.Valid != tt.want {
				t.Errorf("status %d: valid=%v, want %v, errors=%v", tt.status, result.Valid, tt.want, result.Errors)
			}
		})
	}
}

func TestWebValidator_ValidateEvidence_APIResponse_Failure(t *testing.T) {
	v := NewWebValidator()
	ev := models.NewEvidence(
		models.EvidenceAPIResponse,
		"https://api.example.com/data",
		"status=404,size=0",
		"web_fetch",
	)

	result := v.ValidateEvidence(context.Background(), ev)
	if result.Valid {
		t.Error("expected invalid for 404 status")
	}
}

func TestWebValidator_ValidateEvidence_APIResponse_InvalidFormat(t *testing.T) {
	v := NewWebValidator()
	ev := models.NewEvidence(
		models.EvidenceAPIResponse,
		"https://api.example.com/data",
		"invalid-format",
		"web_fetch",
	)

	result := v.ValidateEvidence(context.Background(), ev)
	if result.Valid {
		t.Error("expected invalid for malformed value")
	}
}

func TestWebValidator_ValidateEvidence_WrongType(t *testing.T) {
	v := NewWebValidator()
	ev := models.NewEvidence(
		models.EvidenceFileExists,
		"/tmp/test.txt",
		"size=100",
		"file_read",
	)

	result := v.ValidateEvidence(context.Background(), ev)
	if result.Valid {
		t.Error("expected invalid for wrong evidence type")
	}
	if len(result.Errors) == 0 {
		t.Error("expected error message for wrong type")
	}
}

func TestWebValidator_Validate_StepIntegration(t *testing.T) {
	v := NewWebValidator()

	step := &task.TaskStep{
		ToolHint: "web_fetch",
		Evidence: []models.Evidence{
			models.NewEvidence(models.EvidenceAPIResponse, "http://example.com", "status=200", "web_fetch"),
		},
	}

	result := v.Validate(context.Background(), step)
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}
