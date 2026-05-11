package q

import (
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
)

// ImpactEstimator quantifies expected improvement from recommendations.
type ImpactEstimator struct {
	logger *slog.Logger
	config ImpactEstimatorConfig
}

// ImpactEstimatorConfig holds configuration for the ImpactEstimator.
type ImpactEstimatorConfig struct {
	// WeeklySessionsEstimate is the estimated number of sessions per week
	WeeklySessionsEstimate int
	// AverageTokenCost is the average cost per 1000 tokens
	AverageTokenCost float64
	// LaborCostPerMinute is the implied labor cost per minute of agent time
	LaborCostPerMinute float64
}

// NewImpactEstimator creates a new ImpactEstimator.
func NewImpactEstimator(logger *slog.Logger, config ImpactEstimatorConfig) *ImpactEstimator {
	return &ImpactEstimator{
		logger: logger,
		config: config,
	}
}

// EstimateImpact calculates the expected impact of a recommendation.
func (e *ImpactEstimator) EstimateImpact(pattern PatternReport, research *ResearchReport, recommendation Recommendation) *ImpactEstimate {
	switch recommendation.Type {
	case "new_agent":
		return e.estimateNewAgentImpact(pattern, research, recommendation)
	case "update_spec":
		return e.estimateSpecUpdateImpact(pattern, research, recommendation)
	case "reassign_model":
		return e.estimateModelReassignmentImpact(pattern, research, recommendation)
	case "add_tool":
		return e.estimateToolAdditionImpact(pattern, research, recommendation)
	default:
		return e.estimateGenericImpact(pattern, recommendation)
	}
}

// estimateNewAgentImpact estimates impact of creating a new specialist agent.
func (e *ImpactEstimator) estimateNewAgentImpact(pattern PatternReport, research *ResearchReport, rec Recommendation) *ImpactEstimate {
	// Baseline: current time spent on these tasks
	avgDuration := e.averageDurationFromEvidence(pattern)
	weeklySessions := e.estimateWeeklySessions(pattern)

	// Current weekly time spent
	currentWeeklyTime := time.Duration(avgDuration) * time.Duration(weeklySessions)

	// Expected improvement: specialists are typically 40-60% faster
	improvementFactor := 0.5 // 50% speedup estimate
	expectedWeeklyTime := time.Duration(float64(currentWeeklyTime) * (1 - improvementFactor))
	timeSaved := currentWeeklyTime - expectedWeeklyTime

	// Convert to minutes for reporting
	timeSavedMinutes := timeSaved.Minutes()

	return &ImpactEstimate{
		RecommendationID:   rec.Title,
		MetricType:         "time_saved",
		BaselineValue:      currentWeeklyTime.Minutes(),
		ExpectedValue:      expectedWeeklyTime.Minutes(),
		ImprovementPercent: improvementFactor * 100,
		WeeklyImpact:       fmt.Sprintf("Save ~%.0f minutes/week (%.1f hours/month)", timeSavedMinutes, timeSavedMinutes/60),
		Confidence:         pattern.Confidence * 0.8, // Reduce confidence for projections
	}
}

// estimateSpecUpdateImpact estimates impact of updating agent specification.
func (e *ImpactEstimator) estimateSpecUpdateImpact(pattern PatternReport, research *ResearchReport, rec Recommendation) *ImpactEstimate {
	// Baseline: current rejection/revision rate
	currentRejectionRate := pattern.MetricObserved
	targetRejectionRate := 0.1 // Target 10% rejection rate

	weeklySessions := e.estimateWeeklySessions(pattern)
	avgRevisionCost := 5.0 // Assume 5 minutes per revision

	// Current weekly revision time
	currentRevisions := float64(weeklySessions) * currentRejectionRate
	currentWeeklyTime := currentRevisions * avgRevisionCost

	// Expected revisions after fix
	expectedRevisions := float64(weeklySessions) * targetRejectionRate
	expectedWeeklyTime := expectedRevisions * avgRevisionCost

	timeSaved := currentWeeklyTime - expectedWeeklyTime
	improvementPercent := ((currentRejectionRate - targetRejectionRate) / currentRejectionRate) * 100

	return &ImpactEstimate{
		RecommendationID:   rec.Title,
		MetricType:         "revision_reduction",
		BaselineValue:      currentRejectionRate * 100,
		ExpectedValue:      targetRejectionRate * 100,
		ImprovementPercent: math.Min(improvementPercent, 80), // Cap at 80%
		WeeklyImpact:       fmt.Sprintf("Save ~%.0f minutes/week by reducing revisions from %.0f to %.0f per week", timeSaved, currentRevisions, expectedRevisions),
		Confidence:         pattern.Confidence * 0.85,
	}
}

