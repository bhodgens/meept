// Package main provides the meept-lite minimalistic TUI.
package main

import (
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/liteclient"
	"github.com/caimlas/meept/internal/transport"
	"github.com/nsf/termbox-go"
)

// TUI represents the meept-lite terminal interface.
type TUI struct {
	client       transport.Client
	sessionMgr   *liteclient.SessionManager
	prompt       *liteclient.PromptRenderer
	history      *liteclient.History
	autocomplete *liteclient.SlashAutocomplete

	// Input state
	inputBuffer strings.Builder
	cursorX     int

	// Scrollback
	scrollback   []string
	scrollOffset int // 0 = at bottom, >0 = scrolled up

	// Command mode state
	commandMode   bool
	commandKey    string // waiting for second key after ctrl+x

	// Running state
	quitting bool
}

// NewTUI creates a new TUI instance.
func NewTUI(client transport.Client, sessionMgr *liteclient.SessionManager) *TUI {
	prompt := liteclient.NewPromptRenderer(sessionMgr.GetSessionName())
	history := liteclient.NewHistory(1000)
	autocomplete := liteclient.NewSlashAutocomplete()

	return &TUI{
		client:       client,
		sessionMgr:   sessionMgr,
		prompt:       prompt,
		history:      history,
		autocomplete: autocomplete,
	}
}

// Run starts the TUI main loop.
func (t *TUI) Run() error {
	if err := termbox.Init(); err != nil {
		return err
	}
	defer termbox.Close()

	termbox.SetInputMode(termbox.InputAlt)
	termbox.SetOutputMode(termbox.Output256)
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)

	// Initial render
	t.render()

	for !t.quitting {
		ev := termbox.PollEvent()
		t.handleEvent(ev)
	}

	return nil
}

func (t *TUI) handleEvent(ev termbox.Event) {
	switch ev.Type {
	case termbox.EventKey:
		t.handleKeyEvent(ev)
	case termbox.EventResize:
		termbox.Sync()
		t.render()
	case termbox.EventError:
		// Ignore errors, keep running
	}
}

func (t *TUI) handleKeyEvent(ev termbox.Event) {
	// Handle escape for cancelling autocomplete or command mode
	if ev.Key == termbox.KeyEsc {
		if t.autocomplete.IsVisible() {
			t.autocomplete.Hide()
			t.render()
			return
		}
		if t.commandMode {
			t.commandMode = false
			t.render()
			return
		}
	}

	// Handle command mode (after ctrl+x)
	if t.commandMode {
		t.handleCommandModeKey(ev)
		return
	}

	// Handle autocomplete navigation
	if t.autocomplete.IsVisible() {
		switch ev.Key {
		case termbox.KeyArrowUp:
			t.autocomplete.Up()
			t.render()
			return
		case termbox.KeyArrowDown:
			t.autocomplete.Down()
			t.render()
			return
		case termbox.KeyEnter:
			if cmd, ok := t.autocomplete.Select(); ok {
				t.inputBuffer.Reset()
				t.inputBuffer.WriteString(cmd)
				t.cursorX = len(cmd)
			}
			t.autocomplete.Hide()
			t.render()
			return
		}

		// Check for text input that continues filtering
		if ev.Ch != 0 {
			input := t.inputBuffer.String()
			if strings.HasPrefix(input, "/") {
				filter := input[1:]
				t.autocomplete.SetFilter(filter)
			}
		}
	}

	// Normal input handling
	switch ev.Key {
	case termbox.KeyEnter:
		t.handleEnter()
	case termbox.KeyBackspace, termbox.KeyBackspace2:
		t.handleBackspace()
	case termbox.KeyArrowUp:
		t.handleUp()
	case termbox.KeyArrowDown:
		t.handleDown()
	case termbox.KeyCtrlC:
		t.handleCtrlC()
	case termbox.KeyCtrlX:
		t.commandMode = true
		t.render()
	case termbox.KeyCtrlG:
		t.scrollOffset = 0 // Return to bottom
		t.render()
	case termbox.KeyPgup:
		t.scrollUp()
	case termbox.KeyPgdn:
		t.scrollDown()
	case termbox.KeyHome:
		t.scrollOffset = len(t.scrollback)
		t.render()
	case termbox.KeyEnd:
		t.scrollOffset = 0
		t.render()
	case termbox.KeyArrowLeft:
		if t.cursorX > 0 {
			t.cursorX--
		}
		t.render()
	case termbox.KeyArrowRight:
		if t.cursorX < len(t.inputBuffer.String()) {
			t.cursorX++
		}
		t.render()
	case termbox.KeySpace:
		t.inputBuffer.WriteByte(' ')
		t.cursorX = len(t.inputBuffer.String())
		t.checkAutocomplete()
		t.render()
	default:
		if ev.Ch != 0 {
			t.inputBuffer.WriteRune(ev.Ch)
			t.cursorX = len(t.inputBuffer.String())
			t.checkAutocomplete()
		}
		t.render()
	}
}

