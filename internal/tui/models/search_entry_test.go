package models

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// TestSessionSearchEntry pins the sessions-tree search entry (leaf 02/05):
// pressing 'f' on the focused sessions tree emits an OpenSearchViewMsg scoped
// to sessions. Given the tree focused; When 'f'; Then the emitted message
// requests a sessions-scoped search view.
func TestSessionSearchEntry(t *testing.T) {
	rpc := &mockSessionsRPC{connected: true}
	sessions := []types.Session{{ID: "s1", Name: "session one"}}
	m := newSessionsModelWithSessions(rpc, sessions)

	cmd := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from pressing 'f'")
	}

	msg := cmd()
	open, ok := msg.(OpenSearchViewMsg)
	if !ok {
		t.Fatalf("expected OpenSearchViewMsg, got %T", msg)
	}
	if open.Scope != "sessions" {
		t.Errorf("expected search scope %q, got %q", "sessions", open.Scope)
	}
}

// TestSearchModelOpenScope pins that Open selects the requested scope and
// that an empty scope preserves the default "all" (global) behavior.
func TestSearchModelOpenScope(t *testing.T) {
	m := NewSearchModel(nil, nil)

	m.Open("sessions")
	if got := m.scopes[m.scopeIndex]; got != "sessions" {
		t.Errorf("Open(\"sessions\") selected scope %q, want %q", got, "sessions")
	}

	m.Open("")
	if got := m.scopes[m.scopeIndex]; got != "all" {
		t.Errorf("Open(\"\") selected scope %q, want %q", got, "all")
	}
}
