package q

import (
	"log/slog"
	"strings"
	"testing"
)

var impactLogger = slog.Default()

func TestImpactEstimatorEstimateNewAgentImpact(t *testing.T) {
	config := ImpactEstimatorConfig{
		WeeklySessionsEstimate: 5,
		AverageTokenCost:       0.001,
		LaborCostPerMinute:     0.50,
	}

	estimator := NewImpactEstimator(impactLogger, config)

	pattern := PatternReport{
		ID:       "p1",
		Confidence: 0.8,
		SessionCount: 20,
		Evidence: []PatternEvidence{
			{Metric: "duration", Value: 600.0}, // 10 min default
		},
	}

	research := &ResearchReport{
		Recommendations: []Recommendation{
			{Type: "new_agent", Title: "Create debug specialist"},
		},
	}

	rec := Recommendation{
		Type:           "new_agent",
		Title:          "Create debug specialist",
		Description:    "Need specialist agent",
		Priority:       "high",
		ExpectedImpact: "40% improvement",
	}

	estimate := estimator.estimateNewAgentImpact(pattern, research, rec)

	if estimate == nil {
		t.Fatal("estimate should not be nil")
	}
	if estimate.MetricType != "time_saved" {
		t.Errorf("expected metricType 'time_saved', got %q", estimate.MetricType)
	}
	if estimate.ImprovementPercent != 50.0 {
		t.Errorf("expected improvement 50%%, got %.1f%%", estimate.ImprovementPercent)
	}
	if estimate.WeeklyImpact == "" {
		t.Error("weeklyImpact should not be empty")
	}
	if estimate.Confidence <= 0 {
		t.Error("confidence should be > 0")
	}
}

func TestImpactEstimatorEstimateSpecUpdateImpact(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{
		WeeklySessionsEstimate: 5,
		AverageTokenCost:       0.001,
		LaborCostPerMinute:     0.50,
	})

	pattern := PatternReport{
		Confidence:   0.8,
		SessionCount: 20,
		MetricObserved: 0.5, // 50% rejection rate
	}

	rec := Recommendation{
		Type:    "update_spec",
		Title:   "Update coder spec",
		Priority: "high",
	}

	research := &ResearchReport{}

	estimate := estimator.estimateSpecUpdateImpact(pattern, research, rec)

	if estimate == nil {
		t.Fatal("estimate should not be nil")
	}
	if estimate.MetricType != "revision_reduction" {
		t.Errorf("expected metricType 'revision_reduction', got %q", estimate.MetricType)
	}
	if estimate.BaselineValue != 50.0 {
		t.Errorf("expected baselineValue 50.0, got %.1f", estimate.BaselineValue)
	}
	if estimate.ExpectedValue != 10.0 {
		t.Errorf("expected expectedValue 10.0 (10%%), got %.1f", estimate.ExpectedValue)
	}
	if estimate.ImprovementPercent <= 0 {
		t.Error("expected positive improvement")
	}
}

func TestImpactEstimatorEstimateModelReassignmentImpact(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	pattern := PatternReport{
		Confidence:   0.7,
		SessionCount: 10,
		MetricObserved: 4.0, // 4x variance
		Evidence: []PatternEvidence{
			{Metric: "duration", Value: 300.0}, // 5 min
		},
	}

	rec := Recommendation{
		Type:    "reassign_model",
		Title:   "Reassign model",
		Priority: "medium",
	}

	research := &ResearchReport{}

	estimate := estimator.estimateModelReassignmentImpact(pattern, research, rec)

	if estimate == nil {
		t.Fatal("estimate should not be nil")
	}
	if estimate.MetricType != "duration_variance_reduction" {
		t.Errorf("expected metricType 'duration_variance_reduction', got %q", estimate.MetricType)
	}
	if estimate.BaselineValue != 4.0 {
		t.Errorf("expected baseline 4.0, got %.1f", estimate.BaselineValue)
	}
	if estimate.ExpectedValue != 1.5 {
		t.Errorf("expected expected 1.5, got %.1f", estimate.ExpectedValue)
	}
}

func TestImpactEstimatorEstimateToolAdditionImpact(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	pattern := PatternReport{
		Confidence:   0.85,
		SessionCount: 10,
		MetricObserved: 0.5, // 50% tool failure rate
	}

	rec := Recommendation{
		Type:    "add_tool",
		Title:   "Fix shell_execute tool",
		Priority: "high",
	}

	research := &ResearchReport{}

	estimate := estimator.estimateToolAdditionImpact(pattern, research, rec)

	if estimate == nil {
		t.Fatal("estimate should not be nil")
	}
	if estimate.MetricType != "failure_reduction" {
		t.Errorf("expected metricType 'failure_reduction', got %q", estimate.MetricType)
	}
	if estimate.BaselineValue != 50.0 {
		t.Errorf("expected baseline 50.0, got %.1f", estimate.BaselineValue)
	}
	if estimate.ExpectedValue != 5.0 {
		t.Errorf("expected expected 5.0, got %.1f", estimate.ExpectedValue)
	}
}

