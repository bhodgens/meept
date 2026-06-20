// Package main provides the meept-lite minimalistic TUI.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"syscall"

	"github.com/caimlas/meept/internal/errcls"
	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/sharedclient/menus"
	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/nsf/termbox-go"
)


// TUI represents the meept-lite terminal interface.
type TUI struct {
	client       transport.Client
	sessionMgr   *sharedclient.SessionManager
	prompt       *sharedclient.PromptRenderer
	history      *sharedclient.History
	autocomplete *sharedclient.SlashAutocomplete

	// Menus
	sessionMenu *menus.SessionMenu
	tasksMenu   *menus.TasksMenu
	queueMenu   *menus.QueueMenu
	memoryMenu  *menus.MemoryMenu
	chatMenu    *menus.ChatMenu
	cmdPalette  *menus.CommandPalette
	mcpMenu     *menus.MCPMenu
	activeMenu  interface {
		IsVisible() bool
		Hide()
		Render()
	}

	// Input state
	inputBuffer strings.Builder
	cursorX     int

	// Scrollback
	scrollback   []string
	scrollOffset int // 0 = at bottom, >0 = scrolled up

	// Command mode state
	commandMode bool

	// Bracketed paste support
	inPaste     bool
	pasteBuffer strings.Builder
	pasteSeq    strings.Builder // builds the bracket sequence [200~ or [201~
	_pasteState int             // 0=idle, 1-5=parsing, 6=inside paste

	// Running state
	quitting bool

	// Command handler (lazily initialized)
	commandHandler *CommandHandler
}

// NewTUI creates a new TUI instance.
func NewTUI(client transport.Client, sessionMgr *sharedclient.SessionManager) *TUI {
	prompt := sharedclient.NewPromptRenderer(sessionMgr.GetSessionName())
	history := sharedclient.NewHistory(1000)
	autocomplete := sharedclient.NewSlashAutocomplete()

	t := &TUI{
		client:       client,
		sessionMgr:   sessionMgr,
		prompt:       prompt,
		history:      history,
		autocomplete: autocomplete,
	}

	// Initialize menus
	t.sessionMenu = menus.NewSessionMenu(client, sessionMgr)
	t.tasksMenu = menus.NewTasksMenu(client)
	t.queueMenu = menus.NewQueueMenu(client)
	t.memoryMenu = menus.NewMemoryMenu(client)
	t.chatMenu = menus.NewChatMenu()
	t.cmdPalette = menus.NewCommandPalette()
	t.mcpMenu = menus.NewMCPMenu(client)

	// Set up menu callbacks
	t.setupMenuCallbacks()

	return t
}

// setupMenuCallbacks sets up the callback functions for menu actions.
func (t *TUI) setupMenuCallbacks() {
	// Session menu callbacks
	t.sessionMenu.SetCallbacks(
		func(sess *types.Session) {
			if sess != nil {
				t.sessionMgr.SwitchSession(context.TODO(), sess.ID)
				t.prompt.SetSessionName(t.sessionMgr.GetSessionName())
				t.addScrollback(fmt.Sprintf("switched to session: %s", t.sessionMgr.GetSessionName()))
			}
		},
		func(name string) {
			if name == "new-session" {
				t.addScrollback("use /session create <name> to create a new session")
			}
		},
		func(id string) {
			if err := t.sessionMgr.DeleteSession(context.TODO(), id); err != nil {
				t.addScrollback(fmt.Sprintf("error deleting session: %v", err))
			} else {
				t.addScrollback(fmt.Sprintf("deleted session: %s", id))
			}
		},
		func() { t.activeMenu = nil },
	)

	// Chat menu callbacks
	t.chatMenu.SetCallbacks(
		func() {
			t.scrollback = t.scrollback[:0]
			if ctx := t.client; ctx != nil {
				name := t.sessionMgr.GetSessionName() + " (copy)"
				if sess, err := ctx.CreateSession(name); err == nil {
					t.sessionMgr.SetSession(sess)
					t.prompt.SetSessionName(sess.Name)
				}
			}
			t.addScrollback("new conversation started")
		},
		func() {
			t.scrollback = t.scrollback[:0]
			t.addScrollback("scrollback cleared")
		},
		func() { t.activeMenu = nil },
	)

	// Command palette callbacks
	t.cmdPalette.SetCallbacks(
		func(cmd string) {
			t.executePaletteCommand(cmd)
		},
		func() { t.activeMenu = nil },
	)

	// Memory menu callbacks
	t.memoryMenu.SetCallbacks(
		func(mem *types.MemoryItem) {
			if mem != nil {
				t.addScrollback(fmt.Sprintf("memory: %s", mem.Content))
			}
		},
		func(query string) {
			t.addScrollback("memory search: use /memory search <query>")
		},
		func() { t.activeMenu = nil },
	)

	// Tasks menu callbacks
	t.tasksMenu.SetCallbacks(
		func(task *types.Task) {
			if task != nil {
				t.addScrollback(fmt.Sprintf("task: %s (state: %s)", task.Name, task.State))
			}
		},
		func() { t.activeMenu = nil },
	)

	// Queue menu callbacks
	t.queueMenu.SetCallbacks(
		func(job *types.QueueJob) {
			if job != nil {
				t.addScrollback(fmt.Sprintf("queue job: %s (priority: %d)", job.Type, job.Priority))
			}
		},
		func() { t.activeMenu = nil },
	)

	// MCP menu callbacks
	t.mcpMenu.SetCallbacks(func() { t.activeMenu = nil })
}

