package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

// rosterFakeRPC answers the roster fetch with C1-shaped rows:
// {id, name, type, status, active_count, daily_cost_cents, enabled}.
// It records the methods it was asked for so a future leaf can also pin
// the data source, but this leaf asserts only on rendered output.
type rosterFakeRPC struct {
	calls []string
}

func (f *rosterFakeRPC) Call(method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	return json.RawMessage(rosterPayloadJSON), nil
}

func (f *rosterFakeRPC) IsConnected() bool { return true }

// Three distinct C1 types, each with a distinct active_count. The values are
// chosen so they cannot be confused with row indices or the panel's title.
const rosterPayloadJSON = `{"agents":[
	{"id":"a-1","name":"Alpha","type":"agent","status":"running","active_count":13,"daily_cost_cents":1234,"enabled":true},
	{"id":"b-1","name":"Beta","type":"bot","status":"running","active_count":27,"daily_cost_cents":5678,"enabled":true},
	{"id":"e-1","name":"Echo","type":"employee","status":"running","active_count":41,"daily_cost_cents":9012,"enabled":true}
]}`

func newRosterPanel(t *testing.T, f *rosterFakeRPC) *AgentsPanel {
	t.Helper()
	p := NewAgentsPanel(f)
	p.SetSize(160, 40)
	// Drive the fetch chain synchronously the same way buildPanel does for
	// kanban: Init returns the fetch cmd, Update applies the resulting msg.
	cmd := p.Init()
	for i := 0; i < 4 && cmd != nil; i++ {
		msg := cmd()
		cmd = p.Update(msg)
	}
	return p
}

// rosterRowCells flattens every rendered table cell into one string slice so
// a type/active value can be checked as an exact cell (not a substring of
// the panel title "agents" or the "N agents" count).
func rosterRowCells(p *AgentsPanel) []string {
	var cells []string
	for _, row := range p.table.Rows() {
		cells = append(cells, row...)
	}
	return cells
}

func rosterHasCell(p *AgentsPanel, want string) bool {
	for _, cell := range rosterRowCells(p) {
		if strings.TrimSpace(cell) == want {
			return true
		}
	}
	return false
}

// C1/C2 T1 (render half): the agents panel must surface the unified roster's
// type and active columns and render one cell per C1 type value.
//
// RED today: AgentSummary has no Type/ActiveCount mapping and the table has
// no "type"/"active" columns, so neither the headers nor the type/active
// cells appear. This is an assertion failure, not a compile error.
func TestAgentsRoster_TypeAndActiveColumnsVisible(t *testing.T) {
	p := newRosterPanel(t, &rosterFakeRPC{})
	out := p.View()

	// Column headers from leaf 02's contract.
	for _, header := range []string{"type", "active"} {
		if !strings.Contains(out, header) {
			t.Errorf("agents panel View() is missing the %q column header\n%s", header, out)
		}
	}

	// One rendered cell per C1 type value ("agent" | "bot" | "employee").
	// Checking exact cells avoids the false positive from the panel title,
	// which already contains the substring "agent".
	for _, typ := range []string{"agent", "bot", "employee"} {
		if !rosterHasCell(p, typ) {
			t.Errorf("agents panel rendered no %q type cell; rows:%v\n%s", typ, rosterRowCells(p), out)
		}
	}

	// The active_count values must be rendered too (exact cells: 13/27/41).
	for _, count := range []string{"13", "27", "41"} {
		if !rosterHasCell(p, count) {
			t.Errorf("agents panel rendered no active-count cell %q; rows:%v\n%s", count, rosterRowCells(p), out)
		}
	}
}

// The two type values that are not substrings of any existing panel text must
// also be visible in View() itself, confirming values are on screen, not just
// cached in the model's table.
func TestAgentsRoster_ViewShowsDistinctTypeValues(t *testing.T) {
	p := newRosterPanel(t, &rosterFakeRPC{})
	out := p.View()
	for _, typ := range []string{"bot", "employee"} {
		if !strings.Contains(out, typ) {
			t.Errorf("agents panel View() does not show the %q type value\n%s", typ, out)
		}
	}
}