// estimateModelReassignmentImpact estimates impact of reassigning model.
func (e *ImpactEstimator) estimateModelReassignmentImpact(pattern PatternReport, research *ResearchReport, rec Recommendation) *ImpactEstimate {
	// Baseline: current duration variance
	durationVariance := pattern.MetricObserved
	targetVariance := 1.5 // Target 1.5x variance (down from current)

	weeklySessions := e.estimateWeeklySessions(pattern)
	avgDuration := e.averageDurationFromEvidence(pattern)

	// Current weekly time (accounting for variance overhead)
	varianceOverhead := (durationVariance - 1.0) * 0.2 // 20% overhead per unit variance
	currentWeeklyTime := float64(weeklySessions) * avgDuration * (1 + varianceOverhead)

	// Expected time after reassignment
	expectedVarianceOverhead := (targetVariance - 1.0) * 0.2
	expectedWeeklyTime := float64(weeklySessions) * avgDuration * (1 + expectedVarianceOverhead)

	timeSaved := currentWeeklyTime - expectedWeeklyTime
	improvementPercent := (timeSaved / currentWeeklyTime) * 100

	return &ImpactEstimate{
		RecommendationID:   rec.Title,
		MetricType:         "duration_variance_reduction",
		BaselineValue:      durationVariance,
		ExpectedValue:      targetVariance,
		ImprovementPercent: math.Min(improvementPercent, 50),
		WeeklyImpact:       fmt.Sprintf("Save ~%.0f minutes/week through more consistent model performance", timeSaved/60),
		Confidence:         pattern.Confidence * 0.75,
	}
}

// estimateToolAdditionImpact estimates impact of adding/fixing a tool.
func (e *ImpactEstimator) estimateToolAdditionImpact(pattern PatternReport, research *ResearchReport, rec Recommendation) *ImpactEstimate {
	// Baseline: current tool failure rate
	currentFailureRate := pattern.MetricObserved
	targetFailureRate := 0.05 // Target 5% failure rate

	weeklyToolCalls := pattern.SessionCount * 5 // Estimate 5 tool calls per session

	// Current failures per week
	currentFailures := float64(weeklyToolCalls) * currentFailureRate
	avgFailureCost := 3.0 // 3 minutes per failure (retry time)
	currentWeeklyTime := currentFailures * avgFailureCost

	// Expected failures after fix
	expectedFailures := float64(weeklyToolCalls) * targetFailureRate
	expectedWeeklyTime := expectedFailures * avgFailureCost

	timeSaved := currentWeeklyTime - expectedWeeklyTime
	improvementPercent := ((currentFailureRate - targetFailureRate) / currentFailureRate) * 100

	return &ImpactEstimate{
		RecommendationID:   rec.Title,
		MetricType:         "failure_reduction",
		BaselineValue:      currentFailureRate * 100,
		ExpectedValue:      targetFailureRate * 100,
		ImprovementPercent: math.Min(improvementPercent, 90),
		WeeklyImpact:       fmt.Sprintf("Save ~%.0f minutes/week by reducing tool failures from %.0f to %.0f per week", timeSaved, currentFailures, expectedFailures),
		Confidence:         pattern.Confidence * 0.9,
	}
}

// estimateGenericImpact estimates impact for generic recommendations.
func (e *ImpactEstimator) estimateGenericImpact(pattern PatternReport, rec Recommendation) *ImpactEstimate {
	// Generic estimate based on session count and confidence
	weeklySessions := e.estimateWeeklySessions(pattern)

	// Assume 10% improvement for generic recommendations
	improvementFactor := 0.1
	timeSaved := float64(weeklySessions) * 2.0 * improvementFactor // 2 minutes average savings

	return &ImpactEstimate{
		RecommendationID:   rec.Title,
		MetricType:         "general_efficiency",
		BaselineValue:      float64(weeklySessions) * 2.0,
		ExpectedValue:      float64(weeklySessions) * 2.0 * (1 - improvementFactor),
		ImprovementPercent: improvementFactor * 100,
		WeeklyImpact:       fmt.Sprintf("Estimated ~%.0f minutes/week efficiency gain", timeSaved),
		Confidence:         pattern.Confidence * 0.6,
	}
}

