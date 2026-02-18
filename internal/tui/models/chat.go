// Package models provides the view models for the TUI.
package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FocusedElement represents which element has focus in the chat view.
type FocusedElement int

const (
	FocusInput FocusedElement = iota
	FocusViewport
	FocusSidebar // handled by parent App
)

// MessageState tracks the display state of a message.
type MessageState int

const (
	MessageNormal MessageState = iota
	MessageExpanded
	MessageCollapsed
)

// collapsedLineCount is how many lines to show when a message is collapsed.
const collapsedLineCount = 10

// maxHistorySize is the maximum number of sent messages to remember.
const maxHistorySize = 100

// ChatMessage represents a single chat message.
type ChatMessage struct {
	Role      string // "user", "assistant", "system", or "pending"
	Content   string
	Timestamp time.Time
	State     MessageState
}

// ChatModel is the model for the chat view.
type ChatModel struct {
	rpc            RPCClient
	messages       []ChatMessage
	viewport       viewport.Model
	textarea       textarea.Model
	conversationID string
	width          int
	height         int
	loading        bool
	err            error

	// Focus management
	focused        FocusedElement
	viewportActive bool // true when viewport is actively focused for scrolling

	// Message interaction
	selectedMsgIdx  int  // -1 means no selection
	showContextMenu bool
	contextMenuY    int // Y position for context menu

	// Mouse drag detection
	mouseDownX   int
	mouseDownY   int
	mouseDragged bool

	// Text selection (simplified - tracks if user may have selected text)
	hasTextSelection bool

	// Pending message tracking
	pendingMsgIdx int // index of the "Sending..." message, -1 if none

	// Input history for sent messages
	inputHistory    []string
	historyIdx      int  // current position in history, -1 means not browsing
	savedInput      string // saved current input when browsing history

	// Styles
	userStyle       lipgloss.Style
	assistantStyle  lipgloss.Style
	systemStyle     lipgloss.Style
	pendingStyle    lipgloss.Style
	selectedStyle   lipgloss.Style
	separatorStyle  lipgloss.Style
	focusedBorder   lipgloss.Style
	unfocusedBorder lipgloss.Style
}

// RPCClient interface for the chat model.
type RPCClient interface {
	Chat(message, conversationID string) (string, error)
	IsConnected() bool
}

// NewChatModel creates a new chat model.
func NewChatModel(rpc RPCClient, userStyle, assistantStyle, systemStyle lipgloss.Style) *ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.Focus()
	ta.CharLimit = 4000
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false) // Enter sends, Shift+Enter for newline

	vp := viewport.New(80, 20)
	vp.SetContent("")
	vp.MouseWheelEnabled = true

	return &ChatModel{
		rpc:            rpc,
		messages:       []ChatMessage{},
		viewport:       vp,
		textarea:       ta,
		conversationID: generateConversationID(),
		focused:        FocusInput,
		selectedMsgIdx: -1,
		pendingMsgIdx:  -1,
		historyIdx:     -1,
		inputHistory:   make([]string, 0, maxHistorySize),
		mouseDownX:     -1,
		mouseDownY:     -1,
		userStyle:      userStyle,
		assistantStyle: assistantStyle,
		systemStyle:    systemStyle,
		pendingStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Italic(true).
			PaddingLeft(2),
		selectedStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("#374151")).
			PaddingLeft(2),
		separatorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563")),
		focusedBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7C3AED")),
		unfocusedBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")),
	}
}

func generateConversationID() string {
	return fmt.Sprintf("conv-%d", time.Now().UnixNano())
}

// SetSize updates the model dimensions.
func (m *ChatModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Textarea at bottom (3 lines)
	inputHeight := 3
	viewportHeight := height - inputHeight - 2 // 2 for padding

	m.textarea.SetWidth(width - 4)
	m.viewport.Width = width - 2
	m.viewport.Height = viewportHeight
}

// Init initializes the chat model.
func (m *ChatModel) Init() tea.Cmd {
	if len(m.messages) == 0 {
		m.addMessage("system", "Welcome to Meept! Type a message to begin.")
	}
	return textarea.Blink
}

// ChatResponseMsg carries the chat response.
type ChatResponseMsg struct {
	Reply string
	Err   error
}

