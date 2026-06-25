package components

import (
	"strings"
	"testing"
)

// TestPlanView_Render_Phases verifies the widget renders phase rows including
// produces/consumes artifacts and state labels.
func TestPlanView_Render_Phases(t *testing.T) {
	v := NewPlanView(PlanViewConfig{
		Title: "release pipeline",
		Phases: []PhaseRow{
			{
				Name:           "build",
				State:          "completed",
				Sequence:       1,
				TotalSteps:     2,
				CompletedSteps: 2,
				Produces: []PhaseArtifact{
					{Name: "binary", Kind: "file", Description: "compiled output"},
				},
			},
			{
				Name:           "deploy",
				State:          "active",
				Sequence:       2,
				TotalSteps:     4,
				CompletedSteps: 1,
				Consumes: []PhaseArtifact{
					{Name: "binary", Kind: "file", Required: true},
				},
			},
		},
	})

	out := v.Render()
	if !strings.Contains(out, "release pipeline") {
		t.Errorf("missing title: %s", out)
	}
	if !strings.Contains(out, "build") {
		t.Errorf("missing phase 'build': %s", out)
	}
	if !strings.Contains(out, "deploy") {
		t.Errorf("missing phase 'deploy': %s", out)
	}
	if !strings.Contains(out, "[completed]") {
		t.Errorf("missing state label '[completed]': %s", out)
	}
	if !strings.Contains(out, "[active]") {
		t.Errorf("missing state label '[active]': %s", out)
	}
	if !strings.Contains(out, "produces:") {
		t.Errorf("missing produces line: %s", out)
	}
	if !strings.Contains(out, "consumes:") {
		t.Errorf("missing consumes line: %s", out)
	}
	if !strings.Contains(out, "compiled output") {
		t.Errorf("missing artifact description: %s", out)
	}
	if !strings.Contains(out, "2/2 steps") {
		t.Errorf("missing step count for phase 1: %s", out)
	}
	if !strings.Contains(out, "1/4 steps") {
		t.Errorf("missing step count for phase 2: %s", out)
	}
}

// TestPlanView_Render_Empty verifies the widget renders a placeholder when no
// phases are configured.
func TestPlanView_Render_Empty(t *testing.T) {
	v := NewPlanView(PlanViewConfig{})
	out := v.Render()
	if !strings.Contains(out, "no phases") {
		t.Errorf("expected empty placeholder, got: %s", out)
	}
}

// TestPlanView_SetPhases verifies SetPhases replaces the phase data and is
// reflected in subsequent Render calls.
func TestPlanView_SetPhases(t *testing.T) {
	v := NewPlanView(PlanViewConfig{Title: "t"})
	if out := v.Render(); !strings.Contains(out, "no phases") {
		t.Fatalf("expected empty initial state, got: %s", out)
	}

	v.SetPhases([]PhaseRow{{Name: "alpha", State: "pending", TotalSteps: 1}})
	out := v.Render()
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected phase 'alpha' after SetPhases, got: %s", out)
	}
}

// TestPlanView_SetTitle verifies SetTitle replaces the title.
func TestPlanView_SetTitle(t *testing.T) {
	v := NewPlanView(PlanViewConfig{Title: "old"})
	v.SetPhases([]PhaseRow{{Name: "p", State: "pending"}})

	v.SetTitle("new title")
	out := v.Render()
	if !strings.Contains(out, "new title") {
		t.Errorf("expected title 'new title', got: %s", out)
	}
	if strings.Contains(out, "old") {
		t.Errorf("old title should be gone, got: %s", out)
	}
}

// TestPlanView_Render_DefaultTitle verifies that when no title is set, the
// default "phases" header is rendered.
func TestPlanView_Render_DefaultTitle(t *testing.T) {
	v := NewPlanView(PlanViewConfig{})
	v.SetPhases([]PhaseRow{{Name: "p", State: "pending"}})

	out := v.Render()
	if !strings.Contains(out, "phases") {
		t.Errorf("expected default title 'phases', got: %s", out)
	}
}