func TestImpactEstimatorEstimateGenericImpact(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	pattern := PatternReport{
		Confidence:   0.6,
		SessionCount: 8,
	}

	rec := Recommendation{
		Type:    "update_prompt",
		Title:   "Generic improvement",
		Priority: "medium",
	}

	estimate := estimator.estimateGenericImpact(pattern, rec)

	if estimate == nil {
		t.Fatal("estimate should not be nil")
	}
	if estimate.MetricType != "general_efficiency" {
		t.Errorf("expected metricType 'general_efficiency', got %q", estimate.MetricType)
	}
	if estimate.ImprovementPercent != 10.0 {
		t.Errorf("expected improvement 10%%, got %.1f%%", estimate.ImprovementPercent)
	}
}

func TestImpactEstimatorEstimateImpactDispatch(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	pattern := PatternReport{}
	research := &ResearchReport{}

	tests := []struct {
		recType     string
		wantMetric  string
	}{
		{"new_agent", "time_saved"},
		{"update_spec", "revision_reduction"},
		{"reassign_model", "duration_variance_reduction"},
		{"add_tool", "failure_reduction"},
		{"unknown_type", "general_efficiency"},
	}

	for _, tt := range tests {
		t.Run(tt.recType, func(t *testing.T) {
			rec := Recommendation{Type: tt.recType, Title: "test"}
			estimate := estimator.EstimateImpact(pattern, research, rec)

			if estimate == nil {
				t.Fatal("estimate should not be nil")
			}
			if estimate.MetricType != tt.wantMetric {
				t.Errorf("metricType = %q, want %q", estimate.MetricType, tt.wantMetric)
			}
		})
	}
}

func TestImpactEstimatorEstimateWeeklySessions(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	tests := []struct {
		sessionCount int
		wantMin      int
	}{
		{0, 1},
		{1, 1},
		{4, 1},
		{8, 2},
		{20, 5},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			pattern := PatternReport{SessionCount: tt.sessionCount}
			got := estimator.estimateWeeklySessions(pattern)
			if got < tt.wantMin {
				t.Errorf("estimateWeeklySessions(%d) = %d < %d", tt.sessionCount, got, tt.wantMin)
			}
		})
	}
}

func TestImpactEstimatorAverageDurationFromEvidence(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	tests := []struct {
		name     string
		evidence []PatternEvidence
		want     float64
	}{
		{
			name:     "no evidence",
			evidence: nil,
			want:     600.0, // default
		},
		{
			name: "empty evidence",
			evidence: []PatternEvidence{},
			want:     600.0,
		},
		{
			name: "has duration evidence",
			evidence: []PatternEvidence{
				{Metric: "duration", Value: 900.0},
			},
			want: 900.0,
		},
		{
			name: "has non-duration evidence first",
			evidence: []PatternEvidence{
				{Metric: "difficulty", Value: 0.8},
				{Metric: "duration", Value: 1200.0},
			},
			want: 1200.0, // returns first duration match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := PatternReport{Evidence: tt.evidence}
			got := estimator.averageDurationFromEvidence(pattern)
			if got != tt.want {
				t.Errorf("averageDurationFromEvidence() = %.1f, want %.1f", got, tt.want)
			}
		})
	}
}

func TestImpactEstimatorAggregateImpact(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	tests := []struct {
		name        string
		estimates   []*ImpactEstimate
		wantNil     bool
		wantCount   int
	}{
		{
			name:      "nil estimates",
			estimates: nil,
			wantNil:   true,
		},
		{
			name:      "empty estimates",
			estimates: []*ImpactEstimate{},
			wantNil:   true,
		},
		{
			name: "single estimate",
			estimates: []*ImpactEstimate{
				{
					WeeklyImpact:      "Save ~10 minutes/week",
					Confidence:        0.8,
					ImprovementPercent: 50.0,
				},
			},
			wantNil:   false,
			wantCount: 1,
		},
		{
			name: "multiple estimates",
			estimates: []*ImpactEstimate{
				{WeeklyImpact: "Save ~5 minutes/week", Confidence: 0.7},
				{WeeklyImpact: "Save ~15 minutes/week", Confidence: 0.9},
				{WeeklyImpact: "Save ~3 minutes/week", Confidence: 0.6},
			},
			wantNil:   false,
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := estimator.AggregateImpact(tt.estimates)
			if tt.wantNil && result != nil {
				t.Error("expected nil result")
			}
			if !tt.wantNil && result == nil {
				t.Fatal("expected non-nil result")
			}
			if result != nil && result.RecommendationCount != tt.wantCount {
				t.Errorf("expected %d recommendations, got %d", tt.wantCount, result.RecommendationCount)
			}
			if !tt.wantNil && result != nil {
				if result.TotalMonthlyTimeSaved <= 0 {
					t.Errorf("expected positive monthly time saved, got %.1f", result.TotalMonthlyTimeSaved)
				}
				if result.TotalMonthlyTimeSaved < result.TotalWeeklyTimeSaved {
					t.Error("monthly should be >= weekly")
				}
			}
		})
	}
}