// IsFocused returns whether the chat model has focus.
func (m *ChatModel) IsFocused() bool {
	return m.focused == FocusInput || m.focused == FocusViewport
}

// SetFocus sets focus to a specific element.
func (m *ChatModel) SetFocus(elem FocusedElement) {
	m.focused = elem
	if elem == FocusInput {
		m.textarea.Focus()
		m.viewportActive = false
	} else if elem == FocusViewport {
		m.textarea.Blur()
		m.viewportActive = true
	} else {
		m.textarea.Blur()
		m.viewportActive = false
	}
	m.showContextMenu = false
}

// CycleFocus cycles focus to the next element, returns true if cycling to sidebar.
func (m *ChatModel) CycleFocus() bool {
	switch m.focused {
	case FocusInput:
		m.SetFocus(FocusViewport)
		return false
	case FocusViewport:
		// Signal to parent that focus should go to sidebar
		return true
	}
	return false
}

// SetFocusFromSidebar sets focus back from sidebar.
func (m *ChatModel) SetFocusFromSidebar() {
	m.SetFocus(FocusInput)
}

// addToHistory adds a message to the input history buffer.
func (m *ChatModel) addToHistory(text string) {
	// Don't add empty or duplicate of last entry
	if text == "" {
		return
	}
	if len(m.inputHistory) > 0 && m.inputHistory[len(m.inputHistory)-1] == text {
		return
	}

	m.inputHistory = append(m.inputHistory, text)

	// Trim history if too large
	if len(m.inputHistory) > maxHistorySize {
		m.inputHistory = m.inputHistory[1:]
	}

	// Reset history browsing position
	m.historyIdx = -1
	m.savedInput = ""
}

// navigateHistory handles up/down arrows for input history.
func (m *ChatModel) navigateHistory(direction int) {
	if len(m.inputHistory) == 0 {
		return
	}

	if m.historyIdx == -1 {
		// Starting to browse - save current input
		m.savedInput = m.textarea.Value()
		if direction < 0 {
			// Going up - start at end of history
			m.historyIdx = len(m.inputHistory) - 1
		} else {
			return // Can't go down from current input
		}
	} else {
		newIdx := m.historyIdx + direction
		if newIdx < 0 {
			newIdx = 0
		} else if newIdx >= len(m.inputHistory) {
			// Back to current input
			m.historyIdx = -1
			m.textarea.SetValue(m.savedInput)
			return
		}
		m.historyIdx = newIdx
	}

	// Set textarea to history entry
	m.textarea.SetValue(m.inputHistory[m.historyIdx])
}

