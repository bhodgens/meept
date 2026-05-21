package menus

import (
	"fmt"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/nsf/termbox-go"
)

// SessionMenu provides session management menu functionality.
type SessionMenu struct {
	modal      *Modal
	client     transport.Client
	sessionMgr *sharedclient.SessionManager
	sessions   []types.Session
	currentID  string
	onSwitch   func(*types.Session)
	onCreate   func(string)
	onDelete   func(string)
	onDismiss  func()
}

// NewSessionMenu creates a new session management menu.
func NewSessionMenu(client transport.Client, sessionMgr *sharedclient.SessionManager) *SessionMenu {
	m := &SessionMenu{
		client:     client,
		sessionMgr: sessionMgr,
	}

	items := []ModalItem{
		{Key: "1", Label: "list sessions", Hint: "show all sessions"},
		{Key: "2", Label: "create new", Hint: "create session"},
		{Key: "3", Label: "switch", Hint: "switch session"},
		{Key: "4", Label: "delete", Hint: "delete session"},
		{Key: "s", Label: "switch (quick)", Hint: "quick switch"},
	}

	m.modal = NewModal("sessions", items)
	m.modal.onSelect = m.handleSelect

	return m
}

// Show displays the session menu.
func (m *SessionMenu) Show() {
	// Load sessions
	resp, err := m.client.ListSessions()
	if err == nil {
		m.sessions = resp.Sessions
	}

	// Update items with session list
	if len(m.sessions) > 0 {
		items := []ModalItem{
			{Key: "s", Label: "switch (quick)", Hint: "quick switch"},
		}
		for i, s := range m.sessions {
			if i >= 9 {
				break
			}
			key := fmt.Sprintf("%d", i+1)
			name := s.Name
			if s.Description != "" {
				name = s.Description
			}
			items = append(items, ModalItem{Key: key, Label: name})
		}
		items = append(items, ModalItem{Key: "c", Label: "create new"})
		m.modal.items = items
		m.modal.height = len(items) + 4
	}

	if m.sessionMgr != nil {
		if s := m.sessionMgr.GetCurrentSession(); s != nil {
			m.currentID = s.ID
		}
	}

	m.modal.Show()
}

// Hide dismisses the session menu.
func (m *SessionMenu) Hide() {
	m.modal.Hide()
}

// IsVisible returns whether the menu is visible.
func (m *SessionMenu) IsVisible() bool {
	return m.modal.IsVisible()
}

// HandleKey processes keyboard input.
func (m *SessionMenu) HandleKey(ch rune, key termbox.Key) bool {
	return m.modal.HandleKey(ch, key)
}

// Render draws the menu.
func (m *SessionMenu) Render() {
	m.modal.Render()
}

func (m *SessionMenu) handleSelect(idx int) {
	if idx >= len(m.modal.items) {
		return
	}

	item := m.modal.items[idx]

	switch item.Key {
	case "s":
		// Quick switch - cycle through sessions
		if len(m.sessions) > 0 {
			nextIdx := 0
			for i, sess := range m.sessions {
				if sess.ID == m.currentID {
					nextIdx = (i + 1) % len(m.sessions)
					break
				}
			}
			if m.onSwitch != nil {
				m.onSwitch(&m.sessions[nextIdx])
			}
		}
		m.modal.Hide()

	case "c":
		// Create new - would need input, for now just trigger callback
		if m.onCreate != nil {
			m.onCreate("new-session")
		}
		m.modal.Hide()

	default:
		// Numeric key - switch to that session
		for i, sess := range m.sessions {
			if fmt.Sprintf("%d", i+1) == item.Key {
				if m.onSwitch != nil {
					m.onSwitch(&sess)
				}
				m.modal.Hide()
				return
			}
		}
	}
}

// SetCallbacks sets the callback functions.
func (m *SessionMenu) SetCallbacks(onSwitch func(*types.Session), onCreate func(string), onDelete func(string), onDismiss func()) {
	m.onSwitch = onSwitch
	m.onCreate = onCreate
	m.onDelete = onDelete
	m.onDismiss = onDismiss
}
