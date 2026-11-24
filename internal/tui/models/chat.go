// Package models provides the view models for the TUI.
package models

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"slices"

	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/stt"
	"github.com/caimlas/meept/internal/tts"
	"github.com/caimlas/meept/internal/tui/render"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/internal/tui/vim"
	"github.com/caimlas/meept/pkg/id"
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

// sttState represents the speech-to-text recording state machine.
type sttState int

const (
	sttIdle sttState = iota
	sttRecording
	sttTranscribing
)

// doubleEnterTTL is the maximum interval between two Enter presses to
// be considered a double-enter gesture.
const doubleEnterTTL = 500 * time.Millisecond

// ChatMessage represents a single chat message.
type ChatMessage struct {
	ID           string // Stable identity for quote references ("msg-..."); empty for local-only lines
	Role         string // "user", "assistant", "system", "participant", or "pending"
	Content      string
	SourceClient string // Client identifier for participant messages (e.g. "claude", "tui")
	Timestamp    time.Time
	State        MessageState
	Progress     *ProgressState // Progress state for pending messages

	// C1 terminal correlation fields (master C1; 05-navigation T1). The
	// daemon's turn.terminal payload carries all four
	// (internal/agent/handler.go TurnTerminalEvent); the TUI keep-list
	// copies them onto the rendered assistant bubble so the task chip,
	// the (turn_id, task_id) dedupe ledger, and the why-this-route
	// popover all read identity from the message itself. Empty for
	// messages that did not come from a task-bearing terminal.
	TaskID       string
	IntentType   string
	HandlerCase  string
	ClassifiedBy string

	// Rendering cache
	rendered   string // Cached rendered output
	renderedAt int    // Width when rendered

	// localOnly marks a transcript line that exists only in this client
	// (system notices like the C6 dropped-event warning, restore
	// markers). The daemon never stores it, and session loads/switces
	// rebuild the transcript from the server -- localOnly lines are
	// re-appended across that rebuild so the warning is not silently
	// lost (C6/W26; 05-navigation T4).
	localOnly bool

	// gateTaskID marks a task-gate approval card and keys it to its task
	// (bughunt wave2, finding 2): TaskGateResolvedMsg removes every card
	// stamped with the resolved task id so the pending card cannot survive
	// the approve/reject decision. Empty for all other messages.
	gateTaskID string
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
	pendingMsgIdx int // index of the "sending..." message, -1 if none

	// Async turn tracking (async-turn-migration leaf 04): turns submitted
	// via chat.submit that are awaiting their turn.terminal event.
	turns            *turnRouter
	pendingTurns     []*pendingTurn
	livenessTimeout  time.Duration // stalled-turn window; 0 = disabled
	pendingTurnOwner string        // turn id owning the shared pending line slot

	// Early terminal events (F19): turn.terminal events that arrived
	// before the submit ack registered their turn. Consumed on
	// registration; a tiny ephemeral map keyed by turn id.
	earlyTerminals *turnRouterEarlyBuffer

	// Task-reply read surfaces (05-navigation T2/T3; C4/C5). Keyed by
	// ChatMessage.ID, lazily populated by the 'e' trace expand (C4:
	// session.messages.get include_tool_calls) and the 'w' route
	// popover (C5: session.dispatch_trace). The expand commands mutate
	// these maps then emit a refresh msg; the View reads them. Fetched
	// once per message per session (no per-render RPC storm).
	messageTraces map[string][]toolCallTrace
	routeEvidence map[string][]dispatchTraceRow
	traceFetched  map[string]bool
	routeFetched  map[string]bool

	// plansViewKey is the configured plans-view keybinding
	// (client.json5 command_palette.view_plans; 09-cli-affordances
	// task 3). Zero value renders the shipped default "5" in the plan
	// card's hint line — the hint used to hardcode "[5]" even when the
	// operator rebound the key.
	plansViewKey string

	// Task-reply render dedupe (04-clutter/02, C1 key = (turn_id,
	// task_id)): a task's reply arrives as BOTH a task.completed card AND
	// the turn.terminal bubble -- whichever renders first wins, the
	// second is suppressed. renderedTaskIDs holds task ids whose reply
	// already rendered; lastTerminalReply holds the last terminal bubble
	// text (interim card-match until the wire carries task_id);
	// turnCardReplies binds a card-first summary to its pending turn so
	// the following terminal is recognized. See chat_dedupe.go.
	renderedTaskIDs   map[string]bool
	lastTerminalReply string
	turnCardReplies   map[string]string

	// Progress state for the current pending message
	progressState *ProgressState

	// Per-session input history
	history    *sharedclient.SessionHistory
	historyIdx int    // current position in history for display (>=0 = browsing, -1 = not browsing)
	savedInput string // saved current input when browsing history

	// Message persistence - buffered writes
	dirtyMessages map[string][]ChatMessage // unsaved messages per session
	flushPending  bool

	// Session header data
	sessionDescription string
	// descriptionGen counts auto-naming attempts. Every maybeGenerateDescription
	// bump stamps its async result; a result landing after a newer attempt (or
	// after a user rename, which the handler detects via the placeholder) is
	// dropped instead of overwriting the newer state (L16 race).
	descriptionGen uint64

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
	attachments []attachmentEntry

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
	agentActive bool                       // true while agent is processing
	queueStatus *types.QueueStatusResponse // latest queue state (nil if agent idle)
	steerMode   bool                       // when true, next message is a steer (ctrl+s toggle)

	// In-session find bar (ctrl+f)
	findBarVisible      bool
	findInput           textinput.Model
	findMatches         []findMatch
	findCursor          int // index into findMatches, -1 if none
	findCaseSensitive   bool
	findRegex           bool
	findRegexError      string
	findDebouncePending bool

	// Styles
	userStyle             lipgloss.Style
	assistantStyle        lipgloss.Style
	systemStyle           lipgloss.Style
	errorStyle            lipgloss.Style // error bubbles: distinct from system notices
	pendingStyle          lipgloss.Style
	separatorStyle        lipgloss.Style
	focusedBorder         lipgloss.Style
	unfocusedBorder       lipgloss.Style
	headerStyle           lipgloss.Style
	steerBadgeStyle       lipgloss.Style
	followUpBadgeStyle    lipgloss.Style
	agentActiveBadgeStyle lipgloss.Style

	// Speech-to-text state
	recordingState sttState        // current state in the STT state machine
	transcriber    stt.Transcriber // nil if STT disabled or unavailable
	sttConfig      stt.Config
	sttAvailable   bool      // true if engine dependencies found
	sttEnabled     bool      // true if stt is enabled in config
	sttAutoSend    bool      // true if transcription results should auto-send
	lastEnterTime  time.Time // tracks double-enter detection for STT activation

	// Text-to-speech state
	ttsManager *tts.Manager // nil if TTS disabled or unavailable
	ttsEnabled bool         // true if tts is enabled in config

	// Thread indicator (compact header display, no interactive list at this layer)
	threadIndicator *threadIndicatorState

	// sessionLoadPending tracks an in-flight session.messages.get load:
	// set by SetSession when it arms the load cmd, cleared when the
	// SessionMessagesLoadedMsg lands. IsSessionLoadSettled exposes it so
	// the event pump can defer daemon-state cards until the authoritative
	// transcript rebuild has happened (06-planning/01: a task-gate card
	// appended before the load lands is wiped by it).
	sessionLoadPending bool

	// sessionLoadStarted is stamped when a session.messages.get load is
	// armed (SetSession). A late load result must never erase activity
	// that happened AFTER the snapshot was taken (06-planning/03): a
	// turn submitted while the load was in flight carries
	// fresher-than-snapshot local state (the user's message and the
	// resolved terminal bubble); rebuilding from the stale snapshot
	// would silently drop the reply the user just saw.
	sessionLoadStarted time.Time
}

// findMatch points to a span within a rendered message.
type findMatch struct {
	messageIdx int // index into m.messages
	charStart  int // byte offset in m.messages[messageIdx].Content
	charEnd    int // exclusive end
}

// findDebounceDuration is how long to wait after the last keystroke before recomputing matches.
const findDebounceDuration = 50 * time.Millisecond

// findMaxMatches caps the number of matches to avoid pathological regex backtracking.
const findMaxMatches = 1000

// RPCClient interface for the chat model.
type RPCClient interface {
	Chat(ctx context.Context, message, conversationID string) (string, error)
	ChatWithParts(ctx context.Context, message, conversationID string, parts []llm.ContentPart) (string, error)
	// SubmitChat fires the fire-and-forget "chat.submit" RPC (async-turn-
	// migration leaf 04) and returns the immediate ack — never agent work.
	SubmitChat(ctx context.Context, message, sessionID string, parts []llm.ContentPart) (TurnSubmitAck, error)
	UploadFile(ctx context.Context, filePath string) (string, error)
	IsConnected() bool
	SaveSessionMessages(sessionID string, msgs []types.SessionMessage) error
	GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error)
	UpdateSessionDescription(sessionID, description string) error
	GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error)
	Steer(message, conversationID string) error
	FollowUp(message, conversationID string) error
	GetQueueStatus(conversationID string) (*types.QueueStatusResponse, error)
	Call(method string, params any) (json.RawMessage, error)
}

// ChatConfig holds chat viewport behavior settings.
type ChatConfig struct {
	AutoCopyOnRelease bool // Whether to auto-copy text selection on mouse release
	ScrollSpeed       int  // Lines to scroll per mouse wheel event
	// LivenessTimeout is the stalled-turn window for async turns
	// (async-turn-migration leaf 04). Zero disables the stalled indicator.
	LivenessTimeout time.Duration
}

// InputBehaviorConfig holds input textarea behavior settings.
type InputBehaviorConfig struct {
	EnterBehavior string // "shift_sends" or "double_enter"
	AutoExpand    bool   // Enable auto-expanding input height
}

// attachmentEntry tracks a single file attached to the chat input.
// Image attachments are uploaded to the daemon's UploadService and referenced
// by UploadID in ContentPart.ImageRef.URL. Non-image attachments keep only
// their Path so they can be rendered as "[Attached file: <path>]" context.
type attachmentEntry struct {
	Path     string // original filesystem path (for display)
	UploadID string // populated for image uploads; "" for non-images
	IsImage  bool
	Filename string
}

// uploadResultMsg carries the result of an asynchronous image upload. It is
// produced by a tea.Cmd returned from detectAndAttachFile and handled in Update
// to either attach the UploadID to the pending attachment or demote it to a
// path-only reference on failure.
type uploadResultMsg struct {
	path     string
	uploadID string
	err      error
}

// imageExtensions are file extensions treated as image uploads. Files with
// these extensions are uploaded via the daemon and converted to
// ContentPart{Type:"image_url"} entries on send.
var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
}

// isImageFile returns true if the path has an image file extension.
func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return imageExtensions[ext]
}

// NewChatModelWithConfig creates a new chat model with input configuration.
func NewChatModelWithConfig(rpc RPCClient, userStyle, assistantStyle, systemStyle lipgloss.Style, escapeBehavior string, inputConfig InputBehaviorConfig, chatConfig ChatConfig) *ChatModel {
	ta := textarea.New()
	ta.Placeholder = "type a message..."
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
	orange := paletteColor("primary")
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

	// Initialize find bar input
	findInput := textinput.New()
	findInput.Prompt = ""
	findInput.Placeholder = "find..."
	findInput.CharLimit = 200
	findStyles := findInput.Styles()
	findStyles.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")) // palette-exempt: max-contrast literal
	findStyles.Focused.Prompt = lipgloss.NewStyle().Foreground(paletteColor("primary"))
	findStyles.Blurred.Text = lipgloss.NewStyle().Foreground(paletteColor("textMuted"))
	findStyles.Blurred.Prompt = lipgloss.NewStyle().Foreground(paletteColor("primary"))
	findInput.SetStyles(findStyles)

	return &ChatModel{
		rpc:               rpc,
		messages:          []ChatMessage{},
		viewport:          vp,
		textarea:          ta,
		conversationID:    generateConversationID(),
		focused:           FocusInput,
		selectedMsgIdx:    -1,
		pendingMsgIdx:     -1,
		turns:             newTurnRouter(nil),
		earlyTerminals:    newTurnRouterEarlyBuffer(),
		pendingTurns:      nil,
		livenessTimeout:   chatConfig.LivenessTimeout,
		historyIdx:        -1,
		sessionMessages:   make(map[string][]ChatMessage),
		history:           sharedclient.NewSessionHistory(maxHistorySize),
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
		errorStyle: lipgloss.NewStyle().
			Foreground(paletteColor("error")).
			Bold(true).
			PaddingLeft(2),
		pendingStyle: lipgloss.NewStyle().
			Foreground(paletteColor("warning")).
			Italic(true).
			PaddingLeft(2),
		separatorStyle: lipgloss.NewStyle().
			Foreground(paletteColor("textMuted")),
		focusedBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(paletteColor("primary")),
		unfocusedBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(paletteColor("border")),
		headerStyle: lipgloss.NewStyle().
			Background(paletteColor("primary")).
			Foreground(lipgloss.Color("#000000")). // palette-exempt: max-contrast literal
			Bold(true).
			Padding(0, 1),
		steerBadgeStyle: lipgloss.NewStyle().
			Background(paletteColor("error")).
			Foreground(lipgloss.Color("#FFFFFF")). // palette-exempt: max-contrast literal
			Padding(0, 1).
			Bold(true),
		followUpBadgeStyle: lipgloss.NewStyle().
			Background(paletteColor("success")).
			Foreground(lipgloss.Color("#FFFFFF")). // palette-exempt: max-contrast literal
			Padding(0, 1),
		agentActiveBadgeStyle: lipgloss.NewStyle().
			Background(paletteColor("textMuted")).
			Foreground(lipgloss.Color("#FFFFFF")). // palette-exempt: max-contrast literal
			Padding(0, 1).
			Bold(true),
		findInput:   findInput,
		findMatches: nil,
		findCursor:  -1,

		threadIndicator: newThreadIndicatorState(),
	}
}