// executePaletteCommand executes a command from the command palette.
func (t *TUI) executePaletteCommand(cmd string) {
	switch cmd {
	case "session":
		t.sessionMenu.Show()
		t.activeMenu = t.sessionMenu
	case "tasks":
		t.tasksMenu.Show()
		t.activeMenu = t.tasksMenu
	case "queue":
		t.queueMenu.Show()
		t.activeMenu = t.queueMenu
	case "memory":
		t.memoryMenu.Show()
		t.activeMenu = t.memoryMenu
	case "chat":
		t.chatMenu.Show()
		t.activeMenu = t.chatMenu
	case "mcp":
		t.mcpMenu.Show()
		t.activeMenu = t.mcpMenu
	case "help":
		t.executeSlashCommand(&sharedclient.SlashCommand{Name: "help"})
	case "status":
		t.executeSlashCommand(&sharedclient.SlashCommand{Name: "status"})
	case "usage":
		t.executeSlashCommand(&sharedclient.SlashCommand{Name: "usage"})
	}
}

// Run starts the TUI main loop.
func (t *TUI) Run() error {
	if err := termbox.Init(); err != nil {
		return err
	}
	defer termbox.Close()

	// Enable bracketed paste mode
	termbox.SetInputMode(termbox.InputAlt | termbox.InputMouse)
	termbox.SetOutputMode(termbox.Output256)
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)

	// Send bracketed paste enable sequence (write to stdout)
	fmt.Print("\x1b[?2004h")

	// Initial render
	t.render()

	for !t.quitting {
		ev := termbox.PollEvent()
		t.handleEvent(ev)
	}

	return nil
}

// quit saves state before exiting.
func (t *TUI) quit() {
	// Flush any pending scrollback to "session" via session manager
	if t.sessionMgr != nil && t.sessionMgr.GetCurrentSession() != nil {
		// Update the session description with last user message or conversation summary
		lastUserMsg := ""
		for i := len(t.scrollback) - 1; i >= 0; i-- {
			line := t.scrollback[i]
			if strings.HasPrefix(line, "you: ") {
				lastUserMsg = line[5:]
				break
			}
		}
		if lastUserMsg != "" {
			if err := t.sessionMgr.UpdateSessionDescription(context.TODO(), lastUserMsg); err != nil {
				slog.Default().Warn("session description update failed", "error", err)
			}
		}
	}

	// Show shutdown message in scrollback
	t.addScrollback("")
	t.addScrollback("[session state saved] bye.")
	t.render()
	termbox.Flush()
}

func (t *TUI) handleEvent(ev termbox.Event) {
	switch ev.Type {
	case termbox.EventKey:
		t.handleKeyEvent(ev)
	case termbox.EventResize:
		termbox.Sync()
		t.render()
	case termbox.EventError:
		if t.client != nil && !t.client.IsConnected() {
			t.addScrollback("error: connection to daemon lost")
			t.render()
		}
		// Don't quit - keep running in case it reconnects
	}
}

