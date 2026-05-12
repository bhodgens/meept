// Package models provides the view models for the TUI.
package models

import (
	"encoding/json"
	"slices"

	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	Progress  *ProgressState // Progress state for pending messages

	// Rendering cache
	rendered   string // Cached rendered output
	renderedAt int    // Width when rendered
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

	// Focus management
	focused        FocusedElement
	viewportActive bool // true when viewport is actively focused for scrolling

	// Message interaction (keyboard-based)
	selectedMsgIdx int // -1 means no selection

	// Per-session message storage
	sessionMessages map[string][]ChatMessage

	// Pending message tracking
	pendingMsgIdx int // index of the "Sending..." message, -1 if none

	// Progress state for the current pending message
	progressState *ProgressState

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

	// Escape behavior config: "once", "twice", or "off"
	escapeBehavior string
	lastEscapeTime time.Time

	// Chat config
	autoCopyOnRelease bool // Whether to auto-copy text selection on mouse release
	scrollSpeed       int  // Lines to scroll per mouse wheel event

	// Paste compression
	compressedPastes map[int]string // pasteID -> original paste content
	pasteCounter     int

	// File attachments - paths dragged/pasted into the input
	attachments []string

	// Mouse selection state
	mouseDown      bool
	mouseDownY     int // Viewport Y where mouse was pressed
	mouseDownX     int // Viewport X where mouse was pressed
	mouseDragY     int // Current viewport Y during drag
	mouseDragX     int // Current viewport X during drag
	selectionStart int // Character offset in viewport content
	selectionEnd   int // Character offset in viewport content
	isSelecting    bool

	// Click tracking for double/triple click
	lastClickTime time.Time
	lastClickY    int
	clickCount    int

	// Input selection state (mirrors viewport selection)
	inputMouseDown      bool
	inputSelectionStart int
	inputSelectionEnd   int
	inputIsSelecting    bool
	inputLastClickTime  time.Time
	inputClickCount     int

	// Input height (supports dynamic resize)
	inputHeight int

	// Screen Y offset (header height + any chrome above chat area)
	screenYOffset int

	// Slash autocomplete popup (rendered above input)
	slashAutocompletePopup string

	// Steering and follow-up queue state
	agentActive   bool                     // true while agent is processing
	queueStatus   *types.QueueStatusResponse // latest queue state (nil if agent idle)
	steerMode     bool                     // when true, next message is a steer (ctrl+s toggle)

	// Styles
	userStyle       lipgloss.Style
	assistantStyle  lipgloss.Style
	systemStyle     lipgloss.Style
	pendingStyle    lipgloss.Style
	separatorStyle  lipgloss.Style
	focusedBorder   lipgloss.Style
	unfocusedBorder lipgloss.Style
	headerStyle     lipgloss.Style
	steerBadgeStyle lipgloss.Style
	followUpBadgeStyle lipgloss.Style
	agentActiveBadgeStyle lipgloss.Style
}

// RPCClient interface for the chat model.
type RPCClient interface {
	Chat(message, conversationID string) (string, error)
	IsConnected() bool
	SaveSessionMessages(sessionID string, msgs []types.SessionMessage) error
	GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error)
	UpdateSessionDescription(sessionID, description string) error
	GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error)
	Steer(message, conversationID string) error
	FollowUp(message, conversationID string) error
	GetQueueStatus(conversationID string) (*types.QueueStatusResponse, error)
}

// ChatConfig holds chat viewport behavior settings.
type ChatConfig struct {
	AutoCopyOnRelease bool // Whether to auto-copy text selection on mouse release
	ScrollSpeed       int  // Lines to scroll per mouse wheel event
}

// InputBehaviorConfig holds input textarea behavior settings.
type InputBehaviorConfig struct {
	EnterBehavior string // "shift_sends" or "double_enter"
	AutoExpand    bool   // Enable auto-expanding input height
}

// NewChatModel creates a new chat model.
func NewChatModel(rpc RPCClient, userStyle, assistantStyle, systemStyle lipgloss.Style, escapeBehavior string) *ChatModel {
	return NewChatModelWithConfig(rpc, userStyle, assistantStyle, systemStyle, escapeBehavior, InputBehaviorConfig{
		EnterBehavior: "shift_sends",
		AutoExpand:    false,
	}, ChatConfig{
		AutoCopyOnRelease: false,
		ScrollSpeed:       3,
	})
}

// NewChatModelWithConfig creates a new chat model with input configuration.
func NewChatModelWithConfig(rpc RPCClient, userStyle, assistantStyle, systemStyle lipgloss.Style, escapeBehavior string, inputConfig InputBehaviorConfig, chatConfig ChatConfig) *ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.Focus()
	ta.CharLimit = 4000
	ta.SetWidth(80)
	ta.ShowLineNumbers = false

	// Dynamic height: starts at 3 lines, grows up to 8 lines as content increases
	ta.DynamicHeight = true
	ta.MinHeight = 3
	ta.MaxHeight = 8

	// Configure cursor: orange, blinking, vertical bar
	styles := ta.Styles()
	orange := lipgloss.Color("#F97316")
	styles.Cursor.Color = orange
	styles.Cursor.Blink = true
	styles.Cursor.Shape = tea.CursorBar
	ta.SetStyles(styles)

	// Use virtual cursor - rendered as part of textarea content
	ta.SetVirtualCursor(true)

	// ALWAYS disable InsertNewline - we handle both Enter and Shift+Enter manually
	// This prevents bubbles/textarea from intercepting Enter before our Update() handler
	ta.KeyMap.InsertNewline.SetEnabled(false)

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent("")

	// Initialize markdown renderer (will be resized on first SetSize)
	mdRenderer, _ := render.NewMarkdownRenderer(80, true)

	// Initialize vim state (disabled by default)
	vimState := vim.NewState()

	// Default escape behavior if not specified
	if escapeBehavior == "" {
		escapeBehavior = "once"
	}

	return &ChatModel{
		rpc:               rpc,
		messages:          []ChatMessage{},
		viewport:          vp,
		textarea:          ta,
		conversationID:    generateConversationID(),
		focused:           FocusInput,
		selectedMsgIdx:    -1,
		pendingMsgIdx:     -1,
		historyIdx:        -1,
		sessionMessages:   make(map[string][]ChatMessage),
		sessionHistory:    make(map[string][]string),
		dirtyMessages:     make(map[string][]ChatMessage),
		mdRenderer:        mdRenderer,
		renderMarkdown:    true, // Enable markdown by default
		vimState:          vimState,
		escapeBehavior:    escapeBehavior,
		inputHeight:       3, // Fixed input height
		autoCopyOnRelease: chatConfig.AutoCopyOnRelease,
		scrollSpeed:       chatConfig.ScrollSpeed,
		compressedPastes:  make(map[int]string),
		userStyle:         userStyle,
		assistantStyle:    assistantStyle,
		systemStyle:       systemStyle,
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
		steerBadgeStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("#EF4444")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1).
			Bold(true),
		followUpBadgeStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("#10B981")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1),
		agentActiveBadgeStyle: lipgloss.NewStyle().
			Background(lipgloss.Color("#6B7280")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1).
			Bold(true),
	}
}