// InitSTT initializes speech-to-text support from the given configuration.
// This must be called after NewChatModelWithConfig if STT is desired.
// If enabled is false or the engine is unavailable, STT features are disabled.
func (m *ChatModel) InitSTT(cfg stt.Config, enabled, autoSend bool) {
	m.sttEnabled = enabled
	m.sttAutoSend = autoSend
	m.sttConfig = cfg
	m.recordingState = sttIdle

	if !enabled {
		return
	}

	// Check if the engine is available
	if err := stt.CheckAvailable(cfg); err != nil {
		slog.Warn("stt engine unavailable", "engine", cfg.Engine, "error", err)
		m.sttAvailable = false
		return
	}

	// Create the transcriber
	t, err := stt.NewTranscriber(cfg)
	if err != nil {
		slog.Warn("stt transcriber creation failed", "engine", cfg.Engine, "error", err)
		m.sttAvailable = false
		return
	}

	m.transcriber = t
	m.sttAvailable = true
}

// InitTTS initializes text-to-speech support from the given configuration.
// This must be called after NewChatModelWithConfig if TTS is desired.
// If enabled is false, TTS features are disabled.
func (m *ChatModel) InitTTS(mgr *tts.Manager, enabled bool) {
	m.ttsEnabled = enabled
	m.ttsManager = mgr
}

// ToggleTTS toggles text-to-speech on/off and returns the new state.
func (m *ChatModel) ToggleTTS() bool {
	m.ttsEnabled = !m.ttsEnabled
	if m.ttsManager != nil && !m.ttsEnabled {
		_ = m.ttsManager.Stop() // best-effort: TTS stop failure must not block the toggle
	}
	return m.ttsEnabled
}

// IsTTSEnabled returns whether TTS is enabled.
func (m *ChatModel) IsTTSEnabled() bool {
	return m.ttsEnabled
}

// SessionID returns the current session ID.
func (m *ChatModel) SessionID() string {
	return m.sessionID
}

func generateConversationID() string {
	return id.Generate("conv-")
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

	// Size find input (bar takes 1 line; leave room for count + toggles).
	if width > 40 {
		m.findInput.SetWidth(width / 2)
	} else {
		m.findInput.SetWidth(max(width-20, 10))
	}

	// Update markdown renderer width
	if m.mdRenderer != nil {
		if err := m.mdRenderer.SetWidth(width - 8); err != nil { // Account for padding
			slog.Debug("markdown renderer SetWidth failed", "width", width-8, "error", err)
		}
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
		m.addMessage(RoleSystem, "welcome to meept! type a message to begin.")
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
	// Generation stamps the auto-naming attempt that produced this message
	// (0 = a user rename or other non-auto source). The chat model drops
	// auto results whose generation no longer matches the latest attempt.
	Generation uint64
	// Skip marks an auto result that must NOT touch the description (a user
	// rename intervened while generation was in flight).
	Skip bool
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
	AgentID     string
	Stage       string
	Percent     float64
	CurrentTool string
	// ACPAgent / Persona carry the external-harness attribution from
	// agent.progress frames published with stage=acp
	// (internal/agent/acp_executor.go publishACPUpdate: payload keys
	// "acp_agent" + "persona"). Empty on non-acp frames and on frames
	// whose spec carries no binding — the renderer then falls back to
	// the plain agent shape.
	ACPAgent      string
	Persona       string
	TokensUsed    int
	ContextResets int
	ChatVisible   bool
	// Tool-level streaming progress fields
	ToolName    string
	ToolMessage string
	ToolPercent int
}

// IsChatVisible returns true if this progress update should display in chat.
func (m ProgressUpdateMsg) IsChatVisible() bool {
	return m.ChatVisible
}

// ProgressTickMsg is a 1-second tick that re-renders the pending progress
// message so the elapsed timer stays live while the agent is thinking.
type ProgressTickMsg struct{}

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

// TaskGateApprovalMsg carries a strategic task-gate approval card for the
// chat transcript (06-planning/01, F13a). Produced by the App event pump
// from a task.pending_approval bus event; the card renders the task id,
// the step count, the pending step summaries, and approve/reject
// affordances wired to /plan approve|reject (which forward to
// task.approve / task.reject on the C2 contract - reject never executes).
// The event pump scopes delivery to the owning session, so anything that
// reaches this message belongs to the transcript showing it.
type TaskGateApprovalMsg struct {
	TaskID    string
	SessionID string
	Total     int
	Steps     []string
}

// TaskGateResolvedMsg resolves a pending gate card in the transcript
// (bughunt wave2, finding 2). Produced by the App pump from the daemon's
// task.approved / task.rejected bus events (internal/agent/strategic.go
// ApprovePlan/RejectPlan). The chat card's contract comment (chat.go
// TaskGateApprovalMsg case) says the card stays pending until exactly
// these events resolve it. The card message is REMOVED — the daemon's
// gate state is authoritative, and a stale pending card after the
// decision is what this fixes. A short system line records the outcome.
type TaskGateResolvedMsg struct {
	TaskID   string
	Approved bool
	Reason   string // rejection reason, when known
}

// PlanNotificationMsg carries plan lifecycle notifications for inline chat display.
type PlanNotificationMsg struct {
	PlanID     string
	Title      string
	State      string // "submitting", "completed", "confirmed", "rejected"
	PhaseCount int
	StepCount  int
	By         string // who confirmed/rejected
	Reason     string // rejection reason
	// SessionID is the session the plan belongs to. Empty means the plan
	// belongs to NO chat (machine-originated evolver sink plan): the
	// event pump drops those instead of rendering them into whatever
	// conversation happens to be open (observed live 2026-10-22: nine
	// "Skill evolution" boxes flooded an unrelated review turn).
	SessionID string
	Timestamp time.Time
}

// SteeringInjectedMsg signals that a steering message was injected into the queue.
type SteeringInjectedMsg struct{}

// FollowUpInjectedMsg signals that a follow-up message was injected into the queue.
type FollowUpInjectedMsg struct{}

// FollowUpRestoredMsg signals that pending follow-ups were restored.
type FollowUpRestoredMsg struct {
	Count int
}

// STTResultMsg carries the transcription result from the stt engine.
type STTResultMsg struct {
	Text string
	Err  error
}

// ChatToastMsg requests the parent app to show a toast notification.
type ChatToastMsg struct {
	Title   string
	Message string
}

// ProgressState holds current progress for display in chat.
type ProgressState struct {
	AgentID     string
	Stage       string
	Percent     float64
	CurrentTool string
	// ACPAgent / Persona: harness attribution for stage=acp progress
	// frames (see ProgressUpdateMsg). Empty means no acp tag.
	ACPAgent      string
	Persona       string
	TokensUsed    int
	ContextResets int
	LastUpdate    time.Time
	// StartedAt is when the pending operation began, used to render an
	// elapsed timer ("thinking 12s..."). Zero means unknown.
	StartedAt time.Time
}

// Render returns the formatted progress string for display.
func (p *ProgressState) Render() string {
	if p == nil {
		return "sending..."
	}

	var parts []string

	// Agent emoji + name (with acp harness attribution when the frame
	// carries one; see acpAttribution).
	if p.AgentID != "" {
		if label, ok := acpAttribution(p.AgentID, p.ACPAgent, p.Persona); ok {
			parts = append(parts, fmt.Sprintf("🤖 %s", label))
		} else {
			agentDisplay := p.AgentID
			if len(agentDisplay) > 12 {
				agentDisplay = agentDisplay[:12]
			}
			parts = append(parts, fmt.Sprintf("🤖 %s", agentDisplay))
		}
	}

	// Progress bar
	if p.Percent > 0 {
		barWidth := 20
		filled := int(p.Percent / 100 * float64(barWidth))
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		parts = append(parts, fmt.Sprintf("[%s %.0f%%]", bar, p.Percent))
	} else if p.Stage != "" {
		parts = append(parts, formatStageWithTimer(p.Stage, p.StartedAt))
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
		return "processing..."
	}

	return strings.Join(parts, " │ ")
}

// acpAttribution renders the harness attribution label for a stage=acp
// progress frame: "<agent>[/<persona>] (acp: <harness>)", lowercase per the
// repo UI-text rule, mirroring internal/acp's "agent/persona" display
// convention (renderDecisionState). The inputs are the payload keys
// published by internal/agent/acp_executor.go publishACPUpdate:
// "agent_id", "acp_agent" (catalog harness id) and "persona" (optional).
// ok=false when the frame carries no harness id — i.e. it is not an ACP
// frame — and callers keep today's plain agent-name shape. A present but
// empty persona shows the agent name alone (no "/").
func acpAttribution(agentID, acpAgent, persona string) (string, bool) {
	if agentID == "" || acpAgent == "" {
		return "", false
	}
	actor := truncateRunes(strings.ToLower(strings.TrimSpace(agentID)), 12)
	if p := strings.ToLower(strings.TrimSpace(persona)); p != "" {
		actor += "/" + truncateRunes(p, 12)
	}
	return fmt.Sprintf("%s (acp: %s)", actor, truncateRunes(strings.ToLower(strings.TrimSpace(acpAgent)), 16)), true
}

// truncateRunes shortens s to at most n runes (rune-safe, unlike the
// byte-slice truncation the plain agent-name path predates).
func truncateRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
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
			Role:      RoleSystem,
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
	// If recording, cancel and discard audio
	if m.recordingState == sttRecording {
		m.cancelRecording()
		return nil
	}
	// If transcribing, cancel and return to idle
	if m.recordingState == sttTranscribing {
		m.recordingState = sttIdle
		return nil
	}
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
		if m.history != nil && m.sessionID != "" {
			m.history.Reset(m.sessionID)
		}
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
	return m.history.GetEntries(m.sessionID)
}

// addToHistory adds a message to the input history buffer for the current session.
func (m *ChatModel) addToHistory(text string) {
	if text == "" || m.sessionID == "" {
		return
	}

	m.history.Add(m.sessionID, text)
	// Reset history browsing position
	m.historyIdx = -1
	m.savedInput = ""
}

// RecordToHistory records raw input text (e.g. a slash command) into the
// current session's input history so it can be recalled with Up-arrow. It is
// a thin wrapper around addToHistory that callers outside the models package
// can use without depending on internal history mechanics.
func (m *ChatModel) RecordToHistory(text string) {
	m.addToHistory(text)
}

