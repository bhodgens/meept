package models

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// C2 tree tests: sessions are grouped into project buckets using
//   bucketFor(s) = ProjectID || ProjectPath || "unbound"
// and left/right arrows collapse/expand a group's children.
//
// These tests reuse the existing mockSessionsRPC + newSessionsModelWithSessions
// helpers from sessions_test.go and assert only on model-level output (View()
// and the rendered table rows), so they compile against today's symbols and
// fail RED as assertion failures rather than compile errors.

func rightArrow() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyRight, Text: "right"} }
func leftArrow() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyLeft, Text: "left"} }

// T1: with sessions across two projects plus an unbound one, View() must
// render a header row for every C2 bucket. The chosen session names contain
// no project text, so the only way these bucket names appear is via the
// tree's group headers.
//
// RED today: updateSessionsTable emits a flat title/created/activity table
// with no group headers, so "proj-alpha"/"proj-beta"/"unbound" are absent.
func TestSessionTree_GroupHeadersVisible(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "chat one", ProjectID: "proj-alpha"},
		{ID: "s2", Name: "chat two", ProjectID: "proj-beta"},
		{ID: "s3", Name: "chat three"}, // no project -> unbound
	}
	m := newSessionsModelWithSessions(rpc, sessions)
	out := m.View()

	for _, header := range []string{"proj-alpha", "proj-beta", "unbound"} {
		if !strings.Contains(out, header) {
			t.Errorf("sessions View() is missing the project group header %q\n%s", header, out)
		}
	}
}

// T1 variant: a session carrying only ProjectPath (no ProjectID) must be
// bucketed under that path, per the C2 precedence (ID, then path, else
// unbound).
func TestSessionTree_ProjectPathFallbackBucket(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "path session", ProjectPath: "/work/pathproj"},
	}
	m := newSessionsModelWithSessions(rpc, sessions)
	out := m.View()

	if !strings.Contains(out, "pathproj") {
		t.Errorf("sessions View() is missing the ProjectPath fallback bucket\n%s", out)
	}
}

func treeRowText(m *SessionsModel) string {
	var b strings.Builder
	for _, row := range m.table.Rows() {
		for _, cell := range row {
			b.WriteString(cell)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// T2: right arrow expands a group so its child sessions show; left arrow
// collapses it so they are hidden again. The child's marker appears nowhere
// else (the auto-selected session is the "keeper" sibling), so its presence
// in the rendered rows is an unambiguous signal of expansion.
//
// RED today: left/right arrows fall through to bubbles/table and do nothing,
// so the child row stays visible after a left press.
func TestSessionTree_LeftRightToggleChildren(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s0", Name: "group-keeper", ProjectID: "proj-alpha"},
		{ID: "s1", Name: "hidden-child-marker", ProjectID: "proj-alpha"},
	}
	m := newSessionsModelWithSessions(rpc, sessions)

	// Expand the first (selected) group.
	m.Update(rightArrow())
	if got := treeRowText(m); !strings.Contains(got, "hidden-child-marker") {
		t.Fatalf("right arrow did not expand the group; rows:\n%s", got)
	}

	// Collapse it: the child must disappear from the rendered rows.
	m.Update(leftArrow())
	if got := treeRowText(m); strings.Contains(got, "hidden-child-marker") {
		t.Errorf("left arrow did not collapse the group; child row still visible:\n%s", got)
	}
}
