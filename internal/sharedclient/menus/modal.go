// Package menus provides termbox-based modal menus for meept-lite.
package menus

import (
	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/nsf/termbox-go"
)

// Modal represents a popup modal menu.
type Modal struct {
	title     string
	items     []ModalItem
	selected  int
	width     int
	height    int
	x         int
	y         int
	visible   bool
	onSelect  func(int)
	onDismiss func()
}

// ModalItem represents a single item in a modal menu.
type ModalItem struct {
	Key   string // e.g., "1", "2", "s", "t"
	Label string
	Hint  string
}

// NewModal creates a new modal menu.
func NewModal(title string, items []ModalItem) *Modal {
	maxWidth := len(title) + 4
	for _, item := range items {
		lineLen := 4 + len(item.Key) + 2 + len(item.Label)
		if item.Hint != "" {
			lineLen += 4 + len(item.Hint)
		}
		if lineLen > maxWidth {
			maxWidth = lineLen
		}
	}

	return &Modal{
		title:    title,
		items:    items,
		width:    maxWidth,
		height:   len(items) + 4,
		selected: 0,
	}
}

// SetPosition sets the modal position (centered by default).
func (m *Modal) SetPosition(x, y int) {
	m.x = x
	m.y = y
}

// Center positions the modal in the terminal.
func (m *Modal) Center(termWidth, termHeight int) {
	m.x = (termWidth - m.width) / 2
	m.y = (termHeight - m.height) / 2
	if m.x < 0 {
		m.x = 0
	}
	if m.y < 0 {
		m.y = 0
	}
}

// Show makes the modal visible.
func (m *Modal) Show() {
	m.visible = true
}

// Hide dismisses the modal.
func (m *Modal) Hide() {
	m.visible = false
	if m.onDismiss != nil {
		m.onDismiss()
	}
}

// IsVisible returns whether the modal is visible.
func (m *Modal) IsVisible() bool {
	return m.visible
}

// SelectedIndex returns the currently selected item index.
func (m *Modal) SelectedIndex() int {
	return m.selected
}

// SetSelected sets the selected item index.
func (m *Modal) SetSelected(idx int) {
	if idx >= 0 && idx < len(m.items) {
		m.selected = idx
	}
}

// Up moves selection up.
func (m *Modal) Up() {
	if m.selected > 0 {
		m.selected--
	}
}

// Down moves selection down.
func (m *Modal) Down() {
	if m.selected < len(m.items)-1 {
		m.selected++
	}
}

// Select triggers the currently selected item.
func (m *Modal) Select() bool {
	if m.selected >= 0 && m.selected < len(m.items) && m.onSelect != nil {
		m.onSelect(m.selected)
		return true
	}
	return false
}

// HandleKey processes a key press and returns true if handled.
func (m *Modal) HandleKey(ch rune, key termbox.Key) bool {
	if !m.visible {
		return false
	}

	// Navigation
	switch key {
	case termbox.KeyArrowUp, termbox.KeyCtrlP:
		m.Up()
		return true
	case termbox.KeyArrowDown, termbox.KeyCtrlN:
		m.Down()
		return true
	case termbox.KeyEnter:
		m.Select()
		return true
	case termbox.KeyEsc, termbox.KeyCtrlC, termbox.KeyCtrlG:
		m.Hide()
		return true
	}

	// Key shortcuts (1-9, a-z)
	if ch != 0 {
		for i, item := range m.items {
			if item.Key == string(ch) {
				m.selected = i
				m.Select()
				return true
			}
		}
	}

	return false
}

// Render draws the modal to the terminal.
func (m *Modal) Render() {
	if !m.visible {
		return
	}

	width, height := termbox.Size()

	// Ensure modal fits
	if m.width > width-2 {
		m.width = width - 2
	}
	if m.height > height-2 {
		m.height = height - 2
	}

	// Re-center if needed
	m.Center(width, height)

	// Draw background
	for dy := 0; dy < m.height; dy++ {
		for dx := 0; dx < m.width; dx++ {
			ch := ' '
			fg := termbox.Attribute(sharedclient.ColorWhite)
			bg := termbox.ColorDefault

			// Borders
			if dy == 0 || dy == m.height-1 {
				if dx == 0 || dx == m.width-1 {
					ch = '+'
				} else {
					ch = '-'
				}
				fg = termbox.Attribute(sharedclient.ColorMuted)
			} else if dx == 0 || dx == m.width-1 {
				ch = '|'
				fg = termbox.Attribute(sharedclient.ColorMuted)
			}

			termbox.SetCell(m.x+dx, m.y+dy, ch, fg, bg)
		}
	}

	// Title
	title := " " + m.title + " "
	for i, r := range title {
		if i >= m.width-2 {
			break
		}
		attr := termbox.Attribute(sharedclient.ColorOrange) | termbox.AttrBold
		termbox.SetCell(m.x+1+i, m.y, r, attr, termbox.ColorDefault)
	}

	// Items
	for i, item := range m.items {
		y := m.y + 2 + i
		if y >= m.y+m.height-1 {
			break
		}

		x := m.x + 2

		// Selection marker
		marker := " "
		fg := termbox.Attribute(sharedclient.ColorWhite)
		if i == m.selected {
			marker = ">"
			fg = termbox.Attribute(sharedclient.ColorOrange) | termbox.AttrBold
		}
		termbox.SetCell(x, y, rune(marker[0]), fg, termbox.ColorDefault)
		x += 2

		// Key shortcut
		keyStr := "[" + item.Key + "]"
		for _, r := range keyStr {
			termbox.SetCell(x, y, r, termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
			x++
		}
		x++

		// Label
		for _, r := range item.Label {
			if x >= m.x+m.width-2 {
				break
			}
			termbox.SetCell(x, y, r, fg, termbox.ColorDefault)
			x++
		}

		// Hint (right aligned)
		if item.Hint != "" {
			hintX := m.x + m.width - len(item.Hint) - 2
			for _, r := range item.Hint {
				if hintX <= x+1 {
					break
				}
				termbox.SetCell(hintX, y, r, termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
				hintX++
			}
		}
	}

	// Footer hint
	hint := " ↑/↓=nav enter=select esc=cancel "
	hintX := m.x + (m.width-len(hint))/2
	if hintX < m.x+2 {
		hintX = m.x + 2
	}
	y := m.y + m.height - 2
	for i, r := range hint {
		if hintX+i >= m.x+m.width-2 {
			break
		}
		termbox.SetCell(hintX+i, y, r, termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
	}
}