// navigateHistory handles up/down arrows for input history.
func (m *ChatModel) navigateHistory(direction int) {
	history := m.currentHistory()
	if len(history) == 0 {
		return
	}

	if m.historyIdx == -1 {
		if direction < 0 {
			// Going up - save current input and start browsing
			m.savedInput = m.textarea.Value()
			// Reset sharedclient so Up() stores our temporary
			m.history.Reset(m.sessionID)
			entry, ok := m.history.Up(m.sessionID, m.savedInput)
			if !ok {
				return
			}
			m.historyIdx = len(history) - 1
			m.textarea.SetValue(entry)
		}
		return // Can't go down from current input
	}

	newIdx := m.historyIdx + direction
	if newIdx < 0 {
		newIdx = 0
	} else if newIdx >= len(history) {
		// Back to current input - clear navigator state
		m.historyIdx = -1
		m.savedInput = ""
		m.history.Reset(m.sessionID)
		return
	}
	m.historyIdx = newIdx

	entry, ok := m.history.Down(m.sessionID, "")
	if !ok {
		// Reached end of history
		m.historyIdx = -1
		m.savedInput = ""
		m.history.Reset(m.sessionID)
		return
	}
	m.textarea.SetValue(entry)
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
		// Route mouse events based on position and active drag state.
		mousePos := msg.(interface{ Mouse() tea.Mouse }).Mouse()
		textareaStartY, textareaEndY := m.getTextareaBounds()
		inTextarea := textareaStartY >= 0 &&
			mousePos.Y >= textareaStartY && mousePos.Y <= textareaEndY

		switch msg := msg.(type) {
		case tea.MouseWheelMsg:
			// Mouse wheel always scrolls the viewport regardless of position
			lines := m.scrollSpeed
			if lines <= 0 {
				lines = 3
			}
			switch mousePos.Button {
			case tea.MouseWheelUp:
				m.viewport.ScrollUp(lines)
			case tea.MouseWheelDown:
				m.viewport.ScrollDown(lines)
			}
			return nil

		case tea.MouseClickMsg:
			if inTextarea {
				return m.HandleInputMouse(msg)
			}
			if m.isClickInViewportArea(mousePos) {
				return m.handleMousePress(msg)
			}
			return nil

		case tea.MouseMotionMsg:
			// Route drag to whichever handler owns the active drag
			if m.inputMouseDown {
				return m.HandleInputMouse(msg)
			}
			if m.mouseDown {
				return m.handleMouseDrag(msg)
			}
			return nil

		case tea.MouseReleaseMsg:
			// Route release to whichever handler owns the active drag
			if m.inputMouseDown {
				return m.HandleInputMouse(msg)
			}
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

		// Handle find bar keys first when the bar is visible
		if m.findBarVisible {
			if cmd, handled := m.handleFindBarKey(msg); handled {
				return cmd
			}
		}

		// ctrl+f toggles find bar (only when not already handled above)
		if msg.String() == "ctrl+f" {
			m.openFindBar()
			return nil
		}

		switch msg.String() {
		case "tab":
			// Cycle focus within chat view
			if m.CycleFocus() {
				// Return signal to parent to focus sidebar
				return func() tea.Msg { return ChatFocusSidebarMsg{} }
			}
			return nil

		case KeyEnter:
			// Enter sends message when focused on input.
			if m.focused != FocusInput {
				// Task chip navigation (05-navigation T2/W14): Enter on
				// a selected task-bearing reply opens the task detail.
				return m.handleChipEnter()
			}
			return m.handleEnter()

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
				// C4 read surface (05-navigation T3): expanding a task
				// reply lazily loads its persisted tool-call trace once
				// (bounded: no per-render RPC storm).
				if m.messages[m.selectedMsgIdx].Role == RoleAssistant {
					m.fetchTrace(m.messages[m.selectedMsgIdx])
				}
				m.updateViewport()
				return nil
			}

		case "w":
			// Why-this-route popover (C5, 05-navigation T3): load +
			// render the dispatch-trace evidence for the selected reply.
			if msg, _, ok := m.selectedAssistantMessage(); ok {
				m.fetchRoute(msg)
				m.updateViewport()
			}
			return nil

		case "q":
			// Quote selected message into the input (message-quotes leaf 05):
			// inserts a `> [msg <id>]` blockquote of the selected message at
			// the cursor, keeping prior input. No-op without a selection.
			if m.focused == FocusViewport {
				m.QuoteSelected()
				return nil
			}

		case "ctrl+l":
			// Clear chat history
			m.messages = []ChatMessage{}
			// F-C: the turns were submitted under the old conversation —
			// forget them so a later terminal event cannot become a
			// cross-session delivery that silently discards the result.
			m.clearPendingTurns()
			m.conversationID = generateConversationID()
			m.pendingMsgIdx = -1
			m.selectedMsgIdx = -1
			m.loading = false
			m.progressState = nil
			m.updateViewport()
			return nil

		case KeyEsc:
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
		if m.focused == FocusViewport && msg.Text != "" {
			m.SetFocus(FocusInput)
			var taCmd tea.Cmd
			m.textarea, taCmd = m.textarea.Update(msg)
			return taCmd
		}

	case STTResultMsg:
		m.recordingState = sttIdle
		if msg.Err != nil {
			return func() tea.Msg {
				return ChatToastMsg{
					Title:   "stt error",
					Message: msg.Err.Error(),
				}
			}
		}
		if strings.TrimSpace(msg.Text) == "" {
			return func() tea.Msg {
				return ChatToastMsg{
					Title:   "stt",
					Message: "no speech detected",
				}
			}
		}
		// Place transcription result in textarea
		m.textarea.SetValue(msg.Text)
		m.textarea.CursorEnd()
		if m.sttAutoSend {
			return m.doSendMessage()
		}
		return nil

	case uploadResultMsg:
		// finalize an asynchronous image upload started by
		// detectAndAttachFile. On success, attach the UploadID. On failure,
		// demote IsImage so subsequent send falls back to a path reference.
		// The matching attachment may already have been cleared if the user
		// sent or cleared attachments before the upload resolved; that is
		// benign.
		for i := range m.attachments {
			if m.attachments[i].Path != msg.path {
				continue
			}
			if msg.err != nil {
				slog.Warn("Image upload failed; falling back to path reference",
					"path", msg.path,
					"error", msg.err,
				)
				m.attachments[i].IsImage = false
				m.attachments[i].UploadID = ""
			} else {
				m.attachments[i].UploadID = msg.uploadID
			}
			break
		}
		return nil

	case ChatResponseMsg:
		m.loading = false

		// Clear progress state
		m.progressState = nil

		// Remove the pending message
		m.removePendingMessage()

		if msg.Err != nil {
			m.addMessage(RoleError, llm.UserMessage(msg.Err))
		} else {
			// Check if the reply is an async task ack
			if isAsyncAck, taskID, taskMessage := parseAsyncAck(msg.Reply); isAsyncAck {
				// Render as a detached task message
				content := fmt.Sprintf("┌ task detached ─────────────────────────────┐\n"+
					"│ %s\n"+
					"│ Task ID: %s\n"+
					"│ View progress: [ctrl+x 2] tasks\n"+
					"└─────────────────────────────────────────────┘",
					taskMessage,
					taskID,
				)
				m.addMessage(RoleSystem, content)
			} else if result := parseTaskResult(msg.Reply); result != nil {
				// Render as a task result message. DEDUPE (C1,
				// 04-clutter/02): the same first-wins rule as the
				// ChatTaskResultMsg card -- a JSON relay of a reply that
				// already rendered (terminal bubble or prior card) is
				// suppressed.
				suppress := result.IsSuccess && m.duplicateTaskCard(result.TaskID, result.Result)
				if !suppress {
					var content string
					if result.IsSuccess {
						content = fmt.Sprintf("┌ task completed ────────────────────────────┐\n"+
							"│ Task: \"%s\"\n"+
							"│ Steps: %d/%d completed\n",
							result.Name,
							result.Completed, result.Total,
						)
						// Unbounded: the daemon caps summaries at ~12k
						// (buildResultSummary); the client renders what
						// arrived verbatim (leaf 08 truncation fix).
						if result.Result != "" {
							content += fmt.Sprintf("│ %s\n", result.Result)
						}
						content += "└─────────────────────────────────────────────┘"
					} else {
						content = fmt.Sprintf("┌ task failed ───────────────────────────────┐\n"+
							"│ Task: \"%s\"\n"+
							"│ Steps: %d completed, %d failed of %d\n",
							result.Name,
							result.Completed, result.Failed, result.Total,
						)
						// Error text unbounded for the same reason.
						if result.Error != "" {
							content += fmt.Sprintf("│ Error: %s\n", result.Error)
						}
						content += "└─────────────────────────────────────────────┘"
					}
					m.addMessage(RoleSystem, content)
					if result.IsSuccess {
						m.noteTaskCardRendered(result.TaskID, result.Result)
					}
				}
			} else {
				m.addMessage(RoleAssistant, msg.Reply)

				// Trigger TTS if enabled
				if m.ttsEnabled && m.ttsManager != nil {
					if err := m.ttsManager.Speak(msg.Reply); err != nil {
						slog.Debug("TTS speak failed", "error", err)
						// Show toast notification for TTS error
						return func() tea.Msg {
							return ChatToastMsg{
								Title:   "tts error",
								Message: fmt.Sprintf("speech failed: %v", err),
							}
						}
					}
				}
			}

			// Track dirty message for persistence
			m.trackDirtyMessage(RoleAssistant, msg.Reply)

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
			slog.Warn("flush failed; messages remain in memory", "error", msg.Err)
		}
		return nil

	case findDebounceMsg:
		// Recompute matches only if a debounce is still pending (may have been
		// cleared by escape or replaced by a newer keystroke-driven recompute).
		if m.findBarVisible && m.findDebouncePending {
			m.findDebouncePending = false
			m.recomputeFindMatches()
		}
		return nil

	case SessionMessagesLoadedMsg:
		// The load has landed (success or error): the authoritative
		// transcript state is now on screen, so daemon-state cards may
		// render again (06-planning/01 session-load barrier).
		m.sessionLoadPending = false
		if msg.Err != nil || msg.SessionID != m.sessionID {
			return nil
		}
		// Convert server messages to ChatMessages and populate state
		m.loadServerMessages(msg.SessionID, msg.Messages)
		return nil

	case SessionDescriptionUpdatedMsg:
		if msg.SessionID == m.sessionID {
			switch {
			case msg.Skip:
				// A user rename intervened while auto-naming was in flight:
				// keep whatever the rename left, never paint over it.
			case msg.Generation != 0 && msg.Generation != m.descriptionGen:
				// Stale auto result from a superseded attempt (session was
				// reset/switched and re-named since): drop it.
			case msg.Generation != 0 && m.sessionDescription != "summarizing...":
				// The placeholder is gone - a rename (or session load) landed
				// while generation was in flight. The user's text wins.
			default:
				m.sessionDescription = msg.Description
			}
		}
		return nil

	case ChatDetachedTaskMsg:
		// Render a styled detached task message in chat
		content := fmt.Sprintf("┌ task detached ─────────────────────────────┐\n"+
			"│ Working on: \"%s\"\n"+
			"│ Task ID: %s\n"+
			"│ View progress: [ctrl+x 2] tasks\n"+
			"└─────────────────────────────────────────────┘",
			msg.TaskName,
			msg.TaskID,
		)
		m.addMessage(RoleSystem, content)
		return nil

	case turnSubmittedMsg:
		// The chat view does not render its own pending line for
		// submitted turns: the legacy pending "sending..." message
		// (created in doSendMessage) already shows live progress via
		// the shared progressState. This case exists so tests and the
		// parent app can assert a turn is now tracked; the await
		// goroutine was spawned in the submitTurnResultMsg case.
		return nil

	case turnProgressMsg:
		// Latest progress text wins for the tracked turn. Only a matching
		// conversation drives the shared pending line — other
		// conversations' progress must not clobber it.
		matched := false
		for _, pt := range m.pendingTurns {
			if pt.conversationID == msg.ConversationID {
				pt.progressText = msg.Text
				pt.lastActivity = time.Now()
				matched = true
			}
		}
		if matched && m.progressState != nil && msg.Text != "" {
			m.progressState.Stage = msg.Text
			m.progressState.LastUpdate = time.Now()
			m.updateProgressMessage()
		}
		return nil

	case turnTerminalMsg:
		return m.handleTurnTerminal(msg)

	case ChatSubmitErrorMsg:
		// The submission never started: clear the pending line and
		// surface the error honestly (error-styled, leaf 08).
		m.loading = false
		m.progressState = nil
		m.removePendingMessage()
		m.addMessage(RoleError, llm.UserMessage(msg.Err))
		m.updateViewport()
		return nil

	case ChatTaskResultMsg:
		// Render a styled task result message in chat. DEDUPE (C1):
		// the card and the turn.terminal bubble are the SAME logical
		// reply - if this reply already rendered (same task id on
		// either side, or the terminal-first interim match on
		// summary), the card is suppressed.
		if msg.State == StateCompleted && m.duplicateTaskCard(msg.TaskID, msg.ResultSummary) {
			return nil
		}
		stateLabel := StateCompleted
		if msg.State == StateFailed {
			stateLabel = StateFailed
		}
		content := fmt.Sprintf("┌ task %s ────────────────────────────────┐\n"+
			"│ Task: \"%s\"\n"+
			"│ Steps: %d/%d %s\n",
			stateLabel,
			msg.TaskName,
			msg.CompletedSteps, msg.TotalSteps, stateLabel,
		)
		// Unbounded summary: the daemon caps ResultSummary at ~12k
		// (buildResultSummary); the client renders what arrived
		// verbatim (leaf 08 truncation fix - the old 50-char cut
		// made completed-task answers unreadable).
		if msg.ResultSummary != "" {
			content += fmt.Sprintf("│ %s\n", msg.ResultSummary)
		}
		content += "└─────────────────────────────────────────────┘"
		role := RoleSystem
		if msg.State == StateFailed {
			role = RoleError
		}
		m.addMessage(role, content)
		if msg.State == StateCompleted {
			m.noteTaskCardRendered(msg.TaskID, msg.ResultSummary)
		}
		return nil

	case submitTurnResultMsg:
		// chat.submit ack arrived (async-turn-migration leaf 04):
		// register the turn and spawn the await goroutine. Never blocks.
		cmd := m.handleSubmitTurnResult(msg)
		if cmd != nil {
			// Claim the pending line for this turn so per-turn resolution
			// under concurrent submits removes the right line.
			m.claimPendingLine(msg.Ack.TurnID)
			// Initialize the shared progress state if the legacy pending
			// line has not been created yet (doSendMessage creates it).
			if m.progressState == nil {
				m.progressState = &ProgressState{Stage: "thinking...", StartedAt: time.Now()}
			}
		}
		return cmd

	case ProgressUpdateMsg:
		if m.progressState == nil {
			m.progressState = &ProgressState{StartedAt: time.Now()}
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
		// ACP harness attribution (stage=acp frames): carried so the
		// progress line names the acting harness. Non-acp frames leave
		// the previous values; the acp stage itself overwrites Stage.
		if msg.ACPAgent != "" {
			m.progressState.ACPAgent = msg.ACPAgent
			m.progressState.Persona = msg.Persona
		} else if msg.Stage == "acp" {
			// acp frame whose spec carried no binding (no acp_agent key):
			// drop stale attribution so the plain shape renders.
			m.progressState.ACPAgent = ""
			m.progressState.Persona = ""
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

	case ProgressTickMsg:
		// Re-render the pending message to update the elapsed timer.
		// Keep ticking as long as we're still loading.
		if m.loading && m.pendingMsgIdx >= 0 {
			m.updateProgressMessage()
			return tea.Tick(time.Second, func(time.Time) tea.Msg { return ProgressTickMsg{} })
		}
		return nil

	case AgentLifecycleMsg:
		// The loop publishes lifecycle under its OWN conversation id, which
		// the thread router has already rewritten to a thread-scoped id
		// (internal/agent/thread_router.go:140-151 ->
		// Session.GetOrCreateThread mints "<session conv id>-thread-<topic>-<n>"
		//; internal/agent/loop.go:2785 publishes that value verbatim). So the
		// payload NEVER equals the session conv id this model holds and exact
		// matching kept agentActive false forever — the bug that made ctrl+s
		// steering unreachable (tui-steer-01).
		//
		// The stable key is the SESSION CONVERSATION PREFIX that both forms
		// share: agentStartedFor reports whether msg.ConversationID belongs to
		// the session this model is bound to. That survives the loop's
		// conversation-id indirection (the thread suffix is stripped) while
		// still rejecting another session's loop, so a background agent in
		// session A can no longer steal the user's first message in session B.
		//
		// Ended clears on the same key, plus the loop-wide empty id.
		if msg.Active {
			if m.agentStartedFor(msg.ConversationID) {
				m.agentActive = true
			}
		} else if msg.ConversationID == "" || m.agentStartedFor(msg.ConversationID) {
			m.agentActive = false
			m.steerMode = false
			m.queueStatus = nil
		}
		return nil

	case SteeringResultMsg:
		if msg.Success {
			m.addMessage(RoleSystem, "[steering] message queued")
		} else {
			m.addMessage(RoleSystem, fmt.Sprintf("[steering] error: %v", msg.Err))
		}
		return nil

	case SteeringInjectedMsg:
		m.addMessage(RoleSystem, "[steering] steering message injected into agent queue")
		return nil

	case FollowUpResultMsg:
		if msg.Success {
			m.addMessage(RoleSystem, "[follow-up] message queued for processing")
		} else {
			m.addMessage(RoleSystem, fmt.Sprintf("[follow-up] error: %v", msg.Err))
		}
		return nil

	case FollowUpInjectedMsg:
		m.AddSystemMessage("[follow-up] follow-up message injected into agent queue")
		return nil

	case FollowUpRestoredMsg:
		m.AddSystemMessage(fmt.Sprintf("[follow-up] restored %d pending follow-up message(s)", msg.Count))
		return nil

	case PlanNotificationMsg:
		content := m.renderPlanNotification(msg)
		m.addMessage(RoleSystem, content)
		return nil

	case TaskGateApprovalMsg:
		// 06-planning/01: render the strategic approval card inline.
		// The card mirrors DAEMON state (a pending approval gate in
		// tasks.db), so it is marked localOnly: a session transcript
		// rebuild (session.messages.get landing late) must re-append it
		// rather than silently wipe it, and it must never be persisted.
		// Card state stays pending until task.approved/task.rejected
		// resolves the gate via TaskGateResolvedMsg (bughunt wave2), which
		// removes the card stamped with this task id.
		m.addMessage(RoleSystem, m.renderTaskGateCard(msg))
		m.messages[len(m.messages)-1].localOnly = true
		m.messages[len(m.messages)-1].gateTaskID = msg.TaskID
		return nil

	case TaskGateResolvedMsg:
		// The gate was decided (from any client): drop the pending card(s)
		// for this task and record the outcome in one system line.
		m.removeTaskGateCards(msg.TaskID)
		if msg.Approved {
			m.addMessage(RoleSystem, fmt.Sprintf("[task] gate approved: %s", msg.TaskID))
		} else if msg.Reason != "" {
			m.addMessage(RoleSystem, fmt.Sprintf("[task] gate rejected: %s -- %s", msg.TaskID, msg.Reason))
		} else {
			m.addMessage(RoleSystem, fmt.Sprintf("[task] gate rejected: %s", msg.TaskID))
		}
		m.messages[len(m.messages)-1].localOnly = true
		return nil

	case types.ThreadSwitchMsg:
		// If Reply is set, this is an RPC response — handle it
		if msg.Reply != nil {
			if msg.Err != nil {
				m.addMessage(RoleSystem, fmt.Sprintf("[thread] switch failed: %v", msg.Err))
			} else {
				m.addMessage(RoleSystem, "[thread] switched to thread: "+msg.ThreadID)
				// Refresh thread list after successful switch
				if m.rpc.IsConnected() {
					rpc := m.rpc
					sID := m.sessionID
					return func() tea.Msg {
						reply, err := rpc.Call("session.thread.list", map[string]string{
							"session_id": sID,
						})
						if err != nil {
							return ChatToastMsg{Title: "thread", Message: err.Error()}
						}
						return m.parseThreadListResponse(reply)
					}
				}
			}
			return nil
		}
		// First dispatch: no Reply yet, start the RPC call
		return m.switchThread(msg.ThreadID)

	case types.ThreadListChangedMsg:
		// Update the compact thread indicator from refreshed data
		if len(msg.Threads) > 0 {
			m.threadIndicator.threads = msg.Threads
			m.threadIndicator.activeIndex = -1
			for i, t := range msg.Threads {
				if t.ID == msg.ActiveID {
					m.threadIndicator.activeIndex = i
					break
				}
			}
			if m.threadIndicator.activeIndex < 0 {
				m.threadIndicator.activeIndex = 0
			}
		} else {
			m.threadIndicator.threads = []types.Thread{}
			m.threadIndicator.activeIndex = -1
		}
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
			if cmd := m.detectAndAttachFile(oldValue, newValue); cmd != nil {
				cmds = append(cmds, cmd)
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
	case RoleUser:
		prefix = "you: "
	case RoleAssistant:
		prefix = "meept: "
	case RoleParticipant:
		prefix = fmt.Sprintf("[%s] ", msg.SourceClient)
	case StatePending:
		prefix = ""
	case RoleSystem:
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

	// Try JSON formatting for assistant messages (format raw JSON responses)
	if msg.Role == RoleAssistant {
		trimmed := strings.TrimSpace(content)
		if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
			// Try to parse and format as JSON
			var js any
			if err := json.Unmarshal([]byte(trimmed), &js); err == nil {
				if formatted, err := json.MarshalIndent(js, "", "  "); err == nil {
					// Wrap in code block for display
					content = "```json\n" + string(formatted) + "\n```"
				}
			}
		}
	}

	// Try markdown rendering for assistant messages
	if m.renderMarkdown && m.mdRenderer != nil && msg.Role == RoleAssistant {
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

func (m *ChatModel) addMessage(role, content string) {
	m.messages = append(m.messages, ChatMessage{
		ID:        id.Generate("msg-"),
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
		State:     MessageNormal,
	})
	if m.findBarVisible && m.findInput.Value() != "" {
		m.recomputeFindMatches()
	} else {
		m.updateViewport()
	}
}

// removeTaskGateCards drops every transcript message stamped with the
// resolved gate task id (TaskGateResolvedMsg path, bughunt wave2). It also
// scrubs the per-session message storage so a session switch/reload cannot
// resurrect the decided card, and drops any parked gate card payloads the
// App pump has not drained yet (handled App-side). Selection indices are
// re-clamped after removal.
func (m *ChatModel) removeTaskGateCards(taskID string) {
	if taskID == "" {
		return
	}
	kept := m.messages[:0:0]
	removed := 0
	for _, cm := range m.messages {
		if cm.gateTaskID == taskID {
			removed++
			continue
		}
		kept = append(kept, cm)
	}
	if removed == 0 {
		return
	}
	m.messages = kept
	if m.selectedMsgIdx >= len(m.messages) {
		m.selectedMsgIdx = len(m.messages) - 1
	}
	for sid, msgs := range m.sessionMessages {
		if msgs == nil {
			continue
		}
		filtered := msgs[:0:0]
		hit := false
		for _, cm := range msgs {
			if cm.gateTaskID == taskID {
				hit = true
				continue
			}
			filtered = append(filtered, cm)
		}
		if hit {
			m.sessionMessages[sid] = filtered
		}
	}
	if m.findBarVisible && m.findInput.Value() != "" {
		m.recomputeFindMatches()
	} else {
		m.updateViewport()
	}
}

// AddSystemMessage adds a system notice to the chat transcript. Notices
// are client-local: the daemon never persists them, and an async session
// load can rebuild the transcript under them (C6/W26 silent-loss path),
// so they are marked localOnly and re-appended by every rebuild
// (preserveLocalNotices).
func (m *ChatModel) AddSystemMessage(content string) {
	m.addMessage(RoleSystem, content)
	if len(m.messages) > 0 {
		m.messages[len(m.messages)-1].localOnly = true
	}
}

// AddErrorMessage adds an error-styled transcript line (TUI error
// styling, 20261121-interactive-usability leaf 08): rendered through the
// model's errorStyle - bold palette-red, visually distinct from ordinary
// system notices. Client-local like AddSystemMessage.
func (m *ChatModel) AddErrorMessage(content string) {
	m.addMessage(RoleError, content)
	if len(m.messages) > 0 {
		m.messages[len(m.messages)-1].localOnly = true
	}
}

// preserveLocalNotices returns rebuilt with the transcript's localOnly
// notices re-appended (deduped by ID), so a server-driven transcript
// rebuild never silently drops them. Order: rebuilt first (the daemon's
// truth), then the client's notices in arrival order.
func preserveLocalNotices(rebuilt, existing []ChatMessage) []ChatMessage {
	seen := make(map[string]bool, len(rebuilt))
	for _, cm := range rebuilt {
		seen[cm.ID] = true
	}
	out := rebuilt
	for _, cm := range existing {
		if cm.localOnly && !seen[cm.ID] {
			out = append(out, cm)
		}
	}
	return out
}

// mergeFresherThanSnapshot appends to rebuilt the transcript messages
// created after the session load was armed (fresherThan), deduped by ID
// against the server truth. A zero fresherThan disables the merge (no
// arm stamp = the snapshot is fully authoritative).
func mergeFresherThanSnapshot(rebuilt, existing []ChatMessage, fresherThan time.Time) []ChatMessage {
	if fresherThan.IsZero() {
		return rebuilt
	}
	seen := make(map[string]bool, len(rebuilt))
	for _, cm := range rebuilt {
		seen[cm.ID] = true
	}
	out := rebuilt
	for _, cm := range existing {
		if cm.ID != "" && seen[cm.ID] {
			continue
		}
		if !cm.Timestamp.Before(fresherThan) {
			out = append(out, cm)
		}
	}
	return out
}

// AddParticipantMessage adds a message from a session participant to the chat transcript.
func (m *ChatModel) AddParticipantMessage(sourceClient, content string) {
	m.messages = append(m.messages, ChatMessage{
		ID:           id.Generate("msg-"),
		Role:         RoleParticipant,
		Content:      content,
		SourceClient: sourceClient,
		Timestamp:    time.Now().UTC(),
		State:        MessageNormal,
	})
	if m.findBarVisible && m.findInput.Value() != "" {
		m.recomputeFindMatches()
	} else {
		m.updateViewport()
	}
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

	// Build multimodal parts for image attachments. Non-image attachments are
	// surfaced as "[Attached file: <path>]" text references for the LLM.
	var parts []llm.ContentPart
	for _, att := range m.attachments {
		if att.IsImage && att.UploadID != "" {
			parts = append(parts, llm.ContentPart{
				Type:     "image_url",
				ImageURL: &llm.ImageRef{URL: "file://" + att.UploadID},
			})
		} else {
			actualText = fmt.Sprintf("[Attached file: %s]\n%s", att.Path, actualText)
		}
	}
	// When image parts are present, the text body must travel as a text-type
	// ContentPart so provider serializers emit a valid multimodal message.
	if len(parts) > 0 {
		parts = append(parts, llm.ContentPart{Type: "text", Text: actualText})
	}

	m.textarea.Reset()
	m.attachments = nil // Clear attachments after sending

	// Clear compressed pastes after sending
	m.compressedPastes = make(map[int]string)
	m.pasteCounter = 0

	// Route based on agent state. When loading is true (kickoff in flight) or
	// agentActive is true (agent is processing), queue as a follow-up. This
	// keeps the input field usable during the response window.
	if m.agentActive || m.loading {
		m.addToHistory(text)

		if m.steerMode {
			m.steerMode = false // Reset after sending
			m.addMessage(RoleUser, "[steering] "+actualText)
			m.trackDirtyMessage(RoleUser, "[steering] "+actualText)
			return m.SteerQueue(text)
		}
		// Follow-up mode
		m.AddSystemMessage("message queued (follow-up)")
		return m.FollowUpQueue(text)
	}

	// Agent is idle - normal chat
	m.addToHistory(text)

	// Add user message (with expanded content)
	m.addMessage(RoleUser, actualText)

	// Track dirty message for persistence
	m.trackDirtyMessage(RoleUser, actualText)

	// Initialize progress state
	m.progressState = &ProgressState{
		Stage:      "thinking...",
		StartedAt:  time.Now(),
		LastUpdate: time.Now(),
	}

	// Add pending "sending..." message immediately
	m.pendingMsgIdx = len(m.messages)
	m.messages = append(m.messages, ChatMessage{
		Role:      StatePending,
		Content:   m.renderProgressContent(),
		Timestamp: time.Now(),
		State:     MessageNormal,
		Progress:  m.progressState,
	})
	m.updateViewport()

	m.loading = true
	// Async turn path (async-turn-migration leaf 04): fire chat.submit
	// (returns the ack immediately — never agent work) and await the
	// turn.terminal result on a tea.Cmd goroutine so Update never blocks.
	return tea.Batch(
		m.submitChatAsync(actualText, parts),
		tea.Tick(time.Second, func(time.Time) tea.Msg { return ProgressTickMsg{} }),
	)
}

// submitChatAsync fires the chat.submit RPC as a tea.Cmd goroutine and
// returns either the resulting submitTurnResultMsg or a ChatSubmitErrorMsg
// if the daemon rejected the submission outright (transport error, or an
// accepted=false ack — nothing is running, so the user must hear that).
func (m *ChatModel) submitChatAsync(text string, parts []llm.ContentPart) tea.Cmd {
	return func() tea.Msg {
		ack, err := m.rpc.SubmitChat(context.Background(), text, m.sessionID, parts)
		if err != nil {
			return ChatSubmitErrorMsg{Err: err}
		}
		if !ack.Accepted {
			note := ack.Note
			if note == "" {
				note = "submission rejected"
			}
			return ChatSubmitErrorMsg{Err: fmt.Errorf("%s", note)}
		}
		return submitTurnResultMsg{Ack: ack}
	}
}

// submitTurnResultMsg carries the accepted chat.submit ack back into
// Update, which registers the turn and spawns the await goroutine.
type submitTurnResultMsg struct {
	Ack TurnSubmitAck
}

// ChatSubmitErrorMsg signals that a chat submission was NOT accepted —
// the turn never started, so the pending line must clear with an error.
type ChatSubmitErrorMsg struct {
	Err error
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
		case RoleUser:
			userCount++
			if firstUserContent == "" {
				firstUserContent = msg.Content
			}
		case RoleAssistant:
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
	m.descriptionGen++
	gen := m.descriptionGen

	return func() tea.Msg {
		// M13 (GUI parity, 025ba662): name the session from ITS OWN
		// bound project, never the globally active one. Unresolvable
		// bindings pass "" (no project context).
		projectName := resolveSessionProjectName(rpc, sessionID)
		// Try LLM-based generation first
		result, err := rpc.GenerateSessionDescription(sessionID, firstUserContent, projectName)
		if err != nil {
			// L16 race guard: generation failed AFTER a user rename may have
			// landed. The fallback write rides session.update_description -
			// the USER lane - so it would stamp the rename as user-set and
			// clobber it. Re-check the stored marker and skip the write when
			// a rename intervened. A lookup failure degrades to the legacy
			// fallback (logged inside the helper), never a silent drop.
			if sessionDescriptionUserSet(rpc, sessionID) {
				return SessionDescriptionUpdatedMsg{SessionID: sessionID, Generation: gen, Skip: true}
			}
			// Fallback to simple extraction
			desc := extractDescription(firstUserContent)
			if uerr := rpc.UpdateSessionDescription(sessionID, desc); uerr != nil {
				slog.Warn("session description update failed", "session_id", sessionID, "error", uerr)
			}
			return SessionDescriptionUpdatedMsg{SessionID: sessionID, Description: desc, Generation: gen}
		}
		// The daemon already saved both name and description (the auto lane
		// preserves a user rename server-side), so just return the result.
		return SessionDescriptionUpdatedMsg{
			SessionID:   sessionID,
			Name:        result.Name,
			Description: result.Description,
			Generation:  gen,
		}
	}
}

// sessionDescriptionUserSet reports whether the daemon's stored description
// for sessionID is user-authored (a rename landed while auto-naming was in
// flight). Best-effort: any RPC/decode failure returns false so the legacy
// fallback still runs - the failure is logged, never silent.
func sessionDescriptionUserSet(rpc RPCClient, sessionID string) bool {
	raw, err := rpc.Call("session.get", map[string]string{"id": sessionID})
	if err != nil {
		slog.Warn("session naming: rename re-check lookup failed", "session_id", sessionID, "error", err)
		return false
	}
	var sess types.Session
	if err := json.Unmarshal(raw, &sess); err != nil {
		slog.Warn("session naming: rename re-check decode failed", "session_id", sessionID, "error", err)
		return false
	}
	return sess.DescriptionUserSet
}

// resolveSessionProjectName resolves the project bound to sessionID for
// session naming (M13, GUI parity with project_provider.dart's
// resolveSessionProject): session.get supplies the session's own
// project_id/project_path, project.list supplies the candidates.
// Precedence: exact project_id match, then exact local_path match with a
// single trailing separator stripped from both sides. An unbound or
// unresolvable session yields "" — never the globally active project.
// Best-effort: RPC failures degrade to "" (naming still runs, without
// project context), logged so the failure is not silent.
func resolveSessionProjectName(rpc RPCClient, sessionID string) string {
	rawSession, err := rpc.Call("session.get", map[string]string{"id": sessionID})
	if err != nil {
		slog.Warn("session naming: session lookup failed", "session_id", sessionID, "error", err)
		return ""
	}
	var sess types.Session
	if err := json.Unmarshal(rawSession, &sess); err != nil {
		slog.Warn("session naming: session decode failed", "session_id", sessionID, "error", err)
		return ""
	}
	if sess.ProjectID == "" && sess.ProjectPath == "" {
		return "" // unbound session: no project context
	}

	rawProjects, err := rpc.Call("project.list", nil)
	if err != nil {
		slog.Warn("session naming: project list failed", "session_id", sessionID, "error", err)
		return ""
	}
	var resp types.ProjectListResponse
	if err := json.Unmarshal(rawProjects, &resp); err != nil {
		slog.Warn("session naming: project list decode failed", "session_id", sessionID, "error", err)
		return ""
	}

	// 1. Exact id match.
	for _, p := range resp.Projects {
		if sess.ProjectID != "" && p.ID == sess.ProjectID {
			return projectDisplayName(p)
		}
	}
	// 2. Path fallback for sessions bound by path before the daemon
	// recorded a project id: compare with one trailing separator
	// stripped from both sides (GUI normalization).
	if sess.ProjectPath != "" {
		want := strings.TrimSuffix(sess.ProjectPath, "/")
		for _, p := range resp.Projects {
			candidate := strings.TrimSuffix(p.LocalPath, "/")
			if candidate != "" && candidate == want {
				return projectDisplayName(p)
			}
		}
	}
	return ""
}

// projectDisplayName mirrors the GUI's name resolution: the project's
// name, falling back to its id when unnamed.
func projectDisplayName(p types.ProjectInfo) string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
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
		return "sending..."
	}

	var parts []string

	// Agent emoji + name (with acp harness attribution when the frame
	// carries one; see acpAttribution).
	if p.AgentID != "" {
		if label, ok := acpAttribution(p.AgentID, p.ACPAgent, p.Persona); ok {
			parts = append(parts, fmt.Sprintf("🤖 %s", label))
		} else {
			agentDisplay := p.AgentID
			if len(agentDisplay) > 12 {
				agentDisplay = agentDisplay[:12]
			}
			parts = append(parts, fmt.Sprintf("🤖 %s", agentDisplay))
		}
	}

	// Progress bar
	if p.Percent > 0 {
		barWidth := 20
		filled := int(p.Percent / 100 * float64(barWidth))
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		parts = append(parts, fmt.Sprintf("[%s %.0f%%]", bar, p.Percent))
	} else if p.Stage != "" {
		parts = append(parts, formatStageWithTimer(p.Stage, p.StartedAt))
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

// formatStageWithTimer renders a progress stage label with an elapsed
// timer when startedAt is known. The trailing "..." of the stage label
// is preserved after the timer, e.g. "thinking..." + 12s → "thinking 12s...".
// If startedAt is zero the stage is returned unchanged.
func formatStageWithTimer(stage string, startedAt time.Time) string {
	if startedAt.IsZero() {
		return stage
	}
	elapsed := time.Since(startedAt)
	var timer string
	if elapsed < 60*time.Second {
		timer = fmt.Sprintf("%ds", int(elapsed.Seconds()))
	} else {
		timer = fmt.Sprintf("%dm%ds", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	}
	// Insert the timer before a trailing "..." if present, otherwise append.
	if base, dots := strings.CutSuffix(stage, "..."); dots {
		return base + " " + timer + "..."
	}
	return stage + " " + timer
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
			ID:        fmt.Sprintf("msg-%d", sm.ID),
			Role:      sm.Role,
			Content:   sm.Content,
			Timestamp: ts,
			State:     MessageNormal,
		})
		// Populate history from user messages
		if sm.Role == RoleUser {
			history = append(history, sm.Content)
		}
	}

	// Restored transcripts carry no turn.terminal events, so the mode
	// badge (F13c) needs the persisted dispatch rows: stamp them before
	// the rebuild lands (06-planning/03).
	m.hydrateRestoredClassifications(sessionID, chatMsgs)

	m.sessionMessages[sessionID] = chatMsgs
	if sessionID == m.sessionID {
		// Rebuild from the server's truth, then re-append the client's
		// localOnly notices (C6/W26: a load landing after a notice must
		// not silently wipe it). A message created AFTER the load was
		// armed is fresher than the snapshot (a turn submitted while the
		// load was in flight: the user's message and its resolved
		// terminal bubble); the snapshot never saw it, so the rebuild
		// must keep it (06-planning/03: a late load must not erase the
		// reply the user just watched render).
		m.messages = preserveLocalNotices(
			mergeFresherThanSnapshot(chatMsgs, m.messages, m.sessionLoadStarted),
			m.messages,
		)
		m.updateViewport()
	}

	// Merge history (don't overwrite existing entries)
	if len(m.history.GetEntries(sessionID)) == 0 {
		for _, h := range history {
			m.history.Add(sessionID, h)
		}
	}
}

func (m *ChatModel) updateViewport() {
	var content strings.Builder

	// Clamp like the other width math in this file (chat.go:3749,
	// search.go:363): m.width is 0 until a WindowSizeMsg arrives, and
	// strings.Repeat panics on a negative count. A headless test program
	// (and any early render before the first size event) hits exactly that.
	thinSep := m.separatorStyle.Render(strings.Repeat("─", max(m.width-6, 1)))

	// Turn separator: a more visible line with turn number
	turnSepStyle := lipgloss.NewStyle().
		Foreground(paletteColor("border"))
	turnLabelStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Bold(true)

	selectedStartLine := -1
	currentLine := 0
	turnNumber := 0

	for i, msg := range m.messages {
		var style lipgloss.Style

		switch msg.Role {
		case RoleUser:
			style = m.userStyle
		case RoleAssistant:
			style = m.assistantStyle
		case RoleParticipant:
			style = m.systemStyle
		case StatePending:
			style = m.pendingStyle
		case RoleError:
			style = m.errorStyle
		case RoleSystem:
			style = m.systemStyle
		}

		isSelected := i == m.selectedMsgIdx
		if isSelected {
			selectedStartLine = currentLine
			style = style.Background(paletteColor("border"))
		}

		// Message threading: user messages and system messages that follow a user
		// message start a new conversation turn. Consecutive assistant/system
		// messages belong to the same turn.
		isNewTurn := false
		switch msg.Role {
		case RoleUser:
			isNewTurn = true
		case RoleParticipant:
			isNewTurn = true
		case RoleSystem:
			// System message starts a new turn only if the previous message
			// was not also a system message
			if i == 0 || m.messages[i-1].Role != RoleSystem {
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

		// Apply find-bar match highlighting before style.Render.
		msgContent = m.applyFindHighlight(i, msgContent)

		// Add timestamp for non-system messages
		var timestampStr string
		if msg.Role != RoleSystem && msg.Role != StatePending {
			timestampStr = msg.Timestamp.Format("15:04")
		}

		// Render message header with timestamp
		if timestampStr != "" {
			pointer := "  "
			copyHint := ""
			if isSelected {
				pointer = "▸ "
				copyHint = lipgloss.NewStyle().
					Foreground(paletteColor("success")).
					Bold(true).
					Render("  (c) copy  (q) quote")
			}
			header := lipgloss.NewStyle().
				Foreground(paletteColor("textMuted")).
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
				Foreground(paletteColor("textMuted")).
				Italic(true).
				Render("  [collapsed - press e to expand]")
			content.WriteString(indicator)
			content.WriteString("\n")
			currentLine++
		case MessageExpanded:
			indicator := lipgloss.NewStyle().
				Foreground(paletteColor("textMuted")).
				Italic(true).
				Render("  [expanded]")
			content.WriteString(indicator)
			content.WriteString("\n")
			currentLine++
			// C4 persisted tool-call trace rides the expansion
			// (05-navigation T3).
			if trace := m.renderTraceRowsFor(msg); trace != "" {
				content.WriteString(trace)
				currentLine += strings.Count(trace, "\n")
			}
		}

		// Mode badge (06-planning/03 T2, F13c/W17): a classified reply
		// carries its execution mode (plan / quickplan / direct / recall)
		// so the transcript delineates planning from direct execution.
		// The badge uses mapped labels only -- the raw handler_case enum
		// never leaks.
		if msg.Role == RoleAssistant {
			if badge := renderModeBadge(msg); badge != "" {
				content.WriteString(badge)
				content.WriteString("\n")
				currentLine++
			}
		}

		// Task chip (05-navigation T2, W14): a task-bearing reply
		// surfaces which task ran and is Enter-navigable to its detail.
		if msg.Role == RoleAssistant && msg.TaskID != "" {
			content.WriteString(renderTaskChip(msg.TaskID))
			content.WriteString("\n")
			currentLine++
		}

		// C5 why-this-route evidence panel (05-navigation T3).
		if route := m.renderRouteRowsFor(msg); route != "" {
			content.WriteString(route)
			currentLine += strings.Count(route, "\n")
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

	// Find bar (rendered above the viewport when visible)
	if m.findBarVisible {
		b.WriteString(m.renderFindBar())
		b.WriteString("\n")
	}

	// Thread indicator (compact header line; "" when no threads)
	if tiView := m.threadIndicator.View(); tiView != "" {
		b.WriteString(tiView)
		b.WriteString("\n")
	}

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
	// Account for find bar lines when visible (1 line, 2 with regex error).
	findLines := m.findBarLines()
	// Account for thread indicator line (shown only when threads exist).
	threadIndicatorLines := 0
	if m.threadIndicator.IsActive() {
		threadIndicatorLines = 1
	}
	// viewportContentHeight = height - 2(viewport borders) - copyHintLines - inputLines - 2(input borders) - completionsLines - queueIndicatorLines - findLines - threadIndicatorLines - 1(statusbar)
	viewportContentHeight := max(m.height-copyHintLines-inputLines-completionsLines-queueIndicatorLines-findLines-threadIndicatorLines-5, 1)

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

	// Show copy hint when there is an active selection

	// TTS speaking indicator
	ttsIndicator := m.renderTTSIndicator()
	if ttsIndicator != "" {
		b.WriteString(ttsIndicator)
		b.WriteString("\n")
	}
	if m.isSelecting && m.hasSelection() {
		copyHintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")). // palette-exempt: max-contrast literal
			Background(paletteColor("primary")).
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

	// Build input area: optional attachments line + textarea (or STT overlay)
	var inputContent strings.Builder

	if m.recordingState == sttRecording || m.recordingState == sttTranscribing {
		// STT recording/transcribing overlay: replace normal input area
		inputContent.WriteString(m.renderSTTOverlay())
	} else {
		// Normal input rendering

		// Show attached files in orange [filename.ext] format
		if len(m.attachments) > 0 {
			attachStyle := lipgloss.NewStyle().Foreground(paletteColor("primary")) // orange
			attachLabels := make([]string, 0, len(m.attachments))
			for _, att := range m.attachments {
				name := att.Filename
				if name == "" {
					name = filepath.Base(att.Path)
				}
				label := "[" + name + "]"
				if att.IsImage {
					label = "[img:" + name + "]"
				}
				attachLabels = append(attachLabels, attachStyle.Render(label))
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
					Foreground(paletteColor("textMuted")).
					Italic(true).
					Render(fmt.Sprintf(" [history %d/%d]", m.historyIdx+1, len(history)))
				inputView += historyIndicator
			}
			inputContent.WriteString(inputView)
		}
	}

	inputStyle := inputBorder.Width(m.width - 2)
	b.WriteString(inputStyle.Render(inputContent.String()))

	// Render slash command completions below input (only when / is typed and not recording)
	if m.recordingState == sttIdle {
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
				completionStyle := lipgloss.NewStyle().Foreground(paletteColor("textMuted")).PaddingLeft(2)
				b.WriteString(completionStyle.Render(completions))
			}
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
	// F-C: the turns were submitted under the old conversation — forget
	// them so a later terminal event cannot become a cross-session
	// delivery that silently discards the result.
	m.clearPendingTurns()
	m.conversationID = generateConversationID()
	m.sessionID = ""
	m.sessionDescription = ""
	m.threadIndicator.SetSession(nil)
	m.textarea.Reset()
	m.pendingMsgIdx = -1
	m.selectedMsgIdx = -1
	m.historyIdx = -1
	m.savedInput = ""
	m.loading = false
	m.progressState = nil
	m.closeFindBar()
	if m.history != nil {
		m.history.Reset("")
	}
	// Reset STT state
	if m.recordingState != sttIdle {
		m.cancelRecording()
	}
	m.lastEnterTime = time.Time{}
	m.updateViewport()
}

// sessionLoadSettled reports whether the deferred session.messages.get
// load (armed by the last SetSession) has landed. 06-planning/01 uses
// this to keep daemon-state cards (the task-gate approval card) out of
// the window where a transcript rebuild would wipe them: the load is
// authoritative server truth, so anything appended before it lands
// silently disappears.
func (m *ChatModel) IsSessionLoadSettled() bool {
	return !m.sessionLoadPending
}

// SetSession links the chat to a daemon session, preserving messages per session.
func (m *ChatModel) SetSession(session *types.Session) tea.Cmd {
	// Always close the find bar on any session change (including nil).
	m.closeFindBar()
	// Refresh the thread indicator from the new (or nil) session.
	m.threadIndicator.SetSession(session)
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
	if m.history != nil {
		m.history.Reset(m.sessionID)
	}
	m.sessionDescription = session.Description

	// Restore previous messages for this session, or start fresh
	if saved, ok := m.sessionMessages[session.ID]; ok && len(saved) > 0 {
		m.messages = make([]ChatMessage, len(saved))
		copy(m.messages, saved)
		m.updateViewport()
		return flushCmd
	}

	// No local messages - try to load from server. Client-local system
	// notices (C6 dropped-event warning, restore markers) survive the
	// fresh transcript: the daemon never persists them, so this view is
	// their only home until the session's own buffer takes over.
	m.messages = preserveLocalNotices([]ChatMessage{}, m.messages)
	m.updateViewport()

	if m.rpc.IsConnected() {
		rpc := m.rpc
		sessionID := session.ID
		// Mark the load in-flight: the event pump defers daemon-state
		// cards (task-gate approvals) until it lands (06-planning/01).
		m.sessionLoadPending = true
		m.sessionLoadStarted = time.Now()
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

// switchThread switches the active thread for the current session via RPC.
func (m *ChatModel) switchThread(threadID string) tea.Cmd {
	if threadID == "" || m.sessionID == "" || !m.rpc.IsConnected() {
		m.addMessage(RoleSystem, "[thread] cannot switch: no session or connection")
		return nil
	}

	rpc := m.rpc
	sessionID := m.sessionID
	tID := threadID
	m.addMessage(RoleSystem, "[thread] switching to thread: "+threadID)

	// Send the switch request
	return func() tea.Msg {
		reply, err := rpc.Call("session.thread.set_active", map[string]string{
			"session_id": sessionID,
			"thread_id":  tID,
		})
		if err != nil {
			return types.ThreadSwitchMsg{ThreadID: tID, Err: err}
		}
		return types.ThreadSwitchMsg{ThreadID: tID, Reply: reply}
	}
}

// parseThreadListResponse parses an RPC thread-list response into a
// ThreadListChangedMsg that refreshes the UI thread indicator.
func (m *ChatModel) parseThreadListResponse(data json.RawMessage) tea.Msg {
	// Expected format: {"threads": [{...}, ...], "count": N}
	var wrapper struct {
		Threads []json.RawMessage `json:"threads"`
		Count   int               `json:"count"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return ChatToastMsg{Title: "thread", Message: "failed to parse thread list"}
	}

	threads := make([]types.Thread, 0, len(wrapper.Threads))
	for _, raw := range wrapper.Threads {
		var t types.Thread
		if err := json.Unmarshal(raw, &t); err != nil {
			continue
		}
		threads = append(threads, t)
	}

	// Find active thread
	activeID := ""
	for _, t := range threads {
		if t.IsActive {
			activeID = t.ID
			break
		}
	}

	return types.ThreadListChangedMsg{
		Threads:  threads,
		ActiveID: activeID,
	}
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
		return StateNormal
	}
	switch m.vimState.Mode {
	case vim.ModeNormal:
		return StateNormal
	case vim.ModeInsert:
		return "insert"
	case vim.ModeVisual:
		return "visual"
	case vim.ModeCommand:
		return "command"
	default:
		return StateNormal
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

// IsMarkdownEnabled reports whether markdown rendering is currently on.
func (m *ChatModel) IsMarkdownEnabled() bool {
	return m.renderMarkdown
}

// pasteTokenRe matches "{paste: N lines}" tokens for expansion.
var pasteTokenRe = regexp.MustCompile(`\{paste: \d+ lines\}`)

// expandPasteTokens replaces {paste: N lines} tokens with their original content.
func (m *ChatModel) expandPasteTokens(text string) string {
	if len(m.compressedPastes) == 0 {
		return text
	}

	// Collect paste contents in reverse pasteID order (most recent first).
	var contents []string
	for id := m.pasteCounter; id >= 1; id-- {
		if content, exists := m.compressedPastes[id]; exists {
			contents = append(contents, content)
		}
	}

	if len(contents) == 0 {
		return text
	}

	// Replace each {paste: N lines} token with the corresponding content.
	// Tokens appear in the text in chronological order (oldest first), and
	// pasteIDs are assigned sequentially, so we match in forward order.
	result := text
	for _, content := range slices.Backward(contents) {
		loc := pasteTokenRe.FindStringIndex(result)
		if loc == nil {
			break
		}
		result = result[:loc[0]] + content + result[loc[1]:]
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
func parseAsyncAck(reply string) (ok bool, taskID, message string) {
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

// planCountLine renders the plan card's count row. A plan with no phases
// and no steps is a document proposal (evolver skill plans are markdown
// files awaiting approval — they never compile into phases), so it gets a
// description instead of the misleading literal "0 phases · 0 steps".
func planCountLine(phaseCount, stepCount int) string {
	if phaseCount == 0 && stepCount == 0 {
		return "document proposal · no executable steps"
	}
	phaseStr := fmt.Sprintf("%d phase", phaseCount)
	if phaseCount != 1 {
		phaseStr += "s"
	}
	stepStr := fmt.Sprintf("%d step", stepCount)
	if stepCount != 1 {
		stepStr += "s"
	}
	return phaseStr + " · " + stepStr
}

// planCardKeyHint renders the plans-view key hint: the configured
// ViewPlans keybinding when set, the shipped default "5" otherwise
// (09-cli-affordances task 3 — the hint used to hardcode "[5]").
func (m *ChatModel) planCardKeyHint() string {
	if m.plansViewKey == "" {
		return "5"
	}
	return m.plansViewKey
}

// SetPlansViewKey injects the configured plans-view keybinding
// (client.json5 command_palette.view_plans) so the plan card hint
// renders the real key. "" keeps the shipped default "5".
func (m *ChatModel) SetPlansViewKey(key string) {
	m.plansViewKey = key
}

// DocumentProposalLine returns the shared document-proposal wording
// (planCountLine(0, 0)) for surfaces that render a 0-phase/0-step evolver
// plan outside the chat card (plans tab/detail, sidebar). Single source
// of truth: no wording fork (06-planning/03 review checklist).
func DocumentProposalLine() string {
	return planCountLine(0, 0)
}

// renderTaskGateCard renders the approval card for a strategic task gate
// (06-planning/01, F13a): task id, step count, per-step summaries, and
// the approve/reject affordances. The affordances advertise the /plan
// approve|reject subcommands, which forward to task.approve /
// task.reject (C2; reject never executes).
func (m *ChatModel) renderTaskGateCard(msg TaskGateApprovalMsg) string {
	var b strings.Builder

	b.WriteString("+-- task awaiting approval -----------------------+\n")
	b.WriteString(fmt.Sprintf("| task: %s\n", msg.TaskID))
	stepLine := fmt.Sprintf("%d step", msg.Total)
	if msg.Total != 1 {
		stepLine += "s"
	}
	b.WriteString(fmt.Sprintf("| %s pending your approval\n", stepLine))
	for i, s := range msg.Steps {
		b.WriteString(fmt.Sprintf("|   %d. %s\n", i+1, s))
	}
	b.WriteString("| approve: /plan approve " + msg.TaskID + "\n")
	b.WriteString("| reject:  /plan reject " + msg.TaskID + " <reason>\n")
	b.WriteString("| (reject never executes the task)  |")
	return b.String()
}

// renderPlanNotification renders a styled inline notification for plan lifecycle events.
func (m *ChatModel) renderPlanNotification(msg PlanNotificationMsg) string {
	title := msg.Title
	if title == "" {
		title = msg.PlanID
	}
	title = types.TruncateString(title, 42)

	switch msg.State {
	case "submitting":
		// Evolver-origin plans are DOCUMENT proposals (a markdown file
		// awaiting approval) with no executable phases — the store's
		// plan_phases table is empty for them. Rendering the literal
		// counts made every evolver card read "0 phases · 0 steps",
		// which users correctly interpret as "nothing is happening"
		// (live complaint 2026-10-22). Say what the plan IS instead.
		countLine := planCountLine(msg.PhaseCount, msg.StepCount)
		return fmt.Sprintf("┌ plan ready for review ──────────────────────────┐\n"+
			"│ Plan: \"%s\"\n"+
			"│ %s\n"+
			"│ [%s] plans tab to review  ·  /plan approve %s\n"+
			"└─────────────────────────────────────────────────┘",
			title, countLine,
			m.planCardKeyHint(),
			types.TruncateString(msg.PlanID, 16))

	case "completed":
		return fmt.Sprintf("┌ plan completed ─────────────────────────────────┐\n"+
			"│ Plan: \"%s\"\n"+
			"│ %s · awaiting confirmation\n"+
			"│ [%s] plans tab to confirm\n"+
			"└─────────────────────────────────────────────────┘",
			title, planCountLine(msg.PhaseCount, msg.StepCount), m.planCardKeyHint())

	case "confirmed":
		confirmedBy := msg.By
		if confirmedBy == "" {
			confirmedBy = "user"
		}
		return fmt.Sprintf("┌ plan confirmed ─────────────────────────────────┐\n"+
			"│ Plan: \"%s\"\n"+
			"│ Confirmed by %s\n"+
			"└─────────────────────────────────────────────────┘",
			title, confirmedBy)

	case "rejected":
		reason := msg.Reason
		if reason == "" {
			reason = "no reason given"
		}
		return fmt.Sprintf("┌ plan rejected ──────────────────────────────────┐\n"+
			"│ Plan: \"%s\"\n"+
			"│ Reason: %s\n"+
			"└─────────────────────────────────────────────────┘",
			title, types.TruncateString(reason, 45))

	case "failed":
		// Leaf 03 (plan failure visibility): a plan that fails
		// mid-execution used to be silent in chat — the user only saw the
		// plan vanish. Render a failure card mirroring the rejected shape,
		// with the count row (planCountLine keeps evolver document
		// proposals saying what they ARE instead of "0 phases · 0 steps")
		// and the failure reason.
		reason := msg.Reason
		if reason == "" {
			reason = "no reason given"
		}
		return fmt.Sprintf("┌ plan failed ────────────────────────────────────┐\n"+
			"│ Plan: \"%s\"\n"+
			"│ %s\n"+
			"│ Reason: %s\n"+
			"└─────────────────────────────────────────────────┘",
			title, planCountLine(msg.PhaseCount, msg.StepCount),
			types.TruncateString(reason, 45))

	default:
		return fmt.Sprintf("┌ plan %s ────────────────────────────────────┐\n"+
			"│ Plan: \"%s\"\n"+
			"└─────────────────────────────────────────────────┘",
			msg.State, title)
	}
}

// detectAndAttachFile checks if new input content contains a file path and
// converts it to an attachment (shown as [filename] in the UI). Image files
// are uploaded asynchronously to the daemon's UploadService; the resulting
// UploadID is later attached to a multimodal ContentPart on send. Non-image
// files are stored as path-only attachment entries. The returned tea.Cmd, if
// non-nil, performs the upload and yields an uploadResultMsg that Update
// dispatches to finalize the attachment.
func (m *ChatModel) detectAndAttachFile(oldValue, newValue string) tea.Cmd {
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
		return nil
	}

	// Expand ~ to home directory
	if strings.HasPrefix(candidate, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			candidate = filepath.Join(home, candidate[2:])
		}
	}

	// Must be an absolute path
	if !filepath.IsAbs(candidate) {
		return nil
	}

	// Check if file exists and is a regular file
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return nil
	}

	// Avoid duplicates
	for _, att := range m.attachments {
		if att.Path == candidate {
			return nil
		}
	}

	// Build the attachment entry. For image files we kick off an
	// asynchronous upload so the TUI render loop is not blocked. The entry
	// is appended immediately with IsImage=true and an empty UploadID; the
	// returned tea.Cmd yields an uploadResultMsg that Update dispatches to
	// fill in the UploadID (or demote IsImage on failure). If the user
	// sends before the upload resolves, the send path treats an image
	// attachment with an empty UploadID as a path-only reference.
	entry := attachmentEntry{
		Path:     candidate,
		Filename: filepath.Base(candidate),
		IsImage:  isImageFile(candidate),
	}

	var uploadCmd tea.Cmd
	if entry.IsImage && m.rpc != nil && m.rpc.IsConnected() {
		rpc := m.rpc
		path := candidate
		uploadCmd = func() tea.Msg {
			uploadID, upErr := rpc.UploadFile(context.Background(), path)
			return uploadResultMsg{path: path, uploadID: uploadID, err: upErr}
		}
	}

	// Add to attachments and remove from textarea
	m.attachments = append(m.attachments, entry)

	// Remove the path from the textarea, keeping other content
	// Find and remove the added path segment from current textarea value
	currentVal := m.textarea.Value()
	cleaned := strings.Replace(currentVal, added, "", 1)
	cleaned = strings.TrimSpace(cleaned)
	m.textarea.SetValue(cleaned)

	return uploadCmd
}

// GetAttachments returns the list of attached file paths.
func (m *ChatModel) GetAttachments() []string {
	paths := make([]string, len(m.attachments))
	for i, att := range m.attachments {
		paths[i] = att.Path
	}
	return paths
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
		"skill",
		"project",
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
	orangeStyle := lipgloss.NewStyle().Foreground(paletteColor("primary"))
	greyStyle := lipgloss.NewStyle().Foreground(paletteColor("textMuted"))

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
		if v.Role == RoleUser {
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
		if v.Role == RoleUser {
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
		if v.Role == RoleUser {
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

// agentStartedFor reports whether an agent-lifecycle conversation id belongs
// to the session this model is bound to.
//
// The daemon resolves a turn's conversation id BEFORE the loop runs
// (internal/agent/handler.go:759-769): a chat.submit carrying session_id
// resolves to the session's OWN conversation id, and only then does the
// thread router re-key it for the LLM continuity lane
// (internal/agent/dispatcher.go:2915-2926 ->
// internal/agent/thread_router.go:140-151). Session.GetOrCreateThread mints a
// thread id as "<session conv id>-thread-<topic>-<n>"
// (internal/session/session.go:163), so the id the loop publishes
// (internal/agent/loop.go:2785) is that suffixed form — never equal to the
// session conv id the model holds.
//
// Comparing the SESSION CONVERSATION PREFIX is the stable key: it matches
// both the bare session conv and any of its thread-scoped forms, and
// rejects a different session's loop. Exact equality matches neither the
// thread-scoped form (so ctrl+s steering was dead, tui-steer-01) nor nothing
// at all (so a background agent in session A armed session B and the user's
// first message in B was queued as a steer into A).
func (m *ChatModel) agentStartedFor(conversationID string) bool {
	if conversationID == "" {
		// Loop-wide/unspecified id: cannot prove it is another session.
		return true
	}
	mine := m.conversationID
	if mine == "" {
		return false
	}
	if conversationID == mine {
		return true
	}
	// Thread-scoped form of THIS session's conversation.
	return strings.HasPrefix(conversationID, mine+"-thread-")
}

// SetAgentActive updates the agent active state. It applies only when the
// conversation id belongs to this model's session (see agentStartedFor).
func (m *ChatModel) SetAgentActive(active bool, conversationID string) {
	if m.agentStartedFor(conversationID) {
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
	return m.queueStatus.SteeringDepth > 0 || m.queueStatus.FollowUpDepth > 0 || len(m.queueStatus.TaskStates) > 0
}

// isTTSSpeaking returns true if TTS is currently speaking.
func (m *ChatModel) isTTSSpeaking() bool {
	return m.ttsEnabled && m.ttsManager != nil && m.ttsManager.IsSpeaking()
}

// renderTTSIndicator renders a speaker icon when TTS is active.
func (m *ChatModel) renderTTSIndicator() string {
	if !m.isTTSSpeaking() {
		return ""
	}

	// Animated speaker icon with pulse effect
	indicator := "🔊 speaking..."
	style := lipgloss.NewStyle().
		Foreground(paletteColor("primary")). // orange
		Bold(true)

	return style.Render(indicator)
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
		// B5 (leaf 05-navigation/03 T2): parked/planning tasks are part
		// of "work in flight" even with no active loop - badge them too.
		if n := len(m.queueStatus.TaskStates); n > 0 {
			badges = append(badges, m.followUpBadgeStyle.Render(fmt.Sprintf("parked: %d", n)))
		}
	}

	if len(badges) == 0 {
		return ""
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, badges...)
}

// ============================================================================
// Speech-to-Text Methods
// ============================================================================

// handleEnter processes the Enter key with STT-aware double-enter detection.
//
// State transitions:
//   - sttIdle + empty input + double-enter  -> sttRecording (or toast if unavailable)
//   - sttIdle + non-empty input              -> send message (normal)
//   - sttIdle + empty input + single-enter  -> nothing (normal)
//   - sttRecording + double-enter           -> sttTranscribing
//   - sttRecording + single-enter            -> nothing
//   - sttTranscribing                        -> nothing (wait for result)
func (m *ChatModel) handleEnter() tea.Cmd {
	input := strings.TrimSpace(m.textarea.Value())

	switch m.recordingState {
	case sttRecording:
		// Double-enter while recording: stop and transcribe.
		now := time.Now()
		if now.Sub(m.lastEnterTime) < doubleEnterTTL {
			return m.stopRecording()
		}
		m.lastEnterTime = now
		return nil

	case sttTranscribing:
		// Already transcribing, ignore enter.
		return nil

	default:
		// sttIdle: check for empty-input double-enter to start recording
		if input == "" && len(m.attachments) == 0 {
			now := time.Now()
			if now.Sub(m.lastEnterTime) < doubleEnterTTL {
				m.lastEnterTime = time.Time{} // reset
				return m.startRecording()
			}
			m.lastEnterTime = now
			return nil
		}
		// Normal send (has text or attachments)
		m.lastEnterTime = time.Time{}
		return m.doSendMessage()
	}
}

// startRecording begins speech-to-text recording.
// Returns a toast if STT is not enabled or the engine is unavailable.
func (m *ChatModel) startRecording() tea.Cmd {
	if !m.sttEnabled {
		return func() tea.Msg {
			return ChatToastMsg{
				Title:   "stt",
				Message: "speech-to-text is disabled",
			}
		}
	}
	if !m.sttAvailable {
		return func() tea.Msg {
			return ChatToastMsg{
				Title:   "stt",
				Message: fmt.Sprintf("%s not available", m.sttConfig.Engine),
			}
		}
	}
	if m.transcriber == nil {
		return func() tea.Msg {
			return ChatToastMsg{
				Title:   "stt",
				Message: "no transcriber configured",
			}
		}
	}

	m.recordingState = sttRecording
	err := m.transcriber.Start(context.Background(), func(r stt.Result) {
		// Partial results are ignored for now; we only use the final Stop() result.
	})
	if err != nil {
		m.recordingState = sttIdle
		return func() tea.Msg {
			return ChatToastMsg{
				Title:   "stt error",
				Message: err.Error(),
			}
		}
	}
	return nil
}

// stopRecording stops the transcriber and returns a command that yields
// the transcription result as an STTResultMsg.
func (m *ChatModel) stopRecording() tea.Cmd {
	m.recordingState = sttTranscribing
	t := m.transcriber
	return func() tea.Msg {
		text, err := t.Stop()
		return STTResultMsg{Text: text, Err: err}
	}
}

// cancelRecording stops the transcriber and discards the result.
func (m *ChatModel) cancelRecording() {
	if m.transcriber != nil && m.transcriber.IsRecording() {
		// Best-effort stop in a goroutine; we discard the result.
		// Capture the transcriber pointer locally to avoid reading m.transcriber
		// after it may have been replaced by a new recording session.
		t := m.transcriber
		go func() {
			if _, err := t.Stop(); err != nil {
				slog.Debug("transcriber stop returned error on cancel", "error", err)
			}
		}()
	}
	m.recordingState = sttIdle
}

// renderSTTOverlay renders the STT recording/transcribing overlay in place
// of the normal textarea content.
func (m *ChatModel) renderSTTOverlay() string {
	orange := paletteColor("primary")
	darkOrange := lipgloss.Color("#9A3412") // palette-exempt: max-contrast literal

	var content strings.Builder
	overlayHeight := 3

	if m.recordingState == sttRecording {
		// Center "speak to transcribe..." in bold black on orange
		promptStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")). // palette-exempt: max-contrast literal
			Bold(true)
		prompt := promptStyle.Render("speak to transcribe...")

		// Top padding line
		content.WriteString("\n")
		// Center the prompt within the available width
		availWidth := max(m.width-6, 1) // account for borders + padding
		promptWidth := lipgloss.Width(prompt)
		if promptWidth < availWidth {
			padding := (availWidth - promptWidth) / 2
			content.WriteString(strings.Repeat(" ", padding))
		}
		content.WriteString(prompt)
		content.WriteString("\n")

		// Bottom hint line
		hintStyle := lipgloss.NewStyle().
			Foreground(darkOrange).
			Italic(true)
		hint := hintStyle.Render("double-enter to stop  |  esc to cancel")
		hintWidth := lipgloss.Width(hint)
		if hintWidth < availWidth {
			padding := (availWidth - hintWidth) / 2
			content.WriteString(strings.Repeat(" ", padding))
		}
		content.WriteString(hint)
	} else {
		// sttTranscribing: dimmed "transcribing..."
		promptStyle := lipgloss.NewStyle().
			Foreground(darkOrange).
			Bold(true)
		prompt := promptStyle.Render("transcribing...")

		content.WriteString("\n")
		availWidth := max(m.width-6, 1)
		promptWidth := lipgloss.Width(prompt)
		if promptWidth < availWidth {
			padding := (availWidth - promptWidth) / 2
			content.WriteString(strings.Repeat(" ", padding))
		}
		content.WriteString(prompt)
		content.WriteString("\n")
		content.WriteString("\n") // extra padding for transcribing state
	}

	// Wrap in orange background style
	overlayStyle := lipgloss.NewStyle().
		Background(orange).
		Height(overlayHeight)
	return overlayStyle.Render(content.String())
}

// ============================================================================
// In-Session Find Bar
// ============================================================================

// findDebounceMsg is emitted by tea.Tick after the user pauses typing in the find bar.
type findDebounceMsg struct{}

// openFindBar shows the find bar, focuses the input, and clears any prior query.
func (m *ChatModel) openFindBar() {
	m.findBarVisible = true
	m.findInput.Focus()
	m.findInput.SetValue("")
	m.findMatches = nil
	m.findCursor = -1
	m.findRegexError = ""
	m.updateViewport()
}

// closeFindBar hides the find bar and clears all match state.
func (m *ChatModel) closeFindBar() {
	m.findBarVisible = false
	m.findInput.Blur()
	m.findInput.SetValue("")
	m.findMatches = nil
	m.findCursor = -1
	m.findRegexError = ""
	m.findDebouncePending = false
	m.updateViewport()
}

// handleFindBarKey processes keystrokes while the find bar is visible. Returns
// (cmd, true) if the key was consumed by the find bar.
func (m *ChatModel) handleFindBarKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case KeyEsc:
		m.closeFindBar()
		return nil, true
	case "alt+c":
		m.findCaseSensitive = !m.findCaseSensitive
		m.recomputeFindMatches()
		return nil, true
	case "alt+r":
		m.findRegex = !m.findRegex
		m.findRegexError = ""
		m.recomputeFindMatches()
		return nil, true
	case KeyEnter, "down":
		m.findNext()
		return nil, true
	case "shift+enter", "up":
		m.findPrev()
		return nil, true
	case "ctrl+f":
		// Already open: refocus and clear.
		m.findInput.Focus()
		m.findInput.SetValue("")
		m.recomputeFindMatches()
		return nil, true
	}

	// If the find input is focused, route printable keys and editor controls to it.
	if m.findInput.Focused() {
		// Only consume keys that textinput knows how to handle; let
		// ctrl+key combos not listed above pass through.
		switch msg.String() {
		case "left", "right", "home", "end", "ctrl+a", "ctrl+e", "backspace", "ctrl+h", "delete", "ctrl+w", "ctrl+u":
			var cmd tea.Cmd
			m.findInput, cmd = m.findInput.Update(msg)
			m.scheduleFindDebounce()
			return tea.Batch(cmd), true
		}

		// Printable character: forward to textinput.
		if msg.Text != "" && !strings.HasPrefix(msg.String(), "ctrl+") && !strings.HasPrefix(msg.String(), "alt+") && !strings.HasPrefix(msg.String(), "shift+") && msg.String() != " " {
			var cmd tea.Cmd
			m.findInput, cmd = m.findInput.Update(msg)
			m.scheduleFindDebounce()
			return tea.Batch(cmd), true
		}
		// Plain space
		if msg.String() == " " {
			var cmd tea.Cmd
			m.findInput, cmd = m.findInput.Update(msg)
			m.scheduleFindDebounce()
			return tea.Batch(cmd), true
		}
	}

	return nil, false
}

// scheduleFindDebounce marks a debounce as pending and returns a tick command
// that will fire after findDebounceDuration. Callers should batch the returned
// command into their update response.
func (m *ChatModel) scheduleFindDebounce() tea.Cmd {
	m.findDebouncePending = true
	return tea.Tick(findDebounceDuration, func(time.Time) tea.Msg {
		return findDebounceMsg{}
	})
}

// recomputeFindMatches scans m.messages for matches against the current query
// and toggles. It is called after the debounce tick fires, or immediately when
// toggles change. Resets findCursor to 0 when matches are found.
func (m *ChatModel) recomputeFindMatches() {
	query := m.findInput.Value()
	if query == "" {
		m.findMatches = nil
		m.findCursor = -1
		m.findRegexError = ""
		m.updateViewport()
		return
	}

	var matches []findMatch
	if m.findRegex {
		re, err := regexp.Compile(query)
		if err != nil {
			m.findMatches = nil
			m.findCursor = -1
			m.findRegexError = err.Error()
			m.updateViewport()
			return
		}
		m.findRegexError = ""
		for i := range m.messages {
			content := m.messages[i].Content
			if !m.findCaseSensitive {
				// (?i) makes the regex case-insensitive without recompiling text.
				// We compile a case-insensitive variant instead.
				lowerRe, lerr := regexp.Compile("(?i)" + query)
				if lerr == nil {
					re = lowerRe
				}
			}
			locs := re.FindAllStringIndex(content, -1)
			for _, loc := range locs {
				matches = append(matches, findMatch{messageIdx: i, charStart: loc[0], charEnd: loc[1]})
				if len(matches) >= findMaxMatches {
					break
				}
			}
			if len(matches) >= findMaxMatches {
				break
			}
		}
	} else {
		haystackNeedle := func(s string) (string, string) {
			if m.findCaseSensitive {
				return s, query
			}
			return strings.ToLower(s), strings.ToLower(query)
		}
		for i := range m.messages {
			content := m.messages[i].Content
			hay, needle := haystackNeedle(content)
			if needle == "" {
				continue
			}
			start := 0
			for {
				idx := strings.Index(hay[start:], needle)
				if idx < 0 {
					break
				}
				begin := start + idx
				// Match offsets are byte offsets in the lowercased string, which
				// match the original string for ASCII content. For non-ASCII
				// content, byte offsets in lowercase=UTF8 still align with the
				// original because UTF-8 is preserved by lowercasing.
				end := begin + len(needle)
				matches = append(matches, findMatch{messageIdx: i, charStart: begin, charEnd: end})
				if len(matches) >= findMaxMatches {
					break
				}
				start = end
			}
			if len(matches) >= findMaxMatches {
				break
			}
		}
	}

	m.findMatches = matches
	if len(matches) > 0 {
		m.findCursor = 0
	} else {
		m.findCursor = -1
	}
	m.updateViewport()
}

// findNext advances the cursor to the next match (wraps around) and scrolls.
func (m *ChatModel) findNext() {
	if len(m.findMatches) == 0 {
		return
	}
	m.findCursor = (m.findCursor + 1) % len(m.findMatches)
	m.updateViewport()
	m.scrollTofindCursor()
}

// findPrev moves the cursor to the previous match (wraps around) and scrolls.
func (m *ChatModel) findPrev() {
	if len(m.findMatches) == 0 {
		return
	}
	if m.findCursor <= 0 {
		m.findCursor = len(m.findMatches) - 1
	} else {
		m.findCursor--
	}
	m.updateViewport()
	m.scrollTofindCursor()
}

// scrollTofindCursor scrolls the viewport so the current match is centered when possible.
// Because updateViewport rebuilds the rendered string with ANSI escapes, line computation
// is approximate: it walks message line counters in the same order as updateViewport.
func (m *ChatModel) scrollTofindCursor() {
	if m.findCursor < 0 || m.findCursor >= len(m.findMatches) {
		return
	}
	target := m.findMatches[m.findCursor]
	if target.messageIdx < 0 || target.messageIdx >= len(m.messages) {
		return
	}

	// Approximate: compute the number of newlines before the matched message's
	// content block in the rendered output. We mirror the structure of
	// updateViewport (turn separators + header + content) so the line counter
	// stays in sync.
	line := 0
	turnNumber := 0
	for i := 0; i < target.messageIdx && i < len(m.messages); i++ {
		msg := m.messages[i]
		// New-turn separator or thin separator
		isNewTurn := false
		switch msg.Role {
		case RoleUser, RoleParticipant:
			isNewTurn = true
		case RoleSystem:
			if i == 0 || m.messages[i-1].Role != RoleSystem {
				isNewTurn = true
			}
		}
		if i > 0 {
			line++ // separator line
			if isNewTurn {
				turnNumber++
			}
		}
		// Header line for non-system, non-pending
		if msg.Role != RoleSystem && msg.Role != StatePending {
			line++
		}
		content := m.getMessageContent(msg)
		rendered := m.styleForRole(msg.Role).Render(content)
		line += strings.Count(rendered, "\n") + 1
	}

	// Position within the matched message: find the line offset of the match.
	content := m.getMessageContent(m.messages[target.messageIdx])
	upTo := content[:clampInt(target.charStart, 0, len(content))]
	line += strings.Count(upTo, "\n")

	// Center within viewport.
	height := m.viewport.Height()
	yoff := max(line-height/2, 0)
	totalLines := m.viewport.TotalLineCount()
	if yoff > totalLines-height {
		yoff = max(totalLines-height, 0)
	}
	m.viewport.SetYOffset(yoff)
}

// styleForRole returns the lipgloss style for a given role, mirroring updateViewport.
func (m *ChatModel) styleForRole(role string) lipgloss.Style {
	switch role {
	case RoleUser:
		return m.userStyle
	case RoleAssistant:
		return m.assistantStyle
	case RoleParticipant, RoleSystem:
		return m.systemStyle
	case RoleError:
		return m.errorStyle
	case StatePending:
		return m.pendingStyle
	default:
		return m.systemStyle
	}
}

// clampInt restricts v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// renderFindBar renders the find bar row: [input | N/M | Aa | .* | esc].
func (m *ChatModel) renderFindBar() string {
	inputView := m.findInput.View()

	count := "0/0"
	if m.findInput.Value() != "" {
		if len(m.findMatches) >= findMaxMatches {
			count = fmt.Sprintf("%d/%d+", clampInt(m.findCursor+1, 0, len(m.findMatches)), findMaxMatches)
		} else if len(m.findMatches) > 0 {
			count = fmt.Sprintf("%d/%d", clampInt(m.findCursor+1, 1, len(m.findMatches)), len(m.findMatches))
		} else {
			count = "0/0"
		}
	}

	countStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Padding(0, 1)
	toggleStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Padding(0, 1)
	toggleActiveStyle := lipgloss.NewStyle().
		Background(paletteColor("primary")).
		Foreground(lipgloss.Color("#000000")). // palette-exempt: max-contrast literal
		Bold(true).
		Padding(0, 1)
	closeStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Padding(0, 1)

	caseLabel := "Aa"
	caseRendered := toggleStyle.Render(caseLabel)
	if m.findCaseSensitive {
		caseRendered = toggleActiveStyle.Render(caseLabel)
	}
	regexLabel := ".*"
	regexRendered := toggleStyle.Render(regexLabel)
	if m.findRegex {
		regexRendered = toggleActiveStyle.Render(regexLabel)
	}

	parts := []string{
		inputView,
		countStyle.Render(count),
		caseRendered,
		regexRendered,
		closeStyle.Render("esc"),
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	if m.findRegexError != "" {
		errStyle := lipgloss.NewStyle().
			Foreground(paletteColor("error")).
			Padding(0, 1)
		bar = lipgloss.JoinVertical(lipgloss.Top, bar, errStyle.Render("regex: "+m.findRegexError))
	}

	barStyle := lipgloss.NewStyle().
		Background(paletteColor("surfaceAlt")).
		Padding(0, 1).
		Width(m.width - 2)
	return barStyle.Render(bar)
}

// applyFindHighlight wraps matched spans in msgContent with ANSI backgrounds.
// currentIdx is the absolute match index that should be highlighted brightly.
func (m *ChatModel) applyFindHighlight(msgIdx int, content string) string {
	if !m.findBarVisible || m.findInput.Value() == "" || len(m.findMatches) == 0 {
		return content
	}
	// Collect matches that belong to this message, in order.
	type span struct {
		start, end int
		current    bool
	}
	var spans []span
	for i, fm := range m.findMatches {
		if fm.messageIdx == msgIdx {
			spans = append(spans, span{start: fm.charStart, end: fm.charEnd, current: i == m.findCursor})
		}
	}
	if len(spans) == 0 {
		return content
	}

	// Sort by start (matches are appended in scanning order so already sorted).
	var b strings.Builder
	prev := 0
	normalBG := "\x1b[48;5;239m" // #3B3F45-ish
	normalFG := "\x1b[97m"
	currentBG := "\x1b[48;5;208m" // orange
	currentFG := "\x1b[30m"       // black
	reset := "\x1b[0m"
	for _, sp := range spans {
		if sp.start < prev {
			sp.start = prev
		}
		if sp.end > len(content) {
			sp.end = len(content)
		}
		if sp.start >= sp.end {
			continue
		}
		b.WriteString(content[prev:sp.start])
		if sp.current {
			b.WriteString(currentBG)
			b.WriteString(currentFG)
		} else {
			b.WriteString(normalBG)
			b.WriteString(normalFG)
		}
		b.WriteString(content[sp.start:sp.end])
		b.WriteString(reset)
		prev = sp.end
	}
	if prev < len(content) {
		b.WriteString(content[prev:])
	}
	return b.String()
}

// findBarLines returns the number of terminal lines the find bar occupies (0 if hidden).
func (m *ChatModel) findBarLines() int {
	if !m.findBarVisible {
		return 0
	}
	if m.findRegexError != "" {
		return 2
	}
	return 1
}