// Update handles messages for the chat view.
func (m *ChatModel) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle context menu if visible
		if m.showContextMenu {
			return m.handleContextMenuKey(msg)
		}

		switch msg.String() {
		case "tab":
			// Cycle focus within chat view
			if m.CycleFocus() {
				// Return signal to parent to focus sidebar
				return func() tea.Msg { return ChatFocusSidebarMsg{} }
			}
			return nil

		case "enter":
			if m.focused != FocusInput {
				return nil
			}
			if m.loading {
				return nil
			}
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return nil
			}

			// Add to history buffer
			m.addToHistory(text)

			m.textarea.Reset()

			// Add user message
			m.addMessage("user", text)

			// Add pending "Sending..." message immediately
			m.pendingMsgIdx = len(m.messages)
			m.messages = append(m.messages, ChatMessage{
				Role:      "pending",
				Content:   "Sending...",
				Timestamp: time.Now(),
				State:     MessageNormal,
			})
			m.updateViewport()

			m.loading = true
			return m.sendMessage(text)

		case "up":
			if m.focused == FocusInput {
				// Navigate history if at first line or empty
				if m.textarea.Value() == "" || m.historyIdx >= 0 {
					m.navigateHistory(-1)
					return nil
				}
			} else if m.focused == FocusViewport {
				m.viewport.LineUp(1)
				return nil
			}

		case "down":
			if m.focused == FocusInput {
				// Navigate history if browsing
				if m.historyIdx >= 0 {
					m.navigateHistory(1)
					return nil
				}
			} else if m.focused == FocusViewport {
				m.viewport.LineDown(1)
				return nil
			}

		case "ctrl+l":
			// Clear chat history
			m.messages = []ChatMessage{}
			m.conversationID = generateConversationID()
			m.pendingMsgIdx = -1
			m.selectedMsgIdx = -1
			m.updateViewport()
			return nil

		case "esc":
			// Dismiss context menu or deselect
			if m.showContextMenu {
				m.showContextMenu = false
				return nil
			}
			if m.selectedMsgIdx >= 0 {
				m.selectedMsgIdx = -1
				m.hasTextSelection = false
				m.updateViewport()
				return nil
			}
			// Reset history browsing
			if m.historyIdx >= 0 {
				m.historyIdx = -1
				m.textarea.SetValue(m.savedInput)
				return nil
			}

		case "pgup":
			if m.focused == FocusViewport {
				m.viewport.HalfViewUp()
				return nil
			}

		case "pgdown":
			if m.focused == FocusViewport {
				m.viewport.HalfViewDown()
				return nil
			}
		}

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case ChatResponseMsg:
		m.loading = false

		// Remove the pending message
		if m.pendingMsgIdx >= 0 && m.pendingMsgIdx < len(m.messages) {
			m.messages = append(m.messages[:m.pendingMsgIdx], m.messages[m.pendingMsgIdx+1:]...)
		}
		m.pendingMsgIdx = -1

		if msg.Err != nil {
			m.addMessage("system", fmt.Sprintf("Error: %v", msg.Err))
		} else {
			m.addMessage("assistant", msg.Reply)
		}
		return nil
	}

	// Update textarea if focused
	if m.focused == FocusInput {
		var taCmd tea.Cmd
		m.textarea, taCmd = m.textarea.Update(msg)
		if taCmd != nil {
			cmds = append(cmds, taCmd)
		}
	}

	// Update viewport for scrolling
	if m.focused == FocusViewport {
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		if vpCmd != nil {
			cmds = append(cmds, vpCmd)
		}
	}

	return tea.Batch(cmds...)
}

// ChatFocusSidebarMsg signals that focus should move to sidebar.
type ChatFocusSidebarMsg struct{}

// CopyToClipboardMsg signals that text should be copied to clipboard.
type CopyToClipboardMsg struct {
	Text string
}

func (m *ChatModel) handleMouse(msg tea.MouseMsg) tea.Cmd {
	// Calculate viewport bounds (accounting for border)
	viewportTop := 1
	viewportBottom := viewportTop + m.viewport.Height + 1
	inputTop := viewportBottom + 1

	switch msg.Type {
	case tea.MouseLeft:
		// Track mouse down position for drag detection
		m.mouseDownX = msg.X
		m.mouseDownY = msg.Y
		m.mouseDragged = false
		m.hasTextSelection = false

		// Click in viewport area - just focus, don't select yet
		if msg.Y >= viewportTop && msg.Y < viewportBottom {
			m.SetFocus(FocusViewport)
			return nil
		}

		// Click in textarea area
		if msg.Y >= inputTop {
			m.SetFocus(FocusInput)
			// Pass mouse event to textarea for cursor positioning
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			return cmd
		}

	case tea.MouseMotion:
		// Detect drag movement
		if m.mouseDownX >= 0 {
			dx := msg.X - m.mouseDownX
			dy := msg.Y - m.mouseDownY
			// Consider it a drag if moved more than 2 chars
			if dx*dx+dy*dy > 4 {
				m.mouseDragged = true
				m.hasTextSelection = true
			}
		}

	case tea.MouseRelease:
		wasDragged := m.mouseDragged
		wasInViewport := m.mouseDownY >= viewportTop && m.mouseDownY < viewportBottom

		m.mouseDownX = -1
		m.mouseDownY = -1
		m.mouseDragged = false

		// Only handle click (not drag) for message selection
		if !wasDragged && wasInViewport && msg.Y >= viewportTop && msg.Y < viewportBottom {
			// Clean click in viewport - handle message selection
			clickedMsgIdx := m.getMessageAtY(msg.Y - viewportTop)
			if clickedMsgIdx >= 0 {
				if m.selectedMsgIdx == clickedMsgIdx && !m.showContextMenu {
					// Second click on same message - show context menu
					m.showContextMenu = true
					m.contextMenuY = msg.Y
				} else {
					// First click - select message
					m.selectedMsgIdx = clickedMsgIdx
					m.showContextMenu = false
				}
				m.updateViewport()
			}
		} else if wasDragged && wasInViewport {
			// Dragged in viewport - user likely selecting text
			// The terminal handles native text selection, we just track it
			m.hasTextSelection = true
		}
		return nil

	case tea.MouseWheelUp, tea.MouseWheelDown:
		// Allow scrolling in viewport
		if msg.Y >= viewportTop && msg.Y < viewportBottom {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return cmd
		}
	}

	return nil
}

