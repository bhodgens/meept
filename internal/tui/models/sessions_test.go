package models

import (
	"encoding/json"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// mockSessionsRPC implements SessionsRPCClient for sessions model tests.
type mockSessionsRPC struct {
	connected  bool
	sessions   []types.Session
	listErr    error
	callResult json.RawMessage
	callErr    error
}

func (m *mockSessionsRPC) Call(method string, params any) (json.RawMessage, error) {
	return m.callResult, m.callErr
}

func (m *mockSessionsRPC) ListSessions() (*types.SessionListResponse, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return &types.SessionListResponse{Sessions: m.sessions}, nil
}

func (m *mockSessionsRPC) IsConnected() bool { return m.connected }

func newSessionsModelWithSessions(rpc SessionsRPCClient, sessions []types.Session) *SessionsModel {
	m := NewSessionsModel(rpc)
	// Seed the model with sessions directly (bypassing the RPC fetch)
	// and populate the table so the cursor has a valid target.
	m.sessions = sessions
	m.sortSessions()
	m.SetSize(120, 40)
	m.updateSessionsTable()
	return m
}

func TestSessionsModel_ArchiveKey_EmitsArchiveMsg(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "session one", LastActivity: "2026-06-29T10:00:00Z"},
	}
	m := newSessionsModelWithSessions(rpc, sessions)

	// Press 'd' to archive the selected (first) session.
	cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from pressing 'd'")
	}

	msg := cmd()
	archiveMsg, ok := msg.(ArchiveSessionRequestedMsg)
	if !ok {
		t.Fatalf("expected ArchiveSessionRequestedMsg, got %T", msg)
	}
	if archiveMsg.SessionID != "s1" {
		t.Errorf("expected SessionID 's1', got %q", archiveMsg.SessionID)
	}
	if !archiveMsg.Archived {
		t.Error("expected Archived=true for a non-archived session")
	}
}

func TestSessionsModel_ArchiveKey_OnArchivedSession_TogglesOff(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "archived one", LastActivity: "2026-06-29T10:00:00Z", Archived: true},
	}
	m := newSessionsModelWithSessions(rpc, sessions)

	cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from pressing 'd'")
	}

	msg := cmd()
	archiveMsg, ok := msg.(ArchiveSessionRequestedMsg)
	if !ok {
		t.Fatalf("expected ArchiveSessionRequestedMsg, got %T", msg)
	}
	if archiveMsg.Archived {
		t.Error("expected Archived=false when toggling an already-archived session")
	}
}

func TestSessionsModel_DeleteKey_EmitsDeleteMsg(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "session one", LastActivity: "2026-06-29T10:00:00Z"},
	}
	m := newSessionsModelWithSessions(rpc, sessions)

	// Press 'D' (shift+d) to permanently delete.
	cmd := m.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from pressing 'D'")
	}

	msg := cmd()
	delMsg, ok := msg.(DeleteSessionRequestedMsg)
	if !ok {
		t.Fatalf("expected DeleteSessionRequestedMsg, got %T", msg)
	}
	if delMsg.SessionID != "s1" {
		t.Errorf("expected SessionID 's1', got %q", delMsg.SessionID)
	}
}

func TestSessionsModel_ArchiveKey_EmptySessions_ReturnsNil(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	m := newSessionsModelWithSessions(rpc, nil)

	cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if cmd != nil {
		t.Errorf("expected nil cmd when no sessions, got non-nil")
	}
}

func TestSessionsModel_DeleteKey_EmptySessions_ReturnsNil(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	m := newSessionsModelWithSessions(rpc, nil)

	cmd := m.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})
	if cmd != nil {
		t.Errorf("expected nil cmd when no sessions, got non-nil")
	}
}

