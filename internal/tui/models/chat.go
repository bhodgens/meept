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

	"github.com/caimlas/meept/internal/tui/render"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/internal/tui/vim"
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

// flushDelay is how long to wait before flushing dirty messages to the server.
const flushDelay = 3 * time.Second

// ChatMessage represents a single chat message.
type ChatMessage struct {
	Role      string // "user", "assistant", "system", or "pending"
	Content   string
	Timestamp time.Time
	State     MessageState

	// Rendering cache
	rendered    string // Cached rendered output
	renderedAt  int    // Width when rendered
	hasMarkdown bool   // Detected markdown
}

// ChatModel is the model for the chat view.
type ChatModel struct {
	rpc            RPCClient
	messages       []ChatMessage
	viewport       viewport.Model
	textarea       textarea.Model
	conversationID string
	sessionID      string // Linked to daemon session
	width          int
	height         int
	loading        bool
	err            error

	// Focus management
	focused        FocusedElement
	viewportActive bool // true when viewport is actively focused for scrolling

	// Message interaction (keyboard-based)
	selectedMsgIdx int // -1 means no selection

	// Per-session message storage
	sessionMessages map[string][]ChatMessage

	// Pending message tracking
	pendingMsgIdx int // index of the "Sending..." message, -1 if none

	// Per-session input history
	sessionHistory map[string][]string
	historyIdx     int    // current position in history, -1 means not browsing
	savedInput     string // saved current input when browsing history

	// Message persistence - buffered writes
	dirtyMessages map[string][]ChatMessage // unsaved messages per session
	flushPending  bool

	// Session header data
	sessionDescription string

	// Markdown rendering
	mdRenderer     *render.MarkdownRenderer
	renderMarkdown bool // Whether to render markdown

	// Vim mode
	vimState *vim.State

	// Styles
	userStyle       lipgloss.Style
	assistantStyle  lipgloss.Style
	systemStyle     lipgloss.Style
	pendingStyle    lipgloss.Style
	separatorStyle  lipgloss.Style
	focusedBorder   lipgloss.Style
	unfocusedBorder lipgloss.Style
	headerStyle     lipgloss.Style
}

// RPCClient interface for the chat model.
type RPCClient interface {
	Chat(message, conversationID string) (string, error)
	IsConnected() bool
	SaveSessionMessages(sessionID string, msgs []types.SessionMessage) error
	GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error)
	UpdateSessionDescription(sessionID, description string) error
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

	// Initialize markdown renderer (will be resized on first SetSize)
	mdRenderer, _ := render.NewMarkdownRenderer(80, true)

	// Initialize vim state (disabled by default)
	vimState := vim.NewState()

	return &ChatModel{
		rpc:             rpc,
		messages:        []ChatMessage{},
		viewport:        vp,
		textarea:        ta,
		conversationID:  generateConversationID(),
		focused:         FocusInput,
		selectedMsgIdx:  -1,
		pendingMsgIdx:   -1,
		historyIdx:      -1,
		sessionMessages: make(map[string][]ChatMessage),
		sessionHistory:  make(map[string][]string),
		dirtyMessages:   make(map[string][]ChatMessage),
		mdRenderer:      mdRenderer,
		renderMarkdown:  true, // Enable markdown by default
		vimState:        vimState,
		userStyle:       userStyle,
		assistantStyle:  assistantStyle,
		systemStyle:     systemStyle,
		pendingStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Italic(true).
			PaddingLeft(2),
		separatorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563")),
		focusedBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#F97316")),
		unfocusedBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")),
		headerStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("#F97316")).
			Foreground(lipgloss.Color("#000000")).
			Bold(true).
			Padding(0, 1),
	}
}

func generateConversationID() string {
	return fmt.Sprintf("conv-%d", time.Now().UnixNano())
}