func (m *ChatModel) handleContextMenuKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "c", "1":
		// Copy message (or selected text)
		if m.selectedMsgIdx >= 0 && m.selectedMsgIdx < len(m.messages) {
			msgContent := m.messages[m.selectedMsgIdx].Content
			// If user had text selection (from drag), terminal handles it
			// Otherwise copy full message via OSC52 or system clipboard
			if !m.hasTextSelection {
				// Return command to copy full message
				m.showContextMenu = false
				return func() tea.Msg {
					return CopyToClipboardMsg{Text: msgContent}
				}
			}
			// User has native text selection - just dismiss, terminal handles copy
		}
		m.showContextMenu = false
		return nil

	case "e", "2":
		// Expand message
		if m.selectedMsgIdx >= 0 && m.selectedMsgIdx < len(m.messages) {
			m.messages[m.selectedMsgIdx].State = MessageExpanded
			m.updateViewport()
		}
		m.showContextMenu = false
		return nil

	case "s", "3":
		// Shrink/collapse message
		if m.selectedMsgIdx >= 0 && m.selectedMsgIdx < len(m.messages) {
			m.messages[m.selectedMsgIdx].State = MessageCollapsed
			m.updateViewport()
		}
		m.showContextMenu = false
		return nil

	case "esc", "q":
		m.showContextMenu = false
		return nil
	}
	return nil
}

// getMessageAtY returns the message index at the given viewport Y position.
func (m *ChatModel) getMessageAtY(y int) int {
	// This is an approximation - we track message boundaries
	// by counting lines in the rendered content
	scrollOffset := m.viewport.YOffset
	targetLine := scrollOffset + y

	currentLine := 0
	for i, msg := range m.messages {
		msgLines := m.countMessageLines(msg)
		if targetLine >= currentLine && targetLine < currentLine+msgLines+2 { // +2 for separator
			return i
		}
		currentLine += msgLines + 2 // message lines + separator
	}
	return -1
}

func (m *ChatModel) countMessageLines(msg ChatMessage) int {
	content := m.getMessageContent(msg)
	return strings.Count(content, "\n") + 1
}

func (m *ChatModel) getMessageContent(msg ChatMessage) string {
	var prefix string
	switch msg.Role {
	case "user":
		prefix = "You: "
	case "assistant":
		prefix = "Meept: "
	case "pending":
		prefix = ""
	case "system":
		prefix = ""
	}

	content := prefix + msg.Content

	// Handle collapsed state
	if msg.State == MessageCollapsed {
		lines := strings.Split(content, "\n")
		if len(lines) > collapsedLineCount {
			// Show last N lines with indicator
			collapsedLines := lines[len(lines)-collapsedLineCount:]
			return fmt.Sprintf("... [%d lines hidden] ...\n%s", len(lines)-collapsedLineCount, strings.Join(collapsedLines, "\n"))
		}
	}

	return formatMessage(content, m.width-6)
}

func (m *ChatModel) sendMessage(text string) tea.Cmd {
	return func() tea.Msg {
		reply, err := m.rpc.Chat(text, m.conversationID)
		return ChatResponseMsg{Reply: reply, Err: err}
	}
}

func (m *ChatModel) addMessage(role, content string) {
	m.messages = append(m.messages, ChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		State:     MessageNormal,
	})
	m.updateViewport()
}

