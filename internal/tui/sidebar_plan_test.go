package tui

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tui/components"
)

// TestSidebar_PlanPanel_RendersPhases verifies that the sidebar's Plan panel
// renders phase data from SetPlanPhases via the PlanView widget. This covers
// the user-facing wiring requirement: phases set via SetPlanPhases (or a
// SidebarDataMsg carrying PlanPhases) become visible in the sidebar's View().
func TestSidebar_PlanPanel_RendersPhases(t *testing.T) {
	s := &SidebarModel{
		styles: DefaultStyles(),
		expandedPanels: map[SidebarPanel]bool{
			PanelPlan: true,
		},
		panelHeaderY: make(map[SidebarPanel]int),
		planView: components.NewPlanView(components.PlanViewConfig{
			Title: "test plan",
		}),
	}

	phases := []components.PhaseRow{
		{
			Name:           "ingest",
			State:          "completed",
			Sequence:       1,
			TotalSteps:     3,
			CompletedSteps: 3,
			Produces: []components.PhaseArtifact{
				{Name: "dataset", Kind: "file"},
			},
		},
		{
			Name:           "train",
			State:          "active",
			Sequence:       2,
			TotalSteps:     5,
			CompletedSteps: 2,
			Consumes: []components.PhaseArtifact{
				{Name: "dataset", Kind: "file", Required: true},
			},
		},
	}
	s.SetPlanPhases(phases)

	out := s.renderPlanPanel()
	if out == "" {
		t.Fatal("renderPlanPanel returned empty string")
	}
	if !strings.Contains(out, "test plan") {
		t.Errorf("expected output to contain title %q, got: %s", "test plan", out)
	}
	if !strings.Contains(out, "ingest") {
		t.Errorf("expected output to contain phase %q, got: %s", "ingest", out)
	}
	if !strings.Contains(out, "train") {
		t.Errorf("expected output to contain phase %q, got: %s", "train", out)
	}
	if !strings.Contains(out, "produces:") {
		t.Errorf("expected output to contain produces artifact label, got: %s", out)
	}
	if !strings.Contains(out, "consumes:") {
		t.Errorf("expected output to contain consumes artifact label, got: %s", out)
	}
}

// TestSidebar_PlanPanel_EmptyState verifies the panel renders a placeholder
// when no phases have been provided.
func TestSidebar_PlanPanel_EmptyState(t *testing.T) {
	s := &SidebarModel{
		styles:       DefaultStyles(),
		expandedPanels: map[SidebarPanel]bool{
			PanelPlan: true,
		},
		panelHeaderY: make(map[SidebarPanel]int),
		planView:     components.NewPlanView(components.PlanViewConfig{}),
	}

	out := s.renderPlanPanel()
	if !strings.Contains(out, "Plan") {
		t.Errorf("expected header to contain %q, got: %s", "Plan", out)
	}
	// PlanView renders "no phases" when empty.
	if !strings.Contains(out, "no phases") {
		t.Errorf("expected empty-state placeholder, got: %s", out)
	}
}

// TestSidebar_PlanPanel_SetPlanPhasesExpands verifies that SetPlanPhases
// auto-expands the panel when non-empty data arrives, so users see the
// updated plan without manually toggling the panel.
func TestSidebar_PlanPanel_SetPlanPhasesExpands(t *testing.T) {
	s := &SidebarModel{
		styles:       DefaultStyles(),
		expandedPanels: map[SidebarPanel]bool{
			PanelPlan: false,
		},
		panelHeaderY: make(map[SidebarPanel]int),
		planView:     components.NewPlanView(components.PlanViewConfig{}),
	}

	s.SetPlanPhases([]components.PhaseRow{{Name: "p1", State: "active"}})
	if !s.expandedPanels[PanelPlan] {
		t.Error("expected PanelPlan to be expanded after SetPlanPhases with non-empty data")
	}
}

// TestSidebar_PlanPanel_NilPlanViewSafe verifies the panel does not panic
// when planView is nil (e.g., if sidebar was constructed without one).
func TestSidebar_PlanPanel_NilPlanViewSafe(t *testing.T) {
	s := &SidebarModel{
		styles:       DefaultStyles(),
		expandedPanels: map[SidebarPanel]bool{
			PanelPlan: true,
		},
		panelHeaderY: make(map[SidebarPanel]int),
		planView:     nil,
	}

	// Should not panic and should return the header + placeholder.
	out := s.renderPlanPanel()
	if !strings.Contains(out, "Plan") {
		t.Errorf("expected header to contain %q, got: %s", "Plan", out)
	}
	if !strings.Contains(out, "no active plan") {
		t.Errorf("expected nil placeholder, got: %s", out)
	}
}

// TestSidebar_SetPlanPhases_NilGuard verifies SetPlanPhases does not panic
// when planView is nil. This satisfies the project-wide typed-nil guard rule.
func TestSidebar_SetPlanPhases_NilGuard(t *testing.T) {
	s := &SidebarModel{
		planView: nil,
	}
	// Must not panic.
	s.SetPlanPhases([]components.PhaseRow{{Name: "x"}})
}
