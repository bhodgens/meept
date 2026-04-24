package validator

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// WebValidator validates web-related evidence (API responses, HTTP status codes).
type WebValidator struct{}

// NewWebValidator creates a new WebValidator.
func NewWebValidator() *WebValidator {
	return &WebValidator{}
}

// Validate checks web evidence against expected outcomes.
func (v *WebValidator) Validate(ctx context.Context, step *task.TaskStep) ValidationResult {
	var result ValidationResult

	for _, ev := range step.Evidence {
		switch ev.Type {
		case models.EvidenceAPIResponse:
			// Validate HTTP status and response structure
			if err := v.validateAPIResponse(ev.Subject, ev.Value); err != nil {
				result.Errors = append(result.Errors, err.Error())
			}
		default:
			// Not a web evidence type, skip
			continue
		}
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// ValidateEvidence validates a single piece of web evidence.
func (v *WebValidator) ValidateEvidence(ctx context.Context, ev models.Evidence) ValidationResult {
	var result ValidationResult

	switch ev.Type {
	case models.EvidenceAPIResponse:
		if err := v.validateAPIResponse(ev.Subject, ev.Value); err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
	default:
		// Not a web evidence type - fail validation
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("unexpected evidence type for web validator: %s", ev.Type))
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// validateAPIResponse checks the API response evidence.
// Subject: URL that was fetched
// Value: "status=CODE,size=BYTES" or similar metadata
func (v *WebValidator) validateAPIResponse(url, value string) error {
	// Expected format: "status=200,size=1234" or "status=200"
	if !strings.Contains(value, "status=") {
		return fmt.Errorf("invalid API response format: %s", value)
	}

	// Parse status code
	status := parseStatusValue(value)
	if status < 200 || status >= 400 {
		return fmt.Errorf("HTTP request failed with status %d for URL %s", status, url)
	}

	return nil
}

// parseStatusValue parses an HTTP status code from evidence.
func parseStatusValue(value string) int {
	// Expected format: "status=200,size=1234"
	parts := strings.Split(value, ",")
	for _, part := range parts {
		kv := strings.Split(part, "=")
		if len(kv) == 2 && kv[0] == "status" {
			// Parse status code
			var status int
			for _, c := range kv[1] {
				if c >= '0' && c <= '9' {
					status = status*10 + int(c-'0')
				}
			}
			return status
		}
	}
	return 0
}