// SetSize updates the model dimensions.
func (m *ChatModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Textarea at bottom (3 lines), header bar (1 line)
	inputHeight := 3
	headerHeight := 1
	viewportHeight := height - inputHeight - headerHeight - 2 // 2 for padding

	m.textarea.SetWidth(width - 4)
	m.viewport.Width = width - 2
	m.viewport.Height = viewportHeight

	// Update markdown renderer width
	if m.mdRenderer != nil {
		_ = m.mdRenderer.SetWidth(width - 8) // Account for padding
	}

	// Invalidate render cache when width changes
	for i := range m.messages {
		m.messages[i].rendered = ""
		m.messages[i].renderedAt = 0
	}
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

// FlushMessagesMsg signals that dirty messages should be flushed.
type FlushMessagesMsg struct{}

// FlushResultMsg carries the result of flushing messages.
type FlushResultMsg struct {
	Err error
}

// SessionMessagesLoadedMsg carries messages loaded from the server.
type SessionMessagesLoadedMsg struct {
	SessionID string
	Messages  []types.SessionMessage
	Err       error
}

// SessionDescriptionUpdatedMsg signals a description update completed.
type SessionDescriptionUpdatedMsg struct {
	SessionID   string
	Description string
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

// HandleEscape handles escape key behavior.
// Returns a command if an action was taken.
func (m *ChatModel) HandleEscape() tea.Cmd {
	// If message is selected, deselect it
	if m.selectedMsgIdx >= 0 {
		m.selectedMsgIdx = -1
		m.updateViewport()
		return nil
	}
	// If browsing history, cancel it
	if m.historyIdx >= 0 {
		m.historyIdx = -1
		m.textarea.SetValue(m.savedInput)
		return nil
	}
	// If focused on viewport, switch to input
	if m.focused == FocusViewport {
		m.SetFocus(FocusInput)
		return nil
	}
	// If focused on input, clear it
	if m.focused == FocusInput {
		m.textarea.Reset()
		return nil
	}
	return nil
}

// IsInputFocused returns whether the text input has focus.
func (m *ChatModel) IsInputFocused() bool {
	return m.focused == FocusInput
}

// ClearInput clears the text input contents.
func (m *ChatModel) ClearInput() {
	m.textarea.Reset()
}

// currentHistory returns the input history for the current session.
func (m *ChatModel) currentHistory() []string {
	if m.sessionID == "" {
		return nil
	}
	return m.sessionHistory[m.sessionID]
}

// addToHistory adds a message to the input history buffer for the current session.
func (m *ChatModel) addToHistory(text string) {
	if text == "" || m.sessionID == "" {
		return
	}

	history := m.sessionHistory[m.sessionID]

	// Don't add duplicate of last entry
	if len(history) > 0 && history[len(history)-1] == text {
		return
	}

	history = append(history, text)

	// Trim history if too large
	if len(history) > maxHistorySize {
		history = history[1:]
	}

	m.sessionHistory[m.sessionID] = history

	// Reset history browsing position
	m.historyIdx = -1
	m.savedInput = ""
}

// navigateHistory handles up/down arrows for input history.
func (m *ChatModel) navigateHistory(direction int) {
	history := m.currentHistory()
	if len(history) == 0 {
		return
	}

	if m.historyIdx == -1 {
		// Starting to browse - save current input
		m.savedInput = m.textarea.Value()
		if direction < 0 {
			// Going up - start at end of history
			m.historyIdx = len(history) - 1
		} else {
			return // Can't go down from current input
		}
	} else {
		newIdx := m.historyIdx + direction
		if newIdx < 0 {
			newIdx = 0
		} else if newIdx >= len(history) {
			// Back to current input
			m.historyIdx = -1
			m.textarea.SetValue(m.savedInput)
			return
		}
		m.historyIdx = newIdx
	}

	// Set textarea to history entry
	m.textarea.SetValue(history[m.historyIdx])
}

// selectPreviousMessage moves selection to the previous message.
func (m *ChatModel) selectPreviousMessage() {
	if len(m.messages) == 0 {
		return
	}
	if m.selectedMsgIdx < 0 {
		// Start from the last message
		m.selectedMsgIdx = len(m.messages) - 1
	} else if m.selectedMsgIdx > 0 {
		m.selectedMsgIdx--
	}
	m.updateViewport()
}

// selectNextMessage moves selection to the next message.
func (m *ChatModel) selectNextMessage() {
	if len(m.messages) == 0 {
		return
	}
	if m.selectedMsgIdx < 0 {
		// Start from the first message
		m.selectedMsgIdx = 0
	} else if m.selectedMsgIdx < len(m.messages)-1 {
		m.selectedMsgIdx++
	}
	m.updateViewport()
}

// Update handles messages for the chat view.
func (m *ChatModel) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
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

			// Track dirty message for persistence
			m.trackDirtyMessage("user", text)

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
				m.selectPreviousMessage()
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
				m.selectNextMessage()
				return nil
			}

		case "c":
			// Copy selected message content
			if m.focused == FocusViewport && m.selectedMsgIdx >= 0 && m.selectedMsgIdx < len(m.messages) {
				msgContent := m.messages[m.selectedMsgIdx].Content
				m.selectedMsgIdx = -1
				m.updateViewport()
				return func() tea.Msg {
					return CopyToClipboardMsg{Text: msgContent}
				}
			}

		case "e":
			// Expand selected message
			if m.focused == FocusViewport && m.selectedMsgIdx >= 0 && m.selectedMsgIdx < len(m.messages) {
				m.messages[m.selectedMsgIdx].State = MessageExpanded
				m.updateViewport()
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
			// Escape is handled by App.HandleEscape, but handle here for direct calls
			return m.HandleEscape()

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

		// Auto-focus: if viewport is focused and a printable character is typed,
		// redirect focus to the input and forward the keystroke
		if m.focused == FocusViewport && (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) {
			m.SetFocus(FocusInput)
			var taCmd tea.Cmd
			m.textarea, taCmd = m.textarea.Update(msg)
			return taCmd
		}

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

			// Track dirty message for persistence
			m.trackDirtyMessage("assistant", msg.Reply)

			// Auto-description: after first exchange (1 user + 1 assistant)
			descCmd := m.maybeGenerateDescription()
			if descCmd != nil {
				cmds = append(cmds, descCmd)
			}

			// Schedule flush after response
			if !m.flushPending {
				m.flushPending = true
				cmds = append(cmds, tea.Tick(flushDelay, func(t time.Time) tea.Msg {
					return FlushMessagesMsg{}
				}))
			}
		}
		return tea.Batch(cmds...)

	case FlushMessagesMsg:
		m.flushPending = false
		cmd := m.flushMessages()
		if cmd != nil {
			return cmd
		}
		return nil

	case FlushResultMsg:
		if msg.Err != nil {
			// Log flush error but don't show to user - messages are still in memory
		}
		return nil

	case SessionMessagesLoadedMsg:
		if msg.Err != nil || msg.SessionID != m.sessionID {
			return nil
		}
		// Convert server messages to ChatMessages and populate state
		m.loadServerMessages(msg.SessionID, msg.Messages)
		return nil

	case SessionDescriptionUpdatedMsg:
		if msg.SessionID == m.sessionID {
			m.sessionDescription = msg.Description
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

	content := msg.Content

	// Handle collapsed state
	if msg.State == MessageCollapsed {
		lines := strings.Split(content, "\n")
		if len(lines) > collapsedLineCount {
			// Show last N lines with indicator
			collapsedLines := lines[len(lines)-collapsedLineCount:]
			content = fmt.Sprintf("... [%d lines hidden] ...\n%s", len(lines)-collapsedLineCount, strings.Join(collapsedLines, "\n"))
		}
	}

	// Try markdown rendering for assistant messages
	if m.renderMarkdown && m.mdRenderer != nil && msg.Role == "assistant" {
		// Check if markdown is detected
		if render.DetectMarkdown(content) {
			rendered, err := m.mdRenderer.Render(content)
			if err == nil {
				return prefix + rendered
			}
		}
	}

	// Fallback to plain text with word wrap
	return prefix + formatMessage(content, m.width-6)
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

// trackDirtyMessage adds a message to the dirty buffer for later persistence.
func (m *ChatModel) trackDirtyMessage(role, content string) {
	if m.sessionID == "" {
		return
	}
	m.dirtyMessages[m.sessionID] = append(m.dirtyMessages[m.sessionID], ChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
}

// flushMessages sends dirty messages to the server.
func (m *ChatModel) flushMessages() tea.Cmd {
	if m.sessionID == "" || !m.rpc.IsConnected() {
		return nil
	}

	dirty := m.dirtyMessages[m.sessionID]
	if len(dirty) == 0 {
		return nil
	}

	// Copy and clear
	toSave := make([]ChatMessage, len(dirty))
	copy(toSave, dirty)
	delete(m.dirtyMessages, m.sessionID)

	sessionID := m.sessionID
	rpc := m.rpc

	return func() tea.Msg {
		msgs := make([]types.SessionMessage, len(toSave))
		for i, cm := range toSave {
			msgs[i] = types.SessionMessage{
				Role:      cm.Role,
				Content:   cm.Content,
				Timestamp: cm.Timestamp.Format(time.RFC3339),
			}
		}
		err := rpc.SaveSessionMessages(sessionID, msgs)
		return FlushResultMsg{Err: err}
	}
}

// maybeGenerateDescription generates a session description from the first user message.
func (m *ChatModel) maybeGenerateDescription() tea.Cmd {
	if m.sessionID == "" || m.sessionDescription != "" || !m.rpc.IsConnected() {
		return nil
	}

	// Count user and assistant messages (excluding system/pending)
	var userCount, assistantCount int
	var firstUserContent string
	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			userCount++
			if firstUserContent == "" {
				firstUserContent = msg.Content
			}
		case "assistant":
			assistantCount++
		}
	}

	// Only generate on first exchange
	if userCount != 1 || assistantCount != 1 || firstUserContent == "" {
		return nil
	}

	// Extract first 4-7 words
	desc := extractDescription(firstUserContent)
	m.sessionDescription = desc

	sessionID := m.sessionID
	rpc := m.rpc

	return func() tea.Msg {
		_ = rpc.UpdateSessionDescription(sessionID, desc)
		return SessionDescriptionUpdatedMsg{SessionID: sessionID, Description: desc}
	}
}

// extractDescription extracts the first 4-7 words from text for a session description.
func extractDescription(text string) string {
	words := strings.Fields(text)
	maxWords := 7
	if len(words) < maxWords {
		maxWords = len(words)
	}
	if maxWords < 4 && len(words) > 0 {
		maxWords = len(words)
	}
	desc := strings.Join(words[:maxWords], " ")
	if len(words) > maxWords {
		desc += "..."
	}
	return desc
}

// loadServerMessages converts server messages and populates local state.
func (m *ChatModel) loadServerMessages(sessionID string, serverMsgs []types.SessionMessage) {
	var chatMsgs []ChatMessage
	var history []string

	for _, sm := range serverMsgs {
		ts, _ := time.Parse(time.RFC3339, sm.Timestamp)
		chatMsgs = append(chatMsgs, ChatMessage{
			Role:      sm.Role,
			Content:   sm.Content,
			Timestamp: ts,
			State:     MessageNormal,
		})
		// Populate history from user messages
		if sm.Role == "user" {
			history = append(history, sm.Content)
		}
	}

	m.sessionMessages[sessionID] = chatMsgs
	if sessionID == m.sessionID {
		m.messages = make([]ChatMessage, len(chatMsgs))
		copy(m.messages, chatMsgs)
		m.updateViewport()
	}

	// Merge history (don't overwrite existing entries)
	existing := m.sessionHistory[sessionID]
	if len(existing) == 0 {
		m.sessionHistory[sessionID] = history
	}
}

func (m *ChatModel) updateViewport() {
	var content strings.Builder

	separator := m.separatorStyle.Render(strings.Repeat("─", m.width-6))

	selectedStartLine := -1
	currentLine := 0

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

		isSelected := i == m.selectedMsgIdx
		if isSelected {
			selectedStartLine = currentLine
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
			pointer := "  "
			copyHint := ""
			if isSelected {
				pointer = "▸ "
				copyHint = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#10B981")).
					Bold(true).
					Render("  (c) copy")
			}
			header := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Render(fmt.Sprintf("%s%s", pointer, timestampStr))
			content.WriteString(header + copyHint)
			content.WriteString("\n")
			currentLine++
		}

		// Render message content
		rendered := style.Render(msgContent)
		content.WriteString(rendered)
		content.WriteString("\n")
		currentLine += strings.Count(rendered, "\n") + 1

		// Add state indicator for collapsed/expanded
		if msg.State == MessageCollapsed {
			indicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true).
				Render("  [collapsed - press e to expand]")
			content.WriteString(indicator)
			content.WriteString("\n")
			currentLine++
		} else if msg.State == MessageExpanded {
			indicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true).
				Render("  [expanded]")
			content.WriteString(indicator)
			content.WriteString("\n")
			currentLine++
		}

		// Add separator between messages
		if i < len(m.messages)-1 {
			content.WriteString(separator)
			content.WriteString("\n")
			currentLine++
		}
	}

	m.viewport.SetContent(content.String())

	// Scroll behavior: if a message is selected, keep it visible; otherwise go to bottom
	if m.selectedMsgIdx >= 0 && selectedStartLine >= 0 {
		if selectedStartLine < m.viewport.YOffset {
			m.viewport.SetYOffset(selectedStartLine)
		} else if selectedStartLine >= m.viewport.YOffset+m.viewport.Height {
			m.viewport.SetYOffset(selectedStartLine - m.viewport.Height/3)
		}
	} else {
		m.viewport.GotoBottom()
	}
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

	// Orange session header bar with optional vim mode indicator
	desc := m.sessionDescription
	if desc == "" {
		desc = "New Session"
	}

	// Add vim mode indicator if enabled
	headerContent := desc
	if m.vimState != nil && m.vimState.Enabled {
		modeStr := m.vimState.Mode.String()
		modeStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("#374151")).
			Foreground(lipgloss.Color("#E5E7EB")).
			Bold(true).
			Padding(0, 1)

		// Adjust color based on mode
		switch m.vimState.Mode {
		case vim.ModeInsert:
			modeStyle = modeStyle.Background(lipgloss.Color("#10B981"))
		case vim.ModeVisual:
			modeStyle = modeStyle.Background(lipgloss.Color("#8B5CF6"))
		case vim.ModeCommand:
			modeStyle = modeStyle.Background(lipgloss.Color("#3B82F6"))
		}

		// Calculate available width for description
		modeWidth := len(modeStr) + 2 // +2 for padding
		descWidth := m.width - 4 - modeWidth - 3
		if len(desc) > descWidth && descWidth > 3 {
			desc = desc[:descWidth-3] + "..."
		}

		headerContent = desc + " " + modeStyle.Render(modeStr)
	}

	headerBar := m.headerStyle.Width(m.width - 2).Render(headerContent)
	b.WriteString(headerBar)
	b.WriteString("\n")

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

	// Input textarea with focus-dependent border
	inputBorder := m.unfocusedBorder
	if m.focused == FocusInput {
		inputBorder = m.focusedBorder
	}

	// Add history indicator if browsing
	inputStyle := inputBorder.Width(m.width - 2)
	inputView := m.textarea.View()
	if m.historyIdx >= 0 {
		history := m.currentHistory()
		historyIndicator := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true).
			Render(fmt.Sprintf(" [history %d/%d]", m.historyIdx+1, len(history)))
		inputView = inputView + historyIndicator
	}
	b.WriteString(inputStyle.Render(inputView))

	return b.String()
}