func generateConversationID() string {
	return fmt.Sprintf("conv-%d", time.Now().UnixNano())
}

// SetSize updates the model dimensions.
func (m *ChatModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Minimum input height (dynamic height enabled)
	const minInputHeight = 3
	m.inputHeight = minInputHeight

	// Set textarea width
	m.textarea.SetWidth(width - 4)
	// Don't call SetHeight - DynamicHeight auto-calculates based on content

	// Set textarea width (height is dynamic)
	m.textarea.SetWidth(width - 4)
	m.viewport.SetWidth(width - 2)
	// Viewport height is set in View() to match actual render

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

// SetScreenYOffset sets the screen Y offset for mouse coordinate transformation.
// This should be called by the App to inform the ChatModel of any chrome (header, etc.)
// above the chat area so that mouse coordinates can be properly transformed.
func (m *ChatModel) SetScreenYOffset(offset int) {
	m.screenYOffset = offset
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
	Name        string
	Description string
}

// InputHeightChangedMsg signals that the input area height changed.
type InputHeightChangedMsg struct {
	NewHeight int // New input height in lines (excluding borders)
}

// ChatDetachedTaskMsg signals that a task was dispatched asynchronously.
type ChatDetachedTaskMsg struct {
	TaskID      string
	TaskName    string
	Description string
}

// ChatTaskResultMsg signals that an async task completed or failed.
type ChatTaskResultMsg struct {
	TaskID         string
	TaskName       string
	State          string // "completed" or "failed"
	CompletedSteps int
	TotalSteps     int
	ResultSummary  string
}

// ProgressUpdateMsg carries progress updates for the current pending message.
type ProgressUpdateMsg struct {
	AgentID       string
	Stage         string
	Percent       float64
	CurrentTool   string
	TokensUsed    int
	ContextResets int
	ChatVisible   bool
}

// IsChatVisible returns true if this progress update should display in chat.
func (m ProgressUpdateMsg) IsChatVisible() bool {
	return m.ChatVisible
}

// SteeringResultMsg is returned after a steering RPC call completes.
type SteeringResultMsg struct {
	Success bool
	Err     error
}

// FollowUpResultMsg is returned after a follow-up RPC call completes.
type FollowUpResultMsg struct {
	Success bool
	Err     error
}

// AgentLifecycleMsg signals agent start/stop events.
type AgentLifecycleMsg struct {
	Active         bool
	ConversationID string
}

// SteeringInjectedMsg signals that a steering message was injected into the queue.
type SteeringInjectedMsg struct{}

// FollowUpInjectedMsg signals that a follow-up message was injected into the queue.
type FollowUpInjectedMsg struct{}

// FollowUpRestoredMsg signals that pending follow-ups were restored.
type FollowUpRestoredMsg struct {
	Count int
}

// ProgressState holds current progress for display in chat.
type ProgressState struct {
	AgentID       string
	Stage         string
	Percent       float64
	CurrentTool   string
	TokensUsed    int
	ContextResets int
	LastUpdate    time.Time
}

// Render returns the formatted progress string for display.
func (p *ProgressState) Render() string {
	if p == nil {
		return "Sending..."
	}

	var parts []string

	// Agent emoji + name
	if p.AgentID != "" {
		agentDisplay := p.AgentID
		if len(agentDisplay) > 12 {
			agentDisplay = agentDisplay[:12]
		}
		parts = append(parts, fmt.Sprintf("🤖 %s", agentDisplay))
	}

	// Progress bar
	if p.Percent > 0 {
		barWidth := 20
		filled := int(p.Percent / 100 * float64(barWidth))
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		parts = append(parts, fmt.Sprintf("[%s %.0f%%]", bar, p.Percent))
	} else if p.Stage != "" {
		parts = append(parts, p.Stage)
	}

	// Current tool
	if p.CurrentTool != "" {
		parts = append(parts, fmt.Sprintf("→ %s", p.CurrentTool))
	}

	// Tokens
	if p.TokensUsed > 0 {
		parts = append(parts, fmt.Sprintf("📊 %s", formatTokens(p.TokensUsed)))
	}

	// Context reset indicator
	if p.ContextResets > 0 {
		parts = append(parts, fmt.Sprintf("🔄 %d", p.ContextResets))
	}

	if len(parts) == 0 {
		return "Processing..."
	}

	return strings.Join(parts, " │ ")
}

// IsComplete returns true if progress indicates completion.
func (p *ProgressState) IsComplete() bool {
	return p != nil && p.Percent >= 100
}

// IsStale returns true if progress hasn't been updated recently.
func (p *ProgressState) IsStale() bool {
	return p == nil || time.Since(p.LastUpdate) > 5*time.Minute
}

// IsFocused returns whether the chat model has focus.
func (m *ChatModel) IsFocused() bool {
	return m.focused == FocusInput || m.focused == FocusViewport
}

// IsLoading returns whether the chat is currently waiting for a response.
func (m *ChatModel) IsLoading() bool {
	return m.loading
}

// ClearLoading resets the loading state (e.g., after stopping work).
func (m *ChatModel) ClearLoading() {
	m.loading = false
	// Remove pending message if any
	if m.pendingMsgIdx >= 0 && m.pendingMsgIdx < len(m.messages) {
		// Replace pending message with a cancelled indicator
		m.messages[m.pendingMsgIdx] = ChatMessage{
			Role:      "system",
			Content:   "[work stopped]",
			Timestamp: time.Now(),
			State:     MessageNormal,
		}
		m.pendingMsgIdx = -1
		m.progressState = nil
		m.updateViewport()
	}
}

// SetFocus sets focus to a specific element.
func (m *ChatModel) SetFocus(elem FocusedElement) {
	m.focused = elem
	switch elem {
	case FocusInput:
		m.textarea.Focus()
		m.viewportActive = false
	case FocusViewport:
		m.textarea.Blur()
		m.viewportActive = true
	default:
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
	// If focused on input, clear it based on escape behavior config
	if m.focused == FocusInput {
		switch m.escapeBehavior {
		case "once":
			m.textarea.Reset()
		case "twice":
			now := time.Now()
			if now.Sub(m.lastEscapeTime) < 500*time.Millisecond {
				m.textarea.Reset()
				m.lastEscapeTime = time.Time{} // Reset the timer
			} else {
				m.lastEscapeTime = now
			}
		case "off":
			// Do nothing - escape doesn't clear input
		default:
			m.textarea.Reset() // Fallback to "once"
		}
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
	case tea.MouseMsg:
		// First, check if mouse event is in the textarea input area
		if textareaStartY, _ := m.getTextareaBounds(); textareaStartY >= 0 {
			// Forward mouse events to textarea handler
			return m.HandleInputMouse(msg)
		}

		switch msg := msg.(type) {
		case tea.MouseWheelMsg:
			// Handle wheel scrolling - this prevents terminal buffer scroll
			mouse := msg.Mouse()
			lines := m.scrollSpeed
			if lines <= 0 {
				lines = 3
			}
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.viewport.ScrollUp(lines)
			case tea.MouseWheelDown:
				m.viewport.ScrollDown(lines)
			}
			return nil

		case tea.MouseClickMsg:
			mouse := msg.Mouse()
			if !m.isClickInViewportArea(mouse) {
				return nil
			}
			return m.handleMousePress(msg)

		case tea.MouseMotionMsg:
			if m.mouseDown {
				return m.handleMouseDrag(msg)
			}
			return nil

		case tea.MouseReleaseMsg:
			if m.mouseDown {
				return m.handleMouseRelease(msg)
			}
			return nil
		}
		return nil

	case tea.KeyPressMsg:
		// Handle 'c' key for copying selection before any other logic
		// This allows copying even when input is focused
		if msg.String() == "c" && m.hasSelection() {
			return m.CopySelection()
		}

		// Clear any active text selection when user starts typing (but not for 'c')
		if m.hasSelection() && m.focused == FocusInput {
			m.clearSelection()
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
			// Enter always sends message (when focused on input and not loading)
			if m.focused != FocusInput || m.loading {
				return nil
			}
			return m.doSendMessage()

		case "shift+enter", "ctrl+j":
			// Shift+Enter or Ctrl+J inserts a newline
			if m.focused == FocusInput {
				m.textarea.InsertRune('\n')
				return nil
			}
			return nil

		case "up":
			switch m.focused {
			case FocusInput:
				// Navigate history if at first line or empty
				if m.textarea.Value() == "" || m.historyIdx >= 0 {
					m.navigateHistory(-1)
					return nil
				}
			case FocusViewport:
				m.selectPreviousMessage()
				return nil
			}

		case "down":
			switch m.focused {
			case FocusInput:
				// Navigate history if browsing
				if m.historyIdx >= 0 {
					m.navigateHistory(1)
					return nil
				}
			case FocusViewport:
				m.selectNextMessage()
				return nil
			}

		case "c":
			// Copy selected message content (keyboard navigation)
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

		case "shift+up":
			if m.focused == FocusViewport {
				m.viewport.HalfPageUp()
				return nil
			}

		case "shift+down":
			if m.focused == FocusViewport {
				m.viewport.HalfPageDown()
				return nil
			}

		case "pgup":
			if m.focused == FocusViewport {
				m.viewport.PageUp()
				return nil
			}

		case "pgdown":
			if m.focused == FocusViewport {
				m.viewport.PageDown()
				return nil
			}

		case "j":
			// Vim-style: scroll viewport or select message
			if m.focused == FocusViewport {
				m.viewport.ScrollDown(1)
				return nil
			}

		case "k":
			// Vim-style: scroll viewport
			if m.focused == FocusViewport {
				m.viewport.ScrollUp(1)
				return nil
			}
		}

		// Auto-focus: if viewport is focused and a printable character is typed,
		// redirect focus to the input and forward the keystroke
		if m.focused == FocusViewport && len(msg.Text) > 0 {
			m.SetFocus(FocusInput)
			var taCmd tea.Cmd
			m.textarea, taCmd = m.textarea.Update(msg)
			return taCmd
		}

	case ChatResponseMsg:
		m.loading = false

		// Clear progress state
		m.progressState = nil

		// Remove the pending message
		if m.pendingMsgIdx >= 0 && m.pendingMsgIdx < len(m.messages) {
			m.messages = append(m.messages[:m.pendingMsgIdx], m.messages[m.pendingMsgIdx+1:]...)
		}
		m.pendingMsgIdx = -1

		if msg.Err != nil {
			m.addMessage("system", fmt.Sprintf("Error: %v", msg.Err))
		} else {
			// Check if the reply is an async task ack
			if isAsyncAck, taskID, taskMessage := parseAsyncAck(msg.Reply); isAsyncAck {
				// Render as a detached task message
				content := fmt.Sprintf("┌ task detached ─────────────────────────────┐\n"+
					"│ %s\n"+
					"│ Task ID: %s\n"+
					"│ View progress: [ctrl+x 2] tasks\n"+
					"└─────────────────────────────────────────────┘",
					types.TruncateString(taskMessage, 45),
					types.TruncateString(taskID, 40),
				)
				m.addMessage("system", content)
			} else if result := parseTaskResult(msg.Reply); result != nil {
				// Render as a task result message
				var content string
				if result.IsSuccess {
					content = fmt.Sprintf("┌ task completed ────────────────────────────┐\n"+
						"│ Task: \"%s\"\n"+
						"│ Steps: %d/%d completed\n",
						types.TruncateString(result.Name, 40),
						result.Completed, result.Total,
					)
					if result.Result != "" {
						content += fmt.Sprintf("│ %s\n", types.TruncateString(result.Result, 45))
					}
					content += "└─────────────────────────────────────────────┘"
				} else {
					content = fmt.Sprintf("┌ task failed ───────────────────────────────┐\n"+
						"│ Task: \"%s\"\n"+
						"│ Steps: %d completed, %d failed of %d\n",
						types.TruncateString(result.Name, 40),
						result.Completed, result.Failed, result.Total,
					)
					if result.Error != "" {
						content += fmt.Sprintf("│ Error: %s\n", types.TruncateString(result.Error, 40))
					}
					content += "└─────────────────────────────────────────────┘"
				}
				m.addMessage("system", content)
			} else {
				m.addMessage("assistant", msg.Reply)
			}

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
			_ = msg.Err
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

	case ChatDetachedTaskMsg:
		// Render a styled detached task message in chat
		content := fmt.Sprintf("┌ task detached ─────────────────────────────┐\n"+
			"│ Working on: \"%s\"\n"+
			"│ Task ID: %s\n"+
			"│ View progress: [ctrl+x 2] tasks\n"+
			"└─────────────────────────────────────────────┘",
			types.TruncateString(msg.TaskName, 40),
			types.TruncateString(msg.TaskID, 40),
		)
		m.addMessage("system", content)
		return nil

	case ChatTaskResultMsg:
		// Render a styled task result message in chat
		stateLabel := "completed"
		if msg.State == "failed" {
			stateLabel = "failed"
		}
		content := fmt.Sprintf("┌ task %s ────────────────────────────────┐\n"+
			"│ Task: \"%s\"\n"+
			"│ Steps: %d/%d %s\n",
			stateLabel,
			types.TruncateString(msg.TaskName, 40),
			msg.CompletedSteps, msg.TotalSteps, stateLabel,
		)
		if msg.ResultSummary != "" {
			content += fmt.Sprintf("│ %s\n", types.TruncateString(msg.ResultSummary, 50))
		}
		content += "└─────────────────────────────────────────────┘"
		m.addMessage("system", content)
		return nil

	case ProgressUpdateMsg:
		if m.progressState == nil {
			m.progressState = &ProgressState{}
		}
		if msg.AgentID != "" {
			m.progressState.AgentID = msg.AgentID
		}
		if msg.Stage != "" {
			m.progressState.Stage = msg.Stage
		}
		if msg.Percent > 0 {
			m.progressState.Percent = msg.Percent
		}
		if msg.CurrentTool != "" {
			m.progressState.CurrentTool = msg.CurrentTool
		}
		if msg.TokensUsed > 0 {
			m.progressState.TokensUsed = msg.TokensUsed
		}
		if msg.ContextResets > 0 {
			m.progressState.ContextResets = msg.ContextResets
		}
		m.progressState.LastUpdate = time.Now()
		m.updateProgressMessage()
		return nil

	case AgentLifecycleMsg:
		if msg.Active && msg.ConversationID == m.conversationID {
			m.agentActive = true
		} else if !msg.Active && (msg.ConversationID == "" || msg.ConversationID == m.conversationID) {
			m.agentActive = false
			m.steerMode = false
			m.queueStatus = nil
		}
		return nil

	case SteeringResultMsg:
		if msg.Success {
			m.addMessage("system", "[steering] message queued")
		} else {
			m.addMessage("system", fmt.Sprintf("[steering] error: %v", msg.Err))
		}
		return nil

	case SteeringInjectedMsg:
		m.addMessage("system", "[steering] steering message injected into agent queue")
		return nil

	case FollowUpResultMsg:
		if msg.Success {
			m.addMessage("system", "[follow-up] message queued for processing")
		} else {
			m.addMessage("system", fmt.Sprintf("[follow-up] error: %v", msg.Err))
		}
		return nil

	case FollowUpInjectedMsg:
		m.AddSystemMessage("[follow-up] follow-up message injected into agent queue")
		return nil

	case FollowUpRestoredMsg:
		m.AddSystemMessage(fmt.Sprintf("[follow-up] restored %d pending follow-up message(s)", msg.Count))
		return nil
	}

	// Update textarea if focused
	if m.focused == FocusInput {
		oldValue := m.textarea.Value()
		oldLines := strings.Count(oldValue, "\n") + 1

		var taCmd tea.Cmd
		m.textarea, taCmd = m.textarea.Update(msg)
		if taCmd != nil {
			cmds = append(cmds, taCmd)
		}

		// Detect paste: significant multi-line increase in single update
		newValue := m.textarea.Value()
		newLines := strings.Count(newValue, "\n") + 1
		addedLines := newLines - oldLines

		// If 3+ lines were added at once, treat as paste and compress
		if addedLines >= 3 {
			// Extract the pasted content
			added := ""
			if len(newValue) > len(oldValue) {
				// Simple heuristic: the added content is appended
				// Find where the addition starts
				commonLen := 0
				for i := 0; i < len(oldValue) && i < len(newValue); i++ {
					if oldValue[i] == newValue[i] {
						commonLen = i + 1
					} else {
						break
					}
				}
				added = newValue[commonLen:]
			}

			if added != "" {
				m.pasteCounter++
				pasteID := m.pasteCounter
				m.compressedPastes[pasteID] = added

				// Replace the pasted content with a token
				pasteToken := fmt.Sprintf("{paste: %d lines}", addedLines)
				compressedValue := oldValue + pasteToken
				m.textarea.SetValue(compressedValue)
			}
		}

		// Detect file path drops/pastes: check for newly added path-like content
		// that refers to an existing file. Only trigger on paste (not typing).
		if newValue != oldValue && len(newValue) > len(oldValue) {
			m.detectAndAttachFile(oldValue, newValue)
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

// AddSystemMessage adds a system message to the chat transcript.
func (m *ChatModel) AddSystemMessage(content string) {
	m.addMessage("system", content)
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

// doSendMessage handles the common logic for sending a message.
// When agent is active, messages are queued as follow-ups or steering messages
// instead of starting a new conversation.
func (m *ChatModel) doSendMessage() tea.Cmd {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" && len(m.attachments) == 0 {
		return nil
	}

	// Expand paste tokens to get actual message content
	actualText := m.expandPasteTokens(text)

	// Prepend attachment file references for the LLM context
	if len(m.attachments) > 0 {
		var attachmentRefs []string
		for _, path := range m.attachments {
			// Include file path as context for the LLM
			attachmentRefs = append(attachmentRefs, fmt.Sprintf("[Attached file: %s]", path))
		}
		actualText = strings.Join(attachmentRefs, "\n") + "\n\n" + actualText
	}

	m.textarea.Reset()
	m.attachments = nil // Clear attachments after sending

	// Clear compressed pastes after sending
	m.compressedPastes = make(map[int]string)
	m.pasteCounter = 0

	// Route based on agent state
	if m.agentActive {
		// Agent is running - queue the message
		m.addToHistory(text)

		if m.steerMode {
			m.steerMode = false // Reset after sending
			m.addMessage("user", "[steering] "+actualText)
			m.trackDirtyMessage("user", "[steering] "+actualText)
			return m.SteerQueue(text)
		}
		// Follow-up mode
		m.AddSystemMessage("Message queued (follow-up)")
		return m.FollowUpQueue(text)
	}

	// Agent is idle - normal chat
	m.addToHistory(text)

	// Add user message (with expanded content)
	m.addMessage("user", actualText)

	// Track dirty message for persistence
	m.trackDirtyMessage("user", actualText)

	// Initialize progress state
	m.progressState = &ProgressState{
		Stage:      "Sending...",
		LastUpdate: time.Now(),
	}

	// Add pending "Sending..." message immediately
	m.pendingMsgIdx = len(m.messages)
	m.messages = append(m.messages, ChatMessage{
		Role:      "pending",
		Content:   m.renderProgressContent(),
		Timestamp: time.Now(),
		State:     MessageNormal,
		Progress:  m.progressState,
	})
	m.updateViewport()

	m.loading = true
	return m.sendMessage(actualText)
}

// GetInputHeight returns the fixed input height in lines.
func (m *ChatModel) GetInputHeight() int {
	return m.inputHeight
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
// Uses LLM-based summarization via the daemon for intelligent categorization.
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

	sessionID := m.sessionID
	rpc := m.rpc

	// Set a temporary description while LLM generates
	m.sessionDescription = "summarizing..."

	return func() tea.Msg {
		// Try LLM-based generation first
		result, err := rpc.GenerateSessionDescription(sessionID, firstUserContent, "")
		if err != nil {
			// Fallback to simple extraction
			desc := extractDescription(firstUserContent)
			_ = rpc.UpdateSessionDescription(sessionID, desc)
			return SessionDescriptionUpdatedMsg{SessionID: sessionID, Description: desc}
		}
		// The daemon already saved both name and description, so just return the result
		return SessionDescriptionUpdatedMsg{
			SessionID:   sessionID,
			Name:        result.Name,
			Description: result.Description,
		}
	}
}

// extractDescription extracts the first 4-7 words from text for a session description.
func extractDescription(text string) string {
	words := strings.Fields(text)
	maxWords := min(len(words), 7)
	if maxWords < 4 && len(words) > 0 {
		maxWords = len(words)
	}
	desc := strings.Join(words[:maxWords], " ")
	if len(words) > maxWords {
		desc += "..."
	}
	return desc
}

// updateProgressMessage updates the pending message content with current progress.
func (m *ChatModel) updateProgressMessage() {
	if m.pendingMsgIdx < 0 || m.pendingMsgIdx >= len(m.messages) {
		return
	}

	// Replace the pending message content with progress info
	m.messages[m.pendingMsgIdx].Content = m.renderProgressContent()
	m.messages[m.pendingMsgIdx].Progress = m.progressState
	m.updateViewport()
}

// renderProgressContent renders the current progress state as a string.
func (m *ChatModel) renderProgressContent() string {
	p := m.progressState
	if p == nil {
		return "Sending..."
	}

	var parts []string

	// Agent emoji + name
	if p.AgentID != "" {
		agentDisplay := p.AgentID
		if len(agentDisplay) > 12 {
			agentDisplay = agentDisplay[:12]
		}
		parts = append(parts, fmt.Sprintf("🤖 %s", agentDisplay))
	}

	// Progress bar
	if p.Percent > 0 {
		barWidth := 20
		filled := int(p.Percent / 100 * float64(barWidth))
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		parts = append(parts, fmt.Sprintf("[%s %.0f%%]", bar, p.Percent))
	} else if p.Stage != "" {
		parts = append(parts, p.Stage)
	}

	// Current tool
	if p.CurrentTool != "" {
		parts = append(parts, fmt.Sprintf("→ %s", p.CurrentTool))
	}

	// Tokens
	if p.TokensUsed > 0 {
		parts = append(parts, fmt.Sprintf("📊 %s", formatTokens(p.TokensUsed)))
	}

	// Context reset indicator
	if p.ContextResets > 0 {
		parts = append(parts, fmt.Sprintf("🔄 %d", p.ContextResets))
	}

	return strings.Join(parts, " │ ")
}

// formatTokens formats token counts for display.
func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// loadServerMessages converts server messages and populates local state.
func (m *ChatModel) loadServerMessages(sessionID string, serverMsgs []types.SessionMessage) {
	chatMsgs := make([]ChatMessage, 0, len(serverMsgs))
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

	thinSep := m.separatorStyle.Render(strings.Repeat("─", m.width-6))

	// Turn separator: a more visible line with turn number
	turnSepStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#374151"))
	turnLabelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Bold(true)

	selectedStartLine := -1
	currentLine := 0
	turnNumber := 0

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

		// Message threading: user messages and system messages that follow a user
		// message start a new conversation turn. Consecutive assistant/system
		// messages belong to the same turn.
		isNewTurn := false
		switch msg.Role {
		case "user":
			isNewTurn = true
		case "system":
			// System message starts a new turn only if the previous message
			// was not also a system message
			if i == 0 || m.messages[i-1].Role != "system" {
				isNewTurn = true
			}
		}

		// Add turn separator before new turns (except the very first message)
		if isNewTurn && i > 0 {
			turnNumber++
			turnLabel := turnLabelStyle.Render(fmt.Sprintf(" turn %d ", turnNumber))
			sepWidth := max(m.width-6-lipgloss.Width(turnLabel), 10)
			turnSep := turnSepStyle.Render(strings.Repeat("─", sepWidth))
			content.WriteString(turnLabel + turnSep)
			content.WriteString("\n")
			currentLine++
		} else if i > 0 && !isNewTurn {
			// Thin separator between messages in the same turn
			content.WriteString(thinSep)
			content.WriteString("\n")
			currentLine++
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
		switch msg.State {
		case MessageCollapsed:
			indicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true).
				Render("  [collapsed - press e to expand]")
			content.WriteString(indicator)
			content.WriteString("\n")
			currentLine++
		case MessageExpanded:
			indicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true).
				Render("  [expanded]")
			content.WriteString(indicator)
			content.WriteString("\n")
			currentLine++
		}
	}

	m.viewport.SetContent(content.String())

	// Scroll behavior: if a message is selected, keep it visible; otherwise go to bottom
	if m.selectedMsgIdx >= 0 && selectedStartLine >= 0 {
		if selectedStartLine < m.viewport.YOffset() {
			m.viewport.SetYOffset(selectedStartLine)
		} else if selectedStartLine >= m.viewport.YOffset()+m.viewport.Height() {
			m.viewport.SetYOffset(selectedStartLine - m.viewport.Height()/3)
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
	paragraphs := strings.SplitSeq(text, "\n")

	for para := range paragraphs {
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

	// Calculate viewport height to fill available space
	inputLines := max(m.textarea.LineCount(), 3)
	inputLines = min(inputLines, 8)
	// Layout: viewport(2 borders + content) + copy_hint(0-1) + input(2 borders + content) + completions(0-1) + statusbar(1)
	// Account for copy hint line when selection is active
	copyHintLines := 0
	if m.isSelecting && m.hasSelection() {
		copyHintLines = 1
	}
	// Account for completions line only when slash command is being typed
	completionsLines := 0
	if strings.HasPrefix(m.textarea.Value(), "/") {
		completionsLines = 1
	}
	// Account for queue indicator bar (shown when agent active, steer mode, or queue has items)
	queueIndicatorLines := 0
	if m.agentActive || m.steerMode || m.hasQueueItems() {
		queueIndicatorLines = 1
	}
	// viewportContentHeight = height - 2(viewport borders) - copyHintLines - inputLines - 2(input borders) - completionsLines - queueIndicatorLines - 1(statusbar)
	viewportContentHeight := max(m.height-copyHintLines-inputLines-completionsLines-queueIndicatorLines-5, 1)

	// Update viewport dimensions BEFORE rendering
	m.viewport.SetWidth(m.width - 2)
	m.viewport.SetHeight(viewportContentHeight)

	viewportStyle := viewportBorder.
		Width(m.width - 2).
		Height(viewportContentHeight)

	// Render viewport content
	viewportContent := m.viewport.View()

	// Apply selection highlight if there's an active selection
	if m.isSelecting && m.hasSelection() {
		// Define selection highlight style (reverse video - terminal native)
		selStyle := "\033[7m"
		viewportContent = m.applySelectionHighlight(viewportContent, selStyle)
	}

	b.WriteString(viewportStyle.Render(viewportContent))
	b.WriteString("\n")

	// Show copy hint when there's an active selection
	if m.isSelecting && m.hasSelection() {
		copyHintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#F97316")).
			Padding(0, 1)
		b.WriteString(copyHintStyle.Render(" press 'c' to copy "))
		b.WriteString("\n")
	}

	// Queue status indicator bar (agent active, steer mode, queue depth)
	if m.agentActive || m.steerMode || m.hasQueueItems() {
		b.WriteString(m.renderQueueIndicator())
		b.WriteString("\n")
	}

	// Input textarea with focus-dependent border
	inputBorder := m.unfocusedBorder
	if m.focused == FocusInput {
		inputBorder = m.focusedBorder
	}

	// Build input area: optional attachments line + textarea
	var inputContent strings.Builder

	// Show attached files in orange [filename.ext] format
	if len(m.attachments) > 0 {
		attachStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F97316")) // orange
		attachLabels := make([]string, 0, len(m.attachments))
		for _, path := range m.attachments {
			name := filepath.Base(path)
			attachLabels = append(attachLabels, attachStyle.Render("["+name+"]"))
		}
		inputContent.WriteString(strings.Join(attachLabels, " "))
		inputContent.WriteString("\n")
	}

	// Render input with ghost text completion
	ghostText := m.renderInputWithGhostText()
	if ghostText != "" {
		// Render styled ghost text (textarea will render cursor on top)
		inputContent.WriteString(ghostText)
	} else {
		// No ghost - use normal textarea rendering
		inputView := m.textarea.View()
		if m.historyIdx >= 0 {
			history := m.currentHistory()
			historyIndicator := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true).
				Render(fmt.Sprintf(" [history %d/%d]", m.historyIdx+1, len(history)))
			inputView += historyIndicator
		}
		inputContent.WriteString(inputView)
	}

	inputStyle := inputBorder.Width(m.width - 2)
	b.WriteString(inputStyle.Render(inputContent.String()))

	// Render slash command completions below input (only when / is typed)
	inputValue := m.textarea.Value()
	if strings.HasPrefix(inputValue, "/") {
		commands := builtinCommands()
		filter := strings.TrimPrefix(inputValue, "/")
		filter = strings.TrimSpace(filter)

		// Filter commands
		var matches []string
		for _, cmd := range commands {
			if filter == "" || strings.HasPrefix(cmd, filter) {
				matches = append(matches, cmd)
			}
		}

		if len(matches) > 0 {
			// Show all matching commands (including all when just / is typed)
			// Add newline BEFORE completions line
			b.WriteString("\n")
			completions := "/" + strings.Join(matches, "  /")
			completionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).PaddingLeft(2)
			b.WriteString(completionStyle.Render(completions))
		}
	}

	return b.String()
}

// SetSlashAutocompletePopup sets the autocomplete popup string to render.
func (m *ChatModel) SetSlashAutocompletePopup(popup string) {
	m.slashAutocompletePopup = popup
}

// ClearSlashAutocompletePopup clears the autocomplete popup.
func (m *ChatModel) ClearSlashAutocompletePopup() {
	m.slashAutocompletePopup = ""
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

// GetMode returns the current vim mode as a string for quick actions display.
// Returns "normal", "insert", "visual", or "command" if vim is enabled, otherwise empty string.
func (m *ChatModel) GetMode() string {
	if m.vimState == nil || !m.vimState.Enabled {
		// When vim is disabled, return mode based on focus
		if m.focused == FocusInput {
			return "insert"
		}
		return "normal"
	}
	switch m.vimState.Mode {
	case vim.ModeNormal:
		return "normal"
	case vim.ModeInsert:
		return "insert"
	case vim.ModeVisual:
		return "visual"
	case vim.ModeCommand:
		return "command"
	default:
		return "normal"
	}
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

// expandPasteTokens replaces {paste: N lines} tokens with their original content.
func (m *ChatModel) expandPasteTokens(text string) string {
	if len(m.compressedPastes) == 0 {
		return text
	}

	result := text
	// Expand paste tokens in reverse order of pasteID to handle multiple pastes
	for id := m.pasteCounter; id >= 1; id-- {
		content, exists := m.compressedPastes[id]
		if !exists {
			continue
		}

		// Find and replace the token for this paste
		// Token format: {paste: N lines}
		for lineCount := 100; lineCount >= 3; lineCount-- {
			token := fmt.Sprintf("{paste: %d lines}", lineCount)
			if strings.Contains(result, token) {
				result = strings.Replace(result, token, content, 1)
				break
			}
		}
	}

	return result
}

// handleMousePress handles mouse button press for text selection.
func (m *ChatModel) handleMousePress(msg tea.MouseMsg) tea.Cmd {
	mouse := msg.Mouse()
	m.mouseDown = true
	m.mouseDownY = mouse.Y
	m.mouseDownX = mouse.X

	// Convert screen coordinates to viewport-relative
	adjustedY, adjustedX := m.viewportAdjustedCoords(mouse)

	// Check for double/triple click
	now := time.Now()
	if now.Sub(m.lastClickTime) < 400*time.Millisecond && m.lastClickY == msg.Mouse().Y {
		m.clickCount++
		if m.clickCount == 2 {
			// Double-click: select word
			m.selectWordAt(adjustedY, adjustedX)
			return nil
		} else if m.clickCount >= 3 {
			// Triple-click: select line
			m.selectLineAt(adjustedY)
			m.clickCount = 3 // Cap at 3
			return nil
		}
	} else {
		m.clickCount = 1
	}
	m.lastClickTime = now
	m.lastClickY = msg.Mouse().Y

	// Single click: start selection at cursor position
	m.selectionStart = m.calculateCursorOffset(adjustedY, adjustedX)
	m.selectionEnd = m.selectionStart
	m.isSelecting = true
	return nil
}

// handleMouseDrag handles mouse drag for extending text selection.
func (m *ChatModel) handleMouseDrag(msg tea.MouseMsg) tea.Cmd {
	mouse := msg.Mouse()
	m.mouseDragY = mouse.Y
	m.mouseDragX = mouse.X

	// Convert screen coordinates to viewport-relative
	adjustedY, adjustedX := m.viewportAdjustedCoords(mouse)

	// Clamp to valid range (allow dragging outside viewport to extend selection)
	if adjustedY < 0 {
		adjustedY = 0
	}
	if adjustedX < 0 {
		adjustedX = 0
	}

	m.selectionEnd = m.calculateCursorOffset(adjustedY, adjustedX)
	return nil
}

// handleMouseRelease handles mouse button release. Optionally copies selected text to clipboard.
func (m *ChatModel) handleMouseRelease(_ tea.MouseMsg) tea.Cmd {
	m.mouseDown = false

	// Copy selected text to clipboard only if autoCopyOnRelease is enabled
	if m.autoCopyOnRelease && m.hasSelection() {
		text := m.extractSelectedText()
		if text != "" {
			return func() tea.Msg {
				return CopyToClipboardMsg{Text: text}
			}
		}
	}
	return nil
}

// getTextareaBounds returns the screen Y coordinate range where the textarea input is rendered.
// Returns (startY, endY) where startY is the first line of the textarea content area
// and endY is the last line. If bounds cannot be calculated, returns (-1, -1).
//
// Layout from top of screen:
//   - Header/chrome (screenYOffset): variable
//   - Viewport border top: 1 line
//   - Viewport content: viewport.Height() lines
//   - Viewport border bottom/newline: 1 line
//   - Copy hint (conditional): 0-1 lines
//   - Attachments (conditional): 0-1 lines
//   - Input border top + textarea content: dynamic
//
// Returns screen-relative coordinates suitable for comparing with mouse events.
func (m *ChatModel) getTextareaBounds() (startY, endY int) {
	// Viewport section
	viewportBorderLines := 2 // top and bottom borders/newlines
	viewportContentLines := m.viewport.Height()

	// Conditional lines
	copyHintLines := 0
	if m.isSelecting && m.hasSelection() {
		copyHintLines = 1
	}

	attachmentsLines := 0
	if len(m.attachments) > 0 {
		attachmentsLines = 1
	}

	// Textarea content lines (what we render, not the full textarea.Model height)
	inputLines := max(m.textarea.LineCount(), 3)
	inputLines = min(inputLines, 8)

	// Calculate start Y relative to chat area
	chatRelativeStartY := viewportBorderLines + viewportContentLines + copyHintLines + attachmentsLines

	// Convert to screen-relative by adding the screen offset (header, etc.)
	startY = m.screenYOffset + chatRelativeStartY

	// Calculate end Y: start + textarea content lines - 1 (inclusive)
	endY = startY + inputLines - 1

	return startY, endY
}

// parseAsyncAck checks if a reply is an async task acknowledgment JSON.
// Returns (isAsync, taskID, message).
func parseAsyncAck(reply string) (bool, string, string) {
	// Quick check: must start with { to be JSON
	trimmed := strings.TrimSpace(reply)
	if !strings.HasPrefix(trimmed, "{") {
		return false, "", ""
	}

	var ack struct {
		Async   bool   `json:"async"`
		TaskID  string `json:"task_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(trimmed), &ack); err != nil {
		return false, "", ""
	}
	if !ack.Async {
		return false, "", ""
	}
	return true, ack.TaskID, ack.Message
}

// taskResultInfo holds parsed task completion/failure info.
type taskResultInfo struct {
	IsResult  bool
	IsSuccess bool
	TaskID    string
	Name      string
	Completed int
	Failed    int
	Total     int
	Result    string
	Error     string
}

// parseTaskResult checks if a reply is a task completion/failure JSON.
func parseTaskResult(reply string) *taskResultInfo {
	trimmed := strings.TrimSpace(reply)
	if !strings.HasPrefix(trimmed, "{") {
		return nil
	}

	var result struct {
		TaskCompleted bool   `json:"task_completed"`
		TaskFailed    bool   `json:"task_failed"`
		TaskID        string `json:"task_id"`
		Name          string `json:"name"`
		Completed     int    `json:"completed"`
		Failed        int    `json:"failed"`
		Total         int    `json:"total"`
		Result        string `json:"result"`
		Error         string `json:"error"`
	}

	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		return nil
	}

	if !result.TaskCompleted && !result.TaskFailed {
		return nil
	}

	return &taskResultInfo{
		IsResult:  true,
		IsSuccess: result.TaskCompleted,
		TaskID:    result.TaskID,
		Name:      result.Name,
		Completed: result.Completed,
		Failed:    result.Failed,
		Total:     result.Total,
		Result:    result.Result,
		Error:     result.Error,
	}
}

// detectAndAttachFile checks if new input content contains a file path and
// converts it to an attachment (shown as [filename] in the UI).
func (m *ChatModel) detectAndAttachFile(oldValue, newValue string) {
	// Find what was added
	added := ""
	for i := 0; i < len(oldValue) && i < len(newValue); i++ {
		if oldValue[i] != newValue[i] {
			added = newValue[i:]
			break
		}
	}
	if added == "" && len(newValue) > len(oldValue) {
		added = newValue[len(oldValue):]
	}

	// Trim whitespace and check if it looks like a file path
	candidate := strings.TrimSpace(added)
	if candidate == "" {
		return
	}

	// Expand ~ to home directory
	if strings.HasPrefix(candidate, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			candidate = filepath.Join(home, candidate[2:])
		}
	}

	// Must be an absolute path
	if !filepath.IsAbs(candidate) {
		return
	}

	// Check if file exists and is a regular file
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return
	}

	// Avoid duplicates
	if slices.Contains(m.attachments, candidate) {
		return
	}

	// Add to attachments and remove from textarea
	m.attachments = append(m.attachments, candidate)

	// Remove the path from the textarea, keeping other content
	// Find and remove the added path segment from current textarea value
	currentVal := m.textarea.Value()
	cleaned := strings.Replace(currentVal, added, "", 1)
	cleaned = strings.TrimSpace(cleaned)
	m.textarea.SetValue(cleaned)
}

// GetAttachments returns the list of attached file paths.
func (m *ChatModel) GetAttachments() []string {
	return m.attachments
}

// ClearAttachments removes all attachments.
func (m *ChatModel) ClearAttachments() {
	m.attachments = nil
}

// GetInputValue returns the current value of the input textarea.
func (m *ChatModel) GetInputValue() string {
	return m.textarea.Value()
}

// builtinCommands returns a list of built-in slash command names.
func builtinCommands() []string {
	return []string{
		"help",
		"new",
		"clear",
		"retry",
		"undo",
		"usage",
		"stop",
		"status",
		"vim",
		"session",
		"task",
	}
}

// findBestSlashMatch finds the best matching slash command for the given input.
// Returns the full command string (e.g., "/help") or empty string if no match.
func findBestSlashMatch(input string) string {
	if !strings.HasPrefix(input, "/") {
		return ""
	}

	commands := builtinCommands()
	inputLower := strings.ToLower(strings.TrimPrefix(input, "/"))

	if inputLower == "" {
		// No filter - return first command (will be "/help")
		if len(commands) > 0 {
			return "/" + commands[0]
		}
		return ""
	}

	// Find exact prefix match
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, inputLower) {
			return "/" + cmd
		}
	}

	return ""
}

// renderInputWithGhostText renders the input textarea with ghost text completion.
// The typed portion is orange, ghost portion is grey.
// Returns the styled text without cursor - textarea handles cursor natively.
func (m *ChatModel) renderInputWithGhostText() string {
	inputValue := m.textarea.Value()

	// Check if we should show ghost completion (slash commands at start)
	if !strings.HasPrefix(inputValue, "/") {
		return "" // Let textarea render normally
	}

	// Only show ghost if input is a prefix of a command (not complete)
	bestMatch := findBestSlashMatch(inputValue)
	if bestMatch == "" || bestMatch == inputValue {
		return "" // No ghost - let textarea render normally
	}

	// Build styled input: orange for typed, grey for ghost
	orangeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F97316"))
	greyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	typedPortion := inputValue
	ghostPortion := bestMatch[len(inputValue):]

	return orangeStyle.Render(typedPortion) + greyStyle.Render(ghostPortion)
}

// SetInputValue sets the value of the input textarea.
func (m *ChatModel) SetInputValue(value string) {
	m.textarea.SetValue(value)
}

// RetryLast removes the last assistant message and re-sends the last user message.
// Returns true if retry was performed, false if no user message to retry.
func (m *ChatModel) RetryLast() bool {
	if len(m.messages) == 0 {
		return false
	}

	// Find the last user message
	lastUserIdx := -1
	for i, v := range slices.Backward(m.messages) {
		if v.Role == "user" {
			lastUserIdx = i
			break
		}
	}

	if lastUserIdx == -1 {
		return false
	}

	// Get the last user message content
	lastUserContent := m.messages[lastUserIdx].Content

	// Remove messages from the last user message onwards
	m.messages = m.messages[:lastUserIdx]

	// Update viewport and re-send the message
	m.updateViewport()

	// Set the input to the last user message content for re-sending
	m.textarea.SetValue(lastUserContent)

	return true
}

// GetLastUserMessage returns the content of the last user message, or empty string if none.
func (m *ChatModel) GetLastUserMessage() string {
	for _, v := range slices.Backward(m.messages) {
		if v.Role == "user" {
			return v.Content
		}
	}
	return ""
}

// UndoLast removes the last user message and its corresponding assistant response.
// Returns true if undo was performed, false if no exchange to remove.
func (m *ChatModel) UndoLast() bool {
	if len(m.messages) == 0 {
		return false
	}

	// Find the last user message
	lastUserIdx := -1
	for i, v := range slices.Backward(m.messages) {
		if v.Role == "user" {
			lastUserIdx = i
			break
		}
	}

	if lastUserIdx == -1 {
		return false
	}

	// Remove all messages from the last user message onwards
	m.messages = m.messages[:lastUserIdx]
	m.updateViewport()

	return true
}

// ClearConversation removes all messages from the conversation.
func (m *ChatModel) ClearConversation() {
	m.messages = []ChatMessage{}
	m.dirtyMessages = make(map[string][]ChatMessage)
	m.sessionMessages = make(map[string][]ChatMessage)
	m.updateViewport()
}

// GetMessages returns a copy of the messages slice.
func (m *ChatModel) GetMessages() []ChatMessage {
	result := make([]ChatMessage, len(m.messages))
	copy(result, m.messages)
	return result
}

// ============================================================================
// Steering and Follow-Up Queue Methods
// ============================================================================

// SetAgentActive updates the agent active state.
func (m *ChatModel) SetAgentActive(active bool, conversationID string) {
	if conversationID == "" || conversationID == m.conversationID {
		m.agentActive = active
		if !active {
			m.steerMode = false
			m.queueStatus = nil
			m.AddSystemMessage("[agent] idle")
		} else {
			m.AddSystemMessage("[agent] active")
		}
	}
}

// UpdateQueueStatus updates the queue status from an event or RPC call.
func (m *ChatModel) UpdateQueueStatus(status *types.QueueStatusResponse) {
	m.queueStatus = status
}

// IsAgentActive returns whether the agent is currently processing.
func (m *ChatModel) IsAgentActive() bool {
	return m.agentActive
}

// IsSteerMode returns the current steer mode state.
func (m *ChatModel) IsSteerMode() bool {
	return m.steerMode
}

// ToggleSteerMode flips steer mode and returns the new state.
func (m *ChatModel) ToggleSteerMode() bool {
	m.steerMode = !m.steerMode
	return m.steerMode
}

// SetSteerMode sets steer mode to the given state.
func (m *ChatModel) SetSteerMode(active bool) {
	m.steerMode = active
}

// SteerQueue sends the given text as a steering message to the daemon.
// It also adds a local user message so the steering action is visible in chat.
func (m *ChatModel) SteerQueue(text string) tea.Cmd {
	return func() tea.Msg {
		err := m.rpc.Steer(text, m.conversationID)
		if err != nil {
			return SteeringResultMsg{Err: err}
		}
		return SteeringResultMsg{Success: true}
	}
}

// FollowUpQueue sends the given text as a follow-up message to the daemon.
// It does not add a local user message (follow-ups are not shown in chat).
func (m *ChatModel) FollowUpQueue(text string) tea.Cmd {
	return func() tea.Msg {
		err := m.rpc.FollowUp(text, m.conversationID)
		if err != nil {
			return FollowUpResultMsg{Err: err}
		}
		return FollowUpResultMsg{Success: true}
	}
}

// hasQueueItems returns true if any queue has pending items.
func (m *ChatModel) hasQueueItems() bool {
	if m.queueStatus == nil {
		return false
	}
	return m.queueStatus.SteeringDepth > 0 || m.queueStatus.FollowUpDepth > 0
}

// renderQueueIndicator renders a single-line indicator bar showing agent
// activity, steer mode, and queue depth as styled badges joined horizontally.
func (m *ChatModel) renderQueueIndicator() string {
	var badges []string

	if m.agentActive {
		badges = append(badges, m.agentActiveBadgeStyle.Render("agent active"))
	}

	if m.steerMode {
		badges = append(badges, m.steerBadgeStyle.Render("steer mode"))
	}

	if m.queueStatus != nil {
		if m.queueStatus.SteeringDepth > 0 {
			badges = append(badges, m.steerBadgeStyle.Render(fmt.Sprintf("steer: %d", m.queueStatus.SteeringDepth)))
		}
		if m.queueStatus.FollowUpDepth > 0 {
			badges = append(badges, m.followUpBadgeStyle.Render(fmt.Sprintf("follow-up: %d", m.queueStatus.FollowUpDepth)))
		}
	}

	if len(badges) == 0 {
		return ""
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, badges...)
}
