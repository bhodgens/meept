package menus

import (
	"github.com/nsf/termbox-go"
)

// ChatMenu provides chat-related menu functionality.
type ChatMenu struct {
	modal     *Modal
	onNew     func()
	onClear   func()
	onDismiss func()
}

// NewChatMenu creates a new chat menu.
func NewChatMenu() *ChatMenu {
	m := &ChatMenu{}

	items := []ModalItem{
		{Key: "n", Label: "new conversation", Hint: "clear+new session"},
		{Key: "c", Label: "clear scrollback", Hint: "keep session"},
		{Key: "u", Label: "undo last exchange", Hint: "remove last"},
	}

	m.modal = NewModal("chat", items)
	m.modal.onSelect = m.handleSelect

	return m
}

// Show displays the chat menu.
func (m *ChatMenu) Show() {
	m.modal.Show()
}

// Hide dismisses the chat menu.
func (m *ChatMenu) Hide() {
	m.modal.Hide()
}

// IsVisible returns whether the menu is visible.
func (m *ChatMenu) IsVisible() bool {
	return m.modal.IsVisible()
}

// HandleKey processes keyboard input.
func (m *ChatMenu) HandleKey(ch rune, key termbox.Key) bool {
	return m.modal.HandleKey(ch, key)
}

// Render draws the menu.
func (m *ChatMenu) Render() {
	m.modal.Render()
}

func (m *ChatMenu) handleSelect(idx int) {
	if idx >= len(m.modal.items) {
		return
	}

	item := m.modal.items[idx]

	switch item.Key {
	case "n":
		if m.onNew != nil {
			m.onNew()
		}
		m.modal.Hide()

	case "c":
		if m.onClear != nil {
			m.onClear()
		}
		m.modal.Hide()

	case "u":
		// Undo handled elsewhere
		m.modal.Hide()
	}
}

// SetCallbacks sets the callback functions.
func (m *ChatMenu) SetCallbacks(onNew func(), onClear func(), onDismiss func()) {
	m.onNew = onNew
	m.onClear = onClear
	m.onDismiss = onDismiss
}