// Reset clears the chat state.
func (m *ChatModel) Reset() {
	m.messages = []ChatMessage{}
	m.conversationID = generateConversationID()
	m.sessionID = ""
	m.sessionDescription = ""
	m.textarea.Reset()
	m.pendingMsgIdx = -1
	m.selectedMsgIdx = -1
	m.historyIdx = -1
	m.savedInput = ""
	m.updateViewport()
}

// SetSession links the chat to a daemon session, preserving messages per session.
func (m *ChatModel) SetSession(session *types.Session) tea.Cmd {
	if session == nil {
		m.sessionID = ""
		m.sessionDescription = ""
		return nil
	}

	// Flush current session's dirty messages before switching
	var flushCmd tea.Cmd
	if m.sessionID != "" && len(m.dirtyMessages[m.sessionID]) > 0 {
		flushCmd = m.flushMessages()
	}

	// Save current session's messages before switching
	if m.sessionID != "" {
		saved := make([]ChatMessage, len(m.messages))
		copy(saved, m.messages)
		m.sessionMessages[m.sessionID] = saved
	}

	m.sessionID = session.ID
	m.conversationID = session.ConversationID
	m.selectedMsgIdx = -1
	m.historyIdx = -1
	m.savedInput = ""
	m.sessionDescription = session.Description

	// Restore previous messages for this session, or start fresh
	if saved, ok := m.sessionMessages[session.ID]; ok && len(saved) > 0 {
		m.messages = make([]ChatMessage, len(saved))
		copy(m.messages, saved)
		m.updateViewport()
		return flushCmd
	}

	// No local messages - try to load from server
	m.messages = []ChatMessage{}
	m.updateViewport()

	if m.rpc.IsConnected() {
		rpc := m.rpc
		sessionID := session.ID
		loadCmd := func() tea.Msg {
			resp, err := rpc.GetSessionMessages(sessionID, 0, 1000)
			if err != nil {
				return SessionMessagesLoadedMsg{SessionID: sessionID, Err: err}
			}
			return SessionMessagesLoadedMsg{
				SessionID: sessionID,
				Messages:  resp.Messages,
			}
		}

		if flushCmd != nil {
			return tea.Batch(flushCmd, loadCmd)
		}
		return loadCmd
	}

	return flushCmd
}

