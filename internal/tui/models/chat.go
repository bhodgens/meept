// Package models provides the view models for the TUI.
package models

import (
	"encoding/json"
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
	Progress  *ProgressState // Progress state for pending messages

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

	// Paste compression
	compressedPastes map[int]string // pasteID -> original paste content
	pasteCounter     int
	lastInputValue   string // For detecting pastes
	lastInputLines   int    // Line count at last check

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

	// Fixed input height (no auto-expand)
	inputHeight int

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
	GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error)
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
	})
}

// NewChatModelWithConfig creates a new chat model with input configuration.
func NewChatModelWithConfig(rpc RPCClient, userStyle, assistantStyle, systemStyle lipgloss.Style, escapeBehavior string, inputConfig InputBehaviorConfig) *ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.Focus()
	ta.CharLimit = 4000
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	// ALWAYS disable InsertNewline - we handle both Enter and Shift+Enter manually
	// This prevents bubbles/textarea from intercepting Enter before our Update() handler
	ta.KeyMap.InsertNewline.SetEnabled(false)

	vp := viewport.New(80, 20)
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
		renderMarkdown:   true, // Enable markdown by default
		vimState:         vimState,
		escapeBehavior:   escapeBehavior,
		inputHeight:      3, // Fixed input height
		compressedPastes: make(map[int]string),
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
	}
}

func generateConversationID() string {
	return fmt.Sprintf("conv-%d", time.Now().UnixNano())
}

// SetSize updates the model dimensions.
func (m *ChatModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Fixed input height (5 lines total with border)
	const inputContentHeight = 3

	// Layout: viewport(+border) followed immediately by input(+border).
	// View() joins them with a single "\n" which is a line separator, NOT
	// an extra blank row. So total chrome = viewport border(2) + input border(2)
	// + input content(inputContentHeight) = 4 + inputContentHeight.
	// viewportContent = height - inputContent - 4
	viewportHeight := height - inputContentHeight - 4
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	m.inputHeight = inputContentHeight
	m.textarea.SetWidth(width - 4) // Account for border padding
	m.textarea.SetHeight(inputContentHeight)
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
		// Only handle mouse wheel scrolling in viewport. Text selection is
		// handled by the terminal natively (toggle with Ctrl+M) so we do not
		// consume left-button events here - that avoids distortion from custom
		// highlight rendering and unwanted auto-copy on release.
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.viewport.LineUp(3)
			return nil
		case tea.MouseButtonWheelDown:
			m.viewport.LineDown(3)
			return nil
		}
		return nil

	case tea.KeyMsg:
		// Clear any active text selection when user starts typing
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

		case "shift+up":
			if m.focused == FocusViewport {
				m.viewport.HalfViewUp()
				return nil
			}

		case "shift+down":
			if m.focused == FocusViewport {
				m.viewport.HalfViewDown()
				return nil
			}

		case "pgup":
			if m.focused == FocusViewport {
				m.viewport.ViewUp()
				return nil
			}

		case "pgdown":
			if m.focused == FocusViewport {
				m.viewport.ViewDown()
				return nil
			}

		case "j":
			// Vim-style: scroll viewport or select message
			if m.focused == FocusViewport {
				m.viewport.LineDown(1)
				return nil
			}

		case "k":
			// Vim-style: scroll viewport
			if m.focused == FocusViewport {
				m.viewport.LineUp(1)
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

// doSendMessage handles the common logic for sending a message.
func (m *ChatModel) doSendMessage() tea.Cmd {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return nil
	}

	// Expand paste tokens to get actual message content
	actualText := m.expandPasteTokens(text)

	// Add to history buffer (with tokens for display)
	m.addToHistory(text)

	m.textarea.Reset()

	// Clear compressed pastes after sending
	m.compressedPastes = make(map[int]string)
	m.pasteCounter = 0

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

	// Chat history viewport with focus-dependent border
	viewportBorder := m.unfocusedBorder
	if m.focused == FocusViewport {
		viewportBorder = m.focusedBorder
	}

	// Height sets inner content height; border adds 2 more lines
	viewportStyle := viewportBorder.
		Width(m.width - 2).
		Height(m.viewport.Height)

	// Render the viewport content without custom selection highlighting.
	// Custom highlighting previously stripped all ANSI codes from the content,
	// which destroyed message styling and caused visible distortion during drag.
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

// viewportBorderOffset is the offset from screen Y to viewport content Y.
// The viewport bounds in app.go already account for the header, and the viewport's
// View() output is what gets hit-tested, so no additional offset is needed.
const viewportBorderOffset = 0

// handleMousePress handles mouse button press for text selection.
func (m *ChatModel) handleMousePress(msg tea.MouseMsg) tea.Cmd {
	m.mouseDown = true
	m.mouseDownY = msg.Y
	m.mouseDownX = msg.X

	// Adjust coordinates for viewport border
	// The viewport content starts 1 line down (after top border) and 1 char in (after left border)
	adjustedY := msg.Y - viewportBorderOffset
	adjustedX := msg.X - 1 // left border offset

	// Ignore clicks on the border itself
	if adjustedY < 0 || adjustedX < 0 {
		m.mouseDown = false
		return nil
	}

	// Check for double/triple click
	now := time.Now()
	if now.Sub(m.lastClickTime) < 400*time.Millisecond && m.lastClickY == msg.Y {
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
	m.lastClickY = msg.Y

	// Single click: start selection at cursor position
	m.selectionStart = m.calculateCursorOffset(adjustedY, adjustedX)
	m.selectionEnd = m.selectionStart
	m.isSelecting = true
	return nil
}

// handleMouseDrag handles mouse drag for extending text selection.
func (m *ChatModel) handleMouseDrag(msg tea.MouseMsg) tea.Cmd {
	m.mouseDragY = msg.Y
	m.mouseDragX = msg.X

	// Adjust coordinates for viewport border
	adjustedY := msg.Y - viewportBorderOffset
	adjustedX := msg.X - 1

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

// handleMouseRelease handles mouse button release. Text is NOT automatically
// copied on release; selection is handled natively by the terminal.
func (m *ChatModel) handleMouseRelease(msg tea.MouseMsg) tea.Cmd {
	m.mouseDown = false
	return nil
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
