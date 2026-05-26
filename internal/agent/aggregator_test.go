package agent

import (
	"testing"
)

func TestNewTaskReportAggregator(t *testing.T) {
	a := NewTaskReportAggregator()
	if a == nil {
		t.Fatal("expected non-nil aggregator")
	}
	if a.RecommendationCount() != 0 {
		t.Errorf("expected 0 recommendations, got %d", a.RecommendationCount())
	}
}

func TestTaskReportAggregator_ExtractRecommendations_JSONBlock(t *testing.T) {
	a := NewTaskReportAggregator()

	result := `I completed the task successfully.

` + "```recommendations\n" + `[
  {"category": "security", "priority": "high", "description": "Add input validation", "agent_id": "coder", "confidence": 0.9},
  {"category": "performance", "priority": "medium", "description": "Cache database queries", "agent_id": "coder", "confidence": 0.7}
]
` + "```" + `

The changes are ready for review.`

	recs := a.ExtractRecommendations(result, "coder")
	if len(recs) != 2 {
		t.Fatalf("expected 2 recommendations, got %d", len(recs))
	}

	if recs[0].Category != "security" {
		t.Errorf("expected category 'security', got %s", recs[0].Category)
	}
	if recs[0].Priority != "high" {
		t.Errorf("expected priority 'high', got %s", recs[0].Priority)
	}
	if recs[1].AgentID != "coder" {
		t.Errorf("expected agent_id 'coder', got %s", recs[1].AgentID)
	}
}

func TestTaskReportAggregator_ExtractRecommendations_StandardJSONBlock(t *testing.T) {
	a := NewTaskReportAggregator()

	result := `Task done.

` + "```json\n" + `[
  {"category": "maintainability", "priority": "low", "description": "Refactor helper function", "confidence": 0.6}
]
` + "```"

	recs := a.ExtractRecommendations(result, "debugger")
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}
	if recs[0].AgentID != "debugger" {
		t.Errorf("expected agent_id 'debugger', got %s", recs[0].AgentID)
	}
}

func TestTaskReportAggregator_ExtractRecommendations_Empty(t *testing.T) {
	a := NewTaskReportAggregator()

	recs := a.ExtractRecommendations("", "coder")
	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations for empty input, got %d", len(recs))
	}

	recs = a.ExtractRecommendations("No recommendations here", "coder")
	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations for plain text, got %d", len(recs))
	}
}

func TestTaskReportAggregator_AddRecommendations(t *testing.T) {
	a := NewTaskReportAggregator()

	recs := []CategorizedRecommendation{
		{Category: "security", Priority: "high", Description: "Fix XSS vulnerability", AgentID: "coder"},
		{Category: "performance", Priority: "medium", Description: "Add caching", AgentID: "analyst"},
	}
	a.AddRecommendations(recs)

	if a.RecommendationCount() != 2 {
		t.Errorf("expected 2 recommendations, got %d", a.RecommendationCount())
	}
}

func TestTaskReportAggregator_DeduplicateRecommendations(t *testing.T) {
	a := NewTaskReportAggregator()

	// Add duplicate recommendations
	a.AddRecommendations([]CategorizedRecommendation{
		{Category: "security", Priority: "high", Description: "Fix XSS vulnerability", AgentID: "coder"},
		{Category: "security", Priority: "medium", Description: "Fix XSS vulnerability", AgentID: "analyst"}, // Same desc, different priority
		{Category: "performance", Priority: "low", Description: "Add caching", AgentID: "coder"},
	})

	deduped := a.DeduplicateRecommendations()
	if len(deduped) != 2 {
		t.Fatalf("expected 2 deduplicated recommendations, got %d", len(deduped))
	}

	// The high priority one should be kept (sorted first)
	if deduped[0].Priority != "high" {
		t.Errorf("expected first deduplicated to be high priority, got %s", deduped[0].Priority)
	}
}