// VimState returns the vim state for external access.
func (m *ChatModel) VimState() *vim.State {
	return m.vimState
}

// EnableVim enables vim keybindings.
func (m *ChatModel) EnableVim() {
	if m.vimState != nil {
		m.vimState.Enable()
	}
}

// DisableVim disables vim keybindings.
func (m *ChatModel) DisableVim() {
	if m.vimState != nil {
		m.vimState.Disable()
	}
}

// ToggleVim toggles vim keybindings.
func (m *ChatModel) ToggleVim() {
	if m.vimState != nil {
		m.vimState.Toggle()
	}
}

// SetVimConfig applies vim configuration.
func (m *ChatModel) SetVimConfig(cfg VimConfig) {
	if m.vimState != nil {
		m.vimState.Enabled = cfg.Enabled
		if cfg.EscapeInsert != "" {
			m.vimState.EscapeSequence = cfg.EscapeInsert
		}
		if cfg.Leader != "" {
			m.vimState.LeaderKey = cfg.Leader
		}
	}
}

// VimConfig holds vim configuration for the chat model.
type VimConfig struct {
	Enabled      bool
	EscapeInsert string
	Leader       string
}

// ToggleMarkdown toggles markdown rendering.
func (m *ChatModel) ToggleMarkdown() {
	m.renderMarkdown = !m.renderMarkdown
	// Invalidate cache
	for i := range m.messages {
		m.messages[i].rendered = ""
	}
	m.updateViewport()
}

// SetMarkdownEnabled enables or disables markdown rendering.
func (m *ChatModel) SetMarkdownEnabled(enabled bool) {
	m.renderMarkdown = enabled
	// Invalidate cache
	for i := range m.messages {
		m.messages[i].rendered = ""
	}
	m.updateViewport()
}
