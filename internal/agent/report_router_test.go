package agent

import (
	"context"
	"testing"
)

func TestReportRouterRouteActionClose(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 5,
	})

	report := &AgentReport{
		Status:       ReportStatusCompleted,
		Accomplished: []string{"implemented auth module"},
	}
	action := DetermineRouteAction(report)

	result := router.Route(context.Background(), RouteParams{
		Report:  report,
		Action:  action,
		AgentID: "coder",
		Depth:   0,
	})
	if result.FinalResponse == "" {
		t.Error("expected non-empty final response for RouteActionClose")
	}
	if result.Depth != 0 {
		t.Errorf("Depth = %d, want 0", result.Depth)
	}
}

func TestReportRouterMaxDepth(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 2,
	})

	report := &AgentReport{
		Status:             ReportStatusCompleted,
		SuggestedNextAgent: "reviewer",
	}
	action := DetermineRouteAction(report)

	result := router.Route(context.Background(), RouteParams{
		Report:  report,
		Action:  action,
		AgentID: "coder",
		Depth:   2, // at max depth
	})
	// Should be forced to notify user instead of routing
	if result.ForceNotify {
		t.Log("correctly forced notify at max depth")
	} else {
		t.Error("expected ForceNotify at max depth")
	}
}

func TestReportRouterRouteActionRoute(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 5,
	})

	report := &AgentReport{
		Status:             ReportStatusCompleted,
		SuggestedNextAgent: "reviewer",
	}
	action := DetermineRouteAction(report)

	result := router.Route(context.Background(), RouteParams{
		Report:  report,
		Action:  action,
		AgentID: "coder",
		Depth:   0,
	})
	if result.FinalResponse != "" {
		t.Errorf("expected empty FinalResponse for RouteActionRoute, got %q", result.FinalResponse)
	}
	if result.Depth != 1 {
		t.Errorf("Depth = %d, want 1 (incremented)", result.Depth)
	}
	if result.ForceNotify {
		t.Error("expected ForceNotify=false for RouteActionRoute")
	}
}

func TestReportRouterRouteActionNotifyUser(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 5,
	})

	report := &AgentReport{
		Status:             ReportStatusCompleted,
		UserDecisionNeeded: true,
		DecisionContext:    "choose deployment target",
		NotDone:            []string{"deploy to staging", "run integration tests"},
	}
	action := DetermineRouteAction(report)

	result := router.Route(context.Background(), RouteParams{
		Report:  report,
		Action:  action,
		AgentID: "coder",
		Depth:   0,
	})
	if !result.ForceNotify {
		t.Error("expected ForceNotify for RouteActionNotifyUser")
	}
	if result.FinalResponse == "" {
		t.Error("expected non-empty FinalResponse for RouteActionNotifyUser")
	}
}

func TestReportRouterRouteActionNotifyError(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 5,
	})

	report := &AgentReport{
		Status: ReportStatusFailed,
		Issues: []string{"compilation failed", "missing dependency"},
	}
	action := DetermineRouteAction(report)

	result := router.Route(context.Background(), RouteParams{
		Report:  report,
		Action:  action,
		AgentID: "coder",
		Depth:   0,
	})
	if !result.ForceNotify {
		t.Error("expected ForceNotify for RouteActionNotifyError")
	}
	if result.FinalResponse == "" {
		t.Error("expected non-empty FinalResponse for RouteActionNotifyError")
	}
}

func TestReportRouterNilReport(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 5,
	})

	action := DetermineRouteAction(nil)

	result := router.Route(context.Background(), RouteParams{
		Report:  nil,
		Action:  action,
		AgentID: "coder",
		Depth:   0,
	})
	if result.FinalResponse != "" {
		t.Errorf("expected empty FinalResponse for nil report with RouteActionClose, got %q", result.FinalResponse)
	}
}

func TestReportRouterDefaultMaxDepth(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 0, // zero should use default
	})
	if router.maxDepth != defaultMaxRouteDepth {
		t.Errorf("maxDepth = %d, want default %d", router.maxDepth, defaultMaxRouteDepth)
	}
}

func TestReportRouterNeedsInput(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 5,
	})

	report := &AgentReport{
		Status:          ReportStatusNeedsInput,
		DecisionContext: "which database to use?",
	}
	action := DetermineRouteAction(report)

	result := router.Route(context.Background(), RouteParams{
		Report:  report,
		Action:  action,
		AgentID: "planner",
		Depth:   0,
	})
	if !result.ForceNotify {
		t.Error("expected ForceNotify for needs_input status")
	}
	if result.FinalResponse == "" {
		t.Error("expected non-empty FinalResponse for needs_input status")
	}
}

func TestReportRouterPartialNoRouting(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 5,
	})

	report := &AgentReport{
		Status:  ReportStatusPartial,
		NotDone: []string{"unit tests", "documentation"},
	}
	action := DetermineRouteAction(report)

	result := router.Route(context.Background(), RouteParams{
		Report:  report,
		Action:  action,
		AgentID: "coder",
		Depth:   1,
	})
	if !result.ForceNotify {
		t.Error("expected ForceNotify for partial without routing")
	}
}