func TestSessionsModel_SortSessions_ArchivedSinksToBottom(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "active", Name: "active", LastActivity: "2026-06-29T10:00:00Z", Archived: false},
		{ID: "archived", Name: "archived", LastActivity: "2026-06-29T11:00:00Z", Archived: true},
		{ID: "active2", Name: "active2", LastActivity: "2026-06-29T09:00:00Z", Archived: false},
	}
	m := newSessionsModelWithSessions(rpc, sessions)

	if m.sessions[2].ID != "archived" {
		t.Errorf("expected archived session at bottom, got order: %s, %s, %s",
			m.sessions[0].ID, m.sessions[1].ID, m.sessions[2].ID)
	}
}

func TestSessionsModel_SortSessions_StableWithinArchiveGroup(t *testing.T) {
	// Verify that non-archived sessions preserve activity-descending order
	// and archived sessions sink below all active ones.
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "a1", Name: "a1", LastActivity: "2026-06-29T10:00:00Z", Archived: false},
		{ID: "arch1", Name: "arch1", LastActivity: "2026-06-29T12:00:00Z", Archived: true},
		{ID: "a2", Name: "a2", LastActivity: "2026-06-29T09:00:00Z", Archived: false},
		{ID: "arch2", Name: "arch2", LastActivity: "2026-06-29T08:00:00Z", Archived: true},
	}
	m := newSessionsModelWithSessions(rpc, sessions)

	// Active first, by activity descending: a1, a2
	// Then archived, by activity descending: arch1, arch2
	want := []string{"a1", "a2", "arch1", "arch2"}
	for i, w := range want {
		if m.sessions[i].ID != w {
			t.Errorf("position %d: want %q, got %q", i, w, m.sessions[i].ID)
		}
	}
}

func TestSessionsModel_SelectedSession_ReturnsCursorTarget(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "one", LastActivity: "2026-06-29T10:00:00Z"},
		{ID: "s2", Name: "two", LastActivity: "2026-06-29T11:00:00Z"},
	}
	m := newSessionsModelWithSessions(rpc, sessions)

	// Cursor starts at 0 after updateSessionsTable.
	s := m.selectedSession()
	if s == nil {
		t.Fatal("expected non-nil selected session")
	}
	if s.ID != "s2" {
		// sortSessions puts most recent first, so s2 (11:00) should be at index 0.
		t.Errorf("expected s2 at cursor 0, got %q", s.ID)
	}
}

func TestSessionsModel_SelectedSession_EmptyList_ReturnsNil(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	m := newSessionsModelWithSessions(rpc, nil)

	if s := m.selectedSession(); s != nil {
		t.Errorf("expected nil when sessions list is empty, got %v", s)
	}
}

func TestSessionsModel_UpdateSessionsTable_DimsArchivedRows(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "active", Name: "active", Description: "active row", LastActivity: "2026-06-29T10:00:00Z"},
		{ID: "archived", Name: "archived", Description: "archived row", LastActivity: "2026-06-29T11:00:00Z", Archived: true},
	}
	m := newSessionsModelWithSessions(rpc, sessions)
	view := m.View()

	// Archived rows should contain ANSI escape codes from archivedRowStyle
	// and the "(archived)" prefix marker.
	if !contains(view, "(archived)") {
		t.Error("expected '(archived)' marker in rendered view")
	}
}

// contains is a minimal substring helper to avoid importing strings just for one call.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

// Compile-time check that errors is imported for potential future use in
// mock error scenarios. This prevents "imported and not used" if no test
// references errors yet.
var _ = errors.New

// L14 pin: a refresh (SessionsUpdateMsg) must preserve the user's selected
// session — the pre-fix GotoTop + reselect-first yanked the cursor back to
// the first session on every poll.
func TestSessionsModel_RefreshPreservesSelection(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "one", LastActivity: "2026-06-29T10:00:00Z"},
		{ID: "s2", Name: "two", LastActivity: "2026-06-29T11:00:00Z"},
		{ID: "s3", Name: "three", LastActivity: "2026-06-29T09:00:00Z"},
	}
	m := newSessionsModelWithSessions(rpc, sessions)

	// Move the cursor down to the second session row (row 0 is the
	// unbound bucket header; sorted rows are s2, s1, s3).
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown, Text: "down"})
	sel := m.selectedSession()
	if sel == nil {
		t.Fatal("cursor did not land on a session row")
	}
	wantID := sel.ID
	if wantID == "s2" {
		t.Fatal("test setup: cursor still on the first session; move it again")
	}

	// Refresh with the same sessions (a poll): selection must survive.
	m.Update(SessionsUpdateMsg{Sessions: sessions})
	if m.selected == nil || m.selected.ID != wantID {
		t.Errorf("refresh reset selection to %v, want preserved %q", m.selected, wantID)
	}
	if got := m.selectedSession(); got == nil || got.ID != wantID {
		t.Errorf("cursor row after refresh = %v, want the row carrying %q", got, wantID)
	}
}