func (t *TUI) handleKeyEvent(ev termbox.Event) {
	// --- Bracketed paste detection ---
	// In bracketed paste mode, terminals send:
	//   ESC [ 2 0 0 ~  (start) then <paste text> then ESC [ 2 0 1 ~  (end)
	// termbox-go splits these into individual events in InputAlt mode.
	// We use a simple state machine: _pasteState tracks our position
	// in the bracketed paste sequence.
	switch t._pasteState {
	case 0: // looking for paste start / end
		if ev.Type == termbox.EventKey && ev.Key == termbox.KeyEsc {
			t._pasteState = 1
			t.pasteSeq.Reset()
			return
		}
	case 1: // saw ESC, expecting "["
		if ev.Type == termbox.EventKey && ev.Ch == '[' {
			t._pasteState = 2
			return
		}
		t._pasteState = 0
		// Fall through to normal handling
	case 2: // saw "[", looking for "200" or "201"
		if ev.Type == termbox.EventKey {
			switch ev.Ch {
			case '2':
				t.pasteSeq.WriteByte('2')
				t._pasteState = 3
				return
			case '0':
				t.pasteSeq.WriteByte('0')
				t._pasteState = 3
				return
			}
		}
		t._pasteState = 0
		// Fall through
	case 3: // saw "[2", expecting "0"
		if ev.Type == termbox.EventKey && ev.Ch == '0' {
			t.pasteSeq.WriteByte('0')
			t._pasteState = 4
			return
		}
		t._pasteState = 0
		// Fall through
	case 4: // saw "[20", expecting "0" or "1"
		if ev.Type == termbox.EventKey {
			if ev.Ch == '0' {
				t.pasteSeq.WriteByte('0')
				t._pasteState = 5
				t.pasteSeq.WriteByte('0')
				return
			}
			if ev.Ch == '1' {
				t.pasteSeq.WriteByte('1')
				t._pasteState = 5
				t.pasteSeq.WriteByte('1')
				return
			}
		}
		t._pasteState = 0
		// Fall through
	case 5: // saw "[200" or "[201", expecting "~"
		if ev.Type == termbox.EventKey && ev.Ch == '~' {
			seq := t.pasteSeq.String()
			if seq == "[200~" {
				// Paste start
				t.inPaste = true
				t._pasteState = 6
				return
			}
			if seq == "[201~" {
				// Paste end
				t.inPaste = false
				t._pasteState = 0
				t.flushPaste()
				return
			}
		}
		t._pasteState = 0
		// Fall through
	case 6: // inside paste, collecting characters
		if ev.Type == termbox.EventKey && ev.Ch != 0 {
			t.pasteBuffer.WriteRune(ev.Ch)
			return
		}
		// Ignore non-character events during paste
		return
	}

	// Handle escape for cancelling autocomplete, command mode, or menus
	if ev.Key == termbox.KeyEsc {
		if t.autocomplete.IsVisible() {
			t.autocomplete.Hide()
			t.render()
			return
		}
		if t.activeMenu != nil {
			t.activeMenu.Hide()
			t.activeMenu = nil
			t.commandMode = false
			t.render()
			return
		}
		if t.commandMode {
			t.commandMode = false
			t.render()
			return
		}
	}

	// Handle active menu input
	if t.activeMenu != nil {
		switch menu := t.activeMenu.(type) {
		case *menus.SessionMenu:
			if menu.HandleKey(ev.Ch, ev.Key) {
				t.render()
				return
			}
		case *menus.TasksMenu:
			if menu.HandleKey(ev.Ch, ev.Key) {
				t.render()
				return
			}
		case *menus.QueueMenu:
			if menu.HandleKey(ev.Ch, ev.Key) {
				t.render()
				return
			}
		case *menus.MemoryMenu:
			if menu.HandleKey(ev.Ch, ev.Key) {
				t.render()
				return
			}
		case *menus.ChatMenu:
			if menu.HandleKey(ev.Ch, ev.Key) {
				t.render()
				return
			}
		case *menus.CommandPalette:
			if menu.HandleKey(ev.Ch, ev.Key) {
				t.render()
				return
			}
		case *menus.MCPMenu:
			if menu.HandleKey(ev.Ch, ev.Key) {
				t.render()
				return
			}
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

	// Open the appropriate menu
	switch key {
	case "s":
		t.sessionMenu.Show()
		t.activeMenu = t.sessionMenu
	case "t":
		t.tasksMenu.Show()
		t.activeMenu = t.tasksMenu
	case "q":
		t.queueMenu.Show()
		t.activeMenu = t.queueMenu
	case "m":
		t.memoryMenu.Show()
		t.activeMenu = t.memoryMenu
	case "c":
		t.chatMenu.Show()
		t.activeMenu = t.chatMenu
	case "ctrl+x":
		t.cmdPalette.Show()
		t.activeMenu = t.cmdPalette
	case "o":
		t.mcpMenu.Show()
		t.activeMenu = t.mcpMenu
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
	if cmd := sharedclient.ParseSlash(input); cmd != nil {
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
	} else if !t.quitting {
		t.quit()
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

	// Check connection before sending
	if t.client == nil || !t.client.IsConnected() {
		t.addScrollback("error: not connected to daemon -- make sure the daemon is running:\n  meept daemon start")
		t.render()
		return
	}

	// Send to daemon (in goroutine to avoid blocking UI)
	go func() {
		reply, err := t.client.Chat(message, t.sessionMgr.GetSessionName())
		if err != nil {
			// Use structured network error detection. Also check for
			// syscall.ENOENT which occurs when the Unix socket file doesn't
			// exist (daemon not started). ENOENT is intentionally not in
			// errcls.IsNetworkError because it's too broad for general use.
			if errcls.IsNetworkError(err) || errors.Is(err, syscall.ENOENT) {
				t.addScrollback("error: unable to reach the daemon. is it running?")
			} else {
				t.addScrollback(fmt.Sprintf("error: %v", err))
			}
		} else {
			t.addScrollback(fmt.Sprintf("meept: %s", reply))
		}
		t.render()
	}()
}

func (t *TUI) executeSlashCommand(cmd *sharedclient.SlashCommand) {
	if t.commandHandler == nil {
		t.commandHandler = NewCommandHandler(t)
	}
	t.commandHandler.Handle(context.Background(), cmd)
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
			termbox.SetCell(x, i, r, termbox.Attribute(sharedclient.ColorWhite), termbox.ColorDefault)
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
				termbox.SetCell(x+i, y, r, termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
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

	// Render active menu (takes priority)
	if t.activeMenu != nil {
		t.activeMenu.Render()
		return
	}

	// Render command mode indicator (only when no menu is active)
	if t.commandMode {
		indicator := "COMMAND MODE: s=session, t=tasks, q=queue, m=memory, c=chat, o=mcp, ^x=palette"
		y := height - 2
		if t.scrollOffset > 0 {
			y = height - 3
		}
		x := 0
		for i, r := range indicator {
			if x+i >= width {
				break
			}
			termbox.SetCell(x+i, y, r, termbox.Attribute(sharedclient.ColorOrange)|termbox.AttrBold, termbox.ColorDefault)
		}
	}

	termbox.Flush()
}

// flushPaste appends the accumulated paste text to the input buffer and shows the indicator.
func (t *TUI) flushPaste() {
	data := t.pasteBuffer.String()
	if data == "" {
		return
	}

	lines := strings.Split(data, "\n")
	lineCount := len(lines)

	for _, line := range lines {
		t.inputBuffer.WriteString(line)
		// Newline character between lines
		if len(lines) > 1 {
			t.inputBuffer.WriteString("\n")
		}
	}
	t.cursorX = len(t.inputBuffer.String())
	t.addScrollback(fmt.Sprintf("[pasted %d lines]", lineCount))
	t.render()
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
		termbox.SetCell(x, popupY, '-', termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
		termbox.SetCell(x, popupY+boxHeight-1, '-', termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
	}
	for y := popupY; y < popupY+boxHeight && y < height; y++ {
		termbox.SetCell(popupX, y, '|', termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
		termbox.SetCell(popupX+boxWidth-1, y, '|', termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
	}

	// Header
	header := " commands "
	for i := 0; i < len(header) && popupX+1+i < width; i++ {
		r := rune(header[i])
		attr := termbox.Attribute(sharedclient.ColorOrange) | termbox.AttrBold
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
		fg := termbox.Attribute(sharedclient.ColorWhite)
		if i == selectedIdx {
			marker = ">"
			fg = termbox.Attribute(sharedclient.ColorOrange) | termbox.AttrBold
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
		termbox.SetCell(hintX+i, y, r, termbox.Attribute(sharedclient.ColorMuted), termbox.ColorDefault)
	}
}