func TestImpactEstimatorExtractTimeSaved(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	tests := []struct {
		impact string
		want   float64
	}{
		{"Save ~10 minutes/week", 10.0},
		{"Save ~100.5 minutes/week", 100.5},
		{"no savings info", 5.0}, // default
		{"", 5.0},                // default
	}

	for _, tt := range tests {
		t.Run(tt.impact, func(t *testing.T) {
			got := estimator.extractTimeSaved(tt.impact)
			if got != tt.want {
				t.Errorf("extractTimeSaved(%q) = %.1f, want %.1f", tt.impact, got, tt.want)
			}
		})
	}
}

func TestImpactEstimatorEstimateCostSavings(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{
		LaborCostPerMinute: 0.50,
	})

	savings := estimator.estimateCostSavings(60.0) // 60 minutes saved
	if savings != 30.0 {
		t.Errorf("expected cost savings $30.00, got $%.2f", savings)
	}
}

func TestImpactEstimatorFormatReport(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	estimate := &ImpactEstimate{
		RecommendationID:   "test-recommendation",
		MetricType:         "time_saved",
		BaselineValue:      100.0,
		ExpectedValue:      50.0,
		ImprovementPercent: 50.0,
		WeeklyImpact:       "Save ~50 minutes/week",
		Confidence:         0.8,
	}

	aggregate := &AggregateImpact{
		TotalWeeklyTimeSaved:  50.0,
		TotalMonthlyTimeSaved: 200.0,
		RecommendationCount:   1,
		AverageConfidence:     0.8,
		EstimatedMonthlyCost:  100.0,
	}

	report := estimator.FormatReport([]*ImpactEstimate{estimate}, aggregate)

	if report == "" {
		t.Fatal("formatReport returned empty string")
	}
	if !containsStr(report, "## Impact Analysis Report") {
		t.Error("missing report header")
	}
	if !containsStr(report, "test-recommendation") {
		t.Error("missing recommendation name")
	}
	if !containsStr(report, "time_saved") {
		t.Error("missing metric type")
	}
}

func TestImpactEstimatorFormatReportNilAggregate(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	estimate := &ImpactEstimate{
		RecommendationID: "test",
	}

	report := estimator.FormatReport([]*ImpactEstimate{estimate}, nil)
	if report == "" {
		t.Fatal("formatReport should not be empty even with nil aggregate")
	}
	if !containsStr(report, "## Impact Analysis Report") {
		t.Error("missing header even with nil aggregate")
	}
}

func TestImpactEstimatorSpecUpdateImprovementCapped(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	// Set a very low rejection rate to test the 80% cap
	pattern := PatternReport{
		Confidence:   0.9,
		SessionCount: 100,
		MetricObserved: 0.95, // 95% rejection
	}

	rec := Recommendation{
		Type:    "update_spec",
		Title:   "Update spec",
		Priority: "high",
	}

	research := &ResearchReport{}

	estimate := estimator.estimateSpecUpdateImpact(pattern, research, rec)

	// Improvement from 95%% to 10%% is 89%, should be capped at 80%
	if estimate.ImprovementPercent > 80.0 {
		t.Errorf("improvement should be capped at 80%%, got %.1f%%", estimate.ImprovementPercent)
	}
}

func TestImpactEstimatorModelReassignmentImprovementCapped(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	pattern := PatternReport{
		Confidence:   0.9,
		SessionCount: 50,
		MetricObserved: 10.0, // 10x variance
		Evidence: []PatternEvidence{
			{Metric: "duration", Value: 120.0},
		},
	}

	rec := Recommendation{
		Type:    "reassign_model",
		Title:   "Reassign model",
	}

	research := &ResearchReport{}

	estimate := estimator.estimateModelReassignmentImpact(pattern, research, rec)

	if estimate.ImprovementPercent > 50.0 {
		t.Errorf("model reassignment improvement should be capped at 50%%, got %.1f%%", estimate.ImprovementPercent)
	}
}

func TestImpactEstimatorToolAdditionImprovementCapped(t *testing.T) {
	estimator := NewImpactEstimator(impactLogger, ImpactEstimatorConfig{})

	pattern := PatternReport{
		Confidence:   0.9,
		SessionCount: 50,
		MetricObserved: 0.95, // 95% failure
	}

	rec := Recommendation{
		Type:    "add_tool",
		Title:   "Fix tool",
	}

	research := &ResearchReport{}

	estimate := estimator.estimateToolAdditionImpact(pattern, research, rec)

	if estimate.ImprovementPercent > 90.0 {
		t.Errorf("tool addition improvement should be capped at 90%%, got %.1f%%", estimate.ImprovementPercent)
	}
}

// Helper functions for testing

// Helper functions for testing
func containsStr(s, target string) bool {
	return strings.Contains(s, target)
}