func (t *TUI) handleCommandModeKey(ev termbox.Event) {
	key := string(ev.Ch)
	if ev.Key == termbox.KeyCtrlX {
		key = "ctrl+x"
	}

	switch key {
	case "s":
		t.addScrollback("[ctrl+x s] session menu - coming in Phase 4")
	case "t":
		t.addScrollback("[ctrl+x t] tasks menu - coming in Phase 4")
	case "q":
		t.addScrollback("[ctrl+x q] queue menu - coming in Phase 4")
	case "m":
		t.addScrollback("[ctrl+x m] memory menu - coming in Phase 4")
	case "c":
		t.addScrollback("[ctrl+x c] chat menu - coming in Phase 4")
	case "ctrl+x":
		t.addScrollback("[ctrl+x ctrl+x] command palette - coming in Phase 4")
	default:
		t.addScrollback(fmt.Sprintf("[unknown command mode key: %s]", key))
	}
	t.commandMode = false
	t.render()
}

func (t *TUI) handleEnter() {
	input := t.inputBuffer.String()
	if input == "" {
		return
	}

	t.history.Add(input)
	t.autocomplete.Hide()

	// Check for slash command
	if cmd := liteclient.ParseSlash(input); cmd != nil {
		t.executeSlashCommand(cmd)
	} else {
		// Regular chat message
		t.sendChatMessage(input)
	}

	t.inputBuffer.Reset()
	t.cursorX = 0
	t.render()
}

func (t *TUI) handleBackspace() {
	if t.cursorX > 0 {
		input := t.inputBuffer.String()
		if t.cursorX <= len(input) {
			t.inputBuffer.Reset()
			newInput := input[:t.cursorX-1] + input[t.cursorX:]
			t.inputBuffer.WriteString(newInput)
			t.cursorX--
			t.checkAutocomplete()
		}
	}
	t.render()
}

func (t *TUI) handleUp() {
	if t.autocomplete.IsVisible() {
		t.autocomplete.Up()
	} else {
		input := t.inputBuffer.String()
		if prev, ok := t.history.Up(input); ok {
			t.inputBuffer.Reset()
			t.inputBuffer.WriteString(prev)
			t.cursorX = len(prev)
		}
	}
	t.render()
}

func (t *TUI) handleDown() {
	if t.autocomplete.IsVisible() {
		t.autocomplete.Down()
	} else {
		input := t.inputBuffer.String()
		if next, ok := t.history.Down(input); ok {
			t.inputBuffer.Reset()
			t.inputBuffer.WriteString(next)
			t.cursorX = len(next)
		}
	}
	t.render()
}

func (t *TUI) handleCtrlC() {
	if t.inputBuffer.Len() > 0 {
		t.inputBuffer.Reset()
		t.cursorX = 0
	} else {
		t.quitting = true
	}
	t.render()
}

