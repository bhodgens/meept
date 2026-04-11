// Package lite provides a lightweight terminal UI for meept.
package lite

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines the key bindings for meept-lite.
type KeyMap struct {
	// Send submits the current input message.
	Send key.Binding

	// NewLine inserts a newline in multiline input mode.
	NewLine key.Binding

	// Interrupt stops the current operation (double-press to quit).
	Interrupt key.Binding

	// Quit exits the application.
	Quit key.Binding

	// Menu opens the command menu (Ctrl+X).
	Menu key.Binding

	// Tab autocompletes slash commands.
	Tab key.Binding

	// WordLeft moves cursor one word left.
	WordLeft key.Binding

	// WordRight moves cursor one word right.
	WordRight key.Binding

	// LineStart moves cursor to start of line.
	LineStart key.Binding

	// LineEnd moves cursor to end of line.
	LineEnd key.Binding

	// DeleteEnd deletes from cursor to end of line.
	DeleteEnd key.Binding

	// DeleteStart deletes from cursor to start of line.
	DeleteStart key.Binding

	// HistoryUp navigates to previous input history entry.
	HistoryUp key.Binding

	// HistoryDown navigates to next input history entry.
	HistoryDown key.Binding

	// PageUp scrolls the output viewport up.
	PageUp key.Binding

	// PageDown scrolls the output viewport down.
	PageDown key.Binding

	// ScrollUp scrolls the output viewport up by one line.
	ScrollUp key.Binding

	// ScrollDown scrolls the output viewport down by one line.
	ScrollDown key.Binding
}

// DefaultKeyMap returns the default key bindings for meept-lite.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Send: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		NewLine: key.NewBinding(
			key.WithKeys("alt+enter", "ctrl+j"),
			key.WithHelp("alt+enter", "newline"),
		),
		Interrupt: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "interrupt"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "exit"),
		),
		Menu: key.NewBinding(
			key.WithKeys("ctrl+x"),
			key.WithHelp("ctrl+x", "menu"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "complete"),
		),
		WordLeft: key.NewBinding(
			key.WithKeys("ctrl+left", "alt+b"),
			key.WithHelp("ctrl+left", "word left"),
		),
		WordRight: key.NewBinding(
			key.WithKeys("ctrl+right", "alt+f"),
			key.WithHelp("ctrl+right", "word right"),
		),
		LineStart: key.NewBinding(
			key.WithKeys("ctrl+a", "home"),
			key.WithHelp("ctrl+a", "start of line"),
		),
		LineEnd: key.NewBinding(
			key.WithKeys("ctrl+e", "end"),
			key.WithHelp("ctrl+e", "end of line"),
		),
		DeleteEnd: key.NewBinding(
			key.WithKeys("ctrl+k"),
			key.WithHelp("ctrl+k", "delete to end"),
		),
		DeleteStart: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("ctrl+u", "delete to start"),
		),
		HistoryUp: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("up", "history prev"),
		),
		HistoryDown: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("down", "history next"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdown", "page down"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("ctrl+n", "scroll down"),
		),
	}
}

// ShortHelp returns key bindings for short help display.
// Implements the key.Map interface.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Send,
		k.NewLine,
		k.Menu,
		k.Interrupt,
		k.Quit,
	}
}

// FullHelp returns key bindings for full help display.
// Implements the key.Map interface.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		// input controls
		{k.Send, k.NewLine, k.Tab},
		// navigation
		{k.WordLeft, k.WordRight, k.LineStart, k.LineEnd},
		// editing
		{k.DeleteStart, k.DeleteEnd},
		// history
		{k.HistoryUp, k.HistoryDown},
		// scrolling
		{k.PageUp, k.PageDown, k.ScrollUp, k.ScrollDown},
		// system
		{k.Menu, k.Interrupt, k.Quit},
	}
}