func (m *ChatModel) updateViewport() {
	var content strings.Builder

	separator := m.separatorStyle.Render(strings.Repeat("─", m.width-6))

	for i, msg := range m.messages {
		var style lipgloss.Style

		switch msg.Role {
		case "user":
			style = m.userStyle
		case "assistant":
			style = m.assistantStyle
		case "pending":
			style = m.pendingStyle
		case "system":
			style = m.systemStyle
		}

		// Apply selection highlight
		if i == m.selectedMsgIdx {
			style = style.Background(lipgloss.Color("#374151"))
		}

		// Get message content (handles collapse state)
		msgContent := m.getMessageContent(msg)

		// Add timestamp for non-system messages
		var timestampStr string
		if msg.Role != "system" && msg.Role != "pending" {
			timestampStr = msg.Timestamp.Format("15:04")
		}

		// Render message header with timestamp
		if timestampStr != "" {
			header := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Render(fmt.Sprintf("  %s", timestampStr))
			content.WriteString(header)
			content.WriteString("\n")
		}

		// Render message content
		content.WriteString(style.Render(msgContent))
		content.WriteString("\n")

		// Add state indicator for collapsed/expanded
		if msg.State == MessageCollapsed {
			indicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true).
				Render("  [collapsed - click to expand]")
			content.WriteString(indicator)
			content.WriteString("\n")
		} else if msg.State == MessageExpanded {
			indicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true).
				Render("  [expanded - click to collapse]")
			content.WriteString(indicator)
			content.WriteString("\n")
		}

		// Add separator between messages
		if i < len(m.messages)-1 {
			content.WriteString(separator)
			content.WriteString("\n")
		}
	}

	m.viewport.SetContent(content.String())
	m.viewport.GotoBottom()
}

// formatMessage wraps text to fit within the given width.
func formatMessage(text string, width int) string {
	if width <= 0 {
		return text
	}

	var lines []string
	paragraphs := strings.Split(text, "\n")

	for _, para := range paragraphs {
		if len(para) <= width {
			lines = append(lines, para)
			continue
		}

		// Word wrap
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		currentLine := words[0]
		for _, word := range words[1:] {
			if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		lines = append(lines, currentLine)
	}

	return strings.Join(lines, "\n")
}

// View renders the chat view.
func (m *ChatModel) View() string {
	var b strings.Builder

	// Chat history viewport with focus-dependent border
	viewportBorder := m.unfocusedBorder
	if m.focused == FocusViewport {
		viewportBorder = m.focusedBorder
	}

	viewportStyle := viewportBorder.
		Width(m.width - 2).
		Height(m.viewport.Height + 2)

	b.WriteString(viewportStyle.Render(m.viewport.View()))
	b.WriteString("\n")

	// Context menu overlay (rendered inline for simplicity)
	if m.showContextMenu && m.selectedMsgIdx >= 0 {
		menuStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("#1F2937")).
			Foreground(lipgloss.Color("#E5E7EB")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7C3AED")).
			Padding(0, 1)

		var menuText string
		if m.hasTextSelection {
			menuText = "[c] Copy selected  [e] Expand  [s] Shrink  [Esc] Cancel"
		} else {
			menuText = "[c] Copy message  [e] Expand  [s] Shrink  [Esc] Cancel"
		}

		menu := menuStyle.Render(menuText)
		b.WriteString(menu)
		b.WriteString("\n")
	}

	// Input textarea with focus-dependent border
	inputBorder := m.unfocusedBorder
	if m.focused == FocusInput {
		inputBorder = m.focusedBorder
	}

	// Add history indicator if browsing
	inputStyle := inputBorder.Width(m.width - 2)
	inputView := m.textarea.View()
	if m.historyIdx >= 0 {
		historyIndicator := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true).
			Render(fmt.Sprintf(" [history %d/%d]", m.historyIdx+1, len(m.inputHistory)))
		inputView = inputView + historyIndicator
	}
	b.WriteString(inputStyle.Render(inputView))

	return b.String()
}

// Reset clears the chat state.
func (m *ChatModel) Reset() {
	m.messages = []ChatMessage{}
	m.conversationID = generateConversationID()
	m.textarea.Reset()
	m.pendingMsgIdx = -1
	m.selectedMsgIdx = -1
	m.showContextMenu = false
	m.historyIdx = -1
	m.savedInput = ""
	m.hasTextSelection = false
	m.updateViewport()
}