func (t *TUI) checkAutocomplete() {
	input := t.inputBuffer.String()
	if strings.HasPrefix(input, "/") {
		filter := input[1:]
		if filter != "" {
			t.autocomplete.Show(filter)
		} else {
			t.autocomplete.Hide()
		}
	} else {
		t.autocomplete.Hide()
	}
}

func (t *TUI) sendChatMessage(message string) {
	// Add user message to scrollback
	t.addScrollback(fmt.Sprintf("you: %s", message))
	t.render()

	// Send to daemon (in goroutine to avoid blocking UI)
	go func() {
		reply, err := t.client.Chat(message, t.sessionMgr.GetSessionName())
		if err != nil {
			t.addScrollback(fmt.Sprintf("error: %v", err))
		} else {
			t.addScrollback(fmt.Sprintf("meept: %s", reply))
		}
		t.render()
	}()
}

func (t *TUI) executeSlashCommand(cmd *liteclient.SlashCommand) {
	switch cmd.Name {
	case "help":
		t.showHelp()
	case "clear":
		t.scrollback = t.scrollback[:0]
		t.addScrollback("scrollback cleared")
	case "new":
		t.scrollback = t.scrollback[:0]
		t.addScrollback("conversation cleared")
	default:
		t.addScrollback(fmt.Sprintf("[slash command /%s not yet implemented in Phase 1]", cmd.Name))
	}
}

func (t *TUI) showHelp() {
	help := []string{
		"available commands:",
		"  /help [command]     show help for commands",
		"  /new, /clear        start fresh conversation",
		"  /retry              retry last response",
		"  /undo               remove last exchange",
		"  /usage              show token usage for session",
		"  /stop               stop current session's work",
		"  /status             show daemon health status",
		"  /session            session management",
		"  /tasks              list all tasks",
		"",
		"keyboard shortcuts:",
		"  ctrl+x  enter command mode",
		"  ctrl+c  clear input / quit",
		"  ctrl+g  return to bottom",
		"  up/down history navigation",
		"  pgup/pgdn scroll scrollback",
	}
	for _, line := range help {
		t.addScrollback(line)
	}
}

func (t *TUI) addScrollback(line string) {
	t.scrollback = append(t.scrollback, line)
	// Keep scrollback bounded to prevent memory issues
	if len(t.scrollback) > 10000 {
		t.scrollback = t.scrollback[1000:]
	}
}

func (t *TUI) scrollUp() {
	if t.scrollOffset < len(t.scrollback) {
		t.scrollOffset++
		t.render()
	}
}

func (t *TUI) scrollDown() {
	if t.scrollOffset > 0 {
		t.scrollOffset--
		t.render()
	}
}

func (t *TUI) render() {
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	width, height := termbox.Size()

	// Calculate scrollback area (everything except prompt line)
	scrollbackHeight := height - 1

	// Determine which lines to show
	startIdx := 0
	if t.scrollOffset > 0 {
		startIdx = len(t.scrollback) - scrollbackHeight + t.scrollOffset
		if startIdx < 0 {
			startIdx = 0
		}
	} else {
		// Show from the end
		if len(t.scrollback) > scrollbackHeight {
			startIdx = len(t.scrollback) - scrollbackHeight
		}
	}

	// Render scrollback
	for i := 0; i < scrollbackHeight && startIdx+i < len(t.scrollback); i++ {
		idx := startIdx + i
		line := t.scrollback[idx]
		// Truncate or pad line to width
		if len(line) > width {
			line = line[:width-3] + "..."
		}
		for x, r := range line {
			if x >= width {
				break
			}
			termbox.SetCell(x, i, r, termbox.Attribute(liteclient.ColorWhite), termbox.ColorDefault)
		}
	}

	// Show scroll indicator if scrolled up
	if t.scrollOffset > 0 {
		indicator := fmt.Sprintf("[v %d lines from bottom]", t.scrollOffset)
		x := width - len(indicator) - 1
		y := height - 2
		if t.commandMode {
			y = height - 3
		}
		if x > 0 && y >= 0 {
			for i, r := range indicator {
				termbox.SetCell(x+i, y, r, termbox.Attribute(liteclient.ColorMuted), termbox.ColorDefault)
			}
		}
	}

	// Render prompt at bottom
	promptY := height - 1
	promptX := t.prompt.Render(promptY)

	// Render input after prompt
	input := t.inputBuffer.String()
	t.prompt.RenderInput(promptX, promptY, input, t.cursorX)

	// Render autocomplete popup if visible
	if t.autocomplete.IsVisible() {
		t.renderAutocomplete()
	}

	// Render command mode indicator
	if t.commandMode {
		indicator := "COMMAND MODE: s=session, t=tasks, q=queue, m=memory, c=chat, ^x=palette"
		y := height - 2
		if t.scrollOffset > 0 {
			y = height - 3
		}
		x := 0
		for i, r := range indicator {
			if x+i >= width {
				break
			}
			termbox.SetCell(x+i, y, r, termbox.Attribute(liteclient.ColorOrange)|termbox.AttrBold, termbox.ColorDefault)
		}
	}

	termbox.Flush()
}

