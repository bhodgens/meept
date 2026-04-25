package q

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/memory/memvid"
)

// ReviewerValidator validates Q Agent recommendations before presentation.
type ReviewerValidator struct {
	logger       *slog.Logger
	memvidClient *memvid.Client
}

// NewReviewerValidator creates a new reviewer validator.
func NewReviewerValidator(logger *slog.Logger, memvidClient *memvid.Client) *ReviewerValidator {
	return &ReviewerValidator{
		logger:       logger,
		memvidClient: memvidClient,
	}
}

// ValidationResult represents the result of recommendation validation.
type ValidationResult struct {
	Status      agent.ReviewStatus `json:"status"`
	Feedback    string             `json:"feedback"`
	Issues      []string           `json:"issues,omitempty"`
	Confidence  float64            `json:"confidence"`
	ValidatedAt time.Time          `json:"validated_at"`
}

// ValidateRecommendation validates a recommendation before presentation to the user.
func (v *ReviewerValidator) ValidateRecommendation(ctx context.Context, rec Recommendation, report *ResearchReport) (*ValidationResult, error) {
	result := &ValidationResult{
		Status:      agent.ReviewApproved,
		Feedback:    "Recommendation approved",
		Confidence:  0.9,
		ValidatedAt: time.Now(),
		Issues:      make([]string, 0),
	}

	// Check evidence validity
	if len(report.EvidenceChain) == 0 {
		result.Issues = append(result.Issues, "No evidence chain provided")
		result.Status = agent.ReviewNeedsInfo
		result.Confidence = 0.3
	}

	// Check recommendation feasibility
	if rec.Implementation.FilesToCreate == nil && rec.Implementation.Commands == nil && rec.Implementation.AgentSpec == nil && rec.Implementation.SkillSpec == nil {
		result.Issues = append(result.Issues, "No implementation details provided")
		result.Status = agent.ReviewNeedsInfo
		result.Confidence = 0.4
	}

	// Check for file conflicts (simplified - just check if files exist)
	for _, file := range rec.Implementation.FilesToCreate {
		if file.Path != "" {
			// In a real implementation, check if file exists
			// For now, just validate path format
			if len(file.Path) > 255 {
				result.Issues = append(result.Issues, fmt.Sprintf("File path too long: %s", file.Path))
			}
		}
	}

	// Validate confidence score from pattern
	if report.ConfidenceScore < 0.5 {
		result.Issues = append(result.Issues, fmt.Sprintf("Low confidence score: %.2f", report.ConfidenceScore))
		result.Confidence = report.ConfidenceScore
		if report.ConfidenceScore < 0.3 {
			result.Status = agent.ReviewRejected
			result.Feedback = "Recommendation confidence too low"
		}
	}

	// Build feedback
	if len(result.Issues) > 0 {
		result.Feedback = fmt.Sprintf("Issues found: %s", joinStrings(result.Issues, ", "))
	}

	return result, nil
}

// ValidateRecommendations validates multiple recommendations.
func (v *ReviewerValidator) ValidateRecommendations(ctx context.Context, recs []Recommendation, reports []*ResearchReport) ([]*ValidationResult, error) {
	results := make([]*ValidationResult, 0, len(recs))

	for i, rec := range recs {
		report := reports[i]
		if report == nil {
			results = append(results, &ValidationResult{
				Status:   agent.ReviewRejected,
				Feedback: "No research report available",
			})
			continue
		}

		result, err := v.ValidateRecommendation(ctx, rec, report)
		if err != nil {
			v.logger.Warn("validation error", "recommendation", rec.Title, "error", err)
			results = append(results, &ValidationResult{
				Status:   agent.ReviewRejected,
				Feedback: fmt.Sprintf("Validation error: %v", err),
			})
			continue
		}

		results = append(results, result)
	}

	return results, nil
}

// LogValidationResult logs validation outcomes for tracking.
func (v *ReviewerValidator) LogValidationResult(ctx context.Context, rec Recommendation, result *ValidationResult) error {
	if v.memvidClient == nil {
		return nil
	}

	metadata := map[string]any{
		"recommendation_id": rec.Title,
		"recommendation_type": rec.Type,
		"validation_status": string(result.Status),
		"confidence": result.Confidence,
		"feedback": result.Feedback,
		"issues": result.Issues,
		"validated_at": result.ValidatedAt.Format(time.RFC3339),
	}

	content, _ := json.Marshal(map[string]any{
		"recommendation": rec,
		"validation_result": result,
	})

	_, err := v.memvidClient.StoreWithZone(ctx, string(content), "q_validations", metadata)
	return err
}

// joinStrings joins strings with a separator.
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}