func TestTaskReportAggregator_BuildReport(t *testing.T) {
	a := NewTaskReportAggregator()

	a.AddRecommendations([]CategorizedRecommendation{
		{Category: "security", Priority: "critical", Description: "Fix auth bypass", AgentID: "coder", Confidence: 0.95},
		{Category: "follow-up", Priority: "low", Description: "Add tests", AgentID: "coder", Confidence: 0.6},
	})

	report := a.BuildReport("Fixed auth bypass and added tests", 3, 3, "45s")

	if report.Summary != "Fixed auth bypass and added tests" {
		t.Errorf("unexpected summary: %s", report.Summary)
	}
	if report.StepsCompleted != 3 {
		t.Errorf("expected 3 steps completed, got %d", report.StepsCompleted)
	}
	if report.StepsTotal != 3 {
		t.Errorf("expected 3 steps total, got %d", report.StepsTotal)
	}
	if report.ExecutionTime != "45s" {
		t.Errorf("expected 45s, got %s", report.ExecutionTime)
	}
	if len(report.Recommendations) != 2 {
		t.Errorf("expected 2 recommendations, got %d", len(report.Recommendations))
	}
	// Should be sorted by priority (critical first)
	if report.Recommendations[0].Priority != "critical" {
		t.Errorf("expected critical first, got %s", report.Recommendations[0].Priority)
	}
}

func TestTaskReportAggregator_FilterByCategory(t *testing.T) {
	a := NewTaskReportAggregator()

	a.AddRecommendations([]CategorizedRecommendation{
		{Category: "security", Priority: "high", Description: "Fix auth"},
		{Category: "performance", Priority: "medium", Description: "Add cache"},
		{Category: "security", Priority: "low", Description: "Update deps"},
	})

	security := a.GetRecommendationsByCategory("security")
	if len(security) != 2 {
		t.Errorf("expected 2 security recs, got %d", len(security))
	}

	perf := a.GetRecommendationsByCategory("performance")
	if len(perf) != 1 {
		t.Errorf("expected 1 performance rec, got %d", len(perf))
	}

	none := a.GetRecommendationsByCategory("nonexistent")
	if len(none) != 0 {
		t.Errorf("expected 0 for unknown category, got %d", len(none))
	}
}

func TestTaskReportAggregator_FilterByPriority(t *testing.T) {
	a := NewTaskReportAggregator()

	a.AddRecommendations([]CategorizedRecommendation{
		{Category: "security", Priority: "critical", Description: "Auth bypass"},
		{Category: "performance", Priority: "high", Description: "N+1 query"},
		{Category: "security", Priority: "critical", Description: "SQL injection"},
		{Category: "maintainability", Priority: "medium", Description: "Refactor"},
	})

	critical := a.GetRecommendationsByPriority("critical")
	if len(critical) != 2 {
		t.Errorf("expected 2 critical recs, got %d", len(critical))
	}

	medium := a.GetRecommendationsByPriority("medium")
	if len(medium) != 1 {
		t.Errorf("expected 1 medium rec, got %d", len(medium))
	}
}

func TestAggregatedTaskReport_Structure(t *testing.T) {
	report := AggregatedTaskReport{
		Summary:        "Test summary",
		StepsCompleted: 5,
		StepsTotal:     7,
		Recommendations: []CategorizedRecommendation{
			{Category: "test", Priority: "low", Description: "Test rec"},
		},
		ExecutionTime: "30s",
	}

	if report.Summary != "Test summary" {
		t.Error("summary mismatch")
	}
	if report.StepsCompleted != 5 {
		t.Error("steps completed mismatch")
	}
	if report.StepsTotal != 7 {
		t.Error("steps total mismatch")
	}
	if len(report.Recommendations) != 1 {
		t.Error("recommendations count mismatch")
	}
}

func TestCategorizedRecommendation_Structure(t *testing.T) {
	rec := CategorizedRecommendation{
		Category:        "security",
		Priority:        "high",
		Description:     "Fix authentication",
		AgentID:         "coder",
		Confidence:      0.9,
		CodeSnippet:     "if err != nil { return err }",
		RelatedFiles:    []string{"auth.go", "middleware.go"},
		EstimatedEffort: "small",
	}

	if rec.Category != "security" {
		t.Error("category mismatch")
	}
	if rec.EstimatedEffort != "small" {
		t.Error("effort mismatch")
	}
	if len(rec.RelatedFiles) != 2 {
		t.Error("related files count mismatch")
	}
}
