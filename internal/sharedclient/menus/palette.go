package menus

import (
	"strings"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/nsf/termbox-go"
)

// CommandPalette provides a unified command selection palette.
type CommandPalette struct {
	modal     *Modal
	filter    string
	commands  []ModalItem
	filtered  []ModalItem
	onSelect  func(string)
	onDismiss func()
}

// NewCommandPalette creates a new command palette.
func NewCommandPalette() *CommandPalette {
	p := &CommandPalette{
		commands: []ModalItem{
			// Sessions
			{Key: "1", Label: "session: list", Hint: "sessions"},
			{Key: "2", Label: "session: create", Hint: "sessions"},
			{Key: "3", Label: "session: switch", Hint: "sessions"},
			{Key: "4", Label: "session: delete", Hint: "sessions"},

			// Tasks
			{Key: "5", Label: "tasks: view all", Hint: "tasks"},
			{Key: "6", Label: "tasks: cancel", Hint: "tasks"},
			{Key: "7", Label: "tasks: amend", Hint: "tasks"},
			{Key: "8", Label: "tasks: interrupt", Hint: "tasks"},

			// Queue
			{Key: "9", Label: "queue: view pending", Hint: "queue"},
			{Key: "0", Label: "queue: view running", Hint: "queue"},
			{Key: "q", Label: "queue: view failed", Hint: "queue"},

			// Memory
			{Key: "w", Label: "memory: recent", Hint: "memory"},
			{Key: "e", Label: "memory: search", Hint: "memory"},

			// Chat
			{Key: "r", Label: "chat: new conversation", Hint: "chat"},
			{Key: "t", Label: "chat: clear scrollback", Hint: "chat"},
			{Key: "y", Label: "chat: undo last", Hint: "chat"},

			// System
			{Key: "u", Label: "status: daemon", Hint: "system"},
			{Key: "i", Label: "status: usage", Hint: "system"},
			{Key: "o", Label: "status: stop session", Hint: "system"},
			{Key: "p", Label: "help: show help", Hint: "system"},
		},
	}

	items := p.commands
	p.modal = NewModal("command palette", items)
	p.modal.onSelect = p.handleSelect
	p.filtered = p.commands

	return p
}

// Show displays the command palette.
func (p *CommandPalette) Show() {
	p.filter = ""
	p.filtered = p.commands
	p.modal.items = p.filtered
	p.modal.height = len(p.filtered) + 4
	p.modal.selected = 0
	p.modal.Show()
}

// Hide dismisses the command palette.
func (p *CommandPalette) Hide() {
	p.modal.Hide()
}

// IsVisible returns whether the palette is visible.
func (p *CommandPalette) IsVisible() bool {
	return p.modal.IsVisible()
}

// SetFilter filters the commands based on input.
func (p *CommandPalette) SetFilter(filter string) {
	p.filter = strings.ToLower(filter)
	p.filtered = []ModalItem{}

	for _, cmd := range p.commands {
		if strings.Contains(strings.ToLower(cmd.Label), p.filter) {
			p.filtered = append(p.filtered, cmd)
		}
	}

	p.modal.items = p.filtered
	p.modal.height = len(p.filtered) + 4
	p.modal.selected = 0
}

// HandleKey processes keyboard input.
func (p *CommandPalette) HandleKey(ch rune, key termbox.Key) bool {
	// Handle text input for filtering
	if ch != 0 && ch != '/' {
		// Check if it's a printable character
		if ch >= 32 && ch < 127 {
			p.SetFilter(p.filter + string(ch))
			return true
		}
	}

	return p.modal.HandleKey(ch, key)
}

// Render draws the palette.
func (p *CommandPalette) Render() {
	p.modal.Render()

	// Show filter input at bottom
	if p.filter != "" {
		width, _ := termbox.Size()
		filterText := "filter: " + p.filter
		x := (width - len(filterText)) / 2
		y := p.modal.y + p.modal.height - 1
		for i, r := range filterText {
			termbox.SetCell(x+i, y, r, termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
		}
	}
}

func (p *CommandPalette) handleSelect(idx int) {
	if idx >= len(p.filtered) {
		return
	}

	cmd := p.filtered[idx]
	if p.onSelect != nil {
		// Extract command ID from label
		parts := strings.Split(cmd.Label, ": ")
		if len(parts) > 0 {
			p.onSelect(strings.TrimSpace(parts[0]))
		}
	}
	p.modal.Hide()
}

// SetCallbacks sets the callback functions.
func (p *CommandPalette) SetCallbacks(onSelect func(string), onDismiss func()) {
	p.onSelect = onSelect
	p.onDismiss = onDismiss
}
