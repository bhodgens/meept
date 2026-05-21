package menus

import (
	"fmt"

	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/nsf/termbox-go"
)

// MemoryMenu provides memory search/viewing menu functionality.
type MemoryMenu struct {
	modal      *Modal
	client     transport.Client
	memories   []types.MemoryItem
	onSelect   func(*types.MemoryItem)
	onSearch   func(string)
	onDismiss  func()
}

// NewMemoryMenu creates a new memory menu.
func NewMemoryMenu(client transport.Client) *MemoryMenu {
	m := &MemoryMenu{client: client}

	items := []ModalItem{
		{Key: "r", Label: "recent memories", Hint: "last 10"},
		{Key: "s", Label: "search", Hint: "query"},
		{Key: "e", Label: "episodic", Hint: "episodes"},
		{Key: "t", Label: "task memories", Hint: "tasks"},
	}

	m.modal = NewModal("memory", items)
	m.modal.onSelect = m.handleSelect

	return m
}

// Show displays the memory menu and loads recent memories.
func (m *MemoryMenu) Show() {
	// Load recent memories
	resp, err := m.client.GetRecentMemories(10)
	if err == nil {
		m.memories = resp.Memories

		items := []ModalItem{
			{Key: "r", Label: "recent", Hint: fmt.Sprintf("%d items", len(m.memories))},
			{Key: "s", Label: "search memory", Hint: "query"},
		}

		for i, mem := range m.memories {
			if i >= 7 {
				break
			}
			key := fmt.Sprintf("%d", i+1)
			content := mem.Content
			if len(content) > 30 {
				content = content[:27] + "..."
			}

			items = append(items, ModalItem{
				Key:   key,
				Label: content,
				Hint:  mem.Category,
			})
		}

		m.modal.items = items
		m.modal.height = len(items) + 4
	}

	m.modal.Show()
}

// Hide dismisses the memory menu.
func (m *MemoryMenu) Hide() {
	m.modal.Hide()
}

// IsVisible returns whether the menu is visible.
func (m *MemoryMenu) IsVisible() bool {
	return m.modal.IsVisible()
}

// HandleKey processes keyboard input.
func (m *MemoryMenu) HandleKey(ch rune, key termbox.Key) bool {
	return m.modal.HandleKey(ch, key)
}

// Render draws the menu.
func (m *MemoryMenu) Render() {
	m.modal.Render()
}

func (m *MemoryMenu) handleSelect(idx int) {
	if idx >= len(m.modal.items) {
		return
	}

	item := m.modal.items[idx]

	switch item.Key {
	case "r", "s", "e", "t":
		// Action selection - trigger search callback for 's'
		if item.Key == "s" && m.onSearch != nil {
			m.onSearch("")
		}
		m.modal.Hide()

	default:
		// Memory selection
		for i := range m.memories {
			if fmt.Sprintf("%d", i+1) == item.Key {
				if m.onSelect != nil {
					m.onSelect(&m.memories[i])
				}
				m.modal.Hide()
				return
			}
		}
	}
}

// SetCallbacks sets the callback functions.
func (m *MemoryMenu) SetCallbacks(onSelect func(*types.MemoryItem), onSearch func(string), onDismiss func()) {
	m.onSelect = onSelect
	m.onSearch = onSearch
	m.onDismiss = onDismiss
}