func (t *TUI) renderAutocomplete() {
	items, _, selectedIdx := t.autocomplete.GetVisibleItems()
	if len(items) == 0 {
		return
	}

	width, height := termbox.Size()

	// Calculate popup position (above prompt)
	boxWidth := 45
	boxHeight := len(items) + 4 // items + header + footer
	popupX := width/2 - boxWidth/2
	popupY := height - boxHeight - 2

	// Draw box background
	for y := popupY; y < popupY+boxHeight && y < height; y++ {
		for x := popupX; x < popupX+boxWidth && x < width; x++ {
			termbox.SetCell(x, y, ' ', 0, termbox.ColorDefault)
		}
	}

	// Draw border
	for x := popupX; x < popupX+boxWidth && x < width; x++ {
		termbox.SetCell(x, popupY, '-', termbox.Attribute(liteclient.ColorMuted), termbox.ColorDefault)
		termbox.SetCell(x, popupY+boxHeight-1, '-', termbox.Attribute(liteclient.ColorMuted), termbox.ColorDefault)
	}
	for y := popupY; y < popupY+boxHeight && y < height; y++ {
		termbox.SetCell(popupX, y, '|', termbox.Attribute(liteclient.ColorMuted), termbox.ColorDefault)
		termbox.SetCell(popupX+boxWidth-1, y, '|', termbox.Attribute(liteclient.ColorMuted), termbox.ColorDefault)
	}

	// Header
	header := " commands "
	for i := 0; i < len(header) && popupX+1+i < width; i++ {
		r := rune(header[i])
		attr := termbox.Attribute(liteclient.ColorOrange) | termbox.AttrBold
		termbox.SetCell(popupX+1+i, popupY, r, attr, termbox.ColorDefault)
	}

	// Items
	for i, item := range items {
		y := popupY + 2 + i
		if y >= height-1 {
			break
		}

		// Selection indicator
		marker := " "
		fg := termbox.Attribute(liteclient.ColorWhite)
		if i == selectedIdx {
			marker = ">"
			fg = termbox.Attribute(liteclient.ColorOrange) | termbox.AttrBold
		}

		termbox.SetCell(popupX+2, y, rune(marker[0]), fg, termbox.ColorDefault)

		// Command name
		xPos := popupX + 4
		for _, r := range item {
			if xPos >= popupX+boxWidth-1 {
				break
			}
			termbox.SetCell(xPos, y, r, fg, termbox.ColorDefault)
			xPos++
		}
	}

	// Footer hint
	hint := " up/down=nav enter=select esc=cancel "
	hintX := popupX + (boxWidth-len(hint))/2
	if hintX < popupX+2 {
		hintX = popupX + 2
	}
	y := popupY + boxHeight - 2
	for i := 0; i < len(hint) && hintX+i < popupX+boxWidth-2; i++ {
		r := rune(hint[i])
		termbox.SetCell(hintX+i, y, r, termbox.Attribute(liteclient.ColorMuted), termbox.ColorDefault)
	}
}