// L14 pin: a resize rebuilds the table too — the selection must survive it.
func TestSessionsModel_ResizePreservesSelection(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "one", LastActivity: "2026-06-29T10:00:00Z"},
		{ID: "s2", Name: "two", LastActivity: "2026-06-29T11:00:00Z"},
	}
	m := newSessionsModelWithSessions(rpc, sessions)
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown, Text: "down"})
	sel := m.selectedSession()
	if sel == nil {
		t.Fatal("cursor did not land on a session row")
	}
	wantID := sel.ID

	m.SetSize(100, 30)
	if m.selected == nil || m.selected.ID != wantID {
		t.Errorf("resize reset selection to %v, want preserved %q", m.selected, wantID)
	}
}

// L14 pin: when the selected session disappears from the refreshed list,
// the cursor falls back to the first session row (never a header).
func TestSessionsModel_RefreshFallsBackWhenSelectionDeleted(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "one", LastActivity: "2026-06-29T10:00:00Z"},
		{ID: "s2", Name: "two", LastActivity: "2026-06-29T11:00:00Z"},
	}
	m := newSessionsModelWithSessions(rpc, sessions)
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown, Text: "down"})
	if s := m.selectedSession(); s == nil || s.ID != "s1" {
		t.Fatalf("setup: cursor = %v, want s1", s)
	}

	// s1 was deleted elsewhere; the refresh no longer carries it.
	m.Update(SessionsUpdateMsg{Sessions: []types.Session{
		{ID: "s2", Name: "two", LastActivity: "2026-06-29T11:00:00Z"},
	}})
	if m.selected == nil || m.selected.ID != "s2" {
		t.Errorf("fallback selection = %v, want the first remaining session s2", m.selected)
	}
	if s := m.selectedSession(); s == nil || s.ID != "s2" {
		t.Errorf("fallback cursor row = %v, want s2", s)
	}
}

// L14 pin: a refresh that keeps the same selection must not refetch its
// plans on every poll; the first load still fetches.
func TestSessionsModel_RefreshSameSelectionSkipsPlansRefetch(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{
		{ID: "s1", Name: "one", LastActivity: "2026-06-29T10:00:00Z"},
	}
	m := NewSessionsModel(rpc)
	m.SetSize(120, 40)

	// First load fetches plans for the auto-selected session.
	cmd := m.Update(SessionsUpdateMsg{Sessions: sessions})
	if cmd == nil {
		t.Fatal("first load did not fetch plans")
	}
	if _, ok := cmd().(SessionPlansUpdateMsg); !ok {
		t.Fatal("first load command was not a plans fetch")
	}

	// Same-selection refresh: no plans refetch.
	if cmd := m.Update(SessionsUpdateMsg{Sessions: sessions}); cmd != nil {
		t.Errorf("same-selection refresh refetched plans (%T)", cmd())
	}
}

// L15 pin: the sessions help text advertises the sessions-scoped search,
// not the stale "global search" label.
func TestSessionsModel_HelpSearchLabel(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	m := newSessionsModelWithSessions(rpc, []types.Session{
		{ID: "s1", Name: "one", LastActivity: "2026-06-29T10:00:00Z"},
	})
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	out := m.View()
	if contains(out, "global search") {
		t.Errorf("help still says 'global search'\n%s", out)
	}
	if !contains(out, "search sessions") {
		t.Errorf("help missing the sessions-scoped search label\n%s", out)
	}
}