// averageDurationFrom Evidence extracts average duration from pattern evidence.
func (e *ImpactEstimator) averageDurationFromEvidence(pattern PatternReport) float64 {
	for _, ev := range pattern.Evidence {
		if ev.Metric == "duration" {
			return ev.Value
		}
	}
	return 600.0 // Default 10 minutes if not found
}

// estimateWeeklySessions estimates weekly sessions for this pattern.
func (e *ImpactEstimator) estimateWeeklySessions(pattern PatternReport) int {
	// Assume pattern.SessionCount is monthly, convert to weekly
	weekly := max(pattern.SessionCount/4, 1)
	return weekly
}

// AggregateImpact aggregates multiple impact estimates.
func (e *ImpactEstimator) AggregateImpact(estimates []*ImpactEstimate) *AggregateImpact {
	if len(estimates) == 0 {
		return nil
	}

	totalTimeSaved := 0.0
	highestConfidence := 0.0

	for _, est := range estimates {
		// Extract time saved from weekly impact
		totalTimeSaved += e.extractTimeSaved(est.WeeklyImpact)
		if est.Confidence > highestConfidence {
			highestConfidence = est.Confidence
		}
	}

	return &AggregateImpact{
		TotalWeeklyTimeSaved:  totalTimeSaved,
		TotalMonthlyTimeSaved: totalTimeSaved * 4,
		RecommendationCount:   len(estimates),
		AverageConfidence:     highestConfidence / float64(len(estimates)),
		EstimatedMonthlyCost:  e.estimateCostSavings(totalTimeSaved),
	}
}

// extractTimeSaved extracts numeric time saved from impact string.
func (e *ImpactEstimator) extractTimeSaved(impact string) float64 {
	// Parse "Save ~X minutes/week" format
	var minutes float64
	fmt.Sscanf(impact, "Save ~%f minutes", &minutes)
	if minutes == 0 {
		minutes = 5.0 // Default if parsing fails
	}
	return minutes
}

// estimateCostSavings estimates cost savings from time saved.
func (e *ImpactEstimator) estimateCostSavings(minutesSaved float64) float64 {
	return minutesSaved * e.config.LaborCostPerMinute
}

// AggregateImpact represents aggregated impact across all recommendations.
type AggregateImpact struct {
	TotalWeeklyTimeSaved  float64
	TotalMonthlyTimeSaved float64
	RecommendationCount   int
	AverageConfidence     float64
	EstimatedMonthlyCost  float64
}

// FormatReport formats the impact analysis as a human-readable report.
func (e *ImpactEstimator) FormatReport(estimates []*ImpactEstimate, aggregate *AggregateImpact) string {
	var buf strings.Builder

	buf.WriteString("## Impact Analysis Report\n\n")
	buf.WriteString("### Summary\n")
	if aggregate != nil {
		fmt.Fprintf(&buf, "- **Total Recommendations**: %d\n", aggregate.RecommendationCount)
		fmt.Fprintf(&buf, "- **Estimated Monthly Time Saved**: %.0f hours (%.0f minutes)\n", aggregate.TotalMonthlyTimeSaved/60, aggregate.TotalMonthlyTimeSaved)
		fmt.Fprintf(&buf, "- **Average Confidence**: %.0f%%\n", aggregate.AverageConfidence*100)
		if aggregate.EstimatedMonthlyCost > 0 {
			fmt.Fprintf(&buf, "- **Estimated Monthly Cost Savings**: $%.2f\n", aggregate.EstimatedMonthlyCost)
		}
	}
	buf.WriteString("\n### Detailed Impacts\n\n")

	for i, est := range estimates {
		fmt.Fprintf(&buf, "#### Recommendation %d: %s\n", i+1, est.RecommendationID)
		fmt.Fprintf(&buf, "- **Metric Type**: %s\n", est.MetricType)
		fmt.Fprintf(&buf, "- **Baseline**: %.1f\n", est.BaselineValue)
		fmt.Fprintf(&buf, "- **Expected**: %.1f\n", est.ExpectedValue)
		buf.WriteString(fmt.Sprintf("- **Improvement**: %.0f%%\n", est.ImprovementPercent))
		buf.WriteString(fmt.Sprintf("- **Weekly Impact**: %s\n", est.WeeklyImpact))
		buf.WriteString(fmt.Sprintf("- **Confidence**: %.0f%%\n\n", est.Confidence*100))
	}

	return buf.String()
}
