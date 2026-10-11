package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type stubAgentsRPC struct{}

func (stubAgentsRPC) Call(method string, params any) (json.RawMessage, error) {
	return nil, nil
}

func (stubAgentsRPC) IsConnected() bool { return true }

// Regression: the agents table viewport was never given a width, so the panel
// rendered its header and border with an empty body while navigation worked.
func TestAgentsPanel_ViewRendersRows(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)

	p.agents = []AgentSummary{{
		ID:             "agent-alpha",
		Name:           "Alpha",
		Status:         "running",
		Tier:           "tier_1_reactive",
		LastInvocation: time.Now(),
	}}
	p.updateAgentsTable()

	out := p.View()
	if !strings.Contains(out, "agent-alpha") {
		t.Errorf("agents panel body is empty; want the agent id in the rendered table\n%s", out)
	}
	if !strings.Contains(out, "last run") {
		t.Errorf("agents panel is missing the 'last run' column header\n%s", out)
	}
}

// Regression: resizeColumns cleared the rows and nothing repopulated them, so a
// terminal resize blanked the table until the next data event.
func TestAgentsPanel_ResizeKeepsRows(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.agents = []AgentSummary{{ID: "agent-alpha", Status: "running"}}
	p.updateAgentsTable()

	p.SetSize(140, 40)

	if got := len(p.table.Rows()); got != 1 {
		t.Fatalf("rows after resize = %d, want 1 (resize must not blank the table)", got)
	}
	if !strings.Contains(p.View(), "agent-alpha") {
		t.Error("agents panel body is empty after a resize")
	}
}

// Row cells must always match the column count; bubbles/table panics otherwise.
func TestAgentsPanel_RowWidthMatchesColumns(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.agents = []AgentSummary{{ID: "agent-alpha", Status: "running"}}
	p.updateAgentsTable()

	cols := len(p.table.Columns())
	for i, row := range p.table.Rows() {
		if len(row) != cols {
			t.Errorf("row %d has %d cells, table has %d columns", i, len(row), cols)
		}
	}
}

// The goals sub-view (key 4) renders the cached goals through the
// tableutil-sized goals table, with lowercase health labels.
func TestAgentsPanel_GoalsViewRendersRows(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.selectedID = "agent-alpha"
	p.subView = agentsViewGoals
	p.goals = []AgentGoal{
		{ID: "g1", Title: "keep main shippable", Health: "healthy", State: "active"},
		{ID: "g2", Title: "nightly report", Health: "at_risk", State: "active"},
	}
	p.updateGoalsTable()

	out := p.View()
	if !strings.Contains(out, "keep main shippable") {
		t.Errorf("goals view body is empty; want the goal title in the rendered table\n%s", out)
	}
	if !strings.Contains(out, "healthy") || !strings.Contains(out, "at_risk") {
		t.Errorf("goals view missing health labels\n%s", out)
	}
	for _, banned := range []string{"Healthy", "At_Risk", "Broken"} {
		if strings.Contains(out, banned) {
			t.Errorf("goals view renders non-lowercase label %q", banned)
		}
	}

	// Goal rows must match the goals-table column count.
	cols := len(p.goalsTable.Columns())
	for i, row := range p.goalsTable.Rows() {
		if len(row) != cols {
			t.Errorf("goal row %d has %d cells, table has %d columns", i, len(row), cols)
		}
	}
}

// A resize must not blank the goals table (same regression class as the
// agents list table).
func TestAgentsPanel_GoalsViewResizeKeepsRows(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.selectedID = "agent-alpha"
	p.subView = agentsViewGoals
	p.goals = []AgentGoal{{ID: "g1", Title: "keep main shippable", Health: "healthy"}}
	p.updateGoalsTable()

	p.SetSize(140, 40)

	if got := len(p.goalsTable.Rows()); got != 1 {
		t.Fatalf("goal rows after resize = %d, want 1 (resize must not blank the table)", got)
	}
	if !strings.Contains(p.View(), "keep main shippable") {
		t.Error("goals view body is empty after a resize")
	}
}
